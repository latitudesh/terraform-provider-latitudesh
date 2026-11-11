package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

var _ resource.Resource = &ManagedDatabaseResource{}

func NewManagedDatabaseResource() resource.Resource {
	return &ManagedDatabaseResource{}
}

type ManagedDatabaseResource struct {
	client         *latitudeshgosdk.Latitudesh
	defaultProject string
}

type ManagedDatabaseResourceModel struct {
	ID             types.String   `tfsdk:"id"`
	Project        types.String   `tfsdk:"project"`
	Name           types.String   `tfsdk:"name"`
	Region         types.String   `tfsdk:"region"`
	Plan           types.String   `tfsdk:"plan"`
	Engine         types.String   `tfsdk:"engine"`
	Billing        types.String   `tfsdk:"billing"`
	TrustedSources types.Set      `tfsdk:"trusted_sources"`
	Parameters     types.Map      `tfsdk:"parameters"`
	Status         types.String   `tfsdk:"status"`
	Credentials    types.Object   `tfsdk:"credentials"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

// mdbCredentialsAttrTypes are the attributes of the computed `credentials`
// object. http_port and tcp_port from the API are ClickHouse-only and are not
// exposed.
var mdbCredentialsAttrTypes = map[string]attr.Type{
	"host":     types.StringType,
	"port":     types.Int64Type,
	"database": types.StringType,
	"username": types.StringType,
	"password": types.StringType,
	"url":      types.StringType,
}

func (r *ManagedDatabaseResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_managed_database"
}

func (r *ManagedDatabaseResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Managed database resource. Creates and manages a latitude.sh managed database (PostgreSQL).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Managed database identifier.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "The project (ID or slug) to create the database in. Optional here only if `project` is set on the provider block; one of the two is required. Changing it forces a new resource. If the resource doesn't set it and the provider's default project changes, the plan fails instead of replacing the database.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Display name of the database. Changing it forces a new resource.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Region to create the database in, e.g. `ASH`. Changing it forces a new resource.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"plan": schema.StringAttribute{
				MarkdownDescription: "Plan slug, e.g. `db.psql.small`. Changing it forces a new resource, which destroys the database's data.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"engine": schema.StringAttribute{
				MarkdownDescription: "Database engine, e.g. `postgres`. Changing it forces a new resource.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"billing": schema.StringAttribute{
				MarkdownDescription: "Billing cycle: `monthly` or `yearly`. Set at create time only; the API defaults to monthly. Changing it on an existing database is a plan-time error, because the API can't change it. To stop managing it, remove it from the configuration.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("monthly", "yearly"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"trusted_sources": schema.SetAttribute{
				MarkdownDescription: "CIDR blocks allowed to connect, e.g. `203.0.113.10/32`. Each entry must be in canonical form, with a prefix length and no host bits set. The list can't be emptied through the API. If this attribute is not set, Terraform doesn't manage the allowlist. What an unset allowlist allows is unconfirmed, so always set it.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				Validators: []validator.Set{
					trustedSourcesNotEmpty{},
					setvalidator.ValueStringsAre(canonicalCIDR{}),
				},
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
			"parameters": schema.MapAttribute{
				MarkdownDescription: "Custom PostgreSQL parameters, as strings. Each value must fall within the plan's allowed range. Changing parameters triggers a rolling restart of the database. Removing a key leaves its current value on the server. Only the keys set here are tracked.",
				ElementType:         types.StringType,
				Optional:            true,
				Validators: []validator.Map{
					mapvalidator.NoNullValues(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Database status as reported by the API.",
				Computed:            true,
			},
			"credentials": schema.SingleNestedAttribute{
				MarkdownDescription: "Connection details. `password` and `url` are sensitive. They are stored in Terraform state, so protect the state accordingly.",
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"host": schema.StringAttribute{
						MarkdownDescription: "Hostname to connect to.",
						Computed:            true,
					},
					"port": schema.Int64Attribute{
						MarkdownDescription: "Port to connect to.",
						Computed:            true,
					},
					"database": schema.StringAttribute{
						MarkdownDescription: "Database name.",
						Computed:            true,
					},
					"username": schema.StringAttribute{
						MarkdownDescription: "Username.",
						Computed:            true,
					},
					"password": schema.StringAttribute{
						MarkdownDescription: "Password.",
						Computed:            true,
						Sensitive:           true,
					},
					"url": schema.StringAttribute{
						MarkdownDescription: "Connection string, including the password.",
						Computed:            true,
						Sensitive:           true,
					},
				},
			},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{
				Create:            true,
				Update:            true,
				Delete:            true,
				CreateDescription: `Budget for the whole create: provisioning, then applying trusted_sources. Default: 60 minutes.`,
				UpdateDescription: `Budget for an update: waiting for the database to be ready, sending the change and waiting again. Default: 30 minutes.`,
				DeleteDescription: `Timeout for the database to be fully deleted. Default: 30 minutes.`,
			}),
		},
	}
}

func (r *ManagedDatabaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	resp.Diagnostics.AddError("Not implemented", "latitudesh_managed_database create is not implemented yet.")
}

func (r *ManagedDatabaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	resp.Diagnostics.AddError("Not implemented", "latitudesh_managed_database read is not implemented yet.")
}

func (r *ManagedDatabaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Not implemented", "latitudesh_managed_database update is not implemented yet.")
}

func (r *ManagedDatabaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddError("Not implemented", "latitudesh_managed_database delete is not implemented yet.")
}

// --- validators -------------------------------------------------------------

// trustedSourcesNotEmpty rejects an empty trusted_sources set. The update
// payload's trusted_sources is `omitempty`, so an empty list can't be sent, and
// what an empty allowlist means on the API side is unconfirmed. Null and
// unknown pass: null means "not managed", and unknown is decided at apply.
type trustedSourcesNotEmpty struct{}

func (v trustedSourcesNotEmpty) Description(ctx context.Context) string {
	return "must contain at least one CIDR; the allowlist cannot be cleared through the API"
}

func (v trustedSourcesNotEmpty) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v trustedSourcesNotEmpty) ValidateSet(ctx context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if len(req.ConfigValue.Elements()) == 0 {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"trusted_sources cannot be cleared through the API",
			"Set at least one CIDR, or remove trusted_sources from the configuration to stop managing the allowlist.",
		)
	}
}

// canonicalCIDR requires each trusted_sources entry to be a CIDR in the exact
// form the server would store it: a prefix length is required, host bits must
// be zero, and IPv6 must be in canonical form. A value the server would
// normalise would show a diff, and a PATCH with a rolling restart, on every
// apply. Null and unknown pass, because elements built from other resources'
// attributes are unknown until apply.
type canonicalCIDR struct{}

func (v canonicalCIDR) Description(ctx context.Context) string {
	return "must be a canonical CIDR block, e.g. 203.0.113.10/32"
}

func (v canonicalCIDR) MarkdownDescription(ctx context.Context) string {
	return "must be a canonical CIDR block, e.g. `203.0.113.10/32`"
}

func (v canonicalCIDR) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	value := req.ConfigValue.ValueString()
	_, ipnet, err := net.ParseCIDR(value)
	if err != nil {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid CIDR in trusted_sources",
			fmt.Sprintf("%q is not a CIDR block. Use an address with a prefix length, e.g. 203.0.113.10/32.", value),
		)
		return
	}
	if canonical := ipnet.String(); canonical != value {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Non-canonical CIDR in trusted_sources",
			fmt.Sprintf("%q is not in canonical form; use %q.", value, canonical),
		)
	}
}

// --- mapping ----------------------------------------------------------------

// mdbApplyAttributes maps a managed database read from the API onto data. It
// is pure, so the rules are tested without a server; readManagedDatabaseInto
// calls Show and then this.
//
// planned is a snapshot of the plan taken before Create or Update changed
// data. nil means refresh mode (Read and Import). postResp is the create
// response's attributes, non-nil only at the end of Create, where it is a
// fallback source for credentials.
//
// After an apply (planned != nil), only values that are unknown in planned are
// resolved from the API, and every value known in planned (including a
// planned null, or a set carried from state) is kept. Overwriting a known
// planned value would be "Provider produced inconsistent result after apply"
// whenever the server normalises a value or a read is stale.
//
// On refresh (planned == nil), the API value wins so real drift shows, with
// these exceptions:
//   - Selectors (project, name, region, plan, engine, billing) carry
//     RequiresReplace or can't be updated, so they keep what the practitioner
//     wrote and are filled from the API only when null or unknown (import).
//   - credentials keeps each field's prior value when the API omits it,
//     because the API may return the password only at create. This deviates
//     from the rule that Computed-only attributes go null, on purpose.
//   - trusted_sources and parameters keep their prior value when the API
//     omits them; parameters tracks only the keys already in state.
func mdbApplyAttributes(ctx context.Context, data, planned *ManagedDatabaseResourceModel, attrs, postResp *components.ManagedDatabaseAttributesAttributes, diags *diag.Diagnostics) {
	if attrs == nil {
		attrs = &components.ManagedDatabaseAttributesAttributes{}
	}
	afterApply := planned != nil

	// status: Computed-only.
	if afterApply && !planned.Status.IsUnknown() {
		data.Status = planned.Status
	} else {
		data.Status = mdbStatusValue(attrs.Status)
	}

	// Selectors.
	var planSlug, regionSlug, projectID, billing *string
	if attrs.Plan != nil {
		planSlug = attrs.Plan.Slug
		billing = attrs.Plan.Billing
	}
	if attrs.Region != nil && attrs.Region.Site != nil {
		regionSlug = attrs.Region.Site.Slug
	}
	if attrs.Project != nil {
		projectID = attrs.Project.ID
	}
	data.Name = mdbSelector(data.Name, plannedString(planned, func(m *ManagedDatabaseResourceModel) types.String { return m.Name }), attrs.Name, afterApply)
	data.Engine = mdbSelector(data.Engine, plannedString(planned, func(m *ManagedDatabaseResourceModel) types.String { return m.Engine }), attrs.Engine, afterApply)
	data.Plan = mdbSelector(data.Plan, plannedString(planned, func(m *ManagedDatabaseResourceModel) types.String { return m.Plan }), planSlug, afterApply)
	data.Region = mdbSelector(data.Region, plannedString(planned, func(m *ManagedDatabaseResourceModel) types.String { return m.Region }), regionSlug, afterApply)
	data.Project = mdbSelector(data.Project, plannedString(planned, func(m *ManagedDatabaseResourceModel) types.String { return m.Project }), projectID, afterApply)
	data.Billing = mdbSelector(data.Billing, plannedString(planned, func(m *ManagedDatabaseResourceModel) types.String { return m.Billing }), billing, afterApply)

	// trusted_sources.
	if afterApply && !planned.TrustedSources.IsUnknown() {
		data.TrustedSources = planned.TrustedSources
	} else if attrs.TrustedSources != nil {
		set, d := types.SetValueFrom(ctx, types.StringType, attrs.TrustedSources)
		diags.Append(d...)
		data.TrustedSources = set
	} else if afterApply || data.TrustedSources.IsUnknown() || data.TrustedSources.IsNull() {
		data.TrustedSources = types.SetNull(types.StringType)
	}
	// else (refresh, API omitted it): keep the prior value.

	// parameters.
	if afterApply {
		if planned.Parameters.IsUnknown() {
			data.Parameters = types.MapNull(types.StringType)
		} else {
			data.Parameters = planned.Parameters
		}
	} else {
		data.Parameters = mdbRefreshParameters(data.Parameters, attrs.Parameters, diags)
	}

	// credentials.
	var plannedCreds *types.Object
	if afterApply {
		plannedCreds = &planned.Credentials
	}
	data.Credentials = mdbCredentials(data.Credentials, plannedCreds, attrs.Credentials, postResp, diags)
}

func plannedString(planned *ManagedDatabaseResourceModel, get func(*ManagedDatabaseResourceModel) types.String) types.String {
	if planned == nil {
		return types.StringUnknown()
	}
	return get(planned)
}

// mdbSelector applies the selector rule. After an apply, a value known in the
// plan is kept, and an unknown one is resolved from the API (null if absent).
// On refresh, the current value is kept unless it is null or unknown, as on
// import.
func mdbSelector(current, planned types.String, api *string, afterApply bool) types.String {
	if afterApply {
		if !planned.IsUnknown() {
			return planned
		}
		return mdbStringValue(api)
	}
	if current.IsNull() || current.IsUnknown() {
		return mdbStringValue(api)
	}
	return current
}

func mdbStringValue(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

func mdbStatusValue(s *string) types.String {
	if s == nil || *s == "" {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

// mdbRefreshParameters keeps only the keys already tracked in state. For each
// of them it takes the API value when present, and the prior value otherwise.
// Keys the server adds (defaults, plan-derived values) are ignored, so they
// don't show as a diff on every plan.
func mdbRefreshParameters(prior types.Map, api map[string]string, diags *diag.Diagnostics) types.Map {
	if prior.IsNull() || prior.IsUnknown() {
		return types.MapNull(types.StringType)
	}
	out := make(map[string]attr.Value, len(prior.Elements()))
	for k, v := range prior.Elements() {
		if apiVal, ok := api[k]; ok {
			out[k] = types.StringValue(apiVal)
			continue
		}
		out[k] = v
	}
	m, d := types.MapValue(types.StringType, out)
	diags.Append(d...)
	return m
}

// mdbCredentials builds the credentials object field by field.
//
// When planned is non-nil (after an apply) and the planned object is known,
// it is kept: credentials carry UseStateForUnknown, so a planned known value
// is the prior state, and writing a value the GET returned after a restart
// would be an inconsistent result. The next refresh picks it up.
//
// Otherwise each field comes from the API; if the API omits it, from the
// create response (postResp); and if that omits it too, from the prior value,
// or null when the prior value is null or unknown.
func mdbCredentials(prior types.Object, planned *types.Object, api *components.ManagedDatabaseAttributesCredentials, postResp *components.ManagedDatabaseAttributesAttributes, diags *diag.Diagnostics) types.Object {
	if planned != nil && !planned.IsUnknown() {
		return *planned
	}

	var post *components.ManagedDatabaseAttributesCredentials
	if postResp != nil {
		post = postResp.Credentials
	}
	priorAttrs := map[string]attr.Value{}
	if !prior.IsNull() && !prior.IsUnknown() {
		priorAttrs = prior.Attributes()
	}

	pickString := func(name string, get func(*components.ManagedDatabaseAttributesCredentials) *string) attr.Value {
		if api != nil {
			if v := get(api); v != nil {
				return types.StringValue(*v)
			}
		}
		if post != nil {
			if v := get(post); v != nil {
				return types.StringValue(*v)
			}
		}
		if v, ok := priorAttrs[name]; ok && !v.IsUnknown() {
			return v
		}
		return types.StringNull()
	}
	pickPort := func() attr.Value {
		if api != nil && api.Port != nil {
			return types.Int64Value(*api.Port)
		}
		if post != nil && post.Port != nil {
			return types.Int64Value(*post.Port)
		}
		if v, ok := priorAttrs["port"]; ok && !v.IsUnknown() {
			return v
		}
		return types.Int64Null()
	}

	values := map[string]attr.Value{
		"host":     pickString("host", func(c *components.ManagedDatabaseAttributesCredentials) *string { return c.Host }),
		"port":     pickPort(),
		"database": pickString("database", func(c *components.ManagedDatabaseAttributesCredentials) *string { return c.Database }),
		"username": pickString("username", func(c *components.ManagedDatabaseAttributesCredentials) *string { return c.Username }),
		"password": pickString("password", func(c *components.ManagedDatabaseAttributesCredentials) *string { return c.Password }),
		"url":      pickString("url", func(c *components.ManagedDatabaseAttributesCredentials) *string { return c.URL }),
	}

	allNull := true
	for _, v := range values {
		if !v.IsNull() {
			allNull = false
			break
		}
	}
	if allNull {
		return types.ObjectNull(mdbCredentialsAttrTypes)
	}

	obj, d := types.ObjectValue(mdbCredentialsAttrTypes, values)
	diags.Append(d...)
	return obj
}

// mdbCredentialsHost returns credentials.host from an API credentials block,
// or "" when absent.
func mdbCredentialsHost(c *components.ManagedDatabaseAttributesCredentials) string {
	if c == nil || c.Host == nil {
		return ""
	}
	return *c.Host
}

// mdbParametersPayload converts the configured parameters into the request
// shape. Values are sent as strings, which is the type Show returns them in.
func mdbParametersPayload(params types.Map) map[string]any {
	if params.IsNull() || params.IsUnknown() || len(params.Elements()) == 0 {
		return nil
	}
	out := make(map[string]any, len(params.Elements()))
	for k, v := range params.Elements() {
		if s, ok := v.(types.String); ok && !s.IsNull() && !s.IsUnknown() {
			out[k] = s.ValueString()
		}
	}
	return out
}

// mdbTrustedSourcesPayload returns the configured CIDRs, sorted by the set's
// own ordering, or nil when unset.
func mdbTrustedSourcesPayload(ctx context.Context, set types.Set, diags *diag.Diagnostics) []string {
	if set.IsNull() || set.IsUnknown() {
		return nil
	}
	var out []string
	diags.Append(set.ElementsAs(ctx, &out, false)...)
	return out
}

// --- errors -----------------------------------------------------------------

// managedDatabaseErrorDetail formats an SDK error for a diagnostic without
// leaking secrets. An *APIError with a status outside 4xx/5xx (a success the
// SDK didn't expect, such as a 200 with an unexpected content type) carries
// the full success body, which includes the password, so only the status and
// the SDK's own message are shown. 4xx/5xx bodies are API error messages and
// are kept.
func managedDatabaseErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	var apiErr *components.APIError
	if errors.As(err, &apiErr) && (apiErr.StatusCode < 400 || apiErr.StatusCode >= 600) {
		return fmt.Sprintf("unexpected response (HTTP %d): %s", apiErr.StatusCode, apiErr.Message)
	}
	return err.Error()
}

// managedDatabaseRetryableDuringPoll reports whether err is worth retrying
// while polling Show: not-found right after create (not yet readable), a 429,
// or a 5xx. Anything else (403, 409, 422, ...) won't resolve by waiting.
func managedDatabaseRetryableDuringPoll(err error) bool {
	if managedDatabaseNotFound(err) || mdbRateLimited(err) {
		return true
	}
	var apiErr *components.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500 && apiErr.StatusCode < 600
	}
	return false
}

// mdbRateLimited reports whether err is a 429. Pollers retry it without
// counting it against their error budget.
func mdbRateLimited(err error) bool {
	var apiErr *components.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusTooManyRequests
}

// mdbStatusIn reports whether status, compared lower-cased and trimmed, is in
// set.
func mdbStatusIn(status string, set map[string]bool) bool {
	return set[strings.ToLower(strings.TrimSpace(status))]
}
