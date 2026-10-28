package latitudesh

import (
	"context"
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// The list item is built from the same envelope a live GET returns. Labels and
// taints are the point of listing pools at all — they are what tells one pool
// apart from another — and they were among the nine fields this data source
// used to drop on the floor.
func TestLksNodePoolItemValue(t *testing.T) {
	p := &components.LksNodePoolData{
		ID:         strPtr("lksnp_7341ea2c4ab843"),
		Attributes: liveLksNodePoolAttributes(),
	}

	item, diags := lksNodePoolItemValue(context.Background(), p)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags.Errors())
	}

	strings := map[string]struct{ got, want string }{
		"ID":                {item.ID.ValueString(), "lksnp_7341ea2c4ab843"},
		"Name":              {item.Name.ValueString(), "np-b55eea9b6036"},
		"Plan":              {item.Plan.ValueString(), "f4-metal-small"},
		"Type":              {item.Type.ValueString(), "bare_metal"},
		"Mode":              {item.Mode.ValueString(), "on_demand"},
		"Description":       {item.Description.ValueString(), ""},
		"KubernetesVersion": {item.KubernetesVersion.ValueString(), "1.36.1"},
		"PlatformVersion":   {item.PlatformVersion.ValueString(), "lks-v1.36.1-007"},
		"Status":            {item.Status.ValueString(), "ready"},
		"Message":           {item.Message.ValueString(), "the node pool is ready"},
		"Reason":            {item.Reason.ValueString(), ""},
		"CreatedAt":         {item.CreatedAt.ValueString(), "2026-09-18T15:39:52.045214Z"},
		"UpdatedAt":         {item.UpdatedAt.ValueString(), "2026-09-18T15:42:14.740745Z"},
	}
	for field, v := range strings {
		if v.got != v.want {
			t.Errorf("%s = %q, want %q", field, v.got, v.want)
		}
	}

	if item.NodeCount.ValueInt64() != 1 {
		t.Errorf("NodeCount = %d, want 1", item.NodeCount.ValueInt64())
	}
	if !item.ReadyNodes.IsNull() {
		t.Errorf("ReadyNodes = %s, want null — the live API does not report it", item.ReadyNodes)
	}
	if got := len(item.Labels.Elements()); got != 1 {
		t.Errorf("labels length = %d, want 1", got)
	}
	if got := len(item.Taints.Elements()); got != 1 {
		t.Errorf("taints length = %d, want 1", got)
	}
}
