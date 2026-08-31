package latitudesh

// These tests exercise the plural marketplace apps data source against a local
// mock of the Latitude.sh API, injected through the provider's httpClient (the
// same hook the VCR tests use). They run under TF_ACC without credentials.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

type mockMarketplaceAppsAPI struct{}

func (m *mockMarketplaceAppsAPI) listEnvelope() map[string]any {
	return map[string]any{
		"data": []any{
			map[string]any{
				"id":   "mkapp_mock_1",
				"type": "marketplace_apps",
				"attributes": map[string]any{
					"name":                     "WordPress",
					"slug":                     "wordpress",
					"category":                 "cms",
					"version":                  "6.5.0",
					"system_requirements":      map[string]any{"vcpus": 2, "memory_in_gb": 4, "storage_in_gb": 40, "gpu": false},
					"deployment_strategy":      "user_data",
					"default_operating_system": "ubuntu_24_04_x64_lts",
					"compatible_plans":         []string{"c2-small-x86", "c2-medium-x86"},
				},
			},
			map[string]any{
				"id":   "mkapp_mock_2",
				"type": "marketplace_apps",
				"attributes": map[string]any{
					"name":                     "Grafana",
					"slug":                     "grafana",
					"category":                 "monitoring",
					"version":                  "11.0.0",
					"system_requirements":      map[string]any{"vcpus": 2, "memory_in_gb": 2, "storage_in_gb": 20, "gpu": false},
					"deployment_strategy":      "user_data",
					"default_operating_system": "ubuntu_24_04_x64_lts",
					"compatible_plans":         []string{"c2-small-x86"},
				},
			},
		},
	}
}

func (m *mockMarketplaceAppsAPI) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.api+json")

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/marketplace_apps":
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.listEnvelope())

	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

func testAccMarketplaceAppsConfig(filter string) string {
	return `
provider "latitudesh" {
  auth_token = "mock-token"
}

data "latitudesh_marketplace_apps" "test" {
` + filter + `
}
`
}

func TestAccMarketplaceApps_All(t *testing.T) {
	mock := &mockMarketplaceAppsAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccMarketplaceAppsConfig(``),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_apps.test", "apps.#", "2"),
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_apps.test", "apps.0.slug", "wordpress"),
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_apps.test", "apps.0.category", "cms"),
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_apps.test", "apps.0.system_requirements.vcpus", "2"),
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_apps.test", "apps.0.compatible_plans.#", "2"),
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_apps.test", "apps.1.slug", "grafana"),
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_apps.test", "apps.1.category", "monitoring"),
				),
			},
		},
	})
}

func TestAccMarketplaceApps_FilterByCategory(t *testing.T) {
	mock := &mockMarketplaceAppsAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				// Case-insensitive match on category.
				Config: testAccMarketplaceAppsConfig(`  category = "Monitoring"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_apps.test", "apps.#", "1"),
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_apps.test", "apps.0.slug", "grafana"),
				),
			},
		},
	})
}

func TestAccMarketplaceApps_FilterNoMatch(t *testing.T) {
	mock := &mockMarketplaceAppsAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccMarketplaceAppsConfig(`  category = "does-not-exist"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_apps.test", "apps.#", "0"),
				),
			},
		},
	})
}
