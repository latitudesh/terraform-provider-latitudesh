package latitudesh

import (
	"context"
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
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"

	iprovider "github.com/latitudesh/terraform-provider-latitudesh/internal/provider"
)

var (
	_ datasource.DataSource              = &SSHKeyDataSource{}
	_ datasource.DataSourceWithConfigure = &SSHKeyDataSource{}
)

func NewSSHKeyDataSource() datasource.DataSource {
	return &SSHKeyDataSource{}
}

type SSHKeyDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type SSHKeyDataSourceModel struct {
	// Selectors (exactly one)
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Fingerprint types.String `tfsdk:"fingerprint"`

	// Attributes
	PublicKey types.String `tfsdk:"public_key"`

	CreatedAt types.String `tfsdk:"created_at"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

func (d *SSHKeyDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

func (d *SSHKeyDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *SSHKeyDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "SSH Key data source - lookup an SSH key by id, name, or fingerprint.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "SSH key identifier to look up.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("id"),
						path.MatchRoot("name"),
						path.MatchRoot("fingerprint"),
					),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "SSH key name to look up (must be unique in your account).",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("id"),
						path.MatchRoot("name"),
						path.MatchRoot("fingerprint"),
					),
				},
			},
			"fingerprint": schema.StringAttribute{
				MarkdownDescription: "SSH key fingerprint to look up.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("id"),
						path.MatchRoot("name"),
						path.MatchRoot("fingerprint"),
					),
				},
			},

			"public_key": schema.StringAttribute{
				MarkdownDescription: "The SSH public key.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Timestamp when the SSH key was created.",
				Computed:            true,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "Timestamp when the SSH key was last updated.",
				Computed:            true,
			},
		},
	}
}

func (d *SSHKeyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data SSHKeyDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	// Avoid unknown selectors (e.g., from unresolved variables)
	if data.ID.IsUnknown() || data.Name.IsUnknown() || data.Fingerprint.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unknown selector value",
			"One of 'id', 'name', or 'fingerprint' is unknown. Please provide a concrete value.",
		)
		return
	}

	var key *components.SSHKeyData
	var err error

	switch {
	case !data.ID.IsNull():
		key, err = d.getByID(ctx, data.ID.ValueString())
	case !data.Fingerprint.IsNull():
		key, err = d.findOne(ctx, &findOneArgs{Fingerprint: data.Fingerprint.ValueString()})
	case !data.Name.IsNull():
		key, err = d.findOne(ctx, &findOneArgs{Name: data.Name.ValueString()})
	default:
		resp.Diagnostics.AddError(
			"Missing selector",
			"Exactly one of 'id', 'name', or 'fingerprint' must be provided.",
		)
		return
	}

	if err != nil {
		resp.Diagnostics.AddError("Client Error", err.Error())
		return
	}
	if key == nil {
		resp.Diagnostics.AddError("SSH key not found", fmt.Sprintf("No SSH key exists with ID %q", data.ID.ValueString()))
		return
	}

	if key.ID != nil {
		data.ID = types.StringValue(*key.ID)
	}
	if key.Attributes != nil {
		attr := key.Attributes

		if attr.Name != nil {
			data.Name = types.StringValue(*attr.Name)
		}
		if attr.PublicKey != nil {
			data.PublicKey = types.StringValue(*attr.PublicKey)
		}
		if attr.Fingerprint != nil {
			data.Fingerprint = types.StringValue(*attr.Fingerprint)
		}
		if attr.CreatedAt != nil {
			data.CreatedAt = types.StringValue(*attr.CreatedAt)
		}
		if attr.UpdatedAt != nil {
			data.UpdatedAt = types.StringValue(*attr.UpdatedAt)
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *SSHKeyDataSource) getByID(ctx context.Context, id string) (*components.SSHKeyData, error) {
	res, err := d.client.SSHKeys.Retrieve(ctx, id)
	if err != nil {
		// 404 -> not found
		if apiErr, ok := err.(*components.APIError); ok && apiErr.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("unable to retrieve SSH key %q: %w", id, err)
	}
	if res.Object == nil || res.Object.Data == nil {
		return nil, nil
	}
	return res.Object.Data, nil
}

type findOneArgs struct {
	Name        string
	Fingerprint string
}

// findOne: lists all keys and filters in memory by name or fingerprint.
// This avoids using non-existent fields (e.g., FilterName) in GetSSHKeysRequest.
func (d *SSHKeyDataSource) findOne(ctx context.Context, args *findOneArgs) (*components.SSHKeyData, error) {
	var res *operations.GetSSHKeysResponse
	var err error

	res, err = d.client.SSHKeys.ListAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to list SSH keys: %w", err)
	}
	if res == nil || res.SSHKeys == nil || res.SSHKeys.Data == nil || len(res.SSHKeys.Data) == 0 {
		return nil, nil
	}

	normalize := func(s string) string { return strings.TrimSpace(s) }

	nameQ := normalize(args.Name)
	fpQ := normalize(args.Fingerprint)

	for i := range res.SSHKeys.Data {
		k := res.SSHKeys.Data[i]
		if k.Attributes == nil {
			continue
		}
		if nameQ != "" && k.Attributes.Name != nil && normalize(*k.Attributes.Name) == nameQ {
			return &k, nil
		}
		if fpQ != "" && k.Attributes.Fingerprint != nil && normalize(*k.Attributes.Fingerprint) == fpQ {
			return &k, nil
		}
	}

	return nil, nil
}
