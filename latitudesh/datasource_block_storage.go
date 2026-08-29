package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ datasource.DataSource              = &BlockStorageDataSource{}
	_ datasource.DataSourceWithConfigure = &BlockStorageDataSource{}
)

func NewBlockStorageDataSource() datasource.DataSource {
	return &BlockStorageDataSource{}
}

type BlockStorageDataSource struct {
	client         *latitudeshgosdk.Latitudesh
	defaultProject string
}

type BlockStorageDataSourceModel struct {
	// Selectors (exactly one of id, name)
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`

	// Required alongside name; falls back to the provider default when unset.
	Project types.String `tfsdk:"project"`

	// Attributes
	Region      types.String `tfsdk:"region"`
	SizeInGb    types.Int64  `tfsdk:"size_in_gb"`
	CreatedAt   types.String `tfsdk:"created_at"`
	NamespaceID types.String `tfsdk:"namespace_id"`
	ConnectorID types.String `tfsdk:"connector_id"`
	Keyring     types.String `tfsdk:"keyring"`
	ClusterUser types.String `tfsdk:"cluster_user"`
	VolumePath  types.String `tfsdk:"volume_path"`
	Initiators  types.List   `tfsdk:"initiators"`
}

func (d *BlockStorageDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_block_storage"
}

func (d *BlockStorageDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
	d.defaultProject = deps.DefaultProject
}

func (d *BlockStorageDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Block Storage data source - lookup a volume by id, or by name (scoped to a project).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Volume identifier to look up. Mutually exclusive with `name`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("id"),
						path.MatchRoot("name"),
					),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Volume name to look up within `project`. Mutually exclusive with `id`.",
				Optional:            true,
				Computed:            true,
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "Project (ID or slug) to search when looking up by `name`. Falls back to the provider-level `project` when unset. Not used, and not returned, when looking up by `id`.",
				Optional:            true,
				Computed:            true,
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "The region (site) slug the volume was created in. Not returned by the volume read API, so this is always null.",
				Computed:            true,
			},
			"size_in_gb": schema.Int64Attribute{
				MarkdownDescription: "Size of the volume in GB",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "The timestamp for when the volume was created",
				Computed:            true,
			},
			"namespace_id": schema.StringAttribute{
				MarkdownDescription: "Namespace ID of the volume",
				Computed:            true,
			},
			"connector_id": schema.StringAttribute{
				MarkdownDescription: "Connector ID of the volume",
				Computed:            true,
			},
			"keyring": schema.StringAttribute{
				MarkdownDescription: "Cephx keyring secret used to connect to the volume. Returned only for dashboard-origin requests; null until the volume is provisioned.",
				Computed:            true,
				Sensitive:           true,
			},
			"cluster_user": schema.StringAttribute{
				MarkdownDescription: "Ceph cluster user used to connect to the volume. Returned only for dashboard-origin requests; null until the volume is provisioned.",
				Computed:            true,
			},
			"volume_path": schema.StringAttribute{
				MarkdownDescription: "Path of the volume inside the cluster. Returned only for dashboard-origin requests; null until the volume is provisioned.",
				Computed:            true,
			},
			"initiators": schema.ListNestedAttribute{
				MarkdownDescription: "NVMe initiators (clients) currently allowed to mount the volume.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"nqn": schema.StringAttribute{
							MarkdownDescription: "NVMe Qualified Name (NQN) of the mounted client",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *BlockStorageDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data BlockStorageDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	if data.ID.IsUnknown() || data.Name.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unknown selector value",
			"One of 'id' or 'name' is unknown. Please provide a concrete value.",
		)
		return
	}

	var vol *components.VolumeData
	var err error

	switch {
	case !data.ID.IsNull():
		vol, err = d.getByID(ctx, data.ID.ValueString())
	case !data.Name.IsNull():
		project := ""
		if !data.Project.IsNull() && !data.Project.IsUnknown() && data.Project.ValueString() != "" {
			project = data.Project.ValueString()
		} else if d.defaultProject != "" {
			project = d.defaultProject
		}
		if project == "" {
			resp.Diagnostics.AddError("Missing project",
				"Set `project` on this data source or define a default in the provider block (provider `latitudesh` { project = \"...\" }) when looking up by `name`.")
			return
		}
		data.Project = types.StringValue(project)
		vol, err = d.findByName(ctx, project, data.Name.ValueString())
	default:
		resp.Diagnostics.AddError(
			"Missing selector",
			"Exactly one of 'id' or 'name' must be provided.",
		)
		return
	}

	if err != nil {
		resp.Diagnostics.AddError("Client Error", err.Error())
		return
	}
	if vol == nil {
		resp.Diagnostics.AddError("Block storage volume not found", fmt.Sprintf("No volume found matching the given selector (id=%q, name=%q)", data.ID.ValueString(), data.Name.ValueString()))
		return
	}

	d.mapVolumeToModel(ctx, vol, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *BlockStorageDataSource) getByID(ctx context.Context, id string) (*components.VolumeData, error) {
	res, err := d.client.BlockStorage.GetStorageVolume(ctx, id)
	if err != nil {
		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("unable to retrieve block storage volume %q: %w", id, err)
	}
	if res.Object == nil || res.Object.Data == nil {
		return nil, nil
	}
	return res.Object.Data, nil
}

// findByName lists every volume in project and returns the first exact name
// match. GetStorageVolumes has no name filter, so this filters in memory
// (mirrors SSHKeyDataSource.findOne).
func (d *BlockStorageDataSource) findByName(ctx context.Context, project, name string) (*components.VolumeData, error) {
	res, err := d.client.BlockStorage.GetStorageVolumes(ctx, &project)
	if err != nil {
		return nil, fmt.Errorf("unable to list block storage volumes: %w", err)
	}
	if res == nil || res.Object == nil || res.Object.Data == nil {
		return nil, nil
	}

	for i := range res.Object.Data {
		v := res.Object.Data[i]
		if v.Attributes != nil && v.Attributes.Name != nil && *v.Attributes.Name == name {
			return &v, nil
		}
	}

	return nil, nil
}

func (d *BlockStorageDataSource) mapVolumeToModel(ctx context.Context, vol *components.VolumeData, data *BlockStorageDataSourceModel, diags *diag.Diagnostics) {
	if vol.ID != nil {
		data.ID = types.StringValue(*vol.ID)
	}

	a := vol.Attributes
	if a == nil {
		return
	}

	if a.Name != nil {
		data.Name = types.StringValue(*a.Name)
	}

	if a.SizeInGb != nil {
		data.SizeInGb = types.Int64Value(*a.SizeInGb)
	} else {
		data.SizeInGb = types.Int64Null()
	}

	if a.CreatedAt != nil {
		data.CreatedAt = types.StringValue(a.CreatedAt.Format(time.RFC3339))
	} else {
		data.CreatedAt = types.StringNull()
	}

	if a.NamespaceID != nil {
		data.NamespaceID = types.StringValue(*a.NamespaceID)
	} else {
		data.NamespaceID = types.StringNull()
	}

	if a.ConnectorID != nil {
		data.ConnectorID = types.StringValue(*a.ConnectorID)
	} else {
		data.ConnectorID = types.StringNull()
	}

	if a.Keyring != nil {
		data.Keyring = types.StringValue(*a.Keyring)
	} else {
		data.Keyring = types.StringNull()
	}

	if a.ClusterUser != nil {
		data.ClusterUser = types.StringValue(*a.ClusterUser)
	} else {
		data.ClusterUser = types.StringNull()
	}

	if a.VolumePath != nil {
		data.VolumePath = types.StringValue(*a.VolumePath)
	} else {
		data.VolumePath = types.StringNull()
	}

	// Never returned by GetStorageVolume/GetStorageVolumes (see VolumeDataAttributes).
	data.Region = types.StringNull()

	if (data.Project.IsNull() || data.Project.IsUnknown()) && a.Project != nil {
		if a.Project.Slug != nil {
			data.Project = types.StringValue(*a.Project.Slug)
		} else if a.Project.ID != nil {
			data.Project = types.StringValue(*a.Project.ID)
		}
	}

	initiators, initDiags := blockStorageInitiatorsValue(ctx, a.Initiators)
	diags.Append(initDiags...)
	data.Initiators = initiators
}
