package latitudesh

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"

	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var (
	_ datasource.DataSource              = &LksSitesDataSource{}
	_ datasource.DataSourceWithConfigure = &LksSitesDataSource{}
)

func NewLksSitesDataSource() datasource.DataSource {
	return &LksSitesDataSource{}
}

type LksSitesDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type LksSitesDataSourceModel struct {
	// Synthetic identifier: the lookup takes no arguments, so there is nothing
	// to key it on.
	ID types.String `tfsdk:"id"`

	Sites types.List `tfsdk:"sites"`
}

type LksSiteItemModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Slug        types.String `tfsdk:"slug"`
	Facility    types.String `tfsdk:"facility"`
	Country     types.String `tfsdk:"country"`
	CountryCode types.String `tfsdk:"country_code"`
}

var lksSiteItemObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":           types.StringType,
		"name":         types.StringType,
		"slug":         types.StringType,
		"facility":     types.StringType,
		"country":      types.StringType,
		"country_code": types.StringType,
	},
}

// lksSiteItemValue maps one SDK site into the list item model. `country` is
// flattened into name + code the same way datasource_region.go does, rather
// than nested, so the two read alike.
func lksSiteItemValue(s *components.LksSiteData) LksSiteItemModel {
	item := LksSiteItemModel{
		ID:          types.StringPointerValue(s.ID),
		Name:        types.StringNull(),
		Slug:        types.StringNull(),
		Facility:    types.StringNull(),
		Country:     types.StringNull(),
		CountryCode: types.StringNull(),
	}

	if s.Attributes == nil {
		return item
	}

	item.Name = types.StringPointerValue(s.Attributes.Name)
	item.Slug = types.StringPointerValue(s.Attributes.Slug)
	item.Facility = types.StringPointerValue(s.Attributes.Facility)

	if country := s.Attributes.Country; country != nil {
		item.Country = types.StringPointerValue(country.Name)
		item.CountryCode = types.StringPointerValue(country.Slug)
	}

	return item
}

func (d *LksSitesDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lks_sites"
}

func (d *LksSitesDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *LksSitesDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "LKS (Latitude Kubernetes Service) sites data source - the sites a cluster can be deployed to. This is the authoritative source for `latitudesh_lks`'s `site` argument: LKS runs in its own subset of locations, so `latitudesh_region` is not a substitute. The lookup is global and takes no arguments.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Synthetic identifier. The lookup takes no arguments, so this is always `all`.",
				Computed:            true,
			},
			"sites": schema.ListNestedAttribute{
				MarkdownDescription: "Every site LKS can deploy a cluster to, in the order the API returns them.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "Site identifier. The API documents this as the slug to pass as `site` when creating a cluster, and it matches `slug` below.",
							Computed:            true,
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Human-readable site name.",
							Computed:            true,
						},
						"slug": schema.StringAttribute{
							MarkdownDescription: "Site slug — this is what `latitudesh_lks.site` takes.",
							Computed:            true,
						},
						"facility": schema.StringAttribute{
							MarkdownDescription: "Datacenter facility backing the site.",
							Computed:            true,
						},
						"country": schema.StringAttribute{
							MarkdownDescription: "Country name.",
							Computed:            true,
						},
						"country_code": schema.StringAttribute{
							MarkdownDescription: "Country code.",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *LksSitesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data LksSitesDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	// ListLksSites takes no parameters and returns no pagination metadata: one
	// call is the whole collection.
	res, err := d.client.Lks.ListLksSites(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to list LKS sites, got error: "+err.Error())
		return
	}

	data.ID = types.StringValue("all")

	items := make([]LksSiteItemModel, 0)
	if res != nil && res.LksSites != nil {
		sites := res.LksSites.Data
		for i := range sites {
			items = append(items, lksSiteItemValue(&sites[i]))
		}
	}

	list, diags := types.ListValueFrom(ctx, lksSiteItemObjectType, items)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Sites = list

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
