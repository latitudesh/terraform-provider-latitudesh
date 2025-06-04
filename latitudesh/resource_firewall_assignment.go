package latitudesh

import (
	"context"
	"fmt"
	"net/http"
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

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &FirewallAssignmentResource{}
var _ resource.ResourceWithImportState = &FirewallAssignmentResource{}

func NewFirewallAssignmentResource() resource.Resource {
	return &FirewallAssignmentResource{}
}

// FirewallAssignmentResource defines the resource implementation.
type FirewallAssignmentResource struct {
	client *latitudeshgosdk.Latitudesh
}

// FirewallAssignmentResourceModel describes the resource data model.
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
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"server_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the server to assign the firewall to",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *FirewallAssignmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
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

func (r *FirewallAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data FirewallAssignmentResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	firewallID := data.FirewallID.ValueString()
	serverID := data.ServerID.ValueString()

	createRequest := operations.CreateFirewallAssignmentFirewallsAssignmentsRequestBody{
		Data: operations.CreateFirewallAssignmentFirewallsAssignmentsData{
			Type: operations.CreateFirewallAssignmentFirewallsAssignmentsTypeFirewallAssignments,
			Attributes: &operations.CreateFirewallAssignmentFirewallsAssignmentsAttributes{
				ServerID: serverID,
			},
		},
	}

	response, err := r.client.Firewalls.Assignments.Create(ctx, firewallID, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create firewall assignment, got error: "+err.Error())
		return
	}

	if response.FirewallServer == nil {
		resp.Diagnostics.AddError("API Error", "Unexpected response structure from API")
		return
	}

	assignment := response.FirewallServer
	if assignment.ID == nil {
		resp.Diagnostics.AddError("API Error", "Firewall assignment ID not returned from API")
		return
	}

	// Create a composite ID for the assignment: firewallID:assignmentID
	data.ID = types.StringValue(fmt.Sprintf("%s:%s", firewallID, *assignment.ID))

	if assignment.Attributes != nil {
		if assignment.Attributes.FirewallID != nil {
			data.FirewallID = types.StringValue(*assignment.Attributes.FirewallID)
		}
		if assignment.Attributes.Server != nil && assignment.Attributes.Server.ID != nil {
			data.ServerID = types.StringValue(*assignment.Attributes.Server.ID)
		}
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
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

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Parse the composite ID: firewallID:assignmentID
	idParts := strings.Split(data.ID.ValueString(), ":")
	if len(idParts) != 2 {
		resp.Diagnostics.AddError("ID Parse Error", "Invalid resource ID format, expected firewallID:assignmentID")
		return
	}

	firewallID := idParts[0]

	response, err := r.client.Firewalls.GetFirewallAssignments(ctx, firewallID, nil, nil)
	if err != nil {
		// If we can't find it, assume it's already deleted
		return
	}

	if response.FirewallServer != nil && response.FirewallServer.ID != nil {
		assignmentID := *response.FirewallServer.ID
		_, err := r.client.Firewalls.DeleteFirewallAssignment(ctx, firewallID, assignmentID)
		if err != nil {
			resp.Diagnostics.AddError("Client Error", "Unable to delete firewall assignment, got error: "+err.Error())
			return
		}
	}
}

func (r *FirewallAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var data FirewallAssignmentResourceModel
	data.ID = types.StringValue(req.ID)

	r.readFirewallAssignment(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FirewallAssignmentResource) readFirewallAssignment(ctx context.Context, data *FirewallAssignmentResourceModel, diags *diag.Diagnostics) {
	// Parse the composite ID: firewallID:assignmentID
	idParts := strings.Split(data.ID.ValueString(), ":")
	if len(idParts) != 2 {
		diags.AddError("ID Parse Error", "Invalid resource ID format, expected firewallID:assignmentID")
		return
	}

	firewallID := idParts[0]
	assignmentID := idParts[1]

	// Get firewall assignments to find our specific assignment
	response, err := r.client.Firewalls.GetFirewallAssignments(ctx, firewallID, nil, nil)
	if err != nil {
		if apiErr, ok := err.(*components.APIError); ok && apiErr.StatusCode == http.StatusNotFound {
			data.ID = types.StringNull()
			return
		}
		diags.AddError("Client Error", "Unable to read firewall assignment, got error: "+err.Error())
		return
	}

	if response.FirewallServer == nil {
		data.ID = types.StringNull()
		return
	}

	// Check if this is our assignment
	if response.FirewallServer.ID == nil || *response.FirewallServer.ID != assignmentID {
		data.ID = types.StringNull()
		return
	}

	assignment := response.FirewallServer

	data.FirewallID = types.StringValue(firewallID)

	if assignment.Attributes != nil && assignment.Attributes.Server != nil && assignment.Attributes.Server.ID != nil {
		data.ServerID = types.StringValue(*assignment.Attributes.Server.ID)
	}
}
