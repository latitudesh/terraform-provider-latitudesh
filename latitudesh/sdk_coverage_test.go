package latitudesh

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/latitudesh/terraform-provider-latitudesh/v2/internal/sdkcoverage"
)

// TestProviderSDKCoverage keeps sdk-coverage.yaml honest about the gap between
// what latitudesh-go-sdk exposes and what this provider ships.
//
// It is a static check: no network, no API token, no TF_ACC. It runs on every PR
// as part of the existing unit-test step, which matches `-run
// "TestProvider|TestFrameworkProvider"` — the name prefix is load-bearing, so
// renaming this test would silently drop it from CI.
//
// It fails only on contradictions: a group the SDK no longer exposes, a Terraform
// type the manifest does not claim (or claims under a name the provider does not
// register), or a half-written exclusion. An SDK bump that adds a service group is
// NOT a failure — an undeclared group is what triggers scaffolding, and failing
// would block every bump on a product decision its author was never asked to make.
// Those groups surface in the report instead (`make coverage-report`).
//
// It is also blind to changes *within* an already-covered group: only group and
// method names are parsed, never model fields or signatures. A new field on an
// existing resource is invisible here.
func TestProviderSDKCoverage(t *testing.T) {
	sdkDir, err := sdkcoverage.PinnedModuleDir(sdkcoverage.SDKModulePath)
	if err != nil {
		t.Fatalf("resolving the pinned SDK module: %v", err)
	}

	surface, err := sdkcoverage.ParseSurface(sdkDir)
	if err != nil {
		t.Fatalf("parsing the SDK surface at %s: %v", sdkDir, err)
	}

	manifestPath := filepath.Join("..", sdkcoverage.ManifestFile)
	manifest, err := sdkcoverage.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("loading %s: %v", manifestPath, err)
	}

	ctx := context.Background()
	shipped := sdkcoverage.ShippedTypeNames(ctx, New("test")(), "latitudesh")
	if len(shipped) == 0 {
		t.Fatal("the provider registered no resources or data sources; introspection is broken")
	}

	report := sdkcoverage.Reconcile(surface, manifest, shipped)

	for _, violation := range report.Violations {
		t.Errorf("%s", violation)
	}
	if !report.OK() {
		t.Logf("%d SDK service group(s) parsed from %s", len(surface.Groups), sdkDir)
		t.Logf("%d Terraform type(s) registered by the provider", len(shipped))
		t.Log("fix the mismatch by updating " + sdkcoverage.ManifestFile +
			" (see the header comment there for what each status means)")
	}
}
