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
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &UserDataResource{}
var _ resource.ResourceWithImportState = &UserDataResource{}

func NewUserDataResource() resource.Resource {
	return &UserDataResource{}
}

type UserDataResource struct {
	client *latitudeshgosdk.Latitudesh
}

type UserDataResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Description types.String `tfsdk:"description"`
	Content     types.String `tfsdk:"content"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func (r *UserDataResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user_data"
}

func (r *UserDataResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "User Data resource",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "User Data identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "The User Data description",
				Required:            true,
			},
			"content": schema.StringAttribute{
				MarkdownDescription: "Base64 encoded content of the User Data",
				Required:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "The timestamp for when the User Data was created",
				Computed:            true,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "The timestamp for the last time the User Data was updated",
				Computed:            true,
			},
		},
	}
}

func (r *UserDataResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *UserDataResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data UserDataResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	description := data.Description.ValueString()
	content := data.Content.ValueString()

	createRequest := operations.PostUserDataUserDataRequestBody{
		Data: operations.PostUserDataUserDataData{
			Type: operations.PostUserDataUserDataTypeUserData,
			Attributes: &operations.PostUserDataUserDataAttributes{
				Description: description,
				Content:     content,
			},
		},
	}

	result, err := r.client.UserData.CreateNew(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create user data, got error: "+err.Error())
		return
	}

	if result.UserData == nil || result.UserData.Data == nil || result.UserData.Data.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get user data ID from response")
		return
	}

	data.ID = types.StringValue(*result.UserData.Data.ID)

	// Read the resource to populate all attributes
	r.readUserData(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *UserDataResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data UserDataResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readUserData(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *UserDataResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data UserDataResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	userDataID := data.ID.ValueString()
	description := data.Description.ValueString()
	content := data.Content.ValueString()

	updateRequest := &operations.PatchUserDataUserDataRequestBody{
		Data: operations.PatchUserDataUserDataData{
			ID:   userDataID,
			Type: operations.PatchUserDataUserDataTypeUserData,
			Attributes: &operations.PatchUserDataUserDataAttributes{
				Description: &description,
				Content:     &content,
			},
		},
	}

	_, err := r.client.UserData.Update(ctx, userDataID, updateRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to update user data, got error: "+err.Error())
		return
	}

	// Read the resource to populate all attributes
	r.readUserData(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *UserDataResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data UserDataResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	userDataID := data.ID.ValueString()

	_, err := r.client.UserData.Delete(ctx, userDataID)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to delete user data, got error: "+err.Error())
		return
	}
}

func (r *UserDataResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var data UserDataResourceModel
	data.ID = types.StringValue(req.ID)

	r.readUserData(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *UserDataResource) readUserData(ctx context.Context, data *UserDataResourceModel, diags *diag.Diagnostics) {
	userDataID := data.ID.ValueString()

	// Get decoded content for display
	decodedContent := "decoded_content"
	response, err := r.client.UserData.Retrieve(ctx, userDataID, &decodedContent)
	if err != nil {
		diags.AddError("Client Error", "Unable to read user data, got error: "+err.Error())
		return
	}

	if response.UserData == nil || response.UserData.Data == nil {
		data.ID = types.StringNull()
		return
	}

	userData := response.UserData.Data
	if userData.Attributes != nil {
		if userData.Attributes.Description != nil {
			data.Description = types.StringValue(*userData.Attributes.Description)
		}

		if userData.Attributes.Content != nil {
			data.Content = types.StringValue(*userData.Attributes.Content)
		}

		if userData.Attributes.CreatedAt != nil {
			data.CreatedAt = types.StringValue(*userData.Attributes.CreatedAt)
		}

		if userData.Attributes.UpdatedAt != nil {
			data.UpdatedAt = types.StringValue(*userData.Attributes.UpdatedAt)
		}
	}
}
