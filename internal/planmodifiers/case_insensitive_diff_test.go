package planmodifiers

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestCaseInsensitiveDiff_PlanModifyString(t *testing.T) {
	tests := []struct {
		name        string
		configValue types.String
		stateValue  types.String
		expected    types.String
	}{
		{
			name:        "new resource - no state, keep as-is",
			configValue: types.StringValue("sao2"),
			stateValue:  types.StringNull(),
			expected:    types.StringValue("sao2"), // keeps user input
		},
		{
			name:        "new resource - already uppercase",
			configValue: types.StringValue("SAO2"),
			stateValue:  types.StringNull(),
			expected:    types.StringValue("SAO2"), // keeps user input
		},
		{
			name:        "existing resource - same case",
			configValue: types.StringValue("SAO2"),
			stateValue:  types.StringValue("SAO2"),
			expected:    types.StringValue("SAO2"),
		},
		{
			name:        "existing resource - config lowercase, state uppercase",
			configValue: types.StringValue("sao2"),
			stateValue:  types.StringValue("SAO2"),
			expected:    types.StringValue("SAO2"), // should use state value (suppress diff)
		},
		{
			name:        "existing resource - config mixed, state uppercase",
			configValue: types.StringValue("Sao2"),
			stateValue:  types.StringValue("SAO2"),
			expected:    types.StringValue("SAO2"), // should use state value (suppress diff)
		},
		{
			name:        "existing resource - config uppercase, state lowercase",
			configValue: types.StringValue("SAO2"),
			stateValue:  types.StringValue("sao2"),
			expected:    types.StringValue("sao2"), // should use state value (suppress diff)
		},
		{
			name:        "existing resource - actually different value",
			configValue: types.StringValue("ash"),
			stateValue:  types.StringValue("SAO2"),
			expected:    types.StringValue("ash"), // values differ, use config value
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modifier := CaseInsensitiveDiff{}
			req := planmodifier.StringRequest{
				ConfigValue: tt.configValue,
				StateValue:  tt.stateValue,
			}
			resp := &planmodifier.StringResponse{
				PlanValue: req.ConfigValue,
			}

			modifier.PlanModifyString(context.Background(), req, resp)

			if !resp.PlanValue.Equal(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, resp.PlanValue)
			}
		})
	}
}

func TestCaseInsensitiveDiff_Description(t *testing.T) {
	modifier := CaseInsensitiveDiff{}
	desc := modifier.Description(context.Background())
	if desc == "" {
		t.Error("Description should not be empty")
	}
}

func TestCaseInsensitiveDiff_MarkdownDescription(t *testing.T) {
	modifier := CaseInsensitiveDiff{}
	desc := modifier.MarkdownDescription(context.Background())
	if desc == "" {
		t.Error("MarkdownDescription should not be empty")
	}
}
