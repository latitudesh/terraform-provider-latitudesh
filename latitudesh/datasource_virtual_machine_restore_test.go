package latitudesh

// These tests exercise the VM restore data source against a local mock of the
// Latitude.sh API, injected through the provider's httpClient (the same hook
// the VCR tests use). They run under TF_ACC without requiring credentials.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

type mockVirtualMachineRestoreAPI struct{}

func (m *mockVirtualMachineRestoreAPI) readyEnvelope() map[string]any {
	return map[string]any{
		"data": map[string]any{
			"id":   "vmr_mock_ready",
			"type": "virtual_machine_restores",
			"attributes": map[string]any{
				"status":     "Ready",
				"created_at": "2026-08-01T00:00:00Z",
				"backup":     map[string]any{"id": "backup_mock_1"},
				"virtual_machine": map[string]any{
					"id":   "vm_mock_1",
					"name": "restored-vm",
				},
				"project": map[string]any{
					"id":   "proj_mock_1",
					"slug": "my-project",
				},
			},
		},
	}
}

func (m *mockVirtualMachineRestoreAPI) creatingEnvelope() map[string]any {
	return map[string]any{
		"data": map[string]any{
			"id":   "vmr_mock_creating",
			"type": "virtual_machine_restores",
			"attributes": map[string]any{
				"status":          "Creating",
				"created_at":      "2026-08-01T00:00:00Z",
				"backup":          map[string]any{"id": "backup_mock_1"},
				"virtual_machine": nil,
			},
		},
	}
}

func (m *mockVirtualMachineRestoreAPI) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.api+json")

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/virtual_machine_restores/vmr_mock_ready":
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.readyEnvelope())

	case r.Method == http.MethodGet && r.URL.Path == "/virtual_machine_restores/vmr_mock_creating":
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.creatingEnvelope())

	case r.Method == http.MethodGet && r.URL.Path == "/virtual_machine_restores/does-not-exist":
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))

	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

func testAccVirtualMachineRestoreConfig(id string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

data "latitudesh_virtual_machine_restore" "test" {
  id = %q
}
`, id)
}

func TestAccVirtualMachineRestore_Ready(t *testing.T) {
	mock := &mockVirtualMachineRestoreAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccVirtualMachineRestoreConfig("vmr_mock_ready"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_virtual_machine_restore.test", "status", "Ready"),
					resource.TestCheckResourceAttr("data.latitudesh_virtual_machine_restore.test", "backup_id", "backup_mock_1"),
					resource.TestCheckResourceAttr("data.latitudesh_virtual_machine_restore.test", "virtual_machine_id", "vm_mock_1"),
					resource.TestCheckResourceAttr("data.latitudesh_virtual_machine_restore.test", "virtual_machine_name", "restored-vm"),
					resource.TestCheckResourceAttr("data.latitudesh_virtual_machine_restore.test", "project", "my-project"),
				),
			},
		},
	})
}

func TestAccVirtualMachineRestore_Creating(t *testing.T) {
	mock := &mockVirtualMachineRestoreAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccVirtualMachineRestoreConfig("vmr_mock_creating"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_virtual_machine_restore.test", "status", "Creating"),
					resource.TestCheckNoResourceAttr("data.latitudesh_virtual_machine_restore.test", "virtual_machine_id"),
				),
			},
		},
	})
}

func TestAccVirtualMachineRestore_NotFound(t *testing.T) {
	mock := &mockVirtualMachineRestoreAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config:      testAccVirtualMachineRestoreConfig("does-not-exist"),
				ExpectError: regexp.MustCompile(`(?i)not found`),
			},
		},
	})
}
