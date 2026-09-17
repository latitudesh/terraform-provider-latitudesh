package latitudesh

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"

	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var (
	_ datasource.DataSource              = &LkDataSource{}
	_ datasource.DataSourceWithConfigure = &LkDataSource{}
)

func NewLkDataSource() datasource.DataSource {
	return &LkDataSource{}
}

type LkDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type LkDataSourceModel struct {
	ID                   types.String `tfsdk:"id"`
	Project              types.String `tfsdk:"project"`
	Name                 types.String `tfsdk:"name"`
	Site                 types.String `tfsdk:"site"`
	KubernetesVersion    types.String `tfsdk:"kubernetes_version"`
	Description          types.String `tfsdk:"description"`
	Network              types.Object `tfsdk:"network"`
	Status               types.String `tfsdk:"status"`
	Message              types.String `tfsdk:"message"`
	Reason               types.String `tfsdk:"reason"`
	ControlPlaneEndpoint types.String `tfsdk:"control_plane_endpoint"`
	KubeconfigURL        types.String `tfsdk:"kubeconfig_url"`
	PlatformVersion      types.String `tfsdk:"platform_version"`
	CreatedAt            types.String `tfsdk:"created_at"`
	UpdatedAt            types.String `tfsdk:"updated_at"`
}

func (d *LkDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lk"
}

func (d *LkDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *LkDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "LKS (Latitude Kubernetes Service) cluster data source - lookup a cluster by id. `GetLksCluster` has no name-based lookup, so `id` is the only selector.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "LKS cluster identifier to look up.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "The project ID that owns the cluster.",
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
			"description": schema.StringAttribute{
				MarkdownDescription: "Customer description, if any.",
				Computed:            true,
			},
			"network": schema.SingleNestedAttribute{
				MarkdownDescription: "Cluster CIDR ranges.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"pod_cidrs": schema.SetAttribute{
						MarkdownDescription: "Pod CIDR ranges.",
						ElementType:         types.StringType,
						Computed:            true,
					},
					"service_cidrs": schema.SetAttribute{
						MarkdownDescription: "Service CIDR ranges.",
						ElementType:         types.StringType,
						Computed:            true,
					},
					"node_cidrs": schema.SetAttribute{
						MarkdownDescription: "Node CIDR ranges.",
						ElementType:         types.StringType,
						Computed:            true,
					},
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Cluster lifecycle status. Open enum sourced from the platform controller; values in use today are `provisioning`, `ready`, `updating`, `scaling`, `upgrading`, `paused`, `deleting` and `deleted`, and new ones may appear without notice.",
				Computed:            true,
			},
			"message": schema.StringAttribute{
				MarkdownDescription: "Human-readable detail behind the current `status`.",
				Computed:            true,
			},
			"reason": schema.StringAttribute{
				MarkdownDescription: "Machine-readable status reason (open enum).",
				Computed:            true,
			},
			"control_plane_endpoint": schema.StringAttribute{
				MarkdownDescription: "Kubernetes API server endpoint.",
				Computed:            true,
			},
			"kubeconfig_url": schema.StringAttribute{
				MarkdownDescription: "URL to fetch the cluster kubeconfig from once the control plane is ready.",
				Computed:            true,
			},
			"platform_version": schema.StringAttribute{
				MarkdownDescription: "Platform (LKS controller) version managing this cluster.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Timestamp when the cluster was created.",
				Computed:            true,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "Timestamp when the cluster was last updated.",
				Computed:            true,
			},
		},
	}
}

func (d *LkDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data LkDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	if data.ID.IsUnknown() {
		resp.Diagnostics.AddError("Unknown selector value", "'id' is unknown. Please provide a concrete value.")
		return
	}

	id := data.ID.ValueString()

	result, err := d.client.Lks.GetLksCluster(ctx, id)
	if err != nil {
		if lkClusterNotFound(err) {
			resp.Diagnostics.AddError("LKS cluster not found", "No LKS cluster exists with ID \""+id+"\"")
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to read LKS cluster, got error: "+err.Error())
		return
	}

	if result == nil || result.LksCluster == nil || result.LksCluster.Data == nil {
		resp.Diagnostics.AddError("LKS cluster not found", "No LKS cluster exists with ID \""+id+"\"")
		return
	}

	obj := result.LksCluster.Data
	if obj.ID != nil {
		data.ID = types.StringValue(*obj.ID)
	}

	fields, mapDiags := mapLkAttributes(obj.Attributes)
	resp.Diagnostics.Append(mapDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.Project = fields.Project
	data.Name = fields.Name
	data.Site = fields.Site
	data.KubernetesVersion = fields.KubernetesVersion
	data.Description = fields.Description
	data.Network = fields.Network
	data.Status = fields.Status
	data.Message = fields.Message
	data.Reason = fields.Reason
	data.ControlPlaneEndpoint = fields.ControlPlaneEndpoint
	data.KubeconfigURL = fields.KubeconfigURL
	data.PlatformVersion = fields.PlatformVersion
	data.CreatedAt = fields.CreatedAt
	data.UpdatedAt = fields.UpdatedAt

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
