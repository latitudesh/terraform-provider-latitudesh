package latitudesh

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/latitudesh/terraform-provider-latitudesh/v2/internal/sdkcoverage"
)

// TestProviderSDKFieldDrift keeps sdk-fields.lock.yaml honest about the shape
// of the covered groups' models: the fields, types, tags, enums, and method
// signatures the provider's mappings were written against.
//
// Like TestProviderSDKCoverage it is a static check — no network, no API
// token, no TF_ACC — and like it, it fails only on contradictions: BREAKING
// drift between the pinned SDK and the committed lock (a field removed or
// retyped, a required flip, an enum value gone, a method removed or its
// signature changed). Those are shapes the provider still compiles in, so
// absorbing them silently in an SDK bump risks a runtime break.
//
// Additive drift — a new field, a new enum value, a deprecation, a changed
// default or doc — is logged, never a failure: new capability on a covered
// group is the drift-fix pipeline's queue (see .github/workflows/sdk-watch.yml),
// exactly as an uncovered group is the scaffold pipeline's.
//
// The fix for a breaking failure is to adjust the mapping in the named
// Terraform types (or deliberately omit the change) and regenerate the lock in
// the same PR: `make fields-sync`. The lock's git diff is the review record of
// what was accepted.
func TestProviderSDKFieldDrift(t *testing.T) {
	lockPath := filepath.Join("..", sdkcoverage.FieldLockFile)
	lock, err := sdkcoverage.LoadFieldLock(lockPath)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("%s not present — field drift is not tracked; seed it with `make fields-sync`", sdkcoverage.FieldLockFile)
	}
	if err != nil {
		t.Fatalf("loading %s: %v", lockPath, err)
	}

	sdkDir, err := sdkcoverage.PinnedModuleDir(sdkcoverage.SDKModulePath)
	if err != nil {
		t.Fatalf("resolving the pinned SDK module: %v", err)
	}
	surface, err := sdkcoverage.ParseSurface(sdkDir)
	if err != nil {
		t.Fatalf("parsing the SDK surface at %s: %v", sdkDir, err)
	}
	manifest, err := sdkcoverage.LoadManifest(filepath.Join("..", sdkcoverage.ManifestFile))
	if err != nil {
		t.Fatalf("loading the coverage manifest: %v", err)
	}

	// Covered groups the SDK still exposes; a covered group the SDK lost is
	// TestProviderSDKCoverage's finding, not a field-parse error here.
	var covered []string
	for name, entry := range manifest.Groups {
		if _, ok := surface.Groups[name]; ok && entry.Covered() {
			covered = append(covered, name)
		}
	}
	sort.Strings(covered)

	fields, err := sdkcoverage.ParseFieldSurface(sdkDir, surface, covered)
	if err != nil {
		t.Fatalf("parsing the SDK field surface: %v", err)
	}

	for _, d := range sdkcoverage.DiffFieldSurfaces(lock.Surface(), fields, manifest) {
		if d.Breaking() {
			t.Errorf("breaking field drift: %s — fix the mapping in %v (or deliberately omit it), then run `make fields-sync` and let the lock diff be reviewed",
				d, d.ImplementedBy)
		} else {
			t.Logf("field drift (informational): %s", d)
		}
	}
}
