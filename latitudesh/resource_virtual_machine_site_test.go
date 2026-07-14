package latitudesh

// These tests exercise the virtual machine `site` attribute against a local
// mock of the Latitude.sh API, injected through the provider's httpClient
// (the same hook the VCR tests use). They run under TF_ACC without requiring
// credentials or creating real resources.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

type mockVMAPI struct {
	mu          sync.Mutex
	createdSite *string // site received in the create payload (nil = omitted)
	exists      bool
	deleted     bool
}

// testVMMockSite is the canonical (uppercase) site slug the mock API returns,
// regardless of the case sent in the create payload.
const testVMMockSite = "DAL"

func (m *mockVMAPI) vmEnvelope() map[string]any {
	return map[string]any{
		"data": map[string]any{
			"id":   "vm_mock_1",
			"type": "virtual_machines",
			"attributes": map[string]any{
				"name":             testVMName,
				"site":             testVMMockSite,
				"status":           "Running",
				"primary_ipv4":     "203.0.113.10",
				"created_at":       "2026-07-14T12:00:00Z",
				"operating_system": map[string]any{"slug": "ubuntu_24_04_x64_lts"},
				"plan":             map[string]any{"id": "plan_mock_1", "name": testVMPlan},
				"project":          map[string]any{"id": "proj_mock_1", "slug": "test-project"},
				"credentials":      map[string]any{"username": "ubuntu"},
				"specs":            map[string]any{"vcpu": 1, "ram": "4GB", "storage": "20GB"},
			},
		},
	}
}

func (m *mockVMAPI) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/virtual_machines":
		var payload struct {
			Data struct {
				Attributes map[string]any `json:"attributes"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if s, ok := payload.Data.Attributes["site"].(string); ok {
			m.createdSite = &s
		}
		m.exists = true
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(m.vmEnvelope())

	case r.Method == http.MethodGet && r.URL.Path == "/virtual_machines/vm_mock_1":
		if !m.exists || m.deleted {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.vmEnvelope())

	case r.Method == http.MethodDelete && r.URL.Path == "/virtual_machines/vm_mock_1":
		m.deleted = true
		w.WriteHeader(http.StatusNoContent)

	default:
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

// mockRedirectTransport rewrites every request to the mock server so the SDK's
// hardcoded base URL never reaches the real API.
type mockRedirectTransport struct {
	target *url.URL
}

func (t *mockRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.target.Scheme
	req.URL.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

func testAccProtoV6ProviderFactoriesWithMock(server *httptest.Server) map[string]func() (tfprotov6.ProviderServer, error) {
	target, _ := url.Parse(server.URL)
	httpClient := &http.Client{Transport: &mockRedirectTransport{target: target}}
	return map[string]func() (tfprotov6.ProviderServer, error){
		"latitudesh": providerserver.NewProtocol6WithError(&latitudeshProvider{
			version:    "dev",
			httpClient: httpClient,
		}),
	}
}

func testAccVirtualMachineSiteConfig(siteLine string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_virtual_machine" "test_item" {
  name    = %q
%s
  plan    = %q
  project = "test-project"
}
`, testVMName, siteLine, testVMPlan)
}

func testAccCheckMockVMDestroyed(m *mockVMAPI) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.exists && !m.deleted {
			return fmt.Errorf("mock VM still exists after destroy")
		}
		return nil
	}
}

// A lowercase site slug must be uppercased in the create payload, preserved
// as-is in state, and produce an empty plan on the next run (the API returns
// the canonical uppercase form, which must not trigger a replace).
func TestAccVirtualMachine_SiteCaseInsensitive(t *testing.T) {
	mock := &mockVMAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockVMDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccVirtualMachineSiteConfig(`  site    = "dal"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_virtual_machine.test_item", "site", "dal"),
					resource.TestCheckResourceAttr("latitudesh_virtual_machine.test_item", "status", "Running"),
					func(s *terraform.State) error {
						mock.mu.Lock()
						defer mock.mu.Unlock()
						if mock.createdSite == nil {
							return fmt.Errorf("create payload did not include site")
						}
						if *mock.createdSite != testVMMockSite {
							return fmt.Errorf("create payload site = %q, want %q (uppercased)", *mock.createdSite, testVMMockSite)
						}
						return nil
					},
				),
			},
			{
				Config:   testAccVirtualMachineSiteConfig(`  site    = "dal"`),
				PlanOnly: true,
			},
		},
	})
}

// When site is omitted, the computed value must be populated from the API
// after apply and remain stable on the next plan.
func TestAccVirtualMachine_SiteComputedWhenOmitted(t *testing.T) {
	mock := &mockVMAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockVMDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccVirtualMachineSiteConfig(""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_virtual_machine.test_item", "site", testVMMockSite),
				),
			},
			{
				Config:   testAccVirtualMachineSiteConfig(""),
				PlanOnly: true,
			},
		},
	})
}

// Site must round-trip through import.
func TestAccVirtualMachine_SiteImport(t *testing.T) {
	mock := &mockVMAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockVMDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccVirtualMachineSiteConfig(fmt.Sprintf(`  site    = %q`, testVMMockSite)),
			},
			{
				ResourceName:            "latitudesh_virtual_machine.test_item",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"ssh_keys", "plan", "project"},
			},
		},
	})
}
