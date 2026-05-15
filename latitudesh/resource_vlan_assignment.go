package latitudesh

import (
	"context"
	"fmt"
	"strings"
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
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
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
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.client = deps.Client
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

	id := data.ID.ValueString()

	result, err := r.client.PrivateNetworks.DeleteAssignment(ctx, id)
	if err != nil && err.Error() != "{}" {
		if strings.Contains(err.Error(), "404") {
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to delete virtual network assignment, got error: "+err.Error())
		return
	}

	if result != nil && result.HTTPMeta.Response != nil {
		code := result.HTTPMeta.Response.StatusCode
		if code == 404 {
			return
		}
		if code >= 400 {
			resp.Diagnostics.AddError("Client Error",
				fmt.Sprintf("Unable to delete virtual network assignment, status code: %d", code))
			return
		}
	}

	r.waitForAssignmentRemoval(ctx, id, data.VirtualNetworkID.ValueString(), data.ServerID.ValueString())
}

// waitForAssignmentRemoval polls ListAssignments filtered by both the vnet and
// the server IDs — the tuple is unique in interface_tagged_vlans, so the
// response holds at most one entry and fits on the first page regardless of
// account size. Without the filters, `ListAssignments` returns page 1 of all
// assignments in the account (page size default 20) and the lookup silently
// no-ops when the target lives on page 2+.
func (r *VlanAssignmentResource) waitForAssignmentRemoval(ctx context.Context, id, vnetID, serverID string) {
	const (
		waitDeadline = 2 * time.Minute
		pollInterval = 3 * time.Second
	)
	deadline := time.Now().Add(waitDeadline)

	for time.Now().Before(deadline) {
		response, err := r.client.PrivateNetworks.ListAssignments(ctx, operations.GetVirtualNetworksAssignmentsRequest{
			FilterVirtualNetworkID: &vnetID,
			FilterServer:           &serverID,
		})
		if err != nil {
			// Transient list failures shouldn't fail the destroy; let the
			// parent vnet delete handle any remaining lag.
			return
		}
		if response.VirtualNetworkAssignments == nil || response.VirtualNetworkAssignments.Data == nil {
			return
		}

		stillPresent := false
		for _, a := range response.VirtualNetworkAssignments.Data {
			if a.ID != nil && *a.ID == id {
				stillPresent = true
				break
			}
		}
		if !stillPresent {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(pollInterval):
		}
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

	// Initialize Computed attributes to Null. Plugin Framework requires every
	// Computed attribute to be Known (concrete value or explicit Null) after
	// Apply; if the API response is missing one of these fields we still want
	// a concrete Null in state rather than Unknown.
	data.Vid = types.Int64Null()
	data.Description = types.StringNull()
	data.Status = types.StringNull()

	// The API populates `vid` asynchronously when assignments are created in
	// parallel (NetBox `get_next_vid` allocation). Retry the lookup a few
	// times so we don't surface Null and trigger a phantom diff on refresh.
	const vidRetryAttempts = 3
	for attempt := 0; attempt < vidRetryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return
			}
		}

		response, err := r.client.PrivateNetworks.ListAssignments(ctx, operations.GetVirtualNetworksAssignmentsRequest{})
		if err != nil {
			diags.AddError("Client Error", "Unable to read virtual network assignments, got error: "+err.Error())
			return
		}

		if response.VirtualNetworkAssignments == nil || response.VirtualNetworkAssignments.Data == nil {
			data.ID = types.StringNull()
			return
		}

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

		// If we got the vid, we're done. Otherwise retry.
		if !data.Vid.IsNull() {
			return
		}
	}
	// Exhausted retries — keep whatever Null fallbacks were initialized above.
}

func (r *VlanAssignmentResource) findAssignmentByServerAndVNet(ctx context.Context, data *VlanAssignmentResourceModel, diags *diag.Diagnostics) {
	serverID := data.ServerID.ValueString()
	vnetID := data.VirtualNetworkID.ValueString()

	// Initialize Computed attributes to Null so a partial API response can't
	// leave them Unknown after Apply.
	data.Vid = types.Int64Null()
	data.Description = types.StringNull()
	data.Status = types.StringNull()

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
