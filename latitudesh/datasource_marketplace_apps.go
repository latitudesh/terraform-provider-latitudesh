package latitudesh

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"

	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var (
	_ datasource.DataSource              = &MarketplaceAppsDataSource{}
	_ datasource.DataSourceWithConfigure = &MarketplaceAppsDataSource{}
)

func NewMarketplaceAppsDataSource() datasource.DataSource {
	return &MarketplaceAppsDataSource{}
}

type MarketplaceAppsDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type MarketplaceAppsDataSourceModel struct {
	// Synthetic identifier: the category filter, or "all" when unfiltered.
	ID types.String `tfsdk:"id"`

	// Optional client-side filter.
	Category types.String `tfsdk:"category"`

	// Result set.
	Apps types.List `tfsdk:"apps"`
}

// MarketplaceAppItemModel mirrors the read-only attributes of the singular
// marketplace app data source for each entry in the list.
type MarketplaceAppItemModel struct {
	ID                     types.String `tfsdk:"id"`
	Name                   types.String `tfsdk:"name"`
	Slug                   types.String `tfsdk:"slug"`
	Description            types.String `tfsdk:"description"`
	ShortDescription       types.String `tfsdk:"short_description"`
	Category               types.String `tfsdk:"category"`
	Version                types.String `tfsdk:"version"`
	SystemRequirements     types.Object `tfsdk:"system_requirements"`
	DeploymentStrategy     types.String `tfsdk:"deployment_strategy"`
	DefaultOperatingSystem types.String `tfsdk:"default_operating_system"`
	CompatiblePlans        types.List   `tfsdk:"compatible_plans"`
	AccessInstructions     types.String `tfsdk:"access_instructions"`
	UpstreamURL            types.String `tfsdk:"upstream_url"`
	DocumentationURL       types.String `tfsdk:"documentation_url"`
	LogoURL                types.String `tfsdk:"logo_url"`
	CreatedAt              types.String `tfsdk:"created_at"`
}

var marketplaceAppItemObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":                       types.StringType,
		"name":                     types.StringType,
		"slug":                     types.StringType,
		"description":              types.StringType,
		"short_description":        types.StringType,
		"category":                 types.StringType,
		"version":                  types.StringType,
		"system_requirements":      marketplaceAppSystemRequirementsObjectType,
		"deployment_strategy":      types.StringType,
		"default_operating_system": types.StringType,
		"compatible_plans":         types.ListType{ElemType: types.StringType},
		"access_instructions":      types.StringType,
		"upstream_url":             types.StringType,
		"documentation_url":        types.StringType,
		"logo_url":                 types.StringType,
		"created_at":               types.StringType,
	},
}

// marketplaceAppItemValue maps a single SDK marketplace app into the list item
// model. It shares the system requirements mapper with the singular data source.
func marketplaceAppItemValue(ctx context.Context, app *components.MarketplaceAppData) (MarketplaceAppItemModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	item := MarketplaceAppItemModel{
		ID:                 types.StringPointerValue(app.ID),
		SystemRequirements: types.ObjectNull(marketplaceAppSystemRequirementsObjectType.AttrTypes),
		CompatiblePlans:    types.ListNull(types.StringType),
	}

	if app.Attributes != nil {
		attrs := app.Attributes

		item.Name = types.StringPointerValue(attrs.Name)
		item.Slug = types.StringPointerValue(attrs.Slug)
		item.Description = types.StringPointerValue(attrs.Description)
		item.ShortDescription = types.StringPointerValue(attrs.ShortDescription)
		item.Category = types.StringPointerValue(attrs.Category)
		item.Version = types.StringPointerValue(attrs.Version)
		if attrs.DeploymentStrategy != nil {
			item.DeploymentStrategy = types.StringValue(string(*attrs.DeploymentStrategy))
		} else {
			item.DeploymentStrategy = types.StringNull()
		}
		item.DefaultOperatingSystem = types.StringPointerValue(attrs.DefaultOperatingSystem)
		item.AccessInstructions = types.StringPointerValue(attrs.AccessInstructions)
		item.UpstreamURL = types.StringPointerValue(attrs.UpstreamURL)
		item.DocumentationURL = types.StringPointerValue(attrs.DocumentationURL)
		item.LogoURL = types.StringPointerValue(attrs.LogoURL)
		item.CreatedAt = types.StringPointerValue(attrs.CreatedAt)

		if attrs.CompatiblePlans != nil {
			plansList, d := types.ListValueFrom(ctx, types.StringType, attrs.CompatiblePlans)
			diags.Append(d...)
			item.CompatiblePlans = plansList
		}

		sysReq, d := marketplaceAppSystemRequirementsValue(ctx, attrs.SystemRequirements)
		diags.Append(d...)
		item.SystemRequirements = sysReq
	}

	return item, diags
}

func (d *MarketplaceAppsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_marketplace_apps"
}

func (d *MarketplaceAppsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *MarketplaceAppsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Marketplace apps data source - list all published marketplace apps, optionally filtered by category. Only published apps are visible, and the `marketplace_apps` feature must be enabled for the team.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Synthetic identifier for this query: the `category` filter, or `all` when unfiltered.",
				Computed:            true,
			},
			"category": schema.StringAttribute{
				MarkdownDescription: "Only return apps in this category (case-insensitive). Omit to return every published app.",
				Optional:            true,
			},
			"apps": schema.ListNestedAttribute{
				MarkdownDescription: "The published marketplace apps that match the filter.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "Marketplace app ID.",
							Computed:            true,
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Name of the marketplace app.",
							Computed:            true,
						},
						"slug": schema.StringAttribute{
							MarkdownDescription: "Slug of the marketplace app (e.g. \"wordpress\").",
							Computed:            true,
						},
						"description": schema.StringAttribute{
							MarkdownDescription: "Full description of the marketplace app.",
							Computed:            true,
						},
						"short_description": schema.StringAttribute{
							MarkdownDescription: "Short description of the marketplace app.",
							Computed:            true,
						},
						"category": schema.StringAttribute{
							MarkdownDescription: "Category the marketplace app belongs to.",
							Computed:            true,
						},
						"version": schema.StringAttribute{
							MarkdownDescription: "Version of the marketplace app.",
							Computed:            true,
						},
						"system_requirements": schema.SingleNestedAttribute{
							MarkdownDescription: "Minimum system requirements to run the marketplace app.",
							Computed:            true,
							Attributes: map[string]schema.Attribute{
								"vcpus": schema.Int64Attribute{
									MarkdownDescription: "Minimum number of vCPUs required.",
									Computed:            true,
								},
								"memory_in_gb": schema.Int64Attribute{
									MarkdownDescription: "Minimum memory required, in GB.",
									Computed:            true,
								},
								"storage_in_gb": schema.Int64Attribute{
									MarkdownDescription: "Minimum storage required, in GB.",
									Computed:            true,
								},
								"gpu": schema.BoolAttribute{
									MarkdownDescription: "Whether a GPU is required.",
									Computed:            true,
								},
							},
						},
						"deployment_strategy": schema.StringAttribute{
							MarkdownDescription: "How the app is delivered: cloud-init install on a stock OS image (`user_data`) or a pre-built disk image (`image`).",
							Computed:            true,
						},
						"default_operating_system": schema.StringAttribute{
							MarkdownDescription: "Default operating system used to deploy the app.",
							Computed:            true,
						},
						"compatible_plans": schema.ListAttribute{
							MarkdownDescription: "Server plan slugs compatible with this marketplace app.",
							ElementType:         types.StringType,
							Computed:            true,
						},
						"access_instructions": schema.StringAttribute{
							MarkdownDescription: "Instructions for accessing the app after deployment.",
							Computed:            true,
						},
						"upstream_url": schema.StringAttribute{
							MarkdownDescription: "URL of the upstream project.",
							Computed:            true,
						},
						"documentation_url": schema.StringAttribute{
							MarkdownDescription: "URL of the app's documentation.",
							Computed:            true,
						},
						"logo_url": schema.StringAttribute{
							MarkdownDescription: "URL of the app's logo image.",
							Computed:            true,
						},
						"created_at": schema.StringAttribute{
							MarkdownDescription: "Timestamp when the marketplace app was created.",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *MarketplaceAppsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data MarketplaceAppsDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	if data.Category.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unknown filter value",
			"'category' is unknown. Please provide a concrete value or omit it.",
		)
		return
	}

	res, err := d.client.MarketplaceApps.ListMarketplaceApps(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", err.Error())
		return
	}

	categoryFilter := strings.TrimSpace(data.Category.ValueString())
	hasFilter := !data.Category.IsNull() && categoryFilter != ""

	if hasFilter {
		data.ID = types.StringValue(categoryFilter)
	} else {
		data.ID = types.StringValue("all")
	}

	items := make([]MarketplaceAppItemModel, 0)
	if res != nil && res.MarketplaceApps != nil {
		for i := range res.MarketplaceApps.Data {
			app := res.MarketplaceApps.Data[i]

			if hasFilter {
				category := ""
				if app.Attributes != nil && app.Attributes.Category != nil {
					category = strings.TrimSpace(*app.Attributes.Category)
				}
				if !strings.EqualFold(category, categoryFilter) {
					continue
				}
			}

			item, diags := marketplaceAppItemValue(ctx, &app)
			resp.Diagnostics.Append(diags...)
			if resp.Diagnostics.HasError() {
				return
			}
			items = append(items, item)
		}
	}

	appsList, diags := types.ListValueFrom(ctx, marketplaceAppItemObjectType, items)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Apps = appsList

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
