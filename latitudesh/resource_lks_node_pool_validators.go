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

// lksDefaultPoolHardTaintEffects are the effects that stop a pod from landing
// outright. PreferNoSchedule is deliberately absent: it is a soft preference,
// so the scheduler still places a pod when there is nowhere better.
var lksDefaultPoolHardTaintEffects = map[string]bool{
	"NoSchedule": true,
	"NoExecute":  true,
}

var _ validator.Set = lksDefaultPoolTaintsValidator{}

// lksDefaultPoolTaintsValidator keeps the cluster's own workloads schedulable.
//
// A cluster's system components — CNI, CoreDNS and friends — have nowhere to go
// but the default node pool. Tainting it NoSchedule or NoExecute means they
// never land, so the control plane never converges and the cluster sits in
// "provisioning" indefinitely. The API does not reject this: it accepts the
// taint, the apply succeeds, and then the readiness wait burns its whole
// timeout on a cluster that was never going to come up.
//
// That silent failure is exactly why this is a plan-time error rather than a
// documentation note. It applies only to latitudesh_lks's default_node_pool —
// an additional latitudesh_lks_node_pool is free to be tainted, which is the
// usual way to reserve a GPU pool.
type lksDefaultPoolTaintsValidator struct{}

func (v lksDefaultPoolTaintsValidator) Description(ctx context.Context) string {
	return "the default node pool must not carry a NoSchedule or NoExecute taint, or the cluster's system workloads cannot schedule"
}

func (v lksDefaultPoolTaintsValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v lksDefaultPoolTaintsValidator) ValidateSet(ctx context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	var taints []LksTaintModel
	resp.Diagnostics.Append(req.ConfigValue.ElementsAs(ctx, &taints, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	for _, t := range taints {
		if t.Effect.IsUnknown() || t.Effect.IsNull() {
			continue
		}
		effect := t.Effect.ValueString()
		if !lksDefaultPoolHardTaintEffects[effect] {
			continue
		}

		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Default node pool cannot carry a "+effect+" taint",
			fmt.Sprintf(
				"Taint %q has effect %q on `default_node_pool`. The cluster's own system workloads have nowhere to run but this pool, "+
					"so a %s taint keeps them from scheduling and the cluster never leaves `provisioning` — the apply does not fail, "+
					"it waits out its timeout on a cluster that was never going to converge.\n\n"+
					"Use `PreferNoSchedule` if you want to discourage scheduling here, or put the taint on a separate "+
					"`latitudesh_lks_node_pool`, which is the usual way to reserve a pool for specific workloads.",
				t.Key.ValueString(), effect, effect),
		)
	}
}
