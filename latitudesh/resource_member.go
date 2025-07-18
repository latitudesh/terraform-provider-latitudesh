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
var _ resource.Resource = &MemberResource{}
var _ resource.ResourceWithImportState = &MemberResource{}

func NewMemberResource() resource.Resource {
	return &MemberResource{}
}

type MemberResource struct {
	client *latitudeshgosdk.Latitudesh
}

type MemberResourceModel struct {
	ID        types.String `tfsdk:"id"`
	FirstName types.String `tfsdk:"first_name"`
	LastName  types.String `tfsdk:"last_name"`
	Email     types.String `tfsdk:"email"`
	Role      types.String `tfsdk:"role"`
	// Computed fields
	MfaEnabled  types.Bool   `tfsdk:"mfa_enabled"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
	LastLoginAt types.String `tfsdk:"last_login_at"`
}

func (r *MemberResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_member"
}

func (r *MemberResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Team Member resource",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Member identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"first_name": schema.StringAttribute{
				MarkdownDescription: "First name of the team member",
				Optional:            true,
			},
			"last_name": schema.StringAttribute{
				MarkdownDescription: "Last name of the team member",
				Optional:            true,
			},
			"email": schema.StringAttribute{
				MarkdownDescription: "Email address of the team member",
				Optional:            true,
				Computed:            true,
			},
			"role": schema.StringAttribute{
				MarkdownDescription: "Role of the team member (owner, administrator, collaborator, billing)",
				Optional:            true,
				Computed:            true,
			},
			// Computed attributes
			"mfa_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether MFA is enabled for this member",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Creation timestamp",
				Computed:            true,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "Last update timestamp",
				Computed:            true,
			},
			"last_login_at": schema.StringAttribute{
				MarkdownDescription: "Last login timestamp",
				Computed:            true,
			},
		},
	}
}

func (r *MemberResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *MemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data MemberResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate that required fields are provided during creation
	if data.Email.IsNull() || data.Email.ValueString() == "" {
		resp.Diagnostics.AddError(
			"Missing Required Field",
			"The email field is required when creating a member.",
		)
		return
	}

	if data.Role.IsNull() || data.Role.ValueString() == "" {
		resp.Diagnostics.AddError(
			"Missing Required Field",
			"The role field is required when creating a member.",
		)
		return
	}

	// Prepare attributes for creation
	attrs := &operations.PostTeamMembersTeamMembersAttributes{
		Email: data.Email.ValueString(),
		Role:  operations.PostTeamMembersRole(data.Role.ValueString()),
	}

	if !data.FirstName.IsNull() {
		firstName := data.FirstName.ValueString()
		attrs.FirstName = &firstName
	}

	if !data.LastName.IsNull() {
		lastName := data.LastName.ValueString()
		attrs.LastName = &lastName
	}

	createRequest := operations.PostTeamMembersTeamMembersRequestBody{
		Data: operations.PostTeamMembersTeamMembersData{
			Type:       operations.PostTeamMembersTeamMembersTypeMemberships,
			Attributes: attrs,
		},
	}

	result, err := r.client.TeamMembers.PostTeamMembers(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create team member, got error: "+err.Error())
		return
	}

	if result.Membership == nil || result.Membership.Data == nil || result.Membership.Data.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get team member ID from response")
		return
	}

	data.ID = types.StringValue(*result.Membership.Data.ID)

	// Read the resource to populate all attributes
	r.readMember(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data MemberResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readMember(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MemberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Team members cannot be updated via the API
	// They need to be deleted and recreated, or updated through other means
	resp.Diagnostics.AddError("Update Not Supported", "Team members cannot be updated directly. Please recreate the resource.")
}

func (r *MemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data MemberResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	memberID := data.ID.ValueString()

	_, err := r.client.TeamMembers.Delete(ctx, memberID)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to delete team member, got error: "+err.Error())
		return
	}
}

func (r *MemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var data MemberResourceModel
	data.ID = types.StringValue(req.ID)

	r.readMember(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MemberResource) readMember(ctx context.Context, data *MemberResourceModel, diags *diag.Diagnostics) {
	memberID := data.ID.ValueString()

	// Get all team members and find ours
	response, err := r.client.Teams.Members.GetTeamMembers(ctx, nil, nil)
	if err != nil {
		diags.AddError("Client Error", "Unable to read team members, got error: "+err.Error())
		return
	}

	if response.TeamMembers == nil || response.TeamMembers.Data == nil {
		data.ID = types.StringNull()
		return
	}

	// Find our member by ID (note: we need to match by email since members don't have exposed IDs in the list)
	var member *components.TeamMembersData
	for _, m := range response.TeamMembers.Data {
		// Since the API doesn't provide IDs in team member listings,
		// we'll need to use email for identification
		if m.Email != nil && *m.Email == memberID {
			member = &m
			break
		}
	}

	if member == nil {
		data.ID = types.StringNull()
		return
	}

	// Populate the data model
	if member.FirstName != nil {
		data.FirstName = types.StringValue(*member.FirstName)
	}

	if member.LastName != nil {
		data.LastName = types.StringValue(*member.LastName)
	}

	if member.Email != nil {
		data.Email = types.StringValue(*member.Email)
	}

	if member.Role != nil && member.Role.Name != nil {
		data.Role = types.StringValue(*member.Role.Name)
	}

	if member.MfaEnabled != nil {
		data.MfaEnabled = types.BoolValue(*member.MfaEnabled)
	}

	// Note: creation/update timestamps are not available in the current API response
	// These would need to be populated if the API provides them
}
