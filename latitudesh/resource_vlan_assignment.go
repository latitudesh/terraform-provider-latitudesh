package latitudesh

import (
	"context"

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

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &VlanAssignmentResource{}
var _ resource.ResourceWithImportState = &VlanAssignmentResource{}

func NewVlanAssignmentResource() resource.Resource {
	return &VlanAssignmentResource{}
}

type VlanAssignmentResource struct {
	client *latitudeshgosdk.Latitudesh
}

type VlanAssignmentResourceModel struct {
	ID               types.String `tfsdk:"id"`
	ServerID         types.String `tfsdk:"server_id"`
	VirtualNetworkID types.String `tfsdk:"virtual_network_id"`
	// Computed fields
	Vid         types.Int64  `tfsdk:"vid"`
	Description types.String `tfsdk:"description"`
	Status      types.String `tfsdk:"status"`
}

func (r *VlanAssignmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vlan_assignment"
}

func (r *VlanAssignmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "VLAN Assignment resource",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "VLAN Assignment identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"server_id": schema.StringAttribute{
				MarkdownDescription: "The server ID to assign to the virtual network",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"virtual_network_id": schema.StringAttribute{
				MarkdownDescription: "The virtual network ID to assign the server to",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			// Computed attributes
			"vid": schema.Int64Attribute{
				MarkdownDescription: "VLAN ID of the virtual network",
				Computed:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the virtual network",
				Computed:            true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Status of the assignment",
				Computed:            true,
			},
		},
	}
}

func (r *VlanAssignmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *VlanAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data VlanAssignmentResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	serverID := data.ServerID.ValueString()
	vnetID := data.VirtualNetworkID.ValueString()

	// Create the assignment request
	attrs := &operations.AssignServerVirtualNetworkPrivateNetworksAttributes{
		ServerID:         serverID,
		VirtualNetworkID: vnetID,
	}

	createRequest := operations.AssignServerVirtualNetworkPrivateNetworksRequestBody{
		Data: &operations.AssignServerVirtualNetworkPrivateNetworksData{
			Type:       operations.AssignServerVirtualNetworkPrivateNetworksTypeVirtualNetworkAssignment,
			Attributes: attrs,
		},
	}

	result, err := r.client.PrivateNetworks.AssignServerVirtualNetwork(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create virtual network assignment, got error: "+err.Error())
		return
	}

	if result.VirtualNetworkAssignment == nil || result.VirtualNetworkAssignment.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get virtual network assignment ID from response")
		return
	}

	data.ID = types.StringValue(*result.VirtualNetworkAssignment.ID)

	// Read the resource to populate all attributes
	r.readVlanAssignment(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VlanAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data VlanAssignmentResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readVlanAssignment(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VlanAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// VLAN assignments cannot be updated, they must be replaced
	resp.Diagnostics.AddError("Update Not Supported", "VLAN assignments cannot be updated, they must be replaced.")
}

func (r *VlanAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data VlanAssignmentResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	assignmentID := data.ID.ValueString()

	_, err := r.client.PrivateNetworks.DeleteVirtualNetworksAssignments(ctx, assignmentID)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to delete virtual network assignment, got error: "+err.Error())
		return
	}
}

func (r *VlanAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var data VlanAssignmentResourceModel
	data.ID = types.StringValue(req.ID)

	r.readVlanAssignment(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VlanAssignmentResource) readVlanAssignment(ctx context.Context, data *VlanAssignmentResourceModel, diags *diag.Diagnostics) {
	assignmentID := data.ID.ValueString()

	// Get all virtual network assignments and find ours
	response, err := r.client.PrivateNetworks.GetVirtualNetworksAssignments(ctx, operations.GetVirtualNetworksAssignmentsRequest{})
	if err != nil {
		diags.AddError("Client Error", "Unable to read virtual network assignments, got error: "+err.Error())
		return
	}

	if response.VirtualNetworkAssignments == nil || response.VirtualNetworkAssignments.Data == nil {
		data.ID = types.StringNull()
		return
	}

	// Find our assignment
	var assignment *components.VirtualNetworkAssignment
	for _, a := range response.VirtualNetworkAssignments.Data {
		if a.ID != nil && *a.ID == assignmentID {
			assignment = &a
			break
		}
	}

	if assignment == nil {
		data.ID = types.StringNull()
		return
	}

	if assignment.Attributes != nil {
		attrs := assignment.Attributes

		if attrs.ServerID != nil {
			data.ServerID = types.StringValue(*attrs.ServerID)
		}

		if attrs.VirtualNetworkID != nil {
			data.VirtualNetworkID = types.StringValue(*attrs.VirtualNetworkID)
		}

		if attrs.Vid != nil {
			data.Vid = types.Int64Value(*attrs.Vid)
		}

		if attrs.Description != nil {
			data.Description = types.StringValue(*attrs.Description)
		}

		if attrs.Status != nil {
			data.Status = types.StringValue(*attrs.Status)
		}
	}
}
