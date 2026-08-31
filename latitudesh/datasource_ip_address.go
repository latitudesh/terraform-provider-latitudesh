package latitudesh

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"

	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

// ipAddressExtraFields requests the lazily-loaded "region" and "server"
// attributes. Per the SDK's doc comment on IPAddressDataAttributes.Region,
// these are omitted from the response unless explicitly asked for via
// extra_fields[ip_addresses] — without this the region (and, per the same
// comment, server/assignment) attributes would silently come back null.
const ipAddressExtraFields = "region,server"

var (
	_ datasource.DataSource              = &IPAddressDataSource{}
	_ datasource.DataSourceWithConfigure = &IPAddressDataSource{}
)

func NewIPAddressDataSource() datasource.DataSource {
	return &IPAddressDataSource{}
}

type IPAddressDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

var ipAddressProjectObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":   types.StringType,
		"name": types.StringType,
	},
}

type IPAddressProjectModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

var ipAddressLocationObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":   types.StringType,
		"name": types.StringType,
		"slug": types.StringType,
	},
}

type IPAddressLocationModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	Slug types.String `tfsdk:"slug"`
}

var ipAddressRegionObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":       types.StringType,
		"name":     types.StringType,
		"location": ipAddressLocationObjectType,
	},
}

type IPAddressRegionModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Location types.Object `tfsdk:"location"`
}

// ipAddressAssignmentObjectType mirrors components.Assignment. Kept distinct
// from the top-level "server_id" selector attribute (which the caller
// supplies to filter the lookup): this is the assignment as echoed back by
// the API, always present (as an object with null fields) when the IP is not
// currently assigned to an active server.
var ipAddressAssignmentObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"server_id":   types.StringType,
		"hostname":    types.StringType,
		"assigned_at": types.StringType,
	},
}

type IPAddressAssignmentModel struct {
	ServerID   types.String `tfsdk:"server_id"`
	Hostname   types.String `tfsdk:"hostname"`
	AssignedAt types.String `tfsdk:"assigned_at"`
}

var ipAddressElasticObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":     types.StringType,
		"mode":   types.StringType,
		"status": types.StringType,
	},
}

type IPAddressElasticModel struct {
	ID     types.String `tfsdk:"id"`
	Mode   types.String `tfsdk:"mode"`
	Status types.String `tfsdk:"status"`
}

type IPAddressDataSourceModel struct {
	// Selectors (exactly one)
	ID       types.String `tfsdk:"id"`
	Address  types.String `tfsdk:"address"`
	ServerID types.String `tfsdk:"server_id"`

	// Attributes
	Cidr       types.String `tfsdk:"cidr"`
	Family     types.String `tfsdk:"family"`
	Gateway    types.String `tfsdk:"gateway"`
	Netmask    types.String `tfsdk:"netmask"`
	Type       types.String `tfsdk:"type"`
	Public     types.Bool   `tfsdk:"public"`
	Management types.Bool   `tfsdk:"management"`
	Additional types.Bool   `tfsdk:"additional"`
	Available  types.Bool   `tfsdk:"available"`
	Project    types.Object `tfsdk:"project"`
	Region     types.Object `tfsdk:"region"`
	Assignment types.Object `tfsdk:"assignment"`
	Elastic    types.Object `tfsdk:"elastic"`
	CreatedAt  types.String `tfsdk:"created_at"`
}

func ipAddressProjectValue(ctx context.Context, p *components.IPAddressDataProject) (types.Object, diag.Diagnostics) {
	if p == nil {
		return types.ObjectNull(ipAddressProjectObjectType.AttrTypes), nil
	}
	model := IPAddressProjectModel{
		ID:   types.StringPointerValue(p.ID),
		Name: types.StringPointerValue(p.Name),
	}
	return types.ObjectValueFrom(ctx, ipAddressProjectObjectType.AttrTypes, model)
}

func ipAddressLocationValue(ctx context.Context, l *components.IPAddressDataLocation) (types.Object, diag.Diagnostics) {
	if l == nil {
		return types.ObjectNull(ipAddressLocationObjectType.AttrTypes), nil
	}
	model := IPAddressLocationModel{
		ID:   types.StringPointerValue(l.ID),
		Name: types.StringPointerValue(l.Name),
		Slug: types.StringPointerValue(l.Slug),
	}
	return types.ObjectValueFrom(ctx, ipAddressLocationObjectType.AttrTypes, model)
}

func ipAddressRegionValue(ctx context.Context, r *components.IPAddressDataRegion) (types.Object, diag.Diagnostics) {
	if r == nil {
		return types.ObjectNull(ipAddressRegionObjectType.AttrTypes), nil
	}
	var diags diag.Diagnostics

	location, d := ipAddressLocationValue(ctx, r.Location)
	diags.Append(d...)

	model := IPAddressRegionModel{
		ID:       types.StringPointerValue(r.ID),
		Name:     types.StringPointerValue(r.Name),
		Location: location,
	}
	obj, d := types.ObjectValueFrom(ctx, ipAddressRegionObjectType.AttrTypes, model)
	diags.Append(d...)
	return obj, diags
}

func ipAddressAssignmentValue(ctx context.Context, a *components.Assignment) (types.Object, diag.Diagnostics) {
	if a == nil {
		return types.ObjectNull(ipAddressAssignmentObjectType.AttrTypes), nil
	}
	model := IPAddressAssignmentModel{
		ServerID:   types.StringPointerValue(a.ServerID),
		Hostname:   types.StringPointerValue(a.Hostname),
		AssignedAt: types.StringPointerValue(a.AssignedAt),
	}
	return types.ObjectValueFrom(ctx, ipAddressAssignmentObjectType.AttrTypes, model)
}

func ipAddressElasticValue(ctx context.Context, e *components.Elastic) (types.Object, diag.Diagnostics) {
	if e == nil {
		return types.ObjectNull(ipAddressElasticObjectType.AttrTypes), nil
	}
	model := IPAddressElasticModel{
		ID:     types.StringPointerValue(e.ID),
		Mode:   types.StringPointerValue(e.Mode),
		Status: types.StringPointerValue(e.Status),
	}
	return types.ObjectValueFrom(ctx, ipAddressElasticObjectType.AttrTypes, model)
}

func (d *IPAddressDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ip_address"
}

func (d *IPAddressDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *IPAddressDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "IP Address data source - lookup a management, additional or elastic IP by id, address, or the server it is assigned to.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "IP address identifier to look up.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("id"),
						path.MatchRoot("address"),
						path.MatchRoot("server_id"),
					),
				},
			},
			"address": schema.StringAttribute{
				MarkdownDescription: "Exact IP address (e.g. `192.0.2.10`) to look up.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("id"),
						path.MatchRoot("address"),
						path.MatchRoot("server_id"),
					),
				},
			},
			"server_id": schema.StringAttribute{
				MarkdownDescription: "ID of the server to look up the assigned IP for. Errors if the server has more than one IP assigned (e.g. a management IP plus an additional IP) — disambiguate with `address` in that case.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("id"),
						path.MatchRoot("address"),
						path.MatchRoot("server_id"),
					),
				},
			},

			"cidr": schema.StringAttribute{
				MarkdownDescription: "CIDR notation for the IP address.",
				Computed:            true,
			},
			"family": schema.StringAttribute{
				MarkdownDescription: "Protocol family: `IPv4` or `IPv6`.",
				Computed:            true,
			},
			"gateway": schema.StringAttribute{
				MarkdownDescription: "Gateway address.",
				Computed:            true,
			},
			"netmask": schema.StringAttribute{
				MarkdownDescription: "Netmask.",
				Computed:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "IP type: `Public`, `Private`, or `Elastic`.",
				Computed:            true,
			},
			"public": schema.BoolAttribute{
				MarkdownDescription: "Whether the IP is publicly routable.",
				Computed:            true,
			},
			"management": schema.BoolAttribute{
				MarkdownDescription: "Whether this is a management IP (born and dies with the device, never changes during its lifecycle).",
				Computed:            true,
			},
			"additional": schema.BoolAttribute{
				MarkdownDescription: "Whether this is an additional IP that can be attached to a device.",
				Computed:            true,
			},
			"available": schema.BoolAttribute{
				MarkdownDescription: "Whether the IP is unassigned and available.",
				Computed:            true,
			},
			"project": schema.SingleNestedAttribute{
				MarkdownDescription: "Project the IP belongs to.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"id": schema.StringAttribute{
						MarkdownDescription: "Project ID.",
						Computed:            true,
					},
					"name": schema.StringAttribute{
						MarkdownDescription: "Project name.",
						Computed:            true,
					},
				},
			},
			"region": schema.SingleNestedAttribute{
				MarkdownDescription: "Region the IP belongs to.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"id": schema.StringAttribute{
						MarkdownDescription: "Region ID.",
						Computed:            true,
					},
					"name": schema.StringAttribute{
						MarkdownDescription: "Region name.",
						Computed:            true,
					},
					"location": schema.SingleNestedAttribute{
						MarkdownDescription: "Site location of the region.",
						Computed:            true,
						Attributes: map[string]schema.Attribute{
							"id": schema.StringAttribute{
								MarkdownDescription: "Location ID.",
								Computed:            true,
							},
							"name": schema.StringAttribute{
								MarkdownDescription: "Location name.",
								Computed:            true,
							},
							"slug": schema.StringAttribute{
								MarkdownDescription: "Location slug.",
								Computed:            true,
							},
						},
					},
				},
			},
			"assignment": schema.SingleNestedAttribute{
				MarkdownDescription: "Server assignment. Fields are null when the IP is not currently assigned to an active server (e.g. the server is decommissioning or deleted).",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"server_id": schema.StringAttribute{
						MarkdownDescription: "ID of the server the IP is assigned to.",
						Computed:            true,
					},
					"hostname": schema.StringAttribute{
						MarkdownDescription: "Hostname of the assigned server. Null when the server has no hostname set.",
						Computed:            true,
					},
					"assigned_at": schema.StringAttribute{
						MarkdownDescription: "Timestamp when the IP was assigned.",
						Computed:            true,
					},
				},
			},
			"elastic": schema.SingleNestedAttribute{
				MarkdownDescription: "Elastic IP details. Fields are null when the IP is not an Elastic IP.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"id": schema.StringAttribute{
						MarkdownDescription: "Elastic IP identifier.",
						Computed:            true,
					},
					"mode": schema.StringAttribute{
						MarkdownDescription: "Elastic IP mode.",
						Computed:            true,
					},
					"status": schema.StringAttribute{
						MarkdownDescription: "Elastic IP status.",
						Computed:            true,
					},
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Timestamp when the IP address was created.",
				Computed:            true,
			},
		},
	}
}

func (d *IPAddressDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data IPAddressDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	// Avoid unknown selectors (e.g., from unresolved variables)
	if data.ID.IsUnknown() || data.Address.IsUnknown() || data.ServerID.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unknown selector value",
			"One of 'id', 'address', or 'server_id' is unknown. Please provide a concrete value.",
		)
		return
	}

	var ip *components.IPAddressData
	var err error

	switch {
	case !data.ID.IsNull():
		ip, err = d.getByID(ctx, data.ID.ValueString())
	case !data.Address.IsNull():
		ip, err = d.findByAddress(ctx, data.Address.ValueString())
	case !data.ServerID.IsNull():
		ip, err = d.findByServerID(ctx, data.ServerID.ValueString())
	default:
		resp.Diagnostics.AddError(
			"Missing selector",
			"Exactly one of 'id', 'address', or 'server_id' must be provided.",
		)
		return
	}

	if err != nil {
		resp.Diagnostics.AddError("Client Error", err.Error())
		return
	}
	if ip == nil {
		resp.Diagnostics.AddError("IP address not found", "No IP address matches the given selector.")
		return
	}

	diags := d.mapIPAddressToModel(ctx, ip, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *IPAddressDataSource) getByID(ctx context.Context, id string) (*components.IPAddressData, error) {
	extra := ipAddressExtraFields
	res, err := d.client.IPAddresses.Get(ctx, id, &extra)
	if err != nil {
		if apiErr, ok := err.(*components.APIError); ok && apiErr.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("unable to retrieve IP address %q: %w", id, err)
	}
	if res.IPAddress == nil || res.IPAddress.Data == nil {
		return nil, nil
	}
	return res.IPAddress.Data, nil
}

// listAllIPs paginates through every page of a List call and returns the
// concatenated results. IP lookups here are expected to match at most a
// handful of records, so buffering the full (filtered) result set in memory
// is simpler than threading pagination state through the caller.
func (d *IPAddressDataSource) listAllIPs(ctx context.Context, request operations.GetIpsRequest) ([]components.IPAddressData, error) {
	var out []components.IPAddressData

	result, err := d.client.IPAddresses.List(ctx, request)
	if err != nil {
		return nil, err
	}
	for result != nil {
		if result.IPAddresses != nil {
			out = append(out, result.IPAddresses.Data...)
		}
		if result.Next == nil {
			break
		}
		result, err = result.Next()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// findByAddress lists IPs filtered by address and returns the exact match.
// FilterAddress matches on a "starts with" basis server-side, so the exact
// comparison here is still required.
func (d *IPAddressDataSource) findByAddress(ctx context.Context, address string) (*components.IPAddressData, error) {
	extra := ipAddressExtraFields
	all, err := d.listAllIPs(ctx, operations.GetIpsRequest{
		FilterAddress:          &address,
		ExtraFieldsIPAddresses: &extra,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to search for IP address %q: %w", address, err)
	}

	for i := range all {
		ip := all[i]
		if ip.Attributes != nil && ip.Attributes.Address != nil && *ip.Attributes.Address == address {
			return &ip, nil
		}
	}
	return nil, nil
}

// findByServerID lists IPs assigned to a server. A server can carry more than
// one IP (a management IP plus additional/elastic IPs), so more than one
// match is treated as an error asking the caller to disambiguate with
// 'address' rather than silently picking one.
func (d *IPAddressDataSource) findByServerID(ctx context.Context, serverID string) (*components.IPAddressData, error) {
	extra := ipAddressExtraFields
	all, err := d.listAllIPs(ctx, operations.GetIpsRequest{
		FilterServer:           &serverID,
		ExtraFieldsIPAddresses: &extra,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to search for IP address of server %q: %w", serverID, err)
	}

	if len(all) == 0 {
		return nil, nil
	}
	if len(all) > 1 {
		return nil, fmt.Errorf("server %q has %d IP addresses assigned; use 'address' to select one", serverID, len(all))
	}
	return &all[0], nil
}

func (d *IPAddressDataSource) mapIPAddressToModel(ctx context.Context, ip *components.IPAddressData, data *IPAddressDataSourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	if ip.ID != nil {
		data.ID = types.StringValue(*ip.ID)
	}

	attrs := ip.Attributes
	if attrs == nil {
		return diags
	}

	data.Address = types.StringPointerValue(attrs.Address)
	data.Cidr = types.StringPointerValue(attrs.Cidr)
	if attrs.Family != nil {
		data.Family = types.StringValue(string(*attrs.Family))
	} else {
		data.Family = types.StringNull()
	}
	data.Gateway = types.StringPointerValue(attrs.Gateway)
	data.Netmask = types.StringPointerValue(attrs.Netmask)
	if attrs.Type != nil {
		data.Type = types.StringValue(string(*attrs.Type))
	} else {
		data.Type = types.StringNull()
	}
	data.Public = types.BoolPointerValue(attrs.Public)
	data.Management = types.BoolPointerValue(attrs.Management)
	data.Additional = types.BoolPointerValue(attrs.Additional)
	data.Available = types.BoolPointerValue(attrs.Available)

	project, dg := ipAddressProjectValue(ctx, attrs.Project)
	diags.Append(dg...)
	data.Project = project

	region, dg := ipAddressRegionValue(ctx, attrs.Region)
	diags.Append(dg...)
	data.Region = region

	assignment, dg := ipAddressAssignmentValue(ctx, attrs.Assignment)
	diags.Append(dg...)
	data.Assignment = assignment

	elastic, dg := ipAddressElasticValue(ctx, attrs.Elastic)
	diags.Append(dg...)
	data.Elastic = elastic

	data.CreatedAt = timeValue(attrs.CreatedAt)

	// server_id is a selector, not otherwise echoed back: when the lookup
	// used 'id' or 'address', leave it as whatever the caller supplied
	// (null, if unset) rather than inferring it from the assignment above.

	return diags
}
