package sdkcoverage

import (
	"reflect"
	"testing"
)

const (
	fieldSDKv1 = "testdata/fieldsdk/v1"
	fieldSDKv2 = "testdata/fieldsdk/v2"
)

// parseFieldFixture parses one of the fieldsdk fixtures with Widgets as the
// only covered group — the drift tests' shared setup.
func parseFieldFixture(t *testing.T, dir string) FieldSurface {
	t.Helper()

	surface, err := ParseSurface(dir)
	if err != nil {
		t.Fatalf("ParseSurface(%s): %v", dir, err)
	}
	fields, err := ParseFieldSurface(dir, surface, []string{"Widgets"})
	if err != nil {
		t.Fatalf("ParseFieldSurface(%s): %v", dir, err)
	}
	return fields
}

func TestParseFieldSurfaceModels(t *testing.T) {
	fields := parseFieldFixture(t, fieldSDKv1)

	widgets, ok := fields.Groups["Widgets"]
	if !ok {
		t.Fatal("Widgets not parsed")
	}

	// Everything reachable from the five methods' signatures — and nothing
	// else. Notably absent: components.HTTPMetadata (its field is json:"-"),
	// and every Gadget model (Gadgets is not covered).
	wantModels := []string{
		"components.Pagination",
		"components.Widget",
		"components.WidgetData",
		"components.WidgetList",
		"operations.CreateWidgetResponse",
		"operations.CreateWidgetWidgetsRequestBody",
		"operations.DeleteWidgetResponse",
		"operations.GetWidgetResponse",
		"operations.LegacyWidgetResponse",
		"operations.ListWidgetsRequest",
		"operations.ListWidgetsResponse",
		"operations.WidgetColor",
	}
	if got := sortedKeys(widgets.Models); !reflect.DeepEqual(got, wantModels) {
		t.Errorf("models:\n got %v\nwant %v", got, wantModels)
	}

	wantMethods := []string{"Create", "Delete", "Get", "Legacy", "List"}
	if got := sortedKeys(widgets.Methods); !reflect.DeepEqual(got, wantMethods) {
		t.Errorf("methods:\n got %v\nwant %v", got, wantMethods)
	}
}

func TestParseFieldSurfaceSignatures(t *testing.T) {
	widgets := parseFieldFixture(t, fieldSDKv1).Groups["Widgets"]

	want := map[string]MethodShape{
		"List": {Signature: "(context.Context, operations.ListWidgetsRequest, ...operations.Option) (*operations.ListWidgetsResponse, error)"},
		"Get":  {Signature: "(context.Context, string, ...operations.Option) (*operations.GetWidgetResponse, error)"},
	}
	for name, shape := range want {
		if got := widgets.Methods[name]; got != shape {
			t.Errorf("%s = %+v, want %+v", name, got, shape)
		}
	}
	if widgets.Methods["Legacy"].Deprecated {
		t.Error("Legacy is not deprecated in v1")
	}
}

func TestParseFieldSurfaceFieldShapes(t *testing.T) {
	widgets := parseFieldFixture(t, fieldSDKv1).Groups["Widgets"]

	request := widgets.Models["operations.ListWidgetsRequest"]
	byName := map[string]FieldShape{}
	for _, f := range request.Fields {
		byName[f.Name] = f
	}

	// queryParam name= extraction, the default: tag, and pointer optionality.
	pageSize := byName["PageSize"]
	if pageSize.Wire != "page[size]" || pageSize.Default != "20" || !pageSize.Optional || pageSize.Type != "*int64" {
		t.Errorf("PageSize = %+v", pageSize)
	}
	if f := byName["FilterName"]; f.Wire != "filter[name]" {
		t.Errorf("FilterName wire = %q", f.Wire)
	}

	// json tags: a value field without omitempty is required.
	body := widgets.Models["operations.CreateWidgetWidgetsRequestBody"]
	for _, f := range body.Fields {
		if f.Name == "Name" && (f.Optional || f.Wire != "name" || f.Type != "string") {
			t.Errorf("Name = %+v", f)
		}
	}

	// Enums record sorted wire values.
	if enum := widgets.Models["operations.WidgetColor"].Enum; !reflect.DeepEqual(enum, []string{"blue", "red"}) {
		t.Errorf("WidgetColor enum = %v", enum)
	}

	// The self-referencing Parent field proves the cycle guard; its presence
	// proves untagged recursion into same-package types.
	data := widgets.Models["components.WidgetData"]
	var sawParent bool
	for _, f := range data.Fields {
		if f.Name == "Parent" {
			sawParent = true
		}
		if f.Name == "Status" && f.Optional {
			t.Errorf("Status should be required in v1: %+v", f)
		}
	}
	if !sawParent {
		t.Error("WidgetData.Parent not recorded")
	}

	// The pagination helper is not a wire field.
	response := widgets.Models["operations.ListWidgetsResponse"]
	for _, f := range response.Fields {
		if f.Name == "Next" {
			t.Error("func-typed Next must be filtered")
		}
		if f.Name == "HTTPMeta" {
			t.Error("json:\"-\" HTTPMeta must be filtered")
		}
	}

	// Embedded fields are recorded under their promoted name and their model is
	// walked — an embedding the parser skipped would be a permanent blind spot.
	list := widgets.Models["components.WidgetList"]
	var sawEmbedded bool
	for _, f := range list.Fields {
		if f.Name == "Pagination" && f.Type == "Pagination" {
			sawEmbedded = true
		}
	}
	if !sawEmbedded {
		t.Error("embedded Pagination not recorded on WidgetList")
	}
}

func TestParseFieldSurfaceDeterministic(t *testing.T) {
	a := parseFieldFixture(t, fieldSDKv1)
	b := parseFieldFixture(t, fieldSDKv1)
	if !reflect.DeepEqual(a, b) {
		t.Error("two parses of the same tree differ")
	}
}

// TestParseFieldSurfacePinnedSDK is the smoke test against the real SDK: the
// walk must terminate and produce models for a real covered group, whatever
// shape the pinned release has.
func TestParseFieldSurfacePinnedSDK(t *testing.T) {
	dir, err := PinnedModuleDir(SDKModulePath)
	if err != nil {
		t.Skipf("pinned SDK unavailable: %v", err)
	}

	surface, err := ParseSurface(dir)
	if err != nil {
		t.Fatalf("ParseSurface: %v", err)
	}
	fields, err := ParseFieldSurface(dir, surface, []string{"Servers"})
	if err != nil {
		t.Fatalf("ParseFieldSurface: %v", err)
	}

	servers := fields.Groups["Servers"]
	if len(servers.Methods) == 0 || len(servers.Models) == 0 {
		t.Fatalf("Servers parsed empty: %d methods, %d models", len(servers.Methods), len(servers.Models))
	}
}
