package latitudesh

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

var _ datasource.DataSource = &RoleDataSource{}

func NewRoleDataSource() datasource.DataSource {
	return &RoleDataSource{}
}

type RoleDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type RoleDataSourceModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

func (d *RoleDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (d *RoleDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Role data source",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Role identifier",
				Optional:            true,
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Role name",
				Optional:            true,
				Computed:            true,
			},
		},
	}
}

func (d *RoleDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*latitudeshgosdk.Latitudesh)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			"Expected *latitudeshgosdk.Latitudesh, got: %T. Please report this issue to the provider developers.",
		)
		return
	}

	d.client = client
}

func (d *RoleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data RoleDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Check that either ID or name is provided
	if data.ID.IsNull() && data.Name.IsNull() {
		resp.Diagnostics.AddError(
			"Missing Required Attribute",
			"Either 'id' or 'name' must be provided to look up a role.",
		)
		return
	}

	var role *components.RoleData

	if !data.ID.IsNull() {
		// Look up by ID
		roleID := data.ID.ValueString()
		result, err := d.client.Roles.Get(ctx, roleID)
		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read role %s, got error: %s", roleID, err.Error()))
			return
		}
		if result.Role != nil && result.Role.Data != nil {
			role = result.Role.Data
		}
	} else {
		// Look up by name - get all roles and find matching name
		name := data.Name.ValueString()
		result, err := d.client.Roles.List(ctx, nil, nil)
		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to search for role with name %s, got error: %s", name, err.Error()))
			return
		}
		if result.Object != nil && result.Object.Data != nil {
			for _, r := range result.Object.Data {
				if r.Attributes != nil && r.Attributes.Name != nil && *r.Attributes.Name == name {
					role = &r
					break
				}
			}
		}
	}

	if role == nil {
		resp.Diagnostics.AddError("Role Not Found", "The specified role was not found.")
		return
	}

	// Populate the data model
	if role.ID != nil {
		data.ID = types.StringValue(*role.ID)
	}

	if role.Attributes != nil {
		if role.Attributes.Name != nil {
			data.Name = types.StringValue(*role.Attributes.Name)
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
