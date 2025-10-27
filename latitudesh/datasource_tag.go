package latitudesh

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/internal/provider"
)

var _ datasource.DataSource = &TagDataSource{}

func NewTagDataSource() datasource.DataSource {
	return &TagDataSource{}
}

type TagDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type TagDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Slug        types.String `tfsdk:"slug"`
	Description types.String `tfsdk:"description"`
	Color       types.String `tfsdk:"color"`
}

func (d *TagDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tag"
}

func (d *TagDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Tag data source - lookup tags by ID, name, or slug",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Tag ID to look up. Mutually exclusive with name and slug.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("id"),
						path.MatchRoot("name"),
						path.MatchRoot("slug"),
					),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Tag name to look up. Mutually exclusive with id and slug.",
				Optional:            true,
				Computed:            true,
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "Tag slug to look up. Mutually exclusive with id and name.",
				Optional:            true,
				Computed:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "The tag description",
				Computed:            true,
			},
			"color": schema.StringAttribute{
				MarkdownDescription: "The tag color (hex color code, e.g., #ff0000)",
				Computed:            true,
			},
		},
	}
}

func (d *TagDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *TagDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data TagDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate that at least one selector is provided
	if data.ID.IsNull() && data.Name.IsNull() && data.Slug.IsNull() {
		resp.Diagnostics.AddError(
			"Missing Required Attribute",
			"At least one of id, name, or slug must be specified",
		)
		return
	}

	// Fetch all tags from the API
	response, err := d.client.Tags.List(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read tags, got error: %s", err.Error()))
		return
	}

	if response.CustomTags == nil || response.CustomTags.Data == nil {
		resp.Diagnostics.AddError("Not Found", "No tags found")
		return
	}

	// Find the matching tag
	var foundTag *components.CustomTagData
	for _, tag := range response.CustomTags.Data {
		match := false

		// Match by ID
		if !data.ID.IsNull() && tag.ID != nil && *tag.ID == data.ID.ValueString() {
			match = true
		}

		// Match by name
		if !data.Name.IsNull() && tag.Attributes != nil && tag.Attributes.Name != nil &&
			*tag.Attributes.Name == data.Name.ValueString() {
			match = true
		}

		// Match by slug
		if !data.Slug.IsNull() && tag.Attributes != nil && tag.Attributes.Slug != nil &&
			*tag.Attributes.Slug == data.Slug.ValueString() {
			match = true
		}

		if match {
			foundTag = &tag
			break
		}
	}

	if foundTag == nil {
		selector := ""
		if !data.ID.IsNull() {
			selector = fmt.Sprintf("ID '%s'", data.ID.ValueString())
		} else if !data.Name.IsNull() {
			selector = fmt.Sprintf("name '%s'", data.Name.ValueString())
		} else if !data.Slug.IsNull() {
			selector = fmt.Sprintf("slug '%s'", data.Slug.ValueString())
		}
		resp.Diagnostics.AddError("Not Found", fmt.Sprintf("Tag with %s not found", selector))
		return
	}

	// Map the found tag to the data model
	d.mapTagToModel(foundTag, &data)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// mapTagToModel maps a tag from the API response to the data source model
func (d *TagDataSource) mapTagToModel(tag *components.CustomTagData, data *TagDataSourceModel) {
	if tag.ID != nil {
		data.ID = types.StringValue(*tag.ID)
	}

	if tag.Attributes != nil {
		if tag.Attributes.Name != nil {
			data.Name = types.StringValue(*tag.Attributes.Name)
		}

		if tag.Attributes.Slug != nil {
			data.Slug = types.StringValue(*tag.Attributes.Slug)
		}

		if tag.Attributes.Description != nil && *tag.Attributes.Description != "" {
			data.Description = types.StringValue(*tag.Attributes.Description)
		} else {
			data.Description = types.StringNull()
		}

		if tag.Attributes.Color != nil {
			data.Color = types.StringValue(*tag.Attributes.Color)
		}
	}
}
