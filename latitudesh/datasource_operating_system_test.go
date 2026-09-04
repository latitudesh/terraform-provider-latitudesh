package latitudesh

// These tests exercise the operating system data source against a local mock
// of the Latitude.sh API, injected through the provider's httpClient (the
// same hook the VCR tests use). They run under TF_ACC without requiring
// credentials.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

type mockOperatingSystemAPI struct{}

func (m *mockOperatingSystemAPI) listEnvelope() map[string]any {
	return map[string]any{
		"data": []any{
			map[string]any{
				"id":   "os_mock_1",
				"type": "operating_system",
				"attributes": map[string]any{
					"name":             "Ubuntu 22.04 LTS",
					"slug":             "ubuntu_22_04_x64_lts",
					"distro":           "ubuntu",
					"user":             "ubuntu",
					"version":          "22.04",
					"provisionable_on": []string{"c2-small-x86", "c2-medium-x86"},
					"features": map[string]any{
						"raid":       true,
						"ssh_keys":   true,
						"user_data":  true,
						"accelerate": false,
						"rescue":     true,
						"workflow":   false,
					},
				},
			},
			map[string]any{
				"id":   "os_mock_2",
				"type": "operating_system",
				"attributes": map[string]any{
					"name":    "CentOS 7.4",
					"slug":    "centos_7_4_x64",
					"distro":  "centos",
					"user":    "root",
					"version": "7.4",
				},
			},
		},
	}
}

func (m *mockOperatingSystemAPI) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.api+json")

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/plans/operating_systems":
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.listEnvelope())

	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

func testAccOperatingSystemConfig(selector string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

data "latitudesh_operating_system" "test" {
%s
}
`, selector)
}

func TestAccOperatingSystem_BySlug(t *testing.T) {
	mock := &mockOperatingSystemAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccOperatingSystemConfig(`  slug = "ubuntu_22_04_x64_lts"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "id", "os_mock_1"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "name", "Ubuntu 22.04 LTS"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "distro", "ubuntu"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "user", "ubuntu"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "version", "22.04"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "provisionable_on.#", "2"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "features.raid", "true"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "features.accelerate", "false"),
				),
			},
		},
	})
}

func TestAccOperatingSystem_ByID(t *testing.T) {
	mock := &mockOperatingSystemAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccOperatingSystemConfig(`  id = "os_mock_2"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "slug", "centos_7_4_x64"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "user", "root"),
				),
			},
		},
	})
}

func TestAccOperatingSystem_ByName(t *testing.T) {
	mock := &mockOperatingSystemAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccOperatingSystemConfig(`  name = "CentOS 7.4"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "id", "os_mock_2"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "slug", "centos_7_4_x64"),
				),
			},
		},
	})
}

func TestAccOperatingSystem_NotFound(t *testing.T) {
	mock := &mockOperatingSystemAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config:      testAccOperatingSystemConfig(`  slug = "does-not-exist"`),
				ExpectError: regexp.MustCompile(`(?i)not found`),
			},
		},
	})
}
