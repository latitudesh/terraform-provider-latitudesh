package latitudesh

import (
	"context"
	"fmt"
	"time"

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

var _ datasource.DataSource = &APIKeyDataSource{}

func NewAPIKeyDataSource() datasource.DataSource {
	return &APIKeyDataSource{}
}

type APIKeyDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

// APIKeyDataSourceModel intentionally has no `token` attribute: the SDK only
// ever returns the full token from the create/rotate responses, never from
// the List call this data source is built on (see readAPIKeyInto in
// resource_api_key.go for the same constraint on the resource side).
type APIKeyDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	ReadOnly       types.Bool   `tfsdk:"read_only"`
	AllowedIps     types.List   `tfsdk:"allowed_ips"`
	TokenLastSlice types.String `tfsdk:"token_last_slice"`
	APIVersion     types.String `tfsdk:"api_version"`
	UserID         types.String `tfsdk:"user_id"`
	UserEmail      types.String `tfsdk:"user_email"`
	LastUsedAt     types.String `tfsdk:"last_used_at"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
}

func (d *APIKeyDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}

func (d *APIKeyDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *APIKeyDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "API Key data source - lookup an API key by id or name. Does not expose the token: the API only returns the full token when a key is created or rotated.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "API key identifier to look up. Mutually exclusive with name.",
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
				MarkdownDescription: "API key name to look up. Mutually exclusive with id.",
				Optional:            true,
				Computed:            true,
			},
			"read_only": schema.BoolAttribute{
				MarkdownDescription: "Whether the API key is read-only.",
				Computed:            true,
			},
			"allowed_ips": schema.ListAttribute{
				MarkdownDescription: "List of allowed IP addresses or CIDR ranges that may use this API key.",
				ElementType:         types.StringType,
				Computed:            true,
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

func (d *APIKeyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data APIKeyDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.IsNull() && data.Name.IsNull() {
		resp.Diagnostics.AddError(
			"Missing Required Attribute",
			"One of id or name must be specified",
		)
		return
	}

	result, err := d.client.APIKeys.List(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to list API keys, got error: %s", err.Error()))
		return
	}

	if result.APIKeys == nil {
		resp.Diagnostics.AddError("Not Found", "No API keys found")
		return
	}

	var found *components.APIKey
	for i := range result.APIKeys.Data {
		key := result.APIKeys.Data[i]

		if !data.ID.IsNull() && key.ID != nil && *key.ID == data.ID.ValueString() {
			found = &key
			break
		}
		if !data.Name.IsNull() && key.Attributes != nil && key.Attributes.Name != nil &&
			*key.Attributes.Name == data.Name.ValueString() {
			found = &key
			break
		}
	}

	if found == nil {
		selector := ""
		if !data.ID.IsNull() {
			selector = fmt.Sprintf("ID %q", data.ID.ValueString())
		} else {
			selector = fmt.Sprintf("name %q", data.Name.ValueString())
		}
		resp.Diagnostics.AddError("Not Found", fmt.Sprintf("API key with %s not found", selector))
		return
	}

	d.mapAPIKeyToModel(found, &data)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *APIKeyDataSource) mapAPIKeyToModel(key *components.APIKey, data *APIKeyDataSourceModel) {
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
