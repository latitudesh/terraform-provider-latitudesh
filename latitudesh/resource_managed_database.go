package latitudesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

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
	"github.com/hashicorp/terraform-plugin-log/tflog"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	"github.com/latitudesh/latitudesh-go-sdk/retry"
	providerpkg "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var _ resource.Resource = &ManagedDatabaseResource{}
var _ resource.ResourceWithConfigure = &ManagedDatabaseResource{}
var _ resource.ResourceWithImportState = &ManagedDatabaseResource{}

// Poll intervals and the polling error budget. Declared as vars so tests can
// shorten them; production keeps the defaults.
var (
	mdbReadyPollInterval  = 15 * time.Second
	mdbDeletePollInterval = 10 * time.Second
	// mdbErrorBudget is how long a poller tolerates consecutive retryable
	// errors (not-found, 5xx). Pollers turn SDK retries off, so this replaces
	// the provider-wide 300s per-call retry that LKS relies on.
	mdbErrorBudget = 5 * time.Minute
	// mdbMaxRetryAfter caps how long one Retry-After header can make a poller
	// wait.
	mdbMaxRetryAfter = 60 * time.Second
)

// mdbNoRetries turns SDK retries off. Pollers handle retries themselves, and
// the create POST must never be retried: a 5xx after the server already
// created the database would create a second one.
var mdbNoRetries = retry.Config{Strategy: "none"}

// The API documents no status values; these sets are unverified guesses.
// Statuses are compared lower-cased and trimmed (mdbStatusIn).
var (
	mdbInProgressStates = map[string]bool{
		"provisioning": true,
		"creating":     true,
		"pending":      true,
		"updating":     true,
		"restarting":   true,
	}
	mdbFailedStates = map[string]bool{
		"failed": true,
		"error":  true,
	}
	// mdbUnreachableStates are statuses no readiness wait can succeed from.
	mdbUnreachableStates = map[string]bool{
		"deleting": true,
		"deleted":  true,
	}
	mdbDeleteTerminalStates = map[string]bool{
		"deleted": true,
	}
)

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

func (r *ManagedDatabaseResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// mdbCreateTaintedDetail is the next step after a Create that fails once the
// database exists. Terraform taints a resource whose Create returns an error,
// so this text is for Create only; Update has its own (see Update).
const mdbCreateTaintedDetail = "The database exists and is tracked in state but is marked tainted. " +
	"The next `terraform apply` will destroy and recreate it (or fail, if `prevent_destroy` is set). " +
	"If the latitude.sh dashboard shows it still provisioning, wait until it shows ready, then run " +
	"`terraform untaint <address>` and re-apply."

const mdbCreateFailedStatusDetail = "The database is tainted. The next apply destroys and recreates it. " +
	"If `prevent_destroy` is set, remove it for that apply, or delete the database in the latitude.sh " +
	"dashboard and run `terraform state rm <address>`."

func (r *ManagedDatabaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ManagedDatabaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	planned := data

	effectiveProject := ""
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
	planned.Project = data.Project

	projectID, err := r.resolveProjectID(ctx, effectiveProject)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to resolve project "+effectiveProject+", got error: "+managedDatabaseErrorDetail(err))
		return
	}

	attrs := &components.ManagedDatabasePayloadAttributes{
		Name:       data.Name.ValueString(),
		ProjectID:  projectID,
		Region:     data.Region.ValueString(),
		Plan:       data.Plan.ValueString(),
		Engine:     data.Engine.ValueString(),
		Parameters: mdbParametersPayload(data.Parameters),
	}
	if !data.Billing.IsNull() && !data.Billing.IsUnknown() {
		billing := components.ManagedDatabasePayloadBilling(data.Billing.ValueString())
		attrs.Billing = &billing
	}
	payload := components.ManagedDatabasePayload{
		Data: &components.ManagedDatabasePayloadData{
			Type:       components.ManagedDatabasePayloadTypeManagedDatabases.ToPointer(),
			Attributes: attrs,
		},
	}

	createTimeout, diags := data.Timeouts.Create(ctx, 60*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	deadline := time.Now().Add(createTimeout)

	id, postAttrs, ok := r.createManagedDatabase(ctx, payload, data.Name.ValueString(), effectiveProject, &resp.Diagnostics)
	if !ok {
		return
	}

	// Persist what the POST told us before the long wait, so the database is
	// tracked even if the wait fails. Every value still unknown is set from
	// the response or to a typed null: an apply that fails afterwards would
	// otherwise save unknowns. trusted_sources takes the response's value,
	// never the planned set, so state can't claim an allowlist that was never
	// applied.
	data.ID = types.StringValue(id)
	mdbApplyCreateResponse(ctx, &data, postAttrs, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	credentialsSeen := postAttrs != nil && mdbCredentialsHost(postAttrs.Credentials) != ""
	result := r.waitForManagedDatabaseReady(ctx, id, deadline, mdbWaitOpts{RequireCredentials: true, CredentialsSeen: credentialsSeen})
	if result.LastStatus != "" {
		data.Status = types.StringValue(result.LastStatus)
	}
	if result.Kind != mdbWaitReady {
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		mdbAddCreateWaitError(id, result, &resp.Diagnostics)
		return
	}

	// trusted_sources exists only on the update endpoint, so it is applied with
	// a PATCH once the database is ready. State keeps the API's value until the
	// PATCH succeeds, so a failure can never read as an applied allowlist.
	if sources := mdbTrustedSourcesPayload(ctx, planned.TrustedSources, &resp.Diagnostics); len(sources) > 0 {
		patch := components.ManagedDatabaseUpdatePayloadAttributes{TrustedSources: sources}
		if err := r.patchManagedDatabase(ctx, id, patch, deadline); err != nil {
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
			mdbAddCreatePatchError(id, err, &resp.Diagnostics)
			return
		}
		result = r.waitForManagedDatabaseReady(ctx, id, deadline, mdbWaitOpts{PriorStatus: result.LastStatus})
		if result.LastStatus != "" {
			data.Status = types.StringValue(result.LastStatus)
		}
		if result.Kind != mdbWaitReady {
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
			mdbAddCreateWaitError(id, result, &resp.Diagnostics)
			return
		}
	}

	gone := r.readManagedDatabaseInto(ctx, &data, &planned, postAttrs, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if gone {
		resp.Diagnostics.AddError("API Error", fmt.Sprintf("Managed database %q disappeared right after it became ready.", id))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// createManagedDatabase sends the POST with SDK retries off and returns the
// new ID and the response attributes. ok is false when a diagnostic was added.
func (r *ManagedDatabaseResource) createManagedDatabase(ctx context.Context, payload components.ManagedDatabasePayload, name, project string, diags *diag.Diagnostics) (string, *components.ManagedDatabaseAttributesAttributes, bool) {
	mayExist := func(detail string) {
		diags.AddError(
			"Managed database may have been created",
			fmt.Sprintf("The request failed after it may have reached latitude.sh (%s). Before re-applying, check the dashboard for a database named %q in project %q. "+
				"If it exists, run `terraform import <address> <id>`. Re-applying without checking can create a second, billed database.", detail, name, project),
		)
	}

	result, err := r.client.ManagedDatabases.CreateManagedDatabase(ctx, payload, operations.WithRetries(mdbNoRetries))
	if err != nil {
		var apiErr *components.APIError
		if errors.As(err, &apiErr) {
			switch {
			case apiErr.StatusCode >= 200 && apiErr.StatusCode < 300:
				// The SDK accepts only 201; any other success carries the full
				// record in the body. Recover the ID rather than leak a
				// database. The body is never put in a diagnostic.
				if id := mdbIDFromBody(apiErr.Body); id != "" {
					diags.AddWarning("Unexpected create response", fmt.Sprintf("latitude.sh answered HTTP %d instead of 201; the database ID was recovered from the response.", apiErr.StatusCode))
					return id, nil, true
				}
				mayExist(managedDatabaseErrorDetail(err))
				return "", nil, false
			case apiErr.StatusCode >= 400 && apiErr.StatusCode < 500:
				diags.AddError("Unable to create managed database", managedDatabaseErrorDetail(err))
				return "", nil, false
			}
		}
		mayExist(managedDatabaseErrorDetail(err))
		return "", nil, false
	}

	if result == nil || result.ManagedDatabase == nil || result.ManagedDatabase.Data == nil || result.ManagedDatabase.Data.ID == nil || *result.ManagedDatabase.Data.ID == "" {
		mayExist("the response had no database ID")
		return "", nil, false
	}
	return *result.ManagedDatabase.Data.ID, result.ManagedDatabase.Data.Attributes, true
}

// mdbIDFromBody extracts data.id from a JSON:API body. It decodes into a local
// struct rather than components.ManagedDatabase, whose enum UnmarshalJSON
// would reject an unexpected `type` and lose the ID.
func mdbIDFromBody(body string) string {
	var envelope struct {
		Data struct {
			ID *string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil || envelope.Data.ID == nil {
		return ""
	}
	return *envelope.Data.ID
}

// mdbApplyCreateResponse fills every value still unknown after the POST from
// the response, or with a typed null.
func mdbApplyCreateResponse(ctx context.Context, data *ManagedDatabaseResourceModel, attrs *components.ManagedDatabaseAttributesAttributes, diags *diag.Diagnostics) {
	if attrs == nil {
		attrs = &components.ManagedDatabaseAttributesAttributes{}
	}
	data.Status = mdbStatusValue(attrs.Status)
	data.Credentials = mdbCredentials(types.ObjectNull(mdbCredentialsAttrTypes), nil, attrs.Credentials, nil, diags)
	if data.Billing.IsUnknown() {
		var billing *string
		if attrs.Plan != nil {
			billing = attrs.Plan.Billing
		}
		data.Billing = mdbStringValue(billing)
	}
	if attrs.TrustedSources != nil {
		set, d := types.SetValueFrom(ctx, types.StringType, attrs.TrustedSources)
		diags.Append(d...)
		data.TrustedSources = set
	} else {
		data.TrustedSources = types.SetNull(types.StringType)
	}
}

func mdbAddCreateWaitError(id string, result mdbWaitResult, diags *diag.Diagnostics) {
	switch result.Kind {
	case mdbWaitFailedStatus:
		diags.AddError(
			fmt.Sprintf("Managed database %s failed", id),
			fmt.Sprintf("The database reports status %q. %s", result.LastStatus, mdbCreateFailedStatusDetail),
		)
	case mdbWaitGone:
		diags.AddError(
			fmt.Sprintf("Managed database %s is being deleted", id),
			fmt.Sprintf("The database reports status %q, so it can't become ready. %s", result.LastStatus, mdbCreateTaintedDetail),
		)
	case mdbWaitError:
		diags.AddError(
			fmt.Sprintf("Unable to check managed database %s", id),
			fmt.Sprintf("%s. %s", managedDatabaseErrorDetail(result.Err), mdbCreateTaintedDetail),
		)
	default:
		diags.AddError(
			fmt.Sprintf("Timeout waiting for managed database %s to become ready", id),
			fmt.Sprintf("Last status: %q. %s", result.LastStatus, mdbCreateTaintedDetail),
		)
	}
}

func (r *ManagedDatabaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ManagedDatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	gone := r.readManagedDatabaseInto(ctx, &data, nil, nil, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if gone {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ManagedDatabaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ManagedDatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.ID.ValueString()
	if id == "" {
		return
	}
	priorStatus := data.Status.ValueString()

	_, err := r.client.ManagedDatabases.DestroyManagedDatabase(ctx, id)
	if err != nil {
		if managedDatabaseNotFound(err) {
			return
		}
		var apiErr *components.APIError
		// The SDK accepts only 204; any other success means the delete was
		// accepted (e.g. an async 202).
		if !errors.As(err, &apiErr) || apiErr.StatusCode < 200 || apiErr.StatusCode >= 300 {
			resp.Diagnostics.AddError("Unable to delete managed database", managedDatabaseErrorDetail(err))
			return
		}
	}

	deleteTimeout, diags := data.Timeouts.Delete(ctx, 30*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	result := r.waitForManagedDatabaseDeleted(ctx, id, time.Now().Add(deleteTimeout), priorStatus)
	const stateRm = "Confirm in the latitude.sh dashboard that the database is gone, then run `terraform state rm <address>`."
	switch result.Kind {
	case mdbWaitReady:
		return
	case mdbWaitFailedStatus:
		resp.Diagnostics.AddError(
			fmt.Sprintf("Managed database %s failed while being deleted", id),
			fmt.Sprintf("The database reports status %q. %s", result.LastStatus, stateRm),
		)
	case mdbWaitError:
		resp.Diagnostics.AddError(
			fmt.Sprintf("Unable to check deletion of managed database %s", id),
			fmt.Sprintf("%s. %s", managedDatabaseErrorDetail(result.Err), stateRm),
		)
	default:
		resp.Diagnostics.AddError(
			fmt.Sprintf("Timeout waiting for managed database %s to be deleted", id),
			fmt.Sprintf("Last status: %q. %s", result.LastStatus, stateRm),
		)
	}
}

func (r *ManagedDatabaseResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	data := ManagedDatabaseResourceModel{
		ID:             types.StringValue(req.ID),
		Project:        types.StringNull(),
		Name:           types.StringNull(),
		Region:         types.StringNull(),
		Plan:           types.StringNull(),
		Engine:         types.StringNull(),
		Billing:        types.StringNull(),
		TrustedSources: types.SetNull(types.StringType),
		Parameters:     types.MapNull(types.StringType),
		Status:         types.StringNull(),
		Credentials:    types.ObjectNull(mdbCredentialsAttrTypes),
		// No timeouts block is set on import, so give the field an explicitly
		// typed null; the zero value fails state conversion.
		Timeouts: timeouts.Value{
			Object: types.ObjectNull(map[string]attr.Type{
				"create": types.StringType,
				"update": types.StringType,
				"delete": types.StringType,
			}),
		},
	}

	gone := r.readManagedDatabaseInto(ctx, &data, nil, nil, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if gone {
		resp.Diagnostics.AddError("Not Found", fmt.Sprintf("Managed database %q not found", req.ID))
		return
	}

	resp.Diagnostics.AddWarning("Review the first plan after importing a managed database", mdbImportWarning(&data))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// mdbImportWarning lists what import can't fill reliably, so the first plan
// doesn't surprise anyone.
func mdbImportWarning(data *ManagedDatabaseResourceModel) string {
	var b strings.Builder
	b.WriteString("Check the first plan for a replacement of any of name, engine, plan, region or project: a spelling difference between the API and the configuration plans one. " +
		"Configure project with the ID imported here, not a slug, or use lifecycle { ignore_changes }. " +
		"A provider-level default project written as a slug also gives a \"Default project changed\" error after import, so set project on the resource. " +
		"parameters are not imported: if the configuration sets them, the first apply re-sends them, which triggers a rolling restart.")

	var missing []string
	for name, v := range map[string]types.String{"name": data.Name, "engine": data.Engine, "plan": data.Plan, "region": data.Region, "project": data.Project, "billing": data.Billing} {
		if v.IsNull() {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		b.WriteString(" The API didn't return: " + strings.Join(missing, ", ") + "; set them in the configuration.")
	}
	if v := data.Billing.ValueString(); v != "" && v != "monthly" && v != "yearly" {
		b.WriteString(fmt.Sprintf(" The API reports billing %q, which the configuration can't express; leave billing out of the configuration.", v))
	}
	if data.Credentials.IsNull() {
		b.WriteString(" The API returned no credentials, so credentials stay null.")
	} else if pw, ok := data.Credentials.Attributes()["password"]; ok && pw.IsNull() {
		b.WriteString(" The API returned no password, so credentials.password and credentials.url stay null.")
	}
	return b.String()
}

// readManagedDatabaseInto calls Show and maps the result onto data with
// mdbApplyAttributes. gone is true when the database no longer exists (a 404,
// or status "deleted"); the caller decides what that means.
func (r *ManagedDatabaseResource) readManagedDatabaseInto(ctx context.Context, data, planned *ManagedDatabaseResourceModel, postResp *components.ManagedDatabaseAttributesAttributes, diags *diag.Diagnostics) (gone bool) {
	id := data.ID.ValueString()
	if id == "" {
		diags.AddError("Invalid ID", "Managed database ID is empty")
		return false
	}

	result, err := r.client.ManagedDatabases.ShowManagedDatabase(ctx, id)
	if err != nil {
		if managedDatabaseNotFound(err) {
			return true
		}
		diags.AddError("Unable to read managed database", managedDatabaseErrorDetail(err))
		return false
	}
	if result == nil || result.ManagedDatabase == nil || result.ManagedDatabase.Data == nil {
		return true
	}

	obj := result.ManagedDatabase.Data
	if obj.Attributes != nil && obj.Attributes.Status != nil && mdbStatusIn(*obj.Attributes.Status, mdbDeleteTerminalStates) {
		return true
	}
	if obj.ID != nil && *obj.ID != "" {
		data.ID = types.StringValue(*obj.ID)
	}
	mdbApplyAttributes(ctx, data, planned, obj.Attributes, postResp, diags)
	return false
}

// resolveProjectID turns the `project` selector into the ID the create
// payload's project_id takes. The SDK's doc comment calls it a "Project slug"
// while its example uses an ID, so this resolves conservatively, as
// resource_lks_cluster.go does. The configured selector is never rewritten in
// state.
func (r *ManagedDatabaseResource) resolveProjectID(ctx context.Context, selector string) (string, error) {
	if strings.HasPrefix(selector, "proj_") {
		return selector, nil
	}

	res, err := r.client.Projects.GetProject(ctx, selector)
	if err != nil {
		return "", err
	}
	if res == nil || res.Object == nil || res.Object.Data == nil || res.Object.Data.ID == nil || *res.Object.Data.ID == "" {
		return "", fmt.Errorf("project %q not found", selector)
	}
	return *res.Object.Data.ID, nil
}

// --- pollers ----------------------------------------------------------------

type mdbWaitKind int

const (
	// mdbWaitReady means the wait succeeded (ready, or deleted for the delete
	// poller).
	mdbWaitReady mdbWaitKind = iota
	mdbWaitTimeout
	mdbWaitFailedStatus
	// mdbWaitGone means the database is being deleted or is gone.
	mdbWaitGone
	// mdbWaitError means a non-retryable error, a cancelled context, or the
	// error budget ran out.
	mdbWaitError
)

// mdbWaitResult is what a poller observed. Pollers never format user-facing
// text; Create, Update and Delete do, so Update can never show Create's taint
// wording.
type mdbWaitResult struct {
	Kind       mdbWaitKind
	LastStatus string
	Err        error
}

type mdbWaitOpts struct {
	// RequireCredentials makes the wait end only once credentials.host is
	// known, from the create response (CredentialsSeen) or a polled Show.
	RequireCredentials bool
	CredentialsSeen    bool
	// FailedIsSettled counts a failed status as settled, so a corrective PATCH
	// can still be sent to a failed database.
	FailedIsSettled bool
	// PriorStatus is the status before the operation. A failed status that was
	// already there keeps the wait polling instead of failing fast.
	PriorStatus string
}

// mdbPoll calls Show until observe returns done, the deadline passes, or an
// error ends the wait. Each call runs with SDK retries off and under the
// deadline, so the SDK's own retry can't push a wait past it. Not-found and
// 5xx are retried within mdbErrorBudget of consecutive errors; a 429 is
// retried without counting against it, honouring Retry-After.
func (r *ManagedDatabaseResource) mdbPoll(ctx context.Context, id string, deadline time.Time, interval time.Duration, onNotFound func() (mdbWaitResult, bool), observe func(*components.ManagedDatabaseAttributesAttributes) (mdbWaitResult, bool)) mdbWaitResult {
	var last mdbWaitResult
	var errorsSince time.Time

	for time.Now().Before(deadline) {
		wait := interval

		callCtx, cancel := context.WithDeadline(ctx, deadline)
		result, err := r.client.ManagedDatabases.ShowManagedDatabase(callCtx, id, operations.WithRetries(mdbNoRetries))
		cancel()

		switch {
		case err != nil && ctx.Err() != nil:
			last.Kind, last.Err = mdbWaitError, ctx.Err()
			return last
		case err != nil && !time.Now().Before(deadline):
			last.Kind = mdbWaitTimeout
			return last
		case err != nil && onNotFound != nil && managedDatabaseNotFound(err):
			if res, done := onNotFound(); done {
				res.LastStatus = last.LastStatus
				return res
			}
		case err != nil && mdbRateLimited(err):
			wait = mdbRetryAfter(err, interval)
		case err != nil && managedDatabaseRetryableDuringPoll(err):
			if errorsSince.IsZero() {
				errorsSince = time.Now()
			} else if time.Since(errorsSince) > mdbErrorBudget {
				last.Kind, last.Err = mdbWaitError, err
				return last
			}
		case err != nil:
			last.Kind, last.Err = mdbWaitError, err
			return last
		default:
			errorsSince = time.Time{}
			var attrs *components.ManagedDatabaseAttributesAttributes
			if result != nil && result.ManagedDatabase != nil && result.ManagedDatabase.Data != nil {
				attrs = result.ManagedDatabase.Data.Attributes
			}
			if attrs != nil && attrs.Status != nil {
				last.LastStatus = *attrs.Status
				tflog.Debug(ctx, "managed database status", map[string]any{"id": id, "status": *attrs.Status})
			}
			if res, done := observe(attrs); done {
				res.LastStatus = last.LastStatus
				return res
			}
		}

		if remaining := time.Until(deadline); wait > remaining {
			wait = remaining
		}
		select {
		case <-ctx.Done():
			last.Kind, last.Err = mdbWaitError, ctx.Err()
			return last
		case <-time.After(wait):
		}
	}

	last.Kind = mdbWaitTimeout
	return last
}

// mdbRetryAfter reads a Retry-After header in seconds from a 429, capped at
// mdbMaxRetryAfter; it falls back to the poll interval.
func mdbRetryAfter(err error, fallback time.Duration) time.Duration {
	var apiErr *components.APIError
	if !errors.As(err, &apiErr) || apiErr.RawResponse == nil {
		return fallback
	}
	secs, convErr := strconv.Atoi(strings.TrimSpace(apiErr.RawResponse.Header.Get("Retry-After")))
	if convErr != nil || secs <= 0 {
		return fallback
	}
	d := time.Duration(secs) * time.Second
	if d > mdbMaxRetryAfter {
		d = mdbMaxRetryAfter
	}
	return d
}

// waitForManagedDatabaseReady polls until the database is settled: a
// non-empty status outside the in-progress, failed and unreachable sets, as
// reported by a polled Show (never the create response). With
// RequireCredentials, it also needs credentials.host from the create response
// or any polled Show.
func (r *ManagedDatabaseResource) waitForManagedDatabaseReady(ctx context.Context, id string, deadline time.Time, opts mdbWaitOpts) mdbWaitResult {
	credentialsSeen := opts.CredentialsSeen
	priorFailed := mdbStatusIn(opts.PriorStatus, mdbFailedStates)

	return r.mdbPoll(ctx, id, deadline, mdbReadyPollInterval, nil, func(attrs *components.ManagedDatabaseAttributesAttributes) (mdbWaitResult, bool) {
		if attrs == nil || attrs.Status == nil || strings.TrimSpace(*attrs.Status) == "" {
			return mdbWaitResult{}, false
		}
		if mdbCredentialsHost(attrs.Credentials) != "" {
			credentialsSeen = true
		}
		status := *attrs.Status
		switch {
		case mdbStatusIn(status, mdbUnreachableStates):
			return mdbWaitResult{Kind: mdbWaitGone}, true
		case mdbStatusIn(status, mdbFailedStates):
			if !opts.FailedIsSettled {
				if priorFailed {
					return mdbWaitResult{}, false
				}
				return mdbWaitResult{Kind: mdbWaitFailedStatus}, true
			}
		case mdbStatusIn(status, mdbInProgressStates):
			return mdbWaitResult{}, false
		}
		if opts.RequireCredentials && !credentialsSeen {
			return mdbWaitResult{}, false
		}
		return mdbWaitResult{Kind: mdbWaitReady}, true
	})
}

// waitForManagedDatabaseDeleted polls until Show returns not-found or status
// "deleted". A failed status during the delete is an error, unless the
// database had already failed before the DELETE (priorStatus): then it keeps
// polling, or a database that failed at create could never be destroyed.
func (r *ManagedDatabaseResource) waitForManagedDatabaseDeleted(ctx context.Context, id string, deadline time.Time, priorStatus string) mdbWaitResult {
	priorFailed := mdbStatusIn(priorStatus, mdbFailedStates)
	onNotFound := func() (mdbWaitResult, bool) { return mdbWaitResult{Kind: mdbWaitReady}, true }

	return r.mdbPoll(ctx, id, deadline, mdbDeletePollInterval, onNotFound, func(attrs *components.ManagedDatabaseAttributesAttributes) (mdbWaitResult, bool) {
		if attrs == nil || attrs.Status == nil {
			return mdbWaitResult{}, false
		}
		status := *attrs.Status
		switch {
		case mdbStatusIn(status, mdbDeleteTerminalStates):
			return mdbWaitResult{Kind: mdbWaitReady}, true
		case mdbStatusIn(status, mdbFailedStates) && !priorFailed:
			return mdbWaitResult{Kind: mdbWaitFailedStatus}, true
		}
		return mdbWaitResult{}, false
	})
}

// Update sends trusted_sources and parameters, the only attributes the API can
// change; every other input forces replacement or is a create-time selector.
//
// Update never taints: the framework seeds the response state with the prior
// state, so on any error the database is tracked as before and the planned
// values are never saved. Its error texts say so, and never suggest untaint.
func (r *ManagedDatabaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, state ManagedDatabaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	planned := data
	id := state.ID.ValueString()

	updateTimeout, diags := data.Timeouts.Update(ctx, 30*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	deadline := time.Now().Add(updateTimeout)

	patch, send := mdbUpdatePatch(ctx, &data, &state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if send {
		// Wait for a settled status first, so a PATCH never hits a database
		// that isn't ready (e.g. after an untaint, or mid-restart). A failed
		// status counts as settled here: a corrective PATCH must still be
		// able to reach a database a bad parameter broke.
		pre := r.waitForManagedDatabaseReady(ctx, id, deadline, mdbWaitOpts{FailedIsSettled: true})
		if pre.Kind != mdbWaitReady {
			mdbAddUpdateError(&resp.Diagnostics, id, mdbUpdatePreWait, pre.Kind, pre.LastStatus, pre.Err)
			return
		}

		if err := r.patchManagedDatabase(ctx, id, patch, deadline); err != nil {
			kind := mdbUpdatePatchFailed
			if mdbPatchRejected(err) {
				kind = mdbUpdatePatchRejected
			}
			mdbAddUpdateError(&resp.Diagnostics, id, kind, mdbWaitError, pre.LastStatus, err)
			return
		}

		post := r.waitForManagedDatabaseReady(ctx, id, deadline, mdbWaitOpts{PriorStatus: pre.LastStatus})
		if post.Kind != mdbWaitReady {
			mdbAddUpdateError(&resp.Diagnostics, id, mdbUpdatePostWait, post.Kind, post.LastStatus, post.Err)
			return
		}
	}

	// Always finish with a read, even with no PATCH: the framework marks
	// unconfigured Computed attributes (status) unknown on any diff, and
	// saving them unknown is an invalid result.
	gone := r.readManagedDatabaseInto(ctx, &data, &planned, nil, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if gone {
		mdbAddUpdateError(&resp.Diagnostics, id, mdbUpdatePostWait, mdbWaitGone, "", nil)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// mdbUpdatePatch builds the PATCH from what changed between state and plan.
// trusted_sources is sent when it is configured and differs. parameters is
// sent, as the full configured map, when it differs and isn't empty: an
// empty map can't be sent (it is omitempty, and the API requires at least
// one attribute), and removing keys can't unset them. send is false when
// nothing remains to send (e.g. only timeouts changed, billing was adopted,
// or project was rewritten between a slug and its ID).
func mdbUpdatePatch(ctx context.Context, plan, state *ManagedDatabaseResourceModel, diags *diag.Diagnostics) (components.ManagedDatabaseUpdatePayloadAttributes, bool) {
	var patch components.ManagedDatabaseUpdatePayloadAttributes
	send := false

	if !plan.TrustedSources.IsNull() && !plan.TrustedSources.IsUnknown() && !plan.TrustedSources.Equal(state.TrustedSources) {
		if sources := mdbTrustedSourcesPayload(ctx, plan.TrustedSources, diags); len(sources) > 0 {
			patch.TrustedSources = sources
			send = true
		}
	}
	if !plan.Parameters.Equal(state.Parameters) {
		if params := mdbParametersPayload(plan.Parameters); len(params) > 0 {
			patch.Parameters = params
			send = true
		}
	}
	return patch, send
}

// patchManagedDatabase sends a PATCH. It is idempotent, so a 429 or 5xx is
// retried within the deadline with the poller's backoff (honouring
// Retry-After); every call runs with SDK retries off and under the deadline.
// A 2xx the SDK didn't expect (it accepts only 200, e.g. a 202 for a rolling
// restart) counts as accepted.
func (r *ManagedDatabaseResource) patchManagedDatabase(ctx context.Context, id string, attrs components.ManagedDatabaseUpdatePayloadAttributes, deadline time.Time) error {
	payload := components.ManagedDatabaseUpdatePayload{
		Data: &components.ManagedDatabaseUpdatePayloadData{
			Type:       components.ManagedDatabaseUpdatePayloadTypeManagedDatabases.ToPointer(),
			Attributes: &attrs,
		},
	}

	for {
		callCtx, cancel := context.WithDeadline(ctx, deadline)
		_, err := r.client.ManagedDatabases.UpdateManagedDatabase(callCtx, id, payload, operations.WithRetries(mdbNoRetries))
		cancel()
		if err == nil {
			return nil
		}

		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode >= 200 && apiErr.StatusCode < 300 {
			return nil
		}
		retryable := mdbRateLimited(err) || (errors.As(err, &apiErr) && apiErr.StatusCode >= 500 && apiErr.StatusCode < 600)
		if !retryable || ctx.Err() != nil {
			return err
		}

		wait := mdbRetryAfter(err, mdbReadyPollInterval)
		if !time.Now().Add(wait).Before(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(wait):
		}
	}
}

// mdbPatchRejected reports whether a PATCH error means the value itself was
// rejected (400 or 422). Every other error (401, 403, 409, 423, 429, 5xx,
// timeout) means permissions, not-ready, or a transient failure.
func mdbPatchRejected(err error) bool {
	var apiErr *components.APIError
	return errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusBadRequest || apiErr.StatusCode == http.StatusUnprocessableEntity)
}

func mdbForbidden(err error) bool {
	var apiErr *components.APIError
	return errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden)
}

// mdbAddCreatePatchError reports a failed allowlist PATCH during Create. The
// resource is tainted (Create returned an error), and the next step depends on
// whether the value was rejected.
func mdbAddCreatePatchError(id string, err error, diags *diag.Diagnostics) {
	detail := managedDatabaseErrorDetail(err)
	if mdbPatchRejected(err) {
		diags.AddError(
			"trusted_sources rejected by the API",
			fmt.Sprintf("latitude.sh rejected the allowlist for managed database %s: %s. The database exists and is tracked in state but is tainted. "+
				"Fix trusted_sources, then run `terraform untaint <address>` and apply. Applying without untainting destroys and recreates the database "+
				"(or fails, if `prevent_destroy` is set), and the same value will be rejected again.", id, detail),
		)
		return
	}
	msg := fmt.Sprintf("The allowlist for managed database %s couldn't be applied: %s. %s", id, detail, mdbCreateTaintedDetail)
	if mdbForbidden(err) {
		msg += " Fix the token's permissions before re-applying; recreating the database won't help."
	}
	diags.AddError("Unable to apply trusted_sources", msg)
}

type mdbUpdateStage int

const (
	mdbUpdatePreWait mdbUpdateStage = iota
	mdbUpdatePatchRejected
	mdbUpdatePatchFailed
	mdbUpdatePostWait
)

// mdbAddUpdateError reports an Update failure. Update never taints, so none
// of these texts mention untaint.
func mdbAddUpdateError(diags *diag.Diagnostics, id string, stage mdbUpdateStage, kind mdbWaitKind, status string, err error) {
	summary, detail := mdbUpdateErrorText(id, stage, kind, status, err)
	diags.AddError(summary, detail)
}

func mdbUpdateErrorText(id string, stage mdbUpdateStage, kind mdbWaitKind, status string, err error) (string, string) {
	errDetail := ""
	if err != nil {
		errDetail = managedDatabaseErrorDetail(err)
	}
	switch stage {
	case mdbUpdatePreWait:
		switch kind {
		case mdbWaitGone:
			return fmt.Sprintf("Managed database %s is being deleted", id),
				fmt.Sprintf("The database reports status %q, so no change was sent. Run `terraform plan` to refresh. If it's gone, the plan recreates it.", status)
		case mdbWaitTimeout:
			return fmt.Sprintf("Timeout waiting for managed database %s to be ready", id),
				fmt.Sprintf("The database didn't report ready in time (last status %q), so no change was sent. Terraform still tracks it as before. Re-apply once the latitude.sh dashboard shows it ready.", status)
		default:
			return fmt.Sprintf("Unable to check managed database %s", id),
				fmt.Sprintf("%s. No change was sent. Terraform still tracks the previous values. Run `terraform plan` to refresh and see the current state.", errDetail)
		}
	case mdbUpdatePatchRejected:
		return fmt.Sprintf("latitude.sh rejected the change to managed database %s", id),
			fmt.Sprintf("%s. Nothing was applied. Fix the configuration and re-apply.", errDetail)
	case mdbUpdatePatchFailed:
		detail := fmt.Sprintf("The change couldn't be sent: %s. Terraform still tracks the database as before; re-applying retries the update.", errDetail)
		if mdbForbidden(err) {
			detail += " Fix the token's permissions first."
		}
		return fmt.Sprintf("Unable to update managed database %s", id), detail
	default: // mdbUpdatePostWait
		switch kind {
		case mdbWaitFailedStatus:
			return fmt.Sprintf("Managed database %s failed after the change", id),
				fmt.Sprintf("The change was sent, but the database now reports status %q. Terraform still tracks the previous values. Run `terraform plan` to see what remains to apply. "+
					"To recreate the database, run `terraform apply -replace=<address>` (this destroys its data).", status)
		case mdbWaitTimeout:
			return fmt.Sprintf("Timeout waiting for managed database %s after the change", id),
				fmt.Sprintf("The change was sent, but the database didn't report ready in time (last status %q). Terraform still tracks the previous values. Re-apply once the latitude.sh dashboard shows it ready.", status)
		case mdbWaitGone:
			return fmt.Sprintf("Managed database %s disappeared after the change", id),
				"The database is being deleted or is gone. Terraform still tracks the previous values. Run `terraform plan` to refresh and see the current state."
		default:
			return fmt.Sprintf("Unable to check managed database %s after the change", id),
				fmt.Sprintf("%s. Terraform still tracks the previous values. Run `terraform plan` to refresh and see the current state.", errDetail)
		}
	}
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
