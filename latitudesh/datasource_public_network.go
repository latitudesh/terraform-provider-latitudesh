package latitudesh

import (
	"context"
	"fmt"

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

var _ datasource.DataSource = &PublicNetworkDataSource{}

func NewPublicNetworkDataSource() datasource.DataSource {
	return &PublicNetworkDataSource{}
}

type PublicNetworkDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type PublicNetworkDataSourceModel struct {
	ID         types.String `tfsdk:"id"`
	Project    types.String `tfsdk:"project"`
	Site       types.String `tfsdk:"site"`
	Ipv4       types.String `tfsdk:"ipv4"`
	Ipv6       types.String `tfsdk:"ipv6"`
	Size       types.Int64  `tfsdk:"size"`
	Activated  types.Bool   `tfsdk:"activated"`
	Capacity   types.Int64  `tfsdk:"capacity"`
	IpsUsed    types.Int64  `tfsdk:"ips_used"`
	IpsFree    types.Int64  `tfsdk:"ips_free"`
	CreatedAt  types.String `tfsdk:"created_at"`
	RegionSlug types.String `tfsdk:"region_slug"`
}

func (d *PublicNetworkDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_public_network"
}

func (d *PublicNetworkDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Public Network data source - look up a public network by id, or by project and/or site.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Public network ID to look up. Mutually exclusive with `project`/`site` based lookup.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.ConflictsWith(
						path.MatchRoot("project"),
						path.MatchRoot("site"),
					),
				},
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "The project (ID or slug) to filter by. Used together with `site` (or alone) when `id` is not set.",
				Optional:            true,
				Computed:            true,
			},
			"site": schema.StringAttribute{
				MarkdownDescription: "The site slug to filter by. Used together with `project` (or alone) when `id` is not set.",
				Optional:            true,
			},
			"ipv4": schema.StringAttribute{
				MarkdownDescription: "The IPv4 network in CIDR notation (e.g., \"203.0.113.0/27\")",
				Computed:            true,
			},
			"ipv6": schema.StringAttribute{
				MarkdownDescription: "The paired IPv6 /64 in CIDR notation (e.g., \"2001:db8:1234::/64\")",
				Computed:            true,
			},
			"size": schema.Int64Attribute{
				MarkdownDescription: "IPv4 prefix length. Determines how many servers the public network can host.",
				Computed:            true,
			},
			"activated": schema.BoolAttribute{
				MarkdownDescription: "True once a server has been added to the public network and the public network is ready to use.",
				Computed:            true,
			},
			"capacity": schema.Int64Attribute{
				MarkdownDescription: "Number of servers this public network can host.",
				Computed:            true,
			},
			"ips_used": schema.Int64Attribute{
				MarkdownDescription: "Number of IPs currently in use on this public network.",
				Computed:            true,
			},
			"ips_free": schema.Int64Attribute{
				MarkdownDescription: "Number of IPs still available on this public network.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "The timestamp for when the public network was created.",
				Computed:            true,
			},
			"region_slug": schema.StringAttribute{
				MarkdownDescription: "The slug of the location backing this public network.",
				Computed:            true,
			},
		},
	}
}

func (d *PublicNetworkDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *PublicNetworkDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data PublicNetworkDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.IsUnknown() || data.Project.IsUnknown() || data.Site.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unknown selector value",
			"One of 'id', 'project', or 'site' is unknown. Please provide a concrete value.",
		)
		return
	}

	var found *components.PublicNetworkData

	switch {
	case !data.ID.IsNull() && data.ID.ValueString() != "":
		net, err := d.getByID(ctx, data.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Client Error", "Unable to read public network, got error: "+err.Error())
			return
		}
		found = net
	case (!data.Project.IsNull() && data.Project.ValueString() != "") || (!data.Site.IsNull() && data.Site.ValueString() != ""):
		var projectFilter, siteFilter *string
		if !data.Project.IsNull() && data.Project.ValueString() != "" {
			p := data.Project.ValueString()
			projectFilter = &p
		}
		if !data.Site.IsNull() && data.Site.ValueString() != "" {
			s := data.Site.ValueString()
			siteFilter = &s
		}

		matches, err := d.listByFilter(ctx, projectFilter, siteFilter)
		if err != nil {
			resp.Diagnostics.AddError("Client Error", "Unable to list public networks, got error: "+err.Error())
			return
		}
		switch len(matches) {
		case 0:
			resp.Diagnostics.AddError("Not Found", "No public network found matching the given project/site filter")
			return
		case 1:
			found = &matches[0]
		default:
			resp.Diagnostics.AddError(
				"Ambiguous Lookup",
				fmt.Sprintf("Found %d public networks matching the given filter; narrow the lookup with `site` or `id`", len(matches)),
			)
			return
		}
	default:
		resp.Diagnostics.AddError(
			"Missing selector",
			"Set `id`, or `project` and/or `site`, to look up a public network.",
		)
		return
	}

	if found == nil {
		resp.Diagnostics.AddError("Not Found", "No matching public network found")
		return
	}

	if found.ID != nil {
		data.ID = types.StringValue(*found.ID)
	}

	computed := mapPublicNetworkAttributes(found.Attributes)
	data.Ipv4 = computed.Ipv4
	data.Ipv6 = computed.Ipv6
	data.Size = computed.Size
	data.Activated = computed.Activated
	data.Capacity = computed.Capacity
	data.IpsUsed = computed.IpsUsed
	data.IpsFree = computed.IpsFree
	data.CreatedAt = computed.CreatedAt
	data.RegionSlug = computed.RegionSlug
	// Keep the configured selector (ID or slug) when one was given; only a
	// lookup by `id` (or by `site` alone) needs the API's label filled in.
	if data.Project.IsNull() || data.Project.IsUnknown() {
		data.Project = computed.Project
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *PublicNetworkDataSource) getByID(ctx context.Context, id string) (*components.PublicNetworkData, error) {
	res, err := d.client.PublicNetworks.GetPublicNetwork(ctx, id)
	if err != nil {
		return nil, err
	}
	if res == nil || res.PublicNetwork == nil || res.PublicNetwork.Data == nil {
		return nil, nil
	}
	return res.PublicNetwork.Data, nil
}

func (d *PublicNetworkDataSource) listByFilter(ctx context.Context, project, site *string) ([]components.PublicNetworkData, error) {
	res, err := d.client.PublicNetworks.GetPublicNetworks(ctx, project, site)
	if err != nil {
		return nil, err
	}
	if res == nil || res.PublicNetworks == nil {
		return nil, nil
	}
	return res.PublicNetworks.Data, nil
}
