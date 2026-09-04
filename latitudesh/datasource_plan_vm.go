package latitudesh

import (
	"context"
	"fmt"
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
	_ datasource.DataSource              = &PlanVMDataSource{}
	_ datasource.DataSourceWithConfigure = &PlanVMDataSource{}
)

func NewPlanVMDataSource() datasource.DataSource {
	return &PlanVMDataSource{}
}

type PlanVMDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

// PlanVMDataSourceModel describes the data source model. Presentation-only
// catalog fields (there are none on VirtualMachinePlansAttributes — no
// description, logo, URL, or timestamp) are not excluded because none exist;
// the JSON:API envelope field `type` ("virtual_machine_plans", constant for
// every row) is the only attribute deliberately left out, see sdk-coverage.yaml.
type PlanVMDataSourceModel struct {
	// Selectors (exactly one)
	ID   types.String `tfsdk:"id"`
	Slug types.String `tfsdk:"slug"`
	Name types.String `tfsdk:"name"`

	// Capacity / compatibility attributes
	Memory                    types.Int64  `tfsdk:"memory"`
	GPU                       types.String `tfsdk:"gpu"`
	VramPerGpu                types.Int64  `tfsdk:"vram_per_gpu"`
	Vcpus                     types.Int64  `tfsdk:"vcpus"`
	Vcpu                      types.Object `tfsdk:"vcpu"`
	Nics                      types.List   `tfsdk:"nics"`
	Disk                      types.Object `tfsdk:"disk"`
	Regions                   types.List   `tfsdk:"regions"`
	StockLevel                types.String `tfsdk:"stock_level"`
	AvailableOperatingSystems types.List   `tfsdk:"available_operating_systems"`
}

type PlanVMVcpuModel struct {
	Count types.Int64   `tfsdk:"count"`
	Clock types.Float64 `tfsdk:"clock"`
	Type  types.String  `tfsdk:"type"`
}

var planVMVcpuObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"count": types.Int64Type,
		"clock": types.Float64Type,
		"type":  types.StringType,
	},
}

type PlanVMNicModel struct {
	Type  types.String `tfsdk:"type"`
	Count types.Int64  `tfsdk:"count"`
}

var planVMNicObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"type":  types.StringType,
		"count": types.Int64Type,
	},
}

type PlanVMDiskSizeModel struct {
	Amount types.Int64  `tfsdk:"amount"`
	Unit   types.String `tfsdk:"unit"`
}

var planVMDiskSizeObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"amount": types.Int64Type,
		"unit":   types.StringType,
	},
}

type PlanVMDiskModel struct {
	Type types.String `tfsdk:"type"`
	Size types.Object `tfsdk:"size"`
}

var planVMDiskObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"type": types.StringType,
		"size": planVMDiskSizeObjectType,
	},
}

type PlanVMPricingModel struct {
	Hour  types.Float64 `tfsdk:"hour"`
	Month types.Float64 `tfsdk:"month"`
	Year  types.Float64 `tfsdk:"year"`
}

var planVMPricingObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"hour":  types.Float64Type,
		"month": types.Float64Type,
		"year":  types.Float64Type,
	},
}

type PlanVMLocationsModel struct {
	Available types.List `tfsdk:"available"`
	InStock   types.List `tfsdk:"in_stock"`
}

var planVMLocationsObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"available": types.ListType{ElemType: types.StringType},
		"in_stock":  types.ListType{ElemType: types.StringType},
	},
}

type PlanVMRegionModel struct {
	Name       types.String `tfsdk:"name"`
	Available  types.List   `tfsdk:"available"`
	Pricing    types.Map    `tfsdk:"pricing"`
	Locations  types.Object `tfsdk:"locations"`
	StockLevel types.String `tfsdk:"stock_level"`
}

var planVMRegionObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"name":        types.StringType,
		"available":   types.ListType{ElemType: types.StringType},
		"pricing":     types.MapType{ElemType: planVMPricingObjectType},
		"locations":   planVMLocationsObjectType,
		"stock_level": types.StringType,
	},
}

func (d *PlanVMDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plan_vm"
}

func (d *PlanVMDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *PlanVMDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Virtual machine plan data source - lookup a virtual machine plan by id, slug, or name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Virtual machine plan ID to look up.",
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
				MarkdownDescription: "Virtual machine plan slug to look up. This is the value expected by `latitudesh_virtual_machine.plan`.",
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
				MarkdownDescription: "Virtual machine plan name to look up.",
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
			"memory": schema.Int64Attribute{
				MarkdownDescription: "Total memory, in GB.",
				Computed:            true,
			},
			"gpu": schema.StringAttribute{
				MarkdownDescription: "The GPU type, if this plan includes a GPU.",
				Computed:            true,
			},
			"vram_per_gpu": schema.Int64Attribute{
				MarkdownDescription: "VRAM per GPU, in GB.",
				Computed:            true,
			},
			"vcpus": schema.Int64Attribute{
				MarkdownDescription: "The number of virtual CPUs. Legacy field; prefer `vcpu.count`.",
				Computed:            true,
			},
			"vcpu": schema.SingleNestedAttribute{
				MarkdownDescription: "Detailed vCPU specifications.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"count": schema.Int64Attribute{
						MarkdownDescription: "The number of virtual CPUs.",
						Computed:            true,
					},
					"clock": schema.Float64Attribute{
						MarkdownDescription: "The CPU clock speed, in GHz.",
						Computed:            true,
					},
					"type": schema.StringAttribute{
						MarkdownDescription: "The CPU type/model.",
						Computed:            true,
					},
				},
			},
			"nics": schema.ListNestedAttribute{
				MarkdownDescription: "Network interface cards included in the plan.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type": schema.StringAttribute{
							MarkdownDescription: "NIC speed/type.",
							Computed:            true,
						},
						"count": schema.Int64Attribute{
							MarkdownDescription: "Number of NICs.",
							Computed:            true,
						},
					},
				},
			},
			"disk": schema.SingleNestedAttribute{
				MarkdownDescription: "The plan's disk.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						MarkdownDescription: "The type of the disk (e.g., local SSD, local NVMe).",
						Computed:            true,
					},
					"size": schema.SingleNestedAttribute{
						MarkdownDescription: "The disk size.",
						Computed:            true,
						Attributes: map[string]schema.Attribute{
							"amount": schema.Int64Attribute{
								MarkdownDescription: "The total size of the disk.",
								Computed:            true,
							},
							"unit": schema.StringAttribute{
								MarkdownDescription: "The unit of the disk size (e.g. \"gib\").",
								Computed:            true,
							},
						},
					},
				},
			},
			"regions": schema.ListNestedAttribute{
				MarkdownDescription: "Regions where this plan can be deployed, with per-region stock and pricing.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Region name.",
							Computed:            true,
						},
						"available": schema.ListAttribute{
							MarkdownDescription: "Sites in this region where the plan is offered.",
							ElementType:         types.StringType,
							Computed:            true,
						},
						"pricing": schema.MapNestedAttribute{
							MarkdownDescription: "Prices for this region, keyed by ISO 4217 currency code (e.g. USD, BRL).",
							Computed:            true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"hour": schema.Float64Attribute{
										MarkdownDescription: "Hourly price.",
										Computed:            true,
									},
									"month": schema.Float64Attribute{
										MarkdownDescription: "Monthly price.",
										Computed:            true,
									},
									"year": schema.Float64Attribute{
										MarkdownDescription: "Yearly price.",
										Computed:            true,
									},
								},
							},
						},
						"locations": schema.SingleNestedAttribute{
							MarkdownDescription: "Sites in this region, broken down by availability.",
							Computed:            true,
							Attributes: map[string]schema.Attribute{
								"available": schema.ListAttribute{
									MarkdownDescription: "Sites with clusters that support this plan.",
									ElementType:         types.StringType,
									Computed:            true,
								},
								"in_stock": schema.ListAttribute{
									MarkdownDescription: "Sites with available capacity for this plan.",
									ElementType:         types.StringType,
									Computed:            true,
								},
							},
						},
						"stock_level": schema.StringAttribute{
							MarkdownDescription: "The stock level in this region (`low`, `medium`, `high`, or `unavailable`).",
							Computed:            true,
						},
					},
				},
			},
			"stock_level": schema.StringAttribute{
				MarkdownDescription: "The overall stock level of the plan (`low`, `medium`, `high`, or `unavailable`).",
				Computed:            true,
			},
			"available_operating_systems": schema.ListAttribute{
				MarkdownDescription: "Operating system slugs compatible with this plan, as accepted by `latitudesh_virtual_machine.operating_system`.",
				ElementType:         types.StringType,
				Computed:            true,
			},
		},
	}
}

func (d *PlanVMDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data PlanVMDataSourceModel

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

	var (
		selector path.Path
		value    string
		idQ      string
		slugQ    string
		nameQ    string
	)
	switch {
	case !data.ID.IsNull():
		selector, value = path.Root("id"), data.ID.ValueString()
		idQ = value
	case !data.Slug.IsNull():
		selector, value = path.Root("slug"), data.Slug.ValueString()
		slugQ = value
	case !data.Name.IsNull():
		selector, value = path.Root("name"), data.Name.ValueString()
		nameQ = value
	default:
		resp.Diagnostics.AddError(
			"Missing selector",
			"Exactly one of 'id', 'slug', or 'name' must be provided.",
		)
		return
	}

	// ExactlyOneOf only checks that a selector is set, not that it has content.
	// Reject blank values here rather than walking the whole catalog for a
	// guaranteed miss and reporting it as "not found".
	if strings.TrimSpace(value) == "" {
		resp.Diagnostics.AddAttributeError(
			selector,
			"Blank selector",
			fmt.Sprintf("%q must not be empty or whitespace-only.", selector.String()),
		)
		return
	}

	plan, err := d.findOne(ctx, idQ, slugQ, nameQ)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", err.Error())
		return
	}
	if plan == nil {
		resp.Diagnostics.AddError("Virtual Machine Plan Not Found", "No virtual machine plan matches the given selector.")
		return
	}

	data.Nics = types.ListValueMust(planVMNicObjectType, []attr.Value{})
	data.Regions = types.ListValueMust(planVMRegionObjectType, []attr.Value{})
	data.AvailableOperatingSystems = types.ListValueMust(types.StringType, []attr.Value{})
	data.Vcpu = types.ObjectNull(planVMVcpuObjectType.AttrTypes)
	data.Disk = types.ObjectNull(planVMDiskObjectType.AttrTypes)

	if plan.ID != nil {
		data.ID = types.StringValue(*plan.ID)
	}

	if plan.Attributes != nil {
		attrs := plan.Attributes

		data.Name = types.StringPointerValue(attrs.Name)
		data.Slug = types.StringPointerValue(attrs.Slug)
		if attrs.StockLevel != nil {
			data.StockLevel = types.StringValue(string(*attrs.StockLevel))
		} else {
			data.StockLevel = types.StringNull()
		}

		if attrs.AvailableOperatingSystems != nil {
			list, diags := types.ListValueFrom(ctx, types.StringType, attrs.AvailableOperatingSystems)
			resp.Diagnostics.Append(diags...)
			if resp.Diagnostics.HasError() {
				return
			}
			data.AvailableOperatingSystems = list
		}

		if attrs.Specs != nil {
			specs := attrs.Specs

			data.Memory = types.Int64PointerValue(specs.Memory)
			data.GPU = types.StringPointerValue(specs.Gpu)
			data.VramPerGpu = types.Int64PointerValue(specs.VramPerGpu)
			data.Vcpus = types.Int64PointerValue(specs.Vcpus)

			if specs.Vcpu != nil {
				vcpu, diags := types.ObjectValueFrom(ctx, planVMVcpuObjectType.AttrTypes, PlanVMVcpuModel{
					Count: types.Int64PointerValue(specs.Vcpu.Count),
					Clock: types.Float64PointerValue(specs.Vcpu.Clock),
					Type:  types.StringPointerValue(specs.Vcpu.Type),
				})
				resp.Diagnostics.Append(diags...)
				if resp.Diagnostics.HasError() {
					return
				}
				data.Vcpu = vcpu
			}

			if specs.Nics != nil {
				nics, diags := planVMNicsValue(ctx, specs.Nics)
				resp.Diagnostics.Append(diags...)
				if resp.Diagnostics.HasError() {
					return
				}
				data.Nics = nics
			}

			if specs.Disk != nil {
				disk, diags := planVMDiskValue(ctx, specs.Disk)
				resp.Diagnostics.Append(diags...)
				if resp.Diagnostics.HasError() {
					return
				}
				data.Disk = disk
			}
		}

		if attrs.Regions != nil {
			regions, diags := planVMRegionsValue(ctx, attrs.Regions)
			resp.Diagnostics.Append(diags...)
			if resp.Diagnostics.HasError() {
				return
			}
			data.Regions = regions
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func planVMNicsValue(ctx context.Context, nics []components.VirtualMachinePlansNics) (types.List, diag.Diagnostics) {
	models := make([]PlanVMNicModel, 0, len(nics))
	for _, n := range nics {
		models = append(models, PlanVMNicModel{
			Type:  types.StringPointerValue(n.Type),
			Count: types.Int64PointerValue(n.Count),
		})
	}
	return types.ListValueFrom(ctx, planVMNicObjectType, models)
}

func planVMDiskValue(ctx context.Context, disk *components.Disk) (types.Object, diag.Diagnostics) {
	size := types.ObjectNull(planVMDiskSizeObjectType.AttrTypes)
	if disk.Size != nil {
		var unit types.String
		if disk.Size.Unit != nil {
			unit = types.StringValue(string(*disk.Size.Unit))
		} else {
			unit = types.StringNull()
		}
		sizeValue, diags := types.ObjectValueFrom(ctx, planVMDiskSizeObjectType.AttrTypes, PlanVMDiskSizeModel{
			Amount: types.Int64PointerValue(disk.Size.Amount),
			Unit:   unit,
		})
		if diags.HasError() {
			return types.ObjectNull(planVMDiskObjectType.AttrTypes), diags
		}
		size = sizeValue
	}
	return types.ObjectValueFrom(ctx, planVMDiskObjectType.AttrTypes, PlanVMDiskModel{
		Type: types.StringPointerValue(disk.Type),
		Size: size,
	})
}

func planVMRegionsValue(ctx context.Context, regions []components.VirtualMachinePlansRegions) (types.List, diag.Diagnostics) {
	models := make([]PlanVMRegionModel, 0, len(regions))
	for _, r := range regions {
		available, diags := types.ListValueFrom(ctx, types.StringType, r.Available)
		if diags.HasError() {
			return types.ListNull(planVMRegionObjectType), diags
		}

		locations := types.ObjectNull(planVMLocationsObjectType.AttrTypes)
		if r.Locations != nil {
			locAvailable, diags := types.ListValueFrom(ctx, types.StringType, r.Locations.Available)
			if diags.HasError() {
				return types.ListNull(planVMRegionObjectType), diags
			}
			locInStock, diags := types.ListValueFrom(ctx, types.StringType, r.Locations.InStock)
			if diags.HasError() {
				return types.ListNull(planVMRegionObjectType), diags
			}
			locValue, diags := types.ObjectValueFrom(ctx, planVMLocationsObjectType.AttrTypes, PlanVMLocationsModel{
				Available: locAvailable,
				InStock:   locInStock,
			})
			if diags.HasError() {
				return types.ListNull(planVMRegionObjectType), diags
			}
			locations = locValue
		}

		pricingModels := make(map[string]PlanVMPricingModel, len(r.Pricing))
		for currency, p := range r.Pricing {
			pricingModels[currency] = PlanVMPricingModel{
				Hour:  types.Float64PointerValue(p.Hour),
				Month: types.Float64PointerValue(p.Month),
				Year:  types.Float64PointerValue(p.Year),
			}
		}
		pricing, diags := types.MapValueFrom(ctx, planVMPricingObjectType, pricingModels)
		if diags.HasError() {
			return types.ListNull(planVMRegionObjectType), diags
		}

		var stockLevel types.String
		if r.StockLevel != nil {
			stockLevel = types.StringValue(string(*r.StockLevel))
		} else {
			stockLevel = types.StringNull()
		}

		models = append(models, PlanVMRegionModel{
			Name:       types.StringPointerValue(r.Name),
			Available:  available,
			Pricing:    pricing,
			Locations:  locations,
			StockLevel: stockLevel,
		})
	}
	return types.ListValueFrom(ctx, planVMRegionObjectType, models)
}

// findOne fetches the full VM plan catalog and returns the first plan
// matching the given selector. VM exposes no single-item read (List is its
// only method), so lookup by id, slug, or name all walk the same list.
// GetVMPlansResponse carries no Next/pagination field, so one call returns
// the whole catalog.
func (d *PlanVMDataSource) findOne(ctx context.Context, id, slug, name string) (*components.VirtualMachinePlansData, error) {
	idQ := strings.TrimSpace(id)
	slugQ := strings.TrimSpace(slug)
	nameQ := strings.TrimSpace(name)

	result, err := d.client.Plans.VM.List(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to list virtual machine plans: %w", err)
	}
	if result == nil || result.VirtualMachinePlans == nil {
		return nil, nil
	}

	for i := range result.VirtualMachinePlans.Data {
		p := result.VirtualMachinePlans.Data[i]
		if idQ != "" && p.ID != nil && strings.TrimSpace(*p.ID) == idQ {
			return &p, nil
		}
		if p.Attributes == nil {
			continue
		}
		if slugQ != "" && p.Attributes.Slug != nil && strings.TrimSpace(*p.Attributes.Slug) == slugQ {
			return &p, nil
		}
		if nameQ != "" && p.Attributes.Name != nil && strings.TrimSpace(*p.Attributes.Name) == nameQ {
			return &p, nil
		}
	}

	return nil, nil
}
