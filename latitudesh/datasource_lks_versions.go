package latitudesh

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"

	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var (
	_ datasource.DataSource              = &LksVersionsDataSource{}
	_ datasource.DataSourceWithConfigure = &LksVersionsDataSource{}
)

func NewLksVersionsDataSource() datasource.DataSource {
	return &LksVersionsDataSource{}
}

type LksVersionsDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type LksVersionsDataSourceModel struct {
	// Synthetic identifier: the lookup takes no arguments, so there is nothing
	// to key it on.
	ID types.String `tfsdk:"id"`

	DefaultVersion types.String `tfsdk:"default_version"`
	Versions       types.List   `tfsdk:"versions"`
}

type LksVersionItemModel struct {
	ID                   types.String `tfsdk:"id"`
	Version              types.String `tfsdk:"version"`
	Default              types.Bool   `tfsdk:"default"`
	AvailableForCreation types.Bool   `tfsdk:"available_for_creation"`
	AvailableForUpgrade  types.Bool   `tfsdk:"available_for_upgrade"`
}

var lksVersionItemObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":                     types.StringType,
		"version":                types.StringType,
		"default":                types.BoolType,
		"available_for_creation": types.BoolType,
		"available_for_upgrade":  types.BoolType,
	},
}

// lksVersionItemValue maps one SDK version into the list item model.
func lksVersionItemValue(v *components.LksKubernetesVersionData) LksVersionItemModel {
	item := LksVersionItemModel{
		ID:                   types.StringPointerValue(v.ID),
		Version:              types.StringNull(),
		Default:              types.BoolNull(),
		AvailableForCreation: types.BoolNull(),
		AvailableForUpgrade:  types.BoolNull(),
	}

	if v.Attributes == nil {
		return item
	}

	item.Version = types.StringPointerValue(v.Attributes.Version)
	item.Default = types.BoolPointerValue(v.Attributes.Default)
	item.AvailableForCreation = types.BoolPointerValue(v.Attributes.AvailableForCreation)
	item.AvailableForUpgrade = types.BoolPointerValue(v.Attributes.AvailableForUpgrade)

	return item
}

func (d *LksVersionsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lks_versions"
}

func (d *LksVersionsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *LksVersionsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "LKS (Latitude Kubernetes Service) Kubernetes versions data source - the versions a cluster can be created with or upgraded to. This is the authoritative source for `latitudesh_lks`'s `kubernetes_version` argument; the platform rejects anything else, and a downgrade with 422 `DOWNGRADE_NOT_ALLOWED`. The lookup is global and takes no arguments — filter the list in configuration on `available_for_creation` / `available_for_upgrade`, which is why both are exposed.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Synthetic identifier. The lookup takes no arguments, so this is always `all`.",
				Computed:            true,
			},
			"default_version": schema.StringAttribute{
				MarkdownDescription: "The version the platform marks as `default`, ready to hand to `latitudesh_lks.kubernetes_version`. Null when no entry is flagged as the default.",
				Computed:            true,
			},
			"versions": schema.ListNestedAttribute{
				MarkdownDescription: "Every Kubernetes version the platform reports, in the order the API returns them. A version can be listed but unavailable for new clusters, for upgrades, or for both — check the flags rather than assuming every entry is usable.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "Version identifier.",
							Computed:            true,
						},
						"version": schema.StringAttribute{
							MarkdownDescription: "Kubernetes patch version — this is what `latitudesh_lks.kubernetes_version` takes.",
							Computed:            true,
						},
						"default": schema.BoolAttribute{
							MarkdownDescription: "Whether this is the platform's default version.",
							Computed:            true,
						},
						"available_for_creation": schema.BoolAttribute{
							MarkdownDescription: "Whether a new cluster can be created on this version.",
							Computed:            true,
						},
						"available_for_upgrade": schema.BoolAttribute{
							MarkdownDescription: "Whether an existing cluster can be upgraded to this version.",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *LksVersionsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data LksVersionsDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	// ListLksAvailableVersions takes no parameters and returns no pagination
	// metadata: one call is the whole collection.
	res, err := d.client.Lks.ListLksAvailableVersions(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to list LKS Kubernetes versions, got error: "+err.Error())
		return
	}

	data.ID = types.StringValue("all")
	data.DefaultVersion = types.StringNull()

	items := make([]LksVersionItemModel, 0)
	if res != nil && res.LksKubernetesVersions != nil {
		versions := res.LksKubernetesVersions.Data
		for i := range versions {
			item := lksVersionItemValue(&versions[i])
			// First one wins: nothing documents that exactly one entry carries
			// the flag, and silently preferring the last would be just as
			// arbitrary.
			if item.Default.ValueBool() && data.DefaultVersion.IsNull() {
				data.DefaultVersion = item.Version
			}
			items = append(items, item)
		}
	}

	list, diags := types.ListValueFrom(ctx, lksVersionItemObjectType, items)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Versions = list

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
