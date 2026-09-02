package latitudesh

// These tests drive the managed access key resource end to end against a local
// mock of the Latitude.sh API (create, refresh via the list endpoint, destroy),
// plus unit tests for the PGP encryption path. They run under TF_ACC without
// credentials and without provisioning anything.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// newTestPGPKey generates a throwaway keypair and returns the armored public
// key plus the entity (with private key) for decrypting.
func newTestPGPKey(t *testing.T) (string, *openpgp.Entity) {
	t.Helper()

	// ProtonMail's NewEntity defaults to an Ed25519 key with sane hash/cipher
	// preferences, so it encrypts without the RIPEMD160 workaround the frozen
	// x/crypto package needed — and it exercises the same modern key type real
	// GnuPG emits by default.
	entity, err := openpgp.NewEntity("Terraform Test", "", "test@example.com", nil)
	if err != nil {
		t.Fatalf("generating PGP key: %v", err)
	}

	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatalf("armoring public key: %v", err)
	}
	if err := entity.Serialize(w); err != nil {
		t.Fatalf("serializing public key: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing armor writer: %v", err)
	}
	return buf.String(), entity
}

func decryptTestSecret(t *testing.T, entity *openpgp.Entity, encryptedB64 string) string {
	t.Helper()

	raw, err := base64.StdEncoding.DecodeString(encryptedB64)
	if err != nil {
		t.Fatalf("encrypted_secret is not base64: %v", err)
	}
	md, err := openpgp.ReadMessage(bytes.NewReader(raw), openpgp.EntityList{entity}, nil, nil)
	if err != nil {
		t.Fatalf("decrypting secret: %v", err)
	}
	plain, err := io.ReadAll(md.UnverifiedBody)
	if err != nil {
		t.Fatalf("reading decrypted secret: %v", err)
	}
	return string(plain)
}

func TestPGPEncryptRoundTrip(t *testing.T) {
	armored, entity := newTestPGPKey(t)

	parsed, err := parsePGPPublicKey(armored)
	if err != nil {
		t.Fatalf("parsePGPPublicKey(armored): %v", err)
	}
	fingerprint, encrypted, err := encryptSecretWithPGP(parsed, "s3cr3t-value")
	if err != nil {
		t.Fatalf("encryptSecretWithPGP: %v", err)
	}
	if fingerprint == "" {
		t.Error("expected a non-empty fingerprint")
	}
	if got := decryptTestSecret(t, entity, encrypted); got != "s3cr3t-value" {
		t.Errorf("decrypted secret = %q, want %q", got, "s3cr3t-value")
	}
}

func TestParsePGPPublicKeyBase64(t *testing.T) {
	armored, _ := newTestPGPKey(t)

	// Re-encode the armored key's binary payload as plain base64 (the
	// aws_iam_access_key input format).
	block, err := armor.Decode(strings.NewReader(armored))
	if err != nil {
		t.Fatalf("decoding armor: %v", err)
	}
	binary, err := io.ReadAll(block.Body)
	if err != nil {
		t.Fatalf("reading armor body: %v", err)
	}

	if _, err := parsePGPPublicKey(base64.StdEncoding.EncodeToString(binary)); err != nil {
		t.Errorf("parsePGPPublicKey(base64 binary): %v", err)
	}
}

func TestParsePGPPublicKeyRejectsGarbage(t *testing.T) {
	if _, err := parsePGPPublicKey("not a key at all!!"); err == nil {
		t.Error("expected an error for garbage input")
	}
}

// mockAccessKeyResourceAPI is a stateful mock covering the managed resource
// lifecycle: create, list (used by Read) and delete.
type mockAccessKeyResourceAPI struct {
	mu sync.Mutex

	// simulateVAST reproduces the high_performance quirk: the create succeeds
	// but the list endpoint never returns the key (its high_performance array
	// stays empty). Read must not treat that as deletion.
	simulateVAST bool

	created bool
	deleted bool

	createBodies []map[string]any
	deletePaths  []string
}

func (m *mockAccessKeyResourceAPI) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	w.Header().Set("Content-Type", "application/vnd.api+json")

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/storage/access_keys":
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		m.createBodies = append(m.createBodies, decoded)
		m.created = true
		m.deleted = false

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"type": "access_keys",
				"attributes": map[string]any{
					"access_key": map[string]any{
						"access_key_id":     "AKIAMOCK",
						"secret_access_key": "MOCKSECRET",
						"name":              "ci-key",
						"username":          "iam-ci-key",
						"status":            "Active",
					},
				},
			},
		})

	case r.Method == http.MethodGet && r.URL.Path == "/storage/access_keys":
		keys := []any{}
		// simulateVAST leaves both arrays empty on purpose (the real
		// high_performance listing gap); otherwise the standard key is listed.
		if m.created && !m.deleted && !m.simulateVAST {
			keys = append(keys, map[string]any{
				"name":          "ci-key",
				"username":      "iam-ci-key",
				"access_key_id": "AKIAMOCK",
				"status":        "Active",
			})
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"standard": keys, "high_performance": []any{}},
		})

	case r.Method == http.MethodDelete:
		m.deletePaths = append(m.deletePaths, r.URL.Path)
		m.deleted = true
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

func testAccAccessKeyResourceConfig(extra string) string {
	return testAccAccessKeyResourceConfigClass("standard", extra)
}

func testAccAccessKeyResourceConfigClass(storageClass, extra string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_object_storage_access_key" "test" {
  name          = "CI Key"
  project       = "proj_mock"
  storage_class = %q
  region        = "ASH"
%s
}
`, storageClass, extra)
}

func testAccCheckAccessKeyDestroyed(m *mockAccessKeyResourceAPI) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.created && !m.deleted {
			return fmt.Errorf("access key still exists in the mock API after destroy")
		}
		return nil
	}
}

func TestAccObjectStorageAccessKeyResource_Lifecycle(t *testing.T) {
	mock := &mockAccessKeyResourceAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckAccessKeyDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccAccessKeyResourceConfig(""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_object_storage_access_key.test", "id", "iam-ci-key"),
					resource.TestCheckResourceAttr("latitudesh_object_storage_access_key.test", "username", "iam-ci-key"),
					resource.TestCheckResourceAttr("latitudesh_object_storage_access_key.test", "access_key_id", "AKIAMOCK"),
					resource.TestCheckResourceAttr("latitudesh_object_storage_access_key.test", "secret_access_key", "MOCKSECRET"),
					resource.TestCheckResourceAttr("latitudesh_object_storage_access_key.test", "access_scope", "fullaccess"),
					resource.TestCheckResourceAttr("latitudesh_object_storage_access_key.test", "status", "Active"),
					resource.TestCheckNoResourceAttr("latitudesh_object_storage_access_key.test", "encrypted_secret"),
				),
			},
		},
	})

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.deletePaths) == 0 {
		t.Fatal("expected a DELETE on destroy")
	}
	wantPath := "/storage/access_keys/iam-ci-key/standard"
	for _, p := range mock.deletePaths {
		if p != wantPath {
			t.Errorf("DELETE path = %q, want %q", p, wantPath)
		}
	}
}

// Regression: a live key can be missing from the list response (observed once
// for a high_performance key), so Read must NOT remove the resource when it is
// absent — otherwise a plan after create proposes a destructive recreate (and
// hits 409 because the real key still exists). The post-apply idempotency check
// plus an explicit re-plan both assert the resource survives an empty listing.
func TestAccObjectStorageAccessKeyResource_VASTNotInListIsPreserved(t *testing.T) {
	mock := &mockAccessKeyResourceAPI{simulateVAST: true}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckAccessKeyDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccAccessKeyResourceConfigClass("high_performance", ""),
				Check: resource.TestCheckResourceAttr(
					"latitudesh_object_storage_access_key.test", "access_key_id", "AKIAMOCK"),
			},
			{
				Config:   testAccAccessKeyResourceConfigClass("high_performance", ""),
				PlanOnly: true, // must be empty: absence from the list is not deletion
			},
		},
	})
}

func TestAccObjectStorageAccessKeyResource_PGP(t *testing.T) {
	armored, entity := newTestPGPKey(t)

	mock := &mockAccessKeyResourceAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	pgpBlock := fmt.Sprintf("  pgp_key = <<EOT\n%s\nEOT", strings.TrimSpace(armored))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccAccessKeyResourceConfig(pgpBlock),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_object_storage_access_key.test", "access_key_id", "AKIAMOCK"),
					resource.TestCheckNoResourceAttr("latitudesh_object_storage_access_key.test", "secret_access_key"),
					resource.TestCheckResourceAttrSet("latitudesh_object_storage_access_key.test", "encrypted_secret"),
					resource.TestCheckResourceAttrSet("latitudesh_object_storage_access_key.test", "key_fingerprint"),
					// The state must hold ciphertext that the private key can
					// decrypt back to the API-issued secret — and never the
					// plaintext itself.
					resource.TestCheckResourceAttrWith("latitudesh_object_storage_access_key.test", "encrypted_secret", func(value string) error {
						if value == "MOCKSECRET" {
							return fmt.Errorf("encrypted_secret holds the plaintext secret")
						}
						if got := decryptTestSecret(t, entity, value); got != "MOCKSECRET" {
							return fmt.Errorf("decrypted secret = %q, want MOCKSECRET", got)
						}
						return nil
					}),
				),
			},
		},
	})
}
