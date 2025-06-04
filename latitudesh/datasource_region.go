package latitudesh

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// Ensure provider defined types fully satisfy framework interfaces.
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

	// If a specific region ID is provided, get that region
	if !data.ID.IsNull() && !data.ID.IsUnknown() {
		regionID := data.ID.ValueString()
		response, err := d.client.Regions.GetRegion(ctx, regionID)
		if err != nil {
			resp.Diagnostics.AddError("Client Error", "Unable to read region, got error: "+err.Error())
			return
		}

		if response.Region == nil || response.Region.Data == nil {
			resp.Diagnostics.AddError("API Error", "Region not found")
			return
		}

		d.mapRegionDataToModel(response.Region.Data, &data)
	} else {
		// Get all regions and find the one matching the criteria
		response, err := d.client.Regions.GetRegions(ctx, nil, nil)
		if err != nil {
			resp.Diagnostics.AddError("Client Error", "Unable to read regions, got error: "+err.Error())
			return
		}

		if response.Regions == nil || response.Regions.Data == nil {
			resp.Diagnostics.AddError("API Error", "No regions found")
			return
		}

		var matchedRegion *components.RegionsData
		for _, region := range response.Regions.Data {
			if d.regionMatches(&region, &data) {
				matchedRegion = &region
				break
			}
		}

		if matchedRegion == nil {
			resp.Diagnostics.AddError("Not Found", "No region found matching the specified criteria")
			return
		}

		d.mapRegionsDataToModel(matchedRegion, &data)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *RegionDataSource) regionMatches(region *components.RegionsData, data *RegionDataSourceModel) bool {
	if !data.Name.IsNull() && !data.Name.IsUnknown() {
		if region.Attributes == nil || region.Attributes.Name == nil || *region.Attributes.Name != data.Name.ValueString() {
			return false
		}
	}

	if !data.Slug.IsNull() && !data.Slug.IsUnknown() {
		if region.Attributes == nil || region.Attributes.Slug == nil || *region.Attributes.Slug != data.Slug.ValueString() {
			return false
		}
	}

	return true
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

func (d *RegionDataSource) mapRegionsDataToModel(region *components.RegionsData, data *RegionDataSourceModel) {
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
