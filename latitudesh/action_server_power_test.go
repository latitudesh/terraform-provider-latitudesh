package latitudesh

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
)

// The unit-test step in CI runs `-run "TestProvider|TestFrameworkProvider"`, so
// the names below carry the TestProvider prefix on purpose.

func TestProviderRegistersServerPowerAction(t *testing.T) {
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

	want := "latitudesh_server_power"
	for _, name := range names {
		if name == want {
			return
		}
	}
	t.Fatalf("action %q is not registered; got %v", want, names)
}

func TestProviderServerPowerActionSchema(t *testing.T) {
	ctx := context.Background()

	var resp action.SchemaResponse
	NewServerPowerAction().Schema(ctx, action.SchemaRequest{}, &resp)

	if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("schema is not implementable: %v", diags)
	}

	for _, name := range []string{"server_id", "power_action"} {
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

func TestProviderPowerActionTargetStatus(t *testing.T) {
	tests := []struct {
		action operations.CreateServerActionAction
		want   string
	}{
		{operations.CreateServerActionActionPowerOn, "on"},
		{operations.CreateServerActionActionPowerOff, "off"},
	}
	for _, tc := range tests {
		if got := powerActionTargetStatus(tc.action); got != tc.want {
			t.Errorf("powerActionTargetStatus(%s) = %q, want %q", tc.action, got, tc.want)
		}
	}
}

// A reboot cannot be waited on (the API reports "on" throughout a warm reset),
// so explicitly asking it to wait must warn, and the quiet defaults must not.
func TestProviderRebootWaitWarning(t *testing.T) {
	tests := []struct {
		name        string
		data        ServerPowerActionModel
		wantWarning bool
	}{
		{
			"bare reboot stays quiet",
			ServerPowerActionModel{PowerAction: types.StringValue("reboot")},
			false,
		},
		{
			"reboot with explicit wait_for_status warns",
			ServerPowerActionModel{PowerAction: types.StringValue("reboot"), WaitForStatus: types.BoolValue(true)},
			true,
		},
		{
			"reboot with wait_for_status = false stays quiet",
			ServerPowerActionModel{PowerAction: types.StringValue("reboot"), WaitForStatus: types.BoolValue(false)},
			false,
		},
		{
			"reboot with explicit wait_timeout warns",
			ServerPowerActionModel{PowerAction: types.StringValue("reboot"), WaitTimeout: types.StringValue("10m")},
			true,
		},
		{
			"power_on with explicit wait stays quiet",
			ServerPowerActionModel{PowerAction: types.StringValue("power_on"), WaitForStatus: types.BoolValue(true)},
			false,
		},
		{
			"unknown power_action stays quiet",
			ServerPowerActionModel{PowerAction: types.StringUnknown(), WaitForStatus: types.BoolValue(true)},
			false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			message := rebootWaitWarning(&tc.data)
			if tc.wantWarning && message == "" {
				t.Error("expected a warning, got none")
			}
			if !tc.wantWarning && message != "" {
				t.Errorf("expected no warning, got %q", message)
			}
		})
	}
}

func TestProviderActionWaitTimeout(t *testing.T) {
	fallback := 15 * time.Minute

	tests := []struct {
		name       string
		configured types.String
		want       time.Duration
		wantErr    bool
	}{
		{"unset falls back to the default", types.StringNull(), fallback, false},
		{"unknown falls back to the default", types.StringUnknown(), fallback, false},
		{"empty string falls back to the default", types.StringValue(""), fallback, false},
		{"explicit duration is honored", types.StringValue("10m"), 10 * time.Minute, false},
		{"unparseable duration is rejected", types.StringValue("soon"), 0, true},
		{"zero is rejected", types.StringValue("0s"), 0, true},
		{"negative is rejected", types.StringValue("-5m"), 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := actionWaitTimeout(tc.configured, fallback)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("actionWaitTimeout(%v) = %v, want an error", tc.configured, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("actionWaitTimeout(%v) returned %v", tc.configured, err)
			}
			if got != tc.want {
				t.Errorf("actionWaitTimeout(%v) = %v, want %v", tc.configured, got, tc.want)
			}
		})
	}
}

// The wait must not require a transition for any power action: a reboot that
// completes between two polls reads "on" from start to finish, and requiring a
// transition would turn that into a false timeout.
func TestProviderPowerOffTargetIsTerminal(t *testing.T) {
	terminal, success := isServerStatusTerminal("off", "on", "off", true)
	if !terminal || !success {
		t.Fatalf(`isServerStatusTerminal("off", "on", "off", true) = (%v, %v), want (true, true)`, terminal, success)
	}

	// "on" is not terminal while the wait targets "off": the server has not
	// started powering down yet.
	terminal, _ = isServerStatusTerminal("off", "on", "on", true)
	if terminal {
		t.Fatal(`isServerStatusTerminal("off", "on", "on", true) treated the starting status as terminal`)
	}

	// Failure states stay terminal whatever the target.
	terminal, success = isServerStatusTerminal("off", "on", "failed_deployment", true)
	if !terminal || success {
		t.Fatalf(`isServerStatusTerminal("off", "on", "failed_deployment", true) = (%v, %v), want (true, false)`, terminal, success)
	}
}
