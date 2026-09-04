package latitudesh

// These tests exercise the VM restore data source in two tiers:
//
//   - A local mock of the Latitude.sh API, injected through the provider's
//     httpClient (the same hook the VCR tests use). It runs under TF_ACC
//     without requiring credentials and pins the mapping for each status.
//   - A live/VCR tier (TestAccVirtualMachineRestore_Basic) that discovers a
//     real Ready restore through the recorder and reads it back, so the
//     cassette captures the production route and wire shape rather than a
//     shape the mock asserts about itself.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"gopkg.in/dnaeon/go-vcr.v3/recorder"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
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

// TestAccVirtualMachineRestore_Basic reads a real restore through the provider
// so the recorded cassette proves the production /virtual_machine_restores
// route and response shape, not just the shape the mock above asserts.
//
// Discovery goes through the same recorder as the provider, so the List call
// is captured in the cassette too and the whole test replays offline with
// LATITUDE_TEST_RECORDER=play. Restores are ephemeral and account-specific,
// so nothing is hardcoded: the test skips when the account has no Ready
// restore instead of failing, which keeps e2e safe on accounts without one.
func TestAccVirtualMachineRestore_Basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	// Discovery runs before resource.Test (and therefore before its PreCheck),
	// so the token has to be checked up front as well.
	testAccTokenCheck(t)

	rec, teardown := createTestRecorder(t)
	defer teardown()

	restoreID := testAccReadyVirtualMachineRestoreID(t, rec)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(rec),
		Steps: []resource.TestStep{
			{
				Config: testAccVirtualMachineRestoreLiveConfig(restoreID),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_virtual_machine_restore.test", "id", restoreID),
					resource.TestCheckResourceAttr("data.latitudesh_virtual_machine_restore.test", "status", "Ready"),
					resource.TestCheckResourceAttrSet("data.latitudesh_virtual_machine_restore.test", "backup_id"),
					resource.TestCheckResourceAttrSet("data.latitudesh_virtual_machine_restore.test", "virtual_machine_id"),
					resource.TestCheckResourceAttrSet("data.latitudesh_virtual_machine_restore.test", "virtual_machine_name"),
					resource.TestCheckResourceAttrSet("data.latitudesh_virtual_machine_restore.test", "project"),
				),
			},
			{
				// Records the real 404 wire shape, which the mock can only assume.
				Config:      testAccVirtualMachineRestoreLiveConfig("vmrst_does_not_exist"),
				ExpectError: regexp.MustCompile(`(?i)not found`),
			},
		},
	})
}

// testAccReadyVirtualMachineRestoreID lists the team's restores through the
// recorder and returns the first one that is Ready — the only status whose
// virtual_machine fields are populated, which is what the Check asserts.
// It skips when there is none, so the test never fails on an account that
// has no restore to read.
func testAccReadyVirtualMachineRestoreID(t *testing.T, rec *recorder.Recorder) string {
	t.Helper()

	res, err := createVCRClient(rec).VirtualMachineRestores.List(context.Background())
	if err != nil {
		t.Fatalf("discovering VM restores via VirtualMachineRestores.List: %s", err)
	}
	if res.VirtualMachineRestores == nil {
		t.Skip("no VM restores in this account; restore a backup before recording")
	}
	for _, r := range res.VirtualMachineRestores.Data {
		if r.ID == nil || r.Attributes == nil || r.Attributes.Status == nil {
			continue
		}
		if *r.Attributes.Status == components.VirtualMachineRestoreAttributesStatusReady {
			return *r.ID
		}
	}
	t.Skip("no Ready VM restore in this account; wait for one to finish (or restore a backup) before recording")
	return ""
}

// testAccVirtualMachineRestoreLiveConfig omits the provider block on purpose:
// the live tier authenticates from LATITUDESH_AUTH_TOKEN like the other VCR
// tests, unlike the mock tier above which pins a fake token.
func testAccVirtualMachineRestoreLiveConfig(id string) string {
	return fmt.Sprintf(`
data "latitudesh_virtual_machine_restore" "test" {
  id = %q
}
`, id)
}
