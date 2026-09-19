package latitudesh

import (
	"context"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"

	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var (
	_ datasource.DataSource              = &LksPlansDataSource{}
	_ datasource.DataSourceWithConfigure = &LksPlansDataSource{}
)

func NewLksPlansDataSource() datasource.DataSource {
	return &LksPlansDataSource{}
}

type LksPlansDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type LksPlansDataSourceModel struct {
	// Synthetic identifier: the site filter, or "all" when unfiltered.
	ID types.String `tfsdk:"id"`

	Site  types.String `tfsdk:"site"`
	Stock types.Map    `tfsdk:"stock"`
	Plans types.List   `tfsdk:"plans"`
}

type LksPlanItemModel struct {
	ID           types.String `tfsdk:"id"`
	Slug         types.String `tfsdk:"slug"`
	Name         types.String `tfsdk:"name"`
	InStockCount types.Map    `tfsdk:"in_stock_count"`
	Specs        types.Object `tfsdk:"specs"`
	Regions      types.List   `tfsdk:"regions"`
}

type LksPlanCPUModel struct {
	Type  types.String  `tfsdk:"type"`
	Clock types.Float64 `tfsdk:"clock"`
	Cores types.Int64   `tfsdk:"cores"`
	Count types.Int64   `tfsdk:"count"`
}

type LksPlanNicModel struct {
	Count types.Int64  `tfsdk:"count"`
	Type  types.String `tfsdk:"type"`
}

type LksPlanGPUModel struct {
	Count        types.Int64  `tfsdk:"count"`
	Type         types.String `tfsdk:"type"`
	VramPerGPU   types.Int64  `tfsdk:"vram_per_gpu"`
	Interconnect types.String `tfsdk:"interconnect"`
}

type LksPlanSpecsModel struct {
	CPU         types.Object `tfsdk:"cpu"`
	MemoryTotal types.String `tfsdk:"memory_total"`
	Drives      types.List   `tfsdk:"drives"`
	Nics        types.List   `tfsdk:"nics"`
	GPU         types.Object `tfsdk:"gpu"`
}

type LksPlanRegionModel struct {
	Name           types.String `tfsdk:"name"`
	StockLevel     types.String `tfsdk:"stock_level"`
	AvailableSites types.List   `tfsdk:"available_sites"`
	InStockSites   types.List   `tfsdk:"in_stock_sites"`
	InStockCount   types.Map    `tfsdk:"in_stock_count"`
}

var (
	lksPlanCPUObjectType = types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"type":  types.StringType,
			"clock": types.Float64Type,
			"cores": types.Int64Type,
			"count": types.Int64Type,
		},
	}

	lksPlanNicObjectType = types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"count": types.Int64Type,
			"type":  types.StringType,
		},
	}

	lksPlanGPUObjectType = types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"count":        types.Int64Type,
			"type":         types.StringType,
			"vram_per_gpu": types.Int64Type,
			"interconnect": types.StringType,
		},
	}

	lksPlanSpecsObjectType = types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"cpu":          lksPlanCPUObjectType,
			"memory_total": types.StringType,
			"drives":       types.ListType{ElemType: planDriveObjectType},
			"nics":         types.ListType{ElemType: lksPlanNicObjectType},
			"gpu":          lksPlanGPUObjectType,
		},
	}

	lksPlanRegionObjectType = types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"name":            types.StringType,
			"stock_level":     types.StringType,
			"available_sites": types.ListType{ElemType: types.StringType},
			"in_stock_sites":  types.ListType{ElemType: types.StringType},
			"in_stock_count":  types.MapType{ElemType: types.Int64Type},
		},
	}

	lksPlanItemObjectType = types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"id":             types.StringType,
			"slug":           types.StringType,
			"name":           types.StringType,
			"in_stock_count": types.MapType{ElemType: types.Int64Type},
			"specs":          lksPlanSpecsObjectType,
			"regions":        types.ListType{ElemType: lksPlanRegionObjectType},
		},
	}
)

// lksPlanMemoryTotal renders specs.memory.total. The SDK models it as an
// int-or-string union because the API is not consistent about it, so this
// always answers a string — the same call datasource_plan.go already makes for
// specs.drives.size, which is free-form text for the same reason.
func lksPlanMemoryTotal(total *components.LksPlansTotal) types.String {
	switch {
	case total == nil:
		return types.StringNull()
	case total.Integer != nil:
		return types.StringValue(strconv.FormatInt(*total.Integer, 10))
	case total.Str != nil:
		return types.StringValue(*total.Str)
	default:
		return types.StringNull()
	}
}

func lksPlanSpecsValue(ctx context.Context, specs *components.LksPlansSpecs) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics

	if specs == nil {
		return types.ObjectNull(lksPlanSpecsObjectType.AttrTypes), diags
	}

	model := LksPlanSpecsModel{
		CPU:         types.ObjectNull(lksPlanCPUObjectType.AttrTypes),
		MemoryTotal: lksPlanMemoryTotal(specs.Memory.GetTotal()),
		GPU:         types.ObjectNull(lksPlanGPUObjectType.AttrTypes),
	}

	if cpu := specs.CPU; cpu != nil {
		obj, d := types.ObjectValueFrom(ctx, lksPlanCPUObjectType.AttrTypes, LksPlanCPUModel{
			Type:  types.StringPointerValue(cpu.Type),
			Clock: types.Float64PointerValue(cpu.Clock),
			Cores: types.Int64PointerValue(cpu.Cores),
			Count: types.Int64PointerValue(cpu.Count),
		})
		diags.Append(d...)
		model.CPU = obj
	}

	// specs.drives reuses datasource_plan.go's shape so the two plan lookups
	// describe a disk group the same way, but the SDK models the LKS side with
	// its own type, so the values are converted here rather than shared.
	drives := make([]PlanDriveModel, 0, len(specs.Drives))
	for _, d := range specs.Drives {
		drives = append(drives, PlanDriveModel{
			Count: types.Int64PointerValue(d.Count),
			Size:  types.StringPointerValue(d.Size),
			Type:  types.StringPointerValue(d.Type),
		})
	}
	drivesList, d := types.ListValueFrom(ctx, planDriveObjectType, drives)
	diags.Append(d...)
	model.Drives = drivesList

	nics := make([]LksPlanNicModel, 0, len(specs.Nics))
	for _, n := range specs.Nics {
		nics = append(nics, LksPlanNicModel{
			Count: types.Int64PointerValue(n.Count),
			Type:  types.StringPointerValue(n.Type),
		})
	}
	nicsList, d := types.ListValueFrom(ctx, lksPlanNicObjectType, nics)
	diags.Append(d...)
	model.Nics = nicsList

	// A non-GPU plan comes back as an empty object rather than null, exactly as
	// datasource_plan.go's planHasGPU documents for the server plans endpoint.
	if gpu := specs.Gpu; gpu != nil && (gpu.Type != nil || gpu.Count != nil) {
		obj, d := types.ObjectValueFrom(ctx, lksPlanGPUObjectType.AttrTypes, LksPlanGPUModel{
			Count:        types.Int64PointerValue(gpu.Count),
			Type:         types.StringPointerValue(gpu.Type),
			VramPerGPU:   types.Int64PointerValue(gpu.VramPerGpu),
			Interconnect: types.StringPointerValue(gpu.Interconnect),
		})
		diags.Append(d...)
		model.GPU = obj
	}

	obj, d := types.ObjectValueFrom(ctx, lksPlanSpecsObjectType.AttrTypes, model)
	diags.Append(d...)
	return obj, diags
}

func lksPlanRegionsValue(ctx context.Context, regions []components.LksPlansRegions) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics

	models := make([]LksPlanRegionModel, 0, len(regions))
	for i := range regions {
		r := regions[i]

		model := LksPlanRegionModel{
			Name:           types.StringPointerValue(r.Name),
			StockLevel:     types.StringNull(),
			AvailableSites: types.ListNull(types.StringType),
			InStockSites:   types.ListNull(types.StringType),
			InStockCount:   types.MapNull(types.Int64Type),
		}

		if r.StockLevel != nil {
			model.StockLevel = types.StringValue(string(*r.StockLevel))
		}

		if loc := r.Locations; loc != nil {
			available, d := types.ListValueFrom(ctx, types.StringType, sliceOrEmpty(loc.Available))
			diags.Append(d...)
			model.AvailableSites = available

			inStock, d := types.ListValueFrom(ctx, types.StringType, sliceOrEmpty(loc.InStock))
			diags.Append(d...)
			model.InStockSites = inStock

			counts, d := types.MapValueFrom(ctx, types.Int64Type, mapOrEmpty(loc.InStockCount))
			diags.Append(d...)
			model.InStockCount = counts
		}

		models = append(models, model)
	}

	list, d := types.ListValueFrom(ctx, lksPlanRegionObjectType, models)
	diags.Append(d...)
	return list, diags
}

// sliceOrEmpty and mapOrEmpty keep a missing collection as an empty known value
// rather than null, so a `for` expression over it never has to nil-check.
func sliceOrEmpty(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func mapOrEmpty(in map[string]int64) map[string]int64 {
	if in == nil {
		return map[string]int64{}
	}
	return in
}

func lksPlanItemValue(ctx context.Context, p *components.LksPlansData) (LksPlanItemModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	item := LksPlanItemModel{
		ID:           types.StringPointerValue(p.ID),
		Slug:         types.StringNull(),
		Name:         types.StringNull(),
		InStockCount: types.MapNull(types.Int64Type),
		Specs:        types.ObjectNull(lksPlanSpecsObjectType.AttrTypes),
		Regions:      types.ListNull(lksPlanRegionObjectType),
	}

	if p.Attributes == nil {
		return item, diags
	}

	item.Slug = types.StringPointerValue(p.Attributes.Slug)
	item.Name = types.StringPointerValue(p.Attributes.Name)

	specs, d := lksPlanSpecsValue(ctx, p.Attributes.Specs)
	diags.Append(d...)
	item.Specs = specs

	regions, d := lksPlanRegionsValue(ctx, p.Attributes.Regions)
	diags.Append(d...)
	item.Regions = regions

	stock, d := lksPlanStockBySite(ctx, p.Attributes.Regions)
	diags.Append(d...)
	item.InStockCount = stock

	return item, diags
}

// lksPlanStockBySite flattens regions[].locations.in_stock_count into one map
// keyed by site slug. A site belongs to exactly one region, so merging loses
// nothing — and it turns "how many nodes of this plan can I get at this site"
// from a nested double loop into a lookup, which is the question every caller
// actually has.
func lksPlanStockBySite(ctx context.Context, regions []components.LksPlansRegions) (types.Map, diag.Diagnostics) {
	merged := map[string]int64{}
	for i := range regions {
		if loc := regions[i].Locations; loc != nil {
			for site, count := range loc.InStockCount {
				merged[site] += count
			}
		}
	}
	return types.MapValueFrom(ctx, types.Int64Type, merged)
}

func (d *LksPlansDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lks_plans"
}

func (d *LksPlansDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *LksPlansDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "LKS (Latitude Kubernetes Service) plans data source - the machine plans an LKS node pool can be built from, with per-region availability and stock. `GET /plans/lks` is its own catalog: an ordinary server plan slug from `latitudesh_plan` is not interchangeable with one of these. Pricing is not exposed, matching `latitudesh_plan`. Never scoped to a project; `site` is the only argument — set it to get `stock`, omit it for the whole catalog.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Synthetic identifier for this query: the `site` filter, or `all` when unfiltered.",
				Computed:            true,
			},
			"site": schema.StringAttribute{
				MarkdownDescription: "Only return plans that have capacity at this site **right now**, and fill `stock` for it. Use the same slug you pass to `latitudesh_lks.site`. Omit to return the whole catalog.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"stock": schema.MapAttribute{
				MarkdownDescription: "Nodes available at `site` right now, keyed by plan slug — every entry is greater than zero, because a plan with no capacity is not returned. This is the answer to \"which plan can I build here, and how many nodes of it\" with no loop and no filtering: `keys(...)` is the shortlist and `...[\"slug\"]` is the count. Null when `site` is not set, since a count is only meaningful for one site.",
				ElementType:         types.Int64Type,
				Computed:            true,
			},
			"plans": schema.ListNestedAttribute{
				MarkdownDescription: "Every LKS plan, in the order the API returns them.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "Plan ID.",
							Computed:            true,
						},
						"slug": schema.StringAttribute{
							MarkdownDescription: "Plan slug — this is the value a node pool takes as its `plan`.",
							Computed:            true,
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Human-readable plan name.",
							Computed:            true,
						},
						"in_stock_count": schema.MapAttribute{
							MarkdownDescription: "Nodes available right now, keyed by site slug — `regions[].locations.in_stock_count` flattened, since a site belongs to exactly one region. Only sites with stock appear, so `lookup(plan.in_stock_count, site, 0) > 0` is the whole buildability test for a site+plan pair.",
							ElementType:         types.Int64Type,
							Computed:            true,
						},
						"specs": schema.SingleNestedAttribute{
							MarkdownDescription: "Hardware behind one node on this plan.",
							Computed:            true,
							Attributes: map[string]schema.Attribute{
								"cpu": schema.SingleNestedAttribute{
									MarkdownDescription: "CPU specification.",
									Computed:            true,
									Attributes: map[string]schema.Attribute{
										"type": schema.StringAttribute{
											MarkdownDescription: "CPU model.",
											Computed:            true,
										},
										"clock": schema.Float64Attribute{
											MarkdownDescription: "Clock speed in GHz.",
											Computed:            true,
										},
										"cores": schema.Int64Attribute{
											MarkdownDescription: "Cores per CPU.",
											Computed:            true,
										},
										"count": schema.Int64Attribute{
											MarkdownDescription: "Number of physical CPUs.",
											Computed:            true,
										},
									},
								},
								"memory_total": schema.StringAttribute{
									MarkdownDescription: "Total memory, as returned by the API. Typed as a string because the API answers either a number or free-form text here, the same way `latitudesh_plan`'s `drives.size` does.",
									Computed:            true,
								},
								"drives": schema.ListNestedAttribute{
									MarkdownDescription: "Disk groups included in the plan.",
									Computed:            true,
									NestedObject: schema.NestedAttributeObject{
										Attributes: map[string]schema.Attribute{
											"count": schema.Int64Attribute{
												MarkdownDescription: "Number of disks in this group.",
												Computed:            true,
											},
											"size": schema.StringAttribute{
												MarkdownDescription: "Size of each disk in this group, as free-form text returned by the API (e.g. \"1.9 TB\", \"480GB\").",
												Computed:            true,
											},
											"type": schema.StringAttribute{
												MarkdownDescription: "Disk type for this group (e.g. SSD, NVME).",
												Computed:            true,
											},
										},
									},
								},
								"nics": schema.ListNestedAttribute{
									MarkdownDescription: "Network interface groups.",
									Computed:            true,
									NestedObject: schema.NestedAttributeObject{
										Attributes: map[string]schema.Attribute{
											"count": schema.Int64Attribute{
												MarkdownDescription: "Number of NICs in this group.",
												Computed:            true,
											},
											"type": schema.StringAttribute{
												MarkdownDescription: "NIC type or link speed.",
												Computed:            true,
											},
										},
									},
								},
								"gpu": schema.SingleNestedAttribute{
									MarkdownDescription: "GPU specification, null on plans without one. The API answers an empty object rather than null for those, which this collapses to null.",
									Computed:            true,
									Attributes: map[string]schema.Attribute{
										"count": schema.Int64Attribute{
											MarkdownDescription: "Number of GPUs.",
											Computed:            true,
										},
										"type": schema.StringAttribute{
											MarkdownDescription: "GPU model.",
											Computed:            true,
										},
										"vram_per_gpu": schema.Int64Attribute{
											MarkdownDescription: "VRAM per GPU.",
											Computed:            true,
										},
										"interconnect": schema.StringAttribute{
											MarkdownDescription: "GPU interconnect (e.g. NVLink).",
											Computed:            true,
										},
									},
								},
							},
						},
						"regions": schema.ListNestedAttribute{
							MarkdownDescription: "Where this plan can run, and whether it currently can. `available_sites` is the full list; `in_stock_sites` is the subset with capacity right now — a plan available at a site with no stock will fail to build a node pool there.",
							Computed:            true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"name": schema.StringAttribute{
										MarkdownDescription: "Region name.",
										Computed:            true,
									},
									"stock_level": schema.StringAttribute{
										MarkdownDescription: "Stock level in this region: `unavailable`, `low`, `medium` or `high`.",
										Computed:            true,
									},
									"available_sites": schema.ListAttribute{
										MarkdownDescription: "Site slugs where LKS can deploy this plan. These match `latitudesh_lks_sites`, and `latitudesh_lks.site`.",
										ElementType:         types.StringType,
										Computed:            true,
									},
									"in_stock_sites": schema.ListAttribute{
										MarkdownDescription: "Site slugs that have capacity for a node on this plan right now.",
										ElementType:         types.StringType,
										Computed:            true,
									},
									"in_stock_count": schema.MapAttribute{
										MarkdownDescription: "Available node count keyed by site slug. Only sites with stock appear.",
										ElementType:         types.Int64Type,
										Computed:            true,
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

// lksPlanStockAt reports how many nodes of a plan are available at a site, and
// whether the site appears at all — absent means no capacity, which is a
// different answer from zero and the reason this returns a second value.
func lksPlanStockAt(item LksPlanItemModel, site string) (int64, bool) {
	if item.InStockCount.IsNull() || item.InStockCount.IsUnknown() {
		return 0, false
	}
	elem, ok := item.InStockCount.Elements()[site]
	if !ok {
		return 0, false
	}
	count, ok := elem.(types.Int64)
	if !ok || count.IsNull() || count.ValueInt64() <= 0 {
		return 0, false
	}
	return count.ValueInt64(), true
}

func (d *LksPlansDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data LksPlansDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	// GetLksPlans takes no parameters and returns no pagination metadata: one
	// call is the whole catalog.
	res, err := d.client.Plans.GetLksPlans(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to list LKS plans, got error: "+err.Error())
		return
	}

	site := strings.TrimSpace(data.Site.ValueString())
	filtered := !data.Site.IsNull() && site != ""

	if filtered {
		data.ID = types.StringValue(site)
	} else {
		data.ID = types.StringValue("all")
	}

	items := make([]LksPlanItemModel, 0)
	stock := map[string]int64{}
	if res != nil && res.LksPlans != nil {
		plans := res.LksPlans.Data
		for i := range plans {
			item, diags := lksPlanItemValue(ctx, &plans[i])
			resp.Diagnostics.Append(diags...)
			if resp.Diagnostics.HasError() {
				return
			}

			if filtered {
				// A plan with no capacity at the site cannot build a node pool
				// there, so it is not an answer to the question being asked.
				count, ok := lksPlanStockAt(item, site)
				if !ok {
					continue
				}
				stock[item.Slug.ValueString()] = count
			}

			items = append(items, item)
		}
	}

	if filtered {
		stockMap, diags := types.MapValueFrom(ctx, types.Int64Type, stock)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		data.Stock = stockMap
	} else {
		data.Stock = types.MapNull(types.Int64Type)
	}

	list, diags := types.ListValueFrom(ctx, lksPlanItemObjectType, items)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Plans = list

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
