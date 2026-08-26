package latitudesh

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func strPtr(s string) *string { return &s }

// buildInterfacesListUnsorted reproduces the pre-fix behavior: build the list in
// raw API order with no sort. Used only to prove the bug existed.
func buildInterfacesListUnsorted(ifaces []components.Interfaces) types.List {
	objs := make([]attr.Value, 0, len(ifaces))
	for _, iface := range ifaces {
		obj, _ := types.ObjectValue(ifaceType.AttrTypes, map[string]attr.Value{
			"name":        optionalString(iface.Name),
			"mac_address": optionalString(iface.MacAddress),
			"description": optionalString(iface.Description),
		})
		objs = append(objs, obj)
	}
	list, _ := listIfaces(objs)
	return list
}

// TestBuildInterfacesList_BeforeAfter shows the same interfaces returned in two
// different orders. The old unsorted build produces different lists (which is
// what triggers "inconsistent result after apply"); the fixed build does not.
func TestBuildInterfacesList_BeforeAfter(t *testing.T) {
	a := components.Interfaces{Name: strPtr("eth0"), MacAddress: strPtr("90:5a:08:16:db:04")}
	b := components.Interfaces{Name: strPtr("eth1"), MacAddress: strPtr("90:5a:08:2d:d6:8b")}

	forwardIn := []components.Interfaces{a, b}
	reversedIn := []components.Interfaces{b, a}

	// Before: raw API order — the two reads disagree, so interfaces[1].mac_address
	// flips between applies.
	if buildInterfacesListUnsorted(forwardIn).Equal(buildInterfacesListUnsorted(reversedIn)) {
		t.Fatal("before: expected unsorted builds to differ across input orders, but they matched")
	}

	// After: sorted — the two reads agree, so no inconsistency.
	fwd, _ := buildInterfacesList(forwardIn)
	rev, _ := buildInterfacesList(reversedIn)
	if !fwd.Equal(rev) {
		t.Fatalf("after: expected sorted builds to match, but they differ:\n forward=%s\nreversed=%s", fwd, rev)
	}
}

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
