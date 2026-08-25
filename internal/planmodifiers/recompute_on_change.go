package planmodifiers

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// RecomputeOnChange keeps a computed attribute's prior state value on update —
// like UseStateForUnknown — UNLESS the given source attribute is changing, in
// which case it leaves the planned value unknown so the provider recomputes it
// from the API after apply.
//
// It exists for computed attributes derived from another attribute (e.g. a slug
// derived from name). Plain Computed churns to "(known after apply)" on every
// unrelated update; UseStateForUnknown instead freezes a stale value when the
// source does change, tripping "Provider produced inconsistent result after
// apply". This modifier gives the correct middle ground.
type RecomputeOnChange struct {
	SourceAttribute path.Path
}

var _ planmodifier.String = (*RecomputeOnChange)(nil)

func (m RecomputeOnChange) Description(_ context.Context) string {
	return fmt.Sprintf("Recomputes when %s changes; otherwise preserves the state value.", m.SourceAttribute)
}

func (m RecomputeOnChange) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m RecomputeOnChange) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Create: no prior state, leave unknown so the API value is used.
	if req.StateValue.IsNull() {
		return
	}

	var planSource, stateSource types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, m.SourceAttribute, &planSource)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, m.SourceAttribute, &stateSource)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Source unchanged: keep the prior computed value (no churn, empty plan).
	// Source changed: leave the framework's proposed unknown so it recomputes.
	if planSource.Equal(stateSource) {
		resp.PlanValue = req.StateValue
	}
}
