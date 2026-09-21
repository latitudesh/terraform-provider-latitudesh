package latitudesh

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func lksTaintSet(t *testing.T, taints ...LksTaintModel) types.Set {
	t.Helper()

	elems := make([]attr.Value, 0, len(taints))
	for _, taint := range taints {
		obj, diags := types.ObjectValueFrom(context.Background(), lksTaintObjectType.AttrTypes, taint)
		if diags.HasError() {
			t.Fatalf("building taint object: %v", diags.Errors())
		}
		elems = append(elems, obj)
	}

	set, diags := types.SetValue(lksTaintObjectType, elems)
	if diags.HasError() {
		t.Fatalf("building taint set: %v", diags.Errors())
	}
	return set
}

func lksTaint(key, value, effect string) LksTaintModel {
	return LksTaintModel{
		Key:    types.StringValue(key),
		Value:  types.StringValue(value),
		Effect: types.StringValue(effect),
	}
}

// A hard taint on the cluster's own pool is the worst kind of mistake: the API
// accepts it, the apply reports success on the pool, and then the cluster sits
// in "provisioning" until the timeout expires, because nothing the control
// plane needs can schedule. Catching it at plan time is the only place it is
// cheap.
func TestLksDefaultPoolTaintsValidator(t *testing.T) {
	cases := []struct {
		name      string
		taints    []LksTaintModel
		wantError bool
	}{
		{"no taints", nil, false},
		// Soft preference: the scheduler still places system pods when there
		// is nowhere else, so the cluster converges.
		{"PreferNoSchedule is allowed", []LksTaintModel{lksTaint("dedicated", "gpu", "PreferNoSchedule")}, false},
		{"NoSchedule is rejected", []LksTaintModel{lksTaint("dedicated", "gpu", "NoSchedule")}, true},
		{"NoExecute is rejected", []LksTaintModel{lksTaint("dedicated", "gpu", "NoExecute")}, true},
		// One bad entry among good ones still has to be caught.
		{"rejected among allowed", []LksTaintModel{
			lksTaint("a", "1", "PreferNoSchedule"),
			lksTaint("b", "2", "NoExecute"),
		}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &validator.SetResponse{}
			lksDefaultPoolTaintsValidator{}.ValidateSet(
				context.Background(),
				validator.SetRequest{
					Path:        path.Root("default_node_pool").AtName("taints"),
					ConfigValue: lksTaintSet(t, tc.taints...),
				},
				resp,
			)

			if got := resp.Diagnostics.HasError(); got != tc.wantError {
				t.Errorf("HasError = %v, want %v (%v)", got, tc.wantError, resp.Diagnostics.Errors())
			}
		})
	}
}

// The standalone pool is exactly where a hard taint belongs — reserving a pool
// for GPU or batch workloads is the normal reason to use one — so the
// restriction must not leak onto it.
func TestLksNodePoolAllowsHardTaints(t *testing.T) {
	resp := &validator.SetResponse{}
	lksUniqueTaintsValidator{}.ValidateSet(
		context.Background(),
		validator.SetRequest{
			Path:        path.Root("taints"),
			ConfigValue: lksTaintSet(t, lksTaint("dedicated", "gpu", "NoSchedule")),
		},
		resp,
	)

	if resp.Diagnostics.HasError() {
		t.Errorf("a NoSchedule taint on a standalone pool must be allowed, got %v", resp.Diagnostics.Errors())
	}
}
