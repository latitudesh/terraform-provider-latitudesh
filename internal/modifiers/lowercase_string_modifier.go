package modifiers

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type LowercaseStringModifier struct{}

var _ planmodifier.String = LowercaseStringModifier{}

func (m LowercaseStringModifier) Description(_ context.Context) string {
	return "Normalizes the string value to lowercase during the plan phase."
}

func (m LowercaseStringModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m LowercaseStringModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.PlanValue.IsUnknown() || req.PlanValue.IsNull() {
		return
	}

	val := req.PlanValue.ValueString()
	resp.PlanValue = types.StringValue(strings.ToLower(val))
}
