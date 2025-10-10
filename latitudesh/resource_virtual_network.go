package latitudesh

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	providerpkg "github.com/latitudesh/terraform-provider-latitudesh/internal/provider"
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
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"site": schema.StringAttribute{
				MarkdownDescription: "The site to deploy the virtual network",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "The virtual network description",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "List of virtual network tags",
				ElementType:         types.StringType,
				Optional:            true,
			},
			// Computed attributes
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

	var cfg, state, plan VirtualNetworkResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	// state only exists for update operation
	if !req.State.Raw.IsNull() {
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	planModified := false

	// For updates: ignore project changes
	if !req.State.Raw.IsNull() && !state.Project.IsNull() {
		if !plan.Project.Equal(state.Project) {
			// Project change detected: ignore it by reverting to state value
			plan.Project = state.Project
			planModified = true

			resp.Diagnostics.AddWarning(
				"Project Change Ignored",
				"Virtual network project cannot be changed after creation. The project will remain unchanged. "+
					"To move a virtual network to a different project, you must create a new resource.",
			)
		}
	} else {
		// For creation: handle project resolution
		if !cfg.Project.IsNull() && !cfg.Project.IsUnknown() && cfg.Project.ValueString() != "" {
			plan.Project = cfg.Project
			planModified = true
		} else if r.defaultProject != "" {
			plan.Project = types.StringValue(r.defaultProject)
			planModified = true
		} else {
			resp.Diagnostics.AddError(
				"Missing project",
				"Set `project` on this resource or define a default in the provider block (provider `latitudesh` { project = \"...\" }).",
			)
			return
		}
	}

	// Apply the modified plan if made changes
	if planModified {
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
	}
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

	// Prepare attributes for creation
	attrs := operations.CreateVirtualNetworkPrivateNetworksAttributes{}

	// Required fields
	attrs.Project = effectiveProject

	if !data.Site.IsNull() {
		siteValue := data.Site.ValueString()
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

		// Read the resource to populate all attributes
		r.readVirtualNetwork(ctx, &data, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
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

	r.readVirtualNetwork(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VirtualNetworkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update Not Supported",
		"Virtual network updates are not supported due to SDK limitations. "+
			"Changes to description require resource replacement.")
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
				if *region.Site.Slug == site {
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
			data.Site = types.StringValue(*region.Site.Slug)
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

	// Attempt to delete using the virtual network ID directly
	_, err := r.client.VirtualNetworks.Delete(ctx, vlanID)
	if err != nil {
		// If we get a 404, the resource was already deleted
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not_found") {
			resp.Diagnostics.AddWarning("Virtual Network Already Deleted", "Virtual network was already deleted")
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to delete virtual network, got error: "+err.Error())
		return
	}
}

func (r *VirtualNetworkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var data VirtualNetworkResourceModel
	data.ID = types.StringValue(req.ID)

	r.readVirtualNetwork(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
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
			data.Site = types.StringValue(*region.Site.Slug)
		} else {
			data.Region = types.StringNull()
		}

		// TODO: SDK should expose tags in read operations
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
