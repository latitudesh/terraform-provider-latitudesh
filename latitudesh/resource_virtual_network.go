package latitudesh

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	"github.com/latitudesh/terraform-provider-latitudesh/v2/internal/planmodifiers"
	providerpkg "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var _ resource.Resource = &VirtualNetworkResource{}
var _ resource.ResourceWithImportState = &VirtualNetworkResource{}
var _ resource.ResourceWithModifyPlan = &VirtualNetworkResource{}

func NewVirtualNetworkResource() resource.Resource {
	return &VirtualNetworkResource{}
}

type VirtualNetworkResource struct {
	client         *latitudeshgosdk.Latitudesh
	defaultProject string
}

type VirtualNetworkResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Project          types.String `tfsdk:"project"`
	Site             types.String `tfsdk:"site"`
	Description      types.String `tfsdk:"description"`
	Tags             types.List   `tfsdk:"tags"`
	Vid              types.Int64  `tfsdk:"vid"`
	Region           types.String `tfsdk:"region"`
	AssignmentsCount types.Int64  `tfsdk:"assignments_count"`
}

func (r *VirtualNetworkResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_network"
}

func (r *VirtualNetworkResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Virtual Network resource",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Virtual Network identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "The project (ID or Slug) to deploy the virtual network",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"site": schema.StringAttribute{
				MarkdownDescription: "The site to deploy the virtual network (case-insensitive)",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					planmodifiers.CaseInsensitiveDiff{},
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "The virtual network description",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "List of virtual network tag IDs",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"vid": schema.Int64Attribute{
				MarkdownDescription: "VLAN ID of the virtual network",
				Computed:            true,
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "The region where the virtual network is deployed",
				Computed:            true,
			},
			"assignments_count": schema.Int64Attribute{
				MarkdownDescription: "Number of devices assigned to the virtual network",
				Computed:            true,
			},
		},
	}
}

func (r *VirtualNetworkResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *VirtualNetworkResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}

	var cfg, plan VirtualNetworkResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Check if only the case of 'site' has changed (only for existing resources)
	if !req.State.Raw.IsNull() {
		var state VirtualNetworkResourceModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}

		if !cfg.Site.IsNull() && !state.Site.IsNull() {
			if strings.EqualFold(cfg.Site.ValueString(), state.Site.ValueString()) &&
				cfg.Site.ValueString() != state.Site.ValueString() {
				// Only the case changed - preserve computed values from state
				plan.Vid = state.Vid
				plan.Region = state.Region
				plan.AssignmentsCount = state.AssignmentsCount
				resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
				return
			}
		}
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

func (r *VirtualNetworkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data VirtualNetworkResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Resolve "effective project": resource > provider; if absent, error
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
	// persist in state to avoid flapping of Optional+Computed
	data.Project = types.StringValue(effectiveProject)

	// Extract and validate tag IDs *before* the Create POST so an invalid tag
	// can't leave behind an orphan virtual network in the backend.
	var tagIDs []string
	if !data.Tags.IsNull() && !data.Tags.IsUnknown() {
		resp.Diagnostics.Append(data.Tags.ElementsAs(ctx, &tagIDs, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if len(tagIDs) > 0 {
			if err := r.validateTagIDs(ctx, tagIDs); err != nil {
				resp.Diagnostics.AddError("Tag Validation Error", "Unable to validate tag IDs: "+err.Error())
				return
			}
		}
	}

	// Prepare attributes for creation
	attrs := operations.CreateVirtualNetworkPrivateNetworksAttributes{}

	// Required fields
	attrs.Project = effectiveProject

	if !data.Site.IsNull() {
		// Convert site to uppercase for API compatibility (case-insensitive input)
		// Keep original case in state, only uppercase for API call
		siteValue := strings.ToUpper(data.Site.ValueString())
		site := operations.CreateVirtualNetworkPrivateNetworksSite(siteValue)
		attrs.Site = &site
	}

	// Optional fields
	if !data.Description.IsNull() {
		attrs.Description = data.Description.ValueString()
	}

	createRequest := operations.CreateVirtualNetworkPrivateNetworksRequestBody{
		Data: operations.CreateVirtualNetworkPrivateNetworksData{
			Type:       operations.CreateVirtualNetworkPrivateNetworksTypeVirtualNetwork,
			Attributes: attrs,
		},
	}

	result, err := r.client.PrivateNetworks.Create(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create virtual network, got error: "+err.Error())
		return
	}

	// Check for successful status codes
	if result != nil {
		httpStatus := result.HTTPMeta.Response.StatusCode
		if httpStatus != 200 && httpStatus != 201 {
			resp.Diagnostics.AddError("API Error", fmt.Sprintf("Create virtual network returned unexpected status code: %d", httpStatus))
			return
		}
	}

	// Try to get ID from response first
	if result.VirtualNetwork != nil && result.VirtualNetwork.Data != nil && result.VirtualNetwork.Data.ID != nil {
		data.ID = types.StringValue(*result.VirtualNetwork.Data.ID)
	} else {
		// If we can't get ID from response, use List endpoint to find it
		resp.Diagnostics.AddWarning("Virtual Network ID Not in Response", "Virtual network was created but ID not returned in response. Using List endpoint to find it.")

		// Use List endpoint to find the virtual network we just created
		// This will populate all attributes including ID, name, vid, etc.
		r.findVirtualNetworkByProject(ctx, &data, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}

		if data.ID.IsNull() || data.ID.ValueString() == "" {
			resp.Diagnostics.AddError("API Error", "Virtual network was created but could not determine its ID")
			return
		}
	}

	// The Create endpoint does not accept tags; apply them via PATCH so they
	// land on the resource and don't drift on the next plan. If the PATCH
	// fails, attempt to delete the just-created VNet so we don't leave an
	// orphan in the backend that blocks downstream destroys.
	if len(tagIDs) > 0 {
		if err := r.applyTags(ctx, data.ID.ValueString(), tagIDs, data.Description.ValueString()); err != nil {
			r.cleanupOrphanVNet(ctx, data.ID.ValueString(), &resp.Diagnostics)
			resp.Diagnostics.AddError("Tag Update Error", "Unable to apply tags to virtual network: "+err.Error())
			return
		}
	}

	// Read the resource to populate all attributes
	r.readVirtualNetwork(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VirtualNetworkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data VirtualNetworkResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Use readVirtualNetworkByID which uses the Get endpoint directly
	r.readVirtualNetworkByID(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VirtualNetworkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan VirtualNetworkResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// description, site, project all have RequiresReplace, so the only
	// attribute that reaches Update is `tags`.
	var tagIDs []string
	if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
		resp.Diagnostics.Append(plan.Tags.ElementsAs(ctx, &tagIDs, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if err := r.validateTagIDs(ctx, tagIDs); err != nil {
			resp.Diagnostics.AddError("Tag Validation Error", "Unable to validate tag IDs: "+err.Error())
			return
		}
	}

	if err := r.applyTags(ctx, plan.ID.ValueString(), tagIDs, plan.Description.ValueString()); err != nil {
		resp.Diagnostics.AddError("Tag Update Error", "Unable to update virtual network tags: "+err.Error())
		return
	}

	r.readVirtualNetworkByID(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *VirtualNetworkResource) findVirtualNetworkByProject(ctx context.Context, data *VirtualNetworkResourceModel, diags *diag.Diagnostics) {
	project := data.Project.ValueString()
	site := data.Site.ValueString()
	currentID := data.ID.ValueString()
	description := data.Description.ValueString()

	listRequest := operations.GetVirtualNetworksRequest{
		FilterProject: &project,
	}

	response, err := r.client.PrivateNetworks.List(ctx, listRequest)
	if err != nil {
		diags.AddError("Client Error", "Unable to list virtual networks to find network, got error: "+err.Error())
		return
	}

	if response.VirtualNetworks == nil || response.VirtualNetworks.Data == nil {
		diags.AddError("API Error", "No virtual networks found for project")
		return
	}

	var bestMatch *components.VirtualNetworkData
	var bestScore int

	for _, vnet := range response.VirtualNetworks.Data {
		if vnet.GetID() != nil && vnet.GetAttributes() != nil {
			score := 0

			if currentID != "" && *vnet.GetID() == currentID {
				score += 100
			}

			attrs := vnet.GetAttributes()
			region := attrs.GetRegion()

			if region != nil && region.Site != nil && region.Site.Slug != nil {
				// Case-insensitive comparison for site
				if strings.EqualFold(*region.Site.Slug, site) {
					score += 50
				}
			}

			if description != "" && attrs.GetDescription() != nil {
				if *attrs.GetDescription() == description {
					score += 30
				}
			}

			if currentID == "" {
				score += 10
			}

			if score > bestScore {
				bestScore = score
				bestMatch = &vnet
			}
		}
	}

	if bestMatch == nil || bestMatch.GetID() == nil {
		diags.AddError("API Error", fmt.Sprintf("Could not find virtual network in project '%s' with site '%s'", project, site))
		return
	}

	// Set the ID and populate computed attributes from the found virtual network
	newID := *bestMatch.GetID()
	if currentID != "" && newID != currentID {
		diags.AddWarning("Virtual Network ID Changed", fmt.Sprintf("Virtual network ID changed from '%s' to '%s'", currentID, newID))
	}

	data.ID = types.StringValue(newID)

	if bestMatch.GetAttributes() != nil {
		attrs := bestMatch.GetAttributes()

		if attrs.GetVid() != nil {
			data.Vid = types.Int64Value(*attrs.GetVid())
		} else {
			data.Vid = types.Int64Null()
		}

		if attrs.GetDescription() != nil {
			data.Description = types.StringValue(*attrs.GetDescription())
		} else {
			if data.Description.IsUnknown() {
				data.Description = types.StringNull()
			}
		}

		if attrs.GetAssignmentsCount() != nil {
			data.AssignmentsCount = types.Int64Value(*attrs.GetAssignmentsCount())
		} else {
			data.AssignmentsCount = types.Int64Null()
		}

		region := attrs.GetRegion()
		if region != nil && region.Site != nil && region.Site.Slug != nil {
			data.Region = types.StringValue(*region.Site.Slug)
			// Don't update Site - preserve the user's input case
		} else {
			data.Region = types.StringNull()
		}
	} else {
		data.Vid = types.Int64Null()
		data.Description = types.StringNull()
		data.AssignmentsCount = types.Int64Null()
		data.Region = types.StringNull()
	}
}

func (r *VirtualNetworkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data VirtualNetworkResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vlanID := data.ID.ValueString()
	if vlanID == "" {
		return
	}

	const (
		retryDeadline  = 5 * time.Minute
		initialBackoff = 5 * time.Second
		maxBackoff     = 30 * time.Second
	)

	deadline := time.Now().Add(retryDeadline)
	backoff := initialBackoff

	for {
		_, err := r.client.VirtualNetworks.Delete(ctx, vlanID)
		if err == nil {
			return
		}

		errStr := err.Error()
		if strings.Contains(errStr, "404") || strings.Contains(errStr, "not_found") {
			resp.Diagnostics.AddWarning("Virtual Network Already Deleted", "Virtual network was already deleted")
			return
		}

		if !isDeletionRestrictionError(errStr) {
			resp.Diagnostics.AddError("Client Error", "Unable to delete virtual network, got error: "+err.Error())
			return
		}

		if !time.Now().Add(backoff).Before(deadline) {
			resp.Diagnostics.AddError(
				"Client Error",
				fmt.Sprintf(
					"Unable to delete virtual network after waiting %s for assignments to drain. Last error: %s",
					retryDeadline, err.Error(),
				),
			)
			return
		}

		select {
		case <-ctx.Done():
			resp.Diagnostics.AddError(
				"Context Cancelled",
				"Virtual network delete was cancelled while waiting for assignments to drain",
			)
			return
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func isDeletionRestrictionError(errStr string) bool {
	return strings.Contains(errStr, "VIRTUAL_NETWORK_DELETION_ERROR") ||
		strings.Contains(errStr, "Virtual Network Deletion Restriction") ||
		strings.Contains(errStr, "remove all assignments")
}

func (r *VirtualNetworkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	vlanID := req.ID
	if vlanID == "" {
		resp.Diagnostics.AddError("Invalid Import ID", "Virtual network ID cannot be empty")
		return
	}

	listRequest := operations.GetVirtualNetworksRequest{}
	listResponse, err := r.client.PrivateNetworks.List(ctx, listRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to list virtual networks, got error: "+err.Error())
		return
	}

	if listResponse.VirtualNetworks == nil || listResponse.VirtualNetworks.Data == nil {
		resp.Diagnostics.AddError("API Error", "No virtual networks found")
		return
	}

	// Find the virtual network with the matching ID
	var foundVnet *components.VirtualNetworkData
	for _, vnet := range listResponse.VirtualNetworks.Data {
		if vnet.GetID() != nil && *vnet.GetID() == vlanID {
			foundVnet = &vnet
			break
		}
	}

	if foundVnet == nil {
		resp.Diagnostics.AddError("Not Found", fmt.Sprintf("Virtual network %s not found", vlanID))
		return
	}

	// Create the resource model with all attributes
	var data VirtualNetworkResourceModel
	data.ID = types.StringValue(vlanID)
	data.Tags = types.ListNull(types.StringType)

	if foundVnet.GetAttributes() != nil {
		attrs := foundVnet.GetAttributes()

		// Project
		if attrs.GetProject() != nil && attrs.GetProject().GetID() != nil {
			data.Project = types.StringValue(*attrs.GetProject().GetID())
		}

		// Description
		if attrs.GetDescription() != nil {
			data.Description = types.StringValue(*attrs.GetDescription())
		} else {
			data.Description = types.StringNull()
		}

		// VID
		if attrs.GetVid() != nil {
			data.Vid = types.Int64Value(*attrs.GetVid())
		} else {
			data.Vid = types.Int64Null()
		}

		// Assignments Count
		if attrs.GetAssignmentsCount() != nil {
			data.AssignmentsCount = types.Int64Value(*attrs.GetAssignmentsCount())
		} else {
			data.AssignmentsCount = types.Int64Null()
		}

		// Region and Site
		region := attrs.GetRegion()
		if region != nil && region.Site != nil && region.Site.Slug != nil {
			data.Region = types.StringValue(*region.Site.Slug)
			// Only set Site if not already present (during import)
			if data.Site.IsNull() || data.Site.IsUnknown() {
				data.Site = types.StringValue(*region.Site.Slug)
			}
		} else {
			data.Region = types.StringNull()
			if data.Site.IsNull() || data.Site.IsUnknown() {
				data.Site = types.StringNull()
			}
		}

		tags := attrs.GetTags()
		if len(tags) > 0 {
			tagIDs := make([]attr.Value, 0, len(tags))
			for _, tag := range tags {
				if tag.GetID() != nil {
					tagIDs = append(tagIDs, types.StringValue(*tag.GetID()))
				}
			}
			tagList, diagErr := types.ListValue(types.StringType, tagIDs)
			if diagErr.HasError() {
				resp.Diagnostics.Append(diagErr...)
			} else {
				data.Tags = tagList
			}
		} else {
			data.Tags = types.ListNull(types.StringType)
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VirtualNetworkResource) readVirtualNetworkByID(ctx context.Context, data *VirtualNetworkResourceModel, diags *diag.Diagnostics) {
	vlanID := data.ID.ValueString()
	if vlanID == "" {
		diags.AddError("Invalid ID", "Virtual network ID is empty")
		return
	}

	listRequest := operations.GetVirtualNetworksRequest{}
	listResponse, err := r.client.PrivateNetworks.List(ctx, listRequest)
	if err != nil {
		diags.AddError("Client Error", "Unable to list virtual networks, got error: "+err.Error())
		return
	}

	if listResponse.VirtualNetworks == nil || listResponse.VirtualNetworks.Data == nil {
		data.ID = types.StringNull()
		return
	}

	// Find the virtual network with the matching ID
	var foundVnet *components.VirtualNetworkData
	for _, vnet := range listResponse.VirtualNetworks.Data {
		if vnet.GetID() != nil && *vnet.GetID() == vlanID {
			foundVnet = &vnet
			break
		}
	}

	if foundVnet == nil {
		data.ID = types.StringNull()
		return
	}

	if foundVnet.GetAttributes() == nil {
		diags.AddError("API Error", "Virtual network attributes are nil")
		return
	}

	attrs := foundVnet.GetAttributes()

	// Ensure ID is preserved
	if foundVnet.GetID() != nil {
		data.ID = types.StringValue(*foundVnet.GetID())
	}

	// Extract project from attributes
	if attrs.GetProject() != nil && attrs.GetProject().GetID() != nil {
		data.Project = types.StringValue(*attrs.GetProject().GetID())
	}

	// Description
	if attrs.GetDescription() != nil {
		data.Description = types.StringValue(*attrs.GetDescription())
	} else {
		data.Description = types.StringNull()
	}

	// VID
	if attrs.GetVid() != nil {
		data.Vid = types.Int64Value(*attrs.GetVid())
	} else {
		data.Vid = types.Int64Null()
	}

	// Assignments Count
	if attrs.GetAssignmentsCount() != nil {
		data.AssignmentsCount = types.Int64Value(*attrs.GetAssignmentsCount())
	} else {
		data.AssignmentsCount = types.Int64Null()
	}

	// Region and Site
	region := attrs.GetRegion()
	if region != nil && region.Site != nil && region.Site.Slug != nil {
		data.Region = types.StringValue(*region.Site.Slug)
		// Don't update Site - preserve the user's input case
	} else {
		data.Region = types.StringNull()
	}

	// Tags need set-equivalence handling: the API treats `tags` as a set
	// (no order preserved server-side), but the schema is a List. If we
	// blindly overwrite state.Tags with the API's order, the next plan
	// shows a phantom diff and the framework rejects "inconsistent result
	// after apply". When state and API hold the same set of tag IDs, keep
	// state's order; only adopt the API ordering when an actual drift is
	// detected (different membership).
	tags := attrs.GetTags()
	apiOrder := make([]attr.Value, 0, len(tags))
	apiSet := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		if tag.GetID() != nil {
			apiOrder = append(apiOrder, types.StringValue(*tag.GetID()))
			apiSet[*tag.GetID()] = struct{}{}
		}
	}

	preserveStateOrder := false
	if !data.Tags.IsNull() && !data.Tags.IsUnknown() && len(data.Tags.Elements()) == len(apiOrder) {
		var stateIDs []string
		if !data.Tags.ElementsAs(ctx, &stateIDs, false).HasError() {
			allMatch := true
			for _, sid := range stateIDs {
				if _, ok := apiSet[sid]; !ok {
					allMatch = false
					break
				}
			}
			preserveStateOrder = allMatch
		}
	}

	if preserveStateOrder {
		// data.Tags already reflects the same set; keep its ordering.
		return
	}
	if len(apiOrder) == 0 {
		data.Tags = types.ListNull(types.StringType)
		return
	}
	tagList, diagErr := types.ListValue(types.StringType, apiOrder)
	if diagErr.HasError() {
		diags.Append(diagErr...)
		return
	}
	data.Tags = tagList
}

func (r *VirtualNetworkResource) readVirtualNetwork(ctx context.Context, data *VirtualNetworkResourceModel, diags *diag.Diagnostics) {
	vlanID := data.ID.ValueString()
	project := data.Project.ValueString()

	listRequest := operations.GetVirtualNetworksRequest{
		FilterProject: &project,
	}

	response, err := r.client.PrivateNetworks.List(ctx, listRequest)
	if err != nil {
		diags.AddError("Client Error", "Unable to list virtual networks, got error: "+err.Error())
		return
	}

	if response.VirtualNetworks == nil || response.VirtualNetworks.Data == nil {
		data.ID = types.StringNull()
		return
	}

	var foundVnet *components.VirtualNetworkData
	for _, vnet := range response.VirtualNetworks.Data {
		if vnet.GetID() != nil && *vnet.GetID() == vlanID {
			foundVnet = &vnet
			break
		}
	}

	if foundVnet == nil {
		data.ID = types.StringNull()
		return
	}

	if foundVnet.GetAttributes() != nil {
		attrs := foundVnet.GetAttributes()

		if attrs.GetVid() != nil {
			data.Vid = types.Int64Value(*attrs.GetVid())
		} else {
			data.Vid = types.Int64Null()
		}

		if attrs.GetAssignmentsCount() != nil {
			data.AssignmentsCount = types.Int64Value(*attrs.GetAssignmentsCount())
		} else {
			data.AssignmentsCount = types.Int64Null()
		}

		if attrs.GetDescription() != nil {
			data.Description = types.StringValue(*attrs.GetDescription())
		} else {
			if data.Description.IsUnknown() {
				data.Description = types.StringNull()
			}
		}

		region := attrs.GetRegion()
		if region != nil && region.Site != nil && region.Site.Slug != nil {
			data.Region = types.StringValue(*region.Site.Slug)
			// Don't update Site - preserve the user's input case
		} else {
			data.Region = types.StringNull()
		}

		if data.Tags.IsNull() || data.Tags.IsUnknown() {
			data.Tags = types.ListNull(types.StringType)
		}
	} else {
		data.Vid = types.Int64Null()
		data.AssignmentsCount = types.Int64Null()
		data.Region = types.StringNull()

		if data.Tags.IsNull() || data.Tags.IsUnknown() {
			data.Tags = types.ListNull(types.StringType)
		}

		if data.Description.IsUnknown() {
			data.Description = types.StringNull()
		}
	}
}

// cleanupOrphanVNet best-effort deletes a VNet that was created server-side
// but couldn't complete its post-create work (e.g., tag PATCH failed). A
// failure here is logged as a warning rather than promoted to an error so the
// caller's original failure remains the surfaced one.
func (r *VirtualNetworkResource) cleanupOrphanVNet(ctx context.Context, vlanID string, diags *diag.Diagnostics) {
	if vlanID == "" {
		return
	}
	if _, err := r.client.VirtualNetworks.Delete(ctx, vlanID); err != nil {
		diags.AddWarning(
			"Orphan Cleanup Failed",
			fmt.Sprintf("Failed to delete partially-created virtual network %s: %s. Manual cleanup may be required.", vlanID, err.Error()),
		)
	}
}

func (r *VirtualNetworkResource) validateTagIDs(ctx context.Context, tagIDs []string) error {
	if len(tagIDs) == 0 {
		return nil
	}

	response, err := r.client.Tags.List(ctx)
	if err != nil {
		return err
	}

	if response.CustomTags == nil || response.CustomTags.Data == nil {
		return fmt.Errorf("no tags found")
	}

	validTagIDs := make(map[string]bool)
	for _, tag := range response.CustomTags.Data {
		if tag.ID != nil {
			validTagIDs[*tag.ID] = true
		}
	}

	for _, tagID := range tagIDs {
		if !validTagIDs[tagID] {
			return fmt.Errorf("tag ID '%s' not found", tagID)
		}
	}

	return nil
}

// applyTags PATCHes the virtual network with the provided tag IDs. The SDK's
// Update payload includes a non-omitempty `description` with a default value,
// so we always echo the planned description back to avoid clobbering it.
func (r *VirtualNetworkResource) applyTags(ctx context.Context, vlanID string, tagIDs []string, description string) error {
	if vlanID == "" {
		return fmt.Errorf("virtual network ID is empty")
	}

	desc := description
	attrs := &operations.UpdateVirtualNetworkPrivateNetworksAttributes{
		Tags:        tagIDs,
		Description: &desc,
	}

	body := operations.UpdateVirtualNetworkPrivateNetworksRequestBody{
		Data: operations.UpdateVirtualNetworkPrivateNetworksData{
			ID:         vlanID,
			Type:       operations.UpdateVirtualNetworkPrivateNetworksTypeVirtualNetworks,
			Attributes: attrs,
		},
	}

	result, err := r.client.PrivateNetworks.Update(ctx, vlanID, body)
	if err != nil {
		return fmt.Errorf("unable to update virtual network: %w", err)
	}

	if result != nil && result.HTTPMeta.Response != nil {
		if status := result.HTTPMeta.Response.StatusCode; status >= 400 {
			return fmt.Errorf("virtual network update failed with status code: %d", status)
		}
	}

	return nil
}
