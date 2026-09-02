package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
	_ datasource.DataSource              = &ObjectStorageDataSource{}
	_ datasource.DataSourceWithConfigure = &ObjectStorageDataSource{}
)

func NewObjectStorageDataSource() datasource.DataSource {
	return &ObjectStorageDataSource{}
}

type ObjectStorageDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type ObjectStorageDataSourceModel struct {
	// Selectors
	ID      types.String `tfsdk:"id"`
	Name    types.String `tfsdk:"name"`
	Project types.String `tfsdk:"project"`

	// Attributes
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

func (d *ObjectStorageDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_object_storage"
}

func (d *ObjectStorageDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *ObjectStorageDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Object Storage data source - lookup an object storage bucket by `id`, or by `name` (optionally scoped with `project`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Object storage identifier to look up. Mutually exclusive with `name`.",
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
				MarkdownDescription: "Object storage name to look up. Mutually exclusive with `id`. Names are not guaranteed globally unique; narrow the search with `project` if the lookup is ambiguous.",
				Optional:            true,
				Computed:            true,
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "Project (ID or slug) to filter the `name` lookup by. Ignored when `id` is set.",
				Optional:            true,
				Computed:            true,
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Site slug representing the region (e.g. `DAL`, `SAO2`).",
				Computed:            true,
			},
			"storage_class": schema.StringAttribute{
				MarkdownDescription: "Backend storage tier (`standard` or `high_performance`).",
				Computed:            true,
			},
			"versioning": schema.BoolAttribute{
				MarkdownDescription: "Whether S3 object versioning is enabled.",
				Computed:            true,
			},
			"locking": schema.BoolAttribute{
				MarkdownDescription: "Whether S3 Object Lock (WORM) is enabled.",
				Computed:            true,
			},
			"retention_mode": schema.StringAttribute{
				MarkdownDescription: "Object Lock retention mode applied to new objects (`NONE`, `GOVERNANCE`, or `COMPLIANCE`).",
				Computed:            true,
			},
			"retention_period": schema.Int64Attribute{
				MarkdownDescription: "Default retention period, in days, applied to new objects when Object Lock is enabled.",
				Computed:            true,
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

func (d *ObjectStorageDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data ObjectStorageDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.IsUnknown() || data.Name.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unknown selector value",
			"One of 'id' or 'name' is unknown. Please provide a concrete value.",
		)
		return
	}

	var bucket *components.ObjectStorageData
	var err error

	switch {
	case !data.ID.IsNull():
		bucket, err = d.getByID(ctx, data.ID.ValueString())
	case !data.Name.IsNull():
		var project *string
		if !data.Project.IsNull() && data.Project.ValueString() != "" {
			p := data.Project.ValueString()
			project = &p
		}
		bucket, err = d.findByName(ctx, data.Name.ValueString(), project)
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
	if bucket == nil {
		resp.Diagnostics.AddError("Object storage not found", "No object storage bucket matched the given selector.")
		return
	}

	if bucket.ID != nil {
		data.ID = types.StringValue(*bucket.ID)
	}

	if bucket.Attributes != nil {
		a := bucket.Attributes

		if a.Name != nil {
			data.Name = types.StringValue(*a.Name)
		}
		if a.BucketName != nil {
			data.BucketName = types.StringValue(*a.BucketName)
		}
		if a.StorageType != nil {
			data.StorageType = types.StringValue(*a.StorageType)
		}
		if a.StorageClass != nil {
			data.StorageClass = types.StringValue(string(*a.StorageClass))
		}
		if a.Endpoint != nil {
			data.Endpoint = types.StringValue(*a.Endpoint)
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
		if a.Source != nil {
			data.Source = types.StringValue(*a.Source)
		}
		if a.CreatedAt != nil {
			data.CreatedAt = types.StringValue(a.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
		}
		if a.Region != nil && a.Region.ID != nil {
			data.Region = types.StringValue(*a.Region.ID)
		}
		if a.Project != nil {
			if a.Project.Slug != nil {
				data.Project = types.StringValue(*a.Project.Slug)
			} else if a.Project.ID != nil {
				data.Project = types.StringValue(*a.Project.ID)
			}
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *ObjectStorageDataSource) getByID(ctx context.Context, id string) (*components.ObjectStorageData, error) {
	res, err := d.client.ObjectStorage.GetStorageBucket(ctx, id)
	if err != nil {
		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("unable to retrieve object storage %q: %w", id, err)
	}
	if res.Object == nil || res.Object.Data == nil {
		return nil, nil
	}
	return res.Object.Data, nil
}

// findByName lists buckets (optionally filtered by project server-side) and
// matches by name in memory: GetStorageBuckets takes no name filter.
func (d *ObjectStorageDataSource) findByName(ctx context.Context, name string, project *string) (*components.ObjectStorageData, error) {
	res, err := d.client.ObjectStorage.GetStorageBuckets(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("unable to list object storages: %w", err)
	}
	if res == nil || res.ObjectStorages == nil {
		return nil, nil
	}

	return matchObjectStorageByName(res.ObjectStorages.Data, name), nil
}

// matchObjectStorageByName finds the first bucket whose name matches (after
// trimming whitespace), or nil if none match. Split out from findByName so
// the matching logic can be exercised offline without a live client.
func matchObjectStorageByName(data []components.ObjectStorageData, name string) *components.ObjectStorageData {
	nameQ := strings.TrimSpace(name)

	for i := range data {
		b := data[i]
		if b.Attributes == nil || b.Attributes.Name == nil {
			continue
		}
		if strings.TrimSpace(*b.Attributes.Name) == nameQ {
			return &b
		}
	}

	return nil
}
