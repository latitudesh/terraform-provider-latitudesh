package latitudesh

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func TestLksMatchesStatus(t *testing.T) {
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
			if got := lksMatchesStatus(tc.c, tc.want); got != tc.ok {
				t.Errorf("lksMatchesStatus(..., %q) = %v, want %v", tc.want, got, tc.ok)
			}
		})
	}
}

// The list item is built from the same envelope a live GET returns, so every
// field the API sends has to survive the mapping. Anything dropped here is a
// field a consumer has to go fetch again with a second data source.
func TestLksClusterItemValue(t *testing.T) {
	c := &components.LksClusterData{
		ID:         strPtr("lksc_8d12b878420d45"),
		Attributes: liveLksClusterAttributes(),
	}

	item, diags := lksClusterItemValue(c)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags.Errors())
	}

	strings := map[string]struct{ got, want string }{
		"ID":                   {item.ID.ValueString(), "lksc_8d12b878420d45"},
		"Project":              {item.Project.ValueString(), "proj_M3Beabq3l5Lnb"},
		"Name":                 {item.Name.ValueString(), "tf-manual-lks"},
		"Site":                 {item.Site.ValueString(), "LAX2"},
		"KubernetesVersion":    {item.KubernetesVersion.ValueString(), "1.36.1"},
		"Description":          {item.Description.ValueString(), "created by terraform"},
		"Status":               {item.Status.ValueString(), "ready"},
		"Message":              {item.Message.ValueString(), "the cluster is ready"},
		"Reason":               {item.Reason.ValueString(), ""},
		"ControlPlaneEndpoint": {item.ControlPlaneEndpoint.ValueString(), "https://lksc-8d12b878420d45.lks.lsh.io:6443"},
		"KubeconfigURL":        {item.KubeconfigURL.ValueString(), "/lks/clusters/lksc_8d12b878420d45/kubeconfig"},
		"PlatformVersion":      {item.PlatformVersion.ValueString(), "lks-v1.36.1-007"},
		"CreatedAt":            {item.CreatedAt.ValueString(), "2026-09-18T15:39:51.019044Z"},
		"UpdatedAt":            {item.UpdatedAt.ValueString(), "2026-09-18T15:46:15.110843Z"},
	}
	for field, v := range strings {
		if v.got != v.want {
			t.Errorf("%s = %q, want %q", field, v.got, v.want)
		}
	}

	if item.Network.IsNull() {
		t.Fatal("expected the network object to be mapped, got null")
	}
	var network LksNetworkModel
	if d := item.Network.As(context.Background(), &network, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatalf("decoding network: %v", d.Errors())
	}
	pods, d := setToStrings(context.Background(), network.PodCidrs)
	if d.HasError() {
		t.Fatalf("reading pod_cidrs: %v", d.Errors())
	}
	if len(pods) != 1 || pods[0] != "10.0.0.0/12" {
		t.Errorf("pod_cidrs = %v, want [10.0.0.0/12]", pods)
	}
}
