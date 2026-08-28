package sdkcoverage

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFieldLockRoundTrip(t *testing.T) {
	fields := parseFieldFixture(t, fieldSDKv1)
	lock := BuildFieldLock(fields)

	path := filepath.Join(t.TempDir(), FieldLockFile)
	if err := WriteFieldLock(path, lock); err != nil {
		t.Fatalf("WriteFieldLock: %v", err)
	}

	loaded, err := LoadFieldLock(path)
	if err != nil {
		t.Fatalf("LoadFieldLock: %v", err)
	}

	// Byte-stable: what the loaded lock marshals to is exactly what was
	// written. This is the property that keeps lock diffs meaningful.
	if !bytes.Equal(lock.Marshal(), loaded.Marshal()) {
		t.Error("marshal after round trip differs from the original")
	}

	// And the round trip preserves shape, so the differ sees no drift.
	if drift := DiffFieldSurfaces(loaded.Surface(), fields, Manifest{Groups: map[string]Entry{
		"Widgets": {ImplementedBy: []string{"latitudesh_widget"}},
	}}); len(drift) != 0 {
		t.Errorf("round trip drifted: %v", drift)
	}
}

func TestLoadFieldLockRejectsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), FieldLockFile)
	doc := `version: 1
sdk_module: github.com/latitudesh/latitudesh-go-sdk
groups:
  Widgets:
    methods:
      List: {signature: "()", bogus: true}
`
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFieldLock(path); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("unknown key not rejected: %v", err)
	}
}

func TestLoadFieldLockChecksIdentity(t *testing.T) {
	write := func(t *testing.T, doc string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), FieldLockFile)
		if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	if _, err := LoadFieldLock(write(t, "version: 2\nsdk_module: github.com/latitudesh/latitudesh-go-sdk\ngroups: {}\n")); err == nil {
		t.Error("unsupported version not rejected")
	}
	if _, err := LoadFieldLock(write(t, "version: 1\nsdk_module: example.com/other\ngroups: {}\n")); err == nil {
		t.Error("wrong sdk_module not rejected")
	}
}

func TestLoadFieldLockMissingFile(t *testing.T) {
	_, err := LoadFieldLock(filepath.Join(t.TempDir(), FieldLockFile))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing lock should surface os.ErrNotExist, got %v", err)
	}
}
