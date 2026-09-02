package latitudesh

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	// ProtonMail's fork is the maintained successor to golang.org/x/crypto/openpgp
	// (frozen); unlike the frozen package it parses modern Ed25519/EdDSA keys,
	// which are GnuPG's default. Drop-in compatible API.
	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"

	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var (
	_ resource.Resource              = &ObjectStorageAccessKeyResource{}
	_ resource.ResourceWithConfigure = &ObjectStorageAccessKeyResource{}
)

func NewObjectStorageAccessKeyResource() resource.Resource {
	return &ObjectStorageAccessKeyResource{}
}

// ObjectStorageAccessKeyResource manages a long-lived object storage access
// key. The API returns the secret only in the create response, so the secret
// is either stored in state (sensitive) or — when `pgp_key` is set — stored
// only as a PGP-encrypted blob, mirroring aws_iam_access_key. For run-scoped
// credentials that never touch state, use the ephemeral variant instead.
type ObjectStorageAccessKeyResource struct {
	client         *latitudeshgosdk.Latitudesh
	defaultProject string
}

type ObjectStorageAccessKeyResourceModel struct {
	ID                types.String                            `tfsdk:"id"`
	Name              types.String                            `tfsdk:"name"`
	Project           types.String                            `tfsdk:"project"`
	StorageClass      types.String                            `tfsdk:"storage_class"`
	Region            types.String                            `tfsdk:"region"`
	AccessScope       types.String                            `tfsdk:"access_scope"`
	BucketPermissions []ObjectStorageAccessKeyPermissionModel `tfsdk:"bucket_permissions"`
	PgpKey            types.String                            `tfsdk:"pgp_key"`

	AccessKeyID     types.String `tfsdk:"access_key_id"`
	SecretAccessKey types.String `tfsdk:"secret_access_key"`
	EncryptedSecret types.String `tfsdk:"encrypted_secret"`
	KeyFingerprint  types.String `tfsdk:"key_fingerprint"`
	Username        types.String `tfsdk:"username"`
	Status          types.String `tfsdk:"status"`
}

func (r *ObjectStorageAccessKeyResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_object_storage_access_key"
}

func (r *ObjectStorageAccessKeyResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.client = deps.Client
	r.defaultProject = deps.DefaultProject
}

func (r *ObjectStorageAccessKeyResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Object Storage access key resource. Creates and manages a long-lived S3-compatible access key. " +
			"The API returns the secret only once, on creation: by default it is stored in the Terraform state as a sensitive value; " +
			"set `pgp_key` to store only a PGP-encrypted copy instead. There is no update endpoint, so changing any attribute " +
			"forces a new resource (which rotates the credentials). For a run-scoped key that never touches state, use the " +
			"`latitudesh_object_storage_access_key` ephemeral resource. " +
			"Note: deleting a key outside Terraform is not auto-detected (the secret is create-only, so the resource errs " +
			"against destructive recreation); run `terraform apply -replace=...` to rotate or recreate it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Access key identifier (the server-side username).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name for the access key. Normalized server-side (lowercased, with special characters replaced by hyphens). Changing this forces a new resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "Project (ID or slug) the access key belongs to. Optional here only if `project` is set on the provider block; one of the two is required. Changing it forces a new resource.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"storage_class": schema.StringAttribute{
				MarkdownDescription: "Backend storage tier. `standard` provisions the key on Wasabi; `high_performance` provisions it on VAST. Changing this forces a new resource.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("standard", "high_performance"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Region slug (e.g. `DAL`, `SAO2`). Selects the VAST cluster for `high_performance` keys. Changing this forces a new resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"access_scope": schema.StringAttribute{
				MarkdownDescription: "`fullaccess` grants access to all of the project's buckets. `limited_access` restricts the key to the buckets listed in `bucket_permissions`. Defaults to `fullaccess`. Changing this forces a new resource.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("fullaccess"),
				Validators: []validator.String{
					stringvalidator.OneOf("fullaccess", "limited_access"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"bucket_permissions": schema.ListNestedAttribute{
				MarkdownDescription: "Per-bucket permissions. Required when `access_scope` is `limited_access`; ignored for `fullaccess`. Changing this forces a new resource.",
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
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"pgp_key": schema.StringAttribute{
				MarkdownDescription: "A PGP public key — either base64-encoded binary or ASCII-armored (`-----BEGIN PGP PUBLIC KEY BLOCK-----`) — used to encrypt the secret. When set, the plaintext secret is never stored in state: only `encrypted_secret` is, and decrypting it requires the matching private key. Changing this forces a new resource.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"access_key_id": schema.StringAttribute{
				MarkdownDescription: "Access key ID for S3-compatible clients.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"secret_access_key": schema.StringAttribute{
				MarkdownDescription: "Secret access key. Stored in state as a sensitive value; null when `pgp_key` is set. The API never returns it again after creation.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"encrypted_secret": schema.StringAttribute{
				MarkdownDescription: "Base64 PGP-encrypted secret, set only when `pgp_key` is provided. Decrypt with e.g. `terraform output -raw encrypted_secret | base64 --decode | gpg --decrypt`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"key_fingerprint": schema.StringAttribute{
				MarkdownDescription: "Fingerprint of the PGP key used to encrypt the secret. Empty when `pgp_key` is not set.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"username": schema.StringAttribute{
				MarkdownDescription: "Underlying IAM user the key belongs to.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Access key status (e.g. `Active`).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *ObjectStorageAccessKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ObjectStorageAccessKeyResourceModel

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

	accessScope := operations.AccessScope(data.AccessScope.ValueString())
	if accessScope == operations.AccessScopeLimitedAccess && len(data.BucketPermissions) == 0 {
		resp.Diagnostics.AddError("Missing bucket_permissions",
			"`access_scope = \"limited_access\"` requires at least one entry in `bucket_permissions`.")
		return
	}

	// Parse the PGP key and prove it can encrypt before creating anything: a
	// malformed or unusable key must not leave an orphaned credential behind,
	// because the real secret is only ever returned once.
	var pgpEntity *openpgp.Entity
	if !data.PgpKey.IsNull() && data.PgpKey.ValueString() != "" {
		entity, err := parsePGPPublicKey(data.PgpKey.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Invalid pgp_key", err.Error())
			return
		}
		if _, _, err := encryptSecretWithPGP(entity, "probe"); err != nil {
			resp.Diagnostics.AddError("Invalid pgp_key", fmt.Sprintf("the key cannot be used for encryption: %s", err))
			return
		}
		pgpEntity = entity
	}

	res, err := r.client.ObjectStorage.PostStorageAccessKeys(ctx, operations.PostStorageAccessKeysRequestBody{
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

	username := resolveAccessKeyUsername(key, data.Name.ValueString())

	data.ID = types.StringValue(username)
	data.Username = types.StringValue(username)
	data.Project = types.StringValue(project)
	data.AccessKeyID = types.StringValue(keyID)

	if pgpEntity != nil {
		fingerprint, encrypted, err := encryptSecretWithPGP(pgpEntity, secret)
		if err != nil {
			resp.Diagnostics.AddError("Unable to encrypt secret",
				fmt.Sprintf("The access key %q was created but its secret could not be PGP-encrypted: %s. "+
					"The secret cannot be retrieved again; taint or replace this resource to rotate it.", username, err))
			return
		}
		data.SecretAccessKey = types.StringNull()
		data.EncryptedSecret = types.StringValue(encrypted)
		data.KeyFingerprint = types.StringValue(fingerprint)
	} else {
		data.SecretAccessKey = types.StringValue(secret)
		data.EncryptedSecret = types.StringNull()
		data.KeyFingerprint = types.StringNull()
	}

	if key.Status != nil {
		data.Status = types.StringValue(*key.Status)
	} else {
		data.Status = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ObjectStorageAccessKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ObjectStorageAccessKeyResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.ObjectStorage.GetStorageAccessKeys(ctx, data.Project.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("unable to list object storage access keys: %s", err))
		return
	}

	// Absence from the list is deliberately NOT treated as deletion. The
	// secret is create-only and cannot be re-read, so a false "deleted" would
	// trigger a destructive recreate that loses the secret (and, if the key
	// still exists, 409s on the way back). We observed at least one case where
	// a live high_performance key was missing from the list response, so we do
	// not trust absence as proof of deletion: refresh computed fields when the
	// key IS found, otherwise keep the recorded state untouched. Out-of-band
	// deletion is handled by the operator with `terraform apply -replace=...`.
	if entry := findAccessKeyEntry(res, data.StorageClass.ValueString(), data.Username.ValueString(), data.Region.ValueString()); entry != nil {
		if entry.accessKeyID != "" {
			data.AccessKeyID = types.StringValue(entry.accessKeyID)
		}
		if entry.status != "" {
			data.Status = types.StringValue(entry.status)
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update never runs: every configurable attribute forces replacement and the
// API has no update endpoint. The framework still requires the method.
func (r *ObjectStorageAccessKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ObjectStorageAccessKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ObjectStorageAccessKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ObjectStorageAccessKeyResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	region := data.Region.ValueString()
	_, err := r.client.ObjectStorage.DeleteStorageAccessKeysUsername(
		ctx,
		data.Username.ValueString(),
		operations.PathParamStorageClass(data.StorageClass.ValueString()),
		data.Project.ValueString(),
		&region,
	)
	if err != nil {
		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return
		}
		resp.Diagnostics.AddError("Client Error",
			fmt.Sprintf("unable to delete object storage access key %q: %s", data.Username.ValueString(), err))
	}
}

// accessKeyListEntry flattens the two per-tier list item types into the fields
// Read needs.
type accessKeyListEntry struct {
	accessKeyID string
	status      string
}

func findAccessKeyEntry(res *operations.GetStorageAccessKeysResponse, storageClass, username, region string) *accessKeyListEntry {
	if res == nil || res.Object == nil || res.Object.Data == nil {
		return nil
	}

	match := func(entryUsername, entryName, entryRegion *string, keyID, status *string) *accessKeyListEntry {
		id := ""
		if entryUsername != nil && *entryUsername != "" {
			id = *entryUsername
		} else if entryName != nil {
			id = *entryName
		}
		if id != username {
			return nil
		}
		// high_performance keys are listed across every VAST region; only the
		// entry from this resource's region is ours.
		if entryRegion != nil && *entryRegion != "" && region != "" && !strings.EqualFold(*entryRegion, region) {
			return nil
		}
		e := &accessKeyListEntry{}
		if keyID != nil {
			e.accessKeyID = *keyID
		}
		if status != nil {
			e.status = *status
		}
		return e
	}

	switch storageClass {
	case "high_performance":
		for _, k := range res.Object.Data.HighPerformance {
			if e := match(k.Username, k.Name, k.Region, k.AccessKeyID, k.Status); e != nil {
				return e
			}
		}
	default:
		for _, k := range res.Object.Data.Standard {
			if e := match(k.Username, k.Name, k.Region, k.AccessKeyID, k.Status); e != nil {
				return e
			}
		}
	}
	return nil
}

// parsePGPPublicKey accepts either an ASCII-armored public key or the
// AWS-provider-style base64-encoded binary key.
func parsePGPPublicKey(raw string) (*openpgp.Entity, error) {
	raw = strings.TrimSpace(raw)

	var reader io.Reader
	if strings.Contains(raw, "BEGIN PGP PUBLIC KEY BLOCK") {
		block, err := armor.Decode(strings.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("unable to decode armored PGP key: %w", err)
		}
		if block.Type != openpgp.PublicKeyType {
			return nil, fmt.Errorf("expected a PGP public key, got %q", block.Type)
		}
		reader = block.Body
	} else {
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("pgp_key must be an ASCII-armored PGP public key or its base64-encoded binary form: %w", err)
		}
		reader = bytes.NewReader(decoded)
	}

	entity, err := openpgp.ReadEntity(packet.NewReader(reader))
	if err != nil {
		return nil, fmt.Errorf("unable to parse PGP public key: %w", err)
	}
	return entity, nil
}

// encryptSecretWithPGP returns the key fingerprint and the base64-encoded
// PGP-encrypted secret, matching the aws_iam_access_key `encrypted_secret`
// format (decrypt with `base64 --decode | gpg --decrypt`).
func encryptSecretWithPGP(entity *openpgp.Entity, secret string) (fingerprint, encrypted string, err error) {
	var buf bytes.Buffer
	w, err := openpgp.Encrypt(&buf, []*openpgp.Entity{entity}, nil, nil, nil)
	if err != nil {
		return "", "", fmt.Errorf("unable to start PGP encryption: %w", err)
	}
	if _, err := w.Write([]byte(secret)); err != nil {
		return "", "", fmt.Errorf("unable to encrypt secret: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", "", fmt.Errorf("unable to finish PGP encryption: %w", err)
	}

	return hex.EncodeToString(entity.PrimaryKey.Fingerprint[:]), base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
