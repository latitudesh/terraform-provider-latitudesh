package latitudesh

import (
	"context"
	"time"

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
var _ datasource.DataSource = &BillingDataSource{}
var _ datasource.DataSourceWithConfigure = &BillingDataSource{}

func NewBillingDataSource() datasource.DataSource {
	return &BillingDataSource{}
}

type BillingDataSource struct {
	client         *latitudeshgosdk.Latitudesh
	defaultProject string
}

type BillingDataSourceModel struct {
	// Filters
	Project        types.String `tfsdk:"project"`
	FilterProducts types.List   `tfsdk:"filter_products"`
	FilterPlan     types.String `tfsdk:"filter_plan"`

	// Attributes
	ID                     types.String  `tfsdk:"id"`
	Type                   types.String  `tfsdk:"type"`
	AvailableCreditBalance types.Int64   `tfsdk:"available_credit_balance"`
	Amount                 types.Float64 `tfsdk:"amount"`
	Price                  types.Float64 `tfsdk:"price"`
	Threshold              types.Float64 `tfsdk:"threshold"`
	Period                 types.Object  `tfsdk:"period"`
	BillingProject         types.Object  `tfsdk:"billing_project"`
	Products               types.List    `tfsdk:"products"`
}

type BillingPeriodModel struct {
	Start types.String `tfsdk:"start"`
	End   types.String `tfsdk:"end"`
}

var billingPeriodObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"start": types.StringType,
		"end":   types.StringType,
	},
}

// BillingProjectInfoModel is the project the usage report belongs to, as
// echoed back by the API. Kept distinct from the top-level "project" filter
// attribute (which the caller supplies as an ID or slug) since the response
// carries id, slug and name together.
type BillingProjectInfoModel struct {
	ID   types.String `tfsdk:"id"`
	Slug types.String `tfsdk:"slug"`
	Name types.String `tfsdk:"name"`
}

var billingProjectInfoObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":   types.StringType,
		"slug": types.StringType,
		"name": types.StringType,
	},
}

type BillingDiscountModel struct {
	Description types.String  `tfsdk:"description"`
	Type        types.String  `tfsdk:"type"`
	Value       types.Float64 `tfsdk:"value"`
}

var billingDiscountObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"description": types.StringType,
		"type":        types.StringType,
		"value":       types.Float64Type,
	},
}

type BillingBucketModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Location types.String `tfsdk:"location"`
}

var billingBucketObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":       types.StringType,
		"name":     types.StringType,
		"location": types.StringType,
	},
}

type BillingServerModel struct {
	ID       types.String `tfsdk:"id"`
	Hostname types.String `tfsdk:"hostname"`
	Plan     types.String `tfsdk:"plan"`
	Tags     types.List   `tfsdk:"tags"`
}

var billingServerObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":       types.StringType,
		"hostname": types.StringType,
		"plan":     types.StringType,
		"tags":     types.ListType{ElemType: types.StringType},
	},
}

type BillingMetadataModel struct {
	BillingUnitDivisor types.Int64  `tfsdk:"billing_unit_divisor"`
	Bucket             types.Object `tfsdk:"bucket"`
	Servers            types.List   `tfsdk:"servers"`
}

var billingMetadataObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"billing_unit_divisor": types.Int64Type,
		"bucket":               billingBucketObjectType,
		"servers":              types.ListType{ElemType: billingServerObjectType},
	},
}

type BillingProductModel struct {
	ID                    types.String  `tfsdk:"id"`
	Resource              types.String  `tfsdk:"resource"`
	Name                  types.String  `tfsdk:"name"`
	Proration             types.Bool    `tfsdk:"proration"`
	Discounts             types.List    `tfsdk:"discounts"`
	Discountable          types.Bool    `tfsdk:"discountable"`
	Description           types.String  `tfsdk:"description"`
	AmountWithoutDiscount types.Int64   `tfsdk:"amount_without_discount"`
	Start                 types.String  `tfsdk:"start"`
	End                   types.String  `tfsdk:"end"`
	Unit                  types.String  `tfsdk:"unit"`
	UnitAmount            types.Float64 `tfsdk:"unit_amount"`
	UnitPrice             types.Float64 `tfsdk:"unit_price"`
	UsageType             types.String  `tfsdk:"usage_type"`
	Quantity              types.Float64 `tfsdk:"quantity"`
	Amount                types.Float64 `tfsdk:"amount"`
	Price                 types.Float64 `tfsdk:"price"`
	Metadata              types.Object  `tfsdk:"metadata"`
}

var billingProductObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":                      types.StringType,
		"resource":                types.StringType,
		"name":                    types.StringType,
		"proration":               types.BoolType,
		"discounts":               types.ListType{ElemType: billingDiscountObjectType},
		"discountable":            types.BoolType,
		"description":             types.StringType,
		"amount_without_discount": types.Int64Type,
		"start":                   types.StringType,
		"end":                     types.StringType,
		"unit":                    types.StringType,
		"unit_amount":             types.Float64Type,
		"unit_price":              types.Float64Type,
		"usage_type":              types.StringType,
		"quantity":                types.Float64Type,
		"amount":                  types.Float64Type,
		"price":                   types.Float64Type,
		"metadata":                billingMetadataObjectType,
	},
}

func timeValue(t *time.Time) types.String {
	if t == nil {
		return types.StringNull()
	}
	return types.StringValue(t.Format(time.RFC3339))
}

func billingPeriodValue(ctx context.Context, p *components.Period) (types.Object, diag.Diagnostics) {
	if p == nil {
		return types.ObjectNull(billingPeriodObjectType.AttrTypes), nil
	}
	model := BillingPeriodModel{
		Start: timeValue(p.Start),
		End:   timeValue(p.End),
	}
	return types.ObjectValueFrom(ctx, billingPeriodObjectType.AttrTypes, model)
}

func billingProjectInfoValue(ctx context.Context, p *components.BillingUsageProject) (types.Object, diag.Diagnostics) {
	if p == nil {
		return types.ObjectNull(billingProjectInfoObjectType.AttrTypes), nil
	}
	model := BillingProjectInfoModel{
		ID:   types.StringPointerValue(p.ID),
		Slug: types.StringPointerValue(p.Slug),
		Name: types.StringPointerValue(p.Name),
	}
	return types.ObjectValueFrom(ctx, billingProjectInfoObjectType.AttrTypes, model)
}

func billingDiscountsValue(ctx context.Context, discounts []components.Discounts) (types.List, diag.Diagnostics) {
	models := make([]BillingDiscountModel, 0, len(discounts))
	for _, d := range discounts {
		models = append(models, BillingDiscountModel{
			Description: types.StringValue(d.Description),
			Type:        types.StringValue(string(d.Type)),
			Value:       types.Float64Value(float64(d.Value)),
		})
	}
	return types.ListValueFrom(ctx, billingDiscountObjectType, models)
}

func billingBucketValue(ctx context.Context, b *components.Bucket) (types.Object, diag.Diagnostics) {
	if b == nil {
		return types.ObjectNull(billingBucketObjectType.AttrTypes), nil
	}
	model := BillingBucketModel{
		ID:       types.StringPointerValue(b.ID),
		Name:     types.StringPointerValue(b.Name),
		Location: types.StringPointerValue(b.Location),
	}
	return types.ObjectValueFrom(ctx, billingBucketObjectType.AttrTypes, model)
}

func billingServersValue(ctx context.Context, servers []components.BillingUsageServers) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	models := make([]BillingServerModel, 0, len(servers))
	for _, s := range servers {
		tags, d := types.ListValueFrom(ctx, types.StringType, s.Tags)
		diags.Append(d...)
		models = append(models, BillingServerModel{
			ID:       types.StringPointerValue(s.ID),
			Hostname: types.StringPointerValue(s.Hostname),
			Plan:     types.StringPointerValue(s.Plan),
			Tags:     tags,
		})
	}
	list, d := types.ListValueFrom(ctx, billingServerObjectType, models)
	diags.Append(d...)
	return list, diags
}

func billingMetadataValue(ctx context.Context, m *components.Metadata) (types.Object, diag.Diagnostics) {
	if m == nil {
		return types.ObjectNull(billingMetadataObjectType.AttrTypes), nil
	}

	var diags diag.Diagnostics

	servers, d := billingServersValue(ctx, m.Servers)
	diags.Append(d...)

	bucket, d := billingBucketValue(ctx, m.Bucket)
	diags.Append(d...)

	model := BillingMetadataModel{
		BillingUnitDivisor: types.Int64PointerValue(m.BillingUnitDivisor),
		Bucket:             bucket,
		Servers:            servers,
	}
	obj, d := types.ObjectValueFrom(ctx, billingMetadataObjectType.AttrTypes, model)
	diags.Append(d...)
	return obj, diags
}

func billingProductsValue(ctx context.Context, products []components.Products) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	models := make([]BillingProductModel, 0, len(products))
	for _, p := range products {
		discounts, d := billingDiscountsValue(ctx, p.Discounts)
		diags.Append(d...)

		metadata, d := billingMetadataValue(ctx, p.Metadata)
		diags.Append(d...)

		unit := types.StringNull()
		if p.Unit != nil {
			unit = types.StringValue(string(*p.Unit))
		}
		usageType := types.StringNull()
		if p.UsageType != nil {
			usageType = types.StringValue(string(*p.UsageType))
		}

		models = append(models, BillingProductModel{
			ID:                    types.StringPointerValue(p.ID),
			Resource:              types.StringPointerValue(p.Resource),
			Name:                  types.StringPointerValue(p.Name),
			Proration:             types.BoolPointerValue(p.Proration),
			Discounts:             discounts,
			Discountable:          types.BoolPointerValue(p.Discountable),
			Description:           types.StringPointerValue(p.Description),
			AmountWithoutDiscount: types.Int64PointerValue(p.AmountWithoutDiscount),
			Start:                 timeValue(p.Start),
			End:                   timeValue(p.End),
			Unit:                  unit,
			UnitAmount:            types.Float64PointerValue(p.UnitAmount),
			UnitPrice:             types.Float64PointerValue(p.UnitPrice),
			UsageType:             usageType,
			Quantity:              types.Float64PointerValue(p.Quantity),
			Amount:                types.Float64PointerValue(p.Amount),
			Price:                 types.Float64PointerValue(p.Price),
			Metadata:              metadata,
		})
	}
	list, d := types.ListValueFrom(ctx, billingProductObjectType, models)
	diags.Append(d...)
	return list, diags
}

func (d *BillingDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_billing"
}

func (d *BillingDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *BillingDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Billing data source - retrieve the billing usage of a project's current billing cycle.",
		Attributes: map[string]schema.Attribute{
			"project": schema.StringAttribute{
				MarkdownDescription: "Project (ID or slug) to retrieve usage for. Falls back to the provider-level `project` when unset.",
				Optional:            true,
			},
			"filter_products": schema.ListAttribute{
				MarkdownDescription: "Restrict usage to these product IDs.",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"filter_plan": schema.StringAttribute{
				MarkdownDescription: "Restrict usage to this plan name.",
				Optional:            true,
			},

			"id": schema.StringAttribute{
				MarkdownDescription: "Billing usage report identifier.",
				Computed:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Resource type, as returned by the API.",
				Computed:            true,
			},
			"available_credit_balance": schema.Int64Attribute{
				MarkdownDescription: "Available credit balance, in cents.",
				Computed:            true,
			},
			"amount": schema.Float64Attribute{
				MarkdownDescription: "Total usage amount, in cents.",
				Computed:            true,
			},
			"price": schema.Float64Attribute{
				MarkdownDescription: "Total usage price, in cents.",
				Computed:            true,
			},
			"threshold": schema.Float64Attribute{
				MarkdownDescription: "Threshold used to charge usage, in cents.",
				Computed:            true,
			},
			"period": schema.SingleNestedAttribute{
				MarkdownDescription: "The billing cycle period this usage covers.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"start": schema.StringAttribute{
						MarkdownDescription: "Start of the billing cycle (RFC3339).",
						Computed:            true,
					},
					"end": schema.StringAttribute{
						MarkdownDescription: "End of the billing cycle (RFC3339).",
						Computed:            true,
					},
				},
			},
			"billing_project": schema.SingleNestedAttribute{
				MarkdownDescription: "The project this usage report belongs to, as returned by the API.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"id": schema.StringAttribute{
						MarkdownDescription: "Project ID.",
						Computed:            true,
					},
					"slug": schema.StringAttribute{
						MarkdownDescription: "Project slug.",
						Computed:            true,
					},
					"name": schema.StringAttribute{
						MarkdownDescription: "Project name.",
						Computed:            true,
					},
				},
			},
			"products": schema.ListNestedAttribute{
				MarkdownDescription: "Per-product usage breakdown.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "Product ID.",
							Computed:            true,
						},
						"resource": schema.StringAttribute{
							MarkdownDescription: "Resource type this product line covers.",
							Computed:            true,
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Product name.",
							Computed:            true,
						},
						"proration": schema.BoolAttribute{
							MarkdownDescription: "Whether the product line is prorated.",
							Computed:            true,
						},
						"discountable": schema.BoolAttribute{
							MarkdownDescription: "Whether the product line is eligible for discounts.",
							Computed:            true,
						},
						"description": schema.StringAttribute{
							MarkdownDescription: "Product description.",
							Computed:            true,
						},
						"amount_without_discount": schema.Int64Attribute{
							MarkdownDescription: "Usage amount before discounts, in cents.",
							Computed:            true,
						},
						"start": schema.StringAttribute{
							MarkdownDescription: "Start of this product line's usage window (RFC3339).",
							Computed:            true,
						},
						"end": schema.StringAttribute{
							MarkdownDescription: "End of this product line's usage window (RFC3339).",
							Computed:            true,
						},
						"unit": schema.StringAttribute{
							MarkdownDescription: "Unit the product is billed in.",
							Computed:            true,
						},
						"unit_amount": schema.Float64Attribute{
							MarkdownDescription: "Unit amount of the product, in cents.",
							Computed:            true,
						},
						"unit_price": schema.Float64Attribute{
							MarkdownDescription: "Unit price of the product, in cents.",
							Computed:            true,
						},
						"usage_type": schema.StringAttribute{
							MarkdownDescription: "Usage type (e.g. licensed, metered).",
							Computed:            true,
						},
						"quantity": schema.Float64Attribute{
							MarkdownDescription: "Quantity used.",
							Computed:            true,
						},
						"amount": schema.Float64Attribute{
							MarkdownDescription: "Total usage amount for this product, in cents.",
							Computed:            true,
						},
						"price": schema.Float64Attribute{
							MarkdownDescription: "Total usage price for this product, in cents.",
							Computed:            true,
						},
						"discounts": schema.ListNestedAttribute{
							MarkdownDescription: "Discounts applied to this product line.",
							Computed:            true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"description": schema.StringAttribute{
										MarkdownDescription: "Discount description.",
										Computed:            true,
									},
									"type": schema.StringAttribute{
										MarkdownDescription: "Discount type: `percent` or `fixed`.",
										Computed:            true,
									},
									"value": schema.Float64Attribute{
										MarkdownDescription: "Discount value (percentage or fixed amount, per `type`).",
										Computed:            true,
									},
								},
							},
						},
						"metadata": schema.SingleNestedAttribute{
							MarkdownDescription: "Product-specific metadata. Populated only for products where it applies (e.g. servers, object storage buckets).",
							Computed:            true,
							Attributes: map[string]schema.Attribute{
								"billing_unit_divisor": schema.Int64Attribute{
									MarkdownDescription: "Divisor applied to quantity before pricing for products billed per divided unit. Null for products not priced this way.",
									Computed:            true,
								},
								"bucket": schema.SingleNestedAttribute{
									MarkdownDescription: "Object storage bucket this product line covers, when applicable.",
									Computed:            true,
									Attributes: map[string]schema.Attribute{
										"id": schema.StringAttribute{
											MarkdownDescription: "Bucket ID.",
											Computed:            true,
										},
										"name": schema.StringAttribute{
											MarkdownDescription: "Bucket name.",
											Computed:            true,
										},
										"location": schema.StringAttribute{
											MarkdownDescription: "Bucket location.",
											Computed:            true,
										},
									},
								},
								"servers": schema.ListNestedAttribute{
									MarkdownDescription: "Servers this product line covers, when applicable.",
									Computed:            true,
									NestedObject: schema.NestedAttributeObject{
										Attributes: map[string]schema.Attribute{
											"id": schema.StringAttribute{
												MarkdownDescription: "Server ID.",
												Computed:            true,
											},
											"hostname": schema.StringAttribute{
												MarkdownDescription: "Server hostname.",
												Computed:            true,
											},
											"plan": schema.StringAttribute{
												MarkdownDescription: "Server plan.",
												Computed:            true,
											},
											"tags": schema.ListAttribute{
												MarkdownDescription: "Tags assigned to the server.",
												ElementType:         types.StringType,
												Computed:            true,
											},
										},
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

func (d *BillingDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data BillingDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	project := data.Project.ValueString()
	if project == "" {
		project = d.defaultProject
	}
	if project == "" {
		resp.Diagnostics.AddError("Missing project",
			"Set `project` on this data source or define a default in the provider block (provider `latitudesh` { project = \"...\" }).")
		return
	}
	data.Project = types.StringValue(project)

	var filterProducts []string
	if !data.FilterProducts.IsNull() {
		resp.Diagnostics.Append(data.FilterProducts.ElementsAs(ctx, &filterProducts, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	var filterPlan *string
	if !data.FilterPlan.IsNull() {
		v := data.FilterPlan.ValueString()
		filterPlan = &v
	}

	// Defaults for the nested attributes, kept known (never null) so the
	// generated state is always well-formed even if the response omits them.
	data.Period = types.ObjectNull(billingPeriodObjectType.AttrTypes)
	data.BillingProject = types.ObjectNull(billingProjectInfoObjectType.AttrTypes)
	data.Products = types.ListValueMust(billingProductObjectType, []attr.Value{})

	res, err := d.client.Billing.ListUsage(ctx, project, filterProducts, filterPlan)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to read billing usage, got error: "+err.Error())
		return
	}
	if res.BillingUsage == nil || res.BillingUsage.Data == nil {
		resp.Diagnostics.AddError("API Error", "Billing usage response did not contain any data.")
		return
	}

	usage := res.BillingUsage.Data

	if usage.ID != nil {
		data.ID = types.StringValue(*usage.ID)
	}
	if usage.Type != nil {
		data.Type = types.StringValue(*usage.Type)
	}

	if usage.Attributes != nil {
		attrs := usage.Attributes

		data.AvailableCreditBalance = types.Int64PointerValue(attrs.AvailableCreditBalance)
		data.Amount = types.Float64PointerValue(attrs.Amount)
		data.Price = types.Float64PointerValue(attrs.Price)
		data.Threshold = types.Float64PointerValue(attrs.Threshold)

		period, diags := billingPeriodValue(ctx, attrs.Period)
		resp.Diagnostics.Append(diags...)
		data.Period = period

		billingProject, diags := billingProjectInfoValue(ctx, attrs.Project)
		resp.Diagnostics.Append(diags...)
		data.BillingProject = billingProject

		products, diags := billingProductsValue(ctx, attrs.Products)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		data.Products = products
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
