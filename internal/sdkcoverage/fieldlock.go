package sdkcoverage

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// FieldLockFile is the lock's conventional name at the repository root, next to
// the coverage manifest.
const FieldLockFile = "sdk-fields.lock.yaml"

// SupportedFieldLockVersion is the only lock schema version this code
// understands. Bump it together with any breaking change to the layout.
const SupportedFieldLockVersion = 1

// FieldLock is the committed record of the field-level SDK shape a human last
// acknowledged, go.sum-style: the covered groups' models as they were when
// someone ran `sdkcoverage fields -write` and got the diff reviewed.
//
// The lock — not the pinned SDK version — is the drift baseline. A pinned-vs-
// latest diff evaporates the moment anything bumps go.mod (the scaffold job
// does, routinely) whether or not the drift was ever mapped; the lock stays
// stale until a PR that actually addresses the drift regenerates it, so
// regenerating it is the act of acceptance and the lock's git diff is the
// record of what was accepted.
type FieldLock struct {
	Version    int                    `yaml:"version"`
	SDKModule  string                 `yaml:"sdk_module"`
	SDKVersion string                 `yaml:"sdk_version,omitempty"`
	Groups     map[string]GroupModels `yaml:"groups"`
}

// Surface returns the lock's shape as a FieldSurface, the form the differ
// takes.
func (l FieldLock) Surface() FieldSurface {
	return FieldSurface{SDKVersion: l.SDKVersion, Groups: l.Groups}
}

// BuildFieldLock wraps a parsed surface in the lock's identity envelope.
func BuildFieldLock(fields FieldSurface) FieldLock {
	return FieldLock{
		Version:    SupportedFieldLockVersion,
		SDKModule:  SDKModulePath,
		SDKVersion: fields.SDKVersion,
		Groups:     fields.Groups,
	}
}

// LoadFieldLock reads and parses the lock at path.
func LoadFieldLock(path string) (FieldLock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FieldLock{}, fmt.Errorf("sdkcoverage: reading field lock: %w", err)
	}

	var lock FieldLock
	// KnownFields for the same reason as the manifest: a typo in a lock key must
	// be an error, not a line that silently stops meaning anything.
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&lock); err != nil {
		return FieldLock{}, fmt.Errorf("sdkcoverage: parsing %s: %w", path, err)
	}

	if lock.Version != SupportedFieldLockVersion {
		return FieldLock{}, fmt.Errorf(
			"sdkcoverage: %s declares version %d, but this build understands version %d",
			path, lock.Version, SupportedFieldLockVersion)
	}
	if lock.SDKModule != SDKModulePath {
		return FieldLock{}, fmt.Errorf(
			"sdkcoverage: %s declares sdk_module %q, but drift is computed against %q",
			path, lock.SDKModule, SDKModulePath)
	}

	if lock.Groups == nil {
		lock.Groups = map[string]GroupModels{}
	}
	return lock, nil
}

// Marshal renders the lock deterministically: sorted keys everywhere and one
// flow-style line per field, so two runs over the same SDK are byte-identical,
// lock diffs read like drift reports, and a merge conflict is resolved by
// regenerating rather than by hand-picking hunks.
//
// Rendering is by hand rather than through the yaml encoder because the
// encoder guarantees neither the flow layout nor the key order this file's
// diffability depends on. Loading still goes through the yaml decoder, which
// keeps the two sides honest: anything Marshal writes that the schema does not
// know fails the very next load.
func (l FieldLock) Marshal() []byte {
	var b strings.Builder

	b.WriteString("# Field-level shape of the covered latitudesh-go-sdk service groups, as last\n")
	b.WriteString("# acknowledged. Regenerate with `make fields-sync` (or `go run ./cmd/sdkcoverage\n")
	b.WriteString("# fields -write`) in the PR that maps — or deliberately omits — the change;\n")
	b.WriteString("# this file's git diff is the record of what that PR accepted.\n")
	b.WriteString("# See CONTRIBUTING.md, \"Field drift\".\n")
	fmt.Fprintf(&b, "version: %d\n", l.Version)
	fmt.Fprintf(&b, "sdk_module: %s\n", l.SDKModule)
	if l.SDKVersion != "" {
		fmt.Fprintf(&b, "sdk_version: %s\n", l.SDKVersion)
	}

	b.WriteString("groups:\n")
	for _, group := range sortedKeys(l.Groups) {
		fmt.Fprintf(&b, "  %s:\n", group)
		gm := l.Groups[group]

		if len(gm.Methods) > 0 {
			b.WriteString("    methods:\n")
			for _, name := range sortedKeys(gm.Methods) {
				m := gm.Methods[name]
				fmt.Fprintf(&b, "      %s: {signature: %s", name, strconv.Quote(m.Signature))
				if m.Deprecated {
					b.WriteString(", deprecated: true")
				}
				b.WriteString("}\n")
			}
		}

		if len(gm.Models) > 0 {
			b.WriteString("    models:\n")
			for _, name := range sortedKeys(gm.Models) {
				model := gm.Models[name]
				fmt.Fprintf(&b, "      %s:\n", name)
				if model.Doc != "" {
					fmt.Fprintf(&b, "        doc: %s\n", strconv.Quote(model.Doc))
				}
				if len(model.Enum) > 0 {
					fmt.Fprintf(&b, "        enum: [%s]\n", quotedList(model.Enum))
				}
				if len(model.Fields) > 0 {
					b.WriteString("        fields:\n")
					fields := append([]FieldShape(nil), model.Fields...)
					sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
					for _, f := range fields {
						b.WriteString("          - " + f.flowLine() + "\n")
					}
				}
			}
		}
	}

	return []byte(b.String())
}

// WriteFieldLock writes the lock to path — the one write this package ever
// performs, and only ever on request.
func WriteFieldLock(path string, lock FieldLock) error {
	if err := os.WriteFile(path, lock.Marshal(), 0o644); err != nil {
		return fmt.Errorf("sdkcoverage: writing field lock: %w", err)
	}
	return nil
}

// flowLine renders one field as a single flow-style YAML map, false and empty
// attributes omitted. strconv.Quote emits double-quoted strings, which YAML
// parses identically for the ASCII content these values carry.
func (f FieldShape) flowLine() string {
	parts := []string{
		"name: " + strconv.Quote(f.Name),
	}
	if f.Wire != "" {
		parts = append(parts, "wire: "+strconv.Quote(f.Wire))
	}
	parts = append(parts, "type: "+strconv.Quote(f.Type))
	if f.Optional {
		parts = append(parts, "optional: true")
	}
	if f.Default != "" {
		parts = append(parts, "default: "+strconv.Quote(f.Default))
	}
	if f.Deprecated {
		parts = append(parts, "deprecated: true")
	}
	if f.Doc != "" {
		parts = append(parts, "doc: "+strconv.Quote(f.Doc))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func quotedList(values []string) string {
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	quoted := make([]string, len(sorted))
	for i, v := range sorted {
		quoted[i] = strconv.Quote(v)
	}
	return strings.Join(quoted, ", ")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
