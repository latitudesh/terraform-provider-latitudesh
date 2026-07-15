package latitudesh

// These tests exercise the server `billing` attribute against a local mock of
// the Latitude.sh API, injected through the provider's httpClient (the same
// hook the VCR tests use). They run under TF_ACC without requiring
// credentials or creating real resources.

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

const (
	testServerMockID       = "sv_mock_1"
	testServerMockHostname = "test-billing"
	testServerMockPlan     = "c2-small-x86"
	testServerMockOS       = "ubuntu_24_04_x64_lts"
)

type mockServerAPI struct {
	mu             sync.Mutex
	createdBilling *string // billing received in the create payload (nil = omitted)
	billing        string  // billing the mock reports back on reads
	exists         bool
	deleted        bool
}

func (m *mockServerAPI) serverEnvelope() map[string]any {
	return map[string]any{
		"data": map[string]any{
			"id":   testServerMockID,
			"type": "servers",
			"attributes": map[string]any{
				"hostname":         testServerMockHostname,
				"status":           "on",
				"locked":           false,
				"primary_ipv4":     "203.0.113.20",
				"operating_system": map[string]any{"slug": testServerMockOS},
				"plan": map[string]any{
					"id":      "plan_mock_1",
					"name":    "c2.small.x86",
					"slug":    testServerMockPlan,
					"billing": m.billing,
				},
			},
		},
	}
}

func (m *mockServerAPI) handler(w http.ResponseWriter, r *http.Request) {
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
		if b, ok := payload.Data.Attributes["billing"].(string); ok {
			m.createdBilling = &b
			m.billing = b
		} else {
			// Mirror the real API: monthly when billing is omitted.
			m.billing = "monthly"
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

func testAccServerBillingConfig(billingLine string) string {
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
`, testServerMockHostname, billingLine, testServerMockPlan, testServerMockOS)
}

func testAccCheckMockServerDestroyed(m *mockServerAPI) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.exists && !m.deleted {
			return fmt.Errorf("mock server still exists after destroy")
		}
		return nil
	}
}

// When billing is omitted, the DefaultOnCreate plan modifier must resolve it
// to "monthly" at plan time, so the create payload carries it explicitly and
// state ends up with "monthly". The next plan must be empty.
func TestAccServer_BillingDefaultsToMonthlyWhenOmitted(t *testing.T) {
	mock := &mockServerAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockServerDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccServerBillingConfig(""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_server.test_item", "billing", "monthly"),
					func(s *terraform.State) error {
						mock.mu.Lock()
						defer mock.mu.Unlock()
						if mock.createdBilling == nil {
							return fmt.Errorf("create payload did not include billing; the plan-time default was not applied")
						}
						if *mock.createdBilling != "monthly" {
							return fmt.Errorf("create payload billing = %q, want %q", *mock.createdBilling, "monthly")
						}
						return nil
					},
				),
			},
			{
				Config:   testAccServerBillingConfig(""),
				PlanOnly: true,
			},
		},
	})
}

// Removing the billing attribute from the config of an existing server must
// preserve the value in state — it must not fall back to the create default
// (which would plan a billing change and, for yearly servers, fail the plan
// as a forbidden downgrade).
func TestAccServer_BillingPreservedWhenRemovedFromConfig(t *testing.T) {
	mock := &mockServerAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockServerDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccServerBillingConfig(`  billing          = "hourly"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_server.test_item", "billing", "hourly"),
				),
			},
			{
				Config:   testAccServerBillingConfig(""),
				PlanOnly: true,
			},
			{
				Config: testAccServerBillingConfig(""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_server.test_item", "billing", "hourly"),
				),
			},
		},
	})
}
