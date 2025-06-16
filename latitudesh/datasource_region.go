package latitudesh

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

var _ datasource.DataSource = &RegionDataSource{}

func NewRegionDataSource() datasource.DataSource {
	return &RegionDataSource{}
}

type RegionDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type RegionDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Slug        types.String `tfsdk:"slug"`
	Country     types.String `tfsdk:"country"`
	CountryCode types.String `tfsdk:"country_code"`
	City        types.String `tfsdk:"city"`
}

func (d *RegionDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_region"
}

func (d *RegionDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Region data source",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Region identifier",
				Optional:            true,
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Region name",
				Optional:            true,
				Computed:            true,
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "Region slug",
				Optional:            true,
				Computed:            true,
			},
			"country": schema.StringAttribute{
				MarkdownDescription: "Country name",
				Computed:            true,
			},
			"country_code": schema.StringAttribute{
				MarkdownDescription: "Country code",
				Computed:            true,
			},
			"city": schema.StringAttribute{
				MarkdownDescription: "City name",
				Computed:            true,
			},
		},
	}
}

func (d *RegionDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*latitudeshgosdk.Latitudesh)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			"Expected *latitudeshgosdk.Latitudesh, got: %T. Please report this issue to the provider developers.",
		)
		return
	}

	d.client = client
}

func (d *RegionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data RegionDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Check that either ID or slug is provided
	if data.ID.IsNull() && data.Slug.IsNull() {
		resp.Diagnostics.AddError(
			"Missing Required Attribute",
			"Either 'id' or 'slug' must be provided to look up a region.",
		)
		return
	}

	var region *components.RegionData

	if !data.ID.IsNull() {
		// Look up by ID
		regionID := data.ID.ValueString()
		result, err := d.client.Regions.Fetch(ctx, regionID)
		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read region %s, got error: %s", regionID, err.Error()))
			return
		}
		if result.Region != nil && result.Region.Data != nil {
			region = result.Region.Data
		}
	} else {
		// Look up by slug - get all regions and find matching slug
		slug := data.Slug.ValueString()
		result, err := d.client.Regions.Get(ctx, nil, nil)
		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to search for region with slug %s, got error: %s", slug, err.Error()))
			return
		}
		if result.Regions != nil && result.Regions.Data != nil {
			for _, r := range result.Regions.Data {
				if r.Attributes != nil && r.Attributes.Slug != nil && *r.Attributes.Slug == slug {
					// Convert RegionsData to RegionData format
					var regionCountry *components.RegionCountry
					if r.Attributes.Country != nil {
						regionCountry = &components.RegionCountry{
							Name: r.Attributes.Country.Name,
							Slug: r.Attributes.Country.Slug,
						}
					}
					region = &components.RegionData{
						ID: r.ID,
						Attributes: &components.RegionAttributes{
							Name:    r.Attributes.Name,
							Slug:    r.Attributes.Slug,
							Country: regionCountry,
						},
					}
					break
				}
			}
		}
	}

	if region == nil {
		resp.Diagnostics.AddError("Not Found", "No region found matching the specified criteria")
		return
	}

	d.mapRegionDataToModel(region, &data)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *RegionDataSource) mapRegionDataToModel(region *components.RegionData, data *RegionDataSourceModel) {
	if region.ID != nil {
		data.ID = types.StringValue(*region.ID)
	}

	if region.Attributes != nil {
		if region.Attributes.Name != nil {
			data.Name = types.StringValue(*region.Attributes.Name)
		}

		if region.Attributes.Slug != nil {
			data.Slug = types.StringValue(*region.Attributes.Slug)
		}

		if region.Attributes.Country != nil {
			if region.Attributes.Country.Name != nil {
				data.Country = types.StringValue(*region.Attributes.Country.Name)
			}
			if region.Attributes.Country.Slug != nil {
				data.CountryCode = types.StringValue(*region.Attributes.Country.Slug)
			}
		}
	}
}
