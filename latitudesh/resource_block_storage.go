package latitudesh

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

// blockStorageInitiatorObjectType mirrors BlockStorageInitiatorModel and is used
// to build the initiators list value.
var blockStorageInitiatorObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"nqn": types.StringType,
	},
}

// BlockStorageInitiatorModel is one NVMe initiator allowed to mount the volume
// (populated by a separate mount lifecycle, PostStorageVolumesMount, which this
// resource does not manage; see sdk-coverage.yaml).
type BlockStorageInitiatorModel struct {
	Nqn types.String `tfsdk:"nqn"`
}

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &BlockStorageResource{}
var _ resource.ResourceWithImportState = &BlockStorageResource{}

func NewBlockStorageResource() resource.Resource {
	return &BlockStorageResource{}
}

// BlockStorageResource defines the resource implementation.
type BlockStorageResource struct {
	client         *latitudeshgosdk.Latitudesh
	defaultProject string
}

// BlockStorageResourceModel describes the resource data model.
type BlockStorageResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Project     types.String `tfsdk:"project"`
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

func (r *BlockStorageResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_block_storage"
}

func (r *BlockStorageResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Block Storage resource. Adds a persistent storage volume to a project on [Latitude.sh](https://latitude.sh/). The API has no update endpoint for volumes, so any attribute change replaces the resource.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Block storage volume identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The volume name. Changing this forces a new resource (the API has no update endpoint).",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "The project (ID or slug) to create the volume in. Optional here only if `project` is set on the provider block; one of the two is required. Changing it forces a new resource.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "The region (site) slug where the volume is provisioned (e.g. SAO, ASH). Changing this forces a new resource. Not returned by the read API: it is not refreshed from state and must be kept in configuration, including after import (see the Import section).",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"size_in_gb": schema.Int64Attribute{
				MarkdownDescription: "Size of the volume in GB. Defaults to 1500 if not set. Changing this forces a new resource.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
					int64planmodifier.UseStateForUnknown(),
				},
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
				MarkdownDescription: "NVMe initiators (clients) currently allowed to mount the volume. This resource does not manage mounts (see `PostStorageVolumesMount` in `sdk-coverage.yaml`); this reflects whatever is mounted out-of-band.",
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

func (r *BlockStorageResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.client = deps.Client
	r.defaultProject = deps.DefaultProject
}

func (r *BlockStorageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data BlockStorageResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	project := ""
	if !data.Project.IsNull() && !data.Project.IsUnknown() && data.Project.ValueString() != "" {
		project = data.Project.ValueString()
	} else if r.defaultProject != "" {
		project = r.defaultProject
	}
	if project == "" {
		resp.Diagnostics.AddError("Missing project",
			"Set `project` on this resource or define a default in the provider block (provider `latitudesh` { project = \"...\" }).")
		return
	}

	attrs := operations.PostStorageVolumesBlockStorageAttributes{
		Project: project,
		Name:    data.Name.ValueString(),
		Region:  data.Region.ValueString(),
	}
	if !data.SizeInGb.IsNull() && !data.SizeInGb.IsUnknown() {
		size := data.SizeInGb.ValueInt64()
		attrs.SizeInGb = &size
	}

	createRequest := operations.PostStorageVolumesBlockStorageRequestBody{
		Data: operations.PostStorageVolumesBlockStorageData{
			Type:       operations.PostStorageVolumesBlockStorageTypeVolumes,
			Attributes: attrs,
		},
	}

	result, err := r.client.BlockStorage.PostStorageVolumes(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create block storage volume, got error: "+err.Error())
		return
	}

	if result.Object == nil || result.Object.Data == nil || result.Object.Data.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get block storage volume ID from response")
		return
	}

	data.ID = types.StringValue(*result.Object.Data.ID)
	data.Project = types.StringValue(project)

	r.readBlockStorageInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BlockStorageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data BlockStorageResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readBlockStorageInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.IsNull() {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update is never invoked in practice: every configurable attribute carries
// RequiresReplace because the API has no volume update endpoint (see
// sdk-coverage.yaml). The method still needs to exist to satisfy
// resource.Resource.
func (r *BlockStorageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update Not Supported", "Block storage volumes cannot be updated in place; they must be replaced.")
}

func (r *BlockStorageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data BlockStorageResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.ID.ValueString()

	_, err := r.client.BlockStorage.DeleteStorageVolumes(ctx, id)
	if err != nil {
		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to delete block storage volume, got error: "+err.Error())
		return
	}
}

func (r *BlockStorageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// readBlockStorageInto fetches the volume and maps it onto data. A 404 nulls
// the ID and lets the caller decide whether to RemoveResource. `region` is
// deliberately left untouched: GetStorageVolume's response has no region field
// (see VolumeDataAttributes), so it cannot be refreshed from the API and stays
// whatever the caller (state or plan) already had.
func (r *BlockStorageResource) readBlockStorageInto(ctx context.Context, data *BlockStorageResourceModel, diags *diag.Diagnostics) {
	id := data.ID.ValueString()

	result, err := r.client.BlockStorage.GetStorageVolume(ctx, id)
	if err != nil {
		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			data.ID = types.StringNull()
			return
		}
		diags.AddError("Client Error", "Unable to read block storage volume, got error: "+err.Error())
		return
	}

	if result.Object == nil || result.Object.Data == nil {
		data.ID = types.StringNull()
		return
	}

	vol := result.Object.Data
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

// blockStorageInitiatorsValue maps a volume's initiators into a Terraform list
// value. It always returns a known (possibly empty) list, never null.
func blockStorageInitiatorsValue(ctx context.Context, initiators []components.Initiators) (types.List, diag.Diagnostics) {
	models := make([]BlockStorageInitiatorModel, 0, len(initiators))
	for _, i := range initiators {
		m := BlockStorageInitiatorModel{Nqn: types.StringNull()}
		if i.Nqn != nil {
			m.Nqn = types.StringValue(*i.Nqn)
		}
		models = append(models, m)
	}
	return types.ListValueFrom(ctx, blockStorageInitiatorObjectType, models)
}
