package sdkcoverage

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"hash/fnv"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// This file parses the SDK one level below surface.go: not which groups and
// methods exist, but the shape of the models those methods exchange. The group
// walk answers "is there a Terraform type for this?"; the field walk answers
// "has what that type maps changed since a human last looked?".
//
// Parsing stays purely syntactic, like ParseSurface: method signatures name
// their models with operations.* / components.* selectors, and both models/
// directories are flat single packages, so everything is reachable from the
// syntax tree alone — no type-checking, no network, just the module cache.

// FieldSurface is the field-level shape of a set of SDK service groups. The
// same structs serialize into sdk-fields.lock.yaml, so what the parser sees and
// what the lock records can never disagree about layout.
type FieldSurface struct {
	// SDKVersion labels which SDK tree the surface was parsed from. Informational:
	// the diff runs on shapes, never on version strings.
	SDKVersion string `yaml:"sdk_version,omitempty"`

	// Groups is keyed by the dotted group path, covered groups only. Uncovered
	// groups stay on the scaffold pipeline; recording their fields here would
	// just be noise nobody is accountable for yet.
	Groups map[string]GroupModels `yaml:"groups"`
}

// GroupModels is the field-level shape of one service group.
type GroupModels struct {
	// Methods maps each exported method to its rendered signature. A signature
	// is compared as one opaque string: any parameter or result change is drift,
	// and the old/new pair is its own explanation.
	Methods map[string]MethodShape `yaml:"methods,omitempty"`

	// Models are the operations.* and components.* types transitively reachable
	// from the methods' parameters and results, keyed by qualified name. A model
	// shared by several groups is recorded once per group, because each group's
	// mapping owner needs to see it drift.
	Models map[string]ModelShape `yaml:"models,omitempty"`
}

// MethodShape is one method's comparable identity.
type MethodShape struct {
	Signature  string `yaml:"signature"`
	Deprecated bool   `yaml:"deprecated,omitempty"`
}

// ModelShape is one model type: a struct with fields, or a string enum with
// values. Exactly one of Fields/Enum is populated; an empty struct has both nil.
type ModelShape struct {
	// Doc is a short hash of the type's doc comment. Speakeasy regenerates these
	// from the OpenAPI spec, so a doc-only change is often a behavior change the
	// types cannot show (a default rule documented away, a constraint reworded).
	Doc string `yaml:"doc,omitempty"`

	// Enum holds the wire values of a string enum, sorted.
	Enum []string `yaml:"enum,omitempty"`

	// Fields holds a struct's exported wire-relevant fields, sorted by name.
	Fields []FieldShape `yaml:"fields,omitempty"`
}

// FieldShape is one struct field as the wire sees it.
type FieldShape struct {
	// Name is the Go field name.
	Name string `yaml:"name"`

	// Wire is the name on the wire: the json tag, or the name= part of a
	// queryParam tag. Empty for untagged fields (response payload envelopes).
	Wire string `yaml:"wire,omitempty"`

	// Type is the field's type exactly as written, e.g. "*string" or
	// "[]CreateFirewallRules". Same-package references stay unqualified, which is
	// stable because models are keyed per package.
	Type string `yaml:"type"`

	// Optional means a pointer type or an omitempty json tag — either way the
	// wire can omit it. A required↔optional flip is a contract change.
	Optional bool `yaml:"optional,omitempty"`

	// Default is the `default:` tag value the SDK injects client-side. A changed
	// default alters behavior without touching any type.
	Default string `yaml:"default,omitempty"`

	Deprecated bool `yaml:"deprecated,omitempty"`

	// Doc is a short hash of the field's doc comment, for the same reason as
	// ModelShape.Doc.
	Doc string `yaml:"doc,omitempty"`
}

// ParseFieldSurface parses the field-level shape of the named groups from the
// SDK tree at dir. surface supplies each group's backing type name, which is
// how methods are resolved (never by field name — see Group.TypeName).
func ParseFieldSurface(dir string, surface Surface, groups []string) (FieldSurface, error) {
	root, err := indexModels(dir, ".")
	if err != nil {
		return FieldSurface{}, err
	}
	pkgs := map[string]*modelIndex{
		"operations": nil,
		"components": nil,
	}
	for name := range pkgs {
		idx, err := indexModels(dir, filepath.Join("models", name))
		if err != nil {
			return FieldSurface{}, err
		}
		pkgs[name] = idx
	}

	out := FieldSurface{
		SDKVersion: VersionFromDir(dir),
		Groups:     make(map[string]GroupModels, len(groups)),
	}
	for _, name := range groups {
		group, ok := surface.Groups[name]
		if !ok {
			return FieldSurface{}, fmt.Errorf("sdkcoverage: group %q not in the parsed surface", name)
		}
		out.Groups[name] = parseGroupModels(group, root, pkgs)
	}
	return out, nil
}

// parseGroupModels renders every method on the group's type and walks the
// models reachable from their signatures.
func parseGroupModels(group Group, root *modelIndex, pkgs map[string]*modelIndex) GroupModels {
	gm := GroupModels{
		Methods: make(map[string]MethodShape),
		Models:  make(map[string]ModelShape),
	}

	seen := map[string]bool{}
	for _, decl := range root.funcs[group.TypeName] {
		gm.Methods[decl.Name.Name] = MethodShape{
			Signature:  renderSignature(decl.Type),
			Deprecated: isDeprecated(decl.Doc),
		}
		for _, ref := range signatureModelRefs(decl.Type) {
			walkModel(ref, pkgs, gm.Models, seen)
		}
	}

	if len(gm.Methods) == 0 {
		gm.Methods = nil
	}
	if len(gm.Models) == 0 {
		gm.Models = nil
	}
	return gm
}

// modelRef is a qualified type reference, e.g. {pkg: "operations", name:
// "GetServersRequest"}.
type modelRef struct {
	pkg  string
	name string
}

func (r modelRef) String() string { return r.pkg + "." + r.name }

// walkModel records the model behind ref and recurses into every model its
// fields reference. seen guards cycles (WidgetData.Parent *WidgetData is legal
// and the components package has real self-references).
func walkModel(ref modelRef, pkgs map[string]*modelIndex, out map[string]ModelShape, seen map[string]bool) {
	if seen[ref.String()] {
		return
	}
	seen[ref.String()] = true

	idx, ok := pkgs[ref.pkg]
	if !ok || idx == nil {
		return
	}

	if values, ok := idx.enums[ref.name]; ok {
		out[ref.String()] = ModelShape{Doc: idx.docs[ref.name], Enum: values}
		return
	}

	raw, ok := idx.structs[ref.name]
	if !ok {
		// Not declared in the models packages: a leaf like types.Date or a
		// helper from internal/. The referencing field's Type string still
		// carries its name, so a change there is not invisible — just opaque.
		return
	}

	shape := ModelShape{Doc: idx.docs[ref.name]}
	for _, field := range raw {
		fs, refs, keep := field.shape(ref.pkg)
		if !keep {
			continue
		}
		shape.Fields = append(shape.Fields, fs)
		for _, next := range refs {
			walkModel(next, pkgs, out, seen)
		}
	}
	sort.Slice(shape.Fields, func(i, j int) bool { return shape.Fields[i].Name < shape.Fields[j].Name })
	out[ref.String()] = shape
}

// modelIndex is the raw syntactic index of one flat package: struct fields,
// string-enum values, docs, and method declarations by receiver.
type modelIndex struct {
	structs map[string][]rawField
	enums   map[string][]string
	docs    map[string]string
	funcs   map[string][]*ast.FuncDecl
}

// rawField is one struct field before shaping: everything still attached to
// the syntax it came from.
type rawField struct {
	name    string
	typ     ast.Expr
	tag     string
	doc     string
	depr    bool
	funcTyp bool
}

// shape turns a raw field into its recorded form plus the model references its
// type carries. keep is false for fields the wire never sees: json:"-" infra
// (HTTPMeta) and func-typed pagination helpers (Next).
func (f rawField) shape(pkg string) (FieldShape, []modelRef, bool) {
	tag := reflect.StructTag(f.tag)
	jsonName, jsonOpts := splitTag(tag.Get("json"))
	if jsonName == "-" || f.funcTyp {
		return FieldShape{}, nil, false
	}

	wire := jsonName
	if wire == "" {
		wire = queryParamName(tag.Get("queryParam"))
	}

	typeStr := types.ExprString(f.typ)
	return FieldShape{
		Name:       f.name,
		Wire:       wire,
		Type:       typeStr,
		Optional:   strings.HasPrefix(typeStr, "*") || hasOption(jsonOpts, "omitempty"),
		Default:    tag.Get("default"),
		Deprecated: f.depr,
		Doc:        f.doc,
	}, exprModelRefs(f.typ, pkg), true
}

// indexModels parses every non-test .go file in dir/sub and indexes what the
// field walk needs. A missing sub directory is not an error: a fixture SDK (or
// a future SDK layout change) without one simply has no models there.
func indexModels(dir, sub string) (*modelIndex, error) {
	idx := &modelIndex{
		structs: map[string][]rawField{},
		enums:   map[string][]string{},
		docs:    map[string]string{},
		funcs:   map[string][]*ast.FuncDecl{},
	}

	pattern := filepath.Join(dir, sub, "*.go")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("sdkcoverage: globbing %s: %w", pattern, err)
	}
	sort.Strings(files)

	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("sdkcoverage: parsing %s: %w", path, err)
		}
		idx.addModelFile(file)
	}

	for name, values := range idx.enums {
		sort.Strings(values)
		idx.enums[name] = values
	}
	return idx, nil
}

func (idx *modelIndex) addModelFile(file *ast.File) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil || d.Name == nil || !d.Name.IsExported() {
				continue
			}
			if recv := receiverTypeName(d.Recv); recv != "" {
				idx.funcs[recv] = append(idx.funcs[recv], d)
			}

		case *ast.GenDecl:
			switch d.Tok {
			case token.TYPE:
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					doc := ts.Doc
					if doc == nil {
						doc = d.Doc
					}
					idx.docs[ts.Name.Name] = docHash(doc)
					if st, ok := ts.Type.(*ast.StructType); ok {
						idx.structs[ts.Name.Name] = rawFields(st)
					}
				}

			case token.CONST:
				// Speakeasy enums are const blocks of typed string literals:
				//   const ( CreateFirewallProtocolTCP CreateFirewallProtocol = "TCP" ... )
				// The wire value is the literal, so that is what gets recorded.
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok || vs.Type == nil {
						continue
					}
					ident, ok := vs.Type.(*ast.Ident)
					if !ok {
						continue
					}
					for _, value := range vs.Values {
						if lit, ok := value.(*ast.BasicLit); ok && lit.Kind == token.STRING {
							// Unquote, never Trim: the wire value is what the
							// literal MEANS, and Trim would keep escapes verbatim
							// (or over-trim a value ending in an escaped quote).
							if v, err := strconv.Unquote(lit.Value); err == nil {
								idx.enums[ident.Name] = append(idx.enums[ident.Name], v)
							}
						}
					}
				}
			}
		}
	}
}

func rawFields(st *ast.StructType) []rawField {
	if st.Fields == nil {
		return nil
	}

	var fields []rawField
	for _, field := range st.Fields.List {
		var tag string
		if field.Tag != nil {
			tag = strings.Trim(field.Tag.Value, "`")
		}
		doc := docHash(field.Doc)
		depr := isDeprecated(field.Doc)
		_, isFunc := field.Type.(*ast.FuncType)

		names := field.Names
		if len(names) == 0 {
			// An embedded field has no name of its own; record it under its
			// terminal type identifier, like Go promotes it. Skipping it instead
			// would drop the embedded model and every promoted field from the
			// surface — a change there would then be invisible, the exact
			// silent-absorption this parser exists to prevent.
			if ident := terminalIdent(field.Type); ident != nil {
				names = []*ast.Ident{ident}
			}
		}
		for _, name := range names {
			if !name.IsExported() {
				continue
			}
			fields = append(fields, rawField{
				name:    name.Name,
				typ:     field.Type,
				tag:     tag,
				doc:     doc,
				depr:    depr,
				funcTyp: isFunc,
			})
		}
	}
	return fields
}

// terminalIdent unwraps a type expression to the identifier an embedded field
// is promoted under: *Base -> Base, components.Base -> Base.
func terminalIdent(expr ast.Expr) *ast.Ident {
	switch t := expr.(type) {
	case *ast.Ident:
		return t
	case *ast.StarExpr:
		return terminalIdent(t.X)
	case *ast.SelectorExpr:
		return t.Sel
	case *ast.IndexExpr:
		return terminalIdent(t.X)
	}
	return nil
}

// renderSignature renders a method's parameter and result types, e.g.
// "(context.Context, string, ...operations.Option) (*operations.GetServerResponse, error)".
// Only types, never parameter names: a rename that changes no type changes
// nothing on the wire.
func renderSignature(ft *ast.FuncType) string {
	render := func(list *ast.FieldList) string {
		if list == nil {
			return "()"
		}
		var parts []string
		for _, field := range list.List {
			typ := types.ExprString(field.Type)
			// An anonymous parameter is one entry; named ones repeat the type
			// once per name, matching how the signature reads in the source.
			n := len(field.Names)
			if n == 0 {
				n = 1
			}
			for i := 0; i < n; i++ {
				parts = append(parts, typ)
			}
		}
		return "(" + strings.Join(parts, ", ") + ")"
	}
	return render(ft.Params) + " " + render(ft.Results)
}

// signatureModelRefs extracts the operations.*/components.* references from a
// method's parameters and results — the entry points of the model walk.
func signatureModelRefs(ft *ast.FuncType) []modelRef {
	var refs []modelRef
	collect := func(list *ast.FieldList) {
		if list == nil {
			return
		}
		for _, field := range list.List {
			// The root package has no bare model idents, so pkg "" makes
			// exprModelRefs keep only qualified references.
			refs = append(refs, exprModelRefs(field.Type, "")...)
		}
	}
	collect(ft.Params)
	collect(ft.Results)
	return refs
}

// exprModelRefs finds the model references inside a type expression. pkg is the
// package the expression appears in: a bare Ident there is a same-package
// reference, while a SelectorExpr names its package explicitly.
func exprModelRefs(expr ast.Expr, pkg string) []modelRef {
	var refs []modelRef
	ast.Inspect(expr, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.SelectorExpr:
			if x, ok := t.X.(*ast.Ident); ok {
				if x.Name == "operations" || x.Name == "components" {
					refs = append(refs, modelRef{pkg: x.Name, name: t.Sel.Name})
				}
				// Any selector's parts are fully handled here; descending would
				// misread the Sel as a bare Ident.
				return false
			}
		case *ast.Ident:
			if pkg != "" && t.IsExported() {
				refs = append(refs, modelRef{pkg: pkg, name: t.Name})
			}
		}
		return true
	})
	return refs
}

// docHash reduces a doc comment to a short stable fingerprint. Whitespace is
// normalized first so a re-wrap is not drift; the fingerprint is compared for
// equality only, so eight hex digits are plenty.
func docHash(doc *ast.CommentGroup) string {
	if doc == nil {
		return ""
	}
	text := strings.Join(strings.Fields(doc.Text()), " ")
	if text == "" {
		return ""
	}
	h := fnv.New32a()
	h.Write([]byte(text))
	return fmt.Sprintf("%08x", h.Sum32())
}

func isDeprecated(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	return strings.Contains(doc.Text(), "Deprecated:")
}

func splitTag(tag string) (name string, opts []string) {
	parts := strings.Split(tag, ",")
	return parts[0], parts[1:]
}

func hasOption(opts []string, want string) bool {
	for _, o := range opts {
		if o == want {
			return true
		}
	}
	return false
}

// queryParamName extracts the name= part of a Speakeasy queryParam tag, e.g.
// `queryParam:"style=form,explode=true,name=filter[project]"` -> "filter[project]".
func queryParamName(tag string) string {
	for _, part := range strings.Split(tag, ",") {
		if v, ok := strings.CutPrefix(part, "name="); ok {
			return v
		}
	}
	return ""
}
