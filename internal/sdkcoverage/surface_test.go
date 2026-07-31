package sdkcoverage

import (
	"path/filepath"
	"reflect"
	"testing"
)

const miniSDK = "testdata/minisdk"

func TestParseSurfaceFindsTopLevelAndNestedGroups(t *testing.T) {
	surface, err := ParseSurface(miniSDK)
	if err != nil {
		t.Fatalf("ParseSurface: %v", err)
	}

	want := []string{
		"BlockStorage",
		"ElasticIps",
		"Events",
		"Firewalls",
		"Firewalls.Assignments",
		"Oddities",
		"Projects",
		"Projects.SSHKeys",
		"SSHKeys",
		"Servers",
		"Tags",
		"VirtualMachines",
		"VirtualNetworks",
	}
	if got := surface.Names(); !reflect.DeepEqual(got, want) {
		t.Errorf("group paths:\n got %v\nwant %v", got, want)
	}
}

// The field Projects.SSHKeys is typed LatitudeshProjectsSSHKeys, so anything that
// resolves methods by field name instead of by type silently reports an empty
// group. Guard that explicitly.
func TestParseSurfaceResolvesNestedGroupByTypeNotFieldName(t *testing.T) {
	surface, err := ParseSurface(miniSDK)
	if err != nil {
		t.Fatalf("ParseSurface: %v", err)
	}

	group, ok := surface.Groups["Projects.SSHKeys"]
	if !ok {
		t.Fatal("Projects.SSHKeys not found")
	}
	if group.TypeName != "LatitudeshProjectsSSHKeys" {
		t.Errorf("TypeName = %q, want LatitudeshProjectsSSHKeys", group.TypeName)
	}
	if want := []string{"PostProjectSSHKey"}; !reflect.DeepEqual(group.Methods, want) {
		t.Errorf("methods = %v, want %v", group.Methods, want)
	}

	// The top-level SSHKeys group is a different type and must not be confused
	// with the nested one despite sharing a field name.
	if top := surface.Groups["SSHKeys"]; top.TypeName != "SSHKeys" {
		t.Errorf("top-level SSHKeys TypeName = %q, want SSHKeys", top.TypeName)
	}
}

func TestParseSurfaceSkipsNonGroupFields(t *testing.T) {
	surface, err := ParseSurface(miniSDK)
	if err != nil {
		t.Fatalf("ParseSurface: %v", err)
	}

	// SDKVersion (a plain string), the unexported sdkConfiguration/hooks fields,
	// and the unexported back-reference rootSDK must never become groups.
	for _, name := range []string{"SDKVersion", "sdkConfiguration", "hooks", "rootSDK", "Latitudesh"} {
		if _, ok := surface.Groups[name]; ok {
			t.Errorf("%q was collected as a group", name)
		}
	}
}

func TestParseSurfaceSkipsUnexportedMethods(t *testing.T) {
	surface, err := ParseSurface(miniSDK)
	if err != nil {
		t.Fatalf("ParseSurface: %v", err)
	}

	for _, method := range surface.Groups["VirtualMachines"].Methods {
		if method == "unexportedShouldBeSkipped" {
			t.Error("unexported method was collected")
		}
	}
}

func TestParseSurfaceErrorsWithoutRootClient(t *testing.T) {
	// A directory with no Latitudesh struct means the SDK was restructured; that
	// has to be a loud error rather than an empty surface, which would make the
	// gate silently pass.
	if _, err := ParseSurface(filepath.Join("testdata")); err == nil {
		t.Fatal("expected an error when the root client type is absent")
	}
}
