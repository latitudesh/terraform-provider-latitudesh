package latitudesh

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"

	providerpkg "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var (
	_ resource.Resource                = &LksNodePoolResource{}
	_ resource.ResourceWithConfigure   = &LksNodePoolResource{}
	_ resource.ResourceWithModifyPlan  = &LksNodePoolResource{}
	_ resource.ResourceWithImportState = &LksNodePoolResource{}
)

// lksNodePoolReadyPollInterval is a variable so tests can shorten it, matching
// lksReadyPollInterval.
var (
	lksNodePoolReadyPollInterval   = 15 * time.Second
	lksNodePoolDeletePollInterval  = 10 * time.Second
	lksNodePoolMaxConsecutiveError = 5
)

// lksNodePoolReservedLabelPrefixes are the label/taint key prefixes the API
// rejects with 422. Validating client-side turns a failed apply into a failed
// plan, which is the difference between losing a minute and losing the run.
var lksNodePoolReservedLabelPrefixes = []string{
	"kubernetes.io",
	"k8s.io",
	"cluster.x-k8s.io",
	"lks.latitude.sh",
}

func NewLksNodePoolResource() resource.Resource {
	return &LksNodePoolResource{}
}

type LksNodePoolResource struct {
	client *latitudeshgosdk.Latitudesh
}

type LksNodePoolResourceModel struct {
	ID                types.String   `tfsdk:"id"`
	ClusterID         types.String   `tfsdk:"cluster_id"`
	Plan              types.String   `tfsdk:"plan"`
	NodeCount         types.Int64    `tfsdk:"node_count"`
	Type              types.String   `tfsdk:"type"`
	KubernetesVersion types.String   `tfsdk:"kubernetes_version"`
	MaxPodsPerNode    types.Int64    `tfsdk:"max_pods_per_node"`
	Name              types.String   `tfsdk:"name"`
	Description       types.String   `tfsdk:"description"`
	Labels            types.Map      `tfsdk:"labels"`
	Taints            types.Set      `tfsdk:"taints"`
	Mode              types.String   `tfsdk:"mode"`
	Status            types.String   `tfsdk:"status"`
	Message           types.String   `tfsdk:"message"`
	Reason            types.String   `tfsdk:"reason"`
	ReadyNodes        types.Int64    `tfsdk:"ready_nodes"`
	PlatformVersion   types.String   `tfsdk:"platform_version"`
	CreatedAt         types.String   `tfsdk:"created_at"`
	UpdatedAt         types.String   `tfsdk:"updated_at"`
	Timeouts          timeouts.Value `tfsdk:"timeouts"`
}

type LksTaintModel struct {
	Key    types.String `tfsdk:"key"`
	Value  types.String `tfsdk:"value"`
	Effect types.String `tfsdk:"effect"`
}

var lksTaintObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"key":    types.StringType,
		"value":  types.StringType,
		"effect": types.StringType,
	},
}

// lksNodePoolFields holds the values read back from the API.
type lksNodePoolFields struct {
	Plan              types.String
	NodeCount         types.Int64
	Type              types.String
	KubernetesVersion types.String
	MaxPodsPerNode    types.Int64
	Name              types.String
	Description       types.String
	Labels            types.Map
	Taints            types.Set
	Mode              types.String
	Status            types.String
	Message           types.String
	Reason            types.String
	ReadyNodes        types.Int64
	PlatformVersion   types.String
	CreatedAt         types.String
	UpdatedAt         types.String
}

func mapLksNodePoolAttributes(ctx context.Context, attrs *components.LksNodePoolDataAttributes) (lksNodePoolFields, diag.Diagnostics) {
	var diags diag.Diagnostics

	out := lksNodePoolFields{
		Plan:              types.StringNull(),
		NodeCount:         types.Int64Null(),
		Type:              types.StringNull(),
		KubernetesVersion: types.StringNull(),
		MaxPodsPerNode:    types.Int64Null(),
		Name:              types.StringNull(),
		Description:       types.StringNull(),
		Labels:            types.MapNull(types.StringType),
		Taints:            types.SetNull(lksTaintObjectType),
		Mode:              types.StringNull(),
		Status:            types.StringNull(),
		Message:           types.StringNull(),
		Reason:            types.StringNull(),
		ReadyNodes:        types.Int64Null(),
		PlatformVersion:   types.StringNull(),
		CreatedAt:         types.StringNull(),
		UpdatedAt:         types.StringNull(),
	}
	if attrs == nil {
		return out, diags
	}

	out.Plan = types.StringPointerValue(attrs.Plan)
	out.NodeCount = types.Int64PointerValue(attrs.Count)
	out.Type = types.StringPointerValue(attrs.Type)
	out.KubernetesVersion = types.StringPointerValue(attrs.KubernetesVersion)
	out.MaxPodsPerNode = types.Int64PointerValue(attrs.MaxPodsPerNode)
	out.Name = types.StringPointerValue(attrs.Name)
	out.Description = types.StringPointerValue(attrs.Description)
	out.Mode = types.StringPointerValue(attrs.Mode)
	out.Status = types.StringPointerValue(attrs.Status)
	out.Message = types.StringPointerValue(attrs.Message)
	out.Reason = types.StringPointerValue(attrs.Reason)
	out.ReadyNodes = types.Int64PointerValue(attrs.ReadyNodes)
	out.PlatformVersion = types.StringPointerValue(attrs.PlatformVersion)
	out.CreatedAt = types.StringPointerValue(attrs.CreatedAt)
	out.UpdatedAt = types.StringPointerValue(attrs.UpdatedAt)

	// An absent labels map and an empty one are the same thing to the API ("no
	// labels"), and the doc comment says the map is stored exactly as sent —
	// so an empty map is echoed as empty rather than dropped. Keeping null for
	// both sides means a config that omits `labels` never sees a diff.
	if len(attrs.Labels) > 0 {
		labels, d := types.MapValueFrom(ctx, types.StringType, attrs.Labels)
		diags.Append(d...)
		out.Labels = labels
	}

	if len(attrs.Taints) > 0 {
		models := make([]LksTaintModel, 0, len(attrs.Taints))
		for _, t := range attrs.Taints {
			models = append(models, LksTaintModel{
				Key:    types.StringValue(t.Key),
				Value:  types.StringPointerValue(t.Value),
				Effect: types.StringValue(string(t.Effect)),
			})
		}
		taints, d := types.SetValueFrom(ctx, lksTaintObjectType, models)
		diags.Append(d...)
		out.Taints = taints
	}

	return out, diags
}

// lksTaintsFromSet converts the configured taints into the SDK shape. A null or
// empty set sends nil, which omits the field.
func lksTaintsFromSet(ctx context.Context, set types.Set) ([]components.LksNodePoolTaint, diag.Diagnostics) {
	var diags diag.Diagnostics

	if set.IsNull() || set.IsUnknown() {
		return nil, diags
	}

	var models []LksTaintModel
	diags.Append(set.ElementsAs(ctx, &models, false)...)
	if diags.HasError() {
		return nil, diags
	}

	taints := make([]components.LksNodePoolTaint, 0, len(models))
	for _, m := range models {
		taints = append(taints, components.LksNodePoolTaint{
			Key:    m.Key.ValueString(),
			Value:  m.Value.ValueStringPointer(),
			Effect: components.Effect(m.Effect.ValueString()),
		})
	}
	return taints, diags
}

func lksLabelsFromMap(ctx context.Context, m types.Map) (map[string]string, diag.Diagnostics) {
	var diags diag.Diagnostics

	if m.IsNull() || m.IsUnknown() {
		return nil, diags
	}

	labels := make(map[string]string, len(m.Elements()))
	diags.Append(m.ElementsAs(ctx, &labels, false)...)
	return labels, diags
}

// lksReservedLabelKey reports whether a Kubernetes label/taint key sits under
// one of the prefixes the platform reserves. The key's optional prefix is
// everything before the first "/", and a reserved prefix covers its subdomains.
func lksReservedLabelKey(key string) bool {
	prefix, _, found := strings.Cut(key, "/")
	if !found {
		return false
	}
	for _, reserved := range lksNodePoolReservedLabelPrefixes {
		if prefix == reserved || strings.HasSuffix(prefix, "."+reserved) {
			return true
		}
	}
	return false
}

// lksStringPtrIfSet and lksInt64PtrIfSet return a pointer only for a known,
// non-null value. An Optional+Computed attribute the config omits is
// *unknown*, not null, and ValueStringPointer/ValueInt64Pointer only special-
// case null — so the plain call would put an explicit "" (or 0) on the wire
// and the platform would store that instead of generating a name or applying
// its own default.
func lksStringPtrIfSet(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return v.ValueStringPointer()
}

// lksDescriptionFromAPI reconciles what the API reports for a description
// against what is configured.
//
// An unset description comes back as "" rather than absent (seen live
// 2026-09-18, on a node pool whose configuration never gave one). Copying that
// straight into state leaves a config that omits `description` with a
// permanent diff — and one that can never converge, because the API rejects ""
// on the way back in and ignores an explicit null.
//
// So "" collapses to null, but only when the configured value is already null
// — never when it holds a value to drift away from, which is what keeps an
// out-of-band change visible.
func lksDescriptionFromAPI(configured, reported types.String) types.String {
	if configured.IsNull() && reported.ValueString() == "" {
		return configured
	}
	return reported
}

// lksDescriptionForUpdate carries description through an update unchanged.
//
// It exists to hold a finding, not to transform anything: `description` LOOKS
// clearable — `omitempty` on a *string drops only a nil pointer, so a pointer
// to "" does reach the wire, unlike the map and slice fields (see
// lks_clear_transport.go). But the API rejects it. Both update contracts
// declare `optional(:description).filled(:string)`, so `""` is a 422
// ("must be filled"), and an explicit null is dropped by the controller's
// `.compact` before it reaches the writer — a silent no-op.
//
// So there is currently NO way to clear an LKS description, and sending "" on
// every update where the attribute is null would 422 every update rather than
// just the clearing one. Passing nil keeps the field out of the payload, which
// the API reads as "leave unchanged".
//
// Confirmed against the API repo (V3::LKS update contracts) on 2026-09-18;
// revisit if `filled` is relaxed to allow an empty string.
func lksDescriptionForUpdate(v types.String) *string {
	return lksStringPtrIfSet(v)
}

func lksInt64PtrIfSet(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return v.ValueInt64Pointer()
}

func (r *LksNodePoolResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lks_node_pool"
}

func (r *LksNodePoolResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A node pool for an LKS (Latitude Kubernetes Service) cluster. `latitudesh_lks` provisions a control plane only — without at least one node pool the cluster has no workers and schedules nothing.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Node pool identifier.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"cluster_id": schema.StringAttribute{
				MarkdownDescription: "ID of the `latitudesh_lks` cluster the pool belongs to. Changing it forces a new resource; a pool cannot move between clusters.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"plan": schema.StringAttribute{
				MarkdownDescription: "Plan the pool's nodes are provisioned from, as listed by `latitudesh_lks_plans` — not interchangeable with a `latitudesh_plan` server slug. Check the plan has stock at the cluster's site (`in_stock_sites`) before applying. There is no update endpoint for it, so changing it forces a new resource.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			// `count` itself is a reserved Terraform meta-argument, so the
			// attribute takes the name the other Kubernetes providers use.
			"node_count": schema.Int64Attribute{
				MarkdownDescription: "Number of nodes in the pool. Changing it scales the pool in place; apply waits for the new count to be ready.",
				Required:            true,
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Node type. `bare_metal` is the only value the API accepts today. Set at creation; changing it forces a new resource.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("bare_metal"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"kubernetes_version": schema.StringAttribute{
				MarkdownDescription: "Kubernetes patch version for the pool's nodes, from `latitudesh_lks_versions`. **Omit it and the pool tracks the cluster's control-plane version**: a control-plane upgrade brings the pool with it (on the next apply, since the pool learns the new version only once the cluster is on it). Set it explicitly to pin the pool to one version instead. Either way it must never be **newer** than the control plane, which the API rejects with 422 `VERSION_SKEW` — tracking guarantees this by construction, upgrade the cluster first.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"max_pods_per_node": schema.Int64Attribute{
				MarkdownDescription: "kubelet `--max-pods` for every node in the pool. Create-only: the API rejects a PATCH that carries it with 422, so changing it forces a new resource. Omit for the platform default (110).",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Display name for the pool. Generated by the platform when omitted.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Optional customer description. It cannot be removed once set — the API rejects an empty value and ignores a null — so removing it from configuration leaves a diff that cannot converge; change it instead.",
				Optional:            true,
			},
			"labels": schema.MapAttribute{
				MarkdownDescription: "Kubernetes labels applied to every node in the pool (max 50). Keys follow Kubernetes label-key syntax; values may be empty. Keys under `kubernetes.io`, `k8s.io`, `cluster.x-k8s.io`, `lks.latitude.sh` or their subdomains are reserved and rejected with 422.",
				ElementType:         types.StringType,
				Optional:            true,
				Validators: []validator.Map{
					mapvalidator.SizeAtMost(50),
					lksReservedLabelKeysValidator{},
				},
			},
			"taints": schema.SetNestedAttribute{
				MarkdownDescription: "Kubernetes taints applied to every node in the pool (max 50). A set rather than a list: the API imposes no ordering, and each `(key, effect)` pair must be unique.",
				Optional:            true,
				Validators: []validator.Set{
					setvalidator.SizeAtMost(50),
					lksUniqueTaintsValidator{},
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"key": schema.StringAttribute{
							MarkdownDescription: "Taint key. Same syntax and reserved-prefix rules as a label key.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.LengthAtLeast(1),
							},
						},
						"value": schema.StringAttribute{
							MarkdownDescription: "Taint value.",
							Optional:            true,
						},
						"effect": schema.StringAttribute{
							MarkdownDescription: "Taint effect: `NoSchedule`, `PreferNoSchedule` or `NoExecute`.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("NoSchedule", "PreferNoSchedule", "NoExecute"),
							},
						},
					},
				},
			},
			"mode": schema.StringAttribute{
				MarkdownDescription: "Pool mode reported by the platform.",
				Computed:            true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Pool lifecycle status. Open enum sourced from the platform controller — new values may appear without notice, which is why apply waits until the platform reports no operation in progress rather than for one particular word.",
				Computed:            true,
			},
			"message": schema.StringAttribute{
				MarkdownDescription: "Human-readable detail behind the current `status`.",
				Computed:            true,
			},
			"reason": schema.StringAttribute{
				MarkdownDescription: "Machine-readable status reason (open enum).",
				Computed:            true,
			},
			"ready_nodes": schema.Int64Attribute{
				MarkdownDescription: "Nodes currently ready. The platform does not always report it: it is absent while the pool is still building, and stays `null` on pools it never counts — so a null here does not mean zero. When it is reported, apply additionally waits for it to reach `node_count`.",
				Computed:            true,
			},
			"platform_version": schema.StringAttribute{
				MarkdownDescription: "Platform (LKS controller) version managing this pool.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Timestamp when the pool was created.",
				Computed:            true,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "Timestamp when the pool was last updated.",
				Computed:            true,
			},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{
				Create:            true,
				Update:            true,
				Delete:            true,
				CreateDescription: `Timeout for the pool to settle — the platform reporting no operation in progress, and ready_nodes reaching node_count when it reports one. Bare metal, so allow for a real deploy. Default: 60 minutes.`,
				UpdateDescription: `Timeout for a scale or version change to settle. A scale and a version change in one apply are two sequential operations, each waited on, within this one budget. Default: 60 minutes.`,
				DeleteDescription: `Timeout for the pool to be fully removed. Default: 30 minutes.`,
			}),
		},
	}
}

func (r *LksNodePoolResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := providerpkg.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.client = deps.Client
}

// ModifyPlan makes an omitted kubernetes_version follow the cluster's control
// plane. Set explicitly, the version is pinned and left alone. Unlike the
// cluster's default_node_pool — where both versions are in one model — a
// standalone pool only knows its cluster_id, so the cluster's version has to be
// read from the API here.
//
// This gives two-apply convergence on a control-plane upgrade: at the plan that
// bumps the cluster, the GET below still returns the old version (the upgrade
// has not run), so the pool follows on the NEXT apply. Reading the cluster
// per plan is the cost of following without the config referencing the cluster
// resource. A failed read or an unknown cluster_id leaves the version to its
// normal resolution rather than blocking the plan.
func (r *LksNodePoolResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy
	}

	var cfg, plan LksNodePoolResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Explicitly configured version: pinned, not tracked.
	if !cfg.KubernetesVersion.IsNull() {
		return
	}
	// cluster_id from another resource not created yet — nothing to read; Create
	// will let the API default the version to the cluster's.
	if plan.ClusterID.IsNull() || plan.ClusterID.IsUnknown() {
		return
	}
	if r.client == nil {
		return
	}

	result, err := r.client.Lks.GetLksCluster(ctx, plan.ClusterID.ValueString())
	if err != nil {
		// A read failure must not break planning; fall back to the normal
		// resolution (Computed/UseStateForUnknown, then the API on apply).
		return
	}
	if result == nil || result.LksCluster == nil || result.LksCluster.Data == nil ||
		result.LksCluster.Data.Attributes == nil || result.LksCluster.Data.Attributes.KubernetesVersion == nil {
		return
	}

	clusterVersion := *result.LksCluster.Data.Attributes.KubernetesVersion
	if clusterVersion == "" {
		return
	}

	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx,
		path.Root("kubernetes_version"), types.StringValue(clusterVersion))...)
}

func (r *LksNodePoolResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data LksNodePoolResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	labels, diags := lksLabelsFromMap(ctx, data.Labels)
	resp.Diagnostics.Append(diags...)
	taints, diags := lksTaintsFromSet(ctx, data.Taints)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	attrs := components.CreateLksNodePoolAttributes{
		Plan:              data.Plan.ValueString(),
		Count:             data.NodeCount.ValueInt64(),
		KubernetesVersion: lksStringPtrIfSet(data.KubernetesVersion),
		MaxPodsPerNode:    lksInt64PtrIfSet(data.MaxPodsPerNode),
		Name:              lksStringPtrIfSet(data.Name),
		Description:       lksStringPtrIfSet(data.Description),
		Labels:            labels,
		Taints:            taints,
	}
	if !data.Type.IsNull() && !data.Type.IsUnknown() {
		attrs.Type = components.CreateLksNodePoolDataType(data.Type.ValueString()).ToPointer()
	}

	clusterID := data.ClusterID.ValueString()

	result, err := r.client.Lks.CreateLksNodePool(ctx, clusterID, components.CreateLksNodePool{
		Data: components.CreateLksNodePoolData{
			Type:       components.CreateLksNodePoolTypeLksNodePools,
			Attributes: attrs,
		},
	})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create LKS node pool, got error: "+err.Error())
		return
	}
	if result == nil || result.LksNodePool == nil || result.LksNodePool.Data == nil || result.LksNodePool.Data.ID == nil {
		resp.Diagnostics.AddError("Unexpected API response", "The LKS node pool creation response did not include an ID.")
		return
	}

	id := *result.LksNodePool.Data.ID
	data.ID = types.StringValue(id)

	// How many nodes to wait for is the configured count, captured before the
	// response is folded in: node_count is Required, so a response that omits
	// it would otherwise leave the wait chasing zero and return at once.
	wantNodes := data.NodeCount.ValueInt64()

	// Record what the POST already answered before waiting on the nodes: they
	// are physical machines, so this wait is long, and Terraform turns every
	// still-unknown attribute into null when an apply errors. Persisting only
	// the ID is what leaves an interrupted create with a state full of nulls
	// for fields the API had already reported. The read below is still what
	// produces the final state.
	lksApplyNodePoolAttributes(ctx, &data, result.LksNodePool.Data.Attributes, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, diags := data.Timeouts.Create(ctx, 60*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.waitForNodesReady(ctx, clusterID, id, wantNodes, time.Now().Add(createTimeout), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		// The pool exists even though it never came up, and the write above
		// already recorded it, so a follow-up apply or destroy can address it
		// instead of leaking it.
		return
	}

	r.readNodePoolInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *LksNodePoolResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data LksNodePoolResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readNodePoolInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.IsNull() {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *LksNodePoolResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data LksNodePoolResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Prior state decides which of count/kubernetes_version actually changed,
	// and they cannot travel together — see lksPatchNodePool.
	var state LksNodePoolResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateTimeout, diags := data.Timeouts.Update(ctx, 60*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	lksPatchNodePool(ctx, r.client, lksNodePoolPatch{
		ClusterID:      data.ClusterID.ValueString(),
		PoolID:         data.ID.ValueString(),
		Name:           data.Name,
		Description:    data.Description,
		Labels:         data.Labels,
		Taints:         data.Taints,
		Count:          data.NodeCount,
		CountChanged:   !data.NodeCount.Equal(state.NodeCount),
		Version:        data.KubernetesVersion,
		VersionChanged: !data.KubernetesVersion.Equal(state.KubernetesVersion),
		Deadline:       time.Now().Add(updateTimeout),
	}, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readNodePoolInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// lksNodePoolPatch is one pending node pool update, already reduced to what
// changed.
type lksNodePoolPatch struct {
	ClusterID string
	PoolID    string

	Name        types.String
	Description types.String
	Labels      types.Map
	Taints      types.Set

	Count        types.Int64
	CountChanged bool

	Version        types.String
	VersionChanged bool

	// Deadline is absolute and bounds BOTH waits below together: a scale and a
	// version change in one apply are two sequential operations inside one
	// timeout budget, not one budget each.
	Deadline time.Time
}

// lksPatchNodePool applies a node pool update, splitting it across requests
// where the API requires it.
//
// **A scale and an upgrade may not share a PATCH**: the API answers 422
// UNPROCESSABLE_ENTITY when `count` and `kubernetes_version` arrive together.
// The dashboard hits the same wall and guards against it client-side
// (latitude.sh apps/dashboard/pages/api/lks/node-pools.js, "Scale and upgrade
// must be separate operations"), which is where this rule was found — nothing
// in the SDK or the swagger mentions it. Sending both unconditionally, which
// is the obvious implementation, makes EVERY update fail: kubernetes_version
// is Optional+Computed with UseStateForUnknown, so after create the plan
// always carries a known value.
//
// The scale goes first and the upgrade second. Both waits then target the same
// node count, which is what makes them correct: waiting for the TARGET count
// after a version-only PATCH hangs forever, because the pool is still at its
// old size and nothing will move it. Upgrading after the scale does mean new
// nodes are built on the old version and rolled immediately, which is wasteful
// but finite — the alternative deadlocks.
func lksPatchNodePool(ctx context.Context, client *latitudeshgosdk.Latitudesh, patch lksNodePoolPatch, diags *diag.Diagnostics) {
	labels, d := lksLabelsFromMap(ctx, patch.Labels)
	diags.Append(d...)
	taints, d := lksTaintsFromSet(ctx, patch.Taints)
	diags.Append(d...)
	if diags.HasError() {
		return
	}

	send := func(ctx context.Context, attrs *components.UpdateLksNodePoolAttributes) bool {
		_, err := client.Lks.UpdateLksNodePool(ctx, patch.ClusterID, patch.PoolID, components.UpdateLksNodePool{
			Data: components.UpdateLksNodePoolData{
				Type:       components.UpdateLksNodePoolTypeLksNodePools,
				Attributes: attrs,
			},
		})
		if err != nil {
			diags.AddError("Client Error", "Unable to update LKS node pool, got error: "+err.Error())
			return false
		}
		return true
	}

	wantCount := patch.Count.ValueInt64()

	// Metadata rides along with the scale: only the version is excluded.
	// max_pods_per_node is create-only and carries RequiresReplace, so it is
	// deliberately absent here too — sending it at all is a 422.
	attrs := &components.UpdateLksNodePoolAttributes{
		Name:        lksStringPtrIfSet(patch.Name),
		Description: lksDescriptionForUpdate(patch.Description),
		Labels:      labels,
		Taints:      taints,
	}
	if patch.CountChanged {
		attrs.Count = &wantCount
	}

	// An empty collection is how the API is told to remove all labels or
	// taints, and the SDK cannot serialize one — see lks_clear_transport.go.
	clearCtx := withLksClear(ctx, lksClear{
		Labels: len(labels) == 0,
		Taints: len(taints) == 0,
	})
	if !send(clearCtx, attrs) {
		return
	}

	// Scaling is asynchronous: the API accepts the PATCH and keeps reporting
	// the old ready_nodes for a while.
	lksWaitForNodesReady(ctx, client, patch.ClusterID, patch.PoolID, wantCount, patch.Deadline, diags)
	if diags.HasError() || !patch.VersionChanged {
		return
	}

	if !send(ctx, &components.UpdateLksNodePoolAttributes{
		KubernetesVersion: lksStringPtrIfSet(patch.Version),
	}) {
		return
	}

	// Waiting between the two also keeps the second PATCH out of the API's
	// admission check, which answers 422 "operation in progress" while the
	// pool is still settling.
	//
	// An upgrade rolls the nodes, so ready_nodes dips and has to come back.
	lksWaitForNodesReady(ctx, client, patch.ClusterID, patch.PoolID, wantCount, patch.Deadline, diags)
}

func (r *LksNodePoolResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data LksNodePoolResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	clusterID := data.ClusterID.ValueString()
	id := data.ID.ValueString()
	if clusterID == "" || id == "" {
		return
	}

	_, err := r.client.Lks.DeleteLksNodePool(ctx, clusterID, id)
	if err != nil {
		if lksClusterNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to delete LKS node pool, got error: "+err.Error())
		return
	}

	deleteTimeout, diags := data.Timeouts.Delete(ctx, 30*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.waitForNodePoolDeleted(ctx, clusterID, id, time.Now().Add(deleteTimeout), &resp.Diagnostics)
}

func (r *LksNodePoolResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// GetLksNodePool is addressed by (cluster_id, id), so a bare pool ID is not
	// enough to read one back — unlike latitudesh_lks, the composite form is
	// mandatory here rather than an alternative.
	clusterID, id, found := strings.Cut(req.ID, ":")
	if !found || clusterID == "" || id == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID Format",
			"Import ID must be in the format cluster_id:node_pool_id — the API reads a node pool by both.",
		)
		return
	}

	var data LksNodePoolResourceModel
	data.ID = types.StringValue(id)
	data.ClusterID = types.StringValue(clusterID)

	// No timeouts block is set on import, so give the field an explicitly typed
	// null (see latitudesh_lks's ImportState for why the zero value fails).
	data.Timeouts = timeouts.Value{
		Object: types.ObjectNull(map[string]attr.Type{
			"create": types.StringType,
			"update": types.StringType,
			"delete": types.StringType,
		}),
	}

	r.readNodePoolInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.IsNull() {
		resp.Diagnostics.AddError("Not Found", "LKS node pool "+id+" not found in cluster "+clusterID)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// readNodePoolInto issues a Get for (cluster_id, id) and refreshes every
// attribute. cluster_id itself is never echoed back: it is the address, not a
// field of the response.
func (r *LksNodePoolResource) readNodePoolInto(ctx context.Context, data *LksNodePoolResourceModel, diags *diag.Diagnostics) {
	clusterID := data.ClusterID.ValueString()
	id := data.ID.ValueString()
	if clusterID == "" || id == "" {
		diags.AddError("Invalid ID", "LKS node pool needs both cluster_id and id to be read")
		return
	}

	result, err := r.client.Lks.GetLksNodePool(ctx, clusterID, id)
	if err != nil {
		if lksClusterNotFound(err) {
			data.ID = types.StringNull()
			return
		}
		diags.AddError("Client Error", "Unable to read LKS node pool, got error: "+err.Error())
		return
	}

	if result == nil || result.LksNodePool == nil || result.LksNodePool.Data == nil {
		data.ID = types.StringNull()
		return
	}

	obj := result.LksNodePool.Data
	if obj.ID != nil {
		data.ID = types.StringValue(*obj.ID)
	}

	lksApplyNodePoolAttributes(ctx, data, obj.Attributes, diags)
}

// lksApplyNodePoolAttributes folds an attributes envelope into the model. The
// POST response and a later GET carry the same one, so the create path records
// exactly what a refresh would — see the call in Create.
func lksApplyNodePoolAttributes(ctx context.Context, data *LksNodePoolResourceModel, attrs *components.LksNodePoolDataAttributes, diags *diag.Diagnostics) {
	fields, mapDiags := mapLksNodePoolAttributes(ctx, attrs)
	diags.Append(mapDiags...)

	data.Plan = fields.Plan
	data.NodeCount = fields.NodeCount
	data.Type = fields.Type
	data.KubernetesVersion = fields.KubernetesVersion
	data.Name = fields.Name

	// max_pods_per_node is create-only, so a response that omits it must not
	// wipe the configured value — there is no drift to detect on a field the
	// API will not let anyone change. It is Computed too, though, so an omitted
	// value still has to resolve: unknown becomes null, or apply fails with
	// "provider returned invalid result object".
	switch {
	case !fields.MaxPodsPerNode.IsNull():
		data.MaxPodsPerNode = fields.MaxPodsPerNode
	case data.MaxPodsPerNode.IsUnknown():
		data.MaxPodsPerNode = types.Int64Null()
	}

	// description, labels and taints are all updatable, so the API owns them:
	// assigning unconditionally is what makes an out-of-band change show up as
	// drift (the mistake latitudesh_lks shipped with for description).
	data.Description = lksDescriptionFromAPI(data.Description, fields.Description)
	data.Labels = fields.Labels
	data.Taints = fields.Taints

	data.Mode = fields.Mode
	data.Status = fields.Status
	data.Message = fields.Message
	data.Reason = fields.Reason
	data.ReadyNodes = fields.ReadyNodes
	data.PlatformVersion = fields.PlatformVersion
	data.CreatedAt = fields.CreatedAt
	data.UpdatedAt = fields.UpdatedAt
}

// waitForNodesReady polls until ready_nodes reaches want. No terminal failure
// status is documented for node pools, so a pool that never comes up is a
// timeout rather than a fast failure.
func (r *LksNodePoolResource) waitForNodesReady(ctx context.Context, clusterID, id string, want int64, deadline time.Time, diags *diag.Diagnostics) {
	lksWaitForNodesReady(ctx, r.client, clusterID, id, want, deadline, diags)
}

// lksWaitForNodesReady is the free function behind it: latitudesh_lks runs the
// same wait for the default_node_pool it creates inside its own Create.
//
// deadline is absolute and shared with the other waits of the same operation.
func lksWaitForNodesReady(ctx context.Context, client *latitudeshgosdk.Latitudesh, clusterID, id string, want int64, deadline time.Time, diags *diag.Diagnostics) {
	pollInterval := lksNodePoolReadyPollInterval

	lastReady := int64(-1)
	lastStatus := ""
	consecutiveErrors := 0

	for time.Now().Before(deadline) {
		result, err := client.Lks.GetLksNodePool(ctx, clusterID, id)
		if err != nil {
			if !lksRetryableDuringPoll(err) {
				diags.AddError("Client Error", "Unable to check LKS node pool status: "+err.Error())
				return
			}
			consecutiveErrors++
			if consecutiveErrors >= lksNodePoolMaxConsecutiveError {
				diags.AddError("Client Error", fmt.Sprintf("Unable to check LKS node pool status after %d consecutive attempts, last error: %s", consecutiveErrors, err.Error()))
				return
			}
			select {
			case <-ctx.Done():
				diags.AddError("Client Error", "Context cancelled while waiting for LKS node pool nodes: "+ctx.Err().Error())
				return
			case <-time.After(pollInterval):
				continue
			}
		}
		consecutiveErrors = 0

		if result != nil && result.LksNodePool != nil && result.LksNodePool.Data != nil && result.LksNodePool.Data.Attributes != nil {
			attrs := result.LksNodePool.Data.Attributes
			if attrs.Status != nil {
				lastStatus = *attrs.Status
			}
			if attrs.ReadyNodes != nil {
				lastReady = *attrs.ReadyNodes
			}

			done, hopeless := lksSettled(lastStatus)
			if hopeless {
				diags.AddError(
					"LKS node pool cannot become ready",
					fmt.Sprintf("LKS node pool %q is %q, which no amount of waiting will change.", id, lastStatus),
				)
				return
			}

			// ready_nodes is a passthrough with no default on the API side, so
			// it can be absent entirely. Gating only on it means a pool that is
			// demonstrably up polls until the timeout because the number never
			// arrived. The platform's own "no operation running" is the
			// authority; the count is a stronger check layered on top when the
			// API bothers to report it.
			if done && (attrs.ReadyNodes == nil || lastReady >= want) {
				return
			}
		}

		select {
		case <-ctx.Done():
			diags.AddError("Client Error", "Context cancelled while waiting for LKS node pool nodes: "+ctx.Err().Error())
			return
		case <-time.After(pollInterval):
		}
	}

	readyNodes := "not reported"
	if lastReady >= 0 {
		readyNodes = strconv.FormatInt(lastReady, 10)
	}
	diags.AddError(
		"Timeout waiting for LKS node pool",
		fmt.Sprintf("LKS node pool %q in cluster %q did not reach %d ready node(s) before the timeout expired (last ready_nodes: %s, last status: %q).",
			id, clusterID, want, readyNodes, lastStatus),
	)
}

// waitForNodePoolDeleted polls until the pool 404s.
func (r *LksNodePoolResource) waitForNodePoolDeleted(ctx context.Context, clusterID, id string, deadline time.Time, diags *diag.Diagnostics) {
	lksWaitForNodePoolDeleted(ctx, r.client, clusterID, id, deadline, diags)
}

func lksWaitForNodePoolDeleted(ctx context.Context, client *latitudeshgosdk.Latitudesh, clusterID, id string, deadline time.Time, diags *diag.Diagnostics) {
	pollInterval := lksNodePoolDeletePollInterval

	consecutiveErrors := 0

	for time.Now().Before(deadline) {
		_, err := client.Lks.GetLksNodePool(ctx, clusterID, id)
		if err != nil {
			if lksClusterNotFound(err) {
				return
			}
			if !lksRetryableDuringPoll(err) {
				diags.AddError("Client Error", "Unable to check LKS node pool deletion: "+err.Error())
				return
			}
			consecutiveErrors++
			if consecutiveErrors >= lksNodePoolMaxConsecutiveError {
				diags.AddError("Client Error", fmt.Sprintf("Unable to check LKS node pool deletion after %d consecutive attempts, last error: %s", consecutiveErrors, err.Error()))
				return
			}
		} else {
			consecutiveErrors = 0
		}

		select {
		case <-ctx.Done():
			diags.AddError("Client Error", "Context cancelled while waiting for LKS node pool deletion: "+ctx.Err().Error())
			return
		case <-time.After(pollInterval):
		}
	}

	diags.AddError(
		"Timeout waiting for LKS node pool deletion",
		fmt.Sprintf("LKS node pool %q in cluster %q was not removed before the delete timeout expired.", id, clusterID),
	)
}
