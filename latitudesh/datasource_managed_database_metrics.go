package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	"github.com/latitudesh/latitudesh-go-sdk/retry"

	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

// metricsRetryConfig caps how long a metrics read may spend retrying transient
// upstream errors (the SDK retries 429/500/502/503/504). This data source is
// read during `terraform plan`, so it overrides the provider-wide 5-minute
// retry budget with a short one: a persistent 5xx (e.g. a 502 from the metrics
// backend) then fails the plan in a few seconds with the real error, instead of
// hanging while the backoff walks all the way to MaxElapsedTime.
var metricsRetryConfig = retry.Config{
	Strategy: "backoff",
	Backoff: &retry.BackoffStrategy{
		InitialInterval: 500,
		MaxInterval:     2000,
		Exponent:        1.5,
		MaxElapsedTime:  10000,
	},
	RetryConnectionErrors: false,
}

var (
	_ datasource.DataSource              = &ManagedDatabaseMetricsDataSource{}
	_ datasource.DataSourceWithConfigure = &ManagedDatabaseMetricsDataSource{}
)

func NewManagedDatabaseMetricsDataSource() datasource.DataSource {
	return &ManagedDatabaseMetricsDataSource{}
}

type ManagedDatabaseMetricsDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type ManagedDatabaseMetricsDataSourceModel struct {
	// Selectors
	ManagedDatabaseID types.String `tfsdk:"managed_database_id"`
	Period            types.Int64  `tfsdk:"period"`
	Queries           types.String `tfsdk:"queries"`

	// Attributes
	From    types.String `tfsdk:"from"`
	To      types.String `tfsdk:"to"`
	Metrics types.Map    `tfsdk:"metrics"`
}

// ManagedDatabaseMetricPointModel is a single timestamped sample of a metric.
type ManagedDatabaseMetricPointModel struct {
	Timestamp types.String  `tfsdk:"timestamp"`
	Value     types.Float64 `tfsdk:"value"`
}

var managedDatabaseMetricPointObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"timestamp": types.StringType,
		"value":     types.Float64Type,
	},
}

// ManagedDatabaseMetricModel is one entry of the metrics map, keyed by metric
// name (e.g. "cpuUsage", "memoryUsage").
type ManagedDatabaseMetricModel struct {
	Unit    types.String  `tfsdk:"unit"`
	Current types.Float64 `tfsdk:"current"`
	Points  types.List    `tfsdk:"points"`
}

var managedDatabaseMetricObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"unit":    types.StringType,
		"current": types.Float64Type,
		"points":  types.ListType{ElemType: managedDatabaseMetricPointObjectType},
	},
}

func (d *ManagedDatabaseMetricsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_managed_database_metrics"
}

func (d *ManagedDatabaseMetricsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *ManagedDatabaseMetricsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Managed database metrics data source - retrieve a time window of metrics for a managed database (CPU, memory, TPS, connections, deadlocks, blocked queries, database size).",
		Attributes: map[string]schema.Attribute{
			"managed_database_id": schema.StringAttribute{
				MarkdownDescription: "Managed database ID to fetch metrics for.",
				Required:            true,
			},
			"period": schema.Int64Attribute{
				MarkdownDescription: "Time window in seconds. One of `1800`, `3600`, `21600`, `86400`, `604800`. Defaults to `1800`.",
				Optional:            true,
				Validators: []validator.Int64{
					int64validator.OneOf(1800, 3600, 21600, 86400, 604800),
				},
			},
			"queries": schema.StringAttribute{
				MarkdownDescription: "Comma-separated metrics to fetch. Defaults to all: `cpuUsage`, `memoryUsage`, `tpsUsage`, `maxConnections`, `deadlocks`, `blockedQueries`, `databaseSize`.",
				Optional:            true,
			},

			"from": schema.StringAttribute{
				MarkdownDescription: "Start of the metrics time window (RFC3339).",
				Computed:            true,
			},
			"to": schema.StringAttribute{
				MarkdownDescription: "End of the metrics time window (RFC3339).",
				Computed:            true,
			},
			"metrics": schema.MapNestedAttribute{
				MarkdownDescription: "Metrics keyed by metric name (e.g. `cpuUsage`).",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"unit": schema.StringAttribute{
							MarkdownDescription: "Unit the metric is reported in.",
							Computed:            true,
						},
						"current": schema.Float64Attribute{
							MarkdownDescription: "Most recent value of the metric. Null when the metric has no current sample.",
							Computed:            true,
						},
						"points": schema.ListNestedAttribute{
							MarkdownDescription: "Time series samples for the metric, as returned by the API.",
							Computed:            true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"timestamp": schema.StringAttribute{
										MarkdownDescription: "Sample timestamp (RFC3339).",
										Computed:            true,
									},
									"value": schema.Float64Attribute{
										MarkdownDescription: "Sample value.",
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

func (d *ManagedDatabaseMetricsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data ManagedDatabaseMetricsDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	managedDatabaseID := data.ManagedDatabaseID.ValueString()

	var period *int64
	if !data.Period.IsNull() {
		v := data.Period.ValueInt64()
		period = &v
	}

	var queries *string
	if !data.Queries.IsNull() {
		v := data.Queries.ValueString()
		queries = &v
	}

	res, err := d.client.ManagedDatabases.ShowManagedDatabaseMetrics(ctx, managedDatabaseID, period, queries, operations.WithRetries(metricsRetryConfig))
	if err != nil {
		if managedDatabaseMetricsNotFound(err) {
			resp.Diagnostics.AddError("Not Found", fmt.Sprintf("No managed database exists with ID %q", managedDatabaseID))
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read managed database metrics: %s", err.Error()))
		return
	}
	if res.Object == nil {
		resp.Diagnostics.AddError("API Error", "Managed database metrics response did not contain any data.")
		return
	}

	body := res.Object
	data.From = types.StringValue(body.From.Format(time.RFC3339))
	data.To = types.StringValue(body.To.Format(time.RFC3339))

	metrics, diags := managedDatabaseMetricsValue(ctx, body.Metrics)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Metrics = metrics

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// managedDatabaseMetricsNotFound reports whether err is a 404 from the metrics
// endpoint (e.g. an unknown managed_database_id).
func managedDatabaseMetricsNotFound(err error) bool {
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

// managedDatabaseMetricsValue maps the metrics map keyed by metric name. Map
// element order is not significant to Terraform's Map type (equality is
// key-based, not order-based), so the source map's iteration order is not
// normalized here.
func managedDatabaseMetricsValue(ctx context.Context, metrics map[string]operations.Metrics) (types.Map, diag.Diagnostics) {
	var diags diag.Diagnostics

	if len(metrics) == 0 {
		return types.MapValueMust(managedDatabaseMetricObjectType, map[string]attr.Value{}), diags
	}

	models := make(map[string]ManagedDatabaseMetricModel, len(metrics))
	for name, m := range metrics {
		points := make([]ManagedDatabaseMetricPointModel, 0, len(m.Points))
		for _, p := range m.Points {
			points = append(points, ManagedDatabaseMetricPointModel{
				Timestamp: types.StringValue(p.Timestamp.Format(time.RFC3339)),
				Value:     types.Float64Value(p.Value),
			})
		}
		pointsList, d := types.ListValueFrom(ctx, managedDatabaseMetricPointObjectType, points)
		diags.Append(d...)

		models[name] = ManagedDatabaseMetricModel{
			Unit:    types.StringValue(m.Unit),
			Current: types.Float64PointerValue(m.Current),
			Points:  pointsList,
		}
	}

	mapVal, d := types.MapValueFrom(ctx, managedDatabaseMetricObjectType, models)
	diags.Append(d...)
	return mapVal, diags
}
