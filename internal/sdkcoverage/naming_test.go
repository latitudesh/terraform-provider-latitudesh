package sdkcoverage

import "testing"

// TestSuggestTypeName pins the suggested name for every service group the pinned
// SDK exposes today. These are hints, not the final names — several real
// resources are deliberately named otherwise (TeamMembers ships as
// latitudesh_member) — so this test guards the mechanical rule, not product
// intent. If an SDK bump adds a group, add a row here.
func TestSuggestTypeName(t *testing.T) {
	cases := []struct {
		group string
		want  string
	}{
		{"APIKeys", "latitudesh_api_key"},
		{"BaselinesPreview", "latitudesh_baselines_preview"},
		{"Billing", "latitudesh_billing"},
		{"BlockStorage", "latitudesh_block_storage"},
		{"ElasticIps", "latitudesh_elastic_ip"},
		{"Events", "latitudesh_event"},
		{"FilesystemStorage", "latitudesh_filesystem_storage"},
		{"Firewalls", "latitudesh_firewall"},
		{"Firewalls.Assignments", "latitudesh_firewall_assignment"},
		{"IPAddresses", "latitudesh_ip_address"},
		{"KubernetesClusters", "latitudesh_kubernetes_cluster"},
		{"ObjectStorage", "latitudesh_object_storage"},
		{"OperatingSystems", "latitudesh_operating_system"},
		{"Plans", "latitudesh_plan"},
		{"Plans.VM", "latitudesh_plan_vm"},
		{"PrivateNetworks", "latitudesh_private_network"},
		{"Projects", "latitudesh_project"},
		{"Projects.SSHKeys", "latitudesh_project_ssh_key"},
		{"PublicNetworks", "latitudesh_public_network"},
		{"Regions", "latitudesh_region"},
		{"Roles", "latitudesh_role"},
		{"SSHKeys", "latitudesh_ssh_key"},
		{"Servers", "latitudesh_server"},
		{"Tags", "latitudesh_tag"},
		{"TeamMembers", "latitudesh_team_member"},
		{"Teams", "latitudesh_team"},
		{"Teams.Members", "latitudesh_team_member"},
		{"Traffic", "latitudesh_traffic"},
		{"UserData", "latitudesh_user_data"},
		{"UserProfile", "latitudesh_user_profile"},
		{"VirtualMachineBackups", "latitudesh_virtual_machine_backup"},
		{"VirtualMachineRestores", "latitudesh_virtual_machine_restore"},
		{"VirtualMachines", "latitudesh_virtual_machine"},
		{"VirtualNetworks", "latitudesh_virtual_network"},
		{"VpnSessions", "latitudesh_vpn_session"},
	}

	for _, tc := range cases {
		if got := SuggestTypeName(tc.group, "latitudesh"); got != tc.want {
			t.Errorf("SuggestTypeName(%q) = %q, want %q", tc.group, got, tc.want)
		}
	}
}

// The provider prefix is a parameter, not a constant baked into the name, so the
// same rule serves a tool that reports for a differently named provider.
func TestSuggestTypeNameHonorsProviderPrefix(t *testing.T) {
	if got := SuggestTypeName("Servers", "example"); got != "example_server" {
		t.Errorf("SuggestTypeName with custom prefix = %q, want example_server", got)
	}
}

func TestSuggestAttributeName(t *testing.T) {
	cases := []struct {
		field FieldShape
		want  string
	}{
		// json wire names are already Terraform-shaped.
		{FieldShape{Name: "BgpReady", Wire: "bgp_ready"}, "bgp_ready"},
		// filter[] query parameters flatten, including nested brackets.
		{FieldShape{Name: "FilterProject", Wire: "filter[project]"}, "project"},
		{FieldShape{Name: "FilterRAMEql", Wire: "filter[ram][eql]"}, "ram_eql"},
		// Pagination and metadata families are provider-internal: no suggestion.
		{FieldShape{Name: "PageSize", Wire: "page[size]"}, ""},
		{FieldShape{Name: "StatsTotal", Wire: "stats[total]"}, ""},
		{FieldShape{Name: "ExtraFieldsServers", Wire: "extra_fields[servers]"}, ""},
		// Untagged fields fall back to the Go name.
		{FieldShape{Name: "Widget"}, "widget"},
		{FieldShape{Name: "BgpReady"}, "bgp_ready"},
	}
	for _, tc := range cases {
		if got := SuggestAttributeName(tc.field); got != tc.want {
			t.Errorf("SuggestAttributeName(%q/%q) = %q, want %q", tc.field.Name, tc.field.Wire, got, tc.want)
		}
	}
}
