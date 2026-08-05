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
// the names below carry the TestProvider prefix on purpose: renaming them to
// something like TestServerReinstall... would silently drop them from CI.

func TestProviderRegistersServerReinstallAction(t *testing.T) {
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

	want := "latitudesh_server_reinstall"
	for _, name := range names {
		if name == want {
			return
		}
	}
	t.Fatalf("action %q is not registered; got %v", want, names)
}

func TestProviderServerReinstallActionSchema(t *testing.T) {
	ctx := context.Background()

	var resp action.SchemaResponse
	NewServerReinstallAction().Schema(ctx, action.SchemaRequest{}, &resp)

	if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("schema is not implementable: %v", diags)
	}

	serverID, ok := resp.Schema.Attributes["server_id"]
	if !ok {
		t.Fatal("schema has no server_id attribute")
	}
	if !serverID.IsRequired() {
		t.Error("server_id must be required: an action has no state to fall back on")
	}

	// Anything the practitioner leaves out has to stay out of the payload, which
	// only works while every other attribute is optional.
	for name, attr := range resp.Schema.Attributes {
		if name == "server_id" {
			continue
		}
		if attr.IsRequired() {
			t.Errorf("attribute %q is required; only server_id may be", name)
		}
	}
}

func TestProviderReinstallWaitTimeout(t *testing.T) {
	tests := []struct {
		name       string
		configured types.String
		want       time.Duration
		wantErr    bool
	}{
		{"unset falls back to the default", types.StringNull(), defaultReinstallWait, false},
		{"unknown falls back to the default", types.StringUnknown(), defaultReinstallWait, false},
		{"empty string falls back to the default", types.StringValue(""), defaultReinstallWait, false},
		{"explicit duration is honored", types.StringValue("45m"), 45 * time.Minute, false},
		{"hours are honored", types.StringValue("2h"), 2 * time.Hour, false},
		{"unparseable duration is rejected", types.StringValue("soon"), 0, true},
		{"zero is rejected", types.StringValue("0s"), 0, true},
		{"negative is rejected", types.StringValue("-5m"), 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := reinstallWaitTimeout(tc.configured)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("reinstallWaitTimeout(%v) = %v, want an error", tc.configured, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("reinstallWaitTimeout(%v) returned %v", tc.configured, err)
			}
			if got != tc.want {
				t.Errorf("reinstallWaitTimeout(%v) = %v, want %v", tc.configured, got, tc.want)
			}
		})
	}
}

func TestProviderReinstallAttributesFromAction(t *testing.T) {
	ctx := context.Background()

	stringList := func(values ...string) types.List {
		elements := make([]types.String, 0, len(values))
		for _, v := range values {
			elements = append(elements, types.StringValue(v))
		}
		list, diags := types.ListValueFrom(ctx, types.StringType, elements)
		if diags.HasError() {
			t.Fatalf("building a list value: %v", diags)
		}
		return list
	}

	t.Run("only server_id sends an empty payload", func(t *testing.T) {
		attrs, diags := reinstallAttributesFromAction(ctx, &ServerReinstallActionModel{
			ServerID:        types.StringValue("sv_1"),
			OperatingSystem: types.StringNull(),
			Hostname:        types.StringNull(),
			SSHKeys:         types.ListNull(types.StringType),
			UserData:        types.StringNull(),
			Raid:            types.StringNull(),
			Ipxe:            types.StringNull(),
		})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		// An empty payload is what makes a bare reinstall keep the server's
		// current deploy config instead of resetting it.
		if attrs.OperatingSystem != nil || attrs.Hostname != nil || attrs.SSHKeys != nil ||
			attrs.UserData != nil || attrs.Raid != nil || attrs.Ipxe != nil ||
			attrs.PersistentNetboot != nil || attrs.DiskLayout != nil {
			t.Errorf("expected an empty payload, got %+v", attrs)
		}
	})

	t.Run("set attributes reach the payload", func(t *testing.T) {
		attrs, diags := reinstallAttributesFromAction(ctx, &ServerReinstallActionModel{
			ServerID:          types.StringValue("sv_1"),
			OperatingSystem:   types.StringValue("debian_12"),
			Hostname:          types.StringValue("worker-01"),
			SSHKeys:           stringList("ssh_1", "ssh_2"),
			UserData:          types.StringValue("ud_1"),
			Raid:              types.StringValue("raid-1"),
			Ipxe:              types.StringNull(),
			PersistentNetboot: types.BoolValue(true),
		})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if attrs.OperatingSystem == nil || *attrs.OperatingSystem != operations.CreateServerReinstallServersOperatingSystemDebian12 {
			t.Errorf("operating_system = %v, want debian_12", attrs.OperatingSystem)
		}
		if attrs.Hostname == nil || *attrs.Hostname != "worker-01" {
			t.Errorf("hostname = %v, want worker-01", attrs.Hostname)
		}
		if len(attrs.SSHKeys) != 2 || attrs.SSHKeys[0] != "ssh_1" {
			t.Errorf("ssh_keys = %v, want [ssh_1 ssh_2]", attrs.SSHKeys)
		}
		if attrs.UserData == nil || *attrs.UserData != "ud_1" {
			t.Errorf("user_data = %v, want ud_1", attrs.UserData)
		}
		if attrs.Raid == nil || *attrs.Raid != operations.CreateServerReinstallServersRaidRaid1 {
			t.Errorf("raid = %v, want raid-1", attrs.Raid)
		}
		if attrs.PersistentNetboot == nil || !*attrs.PersistentNetboot {
			t.Errorf("persistent_netboot = %v, want true", attrs.PersistentNetboot)
		}
	})

	t.Run("an empty ssh_keys list is sent so keys are removed", func(t *testing.T) {
		attrs, diags := reinstallAttributesFromAction(ctx, &ServerReinstallActionModel{
			ServerID: types.StringValue("sv_1"),
			SSHKeys:  stringList(),
		})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if attrs.SSHKeys == nil || len(attrs.SSHKeys) != 0 {
			t.Errorf("ssh_keys = %v, want a non-nil empty slice", attrs.SSHKeys)
		}
	})

	t.Run("disk_layout supersedes raid", func(t *testing.T) {
		attrs, diags := reinstallAttributesFromAction(ctx, &ServerReinstallActionModel{
			ServerID: types.StringValue("sv_1"),
			Raid:     types.StringValue("raid-1"),
			DiskLayout: []DiskLayoutModel{
				{
					Count:      types.Int64Value(2),
					Role:       types.StringValue("os"),
					RaidLevel:  types.StringValue("raid-1"),
					MountPoint: types.StringNull(),
				},
			},
		})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if attrs.Raid != nil {
			t.Errorf("raid = %v, want nil when disk_layout is set", attrs.Raid)
		}
		if len(attrs.DiskLayout) != 1 || attrs.DiskLayout[0].Count != 2 {
			t.Errorf("disk_layout = %+v, want one group of 2 disks", attrs.DiskLayout)
		}
	})
}
