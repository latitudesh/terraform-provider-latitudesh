package latitudesh

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"

	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var (
	_ datasource.DataSource              = &LksDataSource{}
	_ datasource.DataSourceWithConfigure = &LksDataSource{}
)

func NewLksDataSource() datasource.DataSource {
	return &LksDataSource{}
}

type LksDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type LksDataSourceModel struct {
	// Synthetic identifier: "<project>[/<status>]".
	ID types.String `tfsdk:"id"`

	Project types.String `tfsdk:"project"`
	Status  types.String `tfsdk:"status"`

	Clusters types.List `tfsdk:"clusters"`
}

type LkItemModel struct {
	ID                   types.String `tfsdk:"id"`
	Name                 types.String `tfsdk:"name"`
	Site                 types.String `tfsdk:"site"`
	KubernetesVersion    types.String `tfsdk:"kubernetes_version"`
	Status               types.String `tfsdk:"status"`
	ControlPlaneEndpoint types.String `tfsdk:"control_plane_endpoint"`
	CreatedAt            types.String `tfsdk:"created_at"`
}

var lkItemObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":                     types.StringType,
		"name":                   types.StringType,
		"site":                   types.StringType,
		"kubernetes_version":     types.StringType,
		"status":                 types.StringType,
		"control_plane_endpoint": types.StringType,
		"created_at":             types.StringType,
	},
}

func (d *LksDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lks"
}

func (d *LksDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *LksDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "LKS (Latitude Kubernetes Service) clusters data source - list every cluster in a project, optionally filtered by status.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Synthetic identifier for this query: `project`, followed by `/<status>` when a status filter is set.",
				Computed:            true,
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "Project (ID or slug) to list clusters for. Clusters are always scoped to a project, so this is required.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Only return clusters with this status (case-insensitive), e.g. `ready` or `provisioning`. This is an open enum controlled by the platform, so no fixed set of values is enforced here.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"clusters": schema.ListNestedAttribute{
				MarkdownDescription: "The matching clusters, in the order the API returns them.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "Cluster ID.",
							Computed:            true,
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Display name for the cluster.",
							Computed:            true,
						},
						"site": schema.StringAttribute{
							MarkdownDescription: "Site slug the cluster is deployed to.",
							Computed:            true,
						},
						"kubernetes_version": schema.StringAttribute{
							MarkdownDescription: "Kubernetes patch version currently running.",
							Computed:            true,
						},
						"status": schema.StringAttribute{
							MarkdownDescription: "Cluster lifecycle status.",
							Computed:            true,
						},
						"control_plane_endpoint": schema.StringAttribute{
							MarkdownDescription: "Kubernetes API server endpoint.",
							Computed:            true,
						},
						"created_at": schema.StringAttribute{
							MarkdownDescription: "Timestamp when the cluster was created.",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *LksDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data LksDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	if data.Project.IsUnknown() || data.Status.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unknown filter value",
			"'project' and 'status' must be known at plan time. Please provide concrete values or omit 'status'.",
		)
		return
	}

	projectFilter := strings.TrimSpace(data.Project.ValueString())
	statusFilter := strings.TrimSpace(data.Status.ValueString())

	// ListLksClustersRequest carries no page fields (unlike the paginated list
	// operations in this SDK, whose requests carry PageNumber/PageSize), so one
	// call is the whole collection for this project.
	res, err := d.client.Lks.ListLksClusters(ctx, projectFilter)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to list LKS clusters, got error: "+err.Error())
		return
	}

	id := projectFilter
	if statusFilter != "" {
		id += "/" + statusFilter
	}
	data.ID = types.StringValue(id)

	clusters := make([]components.LksClusterData, 0)
	if res != nil && res.LksClusters != nil {
		for _, c := range res.LksClusters.Data {
			if lkMatchesStatus(c, statusFilter) {
				clusters = append(clusters, c)
			}
		}
	}

	items := make([]LkItemModel, 0, len(clusters))
	for i := range clusters {
		items = append(items, lkItemValue(&clusters[i]))
	}

	list, diags := types.ListValueFrom(ctx, lkItemObjectType, items)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Clusters = list

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// lkItemValue maps one SDK cluster into the list item model. It shares the
// attribute mapper with the singular data source and resource.
func lkItemValue(c *components.LksClusterData) LkItemModel {
	fields, _ := mapLkAttributes(c.Attributes)
	return LkItemModel{
		ID:                   types.StringPointerValue(c.ID),
		Name:                 fields.Name,
		Site:                 fields.Site,
		KubernetesVersion:    fields.KubernetesVersion,
		Status:               fields.Status,
		ControlPlaneEndpoint: fields.ControlPlaneEndpoint,
		CreatedAt:            fields.CreatedAt,
	}
}

// lkMatchesStatus applies the optional, case-insensitive status filter. An
// empty filter matches everything; a cluster with no status never matches a
// non-empty one.
func lkMatchesStatus(c components.LksClusterData, want string) bool {
	if want == "" {
		return true
	}
	if c.Attributes == nil || c.Attributes.Status == nil {
		return false
	}
	return strings.EqualFold(*c.Attributes.Status, want)
}
