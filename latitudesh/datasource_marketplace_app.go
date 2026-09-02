package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"

	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var (
	_ datasource.DataSource              = &MarketplaceAppDataSource{}
	_ datasource.DataSourceWithConfigure = &MarketplaceAppDataSource{}
)

func NewMarketplaceAppDataSource() datasource.DataSource {
	return &MarketplaceAppDataSource{}
}

type MarketplaceAppDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type MarketplaceAppDataSourceModel struct {
	// Selectors (exactly one)
	ID   types.String `tfsdk:"id"`
	Slug types.String `tfsdk:"slug"`
	Name types.String `tfsdk:"name"`

	// Attributes. Presentation-only catalog metadata (descriptions, marketing
	// URLs, logo, created_at, access instructions) is deliberately not exposed:
	// this data source serves provisioning, not catalog browsing.
	Category               types.String `tfsdk:"category"`
	Version                types.String `tfsdk:"version"`
	SystemRequirements     types.Object `tfsdk:"system_requirements"`
	DeploymentStrategy     types.String `tfsdk:"deployment_strategy"`
	DefaultOperatingSystem types.String `tfsdk:"default_operating_system"`
	CompatiblePlans        types.List   `tfsdk:"compatible_plans"`
}

// MarketplaceAppSystemRequirementsModel is the minimum hardware needed to run
// a marketplace app, as reported by the API.
type MarketplaceAppSystemRequirementsModel struct {
	Vcpus       types.Int64 `tfsdk:"vcpus"`
	MemoryInGb  types.Int64 `tfsdk:"memory_in_gb"`
	StorageInGb types.Int64 `tfsdk:"storage_in_gb"`
	Gpu         types.Bool  `tfsdk:"gpu"`
}

var marketplaceAppSystemRequirementsObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"vcpus":         types.Int64Type,
		"memory_in_gb":  types.Int64Type,
		"storage_in_gb": types.Int64Type,
		"gpu":           types.BoolType,
	},
}

func marketplaceAppSystemRequirementsValue(ctx context.Context, sr *components.SystemRequirements) (types.Object, diag.Diagnostics) {
	if sr == nil {
		return types.ObjectNull(marketplaceAppSystemRequirementsObjectType.AttrTypes), nil
	}
	model := MarketplaceAppSystemRequirementsModel{
		Vcpus:       types.Int64PointerValue(sr.Vcpus),
		MemoryInGb:  types.Int64PointerValue(sr.MemoryInGb),
		StorageInGb: types.Int64PointerValue(sr.StorageInGb),
		Gpu:         types.BoolPointerValue(sr.Gpu),
	}
	return types.ObjectValueFrom(ctx, marketplaceAppSystemRequirementsObjectType.AttrTypes, model)
}

func (d *MarketplaceAppDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_marketplace_app"
}

func (d *MarketplaceAppDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *MarketplaceAppDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Marketplace app data source - lookup a marketplace app by id, slug, or name. Only published apps are visible, and the `marketplace_apps` feature must be enabled for the team.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Marketplace app ID to look up.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("id"),
						path.MatchRoot("slug"),
						path.MatchRoot("name"),
					),
				},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "Marketplace app slug to look up (e.g. \"wordpress\").",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("id"),
						path.MatchRoot("slug"),
						path.MatchRoot("name"),
					),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Marketplace app name to look up.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("id"),
						path.MatchRoot("slug"),
						path.MatchRoot("name"),
					),
				},
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
		},
	}
}

func (d *MarketplaceAppDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data MarketplaceAppDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	// Avoid unknown selectors (e.g., from unresolved variables)
	if data.ID.IsUnknown() || data.Slug.IsUnknown() || data.Name.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unknown selector value",
			"One of 'id', 'slug', or 'name' is unknown. Please provide a concrete value.",
		)
		return
	}

	var app *components.MarketplaceAppData
	var err error

	switch {
	case !data.ID.IsNull():
		app, err = d.getByIDOrSlug(ctx, data.ID.ValueString())
	case !data.Slug.IsNull():
		app, err = d.getByIDOrSlug(ctx, data.Slug.ValueString())
	case !data.Name.IsNull():
		app, err = d.findByName(ctx, data.Name.ValueString())
	default:
		resp.Diagnostics.AddError(
			"Missing selector",
			"Exactly one of 'id', 'slug', or 'name' must be provided.",
		)
		return
	}

	if err != nil {
		resp.Diagnostics.AddError("Client Error", err.Error())
		return
	}
	if app == nil {
		resp.Diagnostics.AddError("Marketplace App Not Found", "No marketplace app matches the given selector.")
		return
	}

	if app.ID != nil {
		data.ID = types.StringValue(*app.ID)
	}

	data.SystemRequirements = types.ObjectNull(marketplaceAppSystemRequirementsObjectType.AttrTypes)
	data.CompatiblePlans = types.ListNull(types.StringType)

	if app.Attributes != nil {
		attrs := app.Attributes

		if attrs.Name != nil {
			data.Name = types.StringValue(*attrs.Name)
		}
		if attrs.Slug != nil {
			data.Slug = types.StringValue(*attrs.Slug)
		}
		data.Category = types.StringPointerValue(attrs.Category)
		data.Version = types.StringPointerValue(attrs.Version)
		if attrs.DeploymentStrategy != nil {
			data.DeploymentStrategy = types.StringValue(string(*attrs.DeploymentStrategy))
		} else {
			data.DeploymentStrategy = types.StringNull()
		}
		data.DefaultOperatingSystem = types.StringPointerValue(attrs.DefaultOperatingSystem)

		if attrs.CompatiblePlans != nil {
			plansList, diags := types.ListValueFrom(ctx, types.StringType, attrs.CompatiblePlans)
			resp.Diagnostics.Append(diags...)
			if resp.Diagnostics.HasError() {
				return
			}
			data.CompatiblePlans = plansList
		}

		sysReq, diags := marketplaceAppSystemRequirementsValue(ctx, attrs.SystemRequirements)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		data.SystemRequirements = sysReq
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// marketplaceAppNotFound reports whether err is a 404 from the marketplace app
// endpoint. GetMarketplaceApp declares a typed 404 response, so the SDK returns
// a *components.ErrorObject (JSON:API errors, status "404") rather than the
// generic *components.APIError other endpoints yield — both must be recognized.
func marketplaceAppNotFound(err error) bool {
	var apiErr *components.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}

	var errObj *components.ErrorObject
	if errors.As(err, &errObj) {
		for _, e := range errObj.Errors {
			if e.Status != nil && *e.Status == "404" {
				return true
			}
		}
	}

	return false
}

func (d *MarketplaceAppDataSource) getByIDOrSlug(ctx context.Context, idOrSlug string) (*components.MarketplaceAppData, error) {
	res, err := d.client.MarketplaceApps.GetMarketplaceApp(ctx, idOrSlug)
	if err != nil {
		if marketplaceAppNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("unable to retrieve marketplace app %q: %w", idOrSlug, err)
	}
	if res.MarketplaceApp == nil || res.MarketplaceApp.Data == nil {
		return nil, nil
	}
	return res.MarketplaceApp.Data, nil
}

// findByName lists all marketplace apps and filters in memory by name, since
// ListMarketplaceApps takes no filter parameters.
func (d *MarketplaceAppDataSource) findByName(ctx context.Context, name string) (*components.MarketplaceAppData, error) {
	res, err := d.client.MarketplaceApps.ListMarketplaceApps(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to list marketplace apps: %w", err)
	}
	if res == nil || res.MarketplaceApps == nil || res.MarketplaceApps.Data == nil {
		return nil, nil
	}

	nameQ := strings.TrimSpace(name)
	for i := range res.MarketplaceApps.Data {
		app := res.MarketplaceApps.Data[i]
		if app.Attributes == nil || app.Attributes.Name == nil {
			continue
		}
		if strings.TrimSpace(*app.Attributes.Name) == nameQ {
			return &app, nil
		}
	}

	return nil, nil
}
