package planmodifiers

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// DefaultOnCreate sets a static default for an optional+computed string
// attribute only when the resource is being created, so the default is
// visible in the plan instead of "(known after apply)". On updates with the
// attribute omitted, the value already in state is preserved — unlike a
// schema Default, which would also override the state value on update.
type DefaultOnCreate struct {
	Value string
}

var _ planmodifier.String = (*DefaultOnCreate)(nil)

func (m DefaultOnCreate) Description(_ context.Context) string {
	return fmt.Sprintf("Defaults to %q on create; preserves the state value on update.", m.Value)
}

func (m DefaultOnCreate) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m DefaultOnCreate) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// A value is set in config (or comes from an unknown reference) — leave it.
	if !req.ConfigValue.IsNull() {
		return
	}

	// Update with the attribute omitted — keep whatever is in state.
	if !req.StateValue.IsNull() && !req.StateValue.IsUnknown() {
		resp.PlanValue = req.StateValue
		return
	}

	// Create with the attribute omitted — apply the default at plan time.
	resp.PlanValue = types.StringValue(m.Value)
}
