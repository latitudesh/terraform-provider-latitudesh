package latitudesh

import (
	"context"
	"fmt"
	"time"

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
	Vid              types.Int64  `tfsdk:"vid"`
	Description      types.String `tfsdk:"description"`
	Status           types.String `tfsdk:"status"`
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

	result, err := r.client.PrivateNetworks.Assign(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create virtual network assignment, got error: "+err.Error())
		return
	}

	// Check for successful status codes
	if result != nil {
		httpStatus := result.HTTPMeta.Response.StatusCode
		if httpStatus != 200 && httpStatus != 201 {
			resp.Diagnostics.AddError("API Error", fmt.Sprintf("Create virtual network assignment returned unexpected status code: %d", httpStatus))
			return
		}
	}

	// Try to get ID from response first
	if result.VirtualNetworkAssignment != nil && result.VirtualNetworkAssignment.Data != nil && result.VirtualNetworkAssignment.Data.ID != nil {
		data.ID = types.StringValue(*result.VirtualNetworkAssignment.Data.ID)
	} else {
		// If we can't get ID from response, use List endpoint to find it
		resp.Diagnostics.AddWarning("Assignment ID Not in Response", "Virtual network assignment was created but ID not returned in response. Using List endpoint to find it.")

		// Wait a moment for the assignment to be processed
		time.Sleep(2 * time.Second)

		// Use List endpoint to find the assignment we just created
		r.findAssignmentByServerAndVNet(ctx, &data, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}

		if data.ID.IsNull() || data.ID.ValueString() == "" {
			resp.Diagnostics.AddError("API Error", "Virtual network assignment was created but could not determine its ID")
			return
		}
	}

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

	_, err := r.client.PrivateNetworks.DeleteAssignment(ctx, assignmentID)
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
	response, err := r.client.PrivateNetworks.ListAssignments(ctx, operations.GetVirtualNetworksAssignmentsRequest{})
	if err != nil {
		diags.AddError("Client Error", "Unable to read virtual network assignments, got error: "+err.Error())
		return
	}

	if response.VirtualNetworkAssignments == nil || response.VirtualNetworkAssignments.Data == nil {
		data.ID = types.StringNull()
		return
	}

	// Find our assignment
	var assignment *components.VirtualNetworkAssignmentData
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

		if attrs.Server != nil && attrs.Server.ID != nil {
			data.ServerID = types.StringValue(*attrs.Server.ID)
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

func (r *VlanAssignmentResource) findAssignmentByServerAndVNet(ctx context.Context, data *VlanAssignmentResourceModel, diags *diag.Diagnostics) {
	serverID := data.ServerID.ValueString()
	vnetID := data.VirtualNetworkID.ValueString()

	// Get all virtual network assignments and find ours
	response, err := r.client.PrivateNetworks.ListAssignments(ctx, operations.GetVirtualNetworksAssignmentsRequest{})
	if err != nil {
		diags.AddError("Client Error", "Unable to read virtual network assignments, got error: "+err.Error())
		return
	}

	if response.VirtualNetworkAssignments == nil || response.VirtualNetworkAssignments.Data == nil {
		diags.AddError("API Error", "No virtual network assignments found in response")
		return
	}

	// Find our assignment
	var assignment *components.VirtualNetworkAssignmentData
	for _, a := range response.VirtualNetworkAssignments.Data {
		if a.Attributes != nil {
			attrs := a.Attributes

			if attrs.VirtualNetworkID != nil && *attrs.VirtualNetworkID == vnetID {
				var assignmentServerID string
				if attrs.Server != nil && attrs.Server.ID != nil {
					assignmentServerID = *attrs.Server.ID
				}

				if assignmentServerID == serverID {
					assignment = &a
					break
				}
			}
		}
	}

	if assignment == nil {
		// Add more detailed debugging information
		var foundAssignments []string
		for _, a := range response.VirtualNetworkAssignments.Data {
			if a.Attributes != nil && a.ID != nil {
				attrs := a.Attributes
				serverIDStr := "nil"
				vnetIDStr := "nil"

				if attrs.Server != nil && attrs.Server.ID != nil {
					serverIDStr = *attrs.Server.ID
				}

				if attrs.VirtualNetworkID != nil {
					vnetIDStr = *attrs.VirtualNetworkID
				}
				foundAssignments = append(foundAssignments, fmt.Sprintf("ID: %s, Server: %s, VNet: %s", *a.ID, serverIDStr, vnetIDStr))
			}
		}

		errorMsg := fmt.Sprintf("Could not find virtual network assignment for server '%s' and virtual network '%s'", serverID, vnetID)
		if len(foundAssignments) > 0 {
			errorMsg += fmt.Sprintf(". Found assignments: %v", foundAssignments)
		} else {
			errorMsg += ". No assignments found in response."
		}

		diags.AddError("API Error", errorMsg)
		return
	}

	// Set the ID
	if assignment.ID != nil {
		data.ID = types.StringValue(*assignment.ID)
	}

	if assignment.Attributes != nil {
		attrs := assignment.Attributes

		if attrs.Server != nil && attrs.Server.ID != nil {
			data.ServerID = types.StringValue(*attrs.Server.ID)
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
