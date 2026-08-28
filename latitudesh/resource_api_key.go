package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
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
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
	LastUsedAt     types.String `tfsdk:"last_used_at"`
}

func (r *APIKeyResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}

func (r *APIKeyResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "API Key resource. Creates and manages an API key tied to the current user account on [Latitude.sh](https://latitude.sh/). The full token is returned only once, at creation, and is stored in Terraform state — treat state for this resource as sensitive.",

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
				MarkdownDescription: "Whether the API key is read-only. Read-only keys can only perform GET requests. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"allowed_ips": schema.ListAttribute{
				MarkdownDescription: "List of allowed IP addresses or CIDR ranges (e.g. `192.168.1.100`, `10.0.0.0/24`) that may use this API key. An empty list means no restriction.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"token": schema.StringAttribute{
				MarkdownDescription: "The full API key token. Only ever returned by the create call — the read API never returns it, so it is preserved from state on every subsequent read and is null after import.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"token_last_slice": schema.StringAttribute{
				MarkdownDescription: "The last 5 characters of the token, safe to display for identification purposes.",
				Computed:            true,
			},
			"api_version": schema.StringAttribute{
				MarkdownDescription: "The API version associated with this API key.",
				Computed:            true,
			},
			"user_id": schema.StringAttribute{
				MarkdownDescription: "The identifier of the user that owns this API key.",
				Computed:            true,
			},
			"user_email": schema.StringAttribute{
				MarkdownDescription: "The email of the user that owns this API key.",
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
			"last_used_at": schema.StringAttribute{
				MarkdownDescription: "The last time a request was made to the API using this key.",
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
		var ips []string
		resp.Diagnostics.Append(data.AllowedIps.ElementsAs(ctx, &ips, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		attrs.AllowedIps = ips
	}

	payload := components.CreateAPIKey{
		Data: &components.Data{
			Type:       components.CreateAPIKeyTypeAPIKeys,
			Attributes: attrs,
		},
	}

	result, err := r.client.APIKeys.Create(ctx, payload)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create API key, got error: "+err.Error())
		return
	}

	if result.Object == nil || result.Object.Data == nil || result.Object.Data.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get API key ID from response")
		return
	}

	mapAPIKeyToModel(ctx, result.Object.Data, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

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

	// If the API key no longer exists, drop it from state.
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
		var ips []string
		resp.Diagnostics.Append(data.AllowedIps.ElementsAs(ctx, &ips, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		attrs.AllowedIps = ips
	}

	payload := components.UpdateAPIKey{
		Data: &components.UpdateAPIKeyData{
			ID:         &idStr,
			Type:       components.UpdateAPIKeyTypeAPIKeys,
			Attributes: attrs,
		},
	}

	// Deliberately calls UpdateAPIKey (PATCH — settings only: name, read_only,
	// allowed_ips) and never Update (PUT — rotates the token, invalidating the
	// previous one). Terraform's Update runs on any attribute drift, including
	// changes unrelated to the token; wiring it to the rotate endpoint would
	// silently invalidate the key's live token on those. Rotation is
	// intentionally not exposed by this resource — see the handoff notes.
	_, err := r.client.APIKeys.UpdateAPIKey(ctx, idStr, payload)
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
	var data APIKeyResourceModel
	data.ID = types.StringValue(req.ID)
	// Never available outside the create response; see the token attribute doc.
	data.Token = types.StringNull()

	r.readAPIKeyInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if data.ID.IsNull() {
		resp.Diagnostics.AddError("Not Found", fmt.Sprintf("No API key exists with ID %q", req.ID))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *APIKeyResource) readAPIKeyInto(ctx context.Context, data *APIKeyResourceModel, diags *diag.Diagnostics) {
	id := data.ID.ValueString()

	apiKey, err := r.findAPIKeyByID(ctx, id)
	if err != nil {
		diags.AddError("Client Error", "Unable to read API key, got error: "+err.Error())
		return
	}
	if apiKey == nil {
		data.ID = types.StringNull()
		return
	}

	mapAPIKeyToModel(ctx, apiKey, data, diags)
}

// findAPIKeyByID scans List for the matching ID: the API has no single-item
// GET for API keys, only Create/List/Delete/Update/UpdateAPIKey. GetAPIKeysResponse
// carries a Meta but the SDK models it with no fields, so there is no cursor to
// follow — List does not appear to paginate.
func (r *APIKeyResource) findAPIKeyByID(ctx context.Context, id string) (*components.APIKey, error) {
	res, err := r.client.APIKeys.List(ctx)
	if err != nil {
		return nil, err
	}
	if res == nil || res.APIKeys == nil {
		return nil, nil
	}
	for i := range res.APIKeys.Data {
		k := res.APIKeys.Data[i]
		if k.ID != nil && *k.ID == id {
			return &k, nil
		}
	}
	return nil, nil
}

// mapAPIKeyToModel maps an API key from the API response onto the resource
// model. The token is a special case: it is only ever present in a Create (or
// PUT/rotate) response, never in List, so a nil Token here leaves whatever
// value is already in data untouched instead of nulling it out.
func mapAPIKeyToModel(ctx context.Context, apiKey *components.APIKey, data *APIKeyResourceModel, diags *diag.Diagnostics) {
	if apiKey.ID != nil {
		data.ID = types.StringValue(*apiKey.ID)
	}

	a := apiKey.Attributes
	if a == nil {
		return
	}

	if a.Name != nil {
		data.Name = types.StringValue(*a.Name)
	}

	if a.ReadOnly != nil {
		data.ReadOnly = types.BoolValue(*a.ReadOnly)
	} else {
		data.ReadOnly = types.BoolNull()
	}

	ips := a.AllowedIps
	if ips == nil {
		ips = []string{}
	}
	ipsList, convertDiags := types.ListValueFrom(ctx, types.StringType, ips)
	diags.Append(convertDiags...)
	if !convertDiags.HasError() {
		data.AllowedIps = ipsList
	}

	if a.APIVersion != nil {
		data.APIVersion = types.StringValue(*a.APIVersion)
	} else {
		data.APIVersion = types.StringNull()
	}

	if a.Token != nil {
		data.Token = types.StringValue(*a.Token)
	}

	if a.TokenLastSlice != nil {
		data.TokenLastSlice = types.StringValue(*a.TokenLastSlice)
	} else {
		data.TokenLastSlice = types.StringNull()
	}

	if a.User != nil {
		if a.User.ID != nil {
			data.UserID = types.StringValue(*a.User.ID)
		} else {
			data.UserID = types.StringNull()
		}
		if a.User.Email != nil {
			data.UserEmail = types.StringValue(*a.User.Email)
		} else {
			data.UserEmail = types.StringNull()
		}
	} else {
		data.UserID = types.StringNull()
		data.UserEmail = types.StringNull()
	}

	if a.CreatedAt != nil {
		data.CreatedAt = types.StringValue(a.CreatedAt.Format(time.RFC3339))
	} else {
		data.CreatedAt = types.StringNull()
	}

	if a.UpdatedAt != nil {
		data.UpdatedAt = types.StringValue(a.UpdatedAt.Format(time.RFC3339))
	} else {
		data.UpdatedAt = types.StringNull()
	}

	if a.LastUsedAt != nil {
		data.LastUsedAt = types.StringValue(a.LastUsedAt.Format(time.RFC3339))
	} else {
		data.LastUsedAt = types.StringNull()
	}
}
