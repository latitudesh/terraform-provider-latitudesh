package latitudesh

// These tests exercise the marketplace app data source against a local mock of
// the Latitude.sh API, injected through the provider's httpClient (the same
// hook the VCR tests use). They run under TF_ACC without requiring credentials.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

type mockMarketplaceAppAPI struct{}

// appEnvelope deliberately includes the presentation-only fields the data
// source does not expose (description, URLs, logo, created_at): the real API
// sends them, and the provider must ignore them without erroring.
func (m *mockMarketplaceAppAPI) appEnvelope() map[string]any {
	return map[string]any{
		"data": map[string]any{
			"id":   "mkapp_mock_1",
			"type": "marketplace_apps",
			"attributes": map[string]any{
				"name":                     "WordPress",
				"slug":                     "wordpress",
				"description":              "WordPress is a popular open-source CMS.",
				"short_description":        "Open-source CMS",
				"category":                 "cms",
				"version":                  "6.5.0",
				"system_requirements":      map[string]any{"vcpus": 2, "memory_in_gb": 4, "storage_in_gb": 40, "gpu": false},
				"deployment_strategy":      "user_data",
				"default_operating_system": "ubuntu_24_04_x64_lts",
				"compatible_plans":         []string{"c2-small-x86", "c2-medium-x86"},
				"access_instructions":      "Visit https://<ip>/wp-admin to finish setup.",
				"upstream_url":             "https://wordpress.org",
				"documentation_url":        "https://wordpress.org/documentation/",
				"logo_url":                 "https://example.com/wordpress.png",
				"created_at":               "2026-01-01T00:00:00Z",
			},
		},
	}
}

func (m *mockMarketplaceAppAPI) listEnvelope() map[string]any {
	env := m.appEnvelope()
	return map[string]any{
		"data": []any{env["data"]},
	}
}

func (m *mockMarketplaceAppAPI) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.api+json")

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/marketplace_apps/wordpress":
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.appEnvelope())

	case r.Method == http.MethodGet && r.URL.Path == "/marketplace_apps/mkapp_mock_1":
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.appEnvelope())

	case r.Method == http.MethodGet && r.URL.Path == "/marketplace_apps/does-not-exist":
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))

	case r.Method == http.MethodGet && r.URL.Path == "/marketplace_apps":
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.listEnvelope())

	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

func testAccMarketplaceAppConfig(selector string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

data "latitudesh_marketplace_app" "test" {
%s
}
`, selector)
}

func TestAccMarketplaceApp_BySlug(t *testing.T) {
	mock := &mockMarketplaceAppAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccMarketplaceAppConfig(`  slug = "wordpress"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_app.test", "id", "mkapp_mock_1"),
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_app.test", "name", "WordPress"),
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_app.test", "category", "cms"),
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_app.test", "version", "6.5.0"),
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_app.test", "deployment_strategy", "user_data"),
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_app.test", "system_requirements.vcpus", "2"),
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_app.test", "system_requirements.gpu", "false"),
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_app.test", "compatible_plans.#", "2"),
				),
			},
		},
	})
}

func TestAccMarketplaceApp_ByID(t *testing.T) {
	mock := &mockMarketplaceAppAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccMarketplaceAppConfig(`  id = "mkapp_mock_1"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_app.test", "slug", "wordpress"),
				),
			},
		},
	})
}

func TestAccMarketplaceApp_ByName(t *testing.T) {
	mock := &mockMarketplaceAppAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccMarketplaceAppConfig(`  name = "WordPress"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_app.test", "id", "mkapp_mock_1"),
					resource.TestCheckResourceAttr("data.latitudesh_marketplace_app.test", "slug", "wordpress"),
				),
			},
		},
	})
}

func TestAccMarketplaceApp_NotFound(t *testing.T) {
	mock := &mockMarketplaceAppAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config:      testAccMarketplaceAppConfig(`  slug = "does-not-exist"`),
				ExpectError: regexp.MustCompile(`(?i)not found`),
			},
		},
	})
}
