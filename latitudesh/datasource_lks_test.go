package latitudesh

import (
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func TestLkMatchesStatus(t *testing.T) {
	ready := "ready"
	cases := []struct {
		name string
		c    components.LksClusterData
		want string
		ok   bool
	}{
		{"empty filter matches nil status", components.LksClusterData{}, "", true},
		{"empty filter matches any status", components.LksClusterData{Attributes: &components.LksClusterDataAttributes{Status: &ready}}, "", true},
		{"nil status never matches a filter", components.LksClusterData{}, "ready", false},
		{"case-insensitive match", components.LksClusterData{Attributes: &components.LksClusterDataAttributes{Status: &ready}}, "READY", true},
		{"mismatch", components.LksClusterData{Attributes: &components.LksClusterDataAttributes{Status: &ready}}, "provisioning", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lkMatchesStatus(tc.c, tc.want); got != tc.ok {
				t.Errorf("lkMatchesStatus(..., %q) = %v, want %v", tc.want, got, tc.ok)
			}
		})
	}
}

func TestLkItemValue(t *testing.T) {
	id := "lks_1"
	name := "prod"
	site := "ASH"

	c := &components.LksClusterData{
		ID: &id,
		Attributes: &components.LksClusterDataAttributes{
			Name: &name,
			Site: &site,
		},
	}

	item := lkItemValue(c)
	if item.ID.ValueString() != id {
		t.Errorf("ID = %q, want %q", item.ID.ValueString(), id)
	}
	if item.Name.ValueString() != name {
		t.Errorf("Name = %q, want %q", item.Name.ValueString(), name)
	}
	if item.Site.ValueString() != site {
		t.Errorf("Site = %q, want %q", item.Site.ValueString(), site)
	}
}
