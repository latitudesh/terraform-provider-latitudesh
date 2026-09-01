package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

// defaultVMPowerWait bounds the status poll when wait_timeout is left unset. VM
// power transitions are driven by KubeVirt and reported by event watchers, so
// they normally settle in seconds; the default only has to cover watcher
// hiccups, not slow hardware.
const defaultVMPowerWait = 10 * time.Minute

// vmStatusStopped and vmStatusFailed complete the status vocabulary the power
// wait needs on top of vmStatusRunning (declared with the VM resource). The API
// serializes the full set as Running, Starting, Stopped, Stopping, Failed.
const (
	vmStatusStopped = "Stopped"
	vmStatusFailed  = "Failed"
)

// vmPowerPollInterval is how long waitForVirtualMachineStatus sleeps between
// polls. A variable rather than a constant only so tests can drive a scripted
// status sequence in milliseconds; nothing in the provider changes it at
// runtime.
var vmPowerPollInterval = 10 * time.Second

var (
	_ action.Action                   = &VirtualMachinePowerAction{}
	_ action.ActionWithConfigure      = &VirtualMachinePowerAction{}
	_ action.ActionWithValidateConfig = &VirtualMachinePowerAction{}
)

func NewVirtualMachinePowerAction() action.Action {
	return &VirtualMachinePowerAction{}
}

// VirtualMachinePowerAction runs a power action (power_on, power_off, reboot)
// on an existing virtual machine. power_on and power_off wait for the status
// the action drives the machine to (Running / Stopped); a reboot tears the
// instance down and brings it back to the status it started from, so it cannot
// be waited on and returns once the API accepts the request.
type VirtualMachinePowerAction struct {
	client *latitudeshgosdk.Latitudesh
}

type VirtualMachinePowerActionModel struct {
	VirtualMachineID types.String `tfsdk:"virtual_machine_id"`
	PowerAction      types.String `tfsdk:"power_action"`
	WaitForStatus    types.Bool   `tfsdk:"wait_for_status"`
	WaitTimeout      types.String `tfsdk:"wait_timeout"`
}

func (a *VirtualMachinePowerAction) Metadata(ctx context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_machine_power"
}

func (a *VirtualMachinePowerAction) Schema(ctx context.Context, req action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Runs a power action on a virtual machine: `power_on`, `power_off`, or `reboot`. " +
			"`power_on` and `power_off` wait until the machine reports `Running` / `Stopped`; `reboot` returns as " +
			"soon as the API accepts the request. The `latitudesh_virtual_machine` resource has no attribute " +
			"describing power state, so invoking this action never drifts the resource.",
		Attributes: map[string]schema.Attribute{
			"virtual_machine_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the virtual machine to act on.",
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
				MarkdownDescription: "Wait until the virtual machine reports the status the action drives it to: " +
					"`Running` for `power_on`, `Stopped` for `power_off`. Defaults to `true`; set to `false` to " +
					"return as soon as the API accepts the action. Has no effect for `reboot`: a restart ends at " +
					"the status it started from, so there is no target a wait could distinguish.",
				Optional: true,
			},
			"wait_timeout": schema.StringAttribute{
				MarkdownDescription: "How long to wait for the target status, as a Go duration (for example `5m`). " +
					"Defaults to `10m`. Ignored when `wait_for_status` is `false` and for `reboot`.",
				Optional: true,
			},
		},
	}
}

func (a *VirtualMachinePowerAction) Configure(ctx context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	a.client = deps.Client
}

func (a *VirtualMachinePowerAction) ValidateConfig(ctx context.Context, req action.ValidateConfigRequest, resp *action.ValidateConfigResponse) {
	var data VirtualMachinePowerActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := actionWaitTimeout(data.WaitTimeout, defaultVMPowerWait); err != nil {
		resp.Diagnostics.AddAttributeError(
			path.Root("wait_timeout"),
			"Invalid Wait Timeout",
			err.Error(),
		)
	}

	if message := vmRebootWaitWarning(&data); message != "" {
		resp.Diagnostics.AddWarning("Reboot Cannot Be Waited On", message)
	}
}

// vmRebootWaitWarning returns a warning when the config explicitly asks a
// reboot to wait. A restart tears the instance down and brings it back to the
// status it started from (Running), so a status wait has no target that
// distinguishes "restarted" from "never went down". Only explicit wait
// attributes warn; a bare reboot stays quiet because returning on acceptance is
// its documented behavior.
func vmRebootWaitWarning(data *VirtualMachinePowerActionModel) string {
	if data.PowerAction.IsNull() || data.PowerAction.IsUnknown() || data.PowerAction.ValueString() != "reboot" {
		return ""
	}
	if !explicitWaitRequested(data.WaitForStatus, data.WaitTimeout) {
		return ""
	}

	return "wait_for_status and wait_timeout have no effect for reboot: a restart ends at the status the virtual " +
		"machine started from, so there is nothing a wait could observe. The action returns as soon as the API " +
		"accepts the request."
}

func (a *VirtualMachinePowerAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var data VirtualMachinePowerActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	timeout, err := actionWaitTimeout(data.WaitTimeout, defaultVMPowerWait)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Wait Timeout", err.Error())
		return
	}

	vmID := data.VirtualMachineID.ValueString()
	powerAction := operations.CreateVirtualMachineActionVirtualMachinesAction(data.PowerAction.ValueString())

	resp.SendProgress(action.InvokeProgressEvent{
		Message: fmt.Sprintf("Requesting %s on virtual machine %s", powerAction, vmID),
	})

	_, err = a.client.VirtualMachines.CreateVirtualMachineAction(ctx, vmID,
		operations.CreateVirtualMachineActionVirtualMachinesRequestBody{
			ID:   vmID,
			Type: operations.CreateVirtualMachineActionVirtualMachinesTypeVirtualMachines,
			Attributes: operations.CreateVirtualMachineActionVirtualMachinesAttributes{
				Action: powerAction,
			},
		})
	if err != nil {
		resp.Diagnostics.AddError(
			"Virtual Machine Power Action Error",
			fmt.Sprintf("Unable to run %s on virtual machine %s: %s", powerAction, vmID, err.Error()),
		)
		return
	}

	// A restart cannot be waited on: it ends at the status the machine started
	// from, so polling could not tell a finished restart from one that has not
	// begun. The intermediate Stopped/Starting readings are too short-lived to
	// anchor on without risking a false timeout.
	if powerAction == operations.CreateVirtualMachineActionVirtualMachinesActionReboot {
		resp.SendProgress(action.InvokeProgressEvent{
			Message: fmt.Sprintf("Reboot of virtual machine %s accepted. A restart ends at the status it started "+
				"from, so the action returns without waiting.", vmID),
		})
		return
	}

	if !data.WaitForStatus.IsNull() && !data.WaitForStatus.IsUnknown() && !data.WaitForStatus.ValueBool() {
		resp.Diagnostics.AddWarning(
			"Power Action Not Awaited",
			fmt.Sprintf("The API accepted %s on virtual machine %s. With wait_for_status = false the action "+
				"returns before the machine reaches its target status, so anything downstream may run against a "+
				"machine still in transition.", powerAction, vmID),
		)
		return
	}

	waitForVirtualMachineStatus(ctx, a.client, vmID, string(powerAction), vmPowerActionTargetStatus(powerAction),
		timeout, func(message string) {
			resp.SendProgress(action.InvokeProgressEvent{Message: message})
		}, &resp.Diagnostics)
}

// vmPowerActionTargetStatus is the status a successful power action leaves the
// virtual machine in. Reboot never reaches the wait, so only power_on and
// power_off consult this.
func vmPowerActionTargetStatus(powerAction operations.CreateVirtualMachineActionVirtualMachinesAction) string {
	if powerAction == operations.CreateVirtualMachineActionVirtualMachinesActionPowerOff {
		return vmStatusStopped
	}
	return vmStatusRunning
}

// waitForVirtualMachineStatus polls a virtual machine until it reaches
// targetStatus, appending a diagnostic when it fails or the wait runs out. The
// status the API serves is pushed by KubeVirt event watchers, so the first
// polls typically still read the pre-action status; Failed is terminal.
//
// The transient-versus-fatal error split mirrors the VM resource's
// waitForVMReady: 404 and 5xx keep polling, other API errors fail immediately.
func waitForVirtualMachineStatus(
	ctx context.Context,
	client *latitudeshgosdk.Latitudesh,
	vmID string,
	operation string,
	targetStatus string,
	configuredTimeout time.Duration,
	progress func(message string),
	diags *diag.Diagnostics,
) {
	const maxConsecutiveErrors = 5

	deadline := time.Now().Add(configuredTimeout)
	lastStatus := ""
	consecutiveErrors := 0

	for time.Now().Before(deadline) {
		result, err := client.VirtualMachines.Get(ctx, vmID, nil)
		if err != nil {
			var apiErr *components.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode != http.StatusNotFound && apiErr.StatusCode < 500 {
				diags.AddError("Client Error",
					fmt.Sprintf("Unable to check virtual machine status during %s: %s", operation, err.Error()))
				return
			}
			consecutiveErrors++
			if consecutiveErrors >= maxConsecutiveErrors {
				diags.AddError("Client Error",
					fmt.Sprintf("Unable to check virtual machine status during %s after %d consecutive attempts, last error: %s",
						operation, consecutiveErrors, err.Error()))
				return
			}
		} else {
			consecutiveErrors = 0

			if result.VirtualMachine != nil && result.VirtualMachine.Data != nil &&
				result.VirtualMachine.Data.Attributes != nil && result.VirtualMachine.Data.Attributes.Status != nil {
				status := *result.VirtualMachine.Data.Attributes.Status

				if status != lastStatus && progress != nil {
					progress(fmt.Sprintf("Virtual machine %s: status %s", operation, status))
				}
				lastStatus = status

				if status == targetStatus {
					return
				}
				if status == vmStatusFailed {
					diags.AddError(
						fmt.Sprintf("Virtual Machine %s Failed", operation),
						fmt.Sprintf("Virtual machine %s entered status %q during %s. Please check it in the "+
							"Latitude.sh dashboard.", vmID, status, operation),
					)
					return
				}
			}
		}

		select {
		case <-ctx.Done():
			diags.AddError("Context Cancelled", fmt.Sprintf("Virtual machine %s was cancelled", operation))
			return
		case <-time.After(vmPowerPollInterval):
		}
	}

	diags.AddError(
		fmt.Sprintf("Virtual Machine %s Timeout", operation),
		fmt.Sprintf("Virtual machine did not reach %q within %v (last status: %q). Check it in the Latitude.sh "+
			"dashboard.", targetStatus, configuredTimeout, lastStatus),
	)
}
