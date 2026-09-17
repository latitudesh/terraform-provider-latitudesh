package latitudesh

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"

	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var (
	_ datasource.DataSource              = &BlockStoragesDataSource{}
	_ datasource.DataSourceWithConfigure = &BlockStoragesDataSource{}
)

func NewBlockStoragesDataSource() datasource.DataSource {
	return &BlockStoragesDataSource{}
}

type BlockStoragesDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type BlockStoragesDataSourceModel struct {
	// Synthetic identifier: the `project` filter, or "all".
	ID types.String `tfsdk:"id"`

	// Optional selector.
	Project types.String `tfsdk:"project"`

	// Newest first, so volumes[0] is the most recently created match.
	Volumes types.List `tfsdk:"volumes"`
}

type BlockStorageItemModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Region      types.String `tfsdk:"region"`
	SizeInGb    types.Int64  `tfsdk:"size_in_gb"`
	NamespaceID types.String `tfsdk:"namespace_id"`
	ConnectorID types.String `tfsdk:"connector_id"`
	Project     types.String `tfsdk:"project"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

var blockStorageItemObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":           types.StringType,
		"name":         types.StringType,
		"region":       types.StringType,
		"size_in_gb":   types.Int64Type,
		"namespace_id": types.StringType,
		"connector_id": types.StringType,
		"project":      types.StringType,
		"created_at":   types.StringType,
	},
}

func (d *BlockStoragesDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_block_storages"
}

func (d *BlockStoragesDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *BlockStoragesDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Block Storage volumes data source - list the team's volumes, optionally scoped to one project, newest first.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Synthetic identifier for this query: the `project` filter, or `all`.",
				Computed:            true,
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "Project (ID or slug) to scope the list to. When omitted, every volume in the team is listed.",
				Optional:            true,
			},
			"volumes": schema.ListNestedAttribute{
				MarkdownDescription: "The matching volumes, newest first.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "Volume ID.",
							Computed:            true,
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Volume name.",
							Computed:            true,
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
						"project": schema.StringAttribute{
							MarkdownDescription: "Project (slug, falling back to ID) that owns the volume.",
							Computed:            true,
						},
						"created_at": schema.StringAttribute{
							MarkdownDescription: "Timestamp when the volume was created.",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *BlockStoragesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data BlockStoragesDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	if data.Project.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unknown filter value",
			"'project' must be known at plan time. Please provide a concrete value or omit it.",
		)
		return
	}

	projectFilter := strings.TrimSpace(data.Project.ValueString())

	// GetStorageVolumes takes no page parameters and returns no pagination
	// metadata, unlike the paginated list operations elsewhere in this SDK
	// (servers, projects, firewalls, ...): one call is the whole collection
	// for the requested project scope.
	var filterProject *string
	if projectFilter != "" {
		filterProject = &projectFilter
	}

	res, err := d.client.BlockStorage.GetStorageVolumes(ctx, filterProject)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to list block storage volumes, got error: "+err.Error())
		return
	}

	id := "all"
	if projectFilter != "" {
		id = projectFilter
	}
	data.ID = types.StringValue(id)

	volumes := make([]components.VolumeData, 0)
	if res != nil && res.Object != nil {
		volumes = append(volumes, res.Object.Data...)
	}
	sortBlockStorageVolumesNewestFirst(volumes)

	items := make([]BlockStorageItemModel, 0, len(volumes))
	for i := range volumes {
		items = append(items, blockStorageItemValue(&volumes[i]))
	}

	list, diags := types.ListValueFrom(ctx, blockStorageItemObjectType, items)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Volumes = list

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// blockStorageItemValue maps one SDK volume into the list item model. It
// shares the attribute mapper with the resource and singular data source,
// adding the owning project's label and creation timestamp on top.
func blockStorageItemValue(v *components.VolumeData) BlockStorageItemModel {
	model := BlockStorageResourceModel{}
	mapBlockStorageVolume(&model, v)

	item := BlockStorageItemModel{
		ID:          model.ID,
		Name:        model.Name,
		Region:      model.Region,
		SizeInGb:    model.SizeInGb,
		NamespaceID: model.NamespaceID,
		ConnectorID: model.ConnectorID,
		Project:     types.StringNull(),
		CreatedAt:   types.StringNull(),
	}
	if v.Attributes == nil {
		return item
	}
	if label := projectLabel(v.Attributes.Project); label != "" {
		item.Project = types.StringValue(label)
	}
	if v.Attributes.CreatedAt != nil {
		item.CreatedAt = types.StringValue(v.Attributes.CreatedAt.Format(time.RFC3339))
	}
	return item
}

// sortBlockStorageVolumesNewestFirst orders volumes by created_at descending.
// Entries without a timestamp sort last; the sort is stable so the API's own
// order is kept among them.
func sortBlockStorageVolumesNewestFirst(volumes []components.VolumeData) {
	sort.SliceStable(volumes, func(i, j int) bool {
		return volumeCreatedAt(volumes[i]).After(volumeCreatedAt(volumes[j]))
	})
}

func volumeCreatedAt(v components.VolumeData) time.Time {
	if v.Attributes == nil || v.Attributes.CreatedAt == nil {
		return time.Time{}
	}
	return *v.Attributes.CreatedAt
}
