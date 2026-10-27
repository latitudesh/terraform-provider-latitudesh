package latitudesh

import (
	"context"
	"errors"
	"net/http"

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

var _ resource.Resource = &BlockStorageResource{}
var _ resource.ResourceWithImportState = &BlockStorageResource{}

// BlockStorageResource manages a block storage volume. The SDK exposes no
// update endpoint for volumes, so every attribute that affects the volume
// forces a replacement. Mapping a volume to a server (MapVolume/UnmapVolume)
// is a separate lifecycle owned by latitudesh_block_storage_map; MountVolume
// is the deprecated allowed-list flow and is not modeled - see
// sdk-coverage.yaml.
type BlockStorageResource struct {
	client         *latitudeshgosdk.Latitudesh
	defaultProject string
}

type BlockStorageResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Project     types.String `tfsdk:"project"`
	Name        types.String `tfsdk:"name"`
	Region      types.String `tfsdk:"region"`
	SizeInGb    types.Int64  `tfsdk:"size_in_gb"`
	NamespaceID types.Int64  `tfsdk:"namespace_id"`
	ConnectorID types.String `tfsdk:"connector_id"`
}

func NewBlockStorageResource() resource.Resource {
	return &BlockStorageResource{}
}

func (r *BlockStorageResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_block_storage"
}

func (r *BlockStorageResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Block Storage volume resource. Creates and manages a persistent NVMe-TCP volume on [Latitude.sh](https://latitude.sh/). There is no update endpoint for volumes, so changing any attribute forces a new resource. Attaching the volume to a server is managed by `latitudesh_block_storage_attachment`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Block storage volume identifier.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
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
			"name": schema.StringAttribute{
				MarkdownDescription: "Volume name. Changing this forces a new resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Site slug where the volume is provisioned. Changing this forces a new resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"size_in_gb": schema.Int64Attribute{
				MarkdownDescription: "Volume size, in GB. Changing this forces a new resource.",
				Required:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"namespace_id": schema.Int64Attribute{
				MarkdownDescription: "NVMe namespace identifier assigned to the volume. Only volumes attached through the deprecated mount flow carry one; it is null otherwise.",
				Computed:            true,
			},
			"connector_id": schema.StringAttribute{
				MarkdownDescription: "Connector identifier used to reach the volume over NVMe-TCP.",
				Computed:            true,
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

	createRequest := operations.CreateVolumeBlockStorageRequestBody{
		Data: operations.CreateVolumeBlockStorageData{
			Type: operations.CreateVolumeBlockStorageTypeVolumes,
			Attributes: operations.CreateVolumeBlockStorageAttributes{
				Project:  project,
				Name:     data.Name.ValueString(),
				Region:   data.Region.ValueString(),
				SizeInGb: data.SizeInGb.ValueInt64(),
			},
		},
	}

	result, err := r.client.BlockStorage.CreateVolume(ctx, createRequest)
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

	// The create response already carries the whole volume, so map it first:
	// a read-back that has not caught up yet must not be able to drop what we
	// already know about a volume that exists and is billing.
	mapBlockStorageVolume(&data, result.Object.Data)

	// Refresh from GET so anything the API fills in after the create lands in
	// state. A 404 here only means the volume is not readable yet — keeping
	// the create response is what stops that from writing a null id (which
	// Terraform rejects as an inconsistent result, leaving the volume out of
	// state entirely and leaking it).
	refreshed := data
	found := r.readBlockStorageInto(ctx, &refreshed, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if found {
		data = refreshed
	} else {
		// Purely-computed attributes stay unknown if the create response had
		// no `attributes` at all, and unknowns cannot be written to state.
		if data.NamespaceID.IsUnknown() {
			data.NamespaceID = types.Int64Null()
		}
		if data.ConnectorID.IsUnknown() {
			data.ConnectorID = types.StringNull()
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BlockStorageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data BlockStorageResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found := r.readBlockStorageInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update is unreachable in practice: every attribute that participates in the
// plan is RequiresReplace because the SDK exposes no update call for volumes.
// It only exists to satisfy resource.Resource; it re-reads rather than
// sending any write.
func (r *BlockStorageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data BlockStorageResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found := r.readBlockStorageInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError("Block storage volume not found",
			"The volume no longer exists. Remove it from state (or refresh) and re-apply to recreate it.")
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BlockStorageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data BlockStorageResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.ID.ValueString()

	_, err := r.client.BlockStorage.DeleteVolume(ctx, id)
	if err != nil {
		if blockStorageNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to delete block storage volume, got error: "+err.Error())
		return
	}
}

func (r *BlockStorageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// blockStorageNotFound reports whether err is a 404 from the BlockStorage
// group. RetrieveVolume and DeleteVolume only ever return the
// generic *components.APIError (no typed *components.ErrorObject branch is
// generated for either), unlike groups such as ElasticIps.
func blockStorageNotFound(err error) bool {
	var apiErr *components.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return false
}

// readBlockStorageInto maps a fresh read of the volume into data, reporting
// whether the volume was found. A miss (404 or an empty body) nulls the id so
// callers that write data straight to state cannot keep a dangling resource,
// but the boolean is what callers should branch on.
func (r *BlockStorageResource) readBlockStorageInto(ctx context.Context, data *BlockStorageResourceModel, diags *diag.Diagnostics) bool {
	id := data.ID.ValueString()

	result, err := r.client.BlockStorage.RetrieveVolume(ctx, id)
	if err != nil {
		if blockStorageNotFound(err) {
			data.ID = types.StringNull()
			return false
		}
		diags.AddError("Client Error", "Unable to read block storage volume, got error: "+err.Error())
		return false
	}

	if result.Object == nil || result.Object.Data == nil {
		data.ID = types.StringNull()
		return false
	}

	mapBlockStorageVolume(data, result.Object.Data)
	return true
}

// mapBlockStorageVolume maps one components.VolumeData into the resource
// model, shared by Create/Read/Update and the singular data source.
func mapBlockStorageVolume(data *BlockStorageResourceModel, volume *components.VolumeData) {
	if volume.ID != nil {
		data.ID = types.StringValue(*volume.ID)
	}

	a := volume.Attributes
	if a == nil {
		return
	}

	// name and size_in_gb are Required with no slug/ID ambiguity: the API
	// always echoes back exactly what was configured, so they are
	// unconditionally overwritten on every read.
	if a.Name != nil {
		data.Name = types.StringValue(*a.Name)
	}
	if a.SizeInGb != nil {
		data.SizeInGb = types.Int64Value(*a.SizeInGb)
	}

	// Purely-computed fields (known only after apply): nil from the API is set
	// to null rather than left stale.
	if a.NamespaceID != nil {
		data.NamespaceID = types.Int64Value(*a.NamespaceID)
	} else {
		data.NamespaceID = types.Int64Null()
	}
	if a.ConnectorID != nil {
		data.ConnectorID = types.StringValue(*a.ConnectorID)
	} else {
		data.ConnectorID = types.StringNull()
	}

	// project and region accept either a slug or an ID going in, but the API
	// echoes back its own canonical form; only backfill when state has
	// nothing (import), or a config-provided slug would read as drift on an
	// attribute that forces replacement.
	if (data.Project.IsNull() || data.Project.IsUnknown()) && a.Project != nil {
		if label := projectLabel(a.Project); label != "" {
			data.Project = types.StringValue(label)
		}
	}
	if (data.Region.IsNull() || data.Region.IsUnknown()) && a.Region != nil && a.Region.Site != nil && a.Region.Site.Slug != nil {
		data.Region = types.StringValue(*a.Region.Site.Slug)
	}
}
