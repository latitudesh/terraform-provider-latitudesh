package latitudesh

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
)

// The unit-test step in CI runs `-run "TestProvider|TestFrameworkProvider"`, so
// the names below carry the TestProvider prefix on purpose.

func TestProviderRegistersVirtualMachinePowerAction(t *testing.T) {
	ctx := context.Background()

	p, ok := New("test")().(provider.ProviderWithActions)
	if !ok {
		t.Fatal("provider does not implement provider.ProviderWithActions")
	}

	var names []string
	for _, newAction := range p.Actions(ctx) {
		var resp action.MetadataResponse
		newAction().Metadata(ctx, action.MetadataRequest{ProviderTypeName: "latitudesh"}, &resp)
		names = append(names, resp.TypeName)
	}

	want := "latitudesh_virtual_machine_power"
	for _, name := range names {
		if name == want {
			return
		}
	}
	t.Fatalf("action %q is not registered; got %v", want, names)
}

func TestProviderVirtualMachinePowerActionSchema(t *testing.T) {
	ctx := context.Background()

	var resp action.SchemaResponse
	NewVirtualMachinePowerAction().Schema(ctx, action.SchemaRequest{}, &resp)

	if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("schema is not implementable: %v", diags)
	}

	for _, name := range []string{"virtual_machine_id", "power_action"} {
		attr, ok := resp.Schema.Attributes[name]
		if !ok {
			t.Fatalf("schema has no %s attribute", name)
		}
		if !attr.IsRequired() {
			t.Errorf("%s must be required: an action has no state to fall back on", name)
		}
	}

	for _, name := range []string{"wait_for_status", "wait_timeout"} {
		attr, ok := resp.Schema.Attributes[name]
		if !ok {
			t.Fatalf("schema has no %s attribute", name)
		}
		if attr.IsRequired() {
			t.Errorf("%s must be optional", name)
		}
	}
}

func TestProviderVMPowerActionTargetStatus(t *testing.T) {
	tests := []struct {
		action operations.CreateVirtualMachineActionVirtualMachinesAction
		want   string
	}{
		{operations.CreateVirtualMachineActionVirtualMachinesActionPowerOn, "Running"},
		{operations.CreateVirtualMachineActionVirtualMachinesActionPowerOff, "Stopped"},
	}
	for _, tc := range tests {
		if got := vmPowerActionTargetStatus(tc.action); got != tc.want {
			t.Errorf("vmPowerActionTargetStatus(%s) = %q, want %q", tc.action, got, tc.want)
		}
	}
}

// A VM restart ends at the status it started from, so explicitly asking it to
// wait must warn, and the quiet defaults must not.
func TestProviderVMRebootWaitWarning(t *testing.T) {
	tests := []struct {
		name        string
		data        VirtualMachinePowerActionModel
		wantWarning bool
	}{
		{
			"bare reboot stays quiet",
			VirtualMachinePowerActionModel{PowerAction: types.StringValue("reboot")},
			false,
		},
		{
			"reboot with explicit wait_for_status warns",
			VirtualMachinePowerActionModel{PowerAction: types.StringValue("reboot"), WaitForStatus: types.BoolValue(true)},
			true,
		},
		{
			"reboot with explicit wait_timeout warns",
			VirtualMachinePowerActionModel{PowerAction: types.StringValue("reboot"), WaitTimeout: types.StringValue("5m")},
			true,
		},
		{
			"power_off with explicit wait stays quiet",
			VirtualMachinePowerActionModel{PowerAction: types.StringValue("power_off"), WaitForStatus: types.BoolValue(true)},
			false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			message := vmRebootWaitWarning(&tc.data)
			if tc.wantWarning && message == "" {
				t.Error("expected a warning, got none")
			}
			if !tc.wantWarning && message != "" {
				t.Errorf("expected no warning, got %q", message)
			}
		})
	}
}
