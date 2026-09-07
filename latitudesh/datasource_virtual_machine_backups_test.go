package latitudesh

// Tests for the plural virtual machine backups data source, in three tiers:
// offline unit tests for the pure helpers (ordering, status filter, item
// mapping), mock-backed acceptance tests (TF_ACC, no credentials) covering the
// team-wide and VM-scoped list paths plus the not-found error, and a live/VCR
// tier that records the real list endpoint through the recorder.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func mkBackup(id, vmID, vmName, status, createdAt string) components.VirtualMachineBackupAttributes {
	st := components.VirtualMachineBackupAttributesStatus(status)
	slug := "lab"
	return components.VirtualMachineBackupAttributes{
		ID: &id,
		Attributes: &components.VirtualMachineBackupAttributesAttributes{
			Status:         &st,
			CreatedAt:      &createdAt,
			VirtualMachine: &components.VirtualMachineBackupAttributesVirtualMachine{ID: &vmID, Name: &vmName},
			Project:        &components.ProjectInclude{Slug: &slug},
		},
	}
}

func TestSortVirtualMachineBackupsNewestFirst(t *testing.T) {
	backups := []components.VirtualMachineBackupAttributes{
		mkBackup("old", "vm_a", "a", "Ready", "2026-09-01T00:00:00Z"),
		mkBackup("bad", "vm_a", "a", "Ready", "not-a-timestamp"),
		mkBackup("new", "vm_a", "a", "Ready", "2026-09-03T00:00:00Z"),
		mkBackup("mid", "vm_a", "a", "Ready", "2026-09-02T00:00:00+00:00"),
	}
	sortVirtualMachineBackupsNewestFirst(backups)
	got := []string{*backups[0].ID, *backups[1].ID, *backups[2].ID, *backups[3].ID}
	want := []string{"new", "mid", "old", "bad"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestBackupMatchesStatus(t *testing.T) {
	ready := mkBackup("b", "vm", "n", "Ready", "2026-09-01T00:00:00Z")
	if !backupMatchesStatus(ready, "") || !backupMatchesStatus(ready, "ready") || !backupMatchesStatus(ready, "READY") {
		t.Error("empty filter and case-insensitive match should pass")
	}
	if backupMatchesStatus(ready, "Failed") {
		t.Error("different status must not match")
	}
	noStatus := components.VirtualMachineBackupAttributes{Attributes: &components.VirtualMachineBackupAttributesAttributes{}}
	if backupMatchesStatus(noStatus, "Ready") || !backupMatchesStatus(noStatus, "") {
		t.Error("missing status matches only the empty filter")
	}
}

func TestVirtualMachineBackupItemValue(t *testing.T) {
	b := mkBackup("vmbkp_1", "vm_a", "alpha", "Ready", "2026-09-01T00:00:00Z")
	item := virtualMachineBackupItemValue(&b)
	if item.ID.ValueString() != "vmbkp_1" || item.VirtualMachine.ValueString() != "vm_a" ||
		item.VirtualMachineName.ValueString() != "alpha" || item.Status.ValueString() != "Ready" ||
		item.Project.ValueString() != "lab" || item.CreatedAt.ValueString() != "2026-09-01T00:00:00Z" {
		t.Errorf("unexpected item: %+v", item)
	}
	id := "vmbkp_2"
	bare := virtualMachineBackupItemValue(&components.VirtualMachineBackupAttributes{ID: &id})
	if bare.ID.ValueString() != "vmbkp_2" || !bare.VirtualMachineName.IsNull() || !bare.Project.IsNull() || !bare.Status.IsNull() {
		t.Errorf("nil attributes should map to nulls: %+v", bare)
	}
}

// --- mock-backed acceptance tier -------------------------------------------

type mockVirtualMachineBackupsAPI struct{}

func (m *mockVirtualMachineBackupsAPI) item(id, vmID, vmName, status, createdAt string) string {
	return fmt.Sprintf(`{"id":%q,"type":"virtual_machine_backups","attributes":{"status":%q,"size_bytes":1024,"created_at":%q,"virtual_machine":{"id":%q,"name":%q},"project":{"id":"proj_1","slug":"lab"}}}`,
		id, status, createdAt, vmID, vmName)
}

func (m *mockVirtualMachineBackupsAPI) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.api+json")
	old := m.item("vmbkp_old", "vm_a", "alpha", "Ready", "2026-09-01T00:00:00Z")
	mid := m.item("vmbkp_b", "vm_b", "beta", "Creating", "2026-09-02T00:00:00Z")
	newest := m.item("vmbkp_new", "vm_a", "alpha", "Ready", "2026-09-03T00:00:00Z")

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/virtual_machine_backups":
		// API order is deliberately not newest-first, to prove the sort.
		_, _ = fmt.Fprintf(w, `{"data":[%s,%s,%s]}`, old, mid, newest)
	case r.Method == http.MethodGet && r.URL.Path == "/virtual_machines/vm_a/backups":
		_, _ = fmt.Fprintf(w, `{"data":[%s,%s]}`, old, newest)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

func testAccVirtualMachineBackupsMockConfig(body string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

data "latitudesh_virtual_machine_backups" "test" {
%s
}
`, body)
}

const backupsDS = "data.latitudesh_virtual_machine_backups.test"

func TestAccVirtualMachineBackups_TeamWideNewestFirst(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc((&mockVirtualMachineBackupsAPI{}).handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{{
			Config: testAccVirtualMachineBackupsMockConfig(""),
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr(backupsDS, "id", "all"),
				resource.TestCheckResourceAttr(backupsDS, "backups.#", "3"),
				resource.TestCheckResourceAttr(backupsDS, "backups.0.id", "vmbkp_new"),
				resource.TestCheckResourceAttr(backupsDS, "backups.1.id", "vmbkp_b"),
				resource.TestCheckResourceAttr(backupsDS, "backups.2.id", "vmbkp_old"),
				resource.TestCheckResourceAttr(backupsDS, "backups.0.virtual_machine", "vm_a"),
				resource.TestCheckResourceAttr(backupsDS, "backups.0.virtual_machine_name", "alpha"),
				resource.TestCheckResourceAttr(backupsDS, "backups.0.project", "lab"),
				resource.TestCheckResourceAttr(backupsDS, "backups.0.size_bytes", "1024"),
			),
		}},
	})
}

func TestAccVirtualMachineBackups_ByVMAndStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc((&mockVirtualMachineBackupsAPI{}).handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccVirtualMachineBackupsMockConfig(`  virtual_machine = "vm_a"
  status          = "ready"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(backupsDS, "id", "vm_a/ready"),
					resource.TestCheckResourceAttr(backupsDS, "backups.#", "2"),
					resource.TestCheckResourceAttr(backupsDS, "backups.0.id", "vmbkp_new"),
					resource.TestCheckResourceAttr(backupsDS, "backups.1.id", "vmbkp_old"),
				),
			},
			{
				Config: testAccVirtualMachineBackupsMockConfig(`  virtual_machine = "vm_a"
  status          = "Failed"`),
				Check: resource.TestCheckResourceAttr(backupsDS, "backups.#", "0"),
			},
		},
	})
}

func TestAccVirtualMachineBackups_VMNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc((&mockVirtualMachineBackupsAPI{}).handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{{
			Config:      testAccVirtualMachineBackupsMockConfig(`  virtual_machine = "vm_missing"`),
			ExpectError: regexp.MustCompile(`(?i)virtual machine not found`),
		}},
	})
}

func TestAccVirtualMachineBackups_InvalidStatusRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc((&mockVirtualMachineBackupsAPI{}).handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{{
			// A typo must fail at plan time instead of silently matching nothing.
			Config:      testAccVirtualMachineBackupsMockConfig(`  status = "Readyy"`),
			ExpectError: regexp.MustCompile(`(?i)value must be one of`),
		}},
	})
}

// --- live / VCR tier --------------------------------------------------------

// TestAccVirtualMachineBackups_Basic lists the team's backups through the
// recorder so the cassette captures the real list endpoint and wire shape.
// It asserts only what holds for any account: the synthetic id and a
// well-formed list (possibly empty).
func TestAccVirtualMachineBackups_Basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	rec, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(rec),
		Steps: []resource.TestStep{{
			Config: `
data "latitudesh_virtual_machine_backups" "test" {}
`,
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr(backupsDS, "id", "all"),
				resource.TestMatchResourceAttr(backupsDS, "backups.#", regexp.MustCompile(`^\d+$`)),
			),
		}},
	})
}
