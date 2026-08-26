package latitudesh

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	"github.com/latitudesh/terraform-provider-latitudesh/v2/internal/planmodifiers"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
	"github.com/latitudesh/terraform-provider-latitudesh/v2/internal/validators"
)

// validReinstallTriggers lists the trigger names accepted in
// allowed_reinstall_triggers. The token "user_data" covers both ID changes and
// content changes (the latter detected through the user_data_content_hash
// shadow attribute).
var validReinstallTriggers = []string{
	"operating_system",
	"user_data",
	"raid",
	"disk_layout",
	"ipxe",
	"ssh_keys",
	"hostname",
}

var _ resource.Resource = &ServerResource{}
var _ resource.ResourceWithImportState = &ServerResource{}
var _ resource.ResourceWithModifyPlan = &ServerResource{}
var _ resource.ResourceWithValidateConfig = &ServerResource{}

func NewServerResource() resource.Resource {
	return &ServerResource{}
}

type ServerResource struct {
	client         *latitudeshgosdk.Latitudesh
	defaultProject string
	hashCache      *sync.Map
}

type ServerResourceModel struct {
	ID                       types.String      `tfsdk:"id"`
	Project                  types.String      `tfsdk:"project"`
	Site                     types.String      `tfsdk:"site"`
	Plan                     types.String      `tfsdk:"plan"`
	OperatingSystem          types.String      `tfsdk:"operating_system"`
	Hostname                 types.String      `tfsdk:"hostname"`
	SSHKeys                  types.List        `tfsdk:"ssh_keys"`
	UserData                 types.String      `tfsdk:"user_data"`
	UserDataContentHash      types.String      `tfsdk:"user_data_content_hash"`
	AllowedReinstallTriggers types.List        `tfsdk:"allowed_reinstall_triggers"`
	Raid                     types.String      `tfsdk:"raid"`
	DiskLayout               []DiskLayoutModel `tfsdk:"disk_layout"`
	Ipxe                     types.String      `tfsdk:"ipxe"`
	BgpReady                 types.Bool        `tfsdk:"bgp_ready"`
	Billing                  types.String      `tfsdk:"billing"`
	Tags                     types.List        `tfsdk:"tags"`
	AllowReinstall           types.Bool        `tfsdk:"allow_reinstall"`
	PrimaryIpv4              types.String      `tfsdk:"primary_ipv4"`
	PrimaryIpv6              types.String      `tfsdk:"primary_ipv6"`
	Status                   types.String      `tfsdk:"status"`
	Locked                   types.Bool        `tfsdk:"locked"`
	CreatedAt                types.String      `tfsdk:"created_at"`
	Region                   types.String      `tfsdk:"region"`
	Interfaces               types.List        `tfsdk:"interfaces"`
	Timeouts                 timeouts.Value    `tfsdk:"timeouts"`
}

// DiskLayoutModel describes a single disk group in a custom disk layout.
// It mirrors operations.CreateServer{,Reinstall}ServersDiskLayout in the SDK.
type DiskLayoutModel struct {
	Count      types.Int64  `tfsdk:"count"`
	Role       types.String `tfsdk:"role"`
	RaidLevel  types.String `tfsdk:"raid_level"`
	MountPoint types.String `tfsdk:"mount_point"`
}

func (r *ServerResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server"
}

func (r *ServerResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Server resource",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "The project (ID or slug) to deploy the server into. Optional here only if `project` is set on the provider block; one of the two is required.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"site": schema.StringAttribute{
				MarkdownDescription: "The server site slug. Examples: `AMS`, `ASH`, `BGT`, `BUE`, `CHI`, `FRA`, `TYO4`. For a complete list of available regions and their slugs, see the [API reference](https://www.latitude.sh/docs/api-reference/get-regions).",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					planmodifiers.CaseInsensitiveDiff{},
					stringplanmodifier.RequiresReplace(),
				},
			},
			"plan": schema.StringAttribute{
				MarkdownDescription: "The server plan slug. Examples: `m4-metal-medium`, `c3-large-x86`, `f4-metal-medium`, `rs4-metal-large`, `g4-rtx6kpro-large`. For a complete list of available plans and their slugs, see the [API reference](https://www.latitude.sh/docs/api-reference/get-plans).",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"operating_system": schema.StringAttribute{
				MarkdownDescription: "The server OS slug. Updating the OS requires a reinstall and only succeeds when `allow_reinstall = true`; otherwise the plan fails with an error. Examples: `ubuntu_24_04_x64_lts`, `ubuntu_22_04_x64_lts`, `debian_12`, `rockylinux_8`, `windows_2022_std`. For a complete list of available operating systems and their slugs, see the [API reference](https://www.latitude.sh/docs/api-reference/get-plans-operating-system).",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"hostname": schema.StringAttribute{
				MarkdownDescription: "The server hostname. Required — the API rejects a create without it.\n" +
					"  - Maximum length: 32 characters;\n" +
					"  - Allowed characters: letters (a–z, A–Z), digits (0–9), dots (.), and hyphens (-);\n" +
					"  - Must not begin or end with a dot or hyphen;\n" +
					"  - Underscores (_) are not allowed;\n" +
					"  - These rules are checked only for hostnames this plan creates or changes. A server imported with a hostname that predates them keeps planning cleanly until you rename it;\n" +
					"  - Updating hostname is applied in-place via PATCH by default. Set `allow_reinstall = true` on the resource to make hostname changes trigger a server reinstall instead.",
				// Required as of SDK v1.19.3, which types hostname as a non-pointer on
				// the create payload. It was Optional+Computed before, so omitting it
				// passed validation and only failed at the API. Note this is breaking:
				// a configuration that never set hostname now fails at plan time.
				//
				// The shape rules are enforced in ModifyPlan (validateHostnameOnChange)
				// rather than here: schema validators run in ValidateResourceConfig,
				// which has no state, so they cannot tell an imported legacy hostname
				// apart from one the practitioner just typed.
				Required: true,
			},
			"ssh_keys": schema.ListAttribute{
				MarkdownDescription: "List of server SSH key ids.\n" +
					"    Updating ssh_keys requires a reinstall and only succeeds when `allow_reinstall = true`; otherwise the plan fails with an error.",
				ElementType: types.StringType,
				Optional:    true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"user_data": schema.StringAttribute{
				MarkdownDescription: "The id of user data to set on the server. Changes to the referenced user_data's content are also tracked automatically and trigger a reinstall when `allow_reinstall = true`. Updating user_data requires a reinstall and only succeeds when `allow_reinstall = true`; otherwise the plan fails with an error.",
				Optional:            true,
				Validators:          validators.UserData(),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"raid": schema.StringAttribute{
				MarkdownDescription: "RAID mode for the server. Updating raid requires a reinstall and only succeeds when `allow_reinstall = true`; otherwise the plan fails with an error. Mutually exclusive with `disk_layout`.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"disk_layout": schema.ListNestedAttribute{
				MarkdownDescription: "Custom disk layout made of one or more disk groups, used instead of `raid`. Mutually exclusive with `raid` and `ipxe`. The layout is refreshed from the server deploy config on read, so out-of-band changes are detected and imported servers populate it. Changing it requires a reinstall and only succeeds when `allow_reinstall = true`. The OS group's filesystem is always `ext4` (managed by the API) and is not configurable here.",
				Optional:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"count": schema.Int64Attribute{
							MarkdownDescription: "Number of disks to include in this group.",
							Required:            true,
						},
						"role": schema.StringAttribute{
							MarkdownDescription: "Role of this disk group: `os`, `storage`, or `raw`.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("os", "storage", "raw"),
							},
						},
						"raid_level": schema.StringAttribute{
							MarkdownDescription: "RAID level for this disk group: `raid-0` or `raid-1`. Requires `count >= 2`.",
							Optional:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("raid-0", "raid-1"),
							},
						},
						"mount_point": schema.StringAttribute{
							MarkdownDescription: "Mount point for this disk group, e.g. `/var/lib`. Required for the `storage` role.",
							Optional:            true,
						},
					},
				},
			},
			"ipxe": schema.StringAttribute{
				MarkdownDescription: "The iPXE script to boot. Accepts either a URL pointing at the script, or the script encoded in base64. Required when `operating_system = \"ipxe\"`; the plan fails with an explicit error if it is missing. That check applies only when this plan sets or changes `operating_system` or `ipxe`, so a server imported with `operating_system = \"ipxe\"` and no script of its own — one provisioned through Tinkerbell, for example — keeps planning cleanly. Updating ipxe requires a reinstall and only succeeds when `allow_reinstall = true`; otherwise the plan fails with an error.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"bgp_ready": schema.BoolAttribute{
				MarkdownDescription: "Deploy the server onto hardware capable of announcing an Elastic IP over BGP. " +
					"This is a deploy-time only flag: the API accepts it only when the server is created and never returns it, " +
					"so the provider keeps the configured value in state and does not refresh it from the API. " +
					"Changing it after creation forces the server to be recreated. When omitted, the API default applies.",
				Optional: true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"billing": schema.StringAttribute{
				MarkdownDescription: "The server billing type. Accepts `hourly` and `monthly` for on-demand projects and `yearly` for reserved projects. **Defaults to `monthly`**, which is charged upfront based on the proration of the current billing cycle. Use `hourly` for dynamic or short-lived workloads. When omitted, the plan shows the effective value (`billing = \"monthly\"` on create).",
				Optional:            true,
				Computed:            true,
				Validators:          validators.Billing(),
				PlanModifiers: []planmodifier.String{
					planmodifiers.DefaultOnCreate{Value: validators.BillingMonthly},
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "List of server tag IDs",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"allow_reinstall": schema.BoolAttribute{
				MarkdownDescription: "Allow server reinstallation when `operating_system`, `hostname`, `ssh_keys`, `user_data` (ID or content), `raid`, or `ipxe` changes. **Defaults to `false`.** When `false`, `hostname` changes are applied in-place via PATCH and any other reinstall-only field change fails the plan with an explicit error; set this to `true` on resources where you want reinstalls to happen automatically. See `allowed_reinstall_triggers` to further restrict which kinds of changes are permitted to cause a reinstall.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"allowed_reinstall_triggers": schema.ListAttribute{
				MarkdownDescription: "Optional list restricting which field changes are allowed to trigger a server reinstall when `allow_reinstall = true`. When omitted, all reinstall-only field changes trigger a reinstall (default behavior). When set, only listed names cause a reinstall; changes to reinstall-only fields not in the list fail the plan with an explicit error, except `hostname` which falls back to its in-place PATCH path. Valid values: `operating_system`, `user_data`, `raid`, `disk_layout`, `ipxe`, `ssh_keys`, `hostname`. The token `user_data` covers both ID changes and content changes of the referenced `latitudesh_user_data` resource.",
				Optional:            true,
				ElementType:         types.StringType,
				Validators: []validator.List{
					listvalidator.ValueStringsAre(stringvalidator.OneOf(validReinstallTriggers...)),
					listvalidator.UniqueValues(),
				},
			},
			"user_data_content_hash": schema.StringAttribute{
				MarkdownDescription: "SHA256 hex digest of the user_data `content` currently tracked for this server. Maintained automatically by the provider; a change in this value triggers a server reinstall when `allow_reinstall = true` and `user_data` is an allowed reinstall trigger.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"primary_ipv4": schema.StringAttribute{
				MarkdownDescription: "Primary IPv4 address of the server",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"primary_ipv6": schema.StringAttribute{
				MarkdownDescription: "Primary IPv6 address of the server",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Server power status",
				Computed:            true,
			},
			"locked": schema.BoolAttribute{
				MarkdownDescription: "Lock/unlock the server. A locked server cannot be deleted or updated.",
				Computed:            true,
				Optional:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "The timestamp for when the server was created",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},

			"region": schema.StringAttribute{
				MarkdownDescription: "The region where the server is deployed",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"interfaces": schema.ListNestedAttribute{
				MarkdownDescription: "List of network interfaces",
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Interface name",
							Computed:            true,
						},
						"mac_address": schema.StringAttribute{
							MarkdownDescription: "MAC address",
							Computed:            true,
						},
						"description": schema.StringAttribute{
							MarkdownDescription: "Description",
							Computed:            true,
						},
					},
				},
			},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{
				Create:            true,
				Update:            true,
				CreateDescription: `Timeout for server creation. Default: 30 minutes. Example: "45m", "1h"`,
				UpdateDescription: `Timeout for server update (reinstall operations). Default: 30 minutes. Example: "60m", "1h30m"`,
			}),
		},
	}
}

func (r *ServerResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}

	// Detect if we're in apply phase: ClientCapabilities.DeferralAllowed is true during apply
	// and false during standalone plan. We use this to suppress duplicate warnings.
	isApplyPhase := req.ClientCapabilities.DeferralAllowed

	var cfg, plan, state ServerResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if !req.State.Raw.IsNull() {
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	// Both plan-time guards below judge values, so they run only against what
	// this plan creates or changes. Applied to unchanged state they would
	// re-validate data the API already holds and accepted, which is unfixable
	// from the configuration and blocks `terraform plan` for imported servers.
	isCreate := req.State.Raw.IsNull()

	// Plan-time guard: operating_system = "ipxe" requires the ipxe attribute.
	// Catches misconfiguration before any API call (API otherwise returns 422).
	if err := requiresIpxeAttributeOnChange(isCreate, state.OperatingSystem, state.Ipxe, plan.OperatingSystem, plan.Ipxe); err != nil {
		resp.Diagnostics.AddAttributeError(
			path.Root("ipxe"),
			"Missing iPXE script",
			err.Error(),
		)
		return
	}

	// Plan-time guard: hostname shape. Lives here rather than in the schema
	// because schema validators only ever see the configuration.
	if err := validateHostnameOnChange(isCreate, state.Hostname, plan.Hostname); err != nil {
		resp.Diagnostics.AddAttributeError(
			path.Root("hostname"),
			"Invalid hostname",
			err.Error(),
		)
		return
	}

	// Resolve the planned user_data_content_hash from the in-run cache (populated
	// by latitudesh_user_data.ModifyPlan) or via a direct API GET. This is what
	// surfaces user_data content changes to the reinstall logic in a single apply
	// without the customer having to wire the hash explicitly. When user_data is
	// unknown (e.g. referencing a not-yet-created user_data) the field stays
	// "(known after apply)" and falls through to normal computed propagation.
	if !plan.UserData.IsNull() && !plan.UserData.IsUnknown() {
		liveHash := r.resolveUserDataContentHash(ctx, plan.UserData.ValueString())
		if liveHash != "" {
			plan.UserDataContentHash = types.StringValue(liveHash)
			resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
			if resp.Diagnostics.HasError() {
				return
			}
		}
	}

	// Centralize reinstall diagnostics here (only during plan, not during apply)
	if !req.State.Raw.IsNull() && !isApplyPhase {
		// Resolve effective allow_reinstall from the planned value (UseStateForUnknown
		// already collapsed cfg/state). Default is false.
		allowReinstall := false
		if !plan.AllowReinstall.IsNull() && !plan.AllowReinstall.IsUnknown() {
			allowReinstall = plan.AllowReinstall.ValueBool()
		}

		// Extract allowed_reinstall_triggers (nil/empty when unset).
		var allowedTriggers []string
		allowedTriggersSet := false
		if !plan.AllowedReinstallTriggers.IsNull() && !plan.AllowedReinstallTriggers.IsUnknown() {
			allowedTriggersSet = true
			resp.Diagnostics.Append(plan.AllowedReinstallTriggers.ElementsAs(ctx, &allowedTriggers, false)...)
			if resp.Diagnostics.HasError() {
				return
			}
		}

		// Reinstall-only triggers: API has no in-place path for these.
		var reinstallOnlyReasons []string

		if !state.OperatingSystem.IsNull() && !plan.OperatingSystem.IsNull() {
			if state.OperatingSystem.ValueString() != plan.OperatingSystem.ValueString() {
				reinstallOnlyReasons = append(reinstallOnlyReasons, "operating_system")
			}
		}
		// user_data reason covers both ID change and content change (detected via
		// the content hash). Single token so the customer-facing name matches the
		// schema attribute.
		userDataIDChanged := !state.UserData.Equal(plan.UserData) && (!state.UserData.IsNull() || !plan.UserData.IsNull())
		userDataContentChanged := state.UserData.Equal(plan.UserData) &&
			!plan.UserDataContentHash.IsNull() && !plan.UserDataContentHash.IsUnknown() &&
			!state.UserDataContentHash.IsNull() &&
			state.UserDataContentHash.ValueString() != plan.UserDataContentHash.ValueString()
		if userDataIDChanged || userDataContentChanged {
			reinstallOnlyReasons = append(reinstallOnlyReasons, "user_data")
		}
		if !state.Raid.Equal(plan.Raid) && (!state.Raid.IsNull() || !plan.Raid.IsNull()) {
			reinstallOnlyReasons = append(reinstallOnlyReasons, "raid")
		}
		if diskLayoutChanged(state.DiskLayout, plan.DiskLayout) {
			reinstallOnlyReasons = append(reinstallOnlyReasons, "disk_layout")
		}
		if !state.Ipxe.Equal(plan.Ipxe) && (!state.Ipxe.IsNull() || !plan.Ipxe.IsNull()) {
			reinstallOnlyReasons = append(reinstallOnlyReasons, "ipxe")
		}
		if !state.SSHKeys.Equal(plan.SSHKeys) && (!state.SSHKeys.IsNull() || !plan.SSHKeys.IsNull()) {
			reinstallOnlyReasons = append(reinstallOnlyReasons, "ssh_keys")
		}

		// Hostname is reinstall-trigger only when allow_reinstall=true; otherwise in-place PATCH.
		hostnameChanged := !state.Hostname.IsNull() && !plan.Hostname.IsNull() && !plan.Hostname.IsUnknown() &&
			state.Hostname.ValueString() != plan.Hostname.ValueString()

		switch {
		case !allowReinstall:
			// allow_reinstall = false: hard-block all reinstall-only changes
			// regardless of allowed_reinstall_triggers.
			if len(reinstallOnlyReasons) > 0 {
				resp.Diagnostics.AddError(
					"Server Reinstall Required",
					fmt.Sprintf("%s changes require a server reinstall, but allow_reinstall is false. "+
						"Set allow_reinstall = true on this resource to allow the reinstall, or revert the change.",
						strings.Join(reinstallOnlyReasons, ", ")),
				)
				return
			}
		case allowedTriggersSet:
			// List-restricted reinstall. Reinstall-only fields not in the list
			// hard-block; hostname not in the list falls back to in-place PATCH.
			var allowedReasons, excludedReasons []string
			for _, reason := range reinstallOnlyReasons {
				if slices.Contains(allowedTriggers, reason) {
					allowedReasons = append(allowedReasons, reason)
				} else {
					excludedReasons = append(excludedReasons, reason)
				}
			}
			if len(excludedReasons) > 0 {
				resp.Diagnostics.AddError(
					"Server Reinstall Required",
					fmt.Sprintf("%s change(s) would require a reinstall, but they are not listed in allowed_reinstall_triggers (%v). "+
						"Either add them to the list or revert the change.",
						strings.Join(excludedReasons, ", "), allowedTriggers),
				)
				return
			}
			reasons := append([]string{}, allowedReasons...)
			if hostnameChanged && slices.Contains(allowedTriggers, "hostname") {
				reasons = append(reasons, "hostname")
			}
			if len(reasons) > 0 {
				resp.Diagnostics.AddWarning(
					"Server Reinstall Required",
					fmt.Sprintf("%s changes will trigger a server reinstall. All data on the server will be lost unless backed up.",
						strings.Join(reasons, ", ")),
				)
			}
		default:
			// allow_reinstall = true and no list set: all reinstall-only field
			// changes trigger reinstall (existing behavior).
			reasons := append([]string{}, reinstallOnlyReasons...)
			if hostnameChanged {
				reasons = append(reasons, "hostname")
			}
			if len(reasons) > 0 {
				resp.Diagnostics.AddWarning(
					"Server Reinstall Required",
					fmt.Sprintf("%s changes will trigger a server reinstall. All data on the server will be lost unless backed up.",
						strings.Join(reasons, ", ")),
				)
			}
		}
	}

	// Check if only the case of 'site' has changed (only for existing resources)
	if !req.State.Raw.IsNull() {
		if !cfg.Site.IsNull() && !state.Site.IsNull() {
			if strings.EqualFold(cfg.Site.ValueString(), state.Site.ValueString()) &&
				cfg.Site.ValueString() != state.Site.ValueString() {
				// Only the case changed - this is not a real change, suppress it
				resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
				return
			}
		}
	}

	// Validate billing change during plan phase
	if !req.State.Raw.IsNull() && !plan.Billing.IsNull() && !plan.Billing.IsUnknown() {
		if !state.Billing.IsNull() && !state.Billing.IsUnknown() {
			currentBilling := state.Billing.ValueString()
			newBilling := plan.Billing.ValueString()

			// Only validate if billing is actually changing
			if currentBilling != newBilling {
				if err := validators.ValidateBillingChange(currentBilling, newBilling); err != nil {
					resp.Diagnostics.AddError("Billing Change Validation Error", err.Error())
					return
				}
			}
		}
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

func (r *ServerResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.client = deps.Client
	r.defaultProject = deps.DefaultProject
	r.hashCache = deps.UserDataHashCache
}

// waitForServerReady polls the server until the operation settles. The loop
// itself lives in waitForServerStatus so the reinstall action waits identically.
func (r *ServerResource) waitForServerReady(ctx context.Context, serverID string, diags *diag.Diagnostics, operation string, configuredTimeout time.Duration, requireTransition bool) {
	waitForServerStatus(ctx, r.client, serverID, operation, configuredTimeout, requireTransition, nil, diags)
}

func (r *ServerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ServerResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// billing defaults to "monthly" at plan time via the DefaultOnCreate plan
	// modifier; guard against unknown values from unresolved references.
	if data.Billing.IsNull() || data.Billing.IsUnknown() {
		data.Billing = types.StringValue(validators.BillingMonthly)
	}

	attrs := &operations.CreateServerServersAttributes{}

	var project string
	if !data.Project.IsNull() && !data.Project.IsUnknown() && data.Project.ValueString() != "" {
		project = data.Project.ValueString()
	} else if r.defaultProject != "" {
		project = r.defaultProject
	}

	if project == "" {
		resp.Diagnostics.AddError("Missing project",
			"Set `project` on this resource or define a default in the provider block (provider `latitudesh` { project = \"...\" }).")
		return
	}
	attrs.Project = project
	data.Project = types.StringValue(project)

	// SDK v1.19.3 made project, plan, site, operating_system and hostname required
	// on the create payload — the spec caught up with what the API always enforced.
	// They are assigned unconditionally now; the schema still marks them Optional, so
	// omitting one sends the zero value and the API rejects it. Tightening the schema
	// to Required is a breaking change for existing configurations and is tracked
	// separately.
	attrs.Plan = operations.CreateServerPlan(data.Plan.ValueString())

	// Convert site to uppercase for API compatibility (case-insensitive input).
	// Keep original case in state, only uppercase for the API call.
	attrs.Site = operations.CreateServerSite(strings.ToUpper(data.Site.ValueString()))

	attrs.OperatingSystem = operations.CreateServerOperatingSystem(data.OperatingSystem.ValueString())
	attrs.Hostname = data.Hostname.ValueString()

	if !data.SSHKeys.IsNull() && !data.SSHKeys.IsUnknown() {
		var sshKeys []string
		resp.Diagnostics.Append(data.SSHKeys.ElementsAs(ctx, &sshKeys, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		attrs.SSHKeys = sshKeys
	}

	if !data.UserData.IsNull() {
		userDataValue := data.UserData.ValueString()
		attrs.UserData = &userDataValue
	}

	// disk_layout takes precedence over raid. The two are mutually exclusive
	// (enforced in ValidateConfig); when a layout is set we skip raid entirely.
	if len(data.DiskLayout) > 0 {
		attrs.DiskLayout = createServerDiskLayout(data.DiskLayout)
	} else if !data.Raid.IsNull() {
		raidValue := data.Raid.ValueString()
		raid := operations.CreateServerRaid(raidValue)
		attrs.Raid = &raid
	}

	if !data.Ipxe.IsNull() {
		ipxe := data.Ipxe.ValueString()
		attrs.Ipxe = &ipxe
	}

	// bgp_ready is a deploy-time only flag: the API accepts it solely on create
	// and never returns it, so it is sent here and never read back.
	if !data.BgpReady.IsNull() && !data.BgpReady.IsUnknown() {
		bgpReady := data.BgpReady.ValueBool()
		attrs.BgpReady = &bgpReady
	}

	if !data.Billing.IsNull() {
		billingValue := data.Billing.ValueString()
		billing := operations.CreateServerBilling(billingValue)
		attrs.Billing = &billing
	}

	createRequest := operations.CreateServerServersRequestBody{
		Data: &operations.CreateServerServersData{
			Type:       operations.CreateServerServersTypeServers,
			Attributes: attrs,
		},
	}

	result, err := r.client.Servers.Create(ctx, createRequest)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create server, got error: "+err.Error())
		return
	}

	if result.Server == nil || result.Server.Data == nil || result.Server.Data.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get server ID from response")
		return
	}

	data.ID = types.StringValue(*result.Server.Data.ID)

	if !data.Tags.IsNull() && !data.Tags.IsUnknown() {
		var tagIDs []string
		resp.Diagnostics.Append(data.Tags.ElementsAs(ctx, &tagIDs, false)...)
		if resp.Diagnostics.HasError() {
			return
		}

		if len(tagIDs) > 0 {
			err := r.validateTagIDs(ctx, tagIDs)
			if err != nil {
				resp.Diagnostics.AddError("Tag Validation Error", "Unable to validate tag IDs: "+err.Error())
				return
			}

			var hostnamePtr *string
			if !data.Hostname.IsNull() {
				hostname := data.Hostname.ValueString()
				hostnamePtr = &hostname
			}

			err = r.updateServerTags(ctx, data.ID.ValueString(), tagIDs, hostnamePtr)
			if err != nil {
				resp.Diagnostics.AddError("Tag Update Error", "Unable to update server with tags: "+err.Error())
				return
			}
		}
	}

	// Extract configured timeout with default of 30 minutes
	createTimeout, diags := data.Timeouts.Create(ctx, 30*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.waitForServerReady(ctx, data.ID.ValueString(), &resp.Diagnostics, "creation", createTimeout, false)
	if resp.Diagnostics.HasError() {
		return
	}

	// Lock before readServer so the read reflects the locked state.
	if !data.Locked.IsNull() && !data.Locked.IsUnknown() && data.Locked.ValueBool() {
		if _, err := r.client.Servers.Lock(ctx, data.ID.ValueString()); err != nil {
			resp.Diagnostics.AddError("Lock Error", "Unable to lock server: "+err.Error())
			return
		}
	}

	r.readServer(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ServerResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readServer(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ServerResourceModel
	var currentData ServerResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(req.State.Get(ctx, &currentData)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Locked servers reject modifications, so unlock first when transitioning off.
	lockAction := lockActionFor(data.Locked, currentData.Locked)
	if lockAction == "unlock" {
		if _, err := r.client.Servers.Unlock(ctx, data.ID.ValueString()); err != nil {
			resp.Diagnostics.AddError("Unlock Error", "Unable to unlock server: "+err.Error())
			return
		}
	}

	allowReinstall := true // Default to true for backward compatibility
	if !data.AllowReinstall.IsNull() && !data.AllowReinstall.IsUnknown() {
		allowReinstall = data.AllowReinstall.ValueBool()
	}

	// Determine what changed to decide between reinstall vs in-place update.
	// ModifyPlan rejects reinstall-only triggers when allow_reinstall=false,
	// so reaching the reinstall branch implies allow_reinstall=true (hostname
	// is the one trigger that depends on the flag here).
	needsReinstall, _ := r.needsReinstall(ctx, &data, &currentData, allowReinstall, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if needsReinstall {
		err := r.reinstallServer(ctx, &data, &resp.Diagnostics)
		if err != nil {
			resp.Diagnostics.AddError("Reinstall Error", "Unable to reinstall server: "+err.Error())
			return
		}

		// Extract configured timeout with default of 30 minutes
		updateTimeout, diags := data.Timeouts.Update(ctx, 30*time.Minute)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		// Wait for server to be ready after reinstall
		r.waitForServerReady(ctx, data.ID.ValueString(), &resp.Diagnostics, "reinstall", updateTimeout, true)
		if resp.Diagnostics.HasError() {
			return
		}

		// The reinstall API doesn't accept billing, tags or project. If those
		// changed in the same plan, follow up with an in-place PATCH so the
		// state matches reality after this apply (PD-6011).
		var changedProj bool
		var newProj string
		if inPlaceFieldsChanged(&data, &currentData) {
			var err error
			changedProj, newProj, err = r.updateServerInPlace(ctx, &data, &currentData, &resp.Diagnostics)
			if err != nil {
				resp.Diagnostics.AddError("Update Error", "Unable to update server: "+err.Error())
				return
			}

			r.waitForServerReady(ctx, data.ID.ValueString(), &resp.Diagnostics, "post-reinstall update", updateTimeout, false)
			if resp.Diagnostics.HasError() {
				return
			}
		}

		// Read server to get updated values after reinstall (and follow-up update, if any)
		r.readServer(ctx, &data, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}

		// readServer only writes project when state was null/unknown, so reinstate
		// the changed project explicitly (mirrors the in-place-only branch below).
		if changedProj && newProj != "" {
			data.Project = types.StringValue(newProj)
		}
	} else {
		// Performing in-place update

		// Perform in-place update for hostname, billing, tags, project changes
		changedProj, newProj, err := r.updateServerInPlace(ctx, &data, &currentData, &resp.Diagnostics)
		if err != nil {
			resp.Diagnostics.AddError("Update Error", "Unable to update server: "+err.Error())
			return
		}

		// Extract configured timeout with default of 30 minutes
		updateTimeout, diags := data.Timeouts.Update(ctx, 30*time.Minute)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		// Wait for server to be ready after in-place update
		// The API may trigger a redeployment for certain changes, so we need
		// to wait for the server to reach "on" status before reading state
		r.waitForServerReady(ctx, data.ID.ValueString(), &resp.Diagnostics, "update", updateTimeout, false)
		if resp.Diagnostics.HasError() {
			return
		}

		// Read server to get updated values after update
		r.readServer(ctx, &data, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}

		if changedProj && newProj != "" {
			data.Project = types.StringValue(newProj)
		}
	}

	r.applyEndOfUpdateLock(ctx, lockAction, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// resolveUserDataContentHash returns the SHA256 hex digest of the referenced
// user_data's `content`, preferring the in-run provider cache (populated by
// latitudesh_user_data.ModifyPlan when the user_data is being managed in the
// same Terraform run) and falling back to a direct API GET. Intended for use
// from server.ModifyPlan, where the cache carries the *planned* hash so the
// cascade is visible in the same apply.
//
// Do NOT call this from Read paths: the cache is populated mid-walk by the
// user_data resource's plan step, which on a unified Read+Plan walk runs
// *before* the dependent server's Read step. Calling it from Read would
// silently absorb the planned hash into state and hide the drift from the
// subsequent ModifyPlan comparison.
func (r *ServerResource) resolveUserDataContentHash(ctx context.Context, userDataID string) string {
	if r.hashCache != nil {
		if cached, ok := r.hashCache.Load(userDataID); ok {
			if s, isStr := cached.(string); isStr && s != "" {
				return s
			}
		}
	}
	return r.fetchUserDataContentHashFromAPI(ctx, userDataID)
}

// fetchUserDataContentHashFromAPI fetches the referenced user_data from the
// API and returns the SHA256 hex digest of its `content`, bypassing the in-run
// cache. Use this from Read paths so refresh-time state reflects the API
// state and ModifyPlan can later detect drift against the cache. Logs a
// non-fatal warning on API failures so operators can diagnose silent
// drift-tracking gaps without the plan being blocked; the framework's
// UseStateForUnknown on the receiving attribute keeps the last-known value in
// place when this returns "".
func (r *ServerResource) fetchUserDataContentHashFromAPI(ctx context.Context, userDataID string) string {
	if r.client == nil {
		return ""
	}
	result, err := r.client.UserData.Retrieve(ctx, userDataID, nil)
	if err != nil {
		tflog.Warn(ctx, "failed to fetch user_data content from API; user_data_content_hash tracking will fall back to the prior state value for this plan",
			map[string]any{"user_data_id": userDataID, "error": err.Error()})
		return ""
	}
	if result == nil || result.UserDataObject == nil ||
		result.UserDataObject.Data == nil || result.UserDataObject.Data.Attributes == nil ||
		result.UserDataObject.Data.Attributes.Content == nil {
		tflog.Warn(ctx, "user_data API response missing content; user_data_content_hash tracking will fall back to the prior state value for this plan",
			map[string]any{"user_data_id": userDataID})
		return ""
	}
	return computeContentHash(*result.UserDataObject.Data.Attributes.Content)
}

// requiresIpxeAttribute returns an error when operating_system is the literal
// "ipxe" but the ipxe attribute is null or empty. The Latitude API rejects such
// reinstall requests with a 422 ("ipxe must be informed"); failing the plan
// here surfaces the misconfiguration before any API call is made.
//
// Unknown values are skipped — the check re-runs once Terraform resolves them.
func requiresIpxeAttribute(operatingSystem, ipxe types.String) error {
	if operatingSystem.IsNull() || operatingSystem.IsUnknown() {
		return nil
	}
	if operatingSystem.ValueString() != "ipxe" {
		return nil
	}
	if ipxe.IsUnknown() {
		return nil
	}
	if ipxe.IsNull() || ipxe.ValueString() == "" {
		return fmt.Errorf("operating_system = \"ipxe\" requires the ipxe attribute to be set (URL or base64-encoded script)")
	}
	return nil
}

// requiresIpxeAttributeOnChange applies requiresIpxeAttribute only to values
// this plan creates or changes.
//
// Servers provisioned outside Terraform — Tinkerbell ones, for example — report
// operating_system = "ipxe" with no script of their own, and the API is content
// with that. Re-asserting the rule against unchanged state made `terraform plan`
// fail on every such imported server with no config that could fix it, since the
// missing value lives in the API rather than in the configuration.
//
// A reinstall triggered by some other field on a script-less ipxe server still
// reaches the API's own 422; the provider does not try to pre-empt that, because
// it cannot tell that case apart from a deploy the API would have accepted.
func requiresIpxeAttributeOnChange(isCreate bool, stateOS, stateIpxe, planOS, planIpxe types.String) error {
	if !isCreate && stateOS.Equal(planOS) && stateIpxe.Equal(planIpxe) {
		return nil
	}
	return requiresIpxeAttribute(planOS, planIpxe)
}

// validateHostnameOnChange runs the hostname rules only when this plan creates
// the server or changes its hostname.
//
// The rules describe what the provider will send to the API, not what the API
// is holding: accounts contain servers whose hostnames predate them (spaces,
// ampersands, more than 32 characters). Because `hostname` is Required, such a
// value has to be written into the configuration to import the server at all,
// so validating it on an unchanged value left renaming production as the only
// way to get a clean plan. These rules therefore cannot live in the schema —
// schema validators run in ValidateResourceConfig, which never sees state.
func validateHostnameOnChange(isCreate bool, stateHostname, planHostname types.String) error {
	if planHostname.IsNull() || planHostname.IsUnknown() {
		return nil
	}
	if !isCreate && stateHostname.Equal(planHostname) {
		return nil
	}
	return validators.ValidateHostname(planHostname.ValueString())
}

// inPlaceFieldsChanged reports whether billing, tags or project differ between
// the planned and current models. These are the fields that the reinstall API
// does not accept; when any of them changed in a reinstall apply, a follow-up
// PATCH is required to keep state in sync with reality.
func inPlaceFieldsChanged(planned, current *ServerResourceModel) bool {
	if !planned.Billing.IsNull() && !planned.Billing.IsUnknown() && !planned.Billing.Equal(current.Billing) {
		return true
	}
	if !planned.Project.IsNull() && !planned.Project.IsUnknown() && !planned.Project.Equal(current.Project) {
		return true
	}
	if !planned.Tags.Equal(current.Tags) {
		return true
	}
	return false
}

func lockActionFor(planned, current types.Bool) string {
	// Null/unknown planned means "no opinion" — never act on it.
	if planned.IsNull() || planned.IsUnknown() {
		return ""
	}
	want := planned.ValueBool()
	was := !current.IsNull() && !current.IsUnknown() && current.ValueBool()
	if want == was {
		return ""
	}
	if want {
		return "lock"
	}
	return "unlock"
}

func (r *ServerResource) applyEndOfUpdateLock(ctx context.Context, action string, data *ServerResourceModel, diags *diag.Diagnostics) {
	switch action {
	case "lock":
		if _, err := r.client.Servers.Lock(ctx, data.ID.ValueString()); err != nil {
			diags.AddError("Lock Error", "Unable to lock server: "+err.Error())
			return
		}
		data.Locked = types.BoolValue(true)
	case "unlock":
		data.Locked = types.BoolValue(false)
	}
}

// needsReinstall determines if server needs reinstall based on changed fields.
// hostname only triggers reinstall when allowReinstall is true; otherwise it
// stays on the in-place PATCH path. When the customer sets
// allowed_reinstall_triggers, reasons not in the list are skipped — they were
// already hard-blocked at plan time for reinstall-only fields, and hostname
// silently falls back to the PATCH path.
func (r *ServerResource) needsReinstall(ctx context.Context, planned *ServerResourceModel, current *ServerResourceModel, allowReinstall bool, diags *diag.Diagnostics) (bool, string) {
	var allowedTriggers []string
	allowedTriggersSet := false
	if !planned.AllowedReinstallTriggers.IsNull() && !planned.AllowedReinstallTriggers.IsUnknown() {
		allowedTriggersSet = true
		diags.Append(planned.AllowedReinstallTriggers.ElementsAs(ctx, &allowedTriggers, false)...)
		if diags.HasError() {
			return false, ""
		}
	}
	allowed := func(name string) bool {
		if !allowedTriggersSet {
			return true
		}
		return slices.Contains(allowedTriggers, name)
	}

	var reasons []string

	if !planned.OperatingSystem.Equal(current.OperatingSystem) && allowed("operating_system") {
		reasons = append(reasons, "operating_system")
	}

	if allowReinstall && !planned.Hostname.Equal(current.Hostname) && allowed("hostname") {
		reasons = append(reasons, "hostname")
	}

	// Compare SSH keys with proper handling of null vs empty lists
	sshKeysChanged := false
	if planned.SSHKeys.IsNull() && !current.SSHKeys.IsNull() {
		sshKeysChanged = true
	} else if !planned.SSHKeys.IsNull() && current.SSHKeys.IsNull() {
		sshKeysChanged = true
	} else if !planned.SSHKeys.IsNull() && !current.SSHKeys.IsNull() {
		sshKeysChanged = !planned.SSHKeys.Equal(current.SSHKeys)
	}

	if sshKeysChanged && allowed("ssh_keys") {
		reasons = append(reasons, "ssh_keys")
	}

	// user_data covers both ID change and content change (via the content hash
	// tracked in user_data_content_hash). Both flow under the single token.
	userDataIDChanged := !planned.UserData.Equal(current.UserData)
	userDataContentChanged := planned.UserData.Equal(current.UserData) &&
		!planned.UserDataContentHash.IsNull() && !planned.UserDataContentHash.IsUnknown() &&
		!current.UserDataContentHash.IsNull() &&
		planned.UserDataContentHash.ValueString() != current.UserDataContentHash.ValueString()
	if (userDataIDChanged || userDataContentChanged) && allowed("user_data") {
		reasons = append(reasons, "user_data")
	}

	if !planned.Raid.Equal(current.Raid) && allowed("raid") {
		reasons = append(reasons, "raid")
	}

	if diskLayoutChanged(current.DiskLayout, planned.DiskLayout) && allowed("disk_layout") {
		reasons = append(reasons, "disk_layout")
	}

	if !planned.Ipxe.Equal(current.Ipxe) && allowed("ipxe") {
		reasons = append(reasons, "ipxe")
	}

	if len(reasons) > 0 {
		return true, "Changed: " + fmt.Sprintf("%v", reasons)
	}

	return false, ""
}

func (r *ServerResource) reinstallServer(ctx context.Context, data *ServerResourceModel, diags *diag.Diagnostics) error {
	serverID := data.ID.ValueString()
	attrs := &operations.CreateServerReinstallServersAttributes{}

	if !data.OperatingSystem.IsNull() && !data.OperatingSystem.IsUnknown() {
		osValue := data.OperatingSystem.ValueString()
		if osValue != "" {
			os := operations.CreateServerReinstallServersOperatingSystem(osValue)
			attrs.OperatingSystem = &os
		}
	}

	if !data.Hostname.IsNull() && !data.Hostname.IsUnknown() {
		hostname := data.Hostname.ValueString()
		if hostname != "" {
			attrs.Hostname = &hostname
		}
	}

	if !data.SSHKeys.IsNull() && !data.SSHKeys.IsUnknown() {
		var sshKeys []string
		convertDiags := data.SSHKeys.ElementsAs(ctx, &sshKeys, false)
		diags.Append(convertDiags...)
		if !convertDiags.HasError() {
			// Always send SSH keys list during reinstall, even if empty
			// This ensures keys are removed if the list is empty
			attrs.SSHKeys = sshKeys
		}
	}

	if !data.UserData.IsNull() && !data.UserData.IsUnknown() {
		userDataValue := data.UserData.ValueString()
		if userDataValue != "" {
			attrs.UserData = &userDataValue
		}
	}

	// disk_layout takes precedence over raid and the two are mutually exclusive.
	if len(data.DiskLayout) > 0 {
		attrs.DiskLayout = reinstallDiskLayout(data.DiskLayout)
	} else if !data.Raid.IsNull() && !data.Raid.IsUnknown() {
		raidValue := data.Raid.ValueString()
		if raidValue != "" && (raidValue == "raid-0" || raidValue == "raid-1") {
			raid := operations.CreateServerReinstallServersRaid(raidValue)
			attrs.Raid = &raid
		}
	}

	if !data.Ipxe.IsNull() && !data.Ipxe.IsUnknown() {
		ipxe := data.Ipxe.ValueString()
		if ipxe != "" {
			attrs.Ipxe = &ipxe
		}
	}

	reinstallRequest := operations.CreateServerReinstallServersRequestBody{
		Data: operations.CreateServerReinstallServersData{
			Type:       operations.CreateServerReinstallServersTypeReinstalls,
			Attributes: attrs,
		},
	}

	_, err := r.client.Servers.Reinstall(ctx, serverID, reinstallRequest)
	return err
}

func (r *ServerResource) updateServerInPlace(ctx context.Context, data *ServerResourceModel, currentData *ServerResourceModel, diags *diag.Diagnostics) (bool, string, error) {
	serverID := data.ID.ValueString()
	attrs := &operations.UpdateServerServersAttributes{}

	if !data.Hostname.IsNull() {
		hostname := data.Hostname.ValueString()
		attrs.Hostname = &hostname
	} else if !currentData.Hostname.IsNull() {
		hostname := currentData.Hostname.ValueString()
		attrs.Hostname = &hostname
	}

	if !data.Billing.IsNull() && (currentData == nil || data.Billing.ValueString() != currentData.Billing.ValueString()) {
		billingValue := data.Billing.ValueString()

		// Validate billing change if we have current billing data
		if currentData != nil && !currentData.Billing.IsNull() && !currentData.Billing.IsUnknown() {
			currentBilling := currentData.Billing.ValueString()
			if err := validators.ValidateBillingChange(currentBilling, billingValue); err != nil {
				diags.AddError("Billing Change Validation Error", err.Error())
				return false, "", fmt.Errorf("billing change validation failed: %w", err)
			}
		}

		billing := operations.UpdateServerServersBilling(billingValue)
		attrs.Billing = &billing
	}

	var newProj string
	if !data.Project.IsNull() && !data.Project.IsUnknown() && data.Project.ValueString() != "" {
		newProj = data.Project.ValueString()
	} else if r.defaultProject != "" {
		newProj = r.defaultProject
	} else {
		diags.AddError("Missing project", "Define 'project' in the resource or in the provider block.")
		return false, "", fmt.Errorf("missing project")
	}

	var oldProj string
	if !currentData.Project.IsNull() && !currentData.Project.IsUnknown() {
		oldProj = currentData.Project.ValueString()
	}

	changedProj := newProj != "" && newProj != oldProj
	if changedProj {
		attrs.Project = &newProj
	}

	if !data.Tags.IsNull() && !data.Tags.IsUnknown() {
		var tagIDs []string
		convertDiags := data.Tags.ElementsAs(ctx, &tagIDs, false)
		diags.Append(convertDiags...)
		if convertDiags.HasError() {
			return false, "", fmt.Errorf("failed to convert tag IDs")
		}
		if err := r.validateTagIDs(ctx, tagIDs); err != nil {
			return false, "", fmt.Errorf("tag validation failed: %w", err)
		}
		var hostname *string
		if !data.Hostname.IsNull() {
			hv := data.Hostname.ValueString()
			hostname = &hv
		}
		if err := r.updateServerTags(ctx, serverID, tagIDs, hostname); err != nil {
			return false, "", fmt.Errorf("failed to update server tags: %w", err)
		}
	}

	updateType := operations.UpdateServerServersTypeServers
	updateRequest := operations.UpdateServerServersRequestBody{
		Data: &operations.UpdateServerServersData{
			ID:         &serverID,
			Type:       &updateType,
			Attributes: attrs,
		},
	}

	result, err := r.client.Servers.Update(ctx, serverID, updateRequest)
	if err != nil && err.Error() != "{}" {
		return changedProj, newProj, fmt.Errorf("server update failed: %w", err)
	}
	if result != nil && result.HTTPMeta.Response != nil {
		code := result.HTTPMeta.Response.StatusCode
		if code < 200 || code >= 300 {
			return changedProj, newProj, fmt.Errorf("server update failed with status code: %d", code)
		}
	}
	return changedProj, newProj, nil
}

func (r *ServerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ServerResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	serverID := data.ID.ValueString()

	_, err := r.client.Servers.Delete(ctx, serverID, nil)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			resp.Diagnostics.AddWarning("Server Already Deleted", "Server appears to have been deleted outside of Terraform")
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to delete server, got error: "+err.Error())
		return
	}
}

func (r *ServerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *ServerResource) readServer(ctx context.Context, data *ServerResourceModel, diags *diag.Diagnostics) {
	serverID := data.ID.ValueString()

	response, err := r.client.Servers.Get(ctx, serverID, nil)
	if err != nil {
		diags.AddError("Client Error", "Unable to read server, got error: "+err.Error())
		return
	}

	if response.Server == nil || response.Server.Data == nil {
		data.ID = types.StringNull()
		return
	}

	server := response.Server.Data
	if server.Attributes != nil {
		attrs := server.Attributes

		// hostname is Required, so it must never be nulled here: Terraform rejects a
		// null Required attribute after apply with "provider produced inconsistent
		// result". When the API returns nothing, keep whatever the caller already has.
		if attrs.Hostname != nil && *attrs.Hostname != "" {
			data.Hostname = types.StringValue(*attrs.Hostname)
		}

		if attrs.Status != nil {
			data.Status = types.StringValue(string(*attrs.Status))
		} else {
			data.Status = types.StringNull()
		}

		if attrs.PrimaryIpv4 != nil && *attrs.PrimaryIpv4 != "" {
			data.PrimaryIpv4 = types.StringValue(*attrs.PrimaryIpv4)
		} else {
			data.PrimaryIpv4 = types.StringNull()
		}

		if attrs.PrimaryIpv6 != nil && *attrs.PrimaryIpv6 != "" {
			data.PrimaryIpv6 = types.StringValue(*attrs.PrimaryIpv6)
		} else {
			data.PrimaryIpv6 = types.StringNull()
		}

		if attrs.Locked != nil {
			data.Locked = types.BoolValue(*attrs.Locked)
		}

		if attrs.CreatedAt != nil && *attrs.CreatedAt != "" {
			if data.CreatedAt.IsNull() || data.CreatedAt.IsUnknown() || data.CreatedAt.ValueString() == "" {
				data.CreatedAt = types.StringValue(*attrs.CreatedAt)
			}
		} else {
			if data.CreatedAt.IsNull() || data.CreatedAt.IsUnknown() || data.CreatedAt.ValueString() == "" {
				data.CreatedAt = types.StringNull()
			}
		}

		// Set operating_system from API response
		if attrs.OperatingSystem != nil && attrs.OperatingSystem.Slug != nil && *attrs.OperatingSystem.Slug != "" {
			if data.OperatingSystem.IsNull() || *attrs.OperatingSystem.Slug == data.OperatingSystem.ValueString() {
				data.OperatingSystem = types.StringValue(*attrs.OperatingSystem.Slug)
			}
		} else {
			data.OperatingSystem = types.StringNull()
		}

		// Only update project if it's not already set (e.g., during import)
		// This prevents inconsistency when user provides a slug but API returns an ID
		if attrs.Project != nil && attrs.Project.ID != nil {
			if data.Project.IsNull() || data.Project.IsUnknown() {
				data.Project = types.StringValue(*attrs.Project.ID)
			}
		}

		if attrs.Region != nil && attrs.Region.Site != nil && attrs.Region.Site.Slug != nil {
			data.Site = types.StringValue(*attrs.Region.Site.Slug)
			data.Region = types.StringValue(*attrs.Region.Site.Slug)
		} else {
			// Ensure region is set to a known value even if API doesn't return it
			// This prevents "unknown value after apply" errors
			if data.Region.IsUnknown() {
				data.Region = types.StringNull()
			}
		}

		if attrs.Plan != nil {
			if attrs.Plan.Slug != nil {
				data.Plan = types.StringValue(*attrs.Plan.Slug)
			}
			if attrs.Plan.Billing != nil {
				data.Billing = types.StringValue(*attrs.Plan.Billing)
			}
		}

		if attrs.Interfaces != nil {
			list, diags2 := buildInterfacesList(attrs.Interfaces)
			diags.Append(diags2...)
			data.Interfaces = list
		} else {
			data.Interfaces = emptyIfaces()
		}
	} else {
		data.Interfaces = emptyIfaces()
	}

	// Tags are handled separately - preserve existing tags if not returned by API
	// The server API doesn't return tags in the get response, so we don't overwrite them

	// Ensure billing is set to a known value to prevent "unknown value after apply" errors
	if data.Billing.IsUnknown() {
		data.Billing = types.StringNull()
	}

	// Read deploy config to get SSH keys, user data, raid, and ipxe
	r.readDeployConfig(ctx, data, diags)

	// Populate user_data_content_hash on the first known read (Create-time or
	// when state was previously null). On refresh of an already-applied server
	// we preserve the value stored in state so that ModifyPlan can detect
	// drift between state and the current API content on the next plan; if
	// readServer overwrote it here the drift would be silently absorbed into
	// state and the server would never see a planned reinstall.
	if !data.UserData.IsNull() && !data.UserData.IsUnknown() {
		if data.UserDataContentHash.IsNull() || data.UserDataContentHash.IsUnknown() {
			if h := r.fetchUserDataContentHashFromAPI(ctx, data.UserData.ValueString()); h != "" {
				data.UserDataContentHash = types.StringValue(h)
			}
		}
	}

	if data.UserDataContentHash.IsUnknown() {
		data.UserDataContentHash = types.StringNull()
	}
}

func (r *ServerResource) readDeployConfig(ctx context.Context, data *ServerResourceModel, diags *diag.Diagnostics) {
	serverID := data.ID.ValueString()

	response, err := r.client.Servers.GetDeployConfig(ctx, serverID)
	if err != nil {
		diags.AddError("Client Error", "Unable to read server deploy config, got error: "+err.Error())
		return
	}

	if response.DeployConfig == nil || response.DeployConfig.Data == nil || response.DeployConfig.Data.Attributes == nil {
		return
	}

	attrs := response.DeployConfig.Data.Attributes

	// Only set SSH keys if they exist in the API response
	if len(attrs.SSHKeys) > 0 {
		sshKeysList, convertDiags := types.ListValueFrom(ctx, types.StringType, attrs.SSHKeys)
		diags.Append(convertDiags...)
		if !convertDiags.HasError() {
			data.SSHKeys = sshKeysList
		}
	}
	// If no SSH keys in API response, leave data.SSHKeys as null (don't set to empty list)

	// Refresh the custom disk layout from the deploy config. The server GET does
	// not return it, but deploy_config does, so this is the read path that keeps
	// state in sync and lets imported servers populate disk_layout. The
	// API-managed filesystem is intentionally ignored (not exposed in the schema).
	if len(attrs.DiskLayout) > 0 {
		layout := make([]DiskLayoutModel, 0, len(attrs.DiskLayout))
		for _, d := range attrs.DiskLayout {
			m := DiskLayoutModel{
				Count:      types.Int64Value(d.Count),
				Role:       types.StringValue(string(d.Role)),
				RaidLevel:  types.StringNull(),
				MountPoint: types.StringNull(),
			}
			if d.RaidLevel != nil {
				m.RaidLevel = types.StringValue(string(*d.RaidLevel))
			}
			if d.MountPoint != nil {
				m.MountPoint = types.StringValue(*d.MountPoint)
			}
			layout = append(layout, m)
		}
		data.DiskLayout = layout
	} else {
		// No custom layout on the server (e.g. removed out-of-band). Clear it so
		// Terraform detects the drift instead of keeping a stale value in state.
		data.DiskLayout = nil
	}
}

func (r *ServerResource) validateTagIDs(ctx context.Context, tagIDs []string) error {
	if len(tagIDs) == 0 {
		return nil
	}

	response, err := r.client.Tags.List(ctx)
	if err != nil {
		return err
	}

	if response.CustomTags == nil || response.CustomTags.Data == nil {
		return fmt.Errorf("no tags found")
	}

	validTagIDs := make(map[string]bool)
	for _, tag := range response.CustomTags.Data {
		if tag.ID != nil {
			validTagIDs[*tag.ID] = true
		}
	}

	for _, tagID := range tagIDs {
		if !validTagIDs[tagID] {
			return fmt.Errorf("tag ID '%s' not found", tagID)
		}
	}

	return nil
}

func (r *ServerResource) updateServerTags(ctx context.Context, serverID string, tagIDs []string, hostname *string) error {
	attrs := &operations.UpdateServerServersAttributes{
		Tags: tagIDs,
	}

	// Preserve hostname if provided
	if hostname != nil {
		attrs.Hostname = hostname
	}

	updateType := operations.UpdateServerServersTypeServers
	updateRequest := operations.UpdateServerServersRequestBody{
		Data: &operations.UpdateServerServersData{
			ID:         &serverID,
			Type:       &updateType,
			Attributes: attrs,
		},
	}

	result, err := r.client.Servers.Update(ctx, serverID, updateRequest)
	if err != nil {
		if err.Error() != "{}" {
			return fmt.Errorf("unable to update server with tags: %w", err)
		}
	}

	if result != nil && result.HTTPMeta.Response != nil {
		statusCode := result.HTTPMeta.Response.StatusCode
		if statusCode >= 400 {
			return fmt.Errorf("server tag update failed with status code: %d", statusCode)
		}
	}

	return nil
}

// ValidateConfig enforces that disk_layout is not combined with raid or ipxe.
// All describe the disk/boot configuration and the API accepts only one.
func (r *ServerResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data ServerResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if len(data.DiskLayout) > 0 {
		if !data.Raid.IsNull() && !data.Raid.IsUnknown() {
			resp.Diagnostics.AddError(
				"Conflicting disk configuration",
				"raid and disk_layout are mutually exclusive. Set only one of them.",
			)
		}
		if !data.Ipxe.IsNull() && !data.Ipxe.IsUnknown() {
			resp.Diagnostics.AddError(
				"Conflicting disk configuration",
				"ipxe and disk_layout are mutually exclusive. Set only one of them.",
			)
		}
		validateDiskLayoutGroups(data.DiskLayout, &resp.Diagnostics)
	}
}

// validateDiskLayoutGroups surfaces the disk_layout structural rules at plan
// time instead of letting them fail at apply with an API 422. It mirrors the
// API validation: exactly one os group, at most one storage group, per-group
// field placement, and count constraints. Checks that depend on unknown
// (interpolated) values are skipped so they don't produce false errors.
func validateDiskLayoutGroups(groups []DiskLayoutModel, diags *diag.Diagnostics) {
	osCount, storageCount := 0, 0
	rolesKnown := true

	for i, d := range groups {
		hasRaid := !d.RaidLevel.IsNull() && !d.RaidLevel.IsUnknown()
		hasMount := !d.MountPoint.IsNull() && !d.MountPoint.IsUnknown() && d.MountPoint.ValueString() != ""
		countKnown := !d.Count.IsNull() && !d.Count.IsUnknown()

		if countKnown && d.Count.ValueInt64() < 1 {
			diags.AddError("Invalid disk_layout", fmt.Sprintf("disk_layout[%d]: count must be >= 1.", i))
		}
		if hasRaid && countKnown && d.Count.ValueInt64() < 2 {
			diags.AddError("Invalid disk_layout", fmt.Sprintf("disk_layout[%d]: raid_level requires count >= 2.", i))
		}

		if d.Role.IsUnknown() {
			rolesKnown = false
			continue
		}
		switch d.Role.ValueString() {
		case "os":
			osCount++
			if hasMount {
				diags.AddError("Invalid disk_layout", fmt.Sprintf("disk_layout[%d]: mount_point is not allowed on role \"os\".", i))
			}
		case "storage":
			storageCount++
			if !hasMount {
				diags.AddError("Invalid disk_layout", fmt.Sprintf("disk_layout[%d]: mount_point is required on role \"storage\".", i))
			}
		case "raw":
			if hasRaid {
				diags.AddError("Invalid disk_layout", fmt.Sprintf("disk_layout[%d]: raid_level is not allowed on role \"raw\".", i))
			}
			if hasMount {
				diags.AddError("Invalid disk_layout", fmt.Sprintf("disk_layout[%d]: mount_point is not allowed on role \"raw\".", i))
			}
		}
	}

	if rolesKnown && osCount != 1 {
		diags.AddError("Invalid disk_layout", "disk_layout must contain exactly one group with role \"os\".")
	}
	if rolesKnown && storageCount > 1 {
		diags.AddError("Invalid disk_layout", "disk_layout must contain at most one group with role \"storage\".")
	}
}

// diskLayoutChanged reports whether two disk layouts differ. Used to drive the
// reinstall-trigger detection for the disk_layout attribute. nil and empty are
// treated as equal (both mean "no custom layout").
func diskLayoutChanged(a, b []DiskLayoutModel) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if !a[i].Count.Equal(b[i].Count) ||
			!a[i].Role.Equal(b[i].Role) ||
			!a[i].RaidLevel.Equal(b[i].RaidLevel) ||
			!a[i].MountPoint.Equal(b[i].MountPoint) {
			return true
		}
	}
	return false
}

// createServerDiskLayout maps the Terraform disk_layout model to the SDK create
// request entry struct. count and role are required; the rest are optional.
func createServerDiskLayout(in []DiskLayoutModel) []operations.CreateServerDiskLayout {
	out := make([]operations.CreateServerDiskLayout, 0, len(in))
	for _, d := range in {
		entry := operations.CreateServerDiskLayout{
			Count: d.Count.ValueInt64(),
			Role:  operations.CreateServerServersRole(d.Role.ValueString()),
		}
		if !d.RaidLevel.IsNull() && !d.RaidLevel.IsUnknown() {
			rl := operations.CreateServerRaidLevel(d.RaidLevel.ValueString())
			entry.RaidLevel = &rl
		}
		if !d.MountPoint.IsNull() && !d.MountPoint.IsUnknown() && d.MountPoint.ValueString() != "" {
			mp := d.MountPoint.ValueString()
			entry.MountPoint = &mp
		}
		out = append(out, entry)
	}
	return out
}

// reinstallDiskLayout maps the Terraform disk_layout model to the SDK reinstall
// request entry struct. count and role are required; the rest are optional.
func reinstallDiskLayout(in []DiskLayoutModel) []operations.CreateServerReinstallServersDiskLayout {
	out := make([]operations.CreateServerReinstallServersDiskLayout, 0, len(in))
	for _, d := range in {
		entry := operations.CreateServerReinstallServersDiskLayout{
			Count: d.Count.ValueInt64(),
			Role:  operations.CreateServerReinstallServersRole(d.Role.ValueString()),
		}
		if !d.RaidLevel.IsNull() && !d.RaidLevel.IsUnknown() {
			rl := operations.CreateServerReinstallServersRaidLevel(d.RaidLevel.ValueString())
			entry.RaidLevel = &rl
		}
		if !d.MountPoint.IsNull() && !d.MountPoint.IsUnknown() && d.MountPoint.ValueString() != "" {
			mp := d.MountPoint.ValueString()
			entry.MountPoint = &mp
		}
		out = append(out, entry)
	}
	return out
}
