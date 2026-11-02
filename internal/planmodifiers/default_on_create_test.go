package planmodifiers

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestDefaultOnCreate_PlanModifyString(t *testing.T) {
	tests := []struct {
		name        string
		configValue types.String
		stateValue  types.String
		planValue   types.String
		expected    types.String
	}{
		{
			name:        "create with attribute omitted - default applied at plan time",
			configValue: types.StringNull(),
			stateValue:  types.StringNull(),
			planValue:   types.StringUnknown(),
			expected:    types.StringValue("monthly"),
		},
		{
			name:        "create with explicit value - keep config value",
			configValue: types.StringValue("hourly"),
			stateValue:  types.StringNull(),
			planValue:   types.StringValue("hourly"),
			expected:    types.StringValue("hourly"),
		},
		{
			name:        "update with attribute omitted - preserve state value",
			configValue: types.StringNull(),
			stateValue:  types.StringValue("yearly"),
			planValue:   types.StringUnknown(),
			expected:    types.StringValue("yearly"),
		},
		{
			name:        "update with explicit value - keep config value",
			configValue: types.StringValue("monthly"),
			stateValue:  types.StringValue("hourly"),
			planValue:   types.StringValue("monthly"),
			expected:    types.StringValue("monthly"),
		},
		{
			name:        "config from unknown reference - leave plan untouched",
			configValue: types.StringUnknown(),
			stateValue:  types.StringValue("hourly"),
			planValue:   types.StringUnknown(),
			expected:    types.StringUnknown(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := DefaultOnCreate{Value: "monthly"}

			req := planmodifier.StringRequest{
				ConfigValue: tt.configValue,
				StateValue:  tt.stateValue,
				PlanValue:   tt.planValue,
			}
			resp := &planmodifier.StringResponse{
				PlanValue: tt.planValue,
			}

			m.PlanModifyString(context.Background(), req, resp)

			if !resp.PlanValue.Equal(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, resp.PlanValue)
			}
		})
	}
}

func TestDefaultOnCreateInt64_PlanModifyInt64(t *testing.T) {
	tests := []struct {
		name        string
		configValue types.Int64
		stateValue  types.Int64
		planValue   types.Int64
		expected    types.Int64
	}{
		{
			name:        "create with attribute omitted - default applied at plan time",
			configValue: types.Int64Null(),
			stateValue:  types.Int64Null(),
			planValue:   types.Int64Unknown(),
			expected:    types.Int64Value(1500),
		},
		{
			name:        "create with explicit value - keep config value",
			configValue: types.Int64Value(2000),
			stateValue:  types.Int64Null(),
			planValue:   types.Int64Value(2000),
			expected:    types.Int64Value(2000),
		},
		{
			name:        "update with attribute omitted - preserve state value",
			configValue: types.Int64Null(),
			stateValue:  types.Int64Value(3000),
			planValue:   types.Int64Unknown(),
			expected:    types.Int64Value(3000),
		},
		{
			name:        "update with explicit value - keep config value",
			configValue: types.Int64Value(1500),
			stateValue:  types.Int64Value(3000),
			planValue:   types.Int64Value(1500),
			expected:    types.Int64Value(1500),
		},
		{
			name:        "config from unknown reference - leave plan untouched",
			configValue: types.Int64Unknown(),
			stateValue:  types.Int64Value(3000),
			planValue:   types.Int64Unknown(),
			expected:    types.Int64Unknown(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := DefaultOnCreateInt64{Value: 1500}

			req := planmodifier.Int64Request{
				ConfigValue: tt.configValue,
				StateValue:  tt.stateValue,
				PlanValue:   tt.planValue,
			}
			resp := &planmodifier.Int64Response{
				PlanValue: tt.planValue,
			}

			m.PlanModifyInt64(context.Background(), req, resp)

			if !resp.PlanValue.Equal(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, resp.PlanValue)
			}
		})
	}
}
