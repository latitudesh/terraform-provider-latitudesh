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

// A hard taint is how a pool is reserved for GPU or batch workloads, so the
// taint validator must not reject one.
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
