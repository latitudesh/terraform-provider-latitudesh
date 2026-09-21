package latitudesh

import (
	"context"

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
	_ datasource.DataSource              = &LksNodePoolsDataSource{}
	_ datasource.DataSourceWithConfigure = &LksNodePoolsDataSource{}
)

func NewLksNodePoolsDataSource() datasource.DataSource {
	return &LksNodePoolsDataSource{}
}

type LksNodePoolsDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type LksNodePoolsDataSourceModel struct {
	// Synthetic identifier: the cluster this lists pools for.
	ID        types.String `tfsdk:"id"`
	ClusterID types.String `tfsdk:"cluster_id"`
	NodePools types.List   `tfsdk:"node_pools"`
}

// LksNodePoolItemModel carries every attribute the list response already
// contains — the same envelope latitudesh_lks_node_pool reads. `labels` and
// `taints` in particular are why this data source is worth reading at all:
// they are what tells a pool apart from its siblings.
type LksNodePoolItemModel struct {
	ID                types.String `tfsdk:"id"`
	Name              types.String `tfsdk:"name"`
	Plan              types.String `tfsdk:"plan"`
	NodeCount         types.Int64  `tfsdk:"node_count"`
	ReadyNodes        types.Int64  `tfsdk:"ready_nodes"`
	Type              types.String `tfsdk:"type"`
	Mode              types.String `tfsdk:"mode"`
	Description       types.String `tfsdk:"description"`
	KubernetesVersion types.String `tfsdk:"kubernetes_version"`
	MaxPodsPerNode    types.Int64  `tfsdk:"max_pods_per_node"`
	Labels            types.Map    `tfsdk:"labels"`
	Taints            types.Set    `tfsdk:"taints"`
	Status            types.String `tfsdk:"status"`
	Message           types.String `tfsdk:"message"`
	Reason            types.String `tfsdk:"reason"`
	PlatformVersion   types.String `tfsdk:"platform_version"`
	CreatedAt         types.String `tfsdk:"created_at"`
	UpdatedAt         types.String `tfsdk:"updated_at"`
}

var lksNodePoolItemObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":                 types.StringType,
		"name":               types.StringType,
		"plan":               types.StringType,
		"node_count":         types.Int64Type,
		"ready_nodes":        types.Int64Type,
		"type":               types.StringType,
		"mode":               types.StringType,
		"description":        types.StringType,
		"kubernetes_version": types.StringType,
		"max_pods_per_node":  types.Int64Type,
		"labels":             types.MapType{ElemType: types.StringType},
		"taints":             types.SetType{ElemType: lksTaintObjectType},
		"status":             types.StringType,
		"message":            types.StringType,
		"reason":             types.StringType,
		"platform_version":   types.StringType,
		"created_at":         types.StringType,
		"updated_at":         types.StringType,
	},
}

// lksNodePoolItemValue maps one SDK node pool into the list item model,
// through the same attribute mapper the resource uses.
func lksNodePoolItemValue(ctx context.Context, p *components.LksNodePoolData) (LksNodePoolItemModel, diag.Diagnostics) {
	fields, diags := mapLksNodePoolAttributes(ctx, p.Attributes)

	return LksNodePoolItemModel{
		ID:                types.StringPointerValue(p.ID),
		Name:              fields.Name,
		Plan:              fields.Plan,
		NodeCount:         fields.NodeCount,
		ReadyNodes:        fields.ReadyNodes,
		Type:              fields.Type,
		Mode:              fields.Mode,
		Description:       fields.Description,
		KubernetesVersion: fields.KubernetesVersion,
		MaxPodsPerNode:    fields.MaxPodsPerNode,
		Labels:            fields.Labels,
		Taints:            fields.Taints,
		Status:            fields.Status,
		Message:           fields.Message,
		Reason:            fields.Reason,
		PlatformVersion:   fields.PlatformVersion,
		CreatedAt:         fields.CreatedAt,
		UpdatedAt:         fields.UpdatedAt,
	}, diags
}

func (d *LksNodePoolsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lks_node_pools"
}

func (d *LksNodePoolsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *LksNodePoolsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "LKS (Latitude Kubernetes Service) node pools data source - every node pool in a cluster, including any created outside Terraform. Node pools are always scoped to a cluster, so `cluster_id` is required.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Synthetic identifier for this query: the `cluster_id` it lists pools for.",
				Computed:            true,
			},
			"cluster_id": schema.StringAttribute{
				MarkdownDescription: "ID of the cluster to list node pools for.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"node_pools": schema.ListNestedAttribute{
				MarkdownDescription: "The cluster's node pools, in the order the API returns them.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "Node pool ID.",
							Computed:            true,
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Display name for the pool.",
							Computed:            true,
						},
						"plan": schema.StringAttribute{
							MarkdownDescription: "Plan the pool's nodes are provisioned from.",
							Computed:            true,
						},
						"node_count": schema.Int64Attribute{
							MarkdownDescription: "Nodes the pool is sized for.",
							Computed:            true,
						},
						"ready_nodes": schema.Int64Attribute{
							MarkdownDescription: "Nodes currently ready. Below `node_count` while the pool is still building or scaling, and `null` on a pool the platform does not report it for — which is not the same as zero.",
							Computed:            true,
						},
						"type": schema.StringAttribute{
							MarkdownDescription: "Node type backing the pool, e.g. `bare_metal`.",
							Computed:            true,
						},
						"mode": schema.StringAttribute{
							MarkdownDescription: "Provisioning mode of the pool's nodes, e.g. `on_demand`.",
							Computed:            true,
						},
						"description": schema.StringAttribute{
							MarkdownDescription: "Customer description for the pool, if any.",
							Computed:            true,
						},
						"kubernetes_version": schema.StringAttribute{
							MarkdownDescription: "Kubernetes patch version running on the pool's nodes.",
							Computed:            true,
						},
						"max_pods_per_node": schema.Int64Attribute{
							MarkdownDescription: "kubelet `--max-pods` for the pool's nodes. `null` when the pool runs the platform default.",
							Computed:            true,
						},
						"labels": schema.MapAttribute{
							MarkdownDescription: "Kubernetes labels applied to every node in the pool.",
							ElementType:         types.StringType,
							Computed:            true,
						},
						"taints": schema.SetNestedAttribute{
							MarkdownDescription: "Kubernetes taints applied to every node in the pool.",
							Computed:            true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"key": schema.StringAttribute{
										MarkdownDescription: "Taint key.",
										Computed:            true,
									},
									"value": schema.StringAttribute{
										MarkdownDescription: "Taint value.",
										Computed:            true,
									},
									"effect": schema.StringAttribute{
										MarkdownDescription: "Taint effect: `NoSchedule`, `PreferNoSchedule` or `NoExecute`.",
										Computed:            true,
									},
								},
							},
						},
						"status": schema.StringAttribute{
							MarkdownDescription: "Pool lifecycle status.",
							Computed:            true,
						},
						"message": schema.StringAttribute{
							MarkdownDescription: "Human-readable detail behind the pool's current `status`.",
							Computed:            true,
						},
						"reason": schema.StringAttribute{
							MarkdownDescription: "Machine-readable status reason for the pool (open enum).",
							Computed:            true,
						},
						"platform_version": schema.StringAttribute{
							MarkdownDescription: "Platform (LKS controller) version running on the pool's nodes.",
							Computed:            true,
						},
						"created_at": schema.StringAttribute{
							MarkdownDescription: "Timestamp when the pool was created.",
							Computed:            true,
						},
						"updated_at": schema.StringAttribute{
							MarkdownDescription: "Timestamp when the pool was last updated.",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *LksNodePoolsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data LksNodePoolsDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	clusterID := data.ClusterID.ValueString()

	// ListLksNodePoolsRequest carries no page fields, so one call is the whole
	// collection — same as ListLksClusters.
	res, err := d.client.Lks.ListLksNodePools(ctx, clusterID)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to list LKS node pools, got error: "+err.Error())
		return
	}

	data.ID = types.StringValue(clusterID)

	items := make([]LksNodePoolItemModel, 0)
	if res != nil && res.LksNodePools != nil {
		pools := res.LksNodePools.Data
		for i := range pools {
			item, itemDiags := lksNodePoolItemValue(ctx, &pools[i])
			resp.Diagnostics.Append(itemDiags...)
			items = append(items, item)
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	list, diags := types.ListValueFrom(ctx, lksNodePoolItemObjectType, items)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.NodePools = list

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
