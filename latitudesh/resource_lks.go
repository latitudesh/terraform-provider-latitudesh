package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

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

func (r *LksResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lks"
}

func (r *LksResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "LKS (Latitude Kubernetes Service) cluster resource. Provisions the control plane only; add worker capacity with a separate node pool (not yet supported by this provider — see the provider changelog).",
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
				MarkdownDescription: "Kubernetes patch version, exactly as listed by `GET /lks/available_versions`. Setting a newer patch consents to a control-plane upgrade in place; the platform rejects a lower one (422 `DOWNGRADE_NOT_ALLOWED`).",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Optional customer description.",
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
				MarkdownDescription: "Cluster lifecycle status. Open enum sourced from the platform controller; values in use today are `provisioning`, `ready`, `updating`, `scaling`, `upgrading`, `paused`, `deleting` and `deleted`, and new ones may appear without notice.",
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
			"control_plane_endpoint": schema.StringAttribute{
				MarkdownDescription: "Kubernetes API server endpoint.",
				Computed:            true,
			},
			"kubeconfig_url": schema.StringAttribute{
				MarkdownDescription: "URL to fetch the cluster kubeconfig from once the control plane is ready. It only resolves once `status` is `ready`; `GET /lks/clusters/{id}/kubeconfig` answers 409 `NOT_READY` until then.",
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
				CreateDescription: `Timeout for the cluster to reach status "ready". Default: 30 minutes. Example: "45m", "1h"`,
				UpdateDescription: `Timeout for a control-plane upgrade to reach status "ready" again. Default: 30 minutes.`,
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

	if cfg.Project.IsUnknown() {
		return
	}

	if !cfg.Project.IsNull() && !cfg.Project.IsUnknown() && cfg.Project.ValueString() != "" {
		plan.Project = cfg.Project
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}

	if r.defaultProject != "" {
		plan.Project = types.StringValue(r.defaultProject)
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}

	resp.Diagnostics.AddError(
		"Missing project",
		"Set `project` on this resource or define a default in the provider block (provider `latitudesh` { project = \"...\" }).",
	)
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

	// Persist the ID before the (potentially long) wait so the cluster is
	// tracked in state even if polling times out; otherwise it leaks as an
	// orphan.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, diags := data.Timeouts.Create(ctx, 30*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.waitForClusterReady(ctx, id, createTimeout, &resp.Diagnostics)
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

	id := data.ID.ValueString()

	updateRequest := components.UpdateLksCluster{
		Data: components.UpdateLksClusterData{
			Type: components.UpdateLksClusterTypeLksClusters,
			Attributes: &components.UpdateLksClusterAttributes{
				Name:              data.Name.ValueStringPointer(),
				Description:       data.Description.ValueStringPointer(),
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

	// A kubernetes_version change moves the cluster to "upgrading"; a
	// name/description-only change is synchronous, so this returns
	// immediately if status is already "ready".
	r.waitForClusterReady(ctx, id, updateTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readLksInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
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

	r.waitForClusterDeleted(ctx, id, deleteTimeout, &resp.Diagnostics)
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

// waitForClusterReady polls GET /lks/clusters/{id} until status is "ready".
// No terminal failure status is documented for this operation (unlike VM
// backups' "Failed"), so an unrecognized or stuck status is only ever a
// timeout, never a fast-fail.
func (r *LksResource) waitForClusterReady(ctx context.Context, id string, timeout time.Duration, diags *diag.Diagnostics) {
	const maxConsecutiveErrors = 5
	pollInterval := lksReadyPollInterval

	deadline := time.Now().Add(timeout)
	lastStatus := ""
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
			if status := result.LksCluster.Data.Attributes.Status; status != nil {
				lastStatus = *status
				if *status == "ready" {
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

	diags.AddError(
		"Timeout waiting for LKS cluster",
		fmt.Sprintf("LKS cluster %q did not reach status \"ready\" within %s (last status: %q).", id, timeout, lastStatus),
	)
}

// waitForClusterDeleted polls until the cluster 404s or reports status
// "deleted". Which of the two actually happens is not confirmed live (see
// handoff); both are treated as terminal so destroy does not spin until the
// timeout either way.
func (r *LksResource) waitForClusterDeleted(ctx context.Context, id string, timeout time.Duration, diags *diag.Diagnostics) {
	const maxConsecutiveErrors = 5
	pollInterval := lksDeletePollInterval

	deadline := time.Now().Add(timeout)
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
		fmt.Sprintf("LKS cluster %q was not removed or marked deleted after %s.", id, timeout),
	)
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
	if obj.Attributes != nil && obj.Attributes.Description != nil {
		data.Description = fields.Description
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

// lksClusterNotFound reports whether err is a 404 from the LKS cluster
// endpoints. GetLksCluster and DeleteLksCluster both declare typed
// 403/404(/409/422)/502 responses, so the SDK returns a
// *components.ErrorObject (JSON:API errors, status "404") rather than the
// generic *components.APIError for those; both shapes must be recognized,
// and only a 404 counts — a 403 or 409 is a real error.
func lksClusterNotFound(err error) bool {
	var apiErr *components.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}

	var errObj *components.ErrorObject
	if errors.As(err, &errObj) {
		for _, e := range errObj.Errors {
			if e.Status != nil && *e.Status == "404" {
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
