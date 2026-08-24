package latitudesh

// These tests exercise the server `bgp_ready` attribute against a local mock of
// the Latitude.sh API, injected through the provider's httpClient (the same
// hook the VCR tests use). They run under TF_ACC without requiring credentials
// or creating real resources.
//
// bgp_ready is a deploy-time only flag: the API accepts it solely on create and
// never returns it. The mock therefore records what the create payload carried
// but never echoes it back on reads, mirroring the real API. The value must
// still survive in state (sourced from config) and produce an empty plan.

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

const testServerBgpHostname = "test-bgp-ready"

type mockBgpServerAPI struct {
	mu             sync.Mutex
	createdBgp     *bool // bgp_ready received in the create payload (nil = omitted)
	createPayloads int
	exists         bool
	deleted        bool
}

func (m *mockBgpServerAPI) serverEnvelope() map[string]any {
	// Note: bgp_ready is intentionally absent — the real API never returns it.
	return map[string]any{
		"data": map[string]any{
			"id":   testServerMockID,
			"type": "servers",
			"attributes": map[string]any{
				"hostname":         testServerBgpHostname,
				"status":           "on",
				"locked":           false,
				"primary_ipv4":     "203.0.113.30",
				"operating_system": map[string]any{"slug": testServerMockOS},
				"plan": map[string]any{
					"id":      "plan_mock_1",
					"name":    "c2.small.x86",
					"slug":    testServerMockPlan,
					"billing": "monthly",
				},
			},
		},
	}
}

func (m *mockBgpServerAPI) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/servers":
		var payload struct {
			Data struct {
				Attributes map[string]any `json:"attributes"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		m.createPayloads++
		if b, ok := payload.Data.Attributes["bgp_ready"].(bool); ok {
			m.createdBgp = &b
		}
		m.exists = true
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(m.serverEnvelope())

	case r.Method == http.MethodGet && r.URL.Path == "/servers/"+testServerMockID:
		if !m.exists || m.deleted {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.serverEnvelope())

	case r.Method == http.MethodGet && r.URL.Path == "/servers/"+testServerMockID+"/deploy_config":
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"id":         "deploy_mock_1",
				"type":       "deploy_config",
				"attributes": map[string]any{},
			},
		})

	case r.Method == http.MethodDelete && r.URL.Path == "/servers/"+testServerMockID:
		m.deleted = true
		w.WriteHeader(http.StatusNoContent)

	default:
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

func testAccServerBgpReadyConfig(bgpLine string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_server" "test_item" {
  hostname         = %q
%s
  plan             = %q
  site             = "ASH"
  operating_system = %q
  project          = "proj_mock_1"
}
`, testServerBgpHostname, bgpLine, testServerMockPlan, testServerMockOS)
}

func testAccCheckMockBgpServerDestroyed(m *mockBgpServerAPI) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.exists && !m.deleted {
			return fmt.Errorf("mock server still exists after destroy")
		}
		return nil
	}
}

// Setting bgp_ready = true must forward it in the create payload, keep it in
// state, and — because the API never returns it — still produce an empty plan
// on the next run (the configured value is preserved, not refreshed away).
func TestAccServer_BgpReadySentOnCreateAndPreserved(t *testing.T) {
	mock := &mockBgpServerAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBgpServerDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccServerBgpReadyConfig(`  bgp_ready        = true`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_server.test_item", "bgp_ready", "true"),
					func(s *terraform.State) error {
						mock.mu.Lock()
						defer mock.mu.Unlock()
						if mock.createdBgp == nil {
							return fmt.Errorf("create payload did not include bgp_ready")
						}
						if *mock.createdBgp != true {
							return fmt.Errorf("create payload bgp_ready = %v, want true", *mock.createdBgp)
						}
						return nil
					},
				),
			},
			{
				Config:   testAccServerBgpReadyConfig(`  bgp_ready        = true`),
				PlanOnly: true,
			},
		},
	})
}

// When bgp_ready is omitted the create payload must not carry it (the API
// applies its own default), and the attribute stays null in state.
func TestAccServer_BgpReadyOmittedByDefault(t *testing.T) {
	mock := &mockBgpServerAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBgpServerDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccServerBgpReadyConfig(""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("latitudesh_server.test_item", "bgp_ready"),
					func(s *terraform.State) error {
						mock.mu.Lock()
						defer mock.mu.Unlock()
						if mock.createdBgp != nil {
							return fmt.Errorf("create payload included bgp_ready = %v; want it omitted", *mock.createdBgp)
						}
						return nil
					},
				),
			},
			{
				Config:   testAccServerBgpReadyConfig(""),
				PlanOnly: true,
			},
		},
	})
}
