package latitudesh

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var _ resource.Resource = &APIKeyResource{}
var _ resource.ResourceWithImportState = &APIKeyResource{}

func NewAPIKeyResource() resource.Resource {
	return &APIKeyResource{}
}

// APIKeyResource defines the resource implementation.
type APIKeyResource struct {
	client *latitudeshgosdk.Latitudesh
}

// APIKeyResourceModel describes the resource data model.
type APIKeyResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	ReadOnly       types.Bool   `tfsdk:"read_only"`
	AllowedIps     types.List   `tfsdk:"allowed_ips"`
	Token          types.String `tfsdk:"token"`
	TokenLastSlice types.String `tfsdk:"token_last_slice"`
	APIVersion     types.String `tfsdk:"api_version"`
	UserID         types.String `tfsdk:"user_id"`
	UserEmail      types.String `tfsdk:"user_email"`
	LastUsedAt     types.String `tfsdk:"last_used_at"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
}

func (r *APIKeyResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}

func (r *APIKeyResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "API Key resource. Creates and manages an API key within your [Latitude.sh](https://latitude.sh/) account.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "API key identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the API key",
				Required:            true,
			},
			"read_only": schema.BoolAttribute{
				MarkdownDescription: "Whether the API key is read-only. Read-only keys can only perform GET requests.",
				Optional:            true,
				Computed:            true,
			},
			"allowed_ips": schema.ListAttribute{
				MarkdownDescription: "List of allowed IP addresses or CIDR ranges (e.g., \"192.168.1.100\", \"10.0.0.0/24\") that may use this API key. Unset means no IP restriction.",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"token": schema.StringAttribute{
				MarkdownDescription: "The full API key token. Only returned by the API when the key is created; **this value lands in Terraform state**, so treat the state file as sensitive.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"token_last_slice": schema.StringAttribute{
				MarkdownDescription: "The last 5 characters of the token, safe to display for identification.",
				Computed:            true,
			},
			"api_version": schema.StringAttribute{
				MarkdownDescription: "The API version associated with this API key.",
				Computed:            true,
			},
			"user_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the user that owns this API key.",
				Computed:            true,
			},
			"user_email": schema.StringAttribute{
				MarkdownDescription: "The email of the user that owns this API key.",
				Computed:            true,
			},
			"last_used_at": schema.StringAttribute{
				MarkdownDescription: "The last time a request was made to the API using this API key.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "The timestamp for when the API key was created.",
				Computed:            true,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "The timestamp for the last time the API key was updated.",
				Computed:            true,
			},
		},
	}
}

func (r *APIKeyResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.client = deps.Client
}

func (r *APIKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data APIKeyResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := data.Name.ValueString()
	attrs := &components.CreateAPIKeyAttributes{
		Name: &name,
	}

	if !data.ReadOnly.IsNull() && !data.ReadOnly.IsUnknown() {
		readOnly := data.ReadOnly.ValueBool()
		attrs.ReadOnly = &readOnly
	}

	if !data.AllowedIps.IsNull() && !data.AllowedIps.IsUnknown() {
		var allowedIps []string
		resp.Diagnostics.Append(data.AllowedIps.ElementsAs(ctx, &allowedIps, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		attrs.AllowedIps = allowedIps
	}

	createRequest := components.CreateAPIKey{
		Data: &components.Data{
			Type:       components.CreateAPIKeyTypeAPIKeys,
			Attributes: attrs,
		},
	}

	result, err := r.client.APIKeys.Create(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create API key, got error: "+err.Error())
		return
	}

	if result.Object == nil || result.Object.Data == nil || result.Object.Data.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get API key ID from response")
		return
	}

	data.ID = types.StringValue(*result.Object.Data.ID)

	r.mapAPIKeyToModel(result.Object.Data, &data)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *APIKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data APIKeyResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readAPIKeyInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.IsNull() {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *APIKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data APIKeyResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.ID = id

	idStr := id.ValueString()
	name := data.Name.ValueString()

	attrs := &components.UpdateAPIKeyAttributes{
		Name: &name,
	}

	if !data.ReadOnly.IsNull() && !data.ReadOnly.IsUnknown() {
		readOnly := data.ReadOnly.ValueBool()
		attrs.ReadOnly = &readOnly
	}

	if !data.AllowedIps.IsNull() && !data.AllowedIps.IsUnknown() {
		var allowedIps []string
		resp.Diagnostics.Append(data.AllowedIps.ElementsAs(ctx, &allowedIps, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		attrs.AllowedIps = allowedIps
	}

	updatePayload := components.UpdateAPIKey{
		Data: &components.UpdateAPIKeyData{
			ID:         &idStr,
			Type:       components.UpdateAPIKeyTypeAPIKeys,
			Attributes: attrs,
		},
	}

	// UpdateAPIKey updates name/read_only/allowed_ips without rotating the
	// token. The SDK's Update method calls the separate rotate endpoint
	// instead and is intentionally not wired up here - see sdk-coverage.yaml.
	_, err := r.client.APIKeys.UpdateAPIKey(ctx, idStr, updatePayload)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to update API key, got error: "+err.Error())
		return
	}

	r.readAPIKeyInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *APIKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data APIKeyResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.ID.ValueString()

	_, err := r.client.APIKeys.Delete(ctx, id)
	if err != nil {
		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to delete API key, got error: "+err.Error())
		return
	}
}

func (r *APIKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// readAPIKeyInto refreshes data from the API. The SDK has no single-item Get
// for API keys, so it lists all keys and matches by ID (see the List method
// and the Firewalls-style lookup used by datasource_ssh_key.go/datasource_tag.go).
// A missing key nulls the ID; callers decide whether to RemoveResource.
func (r *APIKeyResource) readAPIKeyInto(ctx context.Context, data *APIKeyResourceModel, diags *diag.Diagnostics) {
	id := data.ID.ValueString()

	result, err := r.client.APIKeys.List(ctx)
	if err != nil {
		diags.AddError("Client Error", "Unable to read API key, got error: "+err.Error())
		return
	}

	if result.APIKeys == nil {
		data.ID = types.StringNull()
		return
	}

	for i := range result.APIKeys.Data {
		key := result.APIKeys.Data[i]
		if key.ID == nil || *key.ID != id {
			continue
		}
		r.mapAPIKeyToModel(&key, data)
		return
	}

	data.ID = types.StringNull()
}

// mapAPIKeyToModel maps an API key from the API response onto the resource
// model. Token is only ever returned by the create/rotate responses (per the
// SDK's field doc comment on Attributes.Token); a nil Token here means "not
// included in this response", not "the key has no token", so the existing
// state value is preserved rather than nulled.
func (r *APIKeyResource) mapAPIKeyToModel(key *components.APIKey, data *APIKeyResourceModel) {
	if key.ID != nil {
		data.ID = types.StringValue(*key.ID)
	}

	attrs := key.Attributes
	if attrs == nil {
		return
	}

	if attrs.Name != nil {
		data.Name = types.StringValue(*attrs.Name)
	}

	if attrs.ReadOnly != nil {
		data.ReadOnly = types.BoolValue(*attrs.ReadOnly)
	} else {
		data.ReadOnly = types.BoolNull()
	}

	if attrs.AllowedIps != nil {
		ips := make([]types.String, 0, len(attrs.AllowedIps))
		for _, ip := range attrs.AllowedIps {
			ips = append(ips, types.StringValue(ip))
		}
		data.AllowedIps, _ = types.ListValueFrom(context.Background(), types.StringType, ips)
	} else {
		data.AllowedIps = types.ListNull(types.StringType)
	}

	if attrs.Token != nil {
		data.Token = types.StringValue(*attrs.Token)
	}

	if attrs.TokenLastSlice != nil {
		data.TokenLastSlice = types.StringValue(*attrs.TokenLastSlice)
	} else {
		data.TokenLastSlice = types.StringNull()
	}

	if attrs.APIVersion != nil {
		data.APIVersion = types.StringValue(*attrs.APIVersion)
	} else {
		data.APIVersion = types.StringNull()
	}

	if attrs.User != nil {
		if attrs.User.ID != nil {
			data.UserID = types.StringValue(*attrs.User.ID)
		} else {
			data.UserID = types.StringNull()
		}
		if attrs.User.Email != nil {
			data.UserEmail = types.StringValue(*attrs.User.Email)
		} else {
			data.UserEmail = types.StringNull()
		}
	} else {
		data.UserID = types.StringNull()
		data.UserEmail = types.StringNull()
	}

	if attrs.LastUsedAt != nil {
		data.LastUsedAt = types.StringValue(attrs.LastUsedAt.Format(time.RFC3339))
	} else {
		data.LastUsedAt = types.StringNull()
	}

	if attrs.CreatedAt != nil {
		data.CreatedAt = types.StringValue(attrs.CreatedAt.Format(time.RFC3339))
	} else {
		data.CreatedAt = types.StringNull()
	}

	if attrs.UpdatedAt != nil {
		data.UpdatedAt = types.StringValue(attrs.UpdatedAt.Format(time.RFC3339))
	} else {
		data.UpdatedAt = types.StringNull()
	}
}
