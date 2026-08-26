package planmodifiers

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestRecomputeOnChange_PlanModifyString(t *testing.T) {
	sch := schema.Schema{
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{Required: true},
			"slug": schema.StringAttribute{Computed: true},
		},
	}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"name": tftypes.String,
		"slug": tftypes.String,
	}}

	build := func(name, slug string) tftypes.Value {
		return tftypes.NewValue(objType, map[string]tftypes.Value{
			"name": tftypes.NewValue(tftypes.String, name),
			"slug": tftypes.NewValue(tftypes.String, slug),
		})
	}

	tests := []struct {
		name       string
		stateValue types.String // prior slug value in state
		planName   string
		stateName  string
		expected   types.String
	}{
		{
			name:       "create - no state, leave unknown",
			stateValue: types.StringNull(),
			expected:   types.StringUnknown(),
		},
		{
			name:       "update, name unchanged - preserve slug",
			stateValue: types.StringValue("env-prod"),
			planName:   "env:prod",
			stateName:  "env:prod",
			expected:   types.StringValue("env-prod"),
		},
		{
			name:       "update, name changed - recompute (stay unknown)",
			stateValue: types.StringValue("env-prod"),
			planName:   "env_prod",
			stateName:  "env:prod",
			expected:   types.StringUnknown(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := RecomputeOnChange{SourceAttribute: path.Root("name")}

			req := planmodifier.StringRequest{
				StateValue: tt.stateValue,
				PlanValue:  types.StringUnknown(),
				Plan:       tfsdk.Plan{Raw: build(tt.planName, ""), Schema: sch},
				State:      tfsdk.State{Raw: build(tt.stateName, tt.stateValue.ValueString()), Schema: sch},
			}
			resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}

			m.PlanModifyString(context.Background(), req, resp)

			if !resp.PlanValue.Equal(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, resp.PlanValue)
			}
		})
	}
}
