package latitudesh

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
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
	client *latitudeshgosdk.Latitudesh
}

type BlockStorageDataSourceModel struct {
	// Selectors (exactly one)
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`

	// Optional filter, only used to disambiguate a name lookup.
	Project types.String `tfsdk:"project"`

	// Attributes
	Region      types.String `tfsdk:"region"`
	SizeInGb    types.Int64  `tfsdk:"size_in_gb"`
	NamespaceID types.String `tfsdk:"namespace_id"`
	ConnectorID types.String `tfsdk:"connector_id"`
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
}

func (d *BlockStorageDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Block Storage volume data source - lookup a volume by id, or by name (optionally scoped to a project).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Volume identifier to look up.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("id"),
						path.MatchRoot("name"),
					),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Volume name to look up.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("id"),
						path.MatchRoot("name"),
					),
				},
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "Project (ID or slug) to narrow a `name` lookup to (ignored when `id` is set). Also returned as the volume's owning project when looking up by `id`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Site slug where the volume is provisioned.",
				Computed:            true,
			},
			"size_in_gb": schema.Int64Attribute{
				MarkdownDescription: "Volume size, in GB.",
				Computed:            true,
			},
			"namespace_id": schema.StringAttribute{
				MarkdownDescription: "NVMe namespace identifier assigned to the volume.",
				Computed:            true,
			},
			"connector_id": schema.StringAttribute{
				MarkdownDescription: "Connector identifier used to reach the volume over NVMe-TCP.",
				Computed:            true,
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

	if data.ID.IsUnknown() || data.Name.IsUnknown() || data.Project.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unknown selector value",
			"One of 'id' or 'name' (and 'project', if set) is unknown. Please provide a concrete value.",
		)
		return
	}

	var volume *components.VolumeData
	var err error

	switch {
	case !data.ID.IsNull():
		volume, err = d.getByID(ctx, data.ID.ValueString())
	case !data.Name.IsNull():
		volume, err = d.findByName(ctx, data.Name.ValueString(), strings.TrimSpace(data.Project.ValueString()))
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
	if volume == nil {
		resp.Diagnostics.AddError("Block storage volume not found", fmt.Sprintf("No block storage volume matches selector %q", selectorDescription(data)))
		return
	}

	model := BlockStorageResourceModel{
		ID:      data.ID,
		Project: data.Project,
		Name:    data.Name,
	}
	mapBlockStorageVolume(&model, volume)

	data.ID = model.ID
	data.Name = model.Name
	data.Project = model.Project
	data.Region = model.Region
	data.SizeInGb = model.SizeInGb
	data.NamespaceID = model.NamespaceID
	data.ConnectorID = model.ConnectorID

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func selectorDescription(data BlockStorageDataSourceModel) string {
	if !data.ID.IsNull() {
		return "id=" + data.ID.ValueString()
	}
	return "name=" + data.Name.ValueString()
}

func (d *BlockStorageDataSource) getByID(ctx context.Context, id string) (*components.VolumeData, error) {
	res, err := d.client.BlockStorage.GetStorageVolume(ctx, id)
	if err != nil {
		if blockStorageNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("unable to retrieve block storage volume %q: %w", id, err)
	}
	if res.Object == nil || res.Object.Data == nil {
		return nil, nil
	}
	return res.Object.Data, nil
}

// findByName lists volumes (optionally scoped to project, using the SDK's
// FilterProject query parameter) and filters in memory by name: the API has
// no filter-by-name query parameter.
func (d *BlockStorageDataSource) findByName(ctx context.Context, name, project string) (*components.VolumeData, error) {
	var filterProject *string
	if project != "" {
		filterProject = &project
	}

	res, err := d.client.BlockStorage.GetStorageVolumes(ctx, filterProject)
	if err != nil {
		return nil, fmt.Errorf("unable to list block storage volumes: %w", err)
	}
	if res == nil || res.Object == nil || len(res.Object.Data) == 0 {
		return nil, nil
	}

	nameQ := strings.TrimSpace(name)
	for i := range res.Object.Data {
		v := res.Object.Data[i]
		if v.Attributes == nil || v.Attributes.Name == nil {
			continue
		}
		if strings.TrimSpace(*v.Attributes.Name) == nameQ {
			return &v, nil
		}
	}

	return nil, nil
}
