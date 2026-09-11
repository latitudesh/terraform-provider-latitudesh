package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	"github.com/latitudesh/latitudesh-go-sdk/retry"
	"github.com/latitudesh/terraform-provider-latitudesh/v2/internal/planmodifiers"
	providerpkg "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var _ resource.Resource = &ElasticIPBgpResource{}
var _ resource.ResourceWithModifyPlan = &ElasticIPBgpResource{}
var _ resource.ResourceWithImportState = &ElasticIPBgpResource{}

func NewElasticIPBgpResource() resource.Resource {
	return &ElasticIPBgpResource{}
}

// ElasticIPBgpResource manages a BGP-mode Elastic IP: a /32 allocated to a site
// and announced over iBGP by one or more servers.
//
// It is a separate resource from latitudesh_elastic_ip (routed mode) because the
// API treats the two as distinct entities, not as two shapes of one: they have
// different id prefixes (`ueip_` vs `eip_`), a BGP address is never moved
// between servers (PATCH answers 422 BGP_ELASTIC_IP_NOT_UPDATABLE), and it
// cannot be released while any session is still up (422
// ELASTIC_IP_HAS_BGP_SESSIONS).
type ElasticIPBgpResource struct {
	client         *latitudeshgosdk.Latitudesh
	defaultProject string
}

type ElasticIPBgpResourceModel struct {
	ID           types.String   `tfsdk:"id"`
	Project      types.String   `tfsdk:"project"`
	Site         types.String   `tfsdk:"site"`
	ServerIDs    types.Set      `tfsdk:"server_ids"`
	Address      types.String   `tfsdk:"address"`
	PrefixLength types.Int64    `tfsdk:"prefix_length"`
	Status       types.String   `tfsdk:"status"`
	BgpSessions  types.List     `tfsdk:"bgp_sessions"`
	Timeouts     timeouts.Value `tfsdk:"timeouts"`
}

// bgpSessionAttrTypes is the object type of a `bgp_sessions` element. Declared
// once so the schema, the null value and every conversion agree.
var bgpSessionAttrTypes = map[string]attr.Type{
	"id":             types.StringType,
	"server_id":      types.StringType,
	"server_ip":      types.StringType,
	"peer_address":   types.StringType,
	"asn":            types.Int64Type,
	"status":         types.StringType,
	"status_message": types.StringType,
}

// bgpPollInterval is how often an operation asks the API whether it is done.
//
// It is a var only so the mock-backed tests can shrink it; production always
// uses this value. Deriving it from the remaining time would be worse than it
// sounds: a user who sets a short timeout would get a burst of requests at the
// exact moment the API is slow enough to need one.
var bgpPollInterval = 10 * time.Second

// bgpReadRetryConfig caps how long a single read may spend retrying transient
// upstream errors (the SDK retries 429 and 5xx). It overrides the provider-wide
// five-minute budget, which is far too long in both places this matters: a
// refresh would sit there instead of failing a plan Terraform will happily run
// again, and inside an operation one unlucky call could eat the entire timeout.
// Every caller either surfaces the error or polls again, so failing fast loses
// nothing.
var bgpReadRetryConfig = retry.Config{
	Strategy: "backoff",
	Backoff: &retry.BackoffStrategy{
		InitialInterval: 500,
		MaxInterval:     2000,
		Exponent:        1.5,
		MaxElapsedTime:  15000,
	},
	RetryConnectionErrors: false,
}

func bgpSessionsObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: bgpSessionAttrTypes}
}

// elasticIPBgpErrorHints maps the API's documented error codes to a diagnostic
// the operator can act on. The SDK surfaces the response body inside the error
// string, so matching on the code is how the cause is recovered.
var elasticIPBgpErrorHints = []struct {
	code   string
	title  string
	detail string
}{
	{
		"SITE_NOT_BGP_ENABLED",
		"Site does not support BGP Elastic IPs",
		"BGP is enabled per site. Pick a site where it is available, or ask support to enable it.",
	},
	{
		"SITE_NOT_SUPPORTED",
		"Site does not support Elastic IPs",
		"Elastic IPs are not available in this site at all.",
	},
	{
		"SITE_MISMATCH",
		"Server is in another site",
		"Every announcing server must live in the same site as the Elastic IP.",
	},
	{
		"SERVER_NOT_BGP_ENABLED",
		"Server was not deployed with BGP enabled",
		"Only a server deployed with `bgp_ready = true` can announce an Elastic IP. Redeploy the server with that flag.",
	},
	{
		"SERVER_NOT_BGP_CAPABLE",
		"Server cannot announce over BGP",
		"No switch could be resolved for this server, so no session can be opened. Contact support.",
	},
	{
		"SERVER_NETWORK_INCOMPATIBLE",
		"Server has an outdated network configuration",
		"The server's primary IPv4 is not a /31, which BGP announcement requires. Contact support.",
	},
	{
		"SERVER_IP_NOT_FOUND",
		"Server has no public IPv4",
		"The announcing server needs a public IPv4 address to peer from.",
	},
	{
		"SERVER_INTERFACE_NOT_FOUND",
		"Server has no ETH0 interface",
		"The announcing server has no ETH0 interface to peer over. Contact support.",
	},
	{
		"SERVER_NOT_IN_PROJECT",
		"Server belongs to another project",
		"The Elastic IP and every announcing server must be in the same project.",
	},
	{
		"BGP_SESSION_LIMIT_REACHED",
		"BGP session limit reached",
		"This Elastic IP already has the maximum number of announcing servers (5 by default). Remove one before adding another.",
	},
	{
		"ELASTIC_IP_LIMIT_REACHED",
		"Elastic IP limit reached",
		"The team has reached its Elastic IP quota. Release an existing Elastic IP or contact support.",
	},
	{
		"ELASTIC_IP_HAS_BGP_SESSIONS",
		"Elastic IP still has BGP sessions",
		"The address cannot be released while a server still announces it. This usually means a session was opened outside Terraform.",
	},
	{
		"BGP_SESSION_SETUP_INCOMPLETE",
		"BGP session setup incomplete",
		"The address was allocated but not every session could be opened. Re-run apply to open the missing ones.",
	},
	{
		"IP_ALLOCATION_UNCONFIRMED",
		"IP allocation unconfirmed",
		"The address was allocated upstream but is not visible yet. Check the Latitude.sh dashboard before retrying, so no address is left behind.",
	},
	{
		"IP_ALLOCATION_FAILED",
		"IP allocation failed",
		"No address could be allocated in this site.",
	},
}

// addElasticIPBgpError appends a named diagnostic for a known API error code,
// falling through to a generic client error.
func addElasticIPBgpError(diags *diag.Diagnostics, op string, err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	for _, hint := range elasticIPBgpErrorHints {
		if strings.Contains(msg, hint.code) {
			diags.AddError(hint.title, hint.detail+" Underlying error: "+msg)
			return
		}
	}
	diags.AddError("Client Error", "Unable to "+op+": "+msg)
}

// isElasticIPBgpNotFound reports whether an SDK error is a 404. The API answers
// with a JSON:API error document, so the status code is the reliable signal.
func isElasticIPBgpNotFound(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *components.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not_found")
}

func (r *ElasticIPBgpResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_elastic_ip_bgp"
}

func (r *ElasticIPBgpResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "BGP-mode Elastic IP. Reserves a static public IPv4 `/32` in a site and announces it over iBGP from one or more servers, so the address can be shared, failed over or load-balanced by the servers themselves. For an address that is routed to a single server, use `latitudesh_elastic_ip` instead.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Elastic IP identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "The project (ID or slug) that owns the Elastic IP. Optional here only if `project` is set on the provider block; one of the two is required. Changing it forces a new resource.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"site": schema.StringAttribute{
				MarkdownDescription: "Site slug the address is allocated in (case-insensitive, e.g. `DAL`). BGP is enabled per site, and every announcing server must be in this same site. Changing it forces a new resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					planmodifiers.CaseInsensitiveDiff{},
					stringplanmodifier.RequiresReplace(),
				},
			},
			"server_ids": schema.SetAttribute{
				MarkdownDescription: "Servers announcing this address over BGP. Each one must be deployed with `bgp_ready = true` and live in `site`. Adding or removing an ID opens or closes that server's session in place — the address is not reallocated. Up to 5 servers by default; the cap is a team limit. Omitting the attribute leaves whatever sessions exist untouched; set it to `[]` to close them all.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
			"address": schema.StringAttribute{
				MarkdownDescription: "The allocated IP address",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"prefix_length": schema.Int64Attribute{
				MarkdownDescription: "Prefix length of the allocated block (always `32` today)",
				Computed:            true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Current status of the Elastic IP (`pending`, `configuring`, `active`, `releasing`, `error`)",
				Computed:            true,
			},
			"bgp_sessions": schema.ListNestedAttribute{
				MarkdownDescription: "One entry per announcing server, ordered by `server_id`. `peer_address` is the address to configure as the BGP neighbor on the server (BIRD `neighbor`, MetalLB `peerAddress`).",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "BGP session identifier",
							Computed:            true,
						},
						"server_id": schema.StringAttribute{
							MarkdownDescription: "The announcing server",
							Computed:            true,
						},
						"server_ip": schema.StringAttribute{
							MarkdownDescription: "The server's address the session peers from",
							Computed:            true,
						},
						"peer_address": schema.StringAttribute{
							MarkdownDescription: "The other end of the announcing server's /31 — configure this as the BGP neighbor",
							Computed:            true,
						},
						"asn": schema.Int64Attribute{
							MarkdownDescription: "Autonomous system number of the session",
							Computed:            true,
						},
						"status": schema.StringAttribute{
							MarkdownDescription: "Session status (`pending`, `configuring`, `active`, `removing`, `error`)",
							Computed:            true,
						},
						"status_message": schema.StringAttribute{
							MarkdownDescription: "Explanation when the session could not be configured",
							Computed:            true,
						},
					},
				},
			},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{
				Create: true,
				Update: true,
				Delete: true,
			}),
		},
	}
}

func (r *ElasticIPBgpResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ModifyPlan resolves `project` from the provider default when the resource
// omits it, so the planned value matches what Create will send.
func (r *ElasticIPBgpResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}

	var cfg, plan ElasticIPBgpResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if cfg.Project.IsUnknown() {
		return
	}

	if !cfg.Project.IsNull() && cfg.Project.ValueString() != "" {
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

func (r *ElasticIPBgpResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ElasticIPBgpResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	effectiveProject := r.effectiveProject(data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Project = types.StringValue(effectiveProject)

	createTimeout, diagTO := data.Timeouts.Create(ctx, 15*time.Minute)
	resp.Diagnostics.Append(diagTO...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Bind the timeout to the context so it also bounds the API calls themselves,
	// not just the gaps between polls. WithDeadline keeps the earlier of the two
	// deadlines, so a shorter one from Terraform still wins.
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(createTimeout))
	defer cancel()
	deadline, _ := ctx.Deadline()

	wanted, diagsIDs := setToStrings(ctx, data.ServerIDs)
	resp.Diagnostics.Append(diagsIDs...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The address is allocated on its own and the sessions are opened one by one
	// afterwards, even though POST /elastic_ips accepts `server_ids` and would do
	// both in a single call. That call opens the sessions inside the request and,
	// if any of them fails, answers 502 BGP_SESSION_SETUP_INCOMPLETE with the /32
	// already allocated — the id never reaches the provider and the address is
	// orphaned outside state. Allocating first means the id is known before
	// anything can fail.
	site := data.Site.ValueString()
	mode := components.CreateElasticIPModeBgp
	createRequest := components.CreateElasticIP{
		Data: components.CreateElasticIPData{
			Type: components.CreateElasticIPTypeElasticIps,
			Attributes: &components.CreateElasticIPAttributes{
				Mode:      &mode,
				ProjectID: effectiveProject,
				Site:      &site,
			},
		},
	}

	result, err := r.client.ElasticIps.CreateElasticIP(ctx, createRequest)
	if err != nil {
		addElasticIPBgpError(&resp.Diagnostics, "create BGP Elastic IP", err)
		return
	}

	createdID, createdAddress := bgpCreateIdentity(result)
	switch {
	case createdID != "":
		data.ID = types.StringValue(createdID)

	case createdAddress != "":
		// No id, but the response named the address it allocated — an exact key.
		// Looking it up is immune to anything else being created concurrently.
		recovered, recErr := r.findBgpIDByAddress(ctx, effectiveProject, createdAddress)
		if recErr != nil {
			resp.Diagnostics.AddError(
				"BGP Elastic IP not found after create",
				"The create call allocated "+createdAddress+" but returned no id, and it could not be found in the project afterwards: "+
					recErr.Error()+". Find "+createdAddress+" in the Latitude.sh dashboard and either import it or release it.",
			)
			return
		}
		data.ID = types.StringValue(recovered)

	default:
		// Defensive only: no API path produces this. The bgp create renders through
		// the shared JSON:API serializer, which always emits both the id and the
		// attributes; the `id: null` shape the SDK warns about belongs to the routed
		// create, and even that carries the address (the case above). It is kept
		// because DX-155 plans to move routed mode onto this same v2 path, which
		// would bring that shape here.
		//
		// If it ever does happen, nothing ties the allocation to this request. The
		// listing could be diffed against a pre-create snapshot, but that is a
		// guess: an address allocated concurrently in the same project is
		// indistinguishable from ours, and adopting it would open sessions on
		// somebody else's /32 while ours stays billable and unmanaged. Identifying
		// it properly needs a key the API does not offer today — an idempotency key,
		// or a client-settable field to stamp the allocation with. Until then,
		// refusing and naming what to look for is the honest answer.
		resp.Diagnostics.AddError(
			"BGP Elastic IP created without an identifier",
			"The create call succeeded but returned neither an id nor an address, so the allocation cannot be identified. "+
				"Look for a BGP Elastic IP in "+site+" with no announcing servers in the Latitude.sh dashboard, and either import it or release it.",
		)
		return
	}

	r.waitForActive(ctx, data.ID.ValueString(), "creation", deadline, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		r.persistPartial(ctx, &data, &resp.State, &resp.Diagnostics)
		return
	}

	// Sessions are opened in a stable order so a partial failure is reproducible.
	sort.Strings(wanted)
	var opened []string
	for _, serverID := range wanted {
		accepted := r.openSession(ctx, data.ID.ValueString(), serverID, deadline, &resp.Diagnostics)
		if accepted {
			opened = append(opened, serverID)
		}
		if resp.Diagnostics.HasError() {
			// The address (and any session already up) exists, so it has to reach
			// state: returning without it would leak a billable /32 that Terraform
			// no longer knows about. Writing state alongside the error marks the
			// resource tainted, and the next apply replaces it cleanly. Only the
			// sessions that actually opened are recorded.
			set, setDiags := stringsToSet(opened)
			resp.Diagnostics.Append(setDiags...)
			data.ServerIDs = set
			r.persistPartial(ctx, &data, &resp.State, &resp.Diagnostics)
			return
		}
	}

	r.readInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		r.persistPartial(ctx, &data, &resp.State, &resp.Diagnostics)
		return
	}
	if data.ID.IsNull() {
		resp.Diagnostics.AddError(
			"BGP Elastic IP disappeared after successful creation",
			"The address was allocated but a subsequent read returned not_found. It may have been released by another actor.",
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ElasticIPBgpResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ElasticIPBgpResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.IsNull() {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ElasticIPBgpResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ElasticIPBgpResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Everything except server_ids is either Computed or ForceNew: a BGP address
	// is never moved (PATCH /elastic_ips answers 422 for this mode), so the only
	// update is opening and closing sessions.
	wanted, diagsWanted := setToStrings(ctx, plan.ServerIDs)
	resp.Diagnostics.Append(diagsWanted...)
	current, diagsCurrent := setToStrings(ctx, state.ServerIDs)
	resp.Diagnostics.Append(diagsCurrent...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateTimeout, diagTO := plan.Timeouts.Update(ctx, 15*time.Minute)
	resp.Diagnostics.Append(diagTO...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(updateTimeout))
	defer cancel()
	deadline, _ := ctx.Deadline()

	id := state.ID.ValueString()
	out := state
	out.Timeouts = plan.Timeouts

	// applied tracks the announcers actually in place, updated after each call
	// that lands. State is written from it rather than from the desired set, so a
	// half-finished update records exactly what happened: recording the desired
	// set would hide the remaining work from the next plan, and keeping the
	// pre-update set would make the next apply re-open a session that already
	// exists (which the API rejects with 422 CONFLICT).
	applied := append([]string(nil), current...)
	dropApplied := func(serverID string) {
		kept := applied[:0]
		for _, candidate := range applied {
			if candidate != serverID {
				kept = append(kept, candidate)
			}
		}
		applied = kept
	}
	persistApplied := func() {
		set, diags := stringsToSet(applied)
		resp.Diagnostics.Append(diags...)
		out.ServerIDs = set
		r.persistPartial(ctx, &out, &resp.State, &resp.Diagnostics)
	}

	add, remove := diffStrings(current, wanted)

	// Removals run first: swapping one announcer for another while already at the
	// per-IP session limit would otherwise fail on the add.
	if len(remove) > 0 {
		sessions, err := r.listSessions(ctx, id)
		if err != nil {
			addElasticIPBgpError(&resp.Diagnostics, "list BGP sessions", err)
			return
		}
		byServer := sessionsByServer(sessions)
		for _, serverID := range remove {
			sessionID, ok := byServer[serverID]
			if !ok {
				// Already gone upstream (the API drops sessions when their server is
				// deleted or recommissioned).
				dropApplied(serverID)
				continue
			}
			r.closeSession(ctx, id, sessionID, deadline, &resp.Diagnostics)
			if resp.Diagnostics.HasError() {
				persistApplied()
				return
			}
			dropApplied(serverID)
		}
	}

	for _, serverID := range add {
		accepted := r.openSession(ctx, id, serverID, deadline, &resp.Diagnostics)
		if accepted {
			applied = append(applied, serverID)
		}
		if resp.Diagnostics.HasError() {
			persistApplied()
			return
		}
	}

	appliedSet, appliedDiags := stringsToSet(applied)
	resp.Diagnostics.Append(appliedDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	out.ServerIDs = appliedSet

	r.readInto(ctx, &out, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		r.fillUnknowns(&out)
		resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
		return
	}
	if out.ID.IsNull() {
		resp.Diagnostics.AddError(
			"BGP Elastic IP disappeared during update",
			"The sessions were reconciled but a subsequent read returned not_found. The address may have been released by another actor.",
		)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
}

func (r *ElasticIPBgpResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ElasticIPBgpResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.ID.ValueString()
	if id == "" {
		return
	}

	deleteTimeout, diagTO := data.Timeouts.Delete(ctx, 15*time.Minute)
	resp.Diagnostics.Append(diagTO...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(deleteTimeout))
	defer cancel()
	deadline, _ := ctx.Deadline()

	// The address cannot be released while it is still announced: DELETE answers
	// 422 ELASTIC_IP_HAS_BGP_SESSIONS. Close every session first — including any
	// opened outside Terraform, which is why this works off the live list rather
	// than off state.
	sessions, err := r.listSessions(ctx, id)
	if err != nil && !isElasticIPBgpNotFound(err) {
		addElasticIPBgpError(&resp.Diagnostics, "list BGP sessions", err)
		return
	}
	for _, session := range sessions {
		if session.ID == nil {
			continue
		}
		r.closeSession(ctx, id, *session.ID, deadline, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	if _, err := r.client.ElasticIps.DeleteElasticIP(ctx, id); err != nil {
		if isElasticIPBgpNotFound(err) {
			resp.Diagnostics.AddWarning("Elastic IP Already Released", "Elastic IP was already released")
			return
		}
		addElasticIPBgpError(&resp.Diagnostics, "release BGP Elastic IP", err)
		return
	}

	r.waitForGone(ctx, id, deadline, &resp.Diagnostics)
}

func (r *ElasticIPBgpResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// effectiveProject resolves the project from the resource, then the provider
// default.
func (r *ElasticIPBgpResource) effectiveProject(data ElasticIPBgpResourceModel, diags *diag.Diagnostics) string {
	if !data.Project.IsNull() && !data.Project.IsUnknown() && data.Project.ValueString() != "" {
		return data.Project.ValueString()
	}
	if r.defaultProject != "" {
		return r.defaultProject
	}
	diags.AddError(
		"Missing project",
		"Set `project` on this resource or define a default in the provider block (provider \"latitudesh\" { project = \"...\" }).",
	)
	return ""
}

// persistPartial writes what is known about a half-built resource so an aborted
// apply still leaves the address under management. Read errors here are dropped:
// the caller already has the diagnostic that matters.
func (r *ElasticIPBgpResource) persistPartial(ctx context.Context, data *ElasticIPBgpResourceModel, state *tfsdk.State, diags *diag.Diagnostics) {
	if data.ID.IsNull() || data.ID.ValueString() == "" {
		return
	}
	// The caller's context carries the operation deadline, and the most likely
	// reason to be here is that it expired. Detach from it so the refresh still
	// gets a chance to record the truth.
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	var scratch diag.Diagnostics
	snapshot := *data
	r.readInto(refreshCtx, &snapshot, &scratch)
	if !scratch.HasError() && !snapshot.ID.IsNull() {
		*data = snapshot
	}
	r.fillUnknowns(data)
	diags.Append(state.Set(ctx, data)...)
}

// fillUnknowns turns any value still unknown into a null, so a partial state can
// be written at all: Terraform rejects unknowns once apply is over.
func (r *ElasticIPBgpResource) fillUnknowns(data *ElasticIPBgpResourceModel) {
	if data.Address.IsUnknown() {
		data.Address = types.StringNull()
	}
	if data.PrefixLength.IsUnknown() {
		data.PrefixLength = types.Int64Null()
	}
	if data.Status.IsUnknown() {
		data.Status = types.StringNull()
	}
	if data.ServerIDs.IsUnknown() {
		data.ServerIDs = types.SetNull(types.StringType)
	}
	if data.BgpSessions.IsUnknown() {
		data.BgpSessions = types.ListNull(bgpSessionsObjectType())
	}
}

// readInto refreshes the address and its sessions. A 404 nulls the id, which the
// callers read as "gone".
//
// `project` and `site` are only filled when state has nothing: the API returns
// the project id and the canonical (upper-case) site slug, and overwriting a
// config that used a project slug or a lower-case site would look like drift on
// an attribute that forces replacement.
func (r *ElasticIPBgpResource) readInto(ctx context.Context, data *ElasticIPBgpResourceModel, diags *diag.Diagnostics) {
	id := data.ID.ValueString()
	if id == "" {
		diags.AddError("Invalid ID", "Elastic IP ID is empty")
		return
	}

	resp, err := r.client.ElasticIps.GetElasticIP(ctx, id, operations.WithRetries(bgpReadRetryConfig))
	if err != nil {
		if isElasticIPBgpNotFound(err) {
			data.ID = types.StringNull()
			return
		}
		addElasticIPBgpError(diags, "read BGP Elastic IP", err)
		return
	}
	if resp == nil || resp.ElasticIP == nil || resp.ElasticIP.Data == nil || resp.ElasticIP.Data.Attributes == nil {
		// A success with an empty body is not a deletion. Dropping the id here
		// would make Terraform forget a live, billable address — only the 404
		// above means gone.
		diags.AddError(
			"API Error",
			"The API returned an empty body for BGP Elastic IP "+id+". The address was left in state; retry, and check the Latitude.sh dashboard if this persists.",
		)
		return
	}

	attrs := resp.ElasticIP.Data.Attributes
	if attrs.Address != nil {
		data.Address = types.StringValue(*attrs.Address)
	} else {
		data.Address = types.StringNull()
	}
	if attrs.PrefixLength != nil {
		data.PrefixLength = types.Int64Value(*attrs.PrefixLength)
	} else {
		data.PrefixLength = types.Int64Null()
	}
	if attrs.Status != nil {
		data.Status = types.StringValue(string(*attrs.Status))
	} else {
		data.Status = types.StringNull()
	}
	if data.Project.IsNull() || data.Project.IsUnknown() || data.Project.ValueString() == "" {
		if attrs.Project != nil && attrs.Project.ID != nil {
			data.Project = types.StringValue(*attrs.Project.ID)
		}
	}
	if data.Site.IsNull() || data.Site.IsUnknown() || data.Site.ValueString() == "" {
		if attrs.Region != nil && attrs.Region.Location != nil && attrs.Region.Location.Slug != nil {
			data.Site = types.StringValue(*attrs.Region.Location.Slug)
		}
	}

	sessions, err := r.listSessions(ctx, id)
	if err != nil {
		if isElasticIPBgpNotFound(err) {
			data.ID = types.StringNull()
			return
		}
		addElasticIPBgpError(diags, "list BGP sessions", err)
		return
	}

	serverIDs, sessionList, convDiags := sessionsToState(sessions)
	diags.Append(convDiags...)
	if diags.HasError() {
		return
	}
	data.ServerIDs = serverIDs
	data.BgpSessions = sessionList
}

// listProjectElasticIPs walks every page of a project's addresses.
//
// Reading only the first page is what makes a listing diff dangerous: an address
// that was already there but sat on a later page is absent from the baseline and
// then looks brand new. Pagination stops on an empty page or on a page that adds
// nothing — the API is free to cap page[size] below what is asked for, and a
// server that ignored page[number] would otherwise loop forever.
func (r *ElasticIPBgpResource) listProjectElasticIPs(ctx context.Context, project string, opts ...operations.Option) ([]components.ElasticIPData, error) {
	const (
		pageSize = int64(100)
		maxPages = 50
	)

	var all []components.ElasticIPData
	seen := make(map[string]struct{})

	for page := int64(1); page <= maxPages; page++ {
		size, number := pageSize, page
		callOpts := append([]operations.Option{operations.WithRetries(bgpReadRetryConfig)}, opts...)
		resp, err := r.client.ElasticIps.ListElasticIps(ctx, operations.ListElasticIpsRequest{
			FilterProject: &project,
			PageSize:      &size,
			PageNumber:    &number,
		}, callOpts...)
		if err != nil {
			return nil, err
		}
		if resp == nil || resp.ElasticIps == nil {
			return nil, fmt.Errorf("the project listing returned an empty body")
		}
		if len(resp.ElasticIps.Data) == 0 {
			return all, nil
		}

		added := 0
		for _, eip := range resp.ElasticIps.Data {
			if eip.ID == nil {
				continue
			}
			if _, dup := seen[*eip.ID]; dup {
				continue
			}
			seen[*eip.ID] = struct{}{}
			all = append(all, eip)
			added++
		}
		if added == 0 {
			return all, nil
		}
	}

	return nil, fmt.Errorf("the project has more addresses than %d pages of %d, so the listing could not be read in full", maxPages, pageSize)
}

// bgpCreateIdentity pulls whatever identifies the allocation out of a create
// response: the id if it came back, otherwise the address, which is unique and
// just as good a key.
func bgpCreateIdentity(result *operations.CreateElasticIPResponse) (id, address string) {
	if result == nil || result.ElasticIP == nil || result.ElasticIP.Data == nil {
		return "", ""
	}
	if result.ElasticIP.Data.ID != nil {
		id = *result.ElasticIP.Data.ID
	}
	if attrs := result.ElasticIP.Data.Attributes; attrs != nil && attrs.Address != nil {
		address = *attrs.Address
	}
	return id, address
}

// findBgpIDByAddress polls the project for the address with this exact value.
// Addresses are unique, so unlike a listing diff this cannot adopt an allocation
// that belongs to somebody else.
func (r *ElasticIPBgpResource) findBgpIDByAddress(ctx context.Context, project, address string) (string, error) {
	const (
		budget       = 60 * time.Second
		pollInterval = 3 * time.Second
	)

	endBy := time.Now().Add(budget)
	var lastErr error

	for {
		all, err := r.listProjectElasticIPs(ctx, project)
		if err != nil {
			lastErr = err
		} else {
			for _, eip := range all {
				if eip.ID == nil || eip.Attributes == nil || eip.Attributes.Address == nil {
					continue
				}
				if *eip.Attributes.Address == address {
					return *eip.ID, nil
				}
			}
		}

		if !time.Now().Before(endBy) {
			break
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("cancelled while looking for %s: %w", address, ctx.Err())
		case <-time.After(pollInterval):
		}
	}

	if lastErr != nil {
		return "", fmt.Errorf("%s did not appear in the project listing within %s (last list error: %w)", address, budget, lastErr)
	}
	return "", fmt.Errorf("%s did not appear in the project listing within %s", address, budget)
}

func (r *ElasticIPBgpResource) listSessions(ctx context.Context, elasticIPID string) ([]components.BgpSessionData, error) {
	resp, err := r.client.ElasticIps.ListElasticIPBgpSessions(ctx, elasticIPID, operations.WithRetries(bgpReadRetryConfig))
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.BgpSessions == nil {
		// An empty `data` list is an answer — no sessions. A missing envelope is
		// not: treating it as one would let a refresh wipe the announcers out of
		// state and have the next apply reopen sessions that are already up.
		return nil, fmt.Errorf("the BGP session listing returned an empty body")
	}
	return resp.BgpSessions.Data, nil
}

// openSession announces the address from one server and waits for the session to
// come up. It reports whether the session exists upstream, which is not the same
// as it being ready: once the API has accepted it and named it, the session is
// there even if the readiness poll then times out. The caller records state from
// this, so a timeout cannot lose a live session and have the next apply try to
// open it again (422 CONFLICT).
func (r *ElasticIPBgpResource) openSession(ctx context.Context, elasticIPID, serverID string, deadline time.Time, diags *diag.Diagnostics) (accepted bool) {
	body := components.CreateBgpSession{
		Data: components.CreateBgpSessionData{
			Type: components.CreateBgpSessionTypeBgpSessions,
			Attributes: &components.CreateBgpSessionAttributes{
				ServerID: serverID,
			},
		},
	}

	resp, err := r.client.ElasticIps.CreateElasticIPBgpSession(ctx, elasticIPID, body)
	if err != nil {
		addElasticIPBgpError(diags, "open the BGP session for server "+serverID, err)
		return false
	}
	if resp == nil || resp.BgpSession == nil || resp.BgpSession.Data == nil ||
		resp.BgpSession.Data.ID == nil || *resp.BgpSession.Data.ID == "" {
		// Without an id there is no proof the session exists. Not recording it is
		// the loud failure mode — the next apply retries and may hit CONFLICT —
		// which beats silently claiming an announcer that never came up.
		diags.AddError(
			"API Error",
			"The BGP session for server "+serverID+" was accepted but the response carried no id.",
		)
		return false
	}

	r.waitForSession(ctx, elasticIPID, *resp.BgpSession.Data.ID, serverID, deadline, diags)
	return true
}

// closeSession stops one server from announcing the address and waits for the
// session to disappear.
func (r *ElasticIPBgpResource) closeSession(ctx context.Context, elasticIPID, sessionID string, deadline time.Time, diags *diag.Diagnostics) {
	if _, err := r.client.ElasticIps.DeleteElasticIPBgpSession(ctx, elasticIPID, sessionID, nil); err != nil {
		if isElasticIPBgpNotFound(err) {
			return
		}
		addElasticIPBgpError(diags, "close BGP session "+sessionID, err)
		return
	}

	pollInterval := bgpPollInterval
	for time.Now().Before(deadline) {
		_, err := r.client.ElasticIps.GetElasticIPBgpSession(ctx, elasticIPID, sessionID)
		if isElasticIPBgpNotFound(err) {
			return
		}
		if err != nil && !isTransientElasticIPBgpError(err) {
			addElasticIPBgpError(diags, "poll BGP session "+sessionID, err)
			return
		}
		select {
		case <-ctx.Done():
			if elasticIPBgpTimedOut(ctx) {
				closeTimeout(diags, sessionID)
				return
			}
			diags.AddError("Cancelled", "Cancelled while waiting for BGP session "+sessionID+" to be removed")
			return
		case <-time.After(pollInterval):
		}
	}

	closeTimeout(diags, sessionID)
}

// closeTimeout is the one wording for "the session is still there".
func closeTimeout(diags *diag.Diagnostics, sessionID string) {
	diags.AddError(
		"Timeout",
		"BGP session "+sessionID+" was still present when the timeout expired. The Elastic IP cannot be released until it is gone.",
	)
}

// waitForSession polls one session until it is announcing.
func (r *ElasticIPBgpResource) waitForSession(ctx context.Context, elasticIPID, sessionID, serverID string, deadline time.Time, diags *diag.Diagnostics) {
	pollInterval := bgpPollInterval
	consecutiveErrors := 0

	for time.Now().Before(deadline) {
		resp, err := r.client.ElasticIps.GetElasticIPBgpSession(ctx, elasticIPID, sessionID)
		if err != nil {
			consecutiveErrors++
			if isTransientElasticIPBgpError(err) && consecutiveErrors <= 5 {
				select {
				case <-ctx.Done():
					if elasticIPBgpTimedOut(ctx) {
						sessionTimeout(diags, serverID)
						return
					}
					diags.AddError("Cancelled", "Cancelled while waiting for the BGP session on server "+serverID)
					return
				case <-time.After(pollInterval):
					continue
				}
			}
			addElasticIPBgpError(diags, "poll the BGP session on server "+serverID, err)
			return
		}
		consecutiveErrors = 0

		if resp == nil || resp.BgpSession == nil || resp.BgpSession.Data == nil || resp.BgpSession.Data.Attributes == nil {
			diags.AddError("API Error", "BGP session response is empty for server "+serverID)
			return
		}

		attrs := resp.BgpSession.Data.Attributes
		if attrs.Status == nil {
			diags.AddError("API Error", "BGP session status is null for server "+serverID)
			return
		}

		switch *attrs.Status {
		case components.BgpSessionDataStatusActive:
			return
		case components.BgpSessionDataStatusError:
			detail := "The BGP session on server " + serverID + " entered the error state."
			if attrs.StatusMessage != nil && *attrs.StatusMessage != "" {
				detail += " " + *attrs.StatusMessage
			}
			diags.AddError("BGP session failed", detail)
			return
		}

		select {
		case <-ctx.Done():
			if elasticIPBgpTimedOut(ctx) {
				sessionTimeout(diags, serverID)
				return
			}
			diags.AddError("Cancelled", "Cancelled while waiting for the BGP session on server "+serverID)
			return
		case <-time.After(pollInterval):
		}
	}

	sessionTimeout(diags, serverID)
}

// sessionTimeout is the one wording for "the session never came up", shared by
// the deadline and the loop exit.
func sessionTimeout(diags *diag.Diagnostics, serverID string) {
	diags.AddError(
		"Timeout",
		"The BGP session on server "+serverID+" did not become active before the timeout expired.",
	)
}

// waitForActive polls the address until it is allocated. Allocation is
// synchronous today, so this normally returns on the first call; it exists for
// the `pending` and `configuring` states the API can still report.
func (r *ElasticIPBgpResource) waitForActive(ctx context.Context, id, op string, deadline time.Time, diags *diag.Diagnostics) {
	pollInterval := bgpPollInterval
	consecutiveErrors := 0

	for time.Now().Before(deadline) {
		resp, err := r.client.ElasticIps.GetElasticIP(ctx, id)
		if err != nil {
			consecutiveErrors++
			if isTransientElasticIPBgpError(err) && consecutiveErrors <= 5 {
				select {
				case <-ctx.Done():
					if elasticIPBgpTimedOut(ctx) {
						activeTimeout(diags, op)
						return
					}
					diags.AddError("Cancelled", "Elastic IP "+op+" cancelled while polling")
					return
				case <-time.After(pollInterval):
					continue
				}
			}
			addElasticIPBgpError(diags, "poll "+op+" status", err)
			return
		}
		consecutiveErrors = 0

		if resp == nil || resp.ElasticIP == nil || resp.ElasticIP.Data == nil || resp.ElasticIP.Data.Attributes == nil {
			diags.AddError("API Error", "Elastic IP response is empty during "+op)
			return
		}
		status := resp.ElasticIP.Data.Attributes.Status
		if status == nil {
			diags.AddError("API Error", "Elastic IP status is null during "+op)
			return
		}

		switch *status {
		case components.StatusActive:
			return
		case components.StatusError:
			diags.AddError(
				"Elastic IP entered error state",
				"The Elastic IP reached status `error` during "+op+". Check the Latitude.sh dashboard.",
			)
			return
		}

		select {
		case <-ctx.Done():
			if elasticIPBgpTimedOut(ctx) {
				activeTimeout(diags, op)
				return
			}
			diags.AddError("Cancelled", "Elastic IP "+op+" cancelled while polling")
			return
		case <-time.After(pollInterval):
		}
	}

	activeTimeout(diags, op)
}

// activeTimeout is the one wording for "the address never became active".
func activeTimeout(diags *diag.Diagnostics, op string) {
	diags.AddError("Timeout", "Elastic IP did not become active during "+op+" before the timeout expired.")
}

// waitForGone polls until the released address 404s.
func (r *ElasticIPBgpResource) waitForGone(ctx context.Context, id string, deadline time.Time, diags *diag.Diagnostics) {
	pollInterval := bgpPollInterval

	for time.Now().Before(deadline) {
		_, err := r.client.ElasticIps.GetElasticIP(ctx, id)
		if isElasticIPBgpNotFound(err) {
			return
		}
		select {
		case <-ctx.Done():
			if elasticIPBgpTimedOut(ctx) {
				diags.AddError("Timeout", "Elastic IP was still present when the delete timeout expired.")
				return
			}
			diags.AddError("Cancelled", "Elastic IP delete cancelled while polling")
			return
		case <-time.After(pollInterval):
		}
	}

	diags.AddError("Timeout", "Elastic IP was still present when the delete timeout expired.")
}

// stopped reports how a wait ended when its context finished: now that the
// operation timeout is bound to the context, the deadline usually fires before
// the loop notices, and reporting that as "cancelled" would send the reader
// looking for an interrupted apply instead of a timeout.
func elasticIPBgpTimedOut(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.DeadlineExceeded)
}

func isTransientElasticIPBgpError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *components.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500
	}
	msg := err.Error()
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "connection reset")
}

// sessionsToState converts the live session list into the `server_ids` set and
// the `bgp_sessions` list, ordered by server id so the list is stable across
// reads.
func sessionsToState(sessions []components.BgpSessionData) (types.Set, types.List, diag.Diagnostics) {
	var diags diag.Diagnostics

	type row struct {
		serverID string
		value    attr.Value
	}
	rows := make([]row, 0, len(sessions))
	serverIDs := make([]attr.Value, 0, len(sessions))

	for _, session := range sessions {
		attrs := session.Attributes
		if attrs == nil {
			continue
		}
		serverID := ""
		if attrs.Server != nil && attrs.Server.ID != nil {
			serverID = *attrs.Server.ID
		}

		obj, objDiags := types.ObjectValue(bgpSessionAttrTypes, map[string]attr.Value{
			"id":             nullableString(session.ID),
			"server_id":      nullableString(&serverID),
			"server_ip":      nullableString(attrs.ServerIP),
			"peer_address":   nullableString(attrs.PeerAddress),
			"asn":            nullableInt64(attrs.Asn),
			"status":         nullableSessionStatus(attrs.Status),
			"status_message": nullableString(attrs.StatusMessage),
		})
		diags.Append(objDiags...)
		if diags.HasError() {
			return types.SetNull(types.StringType), types.ListNull(bgpSessionsObjectType()), diags
		}

		rows = append(rows, row{serverID: serverID, value: obj})

		// A session whose announcer could not be resolved to a server record is
		// still reported, but it cannot go in server_ids: an empty id would show up
		// as a phantom announcer the config can never match.
		if serverID != "" {
			serverIDs = append(serverIDs, types.StringValue(serverID))
		}
	}

	sort.SliceStable(rows, func(i, j int) bool { return rows[i].serverID < rows[j].serverID })
	values := make([]attr.Value, 0, len(rows))
	for _, r := range rows {
		values = append(values, r.value)
	}

	set, setDiags := types.SetValue(types.StringType, serverIDs)
	diags.Append(setDiags...)
	list, listDiags := types.ListValue(bgpSessionsObjectType(), values)
	diags.Append(listDiags...)

	return set, list, diags
}

func nullableString(value *string) attr.Value {
	if value == nil || *value == "" {
		return types.StringNull()
	}
	return types.StringValue(*value)
}

func nullableInt64(value *int64) attr.Value {
	if value == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*value)
}

func nullableSessionStatus(value *components.BgpSessionDataStatus) attr.Value {
	if value == nil {
		return types.StringNull()
	}
	return types.StringValue(string(*value))
}

// sessionsByServer indexes live sessions by the server announcing them.
func sessionsByServer(sessions []components.BgpSessionData) map[string]string {
	byServer := make(map[string]string, len(sessions))
	for _, session := range sessions {
		if session.ID == nil || session.Attributes == nil ||
			session.Attributes.Server == nil || session.Attributes.Server.ID == nil {
			continue
		}
		byServer[*session.Attributes.Server.ID] = *session.ID
	}
	return byServer
}

// stringsToSet builds a set value, sorting first so state stays stable.
func stringsToSet(values []string) (types.Set, diag.Diagnostics) {
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	elements := make([]attr.Value, 0, len(sorted))
	for _, value := range sorted {
		elements = append(elements, types.StringValue(value))
	}
	return types.SetValue(types.StringType, elements)
}

func setToStrings(ctx context.Context, set types.Set) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics
	if set.IsNull() || set.IsUnknown() {
		return nil, diags
	}
	var out []string
	diags.Append(set.ElementsAs(ctx, &out, false)...)
	return out, diags
}

// diffStrings returns what `wanted` adds to `current` and what it drops, both in
// a stable order.
func diffStrings(current, wanted []string) (add, remove []string) {
	inCurrent := make(map[string]struct{}, len(current))
	for _, value := range current {
		inCurrent[value] = struct{}{}
	}
	inWanted := make(map[string]struct{}, len(wanted))
	for _, value := range wanted {
		inWanted[value] = struct{}{}
	}

	for _, value := range wanted {
		if _, ok := inCurrent[value]; !ok {
			add = append(add, value)
		}
	}
	for _, value := range current {
		if _, ok := inWanted[value]; !ok {
			remove = append(remove, value)
		}
	}

	sort.Strings(add)
	sort.Strings(remove)
	return add, remove
}
