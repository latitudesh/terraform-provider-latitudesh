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
// forces a replacement. Mapping a volume to a server (PostStorageVolumesMap)
// and mounting it (PostStorageVolumesMount) are separate lifecycle verbs not
// modeled by this resource - see sdk-coverage.yaml.
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
	NamespaceID types.String `tfsdk:"namespace_id"`
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
		MarkdownDescription: "Block Storage volume resource. Creates and manages a persistent NVMe-TCP volume on [Latitude.sh](https://latitude.sh/). There is no update endpoint for volumes, so changing any attribute forces a new resource. Mapping the volume to a server or mounting it are not managed here.",
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

	createRequest := operations.PostStorageVolumesBlockStorageRequestBody{
		Data: operations.PostStorageVolumesBlockStorageData{
			Type: operations.PostStorageVolumesBlockStorageTypeVolumes,
			Attributes: operations.PostStorageVolumesBlockStorageAttributes{
				Project:  project,
				Name:     data.Name.ValueString(),
				Region:   data.Region.ValueString(),
				SizeInGb: data.SizeInGb.ValueInt64(),
			},
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

	r.readBlockStorageInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
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

	_, err := r.client.BlockStorage.DeleteStorageVolumes(ctx, id)
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
// group. GetStorageVolume and DeleteStorageVolumes only ever return the
// generic *components.APIError (no typed *components.ErrorObject branch is
// generated for either), unlike groups such as ElasticIps.
func blockStorageNotFound(err error) bool {
	var apiErr *components.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return false
}

func (r *BlockStorageResource) readBlockStorageInto(ctx context.Context, data *BlockStorageResourceModel, diags *diag.Diagnostics) {
	id := data.ID.ValueString()

	result, err := r.client.BlockStorage.GetStorageVolume(ctx, id)
	if err != nil {
		if blockStorageNotFound(err) {
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

	mapBlockStorageVolume(data, result.Object.Data)
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
		data.NamespaceID = types.StringValue(*a.NamespaceID)
	} else {
		data.NamespaceID = types.StringNull()
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
