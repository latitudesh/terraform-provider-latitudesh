package latitudesh

import (
	"context"
	"errors"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var _ resource.Resource = &FilesystemStorageResource{}
var _ resource.ResourceWithImportState = &FilesystemStorageResource{}

// FilesystemStorageResource manages a filesystem storage volume. The API has
// no single-item read (confirmed against the SDK — FilesystemStorage carries
// no Get/Retrieve method), so Read and import locate the filesystem by
// scanning ListFilesystems. UpdateFilesystem only ever grows size_in_gb ("Allow
// you to upgrade the size of a filesystem" per the SDK doc comment, the same
// grow-only wording BlockStorage's UpdateVolume uses); every other attribute
// has no update endpoint and forces a replacement. AttachFilesystem/
// DetachFilesystem (mounting the filesystem onto a server) are a separate
// lifecycle, the same shape as BlockStorage's MapVolume/UnmapVolume, and are
// deliberately not modeled here — see sdk-coverage.yaml.
type FilesystemStorageResource struct {
	client         *latitudeshgosdk.Latitudesh
	defaultProject string
}

type FilesystemStorageResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Project      types.String `tfsdk:"project"`
	Name         types.String `tfsdk:"name"`
	Region       types.String `tfsdk:"region"`
	SizeInGb     types.Int64  `tfsdk:"size_in_gb"`
	Protocols    types.Set    `tfsdk:"protocols"`
	StorageClass types.String `tfsdk:"storage_class"`
	NfsMountPath types.String `tfsdk:"nfs_mount_path"`
	FileEndpoint types.String `tfsdk:"file_endpoint"`
}

func NewFilesystemStorageResource() resource.Resource {
	return &FilesystemStorageResource{}
}

func (r *FilesystemStorageResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_filesystem_storage"
}

func (r *FilesystemStorageResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Filesystem Storage resource. Creates and manages a persistent NFS filesystem on [Latitude.sh](https://latitude.sh/) that can be mounted by multiple servers. There is no update endpoint for `name`, `region` or `protocols`, so changing any of them forces a new resource; `size_in_gb` can only grow in place, so a smaller value also forces a new resource. Access to filesystem storage is gated per team — creating this resource requires a team with it enabled.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Filesystem storage identifier.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "The project (ID or slug) to create the filesystem in. Optional here only if `project` is set on the provider block; one of the two is required. Changing it forces a new resource.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Filesystem name. `block` is reserved and cannot be used. Changing this forces a new resource.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.NoneOfCaseInsensitive("block"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Site slug where the filesystem is provisioned. Required for high performance file storage; an unknown slug is rejected by the API. Changing this forces a new resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"size_in_gb": schema.Int64Attribute{
				MarkdownDescription: "Filesystem size, in GB. Defaults to `1500`. Increasing it resizes the filesystem in place; decreasing it forces a new resource.",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(1500),
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplaceIf(
						filesystemStorageShrinkRequiresReplace,
						"Filesystems can only grow, so a smaller size forces a new resource.",
						"Filesystems can only grow, so a smaller `size_in_gb` forces a new resource.",
					),
				},
			},
			"protocols": schema.SetAttribute{
				MarkdownDescription: "NFS protocol version(s) to mount the filesystem with (`nfs3`, `nfs4`, case-insensitive). Pass both to allow either protocol. There is no update endpoint, so changing this forces a new resource.",
				Required:            true,
				ElementType:         types.StringType,
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(
						stringvalidator.OneOfCaseInsensitive("nfs3", "nfs4"),
					),
				},
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.RequiresReplace(),
				},
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

func (r *FilesystemStorageResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *FilesystemStorageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data FilesystemStorageResourceModel

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

	var protocolStrings []string
	resp.Diagnostics.Append(data.Protocols.ElementsAs(ctx, &protocolStrings, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	protocols := make([]operations.CreateFilesystemProtocols, 0, len(protocolStrings))
	for _, p := range protocolStrings {
		protocols = append(protocols, operations.CreateFilesystemProtocols(p))
	}

	sizeInGb := data.SizeInGb.ValueInt64()

	createRequest := operations.CreateFilesystemFilesystemStorageRequestBody{
		Data: operations.CreateFilesystemFilesystemStorageData{
			Type: operations.CreateFilesystemFilesystemStorageTypeFilesystems,
			Attributes: operations.CreateFilesystemFilesystemStorageAttributes{
				Project:   project,
				Name:      data.Name.ValueString(),
				Region:    data.Region.ValueString(),
				SizeInGb:  &sizeInGb,
				Protocols: protocols,
			},
		},
	}

	result, err := r.client.FilesystemStorage.CreateFilesystem(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create filesystem storage, got error: "+err.Error())
		return
	}

	if result == nil || result.Object == nil || result.Object.Data == nil || result.Object.Data.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get filesystem storage ID from response")
		return
	}

	id := *result.Object.Data.ID
	data.ID = types.StringValue(id)
	data.Project = types.StringValue(project)

	// The create response already carries the whole filesystem, so map it
	// first: a read-back that has not caught up yet must not be able to drop
	// what we already know about a filesystem that exists and is billing.
	resp.Diagnostics.Append(mapFilesystemStorage(ctx, &data, result.Object.Data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Persist the ID before the reconciling list, so a failure there leaves a
	// tainted resource in state rather than an orphaned, billed filesystem.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// There is no single-item GET to reconcile with, only the list. A miss here
	// only means the filesystem has not shown up in the list yet, so the create
	// response is kept rather than dropping a filesystem that exists and bills.
	refreshed := data
	var refreshDiags diag.Diagnostics
	found := r.readFilesystemStorageInto(ctx, &refreshed, &refreshDiags)
	if refreshDiags.HasError() {
		resp.Diagnostics.AddWarning("Filesystem storage created, but reading it back failed",
			"The filesystem was created and saved from the create response; the next refresh will read it again. Read error: "+
				refreshDiags.Errors()[0].Detail())
		found = false
	}
	if found {
		data = refreshed
	} else {
		if data.StorageClass.IsUnknown() {
			data.StorageClass = types.StringNull()
		}
		if data.NfsMountPath.IsUnknown() {
			data.NfsMountPath = types.StringNull()
		}
		if data.FileEndpoint.IsUnknown() {
			data.FileEndpoint = types.StringNull()
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FilesystemStorageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data FilesystemStorageResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found := r.readFilesystemStorageInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update only ever has to grow the filesystem: size_in_gb is the one input
// that does not force a replacement, and only when it increases (enforced by
// the RequiresReplaceIf plan modifier on the schema).
func (r *FilesystemStorageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, state FilesystemStorageResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !data.SizeInGb.Equal(state.SizeInGb) {
		_, err := r.client.FilesystemStorage.UpdateFilesystem(ctx, state.ID.ValueString(), operations.UpdateFilesystemFilesystemStorageRequestBody{
			Data: operations.UpdateFilesystemFilesystemStorageData{
				Type: operations.UpdateFilesystemFilesystemStorageTypeFilesystems,
				Attributes: operations.UpdateFilesystemFilesystemStorageAttributes{
					SizeInGb: data.SizeInGb.ValueInt64(),
				},
			},
		})
		if err != nil {
			resp.Diagnostics.AddError("Client Error", "Unable to resize filesystem storage, got error: "+err.Error())
			return
		}
	}

	found := r.readFilesystemStorageInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError("Filesystem storage not found",
			"The filesystem no longer exists. Remove it from state (or refresh) and re-apply to recreate it.")
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FilesystemStorageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data FilesystemStorageResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.ID.ValueString()
	if id == "" {
		return
	}

	_, err := r.client.FilesystemStorage.DeleteFilesystem(ctx, id)
	if err != nil {
		if filesystemStorageNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to delete filesystem storage, got error: "+err.Error())
		return
	}
}

// ImportState takes the filesystem's id. There is no single-item GET, so the
// reconciling read scans the whole team's ListFilesystems for a match; an
// unscoped project filter is used because the project is not known yet.
func (r *FilesystemStorageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var data FilesystemStorageResourceModel
	data.ID = types.StringValue(req.ID)

	found := r.readFilesystemStorageInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if !found {
		resp.Diagnostics.AddError("Not Found", "Filesystem storage "+req.ID+" not found")
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// readFilesystemStorageInto lists filesystems (scoped to `project` when
// already known) and maps the one matching data.ID, reporting whether it was
// found. ListFilesystems has no page parameters and FilesystemsMeta carries no
// pagination fields, so a single call is the whole collection.
func (r *FilesystemStorageResource) readFilesystemStorageInto(ctx context.Context, data *FilesystemStorageResourceModel, diags *diag.Diagnostics) bool {
	id := data.ID.ValueString()
	if id == "" {
		diags.AddError("Invalid ID", "Filesystem storage ID is empty")
		return false
	}

	var filterProject *string
	if !data.Project.IsNull() && !data.Project.IsUnknown() && data.Project.ValueString() != "" {
		p := data.Project.ValueString()
		filterProject = &p
	}

	result, err := r.client.FilesystemStorage.ListFilesystems(ctx, filterProject)
	if err != nil {
		if filesystemStorageNotFound(err) {
			data.ID = types.StringNull()
			return false
		}
		diags.AddError("Client Error", "Unable to list filesystem storage, got error: "+err.Error())
		return false
	}

	if result == nil || result.Filesystems == nil {
		data.ID = types.StringNull()
		return false
	}

	for i := range result.Filesystems.Data {
		fs := result.Filesystems.Data[i]
		if fs.ID != nil && *fs.ID == id {
			diags.Append(mapFilesystemStorage(ctx, data, &fs)...)
			return true
		}
	}

	data.ID = types.StringNull()
	return false
}

// filesystemStorageShrinkRequiresReplace replaces the filesystem only when
// size_in_gb decreases, since UpdateFilesystem can only grow it ("Allow you to
// upgrade the size of a filesystem" per the SDK doc comment). An unknown size
// is left to Update, where the API is expected to reject a shrink, rather than
// risking a replacement that would destroy the filesystem's data.
func filesystemStorageShrinkRequiresReplace(ctx context.Context, req planmodifier.Int64Request, resp *int64planmodifier.RequiresReplaceIfFuncResponse) {
	if req.StateValue.IsNull() || req.PlanValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}
	resp.RequiresReplace = req.PlanValue.ValueInt64() < req.StateValue.ValueInt64()
}

// filesystemStorageNotFound reports whether err is a 404 from the
// FilesystemStorage group. Both ListFilesystems and DeleteFilesystem only ever
// return the generic *components.APIError (no typed *components.ErrorObject
// branch is generated for either, confirmed against both -src listings).
func filesystemStorageNotFound(err error) bool {
	var apiErr *components.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return false
}

// mapFilesystemStorage maps one components.FilesystemData into the resource
// model, shared by Create/Read/Update and the data source.
//
// Not exposed, by team decision (see sdk-coverage.yaml): `keyring`,
// `cluster_user` and `volume_path` (SDK doc comment: dashboard-origin
// responses only, always null for API-driven requests), the nested `team`
// envelope (account metadata, not a provisioning input) and `created_at`
// (presentation metadata, same call BlockStorage made for the identical
// field). Only the site slug of `region` is round-tripped, same as
// latitudesh_block_storage and latitudesh_object_storage.
func mapFilesystemStorage(ctx context.Context, data *FilesystemStorageResourceModel, fs *components.FilesystemData) diag.Diagnostics {
	var diags diag.Diagnostics

	if fs.ID != nil {
		data.ID = types.StringValue(*fs.ID)
	}

	a := fs.Attributes
	if a == nil {
		return diags
	}

	// name, region and protocols are Required with no slug/ID ambiguity, but
	// per house convention a Required attribute is only overwritten when the
	// API actually returns a value — never nulled over a planned value.
	if a.Name != nil {
		data.Name = types.StringValue(*a.Name)
	}
	if a.Region != nil && a.Region.Site != nil && a.Region.Site.Slug != nil {
		data.Region = types.StringValue(*a.Region.Site.Slug)
	}
	if a.Protocols != nil {
		values := make([]string, 0, len(a.Protocols))
		for _, p := range a.Protocols {
			values = append(values, string(p))
		}
		set, setDiags := types.SetValueFrom(ctx, types.StringType, values)
		diags.Append(setDiags...)
		if !diags.HasError() {
			data.Protocols = set
		}
	}

	// size_in_gb is Optional+Computed with a Default: same rule as above, only
	// overwrite on a present value.
	if a.SizeInGb != nil {
		data.SizeInGb = types.Int64Value(*a.SizeInGb)
	}

	// Purely-computed fields (known only after apply): nil from the API is set
	// to null rather than left stale or unknown.
	if a.FilesystemStorageClass != nil {
		data.StorageClass = types.StringValue(string(*a.FilesystemStorageClass))
	} else {
		data.StorageClass = types.StringNull()
	}
	if a.NfsMountPath != nil {
		data.NfsMountPath = types.StringValue(*a.NfsMountPath)
	} else {
		data.NfsMountPath = types.StringNull()
	}
	if a.FileEndpoint != nil {
		data.FileEndpoint = types.StringValue(*a.FileEndpoint)
	} else {
		data.FileEndpoint = types.StringNull()
	}

	// project accepts either a slug or an ID going in, but the API echoes back
	// its own canonical form; only backfill when state has nothing (import), so
	// a configured slug never reads as drift on an attribute that forces
	// replacement.
	if (data.Project.IsNull() || data.Project.IsUnknown()) && a.Project != nil {
		if label := projectLabel(a.Project); label != "" {
			data.Project = types.StringValue(label)
		}
	}

	return diags
}
