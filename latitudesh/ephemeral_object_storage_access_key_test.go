package latitudesh

// These tests drive the object storage access key ephemeral resource end to
// end — Terraform binary (1.10+), protocol, Open/Close, HTTP — against a local
// mock of the Latitude.sh API injected through the provider's httpClient. The
// local echo test provider persists the ephemeral values into state so they
// can be asserted. They run under TF_ACC without credentials and without
// provisioning anything.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"

	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
)

func TestNormalizeAccessKeyCredentials(t *testing.T) {
	cases := []struct {
		name       string
		key        *operations.AccessKey
		wantKeyID  string
		wantSecret string
	}{
		{"nil key", nil, "", ""},
		{
			"wasabi shape",
			&operations.AccessKey{AccessKeyID: strPtr("AKIA123"), SecretAccessKey: strPtr("wsecret")},
			"AKIA123", "wsecret",
		},
		{
			"vast shape",
			&operations.AccessKey{AccessKey: strPtr("VAST123"), SecretKey: strPtr("vsecret")},
			"VAST123", "vsecret",
		},
		{
			"wasabi fields win when both are present",
			&operations.AccessKey{
				AccessKeyID: strPtr("AKIA123"), SecretAccessKey: strPtr("wsecret"),
				AccessKey: strPtr("VAST123"), SecretKey: strPtr("vsecret"),
			},
			"AKIA123", "wsecret",
		},
		{"missing secret", &operations.AccessKey{AccessKey: strPtr("VAST123")}, "VAST123", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keyID, secret := normalizeAccessKeyCredentials(tc.key)
			if keyID != tc.wantKeyID || secret != tc.wantSecret {
				t.Fatalf("normalizeAccessKeyCredentials() = (%q, %q), want (%q, %q)",
					keyID, secret, tc.wantKeyID, tc.wantSecret)
			}
		})
	}
}

// mockAccessKeyAPI serves the create/delete access key endpoints and records
// what it received so tests can assert the exact requests.
type mockAccessKeyAPI struct {
	mu sync.Mutex

	// response shape to serve on create
	responseKey map[string]any

	createBodies []map[string]any
	deletePaths  []string
	deleteQuery  []string
}

func (m *mockAccessKeyAPI) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	w.Header().Set("Content-Type", "application/vnd.api+json")

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/storage/access_keys":
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		m.createBodies = append(m.createBodies, decoded)

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"type": "access_keys",
				"attributes": map[string]any{
					"access_key": m.responseKey,
				},
			},
		})

	case r.Method == http.MethodDelete:
		m.deletePaths = append(m.deletePaths, r.URL.Path)
		m.deleteQuery = append(m.deleteQuery, r.URL.RawQuery)
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

func (m *mockAccessKeyAPI) createAttributes(t *testing.T, i int) map[string]any {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.createBodies) <= i {
		t.Fatalf("expected at least %d create calls, got %d", i+1, len(m.createBodies))
	}
	data, _ := m.createBodies[i]["data"].(map[string]any)
	attrs, _ := data["attributes"].(map[string]any)
	if attrs == nil {
		t.Fatalf("create call %d had no data.attributes: %v", i, m.createBodies[i])
	}
	return attrs
}

func testAccProtoV6FactoriesWithMockAndEcho(server *httptest.Server) map[string]func() (tfprotov6.ProviderServer, error) {
	factories := testAccProtoV6ProviderFactoriesWithMock(server)
	factories["echo"] = echoTestProviderFactory()
	return factories
}

func testAccAccessKeyConfig(extra string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

ephemeral "latitudesh_object_storage_access_key" "test" {
  name          = "CI Upload"
  project       = "proj_mock"
  storage_class = "high_performance"
  region        = "ASH"
%s
}

provider "echo" {
  data = ephemeral.latitudesh_object_storage_access_key.test
}

resource "echo" "test" {}
`, extra)
}

// A VAST-shaped create response (access_key/secret_key, no username) must be
// normalized into access_key_id/secret_access_key, access_scope must default
// to fullaccess, and Close must revoke the key by its server-reported name.
func TestAccObjectStorageAccessKey_VASTFullAccess(t *testing.T) {
	mock := &mockAccessKeyAPI{
		responseKey: map[string]any{
			"access_key": "VASTKEY123",
			"secret_key": "VASTSECRET456",
			"name":       "ci-upload",
			"status":     "Active",
		},
	}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6FactoriesWithMockAndEcho(server),
		Steps: []resource.TestStep{
			{
				Config: testAccAccessKeyConfig(""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("echo.test", "data.access_key_id", "VASTKEY123"),
					resource.TestCheckResourceAttr("echo.test", "data.secret_access_key", "VASTSECRET456"),
					resource.TestCheckResourceAttr("echo.test", "data.username", "ci-upload"),
					resource.TestCheckResourceAttr("echo.test", "data.status", "Active"),
					resource.TestCheckResourceAttr("echo.test", "data.access_scope", "fullaccess"),
				),
			},
		},
	})

	attrs := mock.createAttributes(t, 0)
	if attrs["access_scope"] != "fullaccess" {
		t.Errorf("access_scope sent = %v, want fullaccess", attrs["access_scope"])
	}
	if attrs["storage_class"] != "high_performance" {
		t.Errorf("storage_class sent = %v, want high_performance", attrs["storage_class"])
	}
	if _, present := attrs["bucket_permissions"]; present {
		t.Errorf("bucket_permissions should be omitted for fullaccess, got %v", attrs["bucket_permissions"])
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.deletePaths) == 0 {
		t.Fatal("expected the key to be revoked on Close, no DELETE was received")
	}
	wantPath := "/storage/access_keys/ci-upload/high_performance"
	for i, p := range mock.deletePaths {
		if p != wantPath {
			t.Errorf("DELETE path = %q, want %q", p, wantPath)
		}
		q := mock.deleteQuery[i]
		if q != "project=proj_mock&region=ASH" && q != "region=ASH&project=proj_mock" {
			t.Errorf("DELETE query = %q, want project=proj_mock&region=ASH", q)
		}
	}
}

// limited_access must serialize bucket_permissions into the create request,
// and a Wasabi-shaped response (access_key_id/secret_access_key + username)
// must win the normalization and drive the revocation path.
func TestAccObjectStorageAccessKey_WasabiLimitedAccess(t *testing.T) {
	mock := &mockAccessKeyAPI{
		responseKey: map[string]any{
			"access_key_id":     "AKIAMOCK",
			"secret_access_key": "WASABISECRET",
			"name":              "ci-upload",
			"username":          "iam-ci-upload",
			"status":            "Active",
		},
	}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6FactoriesWithMockAndEcho(server),
		Steps: []resource.TestStep{
			{
				Config: testAccAccessKeyConfig(`
  access_scope = "limited_access"
  bucket_permissions = [
    { bucket_id = "bkt_mock_1", permission = "readonly" },
  ]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("echo.test", "data.access_key_id", "AKIAMOCK"),
					resource.TestCheckResourceAttr("echo.test", "data.secret_access_key", "WASABISECRET"),
					resource.TestCheckResourceAttr("echo.test", "data.username", "iam-ci-upload"),
				),
			},
		},
	})

	attrs := mock.createAttributes(t, 0)
	if attrs["access_scope"] != "limited_access" {
		t.Errorf("access_scope sent = %v, want limited_access", attrs["access_scope"])
	}
	perms, _ := attrs["bucket_permissions"].([]any)
	if len(perms) != 1 {
		t.Fatalf("bucket_permissions sent = %v, want exactly one entry", attrs["bucket_permissions"])
	}
	perm, _ := perms[0].(map[string]any)
	if perm["bucket_id"] != "bkt_mock_1" || perm["permission"] != "readonly" {
		t.Errorf("bucket_permissions[0] = %v, want bkt_mock_1/readonly", perm)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.deletePaths) == 0 {
		t.Fatal("expected the key to be revoked on Close, no DELETE was received")
	}
	wantPath := "/storage/access_keys/iam-ci-upload/high_performance"
	for _, p := range mock.deletePaths {
		if p != wantPath {
			t.Errorf("DELETE path = %q, want %q (username must win over name)", p, wantPath)
		}
	}
}
