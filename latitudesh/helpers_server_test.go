package latitudesh

import (
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func strPtr(s string) *string { return &s }

func TestBuildInterfacesList_StableOrderRegardlessOfInput(t *testing.T) {
	a := components.Interfaces{Name: strPtr("eth0"), MacAddress: strPtr("90:5a:08:16:db:04")}
	b := components.Interfaces{Name: strPtr("eth1"), MacAddress: strPtr("90:5a:08:2d:d6:8b")}

	forward, diags := buildInterfacesList([]components.Interfaces{a, b})
	if diags.HasError() {
		t.Fatalf("forward build errored: %v", diags)
	}
	reversed, diags := buildInterfacesList([]components.Interfaces{b, a})
	if diags.HasError() {
		t.Fatalf("reversed build errored: %v", diags)
	}

	if !forward.Equal(reversed) {
		t.Fatalf("interface ordering is not stable across input orders:\n forward=%s\nreversed=%s", forward, reversed)
	}
}

func TestBuildInterfacesList_Empty(t *testing.T) {
	list, diags := buildInterfacesList(nil)
	if diags.HasError() {
		t.Fatalf("nil build errored: %v", diags)
	}
	if list.IsNull() || list.IsUnknown() {
		t.Fatalf("expected a known empty list, got null/unknown")
	}
	if len(list.Elements()) != 0 {
		t.Fatalf("expected 0 elements, got %d", len(list.Elements()))
	}
}
