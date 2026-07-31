package sdkcoverage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeManifest(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), ManifestFile)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing manifest: %v", err)
	}
	return path
}

const validHeader = "version: 1\nsdk_module: github.com/latitudesh/latitudesh-go-sdk\n"

func TestLoadManifestValid(t *testing.T) {
	path := writeManifest(t, validHeader+`groups:
  Servers:
    implemented_by: [latitudesh_server]
  Teams:
    rationale: product_decision
    ceiling: none
    notes: not a Terraform concern
`)

	manifest, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if manifest.Version != SupportedManifestVersion {
		t.Errorf("Version = %d, want %d", manifest.Version, SupportedManifestVersion)
	}
	if !manifest.Groups["Servers"].Covered() {
		t.Error("Servers should read as covered from a non-empty implemented_by")
	}
	if manifest.Groups["Teams"].Covered() {
		t.Error("Teams has no implemented_by and must not read as covered")
	}
	if got := manifest.Groups["Teams"].Ceiling; got != CeilingNone {
		t.Errorf("ceiling = %q, want %q", got, CeilingNone)
	}
}

// A manifest declaring a schema version this build does not know must be
// rejected, not reconciled on a guess.
func TestLoadManifestRejectsUnknownVersion(t *testing.T) {
	path := writeManifest(t, "version: 99\nsdk_module: github.com/latitudesh/latitudesh-go-sdk\ngroups: {}\n")

	_, err := LoadManifest(path)
	if err == nil {
		t.Fatal("expected an error for an unsupported schema version")
	}
	if !strings.Contains(err.Error(), "version 99") {
		t.Errorf("error should name the offending version, got: %v", err)
	}
}

// Likewise a manifest that claims to describe some other module: the reconciler
// always parses SDKModulePath, so a mismatch means the file is misleading.
func TestLoadManifestRejectsWrongModule(t *testing.T) {
	path := writeManifest(t, "version: 1\nsdk_module: github.com/totally/wrong-module\ngroups: {}\n")

	_, err := LoadManifest(path)
	if err == nil {
		t.Fatal("expected an error for a mismatched sdk_module")
	}
	if !strings.Contains(err.Error(), "wrong-module") {
		t.Errorf("error should name the offending module, got: %v", err)
	}
}

// A missing version reads as 0, which must not be silently treated as v1.
func TestLoadManifestRejectsMissingIdentity(t *testing.T) {
	path := writeManifest(t, "groups: {}\n")

	if _, err := LoadManifest(path); err == nil {
		t.Fatal("expected an error when version and sdk_module are absent")
	}
}

// A mistyped key must fail loudly rather than become a setting that does nothing.
func TestLoadManifestRejectsUnknownField(t *testing.T) {
	path := writeManifest(t, validHeader+`groups:
  Servers:
    implemented_bt: [latitudesh_server]
`)

	_, err := LoadManifest(path)
	if err == nil {
		t.Fatal("expected an error for an unknown field")
	}
	if !strings.Contains(err.Error(), "implemented_bt") {
		t.Errorf("error should name the unknown field, got: %v", err)
	}
}

func TestLoadManifestMissingFile(t *testing.T) {
	if _, err := LoadManifest(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Fatal("expected an error for a missing manifest")
	}
}

// The committed manifest must load and describe the pinned SDK.
func TestLoadManifestRepoFileIsValid(t *testing.T) {
	manifest, err := LoadManifest(filepath.Join("..", "..", ManifestFile))
	if err != nil {
		t.Fatalf("loading the committed manifest: %v", err)
	}
	if len(manifest.Groups) == 0 {
		t.Error("the committed manifest declares no groups")
	}
}
