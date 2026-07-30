package sdkcoverage

import (
	"reflect"
	"strings"
	"testing"
)

func surfaceOf(groups map[string][]string) Surface {
	s := Surface{Groups: make(map[string]Group, len(groups))}
	for name, methods := range groups {
		s.Groups[name] = Group{Name: name, TypeName: name, Methods: methods}
	}
	return s
}

func joinViolations(report Report) string {
	msgs := make([]string, 0, len(report.Violations))
	for _, v := range report.Violations {
		msgs = append(msgs, v.String())
	}
	return strings.Join(msgs, "\n")
}

// requireViolation asserts exactly one violation, mentioning needle. The gate is
// only worth having if each disagreement produces an actionable message, so the
// message text is part of the contract.
func requireViolation(t *testing.T, report Report, needle string) {
	t.Helper()

	if len(report.Violations) != 1 {
		t.Fatalf("got %d violations, want 1: %s", len(report.Violations), joinViolations(report))
	}
	if got := report.Violations[0].String(); !strings.Contains(got, needle) {
		t.Errorf("violation %q does not mention %q", got, needle)
	}
}

// The headline behaviour of the generate-by-default model: a group nobody has
// declared is the trigger for scaffolding, not a blind spot. An SDK bump that adds
// surface must NOT go red — otherwise every bump is blocked on a product decision
// its author was never asked to make.
func TestReconcileUndeclaredGroupIsPendingNotAViolation(t *testing.T) {
	surface := surfaceOf(map[string][]string{
		"Servers":         {"Create", "Get", "Delete"},
		"ManagedDatabase": {"CreateDatabase", "GetDatabase", "DeleteDatabase"},
	})
	manifest := Manifest{Groups: map[string]Entry{
		"Servers": {ImplementedBy: []string{"latitudesh_server"}},
	}}

	report := Reconcile(surface, manifest, []string{"latitudesh_server"})

	if !report.OK() {
		t.Fatalf("an undeclared group must not be a violation, got:\n%s", joinViolations(report))
	}
	if len(report.Pending) != 1 || report.Pending[0].Name != "ManagedDatabase" {
		t.Fatalf("Pending = %v, want one entry for ManagedDatabase", report.Pending)
	}
}

// What gets generated follows the derived shape. Generating a resource for a
// read-only group would be incoherent — there is no create or delete to wire up.
func TestGeneratesFollowsDerivedShape(t *testing.T) {
	tests := []struct {
		name    string
		methods []string
		entry   Entry
		want    []string
	}{
		{
			name:    "full CRUD gets both",
			methods: []string{"Create", "Get", "Update", "Delete"},
			want:    []string{"resource", "datasource"},
		},
		{
			name:    "create-read-delete is still resource-shaped",
			methods: []string{"PostStorageVolumes", "GetStorageVolume", "DeleteStorageVolumes"},
			want:    []string{"resource", "datasource"},
		},
		{
			name:    "read-only gets a data source only",
			methods: []string{"List"},
			want:    []string{"datasource"},
		},
		{
			name:    "create without delete cannot be a resource",
			methods: []string{"Create", "Get"},
			want:    []string{"datasource"},
		},
		{
			name:    "delete-only yields nothing",
			methods: []string{"Delete"},
			want:    nil,
		},
		{
			name:    "actions only yield nothing",
			methods: []string{"Lock", "Unlock"},
			want:    nil,
		},
		{
			name:    "ceiling none suppresses everything",
			methods: []string{"Create", "Get", "Update", "Delete"},
			entry:   Entry{Ceiling: CeilingNone, Rationale: RationaleProductDecision},
			want:    nil,
		},
		{
			name:    "ceiling datasource suppresses the resource only",
			methods: []string{"Create", "Get", "Update", "Delete"},
			entry:   Entry{Ceiling: CeilingDataSource, Rationale: RationaleAPIConstraint},
			want:    []string{"datasource"},
		},
		{
			name:    "a covered group generates nothing further",
			methods: []string{"Create", "Get", "Update", "Delete"},
			entry:   Entry{ImplementedBy: []string{"latitudesh_thing"}},
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := GroupReport{
				Group:        Group{Name: "X", Methods: tt.methods},
				Entry:        tt.entry,
				Capabilities: Classify(tt.methods),
			}
			if got := row.Generates(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Generates() = %v, want %v", got, tt.want)
			}
		})
	}
}

// A group whose lifecycle is too partial to generate anything from needs a human,
// so it is reported separately rather than sitting in the generation queue.
func TestReconcileSortsUnshapedGroupsAside(t *testing.T) {
	surface := surfaceOf(map[string][]string{
		"VirtualNetworks": {"Delete"},
		"Events":          {"List"},
	})

	report := Reconcile(surface, Manifest{Groups: map[string]Entry{}}, nil)

	if !report.OK() {
		t.Fatalf("unexpected violations:\n%s", joinViolations(report))
	}
	if len(report.Pending) != 1 || report.Pending[0].Name != "Events" {
		t.Errorf("Pending = %v, want only Events", report.Pending)
	}
	if len(report.Unshaped) != 1 || report.Unshaped[0].Name != "VirtualNetworks" {
		t.Errorf("Unshaped = %v, want only VirtualNetworks", report.Unshaped)
	}
}

// An entry carrying only notes is an annotation, not an exclusion: it still
// generates, it just arrives with the reviewer already warned about something.
func TestReconcileNotesOnlyEntryStillGenerates(t *testing.T) {
	surface := surfaceOf(map[string][]string{
		"ObjectStorage": {"PostStorageBuckets", "GetStorageBucket", "DeleteStorageBuckets"},
	})
	manifest := Manifest{Groups: map[string]Entry{
		"ObjectStorage": {Notes: "the spec understates this API"},
	}}

	report := Reconcile(surface, manifest, nil)

	if !report.OK() {
		t.Fatalf("a notes-only entry must be valid, got:\n%s", joinViolations(report))
	}
	if len(report.Pending) != 1 {
		t.Fatalf("Pending = %v, want the group still queued for generation", report.Pending)
	}
	if len(report.Pending[0].Generates()) == 0 {
		t.Error("a notes-only entry must not suppress generation")
	}
}

func TestReconcileExcludedGroup(t *testing.T) {
	surface := surfaceOf(map[string][]string{"VpnSessions": {"Create", "List", "Delete"}})
	manifest := Manifest{Groups: map[string]Entry{
		"VpnSessions": {Ceiling: CeilingNone, Rationale: RationaleProductDecision},
	}}

	report := Reconcile(surface, manifest, nil)

	if !report.OK() {
		t.Fatalf("unexpected violations:\n%s", joinViolations(report))
	}
	if len(report.Excluded) != 1 || len(report.Pending) != 0 {
		t.Errorf("excluded=%d pending=%d, want 1/0", len(report.Excluded), len(report.Pending))
	}
}

// A cap without a reason is unauditable; a reason without a cap does not say how
// much to stop generating. Both or neither.
func TestReconcileRequiresCeilingAndRationaleTogether(t *testing.T) {
	surface := surfaceOf(map[string][]string{"Teams": {"Create", "Get", "Update"}})

	report := Reconcile(surface, Manifest{Groups: map[string]Entry{
		"Teams": {Rationale: RationaleProductDecision},
	}}, nil)
	requireViolation(t, report, "needs a ceiling")

	report = Reconcile(surface, Manifest{Groups: map[string]Entry{
		"Teams": {Ceiling: CeilingNone},
	}}, nil)
	requireViolation(t, report, "needs a rationale")
}

func TestReconcileRejectsUnknownCeilingAndRationale(t *testing.T) {
	surface := surfaceOf(map[string][]string{"Events": {"List"}})

	report := Reconcile(surface, Manifest{Groups: map[string]Entry{
		"Events": {Ceiling: CeilingNone, Rationale: Rationale("someday")},
	}}, nil)
	requireViolation(t, report, `unknown rationale "someday"`)

	report = Reconcile(surface, Manifest{Groups: map[string]Entry{
		"Events": {Ceiling: Ceiling("module"), Rationale: RationaleProductDecision},
	}}, nil)
	requireViolation(t, report, `unknown ceiling "module"`)
}

// Ceiling and rationale describe what not to generate. Neither belongs on a group
// that is already built — what got built is the answer.
func TestReconcileRejectsCeilingOrRationaleOnCoveredGroup(t *testing.T) {
	surface := surfaceOf(map[string][]string{"Servers": {"Create", "Get", "Delete"}})
	manifest := Manifest{Groups: map[string]Entry{
		"Servers": {
			ImplementedBy: []string{"latitudesh_server"},
			Ceiling:       CeilingNone,
			Rationale:     RationaleProductDecision,
		},
	}}

	report := Reconcile(surface, manifest, []string{"latitudesh_server"})

	requireViolation(t, report, "do not apply")
}

// A group renamed or dropped upstream while the manifest keeps claiming it.
func TestReconcileFlagsGroupMissingFromSDK(t *testing.T) {
	surface := surfaceOf(map[string][]string{"Servers": {"Create", "Get", "Delete"}})
	manifest := Manifest{Groups: map[string]Entry{
		"Servers":     {ImplementedBy: []string{"latitudesh_server"}},
		"OldEndpoint": {Ceiling: CeilingNone, Rationale: RationaleDeprecated},
	}}

	report := Reconcile(surface, manifest, []string{"latitudesh_server"})

	requireViolation(t, report, "OldEndpoint")
}

// A resource renamed in the provider without updating the manifest.
func TestReconcileFlagsImplementedByUnknownToProvider(t *testing.T) {
	surface := surfaceOf(map[string][]string{"Servers": {"Create", "Get", "Delete"}})
	manifest := Manifest{Groups: map[string]Entry{
		"Servers": {ImplementedBy: []string{"latitudesh_server_renamed"}},
	}}

	report := Reconcile(surface, manifest, []string{"latitudesh_server"})

	// Both directions break: the manifest names something unregistered, and the
	// registered resource is unclaimed.
	if len(report.Violations) != 2 {
		t.Fatalf("got %d violations, want 2:\n%s", len(report.Violations), joinViolations(report))
	}
	joined := joinViolations(report)
	for _, needle := range []string{"latitudesh_server_renamed", "latitudesh_server"} {
		if !strings.Contains(joined, needle) {
			t.Errorf("violations do not mention %q: %s", needle, joined)
		}
	}
}

// With undeclared groups no longer failing, this is the check that still forces the
// manifest to be filled in once a resource actually lands — whether the agent wrote
// it or a human did.
func TestReconcileFlagsUnclaimedProviderType(t *testing.T) {
	surface := surfaceOf(map[string][]string{"Servers": {"Create", "Get", "Delete"}})
	manifest := Manifest{Groups: map[string]Entry{
		"Servers": {ImplementedBy: []string{"latitudesh_server"}},
	}}

	report := Reconcile(surface, manifest, []string{"latitudesh_server", "latitudesh_brand_new"})

	requireViolation(t, report, "latitudesh_brand_new")
}

// One group legitimately backing several resources must not be reported as a
// problem: PrivateNetworks backs both latitudesh_virtual_network and
// latitudesh_vlan_assignment.
func TestReconcileAllowsManyToManyMapping(t *testing.T) {
	surface := surfaceOf(map[string][]string{
		"PrivateNetworks": {"Create", "List", "Update", "Assign", "DeleteAssignment"},
		"VirtualNetworks": {"Delete"},
		"Tags":            {"List", "Create", "Update", "Delete"},
	})
	manifest := Manifest{Groups: map[string]Entry{
		"PrivateNetworks": {ImplementedBy: []string{"latitudesh_virtual_network", "latitudesh_vlan_assignment"}},
		"VirtualNetworks": {ImplementedBy: []string{"latitudesh_virtual_network"}},
		"Tags":            {ImplementedBy: []string{"latitudesh_tag"}},
	}}

	report := Reconcile(surface, manifest,
		[]string{"latitudesh_virtual_network", "latitudesh_vlan_assignment", "latitudesh_tag"})

	if !report.OK() {
		t.Fatalf("many-to-many mapping should be valid, got:\n%s", joinViolations(report))
	}
}

// An api_constraint exclusion is only true while the API stays put. When the SDK
// grows past the recorded ceiling the group surfaces for re-triage — as information,
// never as a violation.
func TestReconcileFlagsOutgrownAPIConstraintWithoutFailing(t *testing.T) {
	manifest := Manifest{Groups: map[string]Entry{
		"Teams": {Ceiling: CeilingDataSource, Rationale: RationaleAPIConstraint},
	}}

	// Before: no delete, so a resource is genuinely impossible.
	before := Reconcile(surfaceOf(map[string][]string{"Teams": {"Create", "Get", "Update"}}), manifest, nil)
	if !before.OK() {
		t.Fatalf("unexpected violations: %s", joinViolations(before))
	}
	if len(before.Revisit) != 0 {
		t.Errorf("nothing to revisit while the constraint holds, got %d", len(before.Revisit))
	}

	// After: the API grew a delete, so the ceiling no longer reflects reality.
	after := Reconcile(surfaceOf(map[string][]string{"Teams": {"Create", "Get", "Update", "Delete"}}), manifest, nil)
	if !after.OK() {
		t.Fatalf("an outgrown ceiling must not be a violation, got: %s", joinViolations(after))
	}
	if len(after.Revisit) != 1 || after.Revisit[0].Name != "Teams" {
		t.Fatalf("Revisit = %v, want one entry for Teams", after.Revisit)
	}
}

// A product decision is exempt: it does not change because an endpoint appeared.
func TestReconcileNeverRevisitsProductDecision(t *testing.T) {
	surface := surfaceOf(map[string][]string{
		"VpnSessions": {"Create", "Get", "List", "Update", "Delete"},
	})
	manifest := Manifest{Groups: map[string]Entry{
		"VpnSessions": {Ceiling: CeilingNone, Rationale: RationaleProductDecision},
	}}

	report := Reconcile(surface, manifest, nil)

	if !report.OK() {
		t.Fatalf("unexpected violations: %s", joinViolations(report))
	}
	if len(report.Revisit) != 0 {
		t.Errorf("a product decision must never be auto-revisited, got %d", len(report.Revisit))
	}
}

// ceiling: none on API grounds is outgrown as soon as anything is readable.
func TestReconcileRevisitsNoneCeilingWhenReadable(t *testing.T) {
	surface := surfaceOf(map[string][]string{"Events": {"List"}})
	manifest := Manifest{Groups: map[string]Entry{
		"Events": {Ceiling: CeilingNone, Rationale: RationaleAPIConstraint},
	}}

	report := Reconcile(surface, manifest, nil)

	if len(report.Revisit) != 1 {
		t.Errorf("a readable group under ceiling none should surface, got %d", len(report.Revisit))
	}
}

func TestReconcileReportsUnclassifiedWithoutFailing(t *testing.T) {
	surface := surfaceOf(map[string][]string{"Oddities": {"FrobnicateWidget", "Get"}})

	report := Reconcile(surface, Manifest{Groups: map[string]Entry{}}, nil)

	if !report.OK() {
		t.Errorf("unrecognized method names must not be a violation, got: %s", joinViolations(report))
	}
	unclassified := report.Unclassified()
	if len(unclassified) != 1 || unclassified[0].Name != "Oddities" {
		t.Fatalf("Unclassified() = %v, want one entry for Oddities", unclassified)
	}
}

// End-to-end over the frozen tree with an entirely empty manifest — the state a
// brand-new repo would be in. Everything should queue for generation, nothing
// should fail.
func TestReconcileAgainstFrozenSurfaceWithEmptyManifest(t *testing.T) {
	surface, err := ParseSurface(miniSDK)
	if err != nil {
		t.Fatalf("ParseSurface: %v", err)
	}

	report := Reconcile(surface, Manifest{Groups: map[string]Entry{}}, nil)

	if !report.OK() {
		t.Fatalf("an empty manifest must not fail, got:\n%s", joinViolations(report))
	}
	if report.Total() != len(surface.Groups) {
		t.Errorf("Total() = %d, want %d", report.Total(), len(surface.Groups))
	}
	if len(report.Pending)+len(report.Unshaped) != len(surface.Groups) {
		t.Errorf("every group should be pending or unshaped, got %d+%d of %d",
			len(report.Pending), len(report.Unshaped), len(surface.Groups))
	}
	// VirtualNetworks is delete-only in the frozen tree, so nothing can be built
	// from it; Projects.SSHKeys is create-only.
	unshaped := map[string]bool{}
	for _, g := range report.Unshaped {
		unshaped[g.Name] = true
	}
	for _, name := range []string{"VirtualNetworks", "Projects.SSHKeys"} {
		if !unshaped[name] {
			t.Errorf("%s should be too partial to generate", name)
		}
	}
	// Markdown rendering must not panic on a fully-populated report.
	if out := report.Markdown("v0.0.0-test"); !strings.Contains(out, "Firewalls.Assignments") {
		t.Error("markdown output is missing the nested group")
	}
}
