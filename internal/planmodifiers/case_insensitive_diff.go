package planmodifiers

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// CaseInsensitiveDiff is a plan modifier that suppresses diffs when the only
// difference between config and state is the case of the string value.
// This allows users to specify values in any case while the API returns them
// in a normalized case (e.g., uppercase).
type CaseInsensitiveDiff struct{}

var _ planmodifier.String = (*CaseInsensitiveDiff)(nil)

func (m CaseInsensitiveDiff) Description(_ context.Context) string {
	return "Suppresses diffs when values differ only in case."
}

func (m CaseInsensitiveDiff) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m CaseInsensitiveDiff) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// If config is unknown or null, nothing to do
	if req.ConfigValue.IsUnknown() || req.ConfigValue.IsNull() {
		return
	}

	configValue := req.ConfigValue.ValueString()

	// If state is null (new resource), use config value as-is
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}

	stateValue := req.StateValue.ValueString()

	// If values are equal case-insensitively, use the state value
	// This suppresses the diff
	if strings.EqualFold(configValue, stateValue) {
		resp.PlanValue = req.StateValue
	}
}
