package modifiers

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestLowercaseStringModifier_PlanModifyString(t *testing.T) {
	tests := []struct {
		name          string
		configValue   types.String
		stateValue    types.String
		expectedValue types.String
		expectedError bool
	}{
		{
			name:          "uppercase color should be normalized to lowercase",
			configValue:   types.StringValue("#FF0000"),
			stateValue:    types.StringValue("#5319e7"),
			expectedValue: types.StringValue("#ff0000"),
		},
		{
			name:          "mixed case color should be normalized to lowercase",
			configValue:   types.StringValue("#AbCdEf"),
			stateValue:    types.StringValue("#5319e7"),
			expectedValue: types.StringValue("#abcdef"),
		},
		{
			name:          "mixed case to lowercase state - should return lowercase",
			configValue:   types.StringValue("#5319E7"),
			stateValue:    types.StringValue("#5319e7"),
			expectedValue: types.StringValue("#5319e7"),
		},
		{
			name:          "uppercase to lowercase state - should return lowercase",
			configValue:   types.StringValue("#FF0000"),
			stateValue:    types.StringValue("#ff0000"),
			expectedValue: types.StringValue("#ff0000"),
		},
		{
			name:          "null config value should return unchanged",
			configValue:   types.StringNull(),
			stateValue:    types.StringValue("#5319e7"),
			expectedValue: types.StringNull(),
		},
		{
			name:          "unknown config value should return unchanged",
			configValue:   types.StringUnknown(),
			stateValue:    types.StringValue("#5319e7"),
			expectedValue: types.StringUnknown(),
		},
		{
			name:          "exact scenario from user issue",
			configValue:   types.StringValue("#FF0000"),
			stateValue:    types.StringValue("#5319e7"),
			expectedValue: types.StringValue("#ff0000"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modifier := &LowercaseStringModifier{}

			req := planmodifier.StringRequest{
				ConfigValue: tt.configValue,
				StateValue:  tt.stateValue,
			}

			resp := &planmodifier.StringResponse{}

			modifier.PlanModifyString(context.Background(), req, resp)

			if resp.Diagnostics.HasError() != tt.expectedError {
				t.Errorf("expected error %v, got %v", tt.expectedError, resp.Diagnostics.HasError())
			}

			if !resp.PlanValue.Equal(tt.expectedValue) {
				t.Errorf("expected %v, got %v", tt.expectedValue, resp.PlanValue)
			}
		})
	}
}
