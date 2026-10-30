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
	_ datasource.DataSource              = &FilesystemStorageDataSource{}
	_ datasource.DataSourceWithConfigure = &FilesystemStorageDataSource{}
)

func NewFilesystemStorageDataSource() datasource.DataSource {
	return &FilesystemStorageDataSource{}
}

type FilesystemStorageDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type FilesystemStorageDataSourceModel struct {
	// Selectors (exactly one)
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`

	// Optional filter, only used to disambiguate a name lookup.
	Project types.String `tfsdk:"project"`

	// Attributes
	Region       types.String `tfsdk:"region"`
	SizeInGb     types.Int64  `tfsdk:"size_in_gb"`
	Protocols    types.Set    `tfsdk:"protocols"`
	StorageClass types.String `tfsdk:"storage_class"`
	NfsMountPath types.String `tfsdk:"nfs_mount_path"`
	FileEndpoint types.String `tfsdk:"file_endpoint"`
}

func (d *FilesystemStorageDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_filesystem_storage"
}

func (d *FilesystemStorageDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *FilesystemStorageDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Filesystem Storage data source - lookup a filesystem by id, or by name (optionally scoped to a project). The API has no single-item read, so every lookup scans `ListFilesystems`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Filesystem identifier to look up.",
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
				MarkdownDescription: "Filesystem name to look up. Names are not guaranteed unique; the first match wins, so narrow the search with `project` if the lookup is ambiguous.",
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
				MarkdownDescription: "Project (ID or slug) to narrow a `name` lookup to. Ignored when `id` is set: an `id` lookup always reports the owning project the API returns, whatever was configured here.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Site slug where the filesystem is provisioned.",
				Computed:            true,
			},
			"size_in_gb": schema.Int64Attribute{
				MarkdownDescription: "Filesystem size, in GB.",
				Computed:            true,
			},
			"protocols": schema.SetAttribute{
				MarkdownDescription: "NFS protocol version(s) the filesystem was requested to be mounted with.",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"storage_class": schema.StringAttribute{
				MarkdownDescription: "Backend storage tier the API assigned based on `region` (`standard` or `high_performance`).",
				Computed:            true,
			},
			"nfs_mount_path": schema.StringAttribute{
				MarkdownDescription: "Path of the NFS view backing the filesystem. Null for filesystems not backed by high performance file storage.",
				Computed:            true,
			},
			"file_endpoint": schema.StringAttribute{
				MarkdownDescription: "Hostname of the NFS endpoint backing the filesystem. Null for filesystems not backed by high performance file storage.",
				Computed:            true,
			},
		},
	}
}

func (d *FilesystemStorageDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data FilesystemStorageDataSourceModel

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

	var fs *components.FilesystemData
	var err error

	switch {
	case !data.ID.IsNull():
		fs, err = d.findByID(ctx, data.ID.ValueString())
	case !data.Name.IsNull():
		fs, err = d.findByName(ctx, data.Name.ValueString(), strings.TrimSpace(data.Project.ValueString()))
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
	if fs == nil {
		resp.Diagnostics.AddError("Filesystem storage not found", fmt.Sprintf("No filesystem storage matches selector %q", selectorDescriptionFilesystemStorage(data)))
		return
	}

	model := FilesystemStorageResourceModel{
		ID:   data.ID,
		Name: data.Name,
	}
	// mapFilesystemStorage only backfills `project` when the model has
	// nothing, which is what the resource needs (a configured slug must not
	// read as drift on an attribute that forces replacement). A data source
	// has no such constraint and must report what the API owns, so the
	// configured value is only carried over on the `name` branch, where it
	// scoped the lookup and therefore already matches.
	if data.ID.IsNull() {
		model.Project = data.Project
	}
	resp.Diagnostics.Append(mapFilesystemStorage(ctx, &model, fs)...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.ID = model.ID
	data.Name = model.Name
	data.Project = model.Project
	data.Region = model.Region
	data.SizeInGb = model.SizeInGb
	data.Protocols = model.Protocols
	data.StorageClass = model.StorageClass
	data.NfsMountPath = model.NfsMountPath
	data.FileEndpoint = model.FileEndpoint

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func selectorDescriptionFilesystemStorage(data FilesystemStorageDataSourceModel) string {
	if !data.ID.IsNull() {
		return "id=" + data.ID.ValueString()
	}
	return "name=" + data.Name.ValueString()
}

// findByID lists every filesystem in the team (there is no single-item GET for
// this group) and returns the one matching id.
func (d *FilesystemStorageDataSource) findByID(ctx context.Context, id string) (*components.FilesystemData, error) {
	res, err := d.client.FilesystemStorage.ListFilesystems(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to list filesystem storage: %w", err)
	}
	if res == nil || res.Filesystems == nil {
		return nil, nil
	}

	for i := range res.Filesystems.Data {
		fs := res.Filesystems.Data[i]
		if fs.ID != nil && *fs.ID == id {
			return &fs, nil
		}
	}

	return nil, nil
}

// findByName lists filesystems (optionally scoped to project, using the SDK's
// FilterProject query parameter) and filters in memory by name: the API has
// no filter-by-name query parameter.
func (d *FilesystemStorageDataSource) findByName(ctx context.Context, name, project string) (*components.FilesystemData, error) {
	var filterProject *string
	if project != "" {
		filterProject = &project
	}

	res, err := d.client.FilesystemStorage.ListFilesystems(ctx, filterProject)
	if err != nil {
		return nil, fmt.Errorf("unable to list filesystem storage: %w", err)
	}
	if res == nil || res.Filesystems == nil || len(res.Filesystems.Data) == 0 {
		return nil, nil
	}

	nameQ := strings.TrimSpace(name)
	for i := range res.Filesystems.Data {
		fs := res.Filesystems.Data[i]
		if fs.Attributes == nil || fs.Attributes.Name == nil {
			continue
		}
		if strings.TrimSpace(*fs.Attributes.Name) == nameQ {
			return &fs, nil
		}
	}

	return nil, nil
}
