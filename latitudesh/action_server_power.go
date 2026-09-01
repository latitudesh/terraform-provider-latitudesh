package latitudesh

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

// defaultPowerWait bounds the status poll when wait_timeout is left unset. Power
// transitions are far quicker than a reinstall, but bare metal can still take
// several minutes to come back up, so the default stays generous.
const defaultPowerWait = 15 * time.Minute

var (
	_ action.Action                   = &ServerPowerAction{}
	_ action.ActionWithConfigure      = &ServerPowerAction{}
	_ action.ActionWithValidateConfig = &ServerPowerAction{}
)

func NewServerPowerAction() action.Action {
	return &ServerPowerAction{}
}

// ServerPowerAction runs a power action (power_on, power_off, reboot) on an
// existing server. The server resource has no attribute that expresses "off" or
// "rebooted", so these operations have no declarative home: the action is the
// only way to drive them from Terraform without touching desired state.
type ServerPowerAction struct {
	client *latitudeshgosdk.Latitudesh
}

type ServerPowerActionModel struct {
	ServerID      types.String `tfsdk:"server_id"`
	PowerAction   types.String `tfsdk:"power_action"`
	WaitForStatus types.Bool   `tfsdk:"wait_for_status"`
	WaitTimeout   types.String `tfsdk:"wait_timeout"`
}

func (a *ServerPowerAction) Metadata(ctx context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server_power"
}

func (a *ServerPowerAction) Schema(ctx context.Context, req action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Runs a power action on a server: `power_on`, `power_off`, or `reboot`. " +
			"The `latitudesh_server` resource has no attribute describing power state, so invoking this action " +
			"never drifts the resource.",
		Attributes: map[string]schema.Attribute{
			"server_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the server to act on.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"power_action": schema.StringAttribute{
				MarkdownDescription: "The power action to run: `power_on`, `power_off`, or `reboot`.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("power_on", "power_off", "reboot"),
				},
			},
			"wait_for_status": schema.BoolAttribute{
				MarkdownDescription: "Wait until the server reports the status the action drives it to: `on` for " +
					"`power_on`, `off` for `power_off`. Defaults to `true`; set to `false` to return as soon as the " +
					"API accepts the action. Has no effect for `reboot`: a reboot is a warm reset, the server's " +
					"status reads `on` throughout, and the API offers nothing to wait on.",
				Optional: true,
			},
			"wait_timeout": schema.StringAttribute{
				MarkdownDescription: "How long to wait for the target status, as a Go duration (for example `10m`). " +
					"Defaults to `15m`. Ignored when `wait_for_status` is `false` and for `reboot`.",
				Optional: true,
			},
		},
	}
}

func (a *ServerPowerAction) Configure(ctx context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	a.client = deps.Client
}

func (a *ServerPowerAction) ValidateConfig(ctx context.Context, req action.ValidateConfigRequest, resp *action.ValidateConfigResponse) {
	var data ServerPowerActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := actionWaitTimeout(data.WaitTimeout, defaultPowerWait); err != nil {
		resp.Diagnostics.AddAttributeError(
			path.Root("wait_timeout"),
			"Invalid Wait Timeout",
			err.Error(),
		)
	}

	if message := rebootWaitWarning(&data); message != "" {
		resp.Diagnostics.AddWarning("Reboot Cannot Be Waited On", message)
	}
}

// rebootWaitWarning returns a warning when the config explicitly asks a reboot
// to wait. A reboot is a warm reset (`ipmi power reset`): chassis power never
// drops, the server's cached status reads "on" before, during, and after, and
// the API has no endpoint reporting action progress — so there is nothing a
// wait could observe. Only explicit wait attributes warn; a bare reboot stays
// quiet because returning on acceptance is its documented behavior.
func rebootWaitWarning(data *ServerPowerActionModel) string {
	if data.PowerAction.IsNull() || data.PowerAction.IsUnknown() || data.PowerAction.ValueString() != "reboot" {
		return ""
	}
	if !explicitWaitRequested(data.WaitForStatus, data.WaitTimeout) {
		return ""
	}

	return "wait_for_status and wait_timeout have no effect for reboot: the Latitude.sh API reports the server " +
		"as \"on\" throughout a reboot and exposes no action progress, so the action returns as soon as the API " +
		"accepts the request."
}

// explicitWaitRequested reports whether the config asked for a wait in so many
// words: wait_for_status set to true, or any wait_timeout at all.
func explicitWaitRequested(waitForStatus types.Bool, waitTimeout types.String) bool {
	explicitWait := !waitForStatus.IsNull() && !waitForStatus.IsUnknown() && waitForStatus.ValueBool()
	return explicitWait || isSet(waitTimeout)
}

func (a *ServerPowerAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var data ServerPowerActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	timeout, err := actionWaitTimeout(data.WaitTimeout, defaultPowerWait)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Wait Timeout", err.Error())
		return
	}

	serverID := data.ServerID.ValueString()
	powerAction := operations.CreateServerActionAction(data.PowerAction.ValueString())

	resp.SendProgress(action.InvokeProgressEvent{
		Message: fmt.Sprintf("Requesting %s on server %s", powerAction, serverID),
	})

	_, err = a.client.Servers.RunAction(ctx, serverID, operations.CreateServerActionServersRequestBody{
		Data: operations.CreateServerActionServersData{
			Type: operations.CreateServerActionServersTypeActions,
			Attributes: &operations.CreateServerActionServersAttributes{
				Action: powerAction,
			},
		},
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Server Power Action Error",
			fmt.Sprintf("Unable to run %s on server %s: %s", powerAction, serverID, err.Error()),
		)
		return
	}

	// A reboot cannot be waited on: it is a warm reset, so chassis power never
	// drops, the cached status reads "on" for its whole duration, and the API has
	// no action-progress endpoint. Polling would only ever read back the status
	// the server started with and call that success, so no poll is attempted.
	if powerAction == operations.CreateServerActionActionReboot {
		resp.SendProgress(action.InvokeProgressEvent{
			Message: fmt.Sprintf("Reboot of server %s accepted. The API does not report reboot progress, so the "+
				"action returns without waiting.", serverID),
		})
		return
	}

	if !data.WaitForStatus.IsNull() && !data.WaitForStatus.IsUnknown() && !data.WaitForStatus.ValueBool() {
		resp.Diagnostics.AddWarning(
			"Power Action Not Awaited",
			fmt.Sprintf("The API accepted %s on server %s. With wait_for_status = false the action returns before "+
				"the server reaches its target status, so anything downstream may run against a server still in "+
				"transition.", powerAction, serverID),
		)
		return
	}

	// requireTransition stays false: the target status ("on" or "off") is the
	// opposite of the state the server starts in, so the first target reading is
	// genuine — the stale-reading guard reinstalls need has nothing to guard here.
	waitForServerTargetStatus(ctx, a.client, serverID, string(powerAction), powerActionTargetStatus(powerAction),
		timeout, false, func(message string) {
			resp.SendProgress(action.InvokeProgressEvent{Message: message})
		}, &resp.Diagnostics)
}

// powerActionTargetStatus is the status a successful power action leaves the
// server in. Reboot never reaches the wait, so only power_on and power_off
// consult this.
func powerActionTargetStatus(powerAction operations.CreateServerActionAction) string {
	if powerAction == operations.CreateServerActionActionPowerOff {
		return "off"
	}
	return "on"
}

// actionWaitTimeout resolves a wait_timeout attribute, which is a plain string
// rather than the resource timeouts block: that helper only covers resources and
// data sources.
func actionWaitTimeout(configured types.String, fallback time.Duration) (time.Duration, error) {
	if configured.IsNull() || configured.IsUnknown() || configured.ValueString() == "" {
		return fallback, nil
	}
	timeout, err := time.ParseDuration(configured.ValueString())
	if err != nil {
		return 0, fmt.Errorf("%q is not a valid duration: %w", configured.ValueString(), err)
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("%q must be a positive duration", configured.ValueString())
	}
	return timeout, nil
}
