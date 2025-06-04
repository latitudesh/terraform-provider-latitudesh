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
var _ resource.Resource = &TagResource{}
var _ resource.ResourceWithImportState = &TagResource{}

func NewTagResource() resource.Resource {
	return &TagResource{}
}

type TagResource struct {
	client *latitudeshgosdk.Latitudesh
}

type TagResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Color       types.String `tfsdk:"color"`
	Slug        types.String `tfsdk:"slug"`
}

func (r *TagResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tag"
}

func (r *TagResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Tag resource",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Tag identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The tag name",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "The tag description",
				Optional:            true,
			},
			"color": schema.StringAttribute{
				MarkdownDescription: "The tag color (hex color code)",
				Optional:            true,
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "The tag slug",
				Computed:            true,
			},
		},
	}
}

func (r *TagResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *TagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data TagResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := data.Name.ValueString()

	attrs := &operations.CreateTagTagsAttributes{
		Name: &name,
	}

	// Add optional description
	if !data.Description.IsNull() {
		desc := data.Description.ValueString()
		attrs.Description = &desc
	}

	// Add optional color
	if !data.Color.IsNull() {
		color := data.Color.ValueString()
		attrs.Color = &color
	}

	createTagType := operations.CreateTagTagsTypeTags
	createRequest := operations.CreateTagTagsRequestBody{
		Data: &operations.CreateTagTagsData{
			Type:       &createTagType,
			Attributes: attrs,
		},
	}

	result, err := r.client.Tags.CreateTag(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create tag, got error: "+err.Error())
		return
	}

	if result.CustomTag == nil || result.CustomTag.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get tag ID from response")
		return
	}

	data.ID = types.StringValue(*result.CustomTag.ID)

	// Read the resource to populate all attributes
	r.readTag(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *TagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data TagResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readTag(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *TagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data TagResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tagID := data.ID.ValueString()
	name := data.Name.ValueString()

	attrs := &operations.UpdateTagTagsAttributes{
		Name: &name,
	}

	// Add optional description
	if !data.Description.IsNull() {
		desc := data.Description.ValueString()
		attrs.Description = &desc
	}

	// Add optional color
	if !data.Color.IsNull() {
		color := data.Color.ValueString()
		attrs.Color = &color
	}

	updateTagType := operations.UpdateTagTagsTypeTags
	updateRequest := operations.UpdateTagTagsRequestBody{
		Data: &operations.UpdateTagTagsData{
			ID:         &tagID,
			Type:       &updateTagType,
			Attributes: attrs,
		},
	}

	_, err := r.client.Tags.UpdateTag(ctx, tagID, updateRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to update tag, got error: "+err.Error())
		return
	}

	// Read the resource to populate all attributes
	r.readTag(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *TagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data TagResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tagID := data.ID.ValueString()

	_, err := r.client.Tags.DestroyTag(ctx, tagID)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to delete tag, got error: "+err.Error())
		return
	}
}

func (r *TagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var data TagResourceModel
	data.ID = types.StringValue(req.ID)

	r.readTag(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *TagResource) readTag(ctx context.Context, data *TagResourceModel, diags *diag.Diagnostics) {
	tagID := data.ID.ValueString()

	// Use GetTags to find the specific tag since there's no GetTag method
	response, err := r.client.Tags.GetTags(ctx)
	if err != nil {
		diags.AddError("Client Error", "Unable to read tags, got error: "+err.Error())
		return
	}

	if response.CustomTag == nil {
		data.ID = types.StringNull()
		return
	}

	// The GetTags response appears to return a single CustomTag, not a list
	// If the ID matches, use it; otherwise the tag doesn't exist
	if response.CustomTag.ID != nil && *response.CustomTag.ID == tagID {
		if response.CustomTag.Attributes != nil {
			if response.CustomTag.Attributes.Name != nil {
				data.Name = types.StringValue(*response.CustomTag.Attributes.Name)
			}

			if response.CustomTag.Attributes.Description != nil {
				data.Description = types.StringValue(*response.CustomTag.Attributes.Description)
			}

			if response.CustomTag.Attributes.Color != nil {
				data.Color = types.StringValue(*response.CustomTag.Attributes.Color)
			}

			if response.CustomTag.Attributes.Slug != nil {
				data.Slug = types.StringValue(*response.CustomTag.Attributes.Slug)
			}
		}
	} else {
		data.ID = types.StringNull()
		return
	}
}
