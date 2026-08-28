package latitudesh

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"

	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var (
	_ datasource.DataSource              = &APIKeyDataSource{}
	_ datasource.DataSourceWithConfigure = &APIKeyDataSource{}
)

func NewAPIKeyDataSource() datasource.DataSource {
	return &APIKeyDataSource{}
}

type APIKeyDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

// APIKeyDataSourceModel deliberately has no `token`: the Latitude.sh API only
// ever returns the full token from the create (or rotate) call, never from
// List, so a data source lookup could never populate it with a real value.
type APIKeyDataSourceModel struct {
	// Selectors (exactly one)
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`

	// Attributes
	ReadOnly       types.Bool   `tfsdk:"read_only"`
	AllowedIps     types.List   `tfsdk:"allowed_ips"`
	TokenLastSlice types.String `tfsdk:"token_last_slice"`
	APIVersion     types.String `tfsdk:"api_version"`
	UserID         types.String `tfsdk:"user_id"`
	UserEmail      types.String `tfsdk:"user_email"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
	LastUsedAt     types.String `tfsdk:"last_used_at"`
}

func (d *APIKeyDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}

func (d *APIKeyDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *APIKeyDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "API Key data source - lookup an API key by id or name. The full token is never returned outside of the create call and so is not exposed here; see the `latitudesh_api_key` resource.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "API key identifier to look up.",
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
				MarkdownDescription: "API key name to look up.",
				Optional:            true,
				Computed:            true,
			},
			"read_only": schema.BoolAttribute{
				MarkdownDescription: "Whether the API key is read-only.",
				Computed:            true,
			},
			"allowed_ips": schema.ListAttribute{
				MarkdownDescription: "List of allowed IP addresses or CIDR ranges for this API key.",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"token_last_slice": schema.StringAttribute{
				MarkdownDescription: "The last 5 characters of the token.",
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
				MarkdownDescription: "Timestamp when the API key was created.",
				Computed:            true,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "Timestamp when the API key was last updated.",
				Computed:            true,
			},
			"last_used_at": schema.StringAttribute{
				MarkdownDescription: "The last time a request was made to the API using this key.",
				Computed:            true,
			},
		},
	}
}

func (d *APIKeyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data APIKeyDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	if data.ID.IsUnknown() || data.Name.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unknown selector value",
			"One of 'id' or 'name' is unknown. Please provide a concrete value.",
		)
		return
	}

	res, err := d.client.APIKeys.List(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to list API keys, got error: "+err.Error())
		return
	}
	if res == nil || res.APIKeys == nil {
		resp.Diagnostics.AddError("Not Found", "No API keys found")
		return
	}

	var found *components.APIKey
	for i := range res.APIKeys.Data {
		k := res.APIKeys.Data[i]
		switch {
		case !data.ID.IsNull():
			if k.ID != nil && *k.ID == data.ID.ValueString() {
				found = &k
			}
		case !data.Name.IsNull():
			if k.Attributes != nil && k.Attributes.Name != nil && *k.Attributes.Name == data.Name.ValueString() {
				found = &k
			}
		}
		if found != nil {
			break
		}
	}

	if found == nil {
		selector := fmt.Sprintf("ID %q", data.ID.ValueString())
		if data.ID.IsNull() {
			selector = fmt.Sprintf("name %q", data.Name.ValueString())
		}
		resp.Diagnostics.AddError("Not Found", fmt.Sprintf("No API key exists with %s", selector))
		return
	}

	d.mapAPIKeyToModel(ctx, found, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// mapAPIKeyToModel maps an API key from the API response onto the data
// source model. There is no `token` field to map: List never returns it.
func (d *APIKeyDataSource) mapAPIKeyToModel(ctx context.Context, apiKey *components.APIKey, data *APIKeyDataSourceModel, diags *diag.Diagnostics) {
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
