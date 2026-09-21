package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	goversion "github.com/hashicorp/go-version"
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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	providerpkg "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var _ resource.Resource = &LksResource{}
var _ resource.ResourceWithImportState = &LksResource{}
var _ resource.ResourceWithModifyPlan = &LksResource{}

// Poll intervals for the async create/update/delete waits. Declared as vars
// (not consts) so tests can shorten them; production keeps the defaults.
var (
	lksReadyPollInterval  = 10 * time.Second
	lksDeletePollInterval = 5 * time.Second
)

func NewLksResource() resource.Resource {
	return &LksResource{}
}

type LksResource struct {
	client         *latitudeshgosdk.Latitudesh
	defaultProject string
}

// LksNetworkModel is the nested `network` object: cluster CIDR overrides.
type LksNetworkModel struct {
	PodCidrs     types.Set `tfsdk:"pod_cidrs"`
	ServiceCidrs types.Set `tfsdk:"service_cidrs"`
	NodeCidrs    types.Set `tfsdk:"node_cidrs"`
}

var lksNetworkAttrTypes = map[string]attr.Type{
	"pod_cidrs":     types.SetType{ElemType: types.StringType},
	"service_cidrs": types.SetType{ElemType: types.StringType},
	"node_cidrs":    types.SetType{ElemType: types.StringType},
}

type LksResourceModel struct {
	ID                   types.String   `tfsdk:"id"`
	Project              types.String   `tfsdk:"project"`
	Name                 types.String   `tfsdk:"name"`
	Site                 types.String   `tfsdk:"site"`
	KubernetesVersion    types.String   `tfsdk:"kubernetes_version"`
	Description          types.String   `tfsdk:"description"`
	Network              types.Object   `tfsdk:"network"`
	DefaultNodePool      types.Object   `tfsdk:"default_node_pool"`
	Status               types.String   `tfsdk:"status"`
	Message              types.String   `tfsdk:"message"`
	Reason               types.String   `tfsdk:"reason"`
	ControlPlaneEndpoint types.String   `tfsdk:"control_plane_endpoint"`
	KubeconfigURL        types.String   `tfsdk:"kubeconfig_url"`
	PlatformVersion      types.String   `tfsdk:"platform_version"`
	CreatedAt            types.String   `tfsdk:"created_at"`
	UpdatedAt            types.String   `tfsdk:"updated_at"`
	Timeouts             timeouts.Value `tfsdk:"timeouts"`
}

// lksFields holds the fields read back from the API, shared between the
// resource and the data sources so the nil-check-and-convert logic is
// written, and tested, once.
type lksFields struct {
	Project              types.String
	Name                 types.String
	Site                 types.String
	KubernetesVersion    types.String
	Description          types.String
	Network              types.Object
	Status               types.String
	Message              types.String
	Reason               types.String
	ControlPlaneEndpoint types.String
	KubeconfigURL        types.String
	PlatformVersion      types.String
	CreatedAt            types.String
	UpdatedAt            types.String
}

// mapLksAttributes converts the SDK's attributes envelope into framework
// values, nil-checking every pointer field per house convention.
func mapLksAttributes(attrs *components.LksClusterDataAttributes) (lksFields, diag.Diagnostics) {
	var diags diag.Diagnostics

	out := lksFields{
		Project:              types.StringNull(),
		Name:                 types.StringNull(),
		Site:                 types.StringNull(),
		KubernetesVersion:    types.StringNull(),
		Description:          types.StringNull(),
		Network:              types.ObjectNull(lksNetworkAttrTypes),
		Status:               types.StringNull(),
		Message:              types.StringNull(),
		Reason:               types.StringNull(),
		ControlPlaneEndpoint: types.StringNull(),
		KubeconfigURL:        types.StringNull(),
		PlatformVersion:      types.StringNull(),
		CreatedAt:            types.StringNull(),
		UpdatedAt:            types.StringNull(),
	}
	if attrs == nil {
		return out, diags
	}

	out.Project = types.StringPointerValue(attrs.ProjectID)
	out.Name = types.StringPointerValue(attrs.Name)
	out.Site = types.StringPointerValue(attrs.Site)
	out.KubernetesVersion = types.StringPointerValue(attrs.KubernetesVersion)
	out.Description = types.StringPointerValue(attrs.Description)

	netObj, netDiags := mapLksNetwork(attrs.Network)
	diags.Append(netDiags...)
	out.Network = netObj

	out.Status = types.StringPointerValue(attrs.Status)
	out.Message = types.StringPointerValue(attrs.Message)
	out.Reason = types.StringPointerValue(attrs.Reason)
	out.ControlPlaneEndpoint = types.StringPointerValue(attrs.ControlPlaneEndpoint)
	out.KubeconfigURL = types.StringPointerValue(attrs.KubeconfigURL)
	out.PlatformVersion = types.StringPointerValue(attrs.PlatformVersion)
	out.CreatedAt = types.StringPointerValue(attrs.CreatedAt)
	out.UpdatedAt = types.StringPointerValue(attrs.UpdatedAt)

	return out, diags
}

// mapLksNetwork converts the API's network envelope (always present in
// responses, per its doc comment) into the nested `network` object. The
// three CIDR lists carry no order of their own, so they are sorted for a
// stable state representation (see stringsToSet in resource_elastic_ip_bgp.go).
func mapLksNetwork(n *components.Network) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	if n == nil {
		return types.ObjectNull(lksNetworkAttrTypes), diags
	}

	pod, d := stringsToSet(n.GetPodCidrs())
	diags.Append(d...)
	svc, d := stringsToSet(n.GetServiceCidrs())
	diags.Append(d...)
	node, d := stringsToSet(n.GetNodeCidrs())
	diags.Append(d...)
	if diags.HasError() {
		return types.ObjectNull(lksNetworkAttrTypes), diags
	}

	obj, objDiags := types.ObjectValue(lksNetworkAttrTypes, map[string]attr.Value{
		"pod_cidrs":     pod,
		"service_cidrs": svc,
		"node_cidrs":    node,
	})
	diags.Append(objDiags...)
	return obj, diags
}

// lksNetworkFromObject converts the configured `network` object into the
// create request's shape. A null or unknown object (network left unset)
// yields a nil pointer, so the platform default is used.
func lksNetworkFromObject(ctx context.Context, obj types.Object) (*components.CreateLksClusterNetwork, diag.Diagnostics) {
	var diags diag.Diagnostics
	if obj.IsNull() || obj.IsUnknown() {
		return nil, diags
	}

	var model LksNetworkModel
	diags.Append(obj.As(ctx, &model, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return nil, diags
	}

	pod, d := setToStrings(ctx, model.PodCidrs)
	diags.Append(d...)
	svc, d := setToStrings(ctx, model.ServiceCidrs)
	diags.Append(d...)
	node, d := setToStrings(ctx, model.NodeCidrs)
	diags.Append(d...)
	if diags.HasError() {
		return nil, diags
	}

	return &components.CreateLksClusterNetwork{
		PodCidrs:     pod,
		ServiceCidrs: svc,
		NodeCidrs:    node,
	}, diags
}

// --- default_node_pool ------------------------------------------------------
//
// The cluster owns exactly one node pool, created inside its own Create. This
// is the AKS/DigitalOcean shape, and here it is not a stylistic choice: a
// cluster with no pool never leaves "provisioning", and POST /lks/clusters
// takes none inline, so a pool expressed as a separate resource could only be
// created after Create returned — which is why Create cannot wait for
// readiness in that design. Folding the first pool in restores both: the
// requirement is enforced by the schema, and waiting for "ready" is honest
// again. Every pool beyond the first belongs to latitudesh_lks_node_pool.

// lksDefaultNodePoolModel mirrors the default_node_pool attribute. The
// read-only half of it is everything GET /lks/clusters/{id}/nodepools/{id}
// reports and latitudesh_lks_node_pool already exposes: the pool folded in
// here is the same object, so it answers the same questions — above all
// `message`/`reason`, which are what explain a pool that is stuck.
type lksDefaultNodePoolModel struct {
	ID                types.String `tfsdk:"id"`
	Plan              types.String `tfsdk:"plan"`
	NodeCount         types.Int64  `tfsdk:"node_count"`
	Name              types.String `tfsdk:"name"`
	Description       types.String `tfsdk:"description"`
	KubernetesVersion types.String `tfsdk:"kubernetes_version"`
	MaxPodsPerNode    types.Int64  `tfsdk:"max_pods_per_node"`
	Labels            types.Map    `tfsdk:"labels"`
	Taints            types.Set    `tfsdk:"taints"`
	Type              types.String `tfsdk:"type"`
	Mode              types.String `tfsdk:"mode"`
	Status            types.String `tfsdk:"status"`
	Message           types.String `tfsdk:"message"`
	Reason            types.String `tfsdk:"reason"`
	ReadyNodes        types.Int64  `tfsdk:"ready_nodes"`
	PlatformVersion   types.String `tfsdk:"platform_version"`
	CreatedAt         types.String `tfsdk:"created_at"`
	UpdatedAt         types.String `tfsdk:"updated_at"`
}

var lksDefaultNodePoolAttrTypes = map[string]attr.Type{
	"id":                 types.StringType,
	"plan":               types.StringType,
	"node_count":         types.Int64Type,
	"name":               types.StringType,
	"description":        types.StringType,
	"kubernetes_version": types.StringType,
	"max_pods_per_node":  types.Int64Type,
	"labels":             types.MapType{ElemType: types.StringType},
	"taints":             types.SetType{ElemType: lksTaintObjectType},
	"type":               types.StringType,
	"mode":               types.StringType,
	"status":             types.StringType,
	"message":            types.StringType,
	"reason":             types.StringType,
	"ready_nodes":        types.Int64Type,
	"platform_version":   types.StringType,
	"created_at":         types.StringType,
	"updated_at":         types.StringType,
}

func lksDefaultNodePoolFromObject(ctx context.Context, obj types.Object) (lksDefaultNodePoolModel, diag.Diagnostics) {
	var model lksDefaultNodePoolModel
	var diags diag.Diagnostics
	if obj.IsNull() || obj.IsUnknown() {
		return model, diags
	}
	diags.Append(obj.As(ctx, &model, basetypes.ObjectAsOptions{})...)
	return model, diags
}

// lksCreateDefaultNodePool POSTs the pool described by default_node_pool and
// returns its ID along with the attributes the API answered with, so the
// caller can record the pool as the platform created it before the long wait
// for its nodes rather than only after.
func (r *LksResource) lksCreateDefaultNodePool(ctx context.Context, clusterID string, pool lksDefaultNodePoolModel, diags *diag.Diagnostics) (string, *components.LksNodePoolDataAttributes) {
	labels, d := lksLabelsFromMap(ctx, pool.Labels)
	diags.Append(d...)
	taints, d := lksTaintsFromSet(ctx, pool.Taints)
	diags.Append(d...)
	if diags.HasError() {
		return "", nil
	}

	result, err := r.client.Lks.CreateLksNodePool(ctx, clusterID, components.CreateLksNodePool{
		Data: components.CreateLksNodePoolData{
			Type: components.CreateLksNodePoolTypeLksNodePools,
			Attributes: components.CreateLksNodePoolAttributes{
				Plan:              pool.Plan.ValueString(),
				Count:             pool.NodeCount.ValueInt64(),
				KubernetesVersion: lksStringPtrIfSet(pool.KubernetesVersion),
				MaxPodsPerNode:    lksInt64PtrIfSet(pool.MaxPodsPerNode),
				Name:              lksStringPtrIfSet(pool.Name),
				Description:       lksStringPtrIfSet(pool.Description),
				Labels:            labels,
				Taints:            taints,
			},
		},
	})
	if err != nil {
		diags.AddError("Client Error", "Unable to create the cluster's default node pool, got error: "+err.Error())
		return "", nil
	}
	if result == nil || result.LksNodePool == nil || result.LksNodePool.Data == nil || result.LksNodePool.Data.ID == nil {
		diags.AddError("Unexpected API response", "The default node pool creation response did not include an ID.")
		return "", nil
	}
	return *result.LksNodePool.Data.ID, result.LksNodePool.Data.Attributes
}

// lksReadDefaultNodePoolInto refreshes default_node_pool from the API. A pool
// that has been removed out of band comes back as a null object, which plans as
// a change and is recreated on the next apply rather than silently leaving the
// cluster without one.
func (r *LksResource) lksReadDefaultNodePoolInto(ctx context.Context, clusterID string, data *LksResourceModel, diags *diag.Diagnostics) {
	planned, d := lksDefaultNodePoolFromObject(ctx, data.DefaultNodePool)
	diags.Append(d...)
	if diags.HasError() {
		return
	}

	poolID := planned.ID.ValueString()
	if poolID == "" {
		data.DefaultNodePool = types.ObjectNull(lksDefaultNodePoolAttrTypes)
		return
	}

	result, err := r.client.Lks.GetLksNodePool(ctx, clusterID, poolID)
	if err != nil {
		if lksClusterNotFound(err) {
			data.DefaultNodePool = types.ObjectNull(lksDefaultNodePoolAttrTypes)
			return
		}
		diags.AddError("Client Error", "Unable to read the cluster's default node pool, got error: "+err.Error())
		return
	}
	if result == nil || result.LksNodePool == nil || result.LksNodePool.Data == nil {
		data.DefaultNodePool = types.ObjectNull(lksDefaultNodePoolAttrTypes)
		return
	}

	data.DefaultNodePool = lksDefaultNodePoolObject(ctx, poolID, planned, result.LksNodePool.Data.Attributes, diags)
}

// lksDefaultNodePoolObject folds the API's view of a pool into the planned
// default_node_pool. Both the POST response and a later GET carry the same
// attributes envelope, so the create path and the refresh path agree on what
// state ends up holding.
func lksDefaultNodePoolObject(ctx context.Context, poolID string, planned lksDefaultNodePoolModel, attrs *components.LksNodePoolDataAttributes, diags *diag.Diagnostics) types.Object {
	fields, mapDiags := mapLksNodePoolAttributes(ctx, attrs)
	diags.Append(mapDiags...)

	out := lksDefaultNodePoolModel{
		ID:                types.StringValue(poolID),
		Plan:              fields.Plan,
		NodeCount:         fields.NodeCount,
		Name:              fields.Name,
		Description:       lksDescriptionFromAPI(planned.Description, fields.Description),
		KubernetesVersion: fields.KubernetesVersion,
		MaxPodsPerNode:    planned.MaxPodsPerNode,
		Labels:            fields.Labels,
		Taints:            fields.Taints,
		Type:              fields.Type,
		Mode:              fields.Mode,
		Status:            fields.Status,
		Message:           fields.Message,
		Reason:            fields.Reason,
		ReadyNodes:        fields.ReadyNodes,
		PlatformVersion:   fields.PlatformVersion,
		CreatedAt:         fields.CreatedAt,
		UpdatedAt:         fields.UpdatedAt,
	}
	// Create-only, so an omitted value must not wipe what was configured —
	// but it still has to resolve, Computed attributes never staying unknown.
	if !fields.MaxPodsPerNode.IsNull() {
		out.MaxPodsPerNode = fields.MaxPodsPerNode
	} else if out.MaxPodsPerNode.IsUnknown() {
		out.MaxPodsPerNode = types.Int64Null()
	}

	obj, objDiags := types.ObjectValueFrom(ctx, lksDefaultNodePoolAttrTypes, out)
	diags.Append(objDiags...)
	return obj
}

func (r *LksResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lks"
}

func (r *LksResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "LKS (Latitude Kubernetes Service) cluster resource. Provisions the control plane and the cluster's first node pool.\n\n" +
			"**A cluster needs at least one node pool**, which is why `default_node_pool` is required here: until one exists the control plane never finishes converging and `status` stays `provisioning` — permanently, not slowly. Create builds that pool and waits for the whole thing to reach `ready`, so an apply that succeeds leaves a usable cluster. Every pool beyond the first is a `latitudesh_lks_node_pool`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "LKS cluster identifier.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "The project (ID or slug) to create the cluster in. Optional here only if `project` is set on the provider block; one of the two is required. Changing it forces a new resource.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Display name for the cluster.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"site": schema.StringAttribute{
				MarkdownDescription: "Site slug the cluster is deployed to (single site per cluster; one of the slugs returned by `GET /lks/sites`). Changing this forces a new resource; there is no update endpoint.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"kubernetes_version": schema.StringAttribute{
				MarkdownDescription: "Kubernetes patch version, exactly as listed by `GET /lks/available_versions`. Setting a newer patch consents to a control-plane upgrade in place. Lowering it is rejected at plan time: the platform allows no in-place downgrade (422 `DOWNGRADE_NOT_ALLOWED`), so this fails before the apply rather than an hour into it.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Optional customer description. It cannot be removed once set — the API rejects an empty value and ignores a null — so removing it from configuration leaves a diff that cannot converge; change it instead.",
				Optional:            true,
			},
			"network": schema.SingleNestedAttribute{
				MarkdownDescription: "Cluster CIDR overrides. Any field left unset takes the platform default. Changing this forces a new resource; there is no update endpoint.",
				Optional:            true,
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"pod_cidrs": schema.SetAttribute{
						MarkdownDescription: "Pod CIDR ranges.",
						ElementType:         types.StringType,
						Optional:            true,
						Computed:            true,
					},
					"service_cidrs": schema.SetAttribute{
						MarkdownDescription: "Service CIDR ranges.",
						ElementType:         types.StringType,
						Optional:            true,
						Computed:            true,
					},
					"node_cidrs": schema.SetAttribute{
						MarkdownDescription: "Node CIDR ranges.",
						ElementType:         types.StringType,
						Optional:            true,
						Computed:            true,
					},
				},
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
					objectplanmodifier.RequiresReplace(),
				},
			},
			"default_node_pool": schema.SingleNestedAttribute{
				MarkdownDescription: "The cluster's first node pool, created with the cluster and required: a cluster with no node pool never leaves `provisioning`, and `POST /lks/clusters` takes none inline, so it cannot be a separate resource without deadlocking. Additional pools go in `latitudesh_lks_node_pool`, which owns its own lifecycle; this resource only ever manages the pool whose `id` it recorded here and ignores any other pool in the cluster.",
				Required:            true,
				Attributes: map[string]schema.Attribute{
					"id": schema.StringAttribute{
						MarkdownDescription: "Node pool identifier, assigned on creation. This is how the cluster recognizes its own pool on refresh.",
						Computed:            true,
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.UseStateForUnknown(),
						},
					},
					"plan": schema.StringAttribute{
						MarkdownDescription: "Plan the pool's nodes are provisioned from, as listed by `latitudesh_lks_plans` — not a `latitudesh_plan` server slug. It must have stock at the cluster's `site` (`in_stock_sites`). Changing it replaces the pool, not the cluster: a new one is built before the old is removed, so the cluster is never left without one.",
						Required:            true,
						Validators: []validator.String{
							stringvalidator.LengthAtLeast(1),
						},
					},
					"node_count": schema.Int64Attribute{
						MarkdownDescription: "Number of nodes. Changing it scales the pool in place.",
						Required:            true,
						Validators: []validator.Int64{
							int64validator.AtLeast(1),
						},
					},
					"name": schema.StringAttribute{
						MarkdownDescription: "Display name for the pool. Generated by the platform when omitted.",
						Optional:            true,
						Computed:            true,
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.UseStateForUnknown(),
						},
					},
					"description": schema.StringAttribute{
						MarkdownDescription: "Optional customer description for the pool. Like the cluster's, it cannot be removed once set; change it instead.",
						Optional:            true,
					},
					"kubernetes_version": schema.StringAttribute{
						MarkdownDescription: "Kubernetes patch version for the pool's nodes. Omit it and the pool tracks the cluster's `kubernetes_version` — a control-plane upgrade upgrades this pool too, in the same apply. Set it explicitly to pin the pool to one version. Never newer than the control plane (422 `VERSION_SKEW`); tracking keeps it in step by construction.",
						Optional:            true,
						Computed:            true,
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.UseStateForUnknown(),
						},
					},
					"max_pods_per_node": schema.Int64Attribute{
						MarkdownDescription: "kubelet `--max-pods` for the pool's nodes. Create-only on the API, so changing it replaces the pool. Omit for the platform default (110).",
						Optional:            true,
						Computed:            true,
						Validators: []validator.Int64{
							int64validator.AtLeast(1),
						},
						PlanModifiers: []planmodifier.Int64{
							int64planmodifier.UseStateForUnknown(),
						},
					},
					"labels": schema.MapAttribute{
						MarkdownDescription: "Kubernetes labels applied to every node in the pool (max 50). Reserved prefixes are rejected — see `latitudesh_lks_node_pool`.",
						ElementType:         types.StringType,
						Optional:            true,
						Validators: []validator.Map{
							mapvalidator.SizeAtMost(50),
							lksReservedLabelKeysValidator{},
						},
					},
					"taints": schema.SetNestedAttribute{
						MarkdownDescription: "Kubernetes taints applied to every node in the pool (max 50). Each `(key, effect)` pair must be unique.\n\n" +
							"`NoSchedule` and `NoExecute` are rejected here: the cluster's own system workloads have nowhere to run but this pool, so a hard taint keeps them from scheduling and the cluster never leaves `provisioning`. Use `PreferNoSchedule`, or put the taint on a separate `latitudesh_lks_node_pool`.",
						Optional: true,
						Validators: []validator.Set{
							setvalidator.SizeAtMost(50),
							lksUniqueTaintsValidator{},
							lksDefaultPoolTaintsValidator{},
						},
						NestedObject: schema.NestedAttributeObject{
							Attributes: map[string]schema.Attribute{
								"key": schema.StringAttribute{
									MarkdownDescription: "Taint key.",
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
					"type": schema.StringAttribute{
						MarkdownDescription: "Node type backing the pool. Read-only here: `POST /lks/clusters/{id}/nodepools` only accepts `bare_metal` today, so there is nothing to choose. `latitudesh_lks_node_pool` exposes it as a write once that changes.",
						Computed:            true,
					},
					"mode": schema.StringAttribute{
						MarkdownDescription: "Provisioning mode the platform assigned to the pool's nodes, e.g. `on_demand`.",
						Computed:            true,
					},
					"status": schema.StringAttribute{
						MarkdownDescription: "Pool lifecycle status.",
						Computed:            true,
					},
					"message": schema.StringAttribute{
						MarkdownDescription: "Human-readable detail behind the pool's current `status` — this is what explains a pool that is not coming up.",
						Computed:            true,
					},
					"reason": schema.StringAttribute{
						MarkdownDescription: "Machine-readable status reason for the pool (open enum).",
						Computed:            true,
					},
					"ready_nodes": schema.Int64Attribute{
						MarkdownDescription: "Nodes currently ready. The platform does not always report it: it is absent while the pool is still building, and stays `null` on pools it never counts, so a null here does not mean zero.",
						Computed:            true,
					},
					"platform_version": schema.StringAttribute{
						MarkdownDescription: "Platform (LKS controller) version running on the pool's nodes.",
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
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Cluster lifecycle status. Open enum sourced from the platform controller; values in use today are `provisioning`, `ready`, `updating`, `scaling`, `upgrading`, `paused`, `deleting` and `deleted`, and new ones may appear without notice.\n\n" +
					"After a successful apply this reads `ready`: create builds `default_node_pool` and waits for the control plane to converge, so a cluster that never got there fails the apply instead of being recorded half-built.",
				Computed: true,
			},
			"message": schema.StringAttribute{
				MarkdownDescription: "Human-readable detail behind the current `status`.",
				Computed:            true,
			},
			"reason": schema.StringAttribute{
				MarkdownDescription: "Machine-readable status reason (open enum).",
				Computed:            true,
			},
			"control_plane_endpoint": schema.StringAttribute{
				MarkdownDescription: "Kubernetes API server endpoint.",
				Computed:            true,
			},
			"kubeconfig_url": schema.StringAttribute{
				MarkdownDescription: "URL to fetch the cluster kubeconfig from once the control plane is ready. `GET /lks/clusters/{id}/kubeconfig` answers 409 `NOT_READY` until `status` is `ready` — and the URL is published moments *after* the status flips, so it can still be null in state right after a create; the next refresh fills it.",
				Computed:            true,
			},
			"platform_version": schema.StringAttribute{
				MarkdownDescription: "Platform (LKS controller) version managing this cluster.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Timestamp when the cluster was created.",
				Computed:            true,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "Timestamp when the cluster was last updated.",
				Computed:            true,
			},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{
				Create:            true,
				Update:            true,
				Delete:            true,
				CreateDescription: `Budget for the entire create: the cluster record becoming readable, the nodes of default_node_pool coming up, and the control plane reaching status "ready". The nodes are the slow part — they are physical machines. Default: 30 minutes. Example: "45m", "1h"`,
				UpdateDescription: `Budget for the entire update: a control-plane upgrade reaching "ready" again, plus whatever default_node_pool needs on top — a scale, a version change following the cluster, or a full pool replacement, which builds new bare metal before releasing the old. The pool's version change is only started while budget remains; otherwise the next apply picks it up. Default: 30 minutes.`,
				DeleteDescription: `Timeout for the cluster to be fully deleted. Default: 15 minutes.`,
			}),
		},
	}
}

func (r *LksResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := providerpkg.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.client = deps.Client
	r.defaultProject = deps.DefaultProject
}

func (r *LksResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy
	}

	var cfg, plan LksResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Reject lowering kubernetes_version in place. The platform forbids it (422
	// DOWNGRADE_NOT_ALLOWED), but only once the apply reaches the PATCH — an
	// hour of build behind it if the pool has to come up first. Comparing here
	// turns that into a plan error, the same reason the default-pool taint check
	// lives in the plan. Only runs on update (prior state exists).
	if !req.State.Raw.IsNull() {
		var state LksResourceModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
		lksCheckNoDowngrade(state.KubernetesVersion, plan.KubernetesVersion, path.Root("kubernetes_version"), &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	switch {
	case cfg.Project.IsUnknown():
		// Project comes from another resource not created yet; leave it unknown
		// and let a later plan resolve it.
	case !cfg.Project.IsNull() && cfg.Project.ValueString() != "":
		plan.Project = cfg.Project
	case r.defaultProject != "":
		plan.Project = types.StringValue(r.defaultProject)
	default:
		resp.Diagnostics.AddError(
			"Missing project",
			"Set `project` on this resource or define a default in the provider block (provider `latitudesh` { project = \"...\" }).",
		)
		return
	}

	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// An omitted default_node_pool.kubernetes_version follows the cluster's
	// version instead of pinning to whatever it got at create — set explicitly,
	// it stays put. Both live in this one resource, so "the cluster's version"
	// is the plan value right here, no lookup needed: on a version bump the pool
	// plans the same upgrade, which reconcileDefaultNodePool issues right after
	// the control plane. Done last so it wins over the whole-plan Set above.
	r.followClusterVersionInDefaultPool(ctx, cfg, plan, resp)
}

// followClusterVersionInDefaultPool pins default_node_pool.kubernetes_version to
// the cluster's version when the configuration omits it. An explicit value is
// left untouched (the user opted out of tracking). Setting it to a known value
// also removes the "known after apply" the omitted attribute would otherwise
// show, and guarantees the pool is never newer than the control plane, so
// VERSION_SKEW is impossible by construction.
func (r *LksResource) followClusterVersionInDefaultPool(ctx context.Context, cfg, plan LksResourceModel, resp *resource.ModifyPlanResponse) {
	if plan.DefaultNodePool.IsNull() || plan.DefaultNodePool.IsUnknown() {
		return
	}
	if plan.KubernetesVersion.IsNull() || plan.KubernetesVersion.IsUnknown() {
		return
	}

	cfgPool, d := lksDefaultNodePoolFromObject(ctx, cfg.DefaultNodePool)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Explicitly configured version: pinned, not tracked.
	if !cfgPool.KubernetesVersion.IsNull() {
		return
	}

	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx,
		path.Root("default_node_pool").AtName("kubernetes_version"),
		plan.KubernetesVersion)...)
}

// lksCheckNoDowngrade fails the plan when kubernetes_version would move
// backwards. The platform answers a lower version with 422
// DOWNGRADE_NOT_ALLOWED; catching it here is the difference between a plan
// error and a failed apply. An unknown or unparseable version on either side is
// left to the API — a check that cannot compare must not block, and must never
// invent a verdict from a string it does not understand. It backs the cluster's
// control-plane version and can back a pool's the same way.
func lksCheckNoDowngrade(priorVal, plannedVal types.String, attrPath path.Path, diags *diag.Diagnostics) {
	if priorVal.IsNull() || priorVal.IsUnknown() || plannedVal.IsNull() || plannedVal.IsUnknown() {
		return
	}

	prior, err := goversion.NewVersion(priorVal.ValueString())
	if err != nil {
		return
	}
	planned, err := goversion.NewVersion(plannedVal.ValueString())
	if err != nil {
		return
	}

	if planned.LessThan(prior) {
		diags.AddAttributeError(
			attrPath,
			"Kubernetes version downgrade is not allowed",
			fmt.Sprintf("kubernetes_version cannot be lowered in place: the cluster is on %q and the plan sets %q. "+
				"The platform rejects a downgrade (422 DOWNGRADE_NOT_ALLOWED). Set a version equal to or newer than the current one; "+
				"to run an older version, replace the cluster explicitly.",
				priorVal.ValueString(), plannedVal.ValueString()),
		)
	}
}

func (r *LksResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data LksResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var effectiveProject string
	if !data.Project.IsNull() && !data.Project.IsUnknown() && data.Project.ValueString() != "" {
		effectiveProject = data.Project.ValueString()
	} else if r.defaultProject != "" {
		effectiveProject = r.defaultProject
	}
	if effectiveProject == "" {
		resp.Diagnostics.AddError(
			"Missing project",
			"Set `project` on this resource or define a default in the provider block (provider \"latitudesh\" { project = \"...\" }).",
		)
		return
	}
	data.Project = types.StringValue(effectiveProject)

	projectID, err := r.resolveProjectID(ctx, effectiveProject)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to resolve project "+effectiveProject+", got error: "+err.Error())
		return
	}

	networkInput, netDiags := lksNetworkFromObject(ctx, data.Network)
	resp.Diagnostics.Append(netDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createRequest := components.CreateLksCluster{
		Data: components.CreateLksClusterData{
			Type: components.CreateLksClusterTypeLksClusters,
			Attributes: components.CreateLksClusterAttributes{
				Name:              data.Name.ValueString(),
				ProjectID:         projectID,
				Site:              data.Site.ValueString(),
				KubernetesVersion: data.KubernetesVersion.ValueString(),
				Description:       data.Description.ValueStringPointer(),
				Network:           networkInput,
			},
		},
	}

	result, err := r.client.Lks.CreateLksCluster(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create LKS cluster, got error: "+err.Error())
		return
	}

	if result == nil || result.LksCluster == nil || result.LksCluster.Data == nil || result.LksCluster.Data.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get LKS cluster ID from response")
		return
	}

	id := *result.LksCluster.Data.ID
	data.ID = types.StringValue(id)

	// Persist what the POST already told us before the (potentially long)
	// waits, so the cluster is tracked in state even if polling times out;
	// otherwise it leaks as an orphan. The response carries the full record,
	// not just the ID, and Terraform turns whatever is still unknown into null
	// when an apply errors — so recording only the ID is what leaves a failed
	// apply with a state full of nulls for fields the API had already
	// answered. The final state still comes from the read at the end.
	lksApplyCreatedCluster(&data, result.LksCluster.Data.Attributes, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, diags := data.Timeouts.Create(ctx, 30*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// One absolute deadline for every wait in this create. The timeout is a
	// budget for the whole operation — which is what the schema promises — not
	// an allowance each sequential wait starts afresh, which would let the
	// default 30 minutes run for 90 before failing.
	deadline := time.Now().Add(createTimeout)

	// The cluster record has to be queryable before anything can be hung off
	// it: the POST answers before the GET necessarily succeeds.
	r.waitForClusterExists(ctx, id, deadline, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// The default node pool is created here, inside the cluster's own Create,
	// and that is the whole reason it lives in this schema — see the comment on
	// lksDefaultNodePoolModel. Without it the cluster would never converge.
	pool, poolDiags := lksDefaultNodePoolFromObject(ctx, data.DefaultNodePool)
	resp.Diagnostics.Append(poolDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	poolID, poolAttrs := r.lksCreateDefaultNodePool(ctx, id, pool, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		// The cluster exists and is tracked by the write above; the pool is
		// not. The next apply finishes the job instead of orphaning it.
		return
	}
	data.DefaultNodePool = lksDefaultNodePoolObject(ctx, poolID, pool, poolAttrs, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Nodes first, then the control plane: the cluster converges only once the
	// pool has nodes, so this order reports the slow part as the slow part.
	// Neither wait mutates the model, and both objects are already in state
	// above, so a failure here needs no further write to stay recoverable.
	lksWaitForNodesReady(ctx, r.client, id, poolID, pool.NodeCount.ValueInt64(), deadline, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Now valid, unlike the pool-less design: the pool exists, so "ready" is
	// reachable rather than a deadlock.
	r.waitForClusterReady(ctx, id, "", deadline, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readLksInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *LksResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data LksResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readLksInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.IsNull() {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update only ever runs for name, description and kubernetes_version: every
// other input attribute carries RequiresReplace.
func (r *LksResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data LksResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The prior state is read only to tell an in-place control-plane upgrade
	// apart from a name/description edit; see the wait below.
	var state LksResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.ID.ValueString()

	updateRequest := components.UpdateLksCluster{
		Data: components.UpdateLksClusterData{
			Type: components.UpdateLksClusterTypeLksClusters,
			Attributes: &components.UpdateLksClusterAttributes{
				Name: data.Name.ValueStringPointer(),
				// Removing `description` from configuration has to reach the
				// API as an explicit empty string; a nil pointer is omitted and
				// the old value survives, which failed the apply on the
				// read-back. See lksDescriptionForUpdate.
				Description:       lksDescriptionForUpdate(data.Description),
				KubernetesVersion: data.KubernetesVersion.ValueStringPointer(),
			},
		},
	}

	_, err := r.client.Lks.UpdateLksCluster(ctx, id, updateRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to update LKS cluster, got error: "+err.Error())
		return
	}

	updateTimeout, diags := data.Timeouts.Update(ctx, 30*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Shared by the control-plane wait and everything reconcileDefaultNodePool
	// does after it — see the same note in Create.
	deadline := time.Now().Add(updateTimeout)

	// A kubernetes_version change is asynchronous *and* lazy: the platform
	// accepts the PATCH while the cluster is still "ready" on the old version
	// and only flips to "upgrading" a moment later. Waiting on status alone
	// therefore returns on the very first poll and hands Terraform back the
	// pre-upgrade version — "Provider produced inconsistent result after
	// apply", with the old version left in state. So an upgrade waits for the
	// requested version to actually land, not just for "ready" to be true.
	// A name/description-only change is synchronous: no version to wait on,
	// status is already "ready", and this returns immediately.
	wantVersion := ""
	if !data.KubernetesVersion.Equal(state.KubernetesVersion) {
		wantVersion = data.KubernetesVersion.ValueString()
	}

	r.waitForClusterReady(ctx, id, wantVersion, deadline, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	r.reconcileDefaultNodePool(ctx, id, &data, state, deadline, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readLksInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// reconcileDefaultNodePool brings the cluster's own pool in line with the plan.
//
// plan and max_pods_per_node have no update endpoint, so changing either
// replaces the pool — the pool, not the cluster, which is where this improves
// on azurerm_kubernetes_cluster's default_node_pool. The replacement is built
// BEFORE the old one is torn down, so the cluster is never momentarily without
// a pool: it is the same allocate-then-release ordering resource_elastic_ip_bgp
// uses to avoid orphaning.
func (r *LksResource) reconcileDefaultNodePool(ctx context.Context, clusterID string, data *LksResourceModel, state LksResourceModel, deadline time.Time, diags *diag.Diagnostics) {
	planned, d := lksDefaultNodePoolFromObject(ctx, data.DefaultNodePool)
	diags.Append(d...)
	current, d := lksDefaultNodePoolFromObject(ctx, state.DefaultNodePool)
	diags.Append(d...)
	if diags.HasError() {
		return
	}

	oldID := current.ID.ValueString()

	// A planned value that is still unknown is a Computed attribute waiting to
	// be filled from the API, NOT a change. Comparing it directly against state
	// makes every update look like a plan/max_pods_per_node edit and rebuilds
	// the pool — an hour of bare metal for a rename.
	changed := func(plannedVal, currentVal attr.Value) bool {
		if plannedVal.IsUnknown() {
			return false
		}
		return !plannedVal.Equal(currentVal)
	}

	// No pool on record: either the first apply failed after the cluster was
	// created, or it was deleted out of band. Either way, build one.
	replace := oldID == "" ||
		changed(planned.Plan, current.Plan) ||
		changed(planned.MaxPodsPerNode, current.MaxPodsPerNode)

	if replace {
		newID, newAttrs := r.lksCreateDefaultNodePool(ctx, clusterID, planned, diags)
		if diags.HasError() {
			return
		}
		planned.ID = types.StringValue(newID)

		data.DefaultNodePool = lksDefaultNodePoolObject(ctx, newID, planned, newAttrs, diags)
		if diags.HasError() {
			return
		}

		lksWaitForNodesReady(ctx, r.client, clusterID, newID, planned.NodeCount.ValueInt64(), deadline, diags)
		if diags.HasError() {
			return
		}

		if oldID != "" {
			if _, err := r.client.Lks.DeleteLksNodePool(ctx, clusterID, oldID); err != nil && !lksClusterNotFound(err) {
				diags.AddError("Client Error", "Replaced the default node pool but could not remove the old one ("+oldID+"): "+err.Error())
				return
			}
			lksWaitForNodePoolDeleted(ctx, r.client, clusterID, oldID, deadline, diags)
		}
		return
	}

	// The default pool goes through the same split as the standalone resource:
	// a scale and an upgrade may not share a PATCH (422), and the version is
	// always known here because the attribute is Computed.
	lksPatchNodePool(ctx, r.client, lksNodePoolPatch{
		ClusterID:      clusterID,
		PoolID:         oldID,
		Name:           planned.Name,
		Description:    planned.Description,
		Labels:         planned.Labels,
		Taints:         planned.Taints,
		Count:          planned.NodeCount,
		CountChanged:   !planned.NodeCount.Equal(current.NodeCount),
		Version:        planned.KubernetesVersion,
		VersionChanged: changed(planned.KubernetesVersion, current.KubernetesVersion),
		Deadline:       deadline,
	}, diags)
	if diags.HasError() {
		return
	}

	planned.ID = types.StringValue(oldID)
	obj, objDiags := types.ObjectValueFrom(ctx, lksDefaultNodePoolAttrTypes, planned)
	diags.Append(objDiags...)
	if diags.HasError() {
		return
	}
	data.DefaultNodePool = obj
}

func (r *LksResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data LksResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.ID.ValueString()
	if id == "" {
		return
	}

	_, err := r.client.Lks.DeleteLksCluster(ctx, id)
	if err != nil {
		if lksClusterNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to delete LKS cluster, got error: "+err.Error())
		return
	}

	deleteTimeout, diags := data.Timeouts.Delete(ctx, 15*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.waitForClusterDeleted(ctx, id, time.Now().Add(deleteTimeout), &resp.Diagnostics)
}

func (r *LksResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var data LksResourceModel
	data.ID = types.StringValue(req.ID)

	// No timeouts block is set on import, so give the field an explicitly
	// typed null. The zero value of timeouts.Value is a null object with no
	// attribute types, which fails state conversion ("Object[]" vs
	// "Object[create:String, update:String, delete:String]").
	data.Timeouts = timeouts.Value{
		Object: types.ObjectNull(map[string]attr.Type{
			"create": types.StringType,
			"update": types.StringType,
			"delete": types.StringType,
		}),
	}

	// Nothing marks a pool as "the default" server-side, so import adopts the
	// first one the cluster reports and leaves the rest to be imported as
	// latitudesh_lks_node_pool. Check the result before the next apply.
	if adopted := r.adoptFirstNodePool(ctx, req.ID, &resp.Diagnostics); adopted != "" {
		obj, objDiags := types.ObjectValueFrom(ctx, lksDefaultNodePoolAttrTypes, lksDefaultNodePoolModel{
			ID:                types.StringValue(adopted),
			Plan:              types.StringNull(),
			NodeCount:         types.Int64Null(),
			Name:              types.StringNull(),
			Description:       types.StringNull(),
			KubernetesVersion: types.StringNull(),
			MaxPodsPerNode:    types.Int64Null(),
			Labels:            types.MapNull(types.StringType),
			Taints:            types.SetNull(lksTaintObjectType),
			Type:              types.StringNull(),
			Mode:              types.StringNull(),
			Status:            types.StringNull(),
			Message:           types.StringNull(),
			Reason:            types.StringNull(),
			ReadyNodes:        types.Int64Null(),
			PlatformVersion:   types.StringNull(),
			CreatedAt:         types.StringNull(),
			UpdatedAt:         types.StringNull(),
		})
		resp.Diagnostics.Append(objDiags...)
		data.DefaultNodePool = obj
	}
	if resp.Diagnostics.HasError() {
		return
	}

	r.readLksInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.IsNull() {
		resp.Diagnostics.AddError("Not Found", "LKS cluster "+req.ID+" not found")
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// The platform's own notion of "an operation is running", lifted from the
// API's LKSService::MutationAdmission — the same set it uses to answer 422
// "operation in progress" on a mutation. Waiting for a specific terminal
// string instead ("ready") is a guess, and a brittle one: any state the
// platform adds, or any object that settles into something else, makes the
// poller spin until its timeout with the resource perfectly healthy.
var lksInProgressStates = map[string]bool{
	"provisioning": true,
	"updating":     true,
	"scaling":      true,
	"upgrading":    true,
}

// States a wait can never succeed from. Failing fast here turns an hour of
// polling into an immediate, legible error.
var lksUnreachableStates = map[string]bool{
	"paused":   true,
	"deleting": true,
	"deleted":  true,
}

// lksSettled reports whether a status means the platform has finished working
// on the object, and whether waiting any longer is pointless.
func lksSettled(status string) (done bool, hopeless bool) {
	switch {
	case status == "":
		return false, false
	case lksUnreachableStates[status]:
		return false, true
	case lksInProgressStates[status]:
		return false, false
	default:
		return true, false
	}
}

// waitForClusterExists polls GET /lks/clusters/{id} until the cluster is
// readable, which is all Create can wait for — see the comment there. The POST
// answers before the record is necessarily queryable, so a 404 (or a 5xx) in
// the first moments is transient, exactly as it is for the readiness poll.
//
// The create's deadline is the ceiling, but this normally returns on the
// first or second poll: it is waiting for a record to appear, not for hardware.
func (r *LksResource) waitForClusterExists(ctx context.Context, id string, deadline time.Time, diags *diag.Diagnostics) {
	const maxConsecutiveErrors = 5
	pollInterval := lksReadyPollInterval

	consecutiveErrors := 0

	for time.Now().Before(deadline) {
		result, err := r.client.Lks.GetLksCluster(ctx, id)
		if err != nil {
			if !lksRetryableDuringPoll(err) {
				diags.AddError("Client Error", "Unable to read the LKS cluster just created: "+err.Error())
				return
			}
			consecutiveErrors++
			if consecutiveErrors >= maxConsecutiveErrors {
				diags.AddError("Client Error", fmt.Sprintf("Unable to read the LKS cluster just created after %d consecutive attempts, last error: %s", consecutiveErrors, err.Error()))
				return
			}
		} else if result != nil && result.LksCluster != nil && result.LksCluster.Data != nil {
			return
		}

		select {
		case <-ctx.Done():
			diags.AddError("Client Error", "Context cancelled while waiting for the LKS cluster to appear: "+ctx.Err().Error())
			return
		case <-time.After(pollInterval):
		}
	}

	diags.AddError(
		"Timeout waiting for LKS cluster",
		fmt.Sprintf("LKS cluster %q was created but never became readable before the create timeout expired.", id),
	)
}

// waitForClusterReady polls GET /lks/clusters/{id} until status is "ready".
// No terminal failure status is documented for this operation (unlike VM
// backups' "Failed"), so an unrecognized or stuck status is only ever a
// timeout, never a fast-fail.
//
// wantVersion, when non-empty, additionally requires kubernetes_version to
// have reached that value before the wait is satisfied. Create passes ""
// (whatever version comes back is the one the cluster was built with); an
// in-place upgrade passes the requested version, because the platform leaves
// the cluster "ready" on the old version for a moment after accepting the
// PATCH and a status-only check would return on that pre-upgrade read.
//
// deadline is absolute and shared with every other wait of the same Create or
// Update, so the configured timeout bounds the operation, not each wait.
func (r *LksResource) waitForClusterReady(ctx context.Context, id, wantVersion string, deadline time.Time, diags *diag.Diagnostics) {
	const maxConsecutiveErrors = 5
	pollInterval := lksReadyPollInterval

	lastStatus := ""
	lastVersion := ""
	consecutiveErrors := 0

	for time.Now().Before(deadline) {
		result, err := r.client.Lks.GetLksCluster(ctx, id)
		if err != nil {
			if !lksRetryableDuringPoll(err) {
				diags.AddError("Client Error", "Unable to check LKS cluster status: "+err.Error())
				return
			}
			consecutiveErrors++
			if consecutiveErrors >= maxConsecutiveErrors {
				diags.AddError("Client Error", fmt.Sprintf("Unable to check LKS cluster status after %d consecutive attempts, last error: %s", consecutiveErrors, err.Error()))
				return
			}
			select {
			case <-ctx.Done():
				diags.AddError("Client Error", "Context cancelled while waiting for LKS cluster to be ready: "+ctx.Err().Error())
				return
			case <-time.After(pollInterval):
				continue
			}
		}
		consecutiveErrors = 0

		if result != nil && result.LksCluster != nil && result.LksCluster.Data != nil && result.LksCluster.Data.Attributes != nil {
			attrs := result.LksCluster.Data.Attributes
			if version := attrs.KubernetesVersion; version != nil {
				lastVersion = *version
			}
			if status := attrs.Status; status != nil {
				lastStatus = *status

				done, hopeless := lksSettled(lastStatus)
				if hopeless {
					diags.AddError(
						"LKS cluster cannot become ready",
						fmt.Sprintf("LKS cluster %q is %q, which no amount of waiting will change.", id, lastStatus),
					)
					return
				}
				if done && (wantVersion == "" || lastVersion == wantVersion) {
					return
				}
			}
		}

		select {
		case <-ctx.Done():
			diags.AddError("Client Error", "Context cancelled while waiting for LKS cluster to be ready: "+ctx.Err().Error())
			return
		case <-time.After(pollInterval):
		}
	}

	if wantVersion != "" {
		diags.AddError(
			"Timeout waiting for LKS cluster upgrade",
			fmt.Sprintf("LKS cluster %q did not reach status \"ready\" on kubernetes_version %q before the timeout expired (last status: %q, last version: %q).", id, wantVersion, lastStatus, lastVersion),
		)
		return
	}

	diags.AddError(
		"Timeout waiting for LKS cluster",
		fmt.Sprintf("LKS cluster %q did not reach status \"ready\" before the timeout expired (last status: %q).", id, lastStatus),
	)
}

// waitForClusterDeleted polls until the cluster 404s or reports status
// "deleted". Which of the two actually happens is not confirmed live (see
// handoff); both are treated as terminal so destroy does not spin until the
// timeout either way.
func (r *LksResource) waitForClusterDeleted(ctx context.Context, id string, deadline time.Time, diags *diag.Diagnostics) {
	const maxConsecutiveErrors = 5
	pollInterval := lksDeletePollInterval

	consecutiveErrors := 0

	for time.Now().Before(deadline) {
		result, err := r.client.Lks.GetLksCluster(ctx, id)
		if err != nil {
			if lksClusterNotFound(err) {
				return
			}
			if !lksRetryableDuringPoll(err) {
				diags.AddError("Client Error", "Unable to check LKS cluster deletion: "+err.Error())
				return
			}
			consecutiveErrors++
			if consecutiveErrors >= maxConsecutiveErrors {
				diags.AddError("Client Error", fmt.Sprintf("Unable to check LKS cluster deletion after %d consecutive attempts, last error: %s", consecutiveErrors, err.Error()))
				return
			}
		} else {
			consecutiveErrors = 0
			if result != nil && result.LksCluster != nil && result.LksCluster.Data != nil && result.LksCluster.Data.Attributes != nil {
				if status := result.LksCluster.Data.Attributes.Status; status != nil && *status == "deleted" {
					return
				}
			}
		}

		select {
		case <-ctx.Done():
			diags.AddError("Client Error", "Context cancelled while waiting for LKS cluster deletion: "+ctx.Err().Error())
			return
		case <-time.After(pollInterval):
		}
	}

	diags.AddError(
		"Timeout waiting for LKS cluster deletion",
		fmt.Sprintf("LKS cluster %q was not removed or marked deleted before the delete timeout expired.", id),
	)
}

// adoptFirstNodePool returns the ID of the cluster's first node pool, or "" if
// it has none — which for an existing cluster means it never converged.
func (r *LksResource) adoptFirstNodePool(ctx context.Context, clusterID string, diags *diag.Diagnostics) string {
	res, err := r.client.Lks.ListLksNodePools(ctx, clusterID)
	if err != nil {
		diags.AddError("Client Error", "Unable to list node pools while importing LKS cluster "+clusterID+": "+err.Error())
		return ""
	}
	if res == nil || res.LksNodePools == nil || len(res.LksNodePools.Data) == 0 {
		diags.AddWarning(
			"Imported LKS cluster has no node pool",
			"Cluster "+clusterID+" reports no node pool, so it cannot have finished provisioning. "+
				"`default_node_pool` is required, so the next apply will create one.",
		)
		return ""
	}
	if id := res.LksNodePools.Data[0].ID; id != nil {
		return *id
	}
	return ""
}

// lksApplyCreatedCluster records what the create response already knows about
// the cluster: the POST answers with the full record, and every computed
// attribute left unknown until the waits finish becomes null in state if the
// apply errors out before them. It fills the same attributes readLksInto
// does, under the same rule — `site` and `network` carry RequiresReplace and
// are only backfilled when the configuration left them open, and `project`
// keeps the configured selector rather than the ID the API echoes back.
func lksApplyCreatedCluster(data *LksResourceModel, attrs *components.LksClusterDataAttributes, diags *diag.Diagnostics) {
	fields, mapDiags := mapLksAttributes(attrs)
	diags.Append(mapDiags...)
	if diags.HasError() {
		return
	}

	if data.Site.IsNull() || data.Site.IsUnknown() {
		data.Site = fields.Site
	}
	if data.Network.IsNull() || data.Network.IsUnknown() {
		data.Network = fields.Network
	}

	data.Status = fields.Status
	data.Message = fields.Message
	data.Reason = fields.Reason
	data.ControlPlaneEndpoint = fields.ControlPlaneEndpoint
	data.KubeconfigURL = fields.KubeconfigURL
	data.PlatformVersion = fields.PlatformVersion
	data.CreatedAt = fields.CreatedAt
	data.UpdatedAt = fields.UpdatedAt
}

// readLksInto issues a Get for data.ID and refreshes every attribute.
// `project`, `site` and `network` are only filled in when not already set
// (i.e. during import): they carry RequiresReplace with no update endpoint
// to drift against, so echoing them back over a configured value would
// yield a spurious "inconsistent result after apply" or a needless
// replacement. `name`, `kubernetes_version` and `description` are
// updatable, so the API is always the source of truth for them.
func (r *LksResource) readLksInto(ctx context.Context, data *LksResourceModel, diags *diag.Diagnostics) {
	id := data.ID.ValueString()
	if id == "" {
		diags.AddError("Invalid ID", "LKS cluster ID is empty")
		return
	}

	result, err := r.client.Lks.GetLksCluster(ctx, id)
	if err != nil {
		if lksClusterNotFound(err) {
			data.ID = types.StringNull()
			return
		}
		diags.AddError("Client Error", "Unable to read LKS cluster, got error: "+err.Error())
		return
	}

	if result == nil || result.LksCluster == nil || result.LksCluster.Data == nil {
		data.ID = types.StringNull()
		return
	}

	obj := result.LksCluster.Data
	if obj.ID != nil {
		data.ID = types.StringValue(*obj.ID)
	}

	fields, mapDiags := mapLksAttributes(obj.Attributes)
	diags.Append(mapDiags...)

	if data.Project.IsNull() || data.Project.IsUnknown() {
		data.Project = fields.Project
	}
	if data.Site.IsNull() || data.Site.IsUnknown() {
		data.Site = fields.Site
	}
	if data.Network.IsNull() || data.Network.IsUnknown() {
		data.Network = fields.Network
	}

	data.Name = fields.Name
	data.KubernetesVersion = fields.KubernetesVersion

	// `description` is Optional and not Computed, so the API is the source of
	// truth for it exactly as it is for name and kubernetes_version. Assigning
	// it only when the response carried a non-nil value pinned state to the
	// last value Terraform had written: a description cleared (or changed to
	// null) outside Terraform never surfaced as drift, and the stale value
	// survived every refresh. The one real ambiguity is that an unset
	// description may come back as either JSON null or "", which mean the same
	// thing for a null-able Optional attribute — so "" collapses to null only
	// when state already holds null, never when it holds a value to drift away
	// from, and an explicitly configured "" still round-trips.
	data.Description = lksDescriptionFromAPI(data.Description, fields.Description)

	r.lksReadDefaultNodePoolInto(ctx, id, data, diags)

	data.Status = fields.Status
	data.Message = fields.Message
	data.Reason = fields.Reason
	data.ControlPlaneEndpoint = fields.ControlPlaneEndpoint
	data.KubeconfigURL = fields.KubeconfigURL
	data.PlatformVersion = fields.PlatformVersion
	data.CreatedAt = fields.CreatedAt
	data.UpdatedAt = fields.UpdatedAt
}

// resolveProjectID turns the `project` selector into the ID
// CreateLksClusterAttributes.ProjectID requires. Its doc comment ("The
// project ID") does not confirm slug acceptance the way the list request's
// does ("Project id_hash or slug"), so this resolves conservatively before
// every create, the same way resource_public_network.go does for its own
// create endpoint. The configured selector itself is never rewritten in
// state. The list filter used by the plural data source does document slug
// support, so no resolution is needed there.
func (r *LksResource) resolveProjectID(ctx context.Context, selector string) (string, error) {
	if strings.HasPrefix(selector, "proj_") {
		return selector, nil
	}

	res, err := r.client.Projects.GetProject(ctx, selector)
	if err != nil {
		return "", err
	}
	if res == nil || res.Object == nil || res.Object.Data == nil || res.Object.Data.ID == nil || *res.Object.Data.ID == "" {
		return "", fmt.Errorf("project %q not found", selector)
	}
	return *res.Object.Data.ID, nil
}

// lksNotFoundMarkers are the values the LKS endpoints use to say "gone".
//
// JSON:API specifies `status` as the HTTP status code as a string, and the
// SDK's own examples use "404" — but the live API renders it in snake_case
// ("not_found"), with the code alongside it ("NOT_FOUND"). Matching only "404"
// meant a destroy polled a cluster that was already deleted, got the
// not-found it was waiting for, and reported it as an unexpected client error
// (observed live 2026-09-18). Both spellings count, on either field.
var lksNotFoundMarkers = map[string]bool{
	"404":       true,
	"not_found": true,
	"notfound":  true,
}

// lksClusterNotFound reports whether err says the object is gone. It backs
// every LKS Read, Delete and poller, for clusters and node pools alike, so a
// spelling it fails to recognize turns "already deleted" into a failed apply.
//
// Only not-found counts: a 403 or a 409 is a real error and must not be
// swallowed as "gone".
func lksClusterNotFound(err error) bool {
	var apiErr *components.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}

	var errObj *components.ErrorObject
	if errors.As(err, &errObj) {
		for _, e := range errObj.Errors {
			if e.Status != nil && lksNotFoundMarkers[strings.ToLower(strings.TrimSpace(*e.Status))] {
				return true
			}
			if e.Code != nil && lksNotFoundMarkers[strings.ToLower(strings.TrimSpace(*e.Code))] {
				return true
			}
		}
	}

	return false
}

// lksRetryableDuringPoll reports whether err is worth retrying while polling
// GetLksCluster: a 404 right after create/delete (not yet queryable, or
// already gone) or a 5xx (transient). Any other status (403, 409, 422, ...)
// will not resolve by waiting, so the caller fails immediately instead of
// burning the full timeout budget.
func lksRetryableDuringPoll(err error) bool {
	if lksClusterNotFound(err) {
		return true
	}

	var apiErr *components.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500
	}

	// Only the numeric spelling is handled here. The live API is known to use
	// snake_case for not-found (see lksNotFoundMarkers); whether it does the
	// same for 5xx has not been observed, and inventing a list of names for a
	// shape nobody has seen is how the wrong thing gets shipped confidently.
	// The cost of missing one is a poll that fails fast instead of retrying,
	// which is visible rather than silent.
	var errObj *components.ErrorObject
	if errors.As(err, &errObj) {
		for _, e := range errObj.Errors {
			if e.Status != nil && len(*e.Status) == 3 && (*e.Status)[0] == '5' {
				return true
			}
		}
		return false
	}

	return false
}
