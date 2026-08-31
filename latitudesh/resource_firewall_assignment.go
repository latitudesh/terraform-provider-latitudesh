package latitudesh

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
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

var _ resource.Resource = &FirewallAssignmentResource{}
var _ resource.ResourceWithImportState = &FirewallAssignmentResource{}
var _ resource.ResourceWithConfigValidators = &FirewallAssignmentResource{}

func NewFirewallAssignmentResource() resource.Resource {
	return &FirewallAssignmentResource{}
}

type FirewallAssignmentResource struct {
	client *latitudeshgosdk.Latitudesh
}

type FirewallAssignmentResourceModel struct {
	ID               types.String `tfsdk:"id"`
	FirewallID       types.String `tfsdk:"firewall_id"`
	ServerID         types.String `tfsdk:"server_id"`
	VirtualMachineID types.String `tfsdk:"virtual_machine_id"`
}

func (r *FirewallAssignmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_firewall_assignment"
}

func (r *FirewallAssignmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Firewall Assignment resource. Assigns a firewall to a server or to a virtual machine — set exactly one of `server_id` or `virtual_machine_id`.",

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
				MarkdownDescription: "The ID of the server to assign the firewall to. Exactly one of `server_id` or `virtual_machine_id` must be set.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"virtual_machine_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the virtual machine to assign the firewall to. Exactly one of `server_id` or `virtual_machine_id` must be set. A virtual machine can be assigned to at most one firewall.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

// ConfigValidators enforces the API contract "provide exactly one of server_id
// or virtual_machine_id" at plan time. Unknown values (e.g. references to
// resources not yet created) are skipped here and re-checked in Create.
func (r *FirewallAssignmentResource) ConfigValidators(ctx context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(
			path.MatchRoot("server_id"),
			path.MatchRoot("virtual_machine_id"),
		),
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
	virtualMachineID := data.VirtualMachineID.ValueString()

	// Validate that we have the required IDs
	if firewallID == "" {
		resp.Diagnostics.AddError("Configuration Error", "firewall_id is required but was empty. Make sure the firewall resource is created successfully first.")
		return
	}

	// ConfigValidators already enforces this on known config values; this
	// re-check covers values that were unknown at plan time.
	if (serverID == "") == (virtualMachineID == "") {
		resp.Diagnostics.AddError("Configuration Error", "Exactly one of server_id or virtual_machine_id must be set.")
		return
	}

	attributes := &operations.CreateFirewallAssignmentFirewallsAssignmentsAttributes{}
	targetDesc := ""
	if serverID != "" {
		attributes.ServerID = &serverID
		targetDesc = "server " + serverID
	} else {
		attributes.VirtualMachineID = &virtualMachineID
		targetDesc = "virtual machine " + virtualMachineID
	}

	createRequest := operations.CreateFirewallAssignmentFirewallsAssignmentsRequestBody{
		Data: operations.CreateFirewallAssignmentFirewallsAssignmentsData{
			Type:       operations.CreateFirewallAssignmentFirewallsAssignmentsTypeFirewallAssignments,
			Attributes: attributes,
		},
	}

	result, err := r.client.Firewalls.Assignments.Create(ctx, firewallID, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create firewall assignment for firewall %s and %s, got error: %s", firewallID, targetDesc, err.Error()))
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
	r.findAssignmentByTargetAndFirewall(ctx, &data, &resp.Diagnostics)
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

// Helper function to find the assignment ID by filtering the firewall's
// assignments on the configured target (server_id or virtual_machine_id).
func (r *FirewallAssignmentResource) findAssignmentByTargetAndFirewall(ctx context.Context, data *FirewallAssignmentResourceModel, diags *diag.Diagnostics) {
	firewallID := data.FirewallID.ValueString()
	serverID := data.ServerID.ValueString()
	virtualMachineID := data.VirtualMachineID.ValueString()

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

	// Look for the assignment matching the firewall ID and the configured target
	for _, assignment := range response.FirewallAssignments.Data {
		if assignment.Attributes != nil && assignment.ID != nil {
			var assignmentServerID string
			var assignmentVirtualMachineID string
			var assignmentFirewallID string

			// The API returns the target as server XOR virtual_machine
			if assignment.Attributes.Server != nil && assignment.Attributes.Server.ID != nil {
				assignmentServerID = *assignment.Attributes.Server.ID
			}
			if assignment.Attributes.VirtualMachine != nil && assignment.Attributes.VirtualMachine.ID != nil {
				assignmentVirtualMachineID = *assignment.Attributes.VirtualMachine.ID
			}

			// Get firewall ID from assignment
			if assignment.Attributes.FirewallID != nil {
				assignmentFirewallID = *assignment.Attributes.FirewallID
			}

			if assignmentFirewallID != firewallID {
				continue
			}

			matchesServer := serverID != "" && assignmentServerID == serverID
			matchesVirtualMachine := virtualMachineID != "" && assignmentVirtualMachineID == virtualMachineID
			if matchesServer || matchesVirtualMachine {
				data.ID = types.StringValue(*assignment.ID)
				return
			}
		}
	}

	// If we get here, we couldn't find the matching assignment
	diags.AddError("API Error", "Assignment was created but couldn't find it in the list with matching target and firewall_id")
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
		if strings.Contains(err.Error(), "404") {
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

		// The API returns the target as server XOR virtual_machine. Mirror
		// that exactly: set the present side and null the other, so the unset
		// attribute ends the apply known (null) instead of unknown.
		if assignment.Attributes.Server != nil && assignment.Attributes.Server.ID != nil {
			data.ServerID = types.StringValue(*assignment.Attributes.Server.ID)
		} else {
			data.ServerID = types.StringNull()
		}

		if assignment.Attributes.VirtualMachine != nil && assignment.Attributes.VirtualMachine.ID != nil {
			data.VirtualMachineID = types.StringValue(*assignment.Attributes.VirtualMachine.ID)
		} else {
			data.VirtualMachineID = types.StringNull()
		}
	}
}

func (r *FirewallAssignmentResource) findFirewallForAssignment(ctx context.Context, data *FirewallAssignmentResourceModel, diags *diag.Diagnostics) {
	assignmentID := data.ID.ValueString()

	response, err := r.client.Firewalls.List(ctx, nil, nil, nil, nil)
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
