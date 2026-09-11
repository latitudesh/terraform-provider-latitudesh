package latitudesh

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	providerpkg "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var _ resource.Resource = &PublicNetworkResource{}
var _ resource.ResourceWithImportState = &PublicNetworkResource{}
var _ resource.ResourceWithModifyPlan = &PublicNetworkResource{}

func NewPublicNetworkResource() resource.Resource {
	return &PublicNetworkResource{}
}

type PublicNetworkResource struct {
	client         *latitudeshgosdk.Latitudesh
	defaultProject string
}

type PublicNetworkResourceModel struct {
	ID         types.String `tfsdk:"id"`
	Project    types.String `tfsdk:"project"`
	Site       types.String `tfsdk:"site"`
	Size       types.Int64  `tfsdk:"size"`
	Ipv4       types.String `tfsdk:"ipv4"`
	Ipv6       types.String `tfsdk:"ipv6"`
	Activated  types.Bool   `tfsdk:"activated"`
	Capacity   types.Int64  `tfsdk:"capacity"`
	IpsUsed    types.Int64  `tfsdk:"ips_used"`
	IpsFree    types.Int64  `tfsdk:"ips_free"`
	CreatedAt  types.String `tfsdk:"created_at"`
	RegionSlug types.String `tfsdk:"region_slug"`
}

// publicNetworkComputedAttrs holds the fields read back from the API that are
// never accepted as input, shared between the resource and data source so a
// nil-check fix only has to happen once.
type publicNetworkComputedAttrs struct {
	Ipv4       types.String
	Ipv6       types.String
	Size       types.Int64
	Activated  types.Bool
	Capacity   types.Int64
	IpsUsed    types.Int64
	IpsFree    types.Int64
	CreatedAt  types.String
	Project    types.String
	RegionSlug types.String
}

// mapPublicNetworkAttributes converts the SDK's attributes envelope into
// framework values, nil-checking every pointer field per house convention.
func mapPublicNetworkAttributes(attrs *components.PublicNetworkDataAttributes) publicNetworkComputedAttrs {
	var out publicNetworkComputedAttrs

	if attrs == nil {
		out.Ipv4 = types.StringNull()
		out.Ipv6 = types.StringNull()
		out.Size = types.Int64Null()
		out.Activated = types.BoolNull()
		out.Capacity = types.Int64Null()
		out.IpsUsed = types.Int64Null()
		out.IpsFree = types.Int64Null()
		out.CreatedAt = types.StringNull()
		out.Project = types.StringNull()
		out.RegionSlug = types.StringNull()
		return out
	}

	if attrs.Ipv4 != nil {
		out.Ipv4 = types.StringValue(*attrs.Ipv4)
	} else {
		out.Ipv4 = types.StringNull()
	}

	if attrs.Ipv6 != nil {
		out.Ipv6 = types.StringValue(*attrs.Ipv6)
	} else {
		out.Ipv6 = types.StringNull()
	}

	if attrs.Size != nil {
		out.Size = types.Int64Value(int64(*attrs.Size))
	} else {
		out.Size = types.Int64Null()
	}

	if attrs.Activated != nil {
		out.Activated = types.BoolValue(*attrs.Activated)
	} else {
		out.Activated = types.BoolNull()
	}

	if attrs.Capacity != nil {
		out.Capacity = types.Int64Value(*attrs.Capacity)
	} else {
		out.Capacity = types.Int64Null()
	}

	if attrs.IpsUsed != nil {
		out.IpsUsed = types.Int64Value(*attrs.IpsUsed)
	} else {
		out.IpsUsed = types.Int64Null()
	}

	if attrs.IpsFree != nil {
		out.IpsFree = types.Int64Value(*attrs.IpsFree)
	} else {
		out.IpsFree = types.Int64Null()
	}

	if attrs.CreatedAt != nil {
		out.CreatedAt = types.StringValue(attrs.CreatedAt.Format(time.RFC3339))
	} else {
		out.CreatedAt = types.StringNull()
	}

	// Prefer the slug: it is what practitioners tend to write in `project` and
	// what `terraform plan -generate-config-out` should emit after an import.
	// Callers only apply this when `project` is not already set, so a configured
	// ID (or slug) is never overwritten with the API's label.
	switch {
	case attrs.Project != nil && attrs.Project.Slug != nil && *attrs.Project.Slug != "":
		out.Project = types.StringValue(*attrs.Project.Slug)
	case attrs.Project != nil && attrs.Project.ID != nil:
		out.Project = types.StringValue(*attrs.Project.ID)
	default:
		out.Project = types.StringNull()
	}

	if attrs.Region != nil && attrs.Region.Location != nil && attrs.Region.Location.Slug != nil {
		out.RegionSlug = types.StringValue(*attrs.Region.Location.Slug)
	} else {
		out.RegionSlug = types.StringNull()
	}

	return out
}

func (r *PublicNetworkResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_public_network"
}

func (r *PublicNetworkResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Public Network resource. Provisions a customer network: an IPv4 block of the chosen size plus a paired IPv6 /64.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Public Network identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "The project (ID or slug) to create the public network in. Optional here only if `project` is set on the provider block; one of the two is required. Changing it forces a new resource.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"site": schema.StringAttribute{
				MarkdownDescription: "The site slug the public network is bound to. Changing this value forces a new resource; there is no update endpoint.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"size": schema.Int64Attribute{
				MarkdownDescription: "IPv4 prefix length. Determines how many servers the public network can host. One of 26, 27, 28, 29. Changing this value forces a new resource; there is no update endpoint.",
				Required:            true,
				Validators: []validator.Int64{
					int64validator.OneOf(26, 27, 28, 29),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"ipv4": schema.StringAttribute{
				MarkdownDescription: "The IPv4 network in CIDR notation (e.g., \"203.0.113.0/27\")",
				Computed:            true,
			},
			"ipv6": schema.StringAttribute{
				MarkdownDescription: "The paired IPv6 /64 in CIDR notation (e.g., \"2001:db8:1234::/64\")",
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

func (r *PublicNetworkResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := providerpkg.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.client = deps.Client
	r.defaultProject = deps.DefaultProject
}

func (r *PublicNetworkResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy
	}

	var cfg, plan PublicNetworkResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if cfg.Project.IsUnknown() {
		return
	}

	if !cfg.Project.IsNull() && !cfg.Project.IsUnknown() && cfg.Project.ValueString() != "" {
		plan.Project = cfg.Project
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}

	if r.defaultProject != "" {
		plan.Project = types.StringValue(r.defaultProject)
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}

	resp.Diagnostics.AddError(
		"Missing project",
		"Set `project` on this resource or define a default in the provider block (provider `latitudesh` { project = \"...\" }).",
	)
}

func (r *PublicNetworkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data PublicNetworkResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var effectiveProject string
	if !data.Project.IsNull() && !data.Project.IsUnknown() && data.Project.ValueString() != "" {
		effectiveProject = data.Project.ValueString()
	} else if r.defaultProject != "" {
		effectiveProject = r.defaultProject
	}
	if effectiveProject == "" {
		resp.Diagnostics.AddError(
			"Missing project",
			"Set `project` on this resource or define a default in the provider block (provider \"latitudesh\" { project = \"...\" }).",
		)
		return
	}
	data.Project = types.StringValue(effectiveProject)

	createRequest := components.CreatePublicNetwork{
		Data: components.CreatePublicNetworkData{
			Type: components.CreatePublicNetworkTypePublicNetworks,
			Attributes: &components.CreatePublicNetworkAttributes{
				ProjectID: effectiveProject,
				Site:      data.Site.ValueString(),
				Size:      components.CreatePublicNetworkSize(data.Size.ValueInt64()),
			},
		},
	}

	result, err := r.client.PublicNetworks.CreatePublicNetwork(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create public network, got error: "+err.Error())
		return
	}

	if result == nil || result.PublicNetwork == nil || result.PublicNetwork.Data == nil || result.PublicNetwork.Data.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get public network ID from response")
		return
	}

	data.ID = types.StringValue(*result.PublicNetwork.Data.ID)

	computed := mapPublicNetworkAttributes(result.PublicNetwork.Data.Attributes)
	data.Ipv4 = computed.Ipv4
	data.Ipv6 = computed.Ipv6
	data.Activated = computed.Activated
	data.Capacity = computed.Capacity
	data.IpsUsed = computed.IpsUsed
	data.IpsFree = computed.IpsFree
	data.CreatedAt = computed.CreatedAt
	data.RegionSlug = computed.RegionSlug

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *PublicNetworkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data PublicNetworkResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readPublicNetworkInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.IsNull() {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update only ever runs for changes to attributes without RequiresReplace;
// every input attribute here is RequiresReplace, so this just re-reads.
func (r *PublicNetworkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data PublicNetworkResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readPublicNetworkInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *PublicNetworkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data PublicNetworkResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.ID.ValueString()
	if id == "" {
		return
	}

	_, err := r.client.PublicNetworks.DestroyPublicNetwork(ctx, id)
	if err != nil {
		if publicNetworkNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to delete public network, got error: "+err.Error())
		return
	}
}

func (r *PublicNetworkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var data PublicNetworkResourceModel
	data.ID = types.StringValue(req.ID)

	r.readPublicNetworkInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.IsNull() {
		resp.Diagnostics.AddError("Not Found", "Public network "+req.ID+" not found")
		return
	}

	if data.Site.IsNull() {
		resp.Diagnostics.AddWarning(
			"Site could not be determined",
			"The API returned no region for public network "+req.ID+", so `site` was left unset. "+
				"`site` forces replacement, so the first plan will propose replacing the network; "+
				"add `lifecycle { ignore_changes = [site] }` to adopt it as-is.",
		)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// readPublicNetworkInto issues a Get for data.ID and refreshes every computed
// attribute. The inputs — `project`, `site` and `size` — are only filled in
// when not already set, i.e. during import. All three are RequiresReplace
// with no update endpoint to drift against, and the API labels `project`
// differently from what a practitioner may have written (slug vs ID), so
// echoing them back over a configured value would yield a spurious
// "inconsistent result after apply" or a needless replacement.
func (r *PublicNetworkResource) readPublicNetworkInto(ctx context.Context, data *PublicNetworkResourceModel, diags *diag.Diagnostics) {
	id := data.ID.ValueString()
	if id == "" {
		diags.AddError("Invalid ID", "Public network ID is empty")
		return
	}

	result, err := r.client.PublicNetworks.GetPublicNetwork(ctx, id)
	if err != nil {
		if publicNetworkNotFound(err) {
			data.ID = types.StringNull()
			return
		}
		diags.AddError("Client Error", "Unable to read public network, got error: "+err.Error())
		return
	}

	if result == nil || result.PublicNetwork == nil || result.PublicNetwork.Data == nil {
		data.ID = types.StringNull()
		return
	}

	if result.PublicNetwork.Data.ID != nil {
		data.ID = types.StringValue(*result.PublicNetwork.Data.ID)
	}

	computed := mapPublicNetworkAttributes(result.PublicNetwork.Data.Attributes)
	data.Ipv4 = computed.Ipv4
	data.Ipv6 = computed.Ipv6
	data.Activated = computed.Activated
	data.Capacity = computed.Capacity
	data.IpsUsed = computed.IpsUsed
	data.IpsFree = computed.IpsFree
	data.CreatedAt = computed.CreatedAt
	data.RegionSlug = computed.RegionSlug

	if data.Project.IsNull() || data.Project.IsUnknown() {
		data.Project = computed.Project
	}
	if data.Site.IsNull() || data.Site.IsUnknown() {
		// The API has no `site` field; the location slug under `region` is the
		// same slug practitioners pass as `site` (e.g. "CHI", "LAX2").
		data.Site = computed.RegionSlug
	}
	if data.Size.IsNull() || data.Size.IsUnknown() {
		data.Size = computed.Size
	}
}

// publicNetworkNotFound reports whether err is a 404 from the public network
// endpoints. GetPublicNetwork and DestroyPublicNetwork declare typed 403/404
// responses, so the SDK returns a *components.ErrorObject (JSON:API errors,
// status "404") rather than the generic *components.APIError; both shapes
// must be recognized, and only a 404 counts — a 403 is a real error.
func publicNetworkNotFound(err error) bool {
	var apiErr *components.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}

	var errObj *components.ErrorObject
	if errors.As(err, &errObj) {
		for _, e := range errObj.Errors {
			if e.Status != nil && *e.Status == "404" {
				return true
			}
		}
	}

	return false
}
