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
)

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
		color := normalizeHexColor(data.Color.ValueString())
		attrs.Color = &color
	}

	createTagType := operations.CreateTagTagsTypeTags
	createRequest := operations.CreateTagTagsRequestBody{
		Data: &operations.CreateTagTagsData{
			Type:       &createTagType,
			Attributes: attrs,
		},
	}

	result, err := r.client.Tags.Create(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create tag, got error: "+err.Error())
		return
	}

	// Try to get ID from response first
	if result.CustomTag != nil && result.CustomTag.ID != nil {
		data.ID = types.StringValue(*result.CustomTag.ID)
	} else {
		// Fallback: API might have created the tag but not returned the ID
		// Use List to find the tag by name
		tagID, err := r.findTagByName(ctx, name)
		if err != nil {
			resp.Diagnostics.AddError("API Error", "Failed to get tag ID from response and unable to find created tag: "+err.Error())
			return
		}
		data.ID = types.StringValue(tagID)
	}

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
		color := normalizeHexColor(data.Color.ValueString())
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

	_, err := r.client.Tags.Update(ctx, tagID, updateRequest)
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

	_, err := r.client.Tags.Delete(ctx, tagID)
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

	// Use List to find the specific tag since there's no Get method
	response, err := r.client.Tags.List(ctx)
	if err != nil {
		diags.AddError("Client Error", "Unable to read tags, got error: "+err.Error())
		return
	}

	if response.CustomTags == nil || response.CustomTags.Data == nil {
		data.ID = types.StringNull()
		return
	}

	// Find our tag in the list
	var foundTag *components.CustomTagData
	for _, tag := range response.CustomTags.Data {
		if tag.ID != nil && *tag.ID == tagID {
			foundTag = &tag
			break
		}
	}

	if foundTag == nil {
		data.ID = types.StringNull()
		return
	}

	if foundTag.Attributes != nil {
		if foundTag.Attributes.Name != nil {
			data.Name = types.StringValue(*foundTag.Attributes.Name)
		}

		if foundTag.Attributes.Description != nil {
			data.Description = types.StringValue(*foundTag.Attributes.Description)
		}

		if foundTag.Attributes.Color != nil {
			data.Color = types.StringValue(normalizeHexColor(*foundTag.Attributes.Color))
		}

		if foundTag.Attributes.Slug != nil {
			data.Slug = types.StringValue(*foundTag.Attributes.Slug)
		}
	}
}

// findTagByName finds a tag by name and returns its ID
func (r *TagResource) findTagByName(ctx context.Context, name string) (string, error) {
	response, err := r.client.Tags.List(ctx)
	if err != nil {
		return "", err
	}

	if response.CustomTags == nil || response.CustomTags.Data == nil {
		return "", fmt.Errorf("no tags found")
	}

	// Find the tag by name
	for _, tag := range response.CustomTags.Data {
		if tag.ID != nil && tag.Attributes != nil && tag.Attributes.Name != nil && *tag.Attributes.Name == name {
			return *tag.ID, nil
		}
	}

	return "", fmt.Errorf("tag with name '%s' not found", name)
}

// normalizeHexColor normalizes hex color codes to uppercase for consistency
func normalizeHexColor(color string) string {
	if color == "" {
		return color
	}
	return strings.ToUpper(color)
}
