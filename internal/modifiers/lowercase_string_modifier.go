package modifiers

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type LowercaseStringModifier struct{}

var _ planmodifier.String = &LowercaseStringModifier{}

func (m *LowercaseStringModifier) Description(_ context.Context) string {
	return "Normalizes string values to lowercase in state, but keeps the original config casing when equivalent."
}

func (m *LowercaseStringModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m *LowercaseStringModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.ConfigValue.IsUnknown() || req.ConfigValue.IsNull() {
		resp.PlanValue = req.ConfigValue
		return
	}

	configRaw := req.ConfigValue.ValueString()
	stateRaw := getStringIfKnown(req.StateValue)

	switch {
	case req.StateValue.IsNull() || req.StateValue.IsUnknown():
		// New resource
		resp.PlanValue = req.ConfigValue

	case strings.EqualFold(configRaw, stateRaw):
		// Same value
		resp.PlanValue = req.StateValue

	default:
		// Different value
		resp.PlanValue = req.ConfigValue
	}
}

func getStringIfKnown(v types.String) string {
	if v.IsNull() || v.IsUnknown() {
		return ""
	}
	return v.ValueString()
}
