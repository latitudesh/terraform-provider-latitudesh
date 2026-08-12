package latitudesh

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The provider's hostname rules and its operating_system="ipxe" guard describe
// what a *new or edited* value must look like. Applied to an unchanged value
// they instead re-litigate data the Latitude API already accepted, which makes
// `terraform plan` impossible for imported servers: the value lives in the API,
// `hostname` is Required so it must also appear in the config, and the only way
// to satisfy the provider is to rename real production servers.
//
// These tests pin the scoping — created or changed values are validated,
// unchanged ones are not.

func TestValidateHostnameOnChange(t *testing.T) {
	t.Parallel()

	// Real hostname from a production account that the API accepted; the
	// charset rule rejects both the spaces and the ampersand.
	const legacyHostname = "Veeam Backup & Replication"

	cases := []struct {
		name          string
		isCreate      bool
		stateHostname types.String
		planHostname  types.String
		wantErr       bool
	}{
		{
			name:          "imported server keeps a hostname the rules reject",
			stateHostname: types.StringValue(legacyHostname),
			planHostname:  types.StringValue(legacyHostname),
			wantErr:       false,
		},
		{
			name:          "imported server changing an unrelated attribute",
			stateHostname: types.StringValue("host_with_underscores"),
			planHostname:  types.StringValue("host_with_underscores"),
			wantErr:       false,
		},
		{
			name:          "renaming to another invalid hostname is still caught",
			stateHostname: types.StringValue(legacyHostname),
			planHostname:  types.StringValue("Veeam Backup & Replication 2"),
			wantErr:       true,
		},
		{
			name:          "renaming to a valid hostname passes",
			stateHostname: types.StringValue(legacyHostname),
			planHostname:  types.StringValue("veeam-backup"),
			wantErr:       false,
		},
		{
			name:         "create with an invalid hostname is caught",
			isCreate:     true,
			planHostname: types.StringValue("bad_hostname"),
			wantErr:      true,
		},
		{
			name:         "create with a hostname over 32 characters is caught",
			isCreate:     true,
			planHostname: types.StringValue("abcdefghijklmnopqrstuvwxyzabcdefg"),
			wantErr:      true,
		},
		{
			name:         "create with a valid hostname passes",
			isCreate:     true,
			planHostname: types.StringValue("terraform-ci-test.latitude.sh"),
			wantErr:      false,
		},
		{
			name:          "unknown hostname defers to the next plan round",
			stateHostname: types.StringValue(legacyHostname),
			planHostname:  types.StringUnknown(),
			wantErr:       false,
		},
		{
			name:         "null hostname is left to the Required check",
			isCreate:     true,
			planHostname: types.StringNull(),
			wantErr:      false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateHostnameOnChange(tc.isCreate, tc.stateHostname, tc.planHostname)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestRequiresIpxeAttributeOnChange(t *testing.T) {
	t.Parallel()

	const script = "https://example.test/boot.ipxe"

	cases := []struct {
		name      string
		isCreate  bool
		stateOS   types.String
		stateIpxe types.String
		planOS    types.String
		planIpxe  types.String
		wantErr   bool
	}{
		{
			// Tinkerbell-provisioned servers report operating_system="ipxe"
			// with no script of their own. Nothing here is changing.
			name:      "imported ipxe server with no script plans cleanly",
			stateOS:   types.StringValue("ipxe"),
			stateIpxe: types.StringNull(),
			planOS:    types.StringValue("ipxe"),
			planIpxe:  types.StringNull(),
			wantErr:   false,
		},
		{
			name:      "switching an existing server to ipxe without a script is caught",
			stateOS:   types.StringValue("ubuntu_24_04_x64_lts"),
			stateIpxe: types.StringNull(),
			planOS:    types.StringValue("ipxe"),
			planIpxe:  types.StringNull(),
			wantErr:   true,
		},
		{
			name:      "removing the script from an ipxe server is caught",
			stateOS:   types.StringValue("ipxe"),
			stateIpxe: types.StringValue(script),
			planOS:    types.StringValue("ipxe"),
			planIpxe:  types.StringNull(),
			wantErr:   true,
		},
		{
			name:      "replacing the script on an ipxe server passes",
			stateOS:   types.StringValue("ipxe"),
			stateIpxe: types.StringValue(script),
			planOS:    types.StringValue("ipxe"),
			planIpxe:  types.StringValue("https://example.test/other.ipxe"),
			wantErr:   false,
		},
		{
			name:     "create with os=ipxe and no script is caught",
			isCreate: true,
			planOS:   types.StringValue("ipxe"),
			planIpxe: types.StringNull(),
			wantErr:  true,
		},
		{
			name:     "create with os=ipxe and a script passes",
			isCreate: true,
			planOS:   types.StringValue("ipxe"),
			planIpxe: types.StringValue(script),
			wantErr:  false,
		},
		{
			name:      "moving an imported ipxe server off ipxe passes",
			stateOS:   types.StringValue("ipxe"),
			stateIpxe: types.StringNull(),
			planOS:    types.StringValue("ubuntu_24_04_x64_lts"),
			planIpxe:  types.StringNull(),
			wantErr:   false,
		},
		{
			name:      "unknown script defers to the next plan round",
			stateOS:   types.StringValue("ubuntu_24_04_x64_lts"),
			stateIpxe: types.StringNull(),
			planOS:    types.StringValue("ipxe"),
			planIpxe:  types.StringUnknown(),
			wantErr:   false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := requiresIpxeAttributeOnChange(tc.isCreate, tc.stateOS, tc.stateIpxe, tc.planOS, tc.planIpxe)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
