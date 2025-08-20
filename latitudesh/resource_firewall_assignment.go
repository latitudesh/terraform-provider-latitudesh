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
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/internal/provider"
)

var _ resource.Resource = &FirewallAssignmentResource{}
var _ resource.ResourceWithImportState = &FirewallAssignmentResource{}

func NewFirewallAssignmentResource() resource.Resource {
	return &FirewallAssignmentResource{}
}

type FirewallAssignmentResource struct {
	client *latitudeshgosdk.Latitudesh
}

type FirewallAssignmentResourceModel struct {
	ID         types.String `tfsdk:"id"`
	FirewallID types.String `tfsdk:"firewall_id"`
	ServerID   types.String `tfsdk:"server_id"`
}

func (r *FirewallAssignmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_firewall_assignment"
}

func (r *FirewallAssignmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Firewall Assignment resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Firewall assignment identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"firewall_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the firewall",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"server_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the server to assign the firewall to",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *FirewallAssignmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.client = deps.Client
}

func (r *FirewallAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data FirewallAssignmentResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	firewallID := data.FirewallID.ValueString()
	serverID := data.ServerID.ValueString()

	// Validate that we have the required IDs
	if firewallID == "" {
		resp.Diagnostics.AddError("Configuration Error", "firewall_id is required but was empty. Make sure the firewall resource is created successfully first.")
		return
	}

	if serverID == "" {
		resp.Diagnostics.AddError("Configuration Error", "server_id is required but was empty.")
		return
	}

	createRequest := operations.CreateFirewallAssignmentFirewallsAssignmentsRequestBody{
		Data: operations.CreateFirewallAssignmentFirewallsAssignmentsData{
			Type: operations.CreateFirewallAssignmentFirewallsAssignmentsTypeFirewallAssignments,
			Attributes: &operations.CreateFirewallAssignmentFirewallsAssignmentsAttributes{
				ServerID: serverID,
			},
		},
	}

	result, err := r.client.Firewalls.Assignments.Create(ctx, firewallID, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create firewall assignment for firewall %s and server %s, got error: %s", firewallID, serverID, err.Error()))
		return
	}

	// Check for successful status codes (200 or 201)
	if result == nil {
		resp.Diagnostics.AddError("API Error", "Create firewall assignment returned nil response")
		return
	}

	httpStatus := result.HTTPMeta.Response.StatusCode
	if httpStatus != 200 && httpStatus != 201 {
		resp.Diagnostics.AddError("API Error", fmt.Sprintf("Create firewall assignment returned unexpected status code: %d", httpStatus))
		return
	}

	// Always find the assignment ID through the List endpoint
	r.findAssignmentByServerAndFirewall(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Verify we got an ID
	if data.ID.IsNull() || data.ID.ValueString() == "" {
		resp.Diagnostics.AddError("API Error", "Failed to get assignment ID after creation")
		return
	}

	// Read the resource to populate all attributes
	r.readFirewallAssignment(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Helper function to find assignment ID by filtering server_id and firewall_id
func (r *FirewallAssignmentResource) findAssignmentByServerAndFirewall(ctx context.Context, data *FirewallAssignmentResourceModel, diags *diag.Diagnostics) {
	firewallID := data.FirewallID.ValueString()
	serverID := data.ServerID.ValueString()

	// Get assignments for this firewall
	response, err := r.client.Firewalls.ListAssignments(ctx, firewallID, nil, nil)
	if err != nil {
		diags.AddError("Client Error", "Unable to list firewall assignments to find assignment ID, got error: "+err.Error())
		return
	}

	// Check if we have assignments data
	if response.FirewallAssignments == nil || response.FirewallAssignments.Data == nil {
		diags.AddError("API Error", "No assignments found for firewall after creation")
		return
	}

	// Look for assignment with matching server ID and firewall ID
	for _, assignment := range response.FirewallAssignments.Data {
		if assignment.Attributes != nil && assignment.ID != nil {
			var assignmentServerID string
			var assignmentFirewallID string

			// Get server ID from assignment
			if assignment.Attributes.Server != nil && assignment.Attributes.Server.ID != nil {
				assignmentServerID = *assignment.Attributes.Server.ID
			}

			// Get firewall ID from assignment
			if assignment.Attributes.FirewallID != nil {
				assignmentFirewallID = *assignment.Attributes.FirewallID
			}

			// If we found a matching server ID and firewall ID, use this assignment
			if assignmentServerID == serverID && assignmentFirewallID == firewallID {
				data.ID = types.StringValue(*assignment.ID)
				return
			}
		}
	}

	// If we get here, we couldn't find the matching assignment
	diags.AddError("API Error", "Assignment was created but couldn't find it in the list with matching server_id and firewall_id")
}

func (r *FirewallAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data FirewallAssignmentResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	r.readFirewallAssignment(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FirewallAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// This resource doesn't support updates, it should force replacement
	resp.Diagnostics.AddError("Update Not Supported", "Firewall assignments cannot be updated, they must be replaced.")
}

func (r *FirewallAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data FirewallAssignmentResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	assignmentID := data.ID.ValueString()
	firewallID := data.FirewallID.ValueString()

	if assignmentID == "" {
		resp.Diagnostics.AddError("Invalid ID", "Assignment ID is empty")
		return
	}

	if firewallID == "" {
		resp.Diagnostics.AddError("Invalid Firewall ID", "Firewall ID is empty")
		return
	}

	_, err := r.client.Firewalls.DeleteAssignment(ctx, firewallID, assignmentID)
	if err != nil {
		// If we get a 404, the resource is already deleted
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not_found") {
			resp.Diagnostics.AddWarning("Firewall Assignment Already Deleted", "Firewall assignment appears to have been deleted outside of Terraform")
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to delete firewall assignment, got error: "+err.Error())
		return
	}
}

func (r *FirewallAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if strings.Contains(req.ID, ":") {
		parts := strings.Split(req.ID, ":")
		if len(parts) != 2 {
			resp.Diagnostics.AddError(
				"Invalid Import ID Format",
				"Import ID must be in the format: firewall_id:assignment_id or just assignment_id",
			)
			return
		}

		firewallID := parts[0]
		assignmentID := parts[1]

		if firewallID == "" || assignmentID == "" {
			resp.Diagnostics.AddError(
				"Invalid Import ID",
				"Both firewall_id and assignment_id must be non-empty",
			)
			return
		}

		var data FirewallAssignmentResourceModel
		data.ID = types.StringValue(assignmentID)
		data.FirewallID = types.StringValue(firewallID)

		r.readFirewallAssignment(ctx, &data, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}

		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		return
	}

	assignmentID := req.ID
	if assignmentID == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"Assignment ID cannot be empty",
		)
		return
	}

	var data FirewallAssignmentResourceModel
	data.ID = types.StringValue(assignmentID)

	r.findFirewallForAssignment(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readFirewallAssignment(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FirewallAssignmentResource) readFirewallAssignment(ctx context.Context, data *FirewallAssignmentResourceModel, diags *diag.Diagnostics) {
	assignmentID := data.ID.ValueString()
	if assignmentID == "" {
		diags.AddError("Invalid ID", "Assignment ID is empty")
		return
	}

	// We need the firewall ID to call ListAssignments
	firewallID := data.FirewallID.ValueString()
	if firewallID == "" {
		diags.AddError("Invalid Firewall ID", "Firewall ID is required to read assignment")
		return
	}

	// Get the first page of assignments for this firewall
	// Based on the API response, there's typically only one assignment per firewall
	response, err := r.client.Firewalls.ListAssignments(ctx, firewallID, nil, nil)
	if err != nil {
		diags.AddError("Client Error", "Unable to read firewall assignments, got error: "+err.Error())
		return
	}

	// Check if we have assignments data
	if response.FirewallAssignments == nil || response.FirewallAssignments.Data == nil {
		// No assignments found, the assignment was likely deleted
		data.ID = types.StringNull()
		return
	}

	// Look for our specific assignment in the data array
	for _, assignment := range response.FirewallAssignments.Data {
		if assignment.ID != nil && *assignment.ID == assignmentID {
			// Found it! Populate the data model
			r.populateAssignmentData(data, &assignment)
			return
		}
	}

	// If not found, the assignment was likely deleted
	data.ID = types.StringNull()
}

// Helper function to populate assignment data
func (r *FirewallAssignmentResource) populateAssignmentData(data *FirewallAssignmentResourceModel, assignment *components.FirewallAssignmentData) {
	if assignment.Attributes != nil {
		if assignment.Attributes.FirewallID != nil {
			data.FirewallID = types.StringValue(*assignment.Attributes.FirewallID)
		}

		if assignment.Attributes.Server != nil && assignment.Attributes.Server.ID != nil {
			data.ServerID = types.StringValue(*assignment.Attributes.Server.ID)
		}
	}
}

func (r *FirewallAssignmentResource) findFirewallForAssignment(ctx context.Context, data *FirewallAssignmentResourceModel, diags *diag.Diagnostics) {
	assignmentID := data.ID.ValueString()

	response, err := r.client.Firewalls.List(ctx, nil, nil, nil)
	if err != nil {
		diags.AddError("Client Error", "Unable to list firewalls to find assignment, got error: "+err.Error())
		return
	}

	if response.Firewalls == nil || response.Firewalls.Data == nil {
		diags.AddError("API Error", "No firewalls found")
		return
	}

	for _, firewall := range response.Firewalls.Data {
		if firewall.ID == nil {
			continue
		}

		firewallID := *firewall.ID

		assignmentsResp, err := r.client.Firewalls.ListAssignments(ctx, firewallID, nil, nil)
		if err != nil {
			continue
		}

		if assignmentsResp.FirewallAssignments == nil || assignmentsResp.FirewallAssignments.Data == nil {
			continue
		}

		for _, assignment := range assignmentsResp.FirewallAssignments.Data {
			if assignment.ID != nil && *assignment.ID == assignmentID {
				data.FirewallID = types.StringValue(firewallID)
				return
			}
		}
	}

	diags.AddError("Assignment Not Found", fmt.Sprintf("Assignment %s not found in any firewall", assignmentID))
}
