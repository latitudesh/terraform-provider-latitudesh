package latitudesh

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"

	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ datasource.DataSource = &TrafficDataSource{}
var _ datasource.DataSourceWithConfigure = &TrafficDataSource{}

func NewTrafficDataSource() datasource.DataSource {
	return &TrafficDataSource{}
}

type TrafficDataSource struct {
	client         *latitudeshgosdk.Latitudesh
	defaultProject string
}

type TrafficDataSourceModel struct {
	// Filters
	Project types.String `tfsdk:"project"`
	Server  types.String `tfsdk:"server"`
	DateGte types.String `tfsdk:"date_gte"`
	DateLte types.String `tfsdk:"date_lte"`

	// Consumption (Traffic.Get)
	ID                              types.String  `tfsdk:"id"`
	Type                            types.String  `tfsdk:"type"`
	FromDate                        types.Int64   `tfsdk:"from_date"`
	ToDate                          types.Int64   `tfsdk:"to_date"`
	TotalInboundGb                  types.Int64   `tfsdk:"total_inbound_gb"`
	TotalOutboundGb                 types.Int64   `tfsdk:"total_outbound_gb"`
	TotalInbound95thPercentileMbps  types.Float64 `tfsdk:"total_inbound_95th_percentile_mbps"`
	TotalOutbound95thPercentileMbps types.Float64 `tfsdk:"total_outbound_95th_percentile_mbps"`
	Regions                         types.List    `tfsdk:"regions"`

	// Quota (Traffic.GetQuota). Kept under a "quota_" prefix so its own id/type
	// envelope fields do not collide with the consumption ones above.
	QuotaID         types.String `tfsdk:"quota_id"`
	QuotaType       types.String `tfsdk:"quota_type"`
	QuotaPerProject types.List   `tfsdk:"quota_per_project"`
}

type TrafficRegionDataModel struct {
	Date                 types.String  `tfsdk:"date"`
	InboundGb            types.Int64   `tfsdk:"inbound_gb"`
	OutboundGb           types.Int64   `tfsdk:"outbound_gb"`
	AvgOutboundSpeedMbps types.Float64 `tfsdk:"avg_outbound_speed_mbps"`
	AvgInboundSpeedMbps  types.Float64 `tfsdk:"avg_inbound_speed_mbps"`
	OutboundSpeedMbps    types.Float64 `tfsdk:"outbound_speed_mbps"`
	InboundSpeedMbps     types.Float64 `tfsdk:"inbound_speed_mbps"`
}

var trafficRegionDataObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"date":                    types.StringType,
		"inbound_gb":              types.Int64Type,
		"outbound_gb":             types.Int64Type,
		"avg_outbound_speed_mbps": types.Float64Type,
		"avg_inbound_speed_mbps":  types.Float64Type,
		"outbound_speed_mbps":     types.Float64Type,
		"inbound_speed_mbps":      types.Float64Type,
	},
}

type TrafficRegionModel struct {
	RegionSlug                      types.String  `tfsdk:"region_slug"`
	TotalInboundGb                  types.Int64   `tfsdk:"total_inbound_gb"`
	TotalOutboundGb                 types.Int64   `tfsdk:"total_outbound_gb"`
	TotalInbound95thPercentileMbps  types.Float64 `tfsdk:"total_inbound_95th_percentile_mbps"`
	TotalOutbound95thPercentileMbps types.Float64 `tfsdk:"total_outbound_95th_percentile_mbps"`
	Data                            types.List    `tfsdk:"data"`
}

var trafficRegionObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"region_slug":                         types.StringType,
		"total_inbound_gb":                    types.Int64Type,
		"total_outbound_gb":                   types.Int64Type,
		"total_inbound_95th_percentile_mbps":  types.Float64Type,
		"total_outbound_95th_percentile_mbps": types.Float64Type,
		"data":                                types.ListType{ElemType: trafficRegionDataObjectType},
	},
}

// QuotaAmountModel backs both quota_in_tb and quota_in_mbps: the two share an
// identical Granted/Additional/Total shape in the SDK (components.QuotaInTb,
// components.QuotaInMbps).
type QuotaAmountModel struct {
	Granted    types.Int64 `tfsdk:"granted"`
	Additional types.Int64 `tfsdk:"additional"`
	Total      types.Int64 `tfsdk:"total"`
}

var quotaAmountObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"granted":    types.Int64Type,
		"additional": types.Int64Type,
		"total":      types.Int64Type,
	},
}

type QuotaPerRegionModel struct {
	RegionID    types.String `tfsdk:"region_id"`
	RegionSlug  types.String `tfsdk:"region_slug"`
	Price       types.Int64  `tfsdk:"price"`
	QuotaInTb   types.Object `tfsdk:"quota_in_tb"`
	QuotaInMbps types.Object `tfsdk:"quota_in_mbps"`
}

var quotaPerRegionObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"region_id":     types.StringType,
		"region_slug":   types.StringType,
		"price":         types.Int64Type,
		"quota_in_tb":   quotaAmountObjectType,
		"quota_in_mbps": quotaAmountObjectType,
	},
}

type QuotaPerProjectModel struct {
	ProjectID      types.String `tfsdk:"project_id"`
	ProjectSlug    types.String `tfsdk:"project_slug"`
	Price          types.Int64  `tfsdk:"price"`
	BillingMethod  types.String `tfsdk:"billing_method"`
	QuotaPerRegion types.List   `tfsdk:"quota_per_region"`
}

var quotaPerProjectObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"project_id":       types.StringType,
		"project_slug":     types.StringType,
		"price":            types.Int64Type,
		"billing_method":   types.StringType,
		"quota_per_region": types.ListType{ElemType: quotaPerRegionObjectType},
	},
}

func emptyTrafficRegions() types.List {
	list, _ := types.ListValue(trafficRegionObjectType, []attr.Value{})
	return list
}

func emptyQuotaPerProject() types.List {
	list, _ := types.ListValue(quotaPerProjectObjectType, []attr.Value{})
	return list
}

func trafficRegionDataSortKey(d components.TrafficDataData) string {
	return derefString(d.Date)
}

func trafficRegionDataValue(ctx context.Context, items []components.TrafficDataData) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics

	if len(items) == 0 {
		return types.ListValue(trafficRegionDataObjectType, []attr.Value{})
	}

	sorted := make([]components.TrafficDataData, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool {
		return trafficRegionDataSortKey(sorted[i]) < trafficRegionDataSortKey(sorted[j])
	})

	models := make([]TrafficRegionDataModel, 0, len(sorted))
	for _, d := range sorted {
		models = append(models, TrafficRegionDataModel{
			Date:                 optionalString(d.Date),
			InboundGb:            types.Int64PointerValue(d.InboundGb),
			OutboundGb:           types.Int64PointerValue(d.OutboundGb),
			AvgOutboundSpeedMbps: types.Float64PointerValue(d.AvgOutboundSpeedMbps),
			AvgInboundSpeedMbps:  types.Float64PointerValue(d.AvgInboundSpeedMbps),
			OutboundSpeedMbps:    types.Float64PointerValue(d.OutboundSpeedMbps),
			InboundSpeedMbps:     types.Float64PointerValue(d.InboundSpeedMbps),
		})
	}
	list, d := types.ListValueFrom(ctx, trafficRegionDataObjectType, models)
	diags.Append(d...)
	return list, diags
}

func trafficRegionSortKey(r components.TrafficRegions) string {
	return derefString(r.RegionSlug)
}

func trafficRegionsValue(ctx context.Context, regions []components.TrafficRegions) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics

	if len(regions) == 0 {
		return emptyTrafficRegions(), diags
	}

	sorted := make([]components.TrafficRegions, len(regions))
	copy(sorted, regions)
	sort.SliceStable(sorted, func(i, j int) bool {
		return trafficRegionSortKey(sorted[i]) < trafficRegionSortKey(sorted[j])
	})

	models := make([]TrafficRegionModel, 0, len(sorted))
	for _, r := range sorted {
		data, d := trafficRegionDataValue(ctx, r.Data)
		diags.Append(d...)

		models = append(models, TrafficRegionModel{
			RegionSlug:                      optionalString(r.RegionSlug),
			TotalInboundGb:                  types.Int64PointerValue(r.TotalInboundGb),
			TotalOutboundGb:                 types.Int64PointerValue(r.TotalOutboundGb),
			TotalInbound95thPercentileMbps:  types.Float64PointerValue(r.TotalInbound95thPercentileMbps),
			TotalOutbound95thPercentileMbps: types.Float64PointerValue(r.TotalOutbound95thPercentileMbps),
			Data:                            data,
		})
	}
	list, d := types.ListValueFrom(ctx, trafficRegionObjectType, models)
	diags.Append(d...)
	return list, diags
}

// quotaInTbValue and quotaInMbpsValue map the two feature-flag-dependent
// quota shapes independently: the API populates exactly one of quota_in_tb
// or quota_in_mbps per region depending on the project's billing method, and
// the other stays null rather than a stale/zero value.
func quotaInTbValue(ctx context.Context, q *components.QuotaInTb) (types.Object, diag.Diagnostics) {
	if q == nil {
		return types.ObjectNull(quotaAmountObjectType.AttrTypes), nil
	}
	model := QuotaAmountModel{
		Granted:    types.Int64PointerValue(q.Granted),
		Additional: types.Int64PointerValue(q.Additional),
		Total:      types.Int64PointerValue(q.Total),
	}
	return types.ObjectValueFrom(ctx, quotaAmountObjectType.AttrTypes, model)
}

func quotaInMbpsValue(ctx context.Context, q *components.QuotaInMbps) (types.Object, diag.Diagnostics) {
	if q == nil {
		return types.ObjectNull(quotaAmountObjectType.AttrTypes), nil
	}
	model := QuotaAmountModel{
		Granted:    types.Int64PointerValue(q.Granted),
		Additional: types.Int64PointerValue(q.Additional),
		Total:      types.Int64PointerValue(q.Total),
	}
	return types.ObjectValueFrom(ctx, quotaAmountObjectType.AttrTypes, model)
}

func quotaPerRegionSortKey(r components.QuotaPerRegion) string {
	return derefString(r.RegionID) + "\x00" + derefString(r.RegionSlug)
}

func quotaPerRegionValue(ctx context.Context, items []components.QuotaPerRegion) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics

	if len(items) == 0 {
		return types.ListValue(quotaPerRegionObjectType, []attr.Value{})
	}

	sorted := make([]components.QuotaPerRegion, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool {
		return quotaPerRegionSortKey(sorted[i]) < quotaPerRegionSortKey(sorted[j])
	})

	models := make([]QuotaPerRegionModel, 0, len(sorted))
	for _, r := range sorted {
		tb, d := quotaInTbValue(ctx, r.QuotaInTb)
		diags.Append(d...)
		mbps, d := quotaInMbpsValue(ctx, r.QuotaInMbps)
		diags.Append(d...)

		models = append(models, QuotaPerRegionModel{
			RegionID:    optionalString(r.RegionID),
			RegionSlug:  optionalString(r.RegionSlug),
			Price:       types.Int64PointerValue(r.Price),
			QuotaInTb:   tb,
			QuotaInMbps: mbps,
		})
	}
	list, d := types.ListValueFrom(ctx, quotaPerRegionObjectType, models)
	diags.Append(d...)
	return list, diags
}

func quotaPerProjectSortKey(p components.QuotaPerProject) string {
	return derefString(p.ProjectID) + "\x00" + derefString(p.ProjectSlug)
}

func quotaPerProjectValue(ctx context.Context, items []components.QuotaPerProject) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics

	if len(items) == 0 {
		return emptyQuotaPerProject(), diags
	}

	sorted := make([]components.QuotaPerProject, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool {
		return quotaPerProjectSortKey(sorted[i]) < quotaPerProjectSortKey(sorted[j])
	})

	models := make([]QuotaPerProjectModel, 0, len(sorted))
	for _, p := range sorted {
		regions, d := quotaPerRegionValue(ctx, p.QuotaPerRegion)
		diags.Append(d...)

		models = append(models, QuotaPerProjectModel{
			ProjectID:      optionalString(p.ProjectID),
			ProjectSlug:    optionalString(p.ProjectSlug),
			Price:          types.Int64PointerValue(p.Price),
			BillingMethod:  optionalString(p.BillingMethod),
			QuotaPerRegion: regions,
		})
	}
	list, d := types.ListValueFrom(ctx, quotaPerProjectObjectType, models)
	diags.Append(d...)
	return list, diags
}

func (d *TrafficDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_traffic"
}

func (d *TrafficDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
	d.defaultProject = deps.DefaultProject
}

func (d *TrafficDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	quotaAmountAttrs := map[string]schema.Attribute{
		"granted": schema.Int64Attribute{
			MarkdownDescription: "Quota granted by the plan.",
			Computed:            true,
		},
		"additional": schema.Int64Attribute{
			MarkdownDescription: "Additional quota purchased on top of the granted amount.",
			Computed:            true,
		},
		"total": schema.Int64Attribute{
			MarkdownDescription: "Total quota available (granted + additional).",
			Computed:            true,
		},
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Traffic data source - read-only traffic consumption and quota reporting.",
		Attributes: map[string]schema.Attribute{
			"project": schema.StringAttribute{
				MarkdownDescription: "Project (ID or slug) to restrict traffic and quota to. Falls back to the provider-level `project` when unset; if neither is set, the report covers every project the token can access.",
				Optional:            true,
			},
			"server": schema.StringAttribute{
				MarkdownDescription: "Server ID to restrict traffic consumption to. Not applied to the quota lookup.",
				Optional:            true,
			},
			"date_gte": schema.StringAttribute{
				MarkdownDescription: "Start of the consumption window, in ISO8601 format (e.g. `2024-04-01T00:00:00Z`).",
				Required:            true,
			},
			"date_lte": schema.StringAttribute{
				MarkdownDescription: "End of the consumption window, in ISO8601 format (e.g. `2024-04-30T23:59:59Z`). The window between `date_gte` and `date_lte` must not exceed 366 days.",
				Required:            true,
			},

			"id": schema.StringAttribute{
				MarkdownDescription: "Traffic consumption report identifier.",
				Computed:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Resource type, as returned by the API.",
				Computed:            true,
			},
			"from_date": schema.Int64Attribute{
				MarkdownDescription: "Start of the reported window, as a unix timestamp.",
				Computed:            true,
			},
			"to_date": schema.Int64Attribute{
				MarkdownDescription: "End of the reported window, as a unix timestamp.",
				Computed:            true,
			},
			"total_inbound_gb": schema.Int64Attribute{
				MarkdownDescription: "Total inbound traffic across all regions, in GB.",
				Computed:            true,
			},
			"total_outbound_gb": schema.Int64Attribute{
				MarkdownDescription: "Total outbound traffic across all regions, in GB.",
				Computed:            true,
			},
			"total_inbound_95th_percentile_mbps": schema.Float64Attribute{
				MarkdownDescription: "The 95th percentile of inbound bandwidth across all regions. This is a global percentile, not a sum of regional percentiles. Value in Mbps.",
				Computed:            true,
			},
			"total_outbound_95th_percentile_mbps": schema.Float64Attribute{
				MarkdownDescription: "The 95th percentile of outbound bandwidth across all regions. This is a global percentile, not a sum of regional percentiles. Value in Mbps.",
				Computed:            true,
			},
			"regions": schema.ListNestedAttribute{
				MarkdownDescription: "Per-region traffic consumption breakdown.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"region_slug": schema.StringAttribute{
							MarkdownDescription: "Region slug.",
							Computed:            true,
						},
						"total_inbound_gb": schema.Int64Attribute{
							MarkdownDescription: "Total inbound traffic for this region, in GB.",
							Computed:            true,
						},
						"total_outbound_gb": schema.Int64Attribute{
							MarkdownDescription: "Total outbound traffic for this region, in GB.",
							Computed:            true,
						},
						"total_inbound_95th_percentile_mbps": schema.Float64Attribute{
							MarkdownDescription: "The 95th percentile of inbound bandwidth for this region. Value in Mbps.",
							Computed:            true,
						},
						"total_outbound_95th_percentile_mbps": schema.Float64Attribute{
							MarkdownDescription: "The 95th percentile of outbound bandwidth for this region. Value in Mbps.",
							Computed:            true,
						},
						"data": schema.ListNestedAttribute{
							MarkdownDescription: "Daily traffic data points for this region.",
							Computed:            true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"date": schema.StringAttribute{
										MarkdownDescription: "The datetime of the day.",
										Computed:            true,
									},
									"inbound_gb": schema.Int64Attribute{
										MarkdownDescription: "Inbound traffic for the day, in GB.",
										Computed:            true,
									},
									"outbound_gb": schema.Int64Attribute{
										MarkdownDescription: "Outbound traffic for the day, in GB.",
										Computed:            true,
									},
									"avg_outbound_speed_mbps": schema.Float64Attribute{
										MarkdownDescription: "Average outbound speed for the day, in Mbps.",
										Computed:            true,
									},
									"avg_inbound_speed_mbps": schema.Float64Attribute{
										MarkdownDescription: "Average inbound speed for the day, in Mbps.",
										Computed:            true,
									},
									"outbound_speed_mbps": schema.Float64Attribute{
										MarkdownDescription: "Outbound speed for the day, in Mbps.",
										Computed:            true,
									},
									"inbound_speed_mbps": schema.Float64Attribute{
										MarkdownDescription: "Inbound speed for the day, in Mbps.",
										Computed:            true,
									},
								},
							},
						},
					},
				},
			},

			"quota_id": schema.StringAttribute{
				MarkdownDescription: "Traffic quota report identifier.",
				Computed:            true,
			},
			"quota_type": schema.StringAttribute{
				MarkdownDescription: "Quota resource type, as returned by the API.",
				Computed:            true,
			},
			"quota_per_project": schema.ListNestedAttribute{
				MarkdownDescription: "Per-project traffic quota breakdown.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"project_id": schema.StringAttribute{
							MarkdownDescription: "Project ID.",
							Computed:            true,
						},
						"project_slug": schema.StringAttribute{
							MarkdownDescription: "Project slug.",
							Computed:            true,
						},
						"price": schema.Int64Attribute{
							MarkdownDescription: "Price associated with this project's quota.",
							Computed:            true,
						},
						"billing_method": schema.StringAttribute{
							MarkdownDescription: "Billing method applied to this project's traffic.",
							Computed:            true,
						},
						"quota_per_region": schema.ListNestedAttribute{
							MarkdownDescription: "Per-region quota breakdown for this project.",
							Computed:            true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"region_id": schema.StringAttribute{
										MarkdownDescription: "Region ID.",
										Computed:            true,
									},
									"region_slug": schema.StringAttribute{
										MarkdownDescription: "Region slug.",
										Computed:            true,
									},
									"price": schema.Int64Attribute{
										MarkdownDescription: "Price associated with this region's quota.",
										Computed:            true,
									},
									"quota_in_tb": schema.SingleNestedAttribute{
										MarkdownDescription: "Quota expressed in TB of volume. Populated only for projects on volume-based billing; null when the project's feature flag selects `quota_in_mbps` instead.",
										Computed:            true,
										Attributes:          quotaAmountAttrs,
									},
									"quota_in_mbps": schema.SingleNestedAttribute{
										MarkdownDescription: "Quota expressed in Mbps of bandwidth. Populated only for projects on bandwidth-based billing; null when the project's feature flag selects `quota_in_tb` instead.",
										Computed:            true,
										Attributes:          quotaAmountAttrs,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (d *TrafficDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data TrafficDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	project := data.Project.ValueString()
	if project == "" {
		project = d.defaultProject
	}
	var filterProject *string
	if project != "" {
		filterProject = &project
		data.Project = types.StringValue(project)
	}

	var filterServer *string
	if !data.Server.IsNull() {
		v := data.Server.ValueString()
		filterServer = &v
	}

	// Defaults for the nested attributes, kept known (never null) so the
	// generated state is always well-formed even if a response omits them.
	data.Regions = emptyTrafficRegions()
	data.QuotaPerProject = emptyQuotaPerProject()

	consumption, err := d.client.Traffic.Get(ctx, data.DateGte.ValueString(), data.DateLte.ValueString(), filterServer, filterProject)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read traffic consumption, got error: %s", err.Error()))
		return
	}
	if consumption.Traffic == nil || consumption.Traffic.Data == nil {
		resp.Diagnostics.AddError("API Error", "Traffic consumption response did not contain any data.")
		return
	}

	trafficData := consumption.Traffic.Data
	if trafficData.ID != nil {
		data.ID = types.StringValue(*trafficData.ID)
	}
	if trafficData.Type != nil {
		data.Type = types.StringValue(string(*trafficData.Type))
	}
	if trafficData.Attributes != nil {
		attrs := trafficData.Attributes

		data.FromDate = types.Int64PointerValue(attrs.FromDate)
		data.ToDate = types.Int64PointerValue(attrs.ToDate)
		data.TotalInboundGb = types.Int64PointerValue(attrs.TotalInboundGb)
		data.TotalOutboundGb = types.Int64PointerValue(attrs.TotalOutboundGb)
		data.TotalInbound95thPercentileMbps = types.Float64PointerValue(attrs.TotalInbound95thPercentileMbps)
		data.TotalOutbound95thPercentileMbps = types.Float64PointerValue(attrs.TotalOutbound95thPercentileMbps)

		regions, diags := trafficRegionsValue(ctx, attrs.Regions)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		data.Regions = regions
	}

	quota, err := d.client.Traffic.GetQuota(ctx, filterProject)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read traffic quota, got error: %s", err.Error()))
		return
	}
	if quota.TrafficQuota != nil && quota.TrafficQuota.Data != nil {
		quotaData := quota.TrafficQuota.Data
		if quotaData.ID != nil {
			data.QuotaID = types.StringValue(*quotaData.ID)
		}
		if quotaData.Type != nil {
			data.QuotaType = types.StringValue(string(*quotaData.Type))
		}
		if quotaData.Attributes != nil {
			perProject, diags := quotaPerProjectValue(ctx, quotaData.Attributes.QuotaPerProject)
			resp.Diagnostics.Append(diags...)
			if resp.Diagnostics.HasError() {
				return
			}
			data.QuotaPerProject = perProject
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
