package latitudesh

import (
	"context"
	"encoding/json"
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
)

var _ resource.Resource = &VirtualNetworkResource{}
var _ resource.ResourceWithImportState = &VirtualNetworkResource{}

func NewVirtualNetworkResource() resource.Resource {
	return &VirtualNetworkResource{}
}

type VirtualNetworkResource struct {
	client *latitudeshgosdk.Latitudesh
}

type VirtualNetworkResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Project     types.String `tfsdk:"project"`
	Site        types.String `tfsdk:"site"`
	Description types.String `tfsdk:"description"`
	Tags        types.List   `tfsdk:"tags"`
	// Computed fields
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

	client, ok := req.ProviderData.(*latitudeshgosdk.Latitudesh)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			"Expected *latitudeshgosdk.Latitudesh, got: %T. Please report this issue to the provider developers.",
		)
		return
	}

	r.client = client
}

func (r *VirtualNetworkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data VirtualNetworkResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.Project.IsNull() || data.Project.ValueString() == "" {
		resp.Diagnostics.AddError("Missing Required Field", "The project field is required when creating a virtual network.")
		return
	}

	attrs := operations.CreateVirtualNetworkPrivateNetworksAttributes{
		Project: data.Project.ValueString(),
	}
	if !data.Site.IsNull() {
		site := operations.CreateVirtualNetworkPrivateNetworksSite(data.Site.ValueString())
		attrs.Site = &site
	}
	if !data.Description.IsNull() {
		attrs.Description = data.Description.ValueString()
	}

	body := operations.CreateVirtualNetworkPrivateNetworksRequestBody{
		Data: operations.CreateVirtualNetworkPrivateNetworksData{
			Type:       operations.CreateVirtualNetworkPrivateNetworksTypeVirtualNetwork,
			Attributes: attrs,
		},
	}

	res, err := r.client.PrivateNetworks.Create(ctx, body)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create virtual network, got error: "+err.Error())
		return
	}
	// valida retorno HTTP
	if res == nil || res.HTTPMeta.Response == nil {
		resp.Diagnostics.AddError("API Error", "Empty response from create virtual network")
		return
	}
	if sc := res.HTTPMeta.Response.StatusCode; sc < 200 || sc >= 300 {
		resp.Diagnostics.AddError("API Error", fmt.Sprintf("Create virtual network returned unexpected status code: %d", sc))
		return
	}

	// Ao invés de tentar ler por ID (que pode não vir), sempre descobrimos via List
	r.findVirtualNetworkByProject(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if data.ID.IsNull() || data.ID.ValueString() == "" {
		resp.Diagnostics.AddError("API Error", "Virtual network was created but could not determine its ID")
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

	if data.Project.IsNull() || data.Project.ValueString() == "" {
		resp.Diagnostics.AddWarning("Missing project", "Virtual network has no 'project' in state; cannot list & match. Marking as not found.")
		resp.State.RemoveResource(ctx)
		return
	}

	r.findVirtualNetworkByProject(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if data.ID.IsNull() || data.ID.ValueString() == "" {
		resp.Diagnostics.AddWarning("Virtual network not found", "Resource may have been deleted outside Terraform. Removing from state.")
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VirtualNetworkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Virtual network updates are problematic with the current SDK
	// Since description now requires replacement, updates should not happen
	resp.Diagnostics.AddError("Update Not Supported",
		"Virtual network updates are not supported due to SDK limitations. "+
			"Changes to description require resource replacement. "+
			"Tags can only be set during creation.")
}

// Helper function to find virtual network by project and other identifying attributes
func (r *VirtualNetworkResource) findVirtualNetworkByProject(ctx context.Context, data *VirtualNetworkResourceModel, diags *diag.Diagnostics) {
	project := data.Project.ValueString()
	site := data.Site.ValueString()
	currentID := data.ID.ValueString()
	description := data.Description.ValueString()

	// Create request to list virtual networks filtered by project
	listRequest := operations.GetVirtualNetworksRequest{
		FilterProject: &project,
	}

	response, err := r.client.PrivateNetworks.List(ctx, listRequest)
	if err != nil {
		diags.AddError("Client Error", "Unable to list virtual networks to find network, got error: "+err.Error())
		return
	}

	// Check if we have virtual networks data
	if response.VirtualNetworks == nil || response.VirtualNetworks.Data == nil {
		diags.AddError("API Error", "No virtual networks found for project")
		return
	}

	// Debug: print the list response as JSON
	if jsonBytes, err := json.MarshalIndent(response, "", "  "); err == nil {
		diags.AddWarning("DEBUG: List Response JSON", string(jsonBytes))
	}

	// Debug: print the first virtual network specifically
	if len(response.VirtualNetworks.Data) > 0 {
		if jsonBytes, err := json.MarshalIndent(response.VirtualNetworks.Data[0], "", "  "); err == nil {
			diags.AddWarning("DEBUG: First VNET from List", string(jsonBytes))
		}
	}

	// Look for virtual network with matching attributes
	var bestMatch *components.VirtualNetworkData
	var bestScore int

	for _, vnet := range response.VirtualNetworks.Data {
		if vnet.ID != nil && vnet.Attributes != nil {
			score := 0

			// If we have a current ID, prioritize exact ID match
			if currentID != "" && *vnet.ID == currentID {
				score += 100
			}

			// Match by site (high priority for new resources)
			if vnet.Attributes.Region != nil && vnet.Attributes.Region.Site != nil && vnet.Attributes.Region.Site.Slug != nil {
				if *vnet.Attributes.Region.Site.Slug == site {
					score += 50
				}
			}

			// Match by description if provided
			if description != "" && vnet.Attributes.Description != nil {
				if *vnet.Attributes.Description == description {
					score += 30
				}
			}

			// For newly created resources without ID, prefer the most recently created one
			// (this is a heuristic since we don't have creation timestamps)
			if currentID == "" {
				score += 10 // Small boost for resources when we're looking for new ones
			}

			// Update best match if this is better
			if score > bestScore {
				bestScore = score
				bestMatch = &vnet
			}
		}
	}

	if bestMatch == nil || bestMatch.ID == nil {
		diags.AddError("API Error", fmt.Sprintf("Could not find virtual network in project '%s' with site '%s'", project, site))
		return
	}

	// Set the ID and populate computed attributes from the found virtual network
	newID := *bestMatch.ID
	if currentID != "" && newID != currentID {
		diags.AddWarning("Virtual Network ID Changed", fmt.Sprintf("Virtual network ID changed from '%s' to '%s'", currentID, newID))
	}

	data.ID = types.StringValue(newID)

	// Populate computed attributes from the found virtual network
	if bestMatch.Attributes != nil {
		attrs := bestMatch.Attributes

		if attrs.Vid != nil {
			data.Vid = types.Int64Value(*attrs.Vid)
		} else {
			data.Vid = types.Int64Null()
		}

		if attrs.Description != nil {
			data.Description = types.StringValue(*attrs.Description)
		} else {
			data.Description = types.StringNull()
		}

		if attrs.AssignmentsCount != nil {
			data.AssignmentsCount = types.Int64Value(*attrs.AssignmentsCount)
		} else {
			data.AssignmentsCount = types.Int64Null()
		}

		if attrs.Region != nil && attrs.Region.Site != nil && attrs.Region.Site.Slug != nil {
			data.Region = types.StringValue(*attrs.Region.Site.Slug)
		} else {
			data.Region = types.StringNull()
		}
	} else {
		// If attributes are nil, set all computed fields to null
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

	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "Provider client is nil in Delete()")
		return
	}

	// Se tivermos contexto suficiente, tenta confirmar o ID usando a busca por projeto/site
	hasProject := !data.Project.IsNull() && data.Project.ValueString() != ""
	hasSite := !data.Site.IsNull() && data.Site.ValueString() != ""
	if hasProject && hasSite {
		r.findVirtualNetworkByProject(ctx, &data, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	id := data.ID.ValueString()
	if id == "" {
		resp.Diagnostics.AddWarning(
			"Missing ID",
			"Could not determine the virtual network ID to delete (state and lookup were empty). Assuming it was already deleted.",
		)
		return
	}

	// Deleta diretamente pelo ID
	if _, err := r.client.VirtualNetworks.Delete(ctx, id); err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not_found") {
			resp.Diagnostics.AddWarning("Virtual Network Already Deleted", "Virtual network appears to have been deleted outside of Terraform")
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
	id := data.ID.ValueString()

	data.Vid = types.Int64Null()
	data.AssignmentsCount = types.Int64Null()
	data.Region = types.StringNull()

	diags.AddWarning("DEBUG: Reading virtual network", "Reading virtual network with ID: "+id)

	response, err := r.client.PrivateNetworks.Get(ctx, id)
	if err != nil {
		diags.AddError("Client Error", "Unable to read virtual network, got error: "+err.Error())
		return
	}

	if response == nil || response.Object == nil || response.Object.Data == nil {
		data.ID = types.StringNull()
		return
	}

	vnet := response.Object.Data

	// Check if the data object is empty (which indicates the resource doesn't exist)
	if vnet == nil || vnet.Data == nil {
		diags.AddWarning("DEBUG: Empty response data", "The API returned an empty data object, indicating the virtual network may not exist")
	}

	// Debug: print the response as JSON
	if jsonBytes, err := json.MarshalIndent(response, "", "  "); err == nil {
		diags.AddWarning("DEBUG: Virtual Network API Response", string(jsonBytes))
	}

	// Debug: print the vnet object specifically
	if jsonBytes, err := json.MarshalIndent(vnet, "", "  "); err == nil {
		diags.AddWarning("DEBUG: VNET Object", string(jsonBytes))
	}

	// Debug: print vnet.Data specifically
	if vnet != nil && vnet.Data != nil {
		if jsonBytes, err := json.MarshalIndent(vnet.Data, "", "  "); err == nil {
			diags.AddWarning("DEBUG: VNET.Data Object", string(jsonBytes))
		}
	}

	if vnet != nil && vnet.Data != nil {
		attrs := vnet.Data.Attributes

		if attrs.Vid != nil {
			data.Vid = types.Int64Value(*attrs.Vid)
		}

		if attrs.Description != nil {
			data.Description = types.StringValue(*attrs.Description)
		}

		if attrs.AssignmentsCount != nil {
			data.AssignmentsCount = types.Int64Value(*attrs.AssignmentsCount)
		}

		if attrs.Region != nil && attrs.Region.Site != nil && attrs.Region.Site.Slug != nil {
			slug := *attrs.Region.Site.Slug
			data.Region = types.StringValue(slug)
			data.Site = types.StringValue(slug)
		}

		data.Tags = types.ListNull(types.StringType)
	}
}
