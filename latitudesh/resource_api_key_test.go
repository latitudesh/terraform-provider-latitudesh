package latitudesh

// These tests exercise the API key resource against a local mock of the
// Latitude.sh API, injected through the provider's httpClient (the same hook
// resource_virtual_machine_site_test.go uses for testAccProtoV6ProviderFactoriesWithMock).
// They run under TF_ACC without requiring credentials or creating real resources.
//
// Routes mocked here (confirmed against the SDK's apikeys.go, not guessed):
//
//	POST   /auth/api_keys              -> 201, only response that includes "token"
//	GET    /auth/api_keys               -> 200, list (never includes "token")
//	PATCH  /auth/api_keys/{id}          -> 200, settings update (never includes "token")
//	DELETE /auth/api_keys/{id}          -> 200

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const testAPIKeyMockToken = "lsh_mock_full_token_abcdef"

type mockAPIKeyAPI struct {
	mu         sync.Mutex
	name       string
	readOnly   bool
	allowedIps []string
	exists     bool
	deleted    bool
}

func (m *mockAPIKeyAPI) attributes(includeToken bool) map[string]any {
	attrs := map[string]any{
		"name":             m.name,
		"read_only":        m.readOnly,
		"allowed_ips":      m.allowedIps,
		"token_last_slice": testAPIKeyMockToken[len(testAPIKeyMockToken)-5:],
		"api_version":      "v1",
		"user":             map[string]any{"id": "user_mock_1", "email": "mock@example.com"},
		"created_at":       "2026-07-14T12:00:00Z",
		"updated_at":       "2026-07-14T12:00:00Z",
	}
	if includeToken {
		attrs["token"] = testAPIKeyMockToken
	}
	return attrs
}

func (m *mockAPIKeyAPI) envelope(includeToken bool) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"id":         "key_mock_1",
			"type":       "api_keys",
			"attributes": m.attributes(includeToken),
		},
	}
}

func (m *mockAPIKeyAPI) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/auth/api_keys":
		var payload struct {
			Data struct {
				Attributes struct {
					Name       string   `json:"name"`
					ReadOnly   bool     `json:"read_only"`
					AllowedIps []string `json:"allowed_ips"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		m.name = payload.Data.Attributes.Name
		m.readOnly = payload.Data.Attributes.ReadOnly
		m.allowedIps = payload.Data.Attributes.AllowedIps
		m.exists = true
		m.deleted = false
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(m.envelope(true))

	case r.Method == http.MethodGet && r.URL.Path == "/auth/api_keys":
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		if !m.exists || m.deleted {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}, "meta": map[string]any{}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{m.envelope(false)["data"]},
			"meta": map[string]any{},
		})

	case r.Method == http.MethodPatch && r.URL.Path == "/auth/api_keys/key_mock_1":
		if !m.exists || m.deleted {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		var payload struct {
			Data struct {
				Attributes struct {
					Name       *string  `json:"name"`
					ReadOnly   *bool    `json:"read_only"`
					AllowedIps []string `json:"allowed_ips"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if payload.Data.Attributes.Name != nil {
			m.name = *payload.Data.Attributes.Name
		}
		if payload.Data.Attributes.ReadOnly != nil {
			m.readOnly = *payload.Data.Attributes.ReadOnly
		}
		m.allowedIps = payload.Data.Attributes.AllowedIps
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.envelope(false))

	case r.Method == http.MethodDelete && r.URL.Path == "/auth/api_keys/key_mock_1":
		m.deleted = true
		w.WriteHeader(http.StatusOK)

	default:
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

func testAccCheckMockAPIKeyDestroyed(m *mockAPIKeyAPI) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.exists && !m.deleted {
			return fmt.Errorf("mock API key still exists after destroy")
		}
		return nil
	}
}

func testAccAPIKeyConfig(name string, readOnly bool) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_api_key" "test_item" {
  name      = %q
  read_only = %t
}
`, name, readOnly)
}

// The token is only returned by the create response; it must be preserved in
// state across subsequent refreshes (which only ever see the list response,
// where the API omits it) instead of being nulled out.
func TestAccAPIKey_TokenPersistsAcrossRefresh(t *testing.T) {
	mock := &mockAPIKeyAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resourceName := "latitudesh_api_key.test_item"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockAPIKeyDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccAPIKeyConfig("tf-acc-mock-key", false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "token", testAPIKeyMockToken),
					resource.TestCheckResourceAttr(resourceName, "read_only", "false"),
				),
			},
			{
				// A plan-only refresh must not show a diff on token, even though
				// the mock's list/read response omits it.
				Config:   testAccAPIKeyConfig("tf-acc-mock-key", false),
				PlanOnly: true,
			},
		},
	})
}

// Renaming and toggling read_only must go through UpdateAPIKey (PATCH, no
// rotation) and must not change the token.
func TestAccAPIKey_UpdateSettingsDoesNotRotateToken(t *testing.T) {
	mock := &mockAPIKeyAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resourceName := "latitudesh_api_key.test_item"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockAPIKeyDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccAPIKeyConfig("tf-acc-mock-key", false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "token", testAPIKeyMockToken),
				),
			},
			{
				Config: testAccAPIKeyConfig("tf-acc-mock-key-renamed", true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "name", "tf-acc-mock-key-renamed"),
					resource.TestCheckResourceAttr(resourceName, "read_only", "true"),
					resource.TestCheckResourceAttr(resourceName, "token", testAPIKeyMockToken),
				),
			},
		},
	})
}

// The token cannot be recovered by import (the API never returns it outside
// create/rotate), so it must be excluded from import verification.
func TestAccAPIKey_Import(t *testing.T) {
	mock := &mockAPIKeyAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockAPIKeyDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccAPIKeyConfig("tf-acc-mock-key", false),
			},
			{
				ResourceName:            "latitudesh_api_key.test_item",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"token"},
			},
		},
	})
}
