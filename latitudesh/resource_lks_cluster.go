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
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
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

var _ resource.Resource = &LksClusterResource{}
var _ resource.ResourceWithImportState = &LksClusterResource{}
var _ resource.ResourceWithModifyPlan = &LksClusterResource{}

// Poll intervals for the async create/update/delete waits. Declared as vars
// (not consts) so tests can shorten them; production keeps the defaults.
var (
	lksReadyPollInterval  = 10 * time.Second
	lksDeletePollInterval = 5 * time.Second
)

func NewLksClusterResource() resource.Resource {
	return &LksClusterResource{}
}

type LksClusterResource struct {
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

type LksClusterResourceModel struct {
	ID                   types.String   `tfsdk:"id"`
	Project              types.String   `tfsdk:"project"`
	Name                 types.String   `tfsdk:"name"`
	Site                 types.String   `tfsdk:"site"`
	KubernetesVersion    types.String   `tfsdk:"kubernetes_version"`
	Description          types.String   `tfsdk:"description"`
	Network              types.Object   `tfsdk:"network"`
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

func (r *LksClusterResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lks_cluster"
}

func (r *LksClusterResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "LKS (Latitude Kubernetes Service) cluster resource. Provisions the control plane only; worker capacity is added with `latitudesh_lks_node_pool`.\n\n" +
			"**A cluster needs at least one node pool to become `ready`.** Without one, the platform's own components (CNI, CoreDNS) have nowhere to run, so `status` stays `provisioning` with `reason` `WaitingForNodePool`. Create therefore waits for the control plane, not for `ready`: it returns once the platform reports `WaitingForNodePool`, which is the point where a node pool can join. The create of that pool then waits for the cluster to reach `ready`.",
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
			"status": schema.StringAttribute{
				MarkdownDescription: "Cluster lifecycle status. Open enum sourced from the platform controller; values in use today are `provisioning`, `ready`, `updating`, `scaling`, `upgrading`, `paused`, `deleting` and `deleted`, and new ones may appear without notice.\n\n" +
					"Right after a create this reads `provisioning` with `reason` `WaitingForNodePool`: the control plane is up, and the cluster becomes `ready` only once a `latitudesh_lks_node_pool` has nodes and the platform has finished installing its components on them. This resource's create cannot wait for that, because no pool can be created before the cluster exists; the pool's create does, and this attribute catches up on the next refresh.",
				Computed: true,
			},
			"message": schema.StringAttribute{
				MarkdownDescription: "Human-readable detail behind the current `status`.",
				Computed:            true,
			},
			"reason": schema.StringAttribute{
				MarkdownDescription: "Machine-readable status reason (open enum). `WaitingForNodePool` means the control plane is up and the cluster has no ready node yet; `FinishingSetup` means a node has joined and the platform is still installing its components.",
				Computed:            true,
			},
			"control_plane_endpoint": schema.StringAttribute{
				MarkdownDescription: "Kubernetes API server endpoint.",
				Computed:            true,
			},
			"kubeconfig_url": schema.StringAttribute{
				MarkdownDescription: "URL to fetch the cluster kubeconfig from. The platform publishes the kubeconfig only once `status` is `ready`, which takes a node pool, so this is null right after a create; the next refresh after the first pool is up fills it. Until then `GET /lks/clusters/{id}/kubeconfig` answers 409 `NOT_READY`.",
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
				CreateDescription: `Budget for the entire create: the cluster record becoming readable and the control plane coming up, which the platform signals with reason "WaitingForNodePool". Node pools are separate resources with their own timeouts. Default: 30 minutes. Example: "45m", "1h"`,
				UpdateDescription: `Budget for an update: a control-plane upgrade reaching "ready" again on the new kubernetes_version. Node pools upgrade separately. Default: 30 minutes.`,
				DeleteDescription: `Timeout for the cluster to be fully deleted. Default: 15 minutes.`,
			}),
		},
	}
}

func (r *LksClusterResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *LksClusterResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy
	}

	var cfg, plan LksClusterResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Reject lowering kubernetes_version in place. The platform forbids it (422
	// DOWNGRADE_NOT_ALLOWED), but only once the apply reaches the PATCH.
	// Comparing here turns that into a plan error. Only runs on update (prior
	// state exists).
	if !req.State.Raw.IsNull() {
		var state LksClusterResourceModel
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

func (r *LksClusterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data LksClusterResourceModel
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
	// an allowance each sequential wait starts afresh.
	deadline := time.Now().Add(createTimeout)

	// The control plane, not "ready": a cluster becomes ready only once a node
	// pool has nodes, and no latitudesh_lks_node_pool can be created until this
	// Create returns, so waiting for ready here would wait on itself. The
	// platform says when the control plane is up and a pool is all that is
	// missing; that is the point this returns at. The POST answers before the
	// GET necessarily succeeds, which the poller absorbs as a transient 404.
	r.waitForControlPlane(ctx, id, deadline, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readLksInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *LksClusterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data LksClusterResourceModel
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
func (r *LksClusterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data LksClusterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The prior state is read only to tell an in-place control-plane upgrade
	// apart from a name/description edit; see the wait below.
	var state LksClusterResourceModel
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

	r.readLksInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *LksClusterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data LksClusterResourceModel
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

func (r *LksClusterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var data LksClusterResourceModel
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

// lksControlPlaneUpReasons are the reasons the platform reports on a cluster
// whose control plane is up but which is not ready yet, because readiness
// waits on node pools. They are the operator's client-facing reason contract
// (lks-platform internal/status/customer_message.go), which the API passes
// through verbatim: WaitingForNodePool is a control plane with no ready node,
// FinishingSetup a node that has joined while the platform's own components
// are still installing on it. The operator reports either one only after the
// network, infrastructure and control-plane gates have passed, so both mean a
// node pool can join.
var lksControlPlaneUpReasons = map[string]bool{
	"WaitingForNodePool": true,
	"FinishingSetup":     true,
}

// lksClusterObservation is what one poll of GET /lks/clusters/{id} reported.
type lksClusterObservation struct {
	status  string
	reason  string
	version string
}

// lksPollCluster polls GET /lks/clusters/{id} until reached accepts what the
// platform reports, and returns the last observation. timedOut is true only
// when the deadline passed first; every other way the poll can end — a status
// no wait can succeed from, a non-retryable error, a cancelled context — is
// already in diags.
//
// No terminal failure status is documented for this operation (unlike VM
// backups' "Failed"), so an unrecognized or stuck status is only ever a
// timeout, never a fast-fail.
//
// deadline is absolute and shared with every other wait of the same Create or
// Update, so the configured timeout bounds the operation, not each wait.
//
// A free function rather than a method because latitudesh_lks_node_pool runs it
// too: see lksWaitForClusterToSettle.
func lksPollCluster(ctx context.Context, client *latitudeshgosdk.Latitudesh, id string, deadline time.Time, diags *diag.Diagnostics, reached func(lksClusterObservation) bool) (last lksClusterObservation, timedOut bool) {
	const maxConsecutiveErrors = 5
	pollInterval := lksReadyPollInterval

	consecutiveErrors := 0

	for time.Now().Before(deadline) {
		result, err := client.Lks.GetLksCluster(ctx, id)
		if err != nil {
			if !lksRetryableDuringPoll(err) {
				diags.AddError("Client Error", "Unable to check LKS cluster status: "+err.Error())
				return last, false
			}
			consecutiveErrors++
			if consecutiveErrors >= maxConsecutiveErrors {
				diags.AddError("Client Error", fmt.Sprintf("Unable to check LKS cluster status after %d consecutive attempts, last error: %s", consecutiveErrors, err.Error()))
				return last, false
			}
		} else {
			consecutiveErrors = 0

			if result != nil && result.LksCluster != nil && result.LksCluster.Data != nil && result.LksCluster.Data.Attributes != nil {
				attrs := result.LksCluster.Data.Attributes
				if version := attrs.KubernetesVersion; version != nil {
					last.version = *version
				}
				last.reason = ""
				if reason := attrs.Reason; reason != nil {
					last.reason = *reason
				}
				if status := attrs.Status; status != nil {
					last.status = *status

					if _, hopeless := lksSettled(last.status); hopeless {
						diags.AddError(
							"LKS cluster cannot become ready",
							fmt.Sprintf("LKS cluster %q is %q, which no amount of waiting will change.", id, last.status),
						)
						return last, false
					}
					if reached(last) {
						return last, false
					}
				}
			}
		}

		select {
		case <-ctx.Done():
			diags.AddError("Client Error", "Context cancelled while waiting for the LKS cluster: "+ctx.Err().Error())
			return last, false
		case <-time.After(pollInterval):
		}
	}

	return last, true
}

// waitForControlPlane polls until the control plane is up: the cluster is
// settled (ready, or any state outside the in-progress set), or it reports one
// of lksControlPlaneUpReasons. It is what Create waits for, since "ready" needs
// a node pool that cannot exist yet — see the comment there.
func (r *LksClusterResource) waitForControlPlane(ctx context.Context, id string, deadline time.Time, diags *diag.Diagnostics) {
	last, timedOut := lksPollCluster(ctx, r.client, id, deadline, diags, func(o lksClusterObservation) bool {
		if done, _ := lksSettled(o.status); done {
			return true
		}
		return lksControlPlaneUpReasons[o.reason]
	})
	if !timedOut {
		return
	}

	diags.AddError(
		"Timeout waiting for LKS control plane",
		fmt.Sprintf("LKS cluster %q was created, but its control plane did not come up before the create timeout expired (last status: %q, last reason: %q). "+
			"A control plane that is up reports reason \"WaitingForNodePool\" until a node pool joins.", id, last.status, last.reason),
	)
}

// waitForClusterReady polls until the cluster is settled — status "ready", or
// any state outside the in-progress set.
//
// wantVersion, when non-empty, additionally requires kubernetes_version to
// have reached that value before the wait is satisfied. An in-place upgrade
// passes the requested version, because the platform leaves the cluster
// "ready" on the old version for a moment after accepting the PATCH and a
// status-only check would return on that pre-upgrade read.
func (r *LksClusterResource) waitForClusterReady(ctx context.Context, id, wantVersion string, deadline time.Time, diags *diag.Diagnostics) {
	last, timedOut := lksPollCluster(ctx, r.client, id, deadline, diags, func(o lksClusterObservation) bool {
		done, _ := lksSettled(o.status)
		return done && (wantVersion == "" || o.version == wantVersion)
	})
	if !timedOut {
		return
	}

	if wantVersion != "" {
		diags.AddError(
			"Timeout waiting for LKS cluster upgrade",
			fmt.Sprintf("LKS cluster %q did not reach status \"ready\" on kubernetes_version %q before the timeout expired (last status: %q, last version: %q).", id, wantVersion, last.status, last.version),
		)
		return
	}

	diags.AddError(
		"Timeout waiting for LKS cluster",
		fmt.Sprintf("LKS cluster %q did not reach status \"ready\" before the timeout expired (last status: %q, last reason: %q).", id, last.status, last.reason),
	)
}

// waitForClusterDeleted polls until the cluster 404s or reports status
// "deleted". Which of the two actually happens is not confirmed live (see
// handoff); both are treated as terminal so destroy does not spin until the
// timeout either way.
func (r *LksClusterResource) waitForClusterDeleted(ctx context.Context, id string, deadline time.Time, diags *diag.Diagnostics) {
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

// lksApplyCreatedCluster records what the create response already knows about
// the cluster: the POST answers with the full record, and every computed
// attribute left unknown until the waits finish becomes null in state if the
// apply errors out before them. It fills the same attributes readLksInto
// does, under the same rule — `site` and `network` carry RequiresReplace and
// are only backfilled when the configuration left them open, and `project`
// keeps the configured selector rather than the ID the API echoes back.
func lksApplyCreatedCluster(data *LksClusterResourceModel, attrs *components.LksClusterDataAttributes, diags *diag.Diagnostics) {
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
func (r *LksClusterResource) readLksInto(ctx context.Context, data *LksClusterResourceModel, diags *diag.Diagnostics) {
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
func (r *LksClusterResource) resolveProjectID(ctx context.Context, selector string) (string, error) {
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
