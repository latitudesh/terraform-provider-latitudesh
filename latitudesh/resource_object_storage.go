package latitudesh

import (
	"context"
	"errors"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var _ resource.Resource = &ObjectStorageResource{}
var _ resource.ResourceWithImportState = &ObjectStorageResource{}

func NewObjectStorageResource() resource.Resource {
	return &ObjectStorageResource{}
}

// ObjectStorageResource manages an object storage bucket. The SDK exposes no
// update endpoint for buckets (PATCH /storage/buckets/:id exists in the API
// but is missing from the spec the SDK is generated from - see sdk-coverage.yaml),
// so every attribute that affects the bucket forces a replacement.
type ObjectStorageResource struct {
	client         *latitudeshgosdk.Latitudesh
	defaultProject string
}

type ObjectStorageResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Project         types.String `tfsdk:"project"`
	Name            types.String `tfsdk:"name"`
	Region          types.String `tfsdk:"region"`
	StorageClass    types.String `tfsdk:"storage_class"`
	Versioning      types.Bool   `tfsdk:"versioning"`
	Locking         types.Bool   `tfsdk:"locking"`
	RetentionMode   types.String `tfsdk:"retention_mode"`
	RetentionPeriod types.Int64  `tfsdk:"retention_period"`
	BucketName      types.String `tfsdk:"bucket_name"`
	Endpoint        types.String `tfsdk:"endpoint"`
	StorageType     types.String `tfsdk:"storage_type"`
	Source          types.String `tfsdk:"source"`
	CreatedAt       types.String `tfsdk:"created_at"`
}

func (r *ObjectStorageResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_object_storage"
}

func (r *ObjectStorageResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Object Storage bucket resource. Creates and manages an S3-compatible bucket on [Latitude.sh](https://latitude.sh/). There is no update endpoint for buckets, so changing any attribute forces a new resource.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Object storage identifier (`bucket_` prefixed).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "The project (ID or slug) to create the bucket in. Optional here only if `project` is set on the provider block; one of the two is required. Changing it forces a new resource.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Object storage name. Cannot contain special characters or spaces. Changing this forces a new resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Site slug representing the region (e.g. `DAL`, `SAO2`). Changing this forces a new resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"storage_class": schema.StringAttribute{
				MarkdownDescription: "Backend storage tier. `standard` is the default S3-compatible tier. `high_performance` is a lower-latency, higher-throughput tier available in select regions only. Changing this forces a new resource.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("standard"),
				Validators: []validator.String{
					stringvalidator.OneOf("standard", "high_performance"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"versioning": schema.BoolAttribute{
				MarkdownDescription: "Enable S3 object versioning. Versioning can be suspended later unless Object Lock is enabled; enabling Object Lock requires versioning and prevents versioning from being suspended. Defaults to `false`. Changing this forces a new resource (there is no update endpoint to suspend it in place).",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"locking": schema.BoolAttribute{
				MarkdownDescription: "Enable S3 Object Lock (WORM). Must be enabled at bucket creation; cannot be added to an existing bucket. When `true`, `versioning` is automatically enabled. Defaults to `false`. Changing this forces a new resource.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"retention_mode": schema.StringAttribute{
				MarkdownDescription: "Object Lock retention mode applied to new objects. `GOVERNANCE` allows privileged users to override the retention; `COMPLIANCE` cannot be overridden by anyone. Only applies when `locking` is `true`. Defaults to `NONE`. Changing this forces a new resource.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("NONE"),
				Validators: []validator.String{
					stringvalidator.OneOf("NONE", "GOVERNANCE", "COMPLIANCE"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"retention_period": schema.Int64Attribute{
				MarkdownDescription: "Default retention period, in days, applied to new objects when Object Lock is enabled. Only applies when `locking` is `true`. Changing this forces a new resource.",
				Optional:            true,
				Validators: []validator.Int64{
					int64validator.AtLeast(0),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"bucket_name": schema.StringAttribute{
				MarkdownDescription: "S3-compatible bucket name.",
				Computed:            true,
			},
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "Region-specific S3-compatible endpoint URL for accessing the bucket.",
				Computed:            true,
			},
			"storage_type": schema.StringAttribute{
				MarkdownDescription: "Type of storage (e.g. `object`).",
				Computed:            true,
			},
			"source": schema.StringAttribute{
				MarkdownDescription: "How the bucket originated: `default` for buckets created through the API, or `synchronized` for buckets imported from the storage provider.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "The timestamp for when the object storage was created.",
				Computed:            true,
			},
		},
	}
}

func (r *ObjectStorageResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ObjectStorageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ObjectStorageResourceModel

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

	attrs := operations.PostStorageBucketsAttributes{
		Project: project,
		Name:    data.Name.ValueString(),
		Region:  data.Region.ValueString(),
	}

	if !data.StorageClass.IsNull() && !data.StorageClass.IsUnknown() {
		storageClass := operations.StorageClass(data.StorageClass.ValueString())
		attrs.StorageClass = &storageClass
	}
	if !data.Versioning.IsNull() && !data.Versioning.IsUnknown() {
		versioning := data.Versioning.ValueBool()
		attrs.Versioning = &versioning
	}
	if !data.Locking.IsNull() && !data.Locking.IsUnknown() {
		locking := data.Locking.ValueBool()
		attrs.Locking = &locking
	}
	if !data.RetentionMode.IsNull() && !data.RetentionMode.IsUnknown() {
		retentionMode := operations.RetentionMode(data.RetentionMode.ValueString())
		attrs.RetentionMode = &retentionMode
	}
	if !data.RetentionPeriod.IsNull() && !data.RetentionPeriod.IsUnknown() {
		retentionPeriod := data.RetentionPeriod.ValueInt64()
		attrs.RetentionPeriod = &retentionPeriod
	}

	createRequest := operations.PostStorageBucketsRequestBody{
		Data: operations.PostStorageBucketsData{
			Type:       operations.PostStorageBucketsTypeObjects,
			Attributes: attrs,
		},
	}

	result, err := r.client.ObjectStorage.PostStorageBuckets(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create object storage, got error: "+err.Error())
		return
	}

	if result.Object == nil || result.Object.Data == nil || result.Object.Data.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get object storage ID from response")
		return
	}

	data.ID = types.StringValue(*result.Object.Data.ID)
	data.Project = types.StringValue(project)

	r.readObjectStorageInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ObjectStorageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ObjectStorageResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readObjectStorageInto(ctx, &data, &resp.Diagnostics)
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
// plan is RequiresReplace because the SDK exposes no PATCH for buckets. It
// only exists to satisfy resource.Resource; it re-reads rather than sending
// any write.
func (r *ObjectStorageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ObjectStorageResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readObjectStorageInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ObjectStorageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ObjectStorageResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.ID.ValueString()

	_, err := r.client.ObjectStorage.DeleteStorageBuckets(ctx, id)
	if err != nil {
		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to delete object storage, got error: "+err.Error())
		return
	}
}

func (r *ObjectStorageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *ObjectStorageResource) readObjectStorageInto(ctx context.Context, data *ObjectStorageResourceModel, diags *diag.Diagnostics) {
	id := data.ID.ValueString()

	result, err := r.client.ObjectStorage.GetStorageBucket(ctx, id)
	if err != nil {
		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			data.ID = types.StringNull()
			return
		}
		diags.AddError("Client Error", "Unable to read object storage, got error: "+err.Error())
		return
	}

	if result.Object == nil || result.Object.Data == nil {
		data.ID = types.StringNull()
		return
	}

	bucket := result.Object.Data
	if bucket.ID != nil {
		data.ID = types.StringValue(*bucket.ID)
	}

	a := bucket.Attributes
	if a == nil {
		return
	}

	if a.Name != nil {
		data.Name = types.StringValue(*a.Name)
	}

	// Purely-computed fields (known only after apply) are set to null when the
	// API omits them, so a create never leaves an unknown value in state. In
	// practice the API always returns these for an existing bucket.
	if a.BucketName != nil {
		data.BucketName = types.StringValue(*a.BucketName)
	} else {
		data.BucketName = types.StringNull()
	}

	if a.StorageType != nil {
		data.StorageType = types.StringValue(*a.StorageType)
	} else {
		data.StorageType = types.StringNull()
	}

	if a.Endpoint != nil {
		data.Endpoint = types.StringValue(*a.Endpoint)
	} else {
		data.Endpoint = types.StringNull()
	}

	if a.Source != nil {
		data.Source = types.StringValue(*a.Source)
	} else {
		data.Source = types.StringNull()
	}

	if a.CreatedAt != nil {
		data.CreatedAt = types.StringValue(a.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	} else {
		data.CreatedAt = types.StringNull()
	}

	// Configured/defaulted fields always carry a concrete value in the plan and
	// prior state (schema defaults for storage_class/versioning/locking/
	// retention_mode; a user value or null for retention_period). The API may
	// omit them from a response, so only overwrite when a value is present:
	// writing null on absence would clobber the default and yield an
	// inconsistent-state error after apply or recurring drift on refresh.
	if a.StorageClass != nil {
		data.StorageClass = types.StringValue(string(*a.StorageClass))
	}

	if a.Versioning != nil {
		data.Versioning = types.BoolValue(*a.Versioning)
	}

	if a.Locking != nil {
		data.Locking = types.BoolValue(*a.Locking)
	}

	if a.RetentionMode != nil {
		data.RetentionMode = types.StringValue(string(*a.RetentionMode))
	}

	if a.RetentionPeriod != nil {
		data.RetentionPeriod = types.Int64Value(*a.RetentionPeriod)
	}

	// The region sub-object (city/country) is an expansion of the site slug
	// already carried by `region`; only the identifier is round-tripped here,
	// and only when the config didn't supply one (import).
	if (data.Region.IsNull() || data.Region.IsUnknown()) && a.Region != nil && a.Region.ID != nil {
		data.Region = types.StringValue(*a.Region.ID)
	}

	if (data.Project.IsNull() || data.Project.IsUnknown()) && a.Project != nil {
		if a.Project.Slug != nil {
			data.Project = types.StringValue(*a.Project.Slug)
		} else if a.Project.ID != nil {
			data.Project = types.StringValue(*a.Project.ID)
		}
	}
}
