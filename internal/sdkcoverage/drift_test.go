package sdkcoverage

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

var widgetsManifest = Manifest{Groups: map[string]Entry{
	"Widgets": {ImplementedBy: []string{"latitudesh_widget"}},
}}

// TestDiffFieldSurfacesFixturePair drives the whole differ through the v1/v2
// fixture pair, which mutates exactly one instance of each drift kind. The
// want list below and the v2 package comment describe the same set — a change
// to either fixture lands here.
func TestDiffFieldSurfacesFixturePair(t *testing.T) {
	v1 := parseFieldFixture(t, fieldSDKv1)
	v2 := parseFieldFixture(t, fieldSDKv2)

	drift := DiffFieldSurfaces(v1, v2, widgetsManifest)

	got := make([]string, 0, len(drift))
	for _, d := range drift {
		got = append(got, fmt.Sprintf("%s|%s|%s|%s", d.Kind, d.Model, d.Field, marker(d.Breaking())))
	}
	sort.Strings(got)

	want := []string{
		"default_changed|operations.ListWidgetsRequest|page[size]|info",
		"deprecated||Legacy|info",
		"doc_changed|components.WidgetData|label|info",
		"enum_value_added|operations.WidgetColor|green|info",
		"enum_value_removed|operations.WidgetColor|blue|BREAKING",
		"field_added|components.WidgetData|badge|info",
		"field_added|components.WidgetData|bgp_ready|info",
		"field_removed|operations.CreateWidgetWidgetsRequestBody|size|BREAKING",
		"field_required_changed|components.WidgetData|status|BREAKING",
		"field_type_changed|components.WidgetData|weight|BREAKING",
		"method_added||Update|info",
		"method_removed||Delete|BREAKING",
		"method_signature_changed||List|BREAKING",
		"model_added|components.Badge||info",
		"model_added|operations.UpdateWidgetResponse||info",
		"model_removed|operations.DeleteWidgetResponse||info",
	}
	sort.Strings(want)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("drift rows:\n got %s\nwant %s", strings.Join(got, "\n     "), strings.Join(want, "\n     "))
	}

	for _, d := range drift {
		if d.Group != "Widgets" {
			t.Errorf("drift attributed to %q, want Widgets: %v", d.Group, d)
		}
		if !reflect.DeepEqual(d.ImplementedBy, []string{"latitudesh_widget"}) {
			t.Errorf("ImplementedBy = %v: %v", d.ImplementedBy, d)
		}
		if d.Kind == DriftFieldAdded && d.Field == "bgp_ready" && d.SuggestedAttribute != "bgp_ready" {
			t.Errorf("bgp_ready suggested attribute = %q", d.SuggestedAttribute)
		}
	}
}

func TestDiffFieldSurfacesIdentical(t *testing.T) {
	v1 := parseFieldFixture(t, fieldSDKv1)
	if drift := DiffFieldSurfaces(v1, v1, widgetsManifest); len(drift) != 0 {
		t.Errorf("identical surfaces drifted: %v", drift)
	}
}

func TestDiffFieldSurfacesGroupBookkeeping(t *testing.T) {
	v1 := parseFieldFixture(t, fieldSDKv1)

	// Covered but never locked: one informational nudge, no per-field noise.
	drift := DiffFieldSurfaces(FieldSurface{}, v1, widgetsManifest)
	if len(drift) != 1 || drift[0].Kind != DriftGroupUnlocked || drift[0].Breaking() {
		t.Errorf("unlocked group: %v", drift)
	}

	// Locked but no longer covered (or gone from the SDK): the stale entry
	// asks for a resync, nothing more.
	drift = DiffFieldSurfaces(v1, FieldSurface{Groups: map[string]GroupModels{}}, widgetsManifest)
	if len(drift) != 1 || drift[0].Kind != DriftGroupStale || drift[0].Breaking() {
		t.Errorf("stale group: %v", drift)
	}
}

// TestDiffFieldSurfacesRenameHint pins the heuristic: a removed model whose
// shape matches an added one is flagged as a possible rename, and neither side
// cascades into per-field rows.
func TestDiffFieldSurfacesRenameHint(t *testing.T) {
	shape := ModelShape{Fields: []FieldShape{{Name: "ID", Wire: "id", Type: "*string", Optional: true}}}
	base := FieldSurface{Groups: map[string]GroupModels{
		"Widgets": {Models: map[string]ModelShape{"operations.PutWidget": shape}},
	}}
	cur := FieldSurface{Groups: map[string]GroupModels{
		"Widgets": {Models: map[string]ModelShape{"operations.PatchWidget": shape}},
	}}

	drift := DiffFieldSurfaces(base, cur, widgetsManifest)
	if len(drift) != 2 {
		t.Fatalf("want exactly removed+added, got %v", drift)
	}
	var sawHint bool
	for _, d := range drift {
		if d.Kind == DriftModelRemoved && strings.Contains(d.Detail, "possibly renamed to operations.PatchWidget") {
			sawHint = true
		}
	}
	if !sawHint {
		t.Errorf("no rename hint in %v", drift)
	}
}

func marker(breaking bool) string {
	if breaking {
		return "BREAKING"
	}
	return "info"
}
