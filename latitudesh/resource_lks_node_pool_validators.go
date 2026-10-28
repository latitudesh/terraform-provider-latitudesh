package latitudesh

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// The two validators below turn 422s the platform would raise at apply time
// into plan-time errors. Both rules are documented on the SDK models
// (CreateLksNodePoolAttributes.Labels / .Taints), so this is enforcing a
// contract rather than guessing at one.

var (
	_ validator.Map = lksReservedLabelKeysValidator{}
	_ validator.Set = lksUniqueTaintsValidator{}
)

// lksReservedLabelKeysValidator rejects label keys under a reserved prefix.
type lksReservedLabelKeysValidator struct{}

func (v lksReservedLabelKeysValidator) Description(ctx context.Context) string {
	return fmt.Sprintf("label keys must not use the reserved prefixes %s (or their subdomains)",
		strings.Join(lksNodePoolReservedLabelPrefixes, ", "))
}

func (v lksReservedLabelKeysValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v lksReservedLabelKeysValidator) ValidateMap(ctx context.Context, req validator.MapRequest, resp *validator.MapResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	for key := range req.ConfigValue.Elements() {
		if lksReservedLabelKey(key) {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Reserved label key",
				fmt.Sprintf("Label key %q uses a prefix reserved by the platform (%s, and their subdomains). The API rejects it with 422.",
					key, strings.Join(lksNodePoolReservedLabelPrefixes, ", ")),
			)
		}
	}
}

// lksUniqueTaintsValidator enforces the API's uniqueness rule on (key, effect)
// and the same reserved-prefix rule on the key. A Set already rejects fully
// identical elements, but two taints sharing a (key, effect) with different
// values are distinct elements and would only fail server-side.
type lksUniqueTaintsValidator struct{}

func (v lksUniqueTaintsValidator) Description(ctx context.Context) string {
	return "each (key, effect) pair must be unique, and keys must not use a reserved prefix"
}

func (v lksUniqueTaintsValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v lksUniqueTaintsValidator) ValidateSet(ctx context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	var taints []LksTaintModel
	resp.Diagnostics.Append(req.ConfigValue.ElementsAs(ctx, &taints, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	seen := make(map[string]struct{}, len(taints))
	for _, t := range taints {
		if t.Key.IsUnknown() || t.Effect.IsUnknown() {
			continue
		}
		key := t.Key.ValueString()

		if lksReservedLabelKey(key) {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Reserved taint key",
				fmt.Sprintf("Taint key %q uses a prefix reserved by the platform (%s, and their subdomains). The API rejects it with 422.",
					key, strings.Join(lksNodePoolReservedLabelPrefixes, ", ")),
			)
			continue
		}

		pair := key + "\x00" + t.Effect.ValueString()
		if _, dup := seen[pair]; dup {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Duplicate taint",
				fmt.Sprintf("More than one taint has key %q with effect %q. The API requires each (key, effect) pair to be unique.",
					key, t.Effect.ValueString()),
			)
			continue
		}
		seen[pair] = struct{}{}
	}
}
