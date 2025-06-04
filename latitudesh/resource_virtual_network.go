package latitudesh

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
)

// Ensure provider defined types fully satisfy framework interfaces.
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
	Name             types.String `tfsdk:"name"`
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
				Required:            true,
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
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the virtual network",
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

	// Prepare attributes for creation
	attrs := operations.CreateVirtualNetworkPrivateNetworksAttributes{}

	// Required fields
	if !data.Project.IsNull() {
		attrs.Project = data.Project.ValueString()
	}

	if !data.Site.IsNull() {
		siteValue := data.Site.ValueString()
		site := operations.CreateVirtualNetworkPrivateNetworksSite(siteValue)
		attrs.Site = &site
	}

	// Optional fields
	if !data.Description.IsNull() {
		attrs.Description = data.Description.ValueString()
	}

	// Note: Tags are not supported in the create operation, only in update

	createRequest := operations.CreateVirtualNetworkPrivateNetworksRequestBody{
		Data: operations.CreateVirtualNetworkPrivateNetworksData{
			Type:       operations.CreateVirtualNetworkPrivateNetworksTypeVirtualNetwork,
			Attributes: attrs,
		},
	}

	result, err := r.client.PrivateNetworks.CreateVirtualNetwork(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create virtual network, got error: "+err.Error())
		return
	}

	if result.VirtualNetwork == nil || result.VirtualNetwork.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get virtual network ID from response")
		return
	}

	data.ID = types.StringValue(*result.VirtualNetwork.ID)

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

	r.readVirtualNetwork(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VirtualNetworkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data VirtualNetworkResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vlanID := data.ID.ValueString()

	// Prepare update request
	attrs := &operations.UpdateVirtualNetworkPrivateNetworksAttributes{}

	if !data.Description.IsNull() {
		description := data.Description.ValueString()
		attrs.Description = &description
	}

	if !data.Tags.IsNull() && !data.Tags.IsUnknown() {
		var tags []string
		resp.Diagnostics.Append(data.Tags.ElementsAs(ctx, &tags, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		attrs.Tags = tags
	}

	updateRequest := operations.UpdateVirtualNetworkPrivateNetworksRequestBody{
		ID: &vlanID,
		Data: operations.UpdateVirtualNetworkPrivateNetworksData{
			Type:       operations.UpdateVirtualNetworkPrivateNetworksTypeVirtualNetworks,
			Attributes: attrs,
		},
	}

	_, err := r.client.PrivateNetworks.UpdateVirtualNetwork(ctx, vlanID, updateRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to update virtual network, got error: "+err.Error())
		return
	}

	// Read the resource to populate all attributes
	r.readVirtualNetwork(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VirtualNetworkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data VirtualNetworkResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vlanID := data.ID.ValueString()

	// Convert string ID to int64 for the delete operation
	vlanIDInt, err := strconv.ParseInt(vlanID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("ID Conversion Error", "Unable to convert virtual network ID to integer: "+err.Error())
		return
	}

	_, err = r.client.VirtualNetworks.Delete(ctx, vlanIDInt)
	if err != nil {
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

	response, err := r.client.PrivateNetworks.GetVirtualNetwork(ctx, vlanID)
	if err != nil {
		diags.AddError("Client Error", "Unable to read virtual network, got error: "+err.Error())
		return
	}

	if response.Object == nil || response.Object.Data == nil {
		data.ID = types.StringNull()
		return
	}

	vnet := response.Object.Data
	if vnet.Attributes != nil {
		attrs := vnet.Attributes

		if attrs.Vid != nil {
			data.Vid = types.Int64Value(*attrs.Vid)
		}

		if attrs.Name != nil {
			data.Name = types.StringValue(*attrs.Name)
		}

		if attrs.Description != nil {
			data.Description = types.StringValue(*attrs.Description)
		}

		if attrs.AssignmentsCount != nil {
			data.AssignmentsCount = types.Int64Value(*attrs.AssignmentsCount)
		}

		if attrs.Region != nil && attrs.Region.Site != nil && attrs.Region.Site.Slug != nil {
			data.Region = types.StringValue(*attrs.Region.Site.Slug)
		}
	}
}
