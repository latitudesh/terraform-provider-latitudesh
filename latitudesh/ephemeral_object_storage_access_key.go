package latitudesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"

	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var (
	_ ephemeral.EphemeralResource              = &ObjectStorageAccessKeyEphemeral{}
	_ ephemeral.EphemeralResourceWithConfigure = &ObjectStorageAccessKeyEphemeral{}
	_ ephemeral.EphemeralResourceWithClose     = &ObjectStorageAccessKeyEphemeral{}
)

func NewObjectStorageAccessKeyEphemeral() ephemeral.EphemeralResource {
	return &ObjectStorageAccessKeyEphemeral{}
}

// ObjectStorageAccessKeyEphemeral provisions a short-lived object storage
// access key: Open creates the key, Close revokes it when the Terraform run
// finishes. The secret therefore never reaches state or plan files.
type ObjectStorageAccessKeyEphemeral struct {
	client         *latitudeshgosdk.Latitudesh
	defaultProject string
}

type ObjectStorageAccessKeyEphemeralModel struct {
	Name              types.String                            `tfsdk:"name"`
	Project           types.String                            `tfsdk:"project"`
	StorageClass      types.String                            `tfsdk:"storage_class"`
	Region            types.String                            `tfsdk:"region"`
	AccessScope       types.String                            `tfsdk:"access_scope"`
	BucketPermissions []ObjectStorageAccessKeyPermissionModel `tfsdk:"bucket_permissions"`

	AccessKeyID     types.String `tfsdk:"access_key_id"`
	SecretAccessKey types.String `tfsdk:"secret_access_key"`
	Username        types.String `tfsdk:"username"`
	Status          types.String `tfsdk:"status"`
}

type ObjectStorageAccessKeyPermissionModel struct {
	BucketID   types.String `tfsdk:"bucket_id"`
	Permission types.String `tfsdk:"permission"`
}

// objectStorageAccessKeyCloseData carries everything Close needs to revoke the
// key through the ephemeral private state, since config values are not
// available again at Close time.
type objectStorageAccessKeyCloseData struct {
	Username     string `json:"username"`
	StorageClass string `json:"storage_class"`
	Project      string `json:"project"`
	Region       string `json:"region"`
}

const objectStorageAccessKeyPrivateKey = "close"

func (e *ObjectStorageAccessKeyEphemeral) Metadata(ctx context.Context, req ephemeral.MetadataRequest, resp *ephemeral.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_object_storage_access_key"
}

func (e *ObjectStorageAccessKeyEphemeral) Configure(ctx context.Context, req ephemeral.ConfigureRequest, resp *ephemeral.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	e.client = deps.Client
	e.defaultProject = deps.DefaultProject
}

func (e *ObjectStorageAccessKeyEphemeral) Schema(ctx context.Context, req ephemeral.SchemaRequest, resp *ephemeral.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Provisions a temporary object storage access key for the duration of the Terraform run. " +
			"The key is created when the ephemeral resource is opened and revoked when the run finishes, so the " +
			"secret never persists in state or plan files. Requires Terraform 1.10 or later.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Name for the access key. Normalized server-side (lowercased, with special characters replaced by hyphens).",
				Required:            true,
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "Project (ID or slug) the access key belongs to. Falls back to the provider-level `project` when omitted.",
				Optional:            true,
				Computed:            true,
			},
			"storage_class": schema.StringAttribute{
				MarkdownDescription: "Backend storage tier. `standard` provisions the key on Wasabi; `high_performance` provisions it on VAST.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("standard", "high_performance"),
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Region slug (e.g. `DAL`, `SAO2`). Selects the VAST cluster for `high_performance` keys.",
				Required:            true,
			},
			"access_scope": schema.StringAttribute{
				MarkdownDescription: "`fullaccess` grants access to all of the project's buckets. `limited_access` restricts the key to the buckets listed in `bucket_permissions`. Defaults to `fullaccess`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("fullaccess", "limited_access"),
				},
			},
			"bucket_permissions": schema.ListNestedAttribute{
				MarkdownDescription: "Per-bucket permissions. Required when `access_scope` is `limited_access`; ignored for `fullaccess`.",
				Optional:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"bucket_id": schema.StringAttribute{
							MarkdownDescription: "Bucket (object storage) ID to grant access to.",
							Required:            true,
						},
						"permission": schema.StringAttribute{
							MarkdownDescription: "`readonly` grants read-only access; `rw` grants read and write access.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("readonly", "rw"),
							},
						},
					},
				},
			},
			"access_key_id": schema.StringAttribute{
				MarkdownDescription: "Access key ID for S3-compatible clients.",
				Computed:            true,
			},
			"secret_access_key": schema.StringAttribute{
				MarkdownDescription: "Secret access key. Returned only on creation and never retrievable again.",
				Computed:            true,
				Sensitive:           true,
			},
			"username": schema.StringAttribute{
				MarkdownDescription: "Underlying IAM user the key belongs to.",
				Computed:            true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Access key status (e.g. `Active`).",
				Computed:            true,
			},
		},
	}
}

func (e *ObjectStorageAccessKeyEphemeral) Open(ctx context.Context, req ephemeral.OpenRequest, resp *ephemeral.OpenResponse) {
	var data ObjectStorageAccessKeyEphemeralModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	project := ""
	if !data.Project.IsNull() && !data.Project.IsUnknown() && data.Project.ValueString() != "" {
		project = data.Project.ValueString()
	} else if e.defaultProject != "" {
		project = e.defaultProject
	}
	if project == "" {
		resp.Diagnostics.AddError("Missing project",
			"Set `project` on this ephemeral resource or define a default in the provider block (provider `latitudesh` { project = \"...\" }).")
		return
	}

	accessScope := operations.AccessScopeFullaccess
	if !data.AccessScope.IsNull() && !data.AccessScope.IsUnknown() && data.AccessScope.ValueString() != "" {
		accessScope = operations.AccessScope(data.AccessScope.ValueString())
	}
	if accessScope == operations.AccessScopeLimitedAccess && len(data.BucketPermissions) == 0 {
		resp.Diagnostics.AddError("Missing bucket_permissions",
			"`access_scope = \"limited_access\"` requires at least one entry in `bucket_permissions`.")
		return
	}

	res, err := e.client.ObjectStorage.PostStorageAccessKeys(ctx, operations.PostStorageAccessKeysRequestBody{
		Data: operations.PostStorageAccessKeysData{
			Type: operations.PostStorageAccessKeysTypeAccessKeys,
			Attributes: buildAccessKeyAttributes(
				project,
				data.StorageClass.ValueString(),
				data.Name.ValueString(),
				data.Region.ValueString(),
				accessScope,
				data.BucketPermissions,
			),
		},
	})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("unable to create object storage access key: %s", err))
		return
	}

	key := extractAccessKey(res)
	if key == nil {
		resp.Diagnostics.AddError("Unexpected API response",
			"The access key creation response did not include the created key.")
		return
	}

	keyID, secret := normalizeAccessKeyCredentials(key)
	if keyID == "" || secret == "" {
		resp.Diagnostics.AddError("Unexpected API response",
			"The access key creation response did not include both the key ID and the secret.")
		return
	}

	data.Project = types.StringValue(project)
	data.AccessScope = types.StringValue(string(accessScope))
	data.AccessKeyID = types.StringValue(keyID)
	data.SecretAccessKey = types.StringValue(secret)

	username := resolveAccessKeyUsername(key, data.Name.ValueString())
	data.Username = types.StringValue(username)

	if key.Status != nil {
		data.Status = types.StringValue(*key.Status)
	} else {
		data.Status = types.StringNull()
	}

	closeData, err := json.Marshal(objectStorageAccessKeyCloseData{
		Username:     username,
		StorageClass: data.StorageClass.ValueString(),
		Project:      project,
		Region:       data.Region.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Internal Error", fmt.Sprintf("unable to encode close data: %s", err))
		return
	}
	resp.Diagnostics.Append(resp.Private.SetKey(ctx, objectStorageAccessKeyPrivateKey, closeData)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.Result.Set(ctx, &data)...)
}

func (e *ObjectStorageAccessKeyEphemeral) Close(ctx context.Context, req ephemeral.CloseRequest, resp *ephemeral.CloseResponse) {
	raw, diags := req.Private.GetKey(ctx, objectStorageAccessKeyPrivateKey)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if len(raw) == 0 {
		resp.Diagnostics.AddWarning("Missing close data",
			"No revocation data was recorded when the access key was created; the key was not revoked.")
		return
	}

	var closeData objectStorageAccessKeyCloseData
	if err := json.Unmarshal(raw, &closeData); err != nil {
		resp.Diagnostics.AddError("Internal Error", fmt.Sprintf("unable to decode close data: %s", err))
		return
	}

	region := closeData.Region
	_, err := e.client.ObjectStorage.DeleteStorageAccessKeysUsername(
		ctx,
		closeData.Username,
		operations.PathParamStorageClass(closeData.StorageClass),
		closeData.Project,
		&region,
	)
	if err != nil {
		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return
		}
		resp.Diagnostics.AddError("Client Error",
			fmt.Sprintf("unable to revoke object storage access key %q: %s", closeData.Username, err))
	}
}

// buildAccessKeyAttributes assembles the create payload shared by the
// ephemeral and the managed access key resource.
func buildAccessKeyAttributes(project, storageClass, name, region string, scope operations.AccessScope, perms []ObjectStorageAccessKeyPermissionModel) operations.PostStorageAccessKeysAttributes {
	attrs := operations.PostStorageAccessKeysAttributes{
		Project:               project,
		AccessKeyStorageClass: operations.AccessKeyStorageClass(storageClass),
		Name:                  name,
		AccessScope:           scope,
		Region:                region,
	}
	for _, p := range perms {
		attrs.BucketPermissions = append(attrs.BucketPermissions, operations.BucketPermissions{
			BucketID:   p.BucketID.ValueString(),
			Permission: operations.Permission(p.Permission.ValueString()),
		})
	}
	return attrs
}

// resolveAccessKeyUsername picks the identifier the delete endpoint expects.
// The server may have normalized the requested name, so what the API reports
// wins over the configured fallback.
func resolveAccessKeyUsername(key *operations.AccessKey, fallback string) string {
	username := fallback
	if key.Name != nil && *key.Name != "" {
		username = *key.Name
	}
	if key.Username != nil && *key.Username != "" {
		username = *key.Username
	}
	return username
}

func extractAccessKey(res *operations.PostStorageAccessKeysResponse) *operations.AccessKey {
	if res == nil || res.Object == nil || res.Object.Data == nil || res.Object.Data.Attributes == nil {
		return nil
	}
	return res.Object.Data.Attributes.AccessKey
}

// normalizeAccessKeyCredentials maps the two provider-specific response shapes
// onto one pair: `standard` (Wasabi) keys arrive as access_key_id /
// secret_access_key, `high_performance` (VAST) keys as access_key / secret_key.
func normalizeAccessKeyCredentials(key *operations.AccessKey) (keyID, secret string) {
	if key == nil {
		return "", ""
	}
	if key.AccessKeyID != nil && *key.AccessKeyID != "" {
		keyID = *key.AccessKeyID
	} else if key.AccessKey != nil {
		keyID = *key.AccessKey
	}
	if key.SecretAccessKey != nil && *key.SecretAccessKey != "" {
		secret = *key.SecretAccessKey
	} else if key.SecretKey != nil {
		secret = *key.SecretKey
	}
	return keyID, secret
}
