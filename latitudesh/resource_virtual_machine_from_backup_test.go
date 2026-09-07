package latitudesh

// Tests for provisioning a virtual machine from a backup (backup_id). Two tiers:
//
//   - Direct tests of waitForRestoreReady against a scripted mock of
//     GET /virtual_machine_restores/{id}, mirroring the backup waiter suite:
//     Ready (with and without a VM id), Failed, transient 404/5xx retries, a
//     fatal client error, the consecutive-error ceiling, and the timeout.
//   - Mock-backed acceptance tests (TF_ACC, no credentials) that drive the
//     whole create-from-backup path through the provider: backup pre-flight,
//     restore POST, adoption of the produced VM, read-back, the post-restore
//     billing reconcile, destroy, and the schema-level validation errors.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// --- waitForRestoreReady -----------------------------------------------------

const mockRestoreID = "vmrst_lifecycle1"

type vmRestoreGetStep struct {
	httpStatus int
	status     string
	vmID       string
}

type vmRestoreMock struct {
	mu       sync.Mutex
	steps    []vmRestoreGetStep
	getCount int
}

func (m *vmRestoreMock) next() vmRestoreGetStep {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.getCount
	m.getCount++
	if idx >= len(m.steps) {
		idx = len(m.steps) - 1
	}
	return m.steps[idx]
}

func (m *vmRestoreMock) gets() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getCount
}

func (m *vmRestoreMock) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.api+json")
	if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/virtual_machine_restores/") {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
		return
	}
	step := m.next()
	if step.httpStatus != http.StatusOK {
		w.WriteHeader(step.httpStatus)
		_, _ = fmt.Fprintf(w, `{"errors":[{"status":"%d"}]}`, step.httpStatus)
		return
	}
	w.WriteHeader(http.StatusOK)
	vm := "null"
	if step.vmID != "" {
		vm = fmt.Sprintf(`{"id":%q,"name":"restored"}`, step.vmID)
	}
	_, _ = fmt.Fprintf(w, `{"data":{"id":%q,"type":"virtual_machine_restores","attributes":{"status":%q,"backup":{"id":"vmbkp_1"},"virtual_machine":%s}}}`,
		mockRestoreID, step.status, vm)
}

func newVMRestoreMock(t *testing.T, steps ...vmRestoreGetStep) (*vmRestoreMock, *VirtualMachineResource) {
	t.Helper()
	if len(steps) == 0 {
		t.Fatal("newVMRestoreMock requires at least one step")
	}
	mock := &vmRestoreMock{steps: steps}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	prev := vmRestoreReadyPollInterval
	vmRestoreReadyPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { vmRestoreReadyPollInterval = prev })

	r := &VirtualMachineResource{
		client: latitudeshgosdk.New(
			latitudeshgosdk.WithSecurity("test"),
			latitudeshgosdk.WithServerURL(server.URL),
		),
	}
	return mock, r
}

func TestWaitForRestoreReady_ReadyImmediately(t *testing.T) {
	mock, r := newVMRestoreMock(t, vmRestoreGetStep{http.StatusOK, "Ready", "vm_restored"})
	var diags diag.Diagnostics
	got := r.waitForRestoreReady(context.Background(), mockRestoreID, time.Second, &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if got != "vm_restored" {
		t.Errorf("vm id = %q, want vm_restored", got)
	}
	if mock.gets() != 1 {
		t.Errorf("gets = %d, want 1", mock.gets())
	}
}

func TestWaitForRestoreReady_CreatingThenReady(t *testing.T) {
	mock, r := newVMRestoreMock(t,
		vmRestoreGetStep{http.StatusOK, "Creating", ""},
		vmRestoreGetStep{http.StatusOK, "Creating", ""},
		vmRestoreGetStep{http.StatusOK, "Ready", "vm_restored"},
	)
	var diags diag.Diagnostics
	got := r.waitForRestoreReady(context.Background(), mockRestoreID, time.Second, &diags)
	if diags.HasError() || got != "vm_restored" {
		t.Fatalf("got %q, diags %v", got, diags)
	}
	if mock.gets() != 3 {
		t.Errorf("gets = %d, want 3", mock.gets())
	}
}

func TestWaitForRestoreReady_ReadyWithoutVMIDIsError(t *testing.T) {
	_, r := newVMRestoreMock(t, vmRestoreGetStep{http.StatusOK, "Ready", ""})
	var diags diag.Diagnostics
	got := r.waitForRestoreReady(context.Background(), mockRestoreID, time.Second, &diags)
	if got != "" || !diags.HasError() {
		t.Fatalf("expected an error and no id, got %q / %v", got, diags)
	}
	if !strings.Contains(diags.Errors()[0].Detail(), "no virtual machine id") {
		t.Errorf("unexpected detail: %s", diags.Errors()[0].Detail())
	}
}

func TestWaitForRestoreReady_FailedIsTerminal(t *testing.T) {
	mock, r := newVMRestoreMock(t, vmRestoreGetStep{http.StatusOK, "Failed", ""})
	var diags diag.Diagnostics
	got := r.waitForRestoreReady(context.Background(), mockRestoreID, time.Second, &diags)
	if got != "" || !diags.HasError() {
		t.Fatalf("expected failure, got %q / %v", got, diags)
	}
	if diags.Errors()[0].Summary() != "Virtual Machine Restore Failed" {
		t.Errorf("summary = %q", diags.Errors()[0].Summary())
	}
	if !strings.Contains(diags.Errors()[0].Detail(), mockRestoreID) {
		t.Errorf("detail should name the restore id: %s", diags.Errors()[0].Detail())
	}
	if mock.gets() != 1 {
		t.Errorf("gets = %d, want 1 (Failed is terminal)", mock.gets())
	}
}

func TestWaitForRestoreReady_TransientNotFoundThenReady(t *testing.T) {
	mock, r := newVMRestoreMock(t,
		vmRestoreGetStep{httpStatus: http.StatusNotFound},
		vmRestoreGetStep{http.StatusOK, "Ready", "vm_restored"},
	)
	var diags diag.Diagnostics
	got := r.waitForRestoreReady(context.Background(), mockRestoreID, time.Second, &diags)
	if diags.HasError() || got != "vm_restored" {
		t.Fatalf("got %q, diags %v", got, diags)
	}
	if mock.gets() != 2 {
		t.Errorf("gets = %d, want 2", mock.gets())
	}
}

func TestWaitForRestoreReady_TransientServerErrorThenReady(t *testing.T) {
	_, r := newVMRestoreMock(t,
		vmRestoreGetStep{httpStatus: http.StatusBadGateway},
		vmRestoreGetStep{http.StatusOK, "Ready", "vm_restored"},
	)
	var diags diag.Diagnostics
	if got := r.waitForRestoreReady(context.Background(), mockRestoreID, time.Second, &diags); diags.HasError() || got != "vm_restored" {
		t.Fatalf("got %q, diags %v", got, diags)
	}
}

func TestWaitForRestoreReady_FatalClientErrorStopsImmediately(t *testing.T) {
	mock, r := newVMRestoreMock(t, vmRestoreGetStep{httpStatus: http.StatusUnauthorized})
	var diags diag.Diagnostics
	got := r.waitForRestoreReady(context.Background(), mockRestoreID, time.Second, &diags)
	if got != "" || !diags.HasError() {
		t.Fatalf("expected error, got %q / %v", got, diags)
	}
	if mock.gets() != 1 {
		t.Errorf("gets = %d, want 1 (401 must not be retried)", mock.gets())
	}
}

func TestWaitForRestoreReady_ConsecutiveErrorCeiling(t *testing.T) {
	mock, r := newVMRestoreMock(t, vmRestoreGetStep{httpStatus: http.StatusInternalServerError})
	var diags diag.Diagnostics
	got := r.waitForRestoreReady(context.Background(), mockRestoreID, 5*time.Second, &diags)
	if got != "" || !diags.HasError() {
		t.Fatalf("expected error, got %q / %v", got, diags)
	}
	if mock.gets() != 5 {
		t.Errorf("gets = %d, want 5 (ceiling)", mock.gets())
	}
}

func TestWaitForRestoreReady_Timeout(t *testing.T) {
	_, r := newVMRestoreMock(t, vmRestoreGetStep{http.StatusOK, "Creating", ""})
	var diags diag.Diagnostics
	got := r.waitForRestoreReady(context.Background(), mockRestoreID, 30*time.Millisecond, &diags)
	if got != "" || !diags.HasError() {
		t.Fatalf("expected timeout, got %q / %v", got, diags)
	}
	e := diags.Errors()[0]
	if e.Summary() != "Timeout waiting for virtual machine restore" {
		t.Errorf("summary = %q", e.Summary())
	}
	if !strings.Contains(e.Detail(), mockRestoreID) || !strings.Contains(e.Detail(), `"Creating"`) || !strings.Contains(e.Detail(), "import") {
		t.Errorf("detail should name the restore id, last status and the import recovery: %s", e.Detail())
	}
}

func TestProjectMatches(t *testing.T) {
	id, slug := "proj_1", "My-Lab"
	p := &components.ProjectInclude{ID: &id, Slug: &slug}
	cases := map[string]bool{"proj_1": true, "my-lab": true, "MY-LAB": true, "other": false, "": false}
	for want, ok := range cases {
		if got := projectMatches(want, p); got != ok {
			t.Errorf("projectMatches(%q) = %v, want %v", want, got, ok)
		}
	}
	if projectMatches("proj_1", nil) {
		t.Error("nil project must not match")
	}
	if projectLabel(p) != "My-Lab" || projectLabel(&components.ProjectInclude{ID: &id}) != "proj_1" || projectLabel(nil) != "" {
		t.Error("projectLabel should prefer slug, fall back to id, and be empty for nil")
	}
}

// --- create from backup, end to end against a mock API ----------------------

// mockVMFromBackupAPI serves the endpoints the from-backup path touches:
// backup pre-flight GETs, the restore POST and its GET, and the adopted VM's
// GET/PATCH/DELETE. It keeps just enough state to echo the restore's name,
// apply a billing PATCH, and 404 the VM after it is deleted.
type mockVMFromBackupAPI struct {
	mu       sync.Mutex
	name     string
	billing  string
	deleted  bool
	restores int
	patches  int
}

func (m *mockVMFromBackupAPI) vmJSON() string {
	return fmt.Sprintf(`{"data":{"id":"vm_restored","type":"virtual_machines","attributes":{"status":"Running","name":%q,"primary_ipv4":"10.10.0.5","site":"ASH","billing":%q,"created_at":"2026-09-04T22:02:08+00:00","plan":{"id":"plan_small","name":"vm.small","slug":"vm-small"},"operating_system":{"name":"Ubuntu","slug":"ubuntu_24_04_x64_lts","version":"24.04"},"project":{"id":"proj_lab","slug":"lab"},"specs":{"vcpu":4,"ram":"16 GB","storage":"160 GB"}}}}`,
		m.name, m.billing)
}

func (m *mockVMFromBackupAPI) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.api+json")
	m.mu.Lock()
	defer m.mu.Unlock()

	write := func(code int, body string) {
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}
	backup := func(id, status, project string) string {
		return fmt.Sprintf(`{"data":{"id":%q,"type":"virtual_machine_backups","attributes":{"status":%q,"created_at":"2026-09-01T00:00:00Z","virtual_machine":{"id":"vm_source","name":"source"},"project":{"id":"proj_%s","slug":%q}}}}`, id, status, project, project)
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/virtual_machine_backups/vmbkp_ready":
		write(http.StatusOK, backup("vmbkp_ready", "Ready", "lab"))
	case r.Method == http.MethodGet && r.URL.Path == "/virtual_machine_backups/vmbkp_creating":
		write(http.StatusOK, backup("vmbkp_creating", "Creating", "lab"))
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/virtual_machine_backups/"):
		write(http.StatusNotFound, `{"errors":[{"status":"404"}]}`)

	case r.Method == http.MethodPost && r.URL.Path == "/virtual_machine_restores":
		var body struct {
			Data struct {
				Attributes struct {
					Backup *string `json:"backup"`
					Name   *string `json:"name"`
				} `json:"attributes"`
			} `json:"data"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Data.Attributes.Name != nil {
			m.name = *body.Data.Attributes.Name
		}
		m.restores++
		write(http.StatusCreated, `{"data":{"id":"vmrst_mock","type":"virtual_machine_restores","attributes":{"status":"Creating","backup":{"id":"vmbkp_ready"},"virtual_machine":null}}}`)
	case r.Method == http.MethodGet && r.URL.Path == "/virtual_machine_restores/vmrst_mock":
		write(http.StatusOK, fmt.Sprintf(`{"data":{"id":"vmrst_mock","type":"virtual_machine_restores","attributes":{"status":"Ready","backup":{"id":"vmbkp_ready"},"virtual_machine":{"id":"vm_restored","name":%q}}}}`, m.name))

	case r.URL.Path == "/virtual_machines/vm_restored" && r.Method == http.MethodGet:
		if m.deleted {
			write(http.StatusNotFound, `{"errors":[{"status":"404"}]}`)
			return
		}
		write(http.StatusOK, m.vmJSON())
	case r.URL.Path == "/virtual_machines/vm_restored" && r.Method == http.MethodPatch:
		var body struct {
			Data struct {
				Attributes struct {
					Billing *string `json:"billing"`
				} `json:"attributes"`
			} `json:"data"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Data.Attributes.Billing != nil {
			m.billing = *body.Data.Attributes.Billing
		}
		m.patches++
		write(http.StatusOK, m.vmJSON())
	case r.URL.Path == "/virtual_machines/vm_restored" && r.Method == http.MethodDelete:
		m.deleted = true
		w.WriteHeader(http.StatusNoContent)

	default:
		write(http.StatusNotFound, `{"errors":[{"status":"404"}]}`)
	}
}

func newMockVMFromBackupAPI(t *testing.T) (*mockVMFromBackupAPI, *httptest.Server) {
	t.Helper()
	mock := &mockVMFromBackupAPI{billing: "hourly"}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	prev := vmRestoreReadyPollInterval
	vmRestoreReadyPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { vmRestoreReadyPollInterval = prev })
	return mock, server
}

func testAccVirtualMachineFromBackupConfig(extra string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_virtual_machine" "restored" {
  name      = "restored-vm"
  backup_id = "vmbkp_ready"
%s
}
`, extra)
}

const fromBackupRes = "latitudesh_virtual_machine.restored"

func TestAccVirtualMachine_FromBackup(t *testing.T) {
	mock, server := newMockVMFromBackupAPI(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccVirtualMachineFromBackupConfig(""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(fromBackupRes, "id", "vm_restored"),
					resource.TestCheckResourceAttr(fromBackupRes, "backup_id", "vmbkp_ready"),
					resource.TestCheckResourceAttr(fromBackupRes, "name", "restored-vm"),
					// Inherited from the backup, read back from the adopted VM.
					resource.TestCheckResourceAttr(fromBackupRes, "plan", "plan_small"),
					resource.TestCheckResourceAttr(fromBackupRes, "operating_system", "ubuntu_24_04_x64_lts"),
					resource.TestCheckResourceAttr(fromBackupRes, "project", "lab"),
					resource.TestCheckResourceAttr(fromBackupRes, "site", "ASH"),
					resource.TestCheckResourceAttr(fromBackupRes, "billing", "hourly"),
					resource.TestCheckResourceAttr(fromBackupRes, "status", "Running"),
					resource.TestCheckResourceAttr(fromBackupRes, "primary_ipv4", "10.10.0.5"),
					func(*terraform.State) error {
						if mock.restores != 1 || mock.patches != 0 {
							return fmt.Errorf("restores=%d patches=%d, want 1/0", mock.restores, mock.patches)
						}
						return nil
					},
				),
			},
		},
	})
	if !mock.deleted {
		t.Error("destroy should have deleted the adopted VM")
	}
}

func TestAccVirtualMachine_FromBackup_BillingReconciled(t *testing.T) {
	mock, server := newMockVMFromBackupAPI(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccVirtualMachineFromBackupConfig(`  billing = "monthly"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(fromBackupRes, "billing", "monthly"),
					func(*terraform.State) error {
						if mock.patches != 1 {
							return fmt.Errorf("patches=%d, want 1 (billing upgrade after restore)", mock.patches)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccVirtualMachine_FromBackup_BackupNotReady(t *testing.T) {
	_, server := newMockVMFromBackupAPI(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{{
			Config:      strings.Replace(testAccVirtualMachineFromBackupConfig(""), "vmbkp_ready", "vmbkp_creating", 1),
			ExpectError: regexp.MustCompile(`Backup Not Ready`),
		}},
	})
}

func TestAccVirtualMachine_FromBackup_BackupNotFound(t *testing.T) {
	_, server := newMockVMFromBackupAPI(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{{
			Config:      strings.Replace(testAccVirtualMachineFromBackupConfig(""), "vmbkp_ready", "vmbkp_missing", 1),
			ExpectError: regexp.MustCompile(`Backup Not Found`),
		}},
	})
}

func TestAccVirtualMachine_FromBackup_ProjectMismatch(t *testing.T) {
	_, server := newMockVMFromBackupAPI(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{{
			Config:      testAccVirtualMachineFromBackupConfig(`  project = "not-the-backups-project"`),
			ExpectError: regexp.MustCompile(`Project Mismatch`),
		}},
	})
}

// Schema-level rules: exactly one of plan/backup_id, and backup_id excludes the
// image-defining inputs. These fail at plan time, before any API call.
func TestAccVirtualMachine_PlanAndBackupIDValidation(t *testing.T) {
	_, server := newMockVMFromBackupAPI(t)
	oneOf := regexp.MustCompile(`(?i)one \(and only one\)`)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{ // both
				Config:      testAccVirtualMachineFromBackupConfig(`  plan = "vm-small"`),
				ExpectError: oneOf,
			},
			{ // neither
				Config: `
provider "latitudesh" { auth_token = "mock-token" }
resource "latitudesh_virtual_machine" "restored" { name = "x" }
`,
				ExpectError: oneOf,
			},
			{ // backup_id with an image-defining input
				Config:      testAccVirtualMachineFromBackupConfig(`  operating_system = "ubuntu_24_04_x64_lts"`),
				ExpectError: regexp.MustCompile(`(?i)Invalid Attribute Combination`),
			},
		},
	})
}

// --- live tier ---------------------------------------------------------------

// TestAccVirtualMachine_FromBackup_Live provisions the whole chain against the
// live API in one configuration: a source VM, a backup of it, and a second VM
// restored from that backup. Terraform orders the three by their references
// and the backup resource's own waiter guarantees the backup is Ready before
// the restore starts, so nothing here depends on a pre-existing fixture.
//
// Live-only, no VCR: the restore alone is polled for ~35 minutes, and a
// cassette of that many identical GETs would replay one real poll interval at
// a time. Budget: the e2e job runs with -timeout 180m (its default -run filter
// is "TestAcc", so this is picked up automatically) and make testacc with
// 120m. Expect ~45-50 minutes end to end, and two billable VMs plus a backup,
// all destroyed at the end.
func TestAccVirtualMachine_FromBackup_Live(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}
	// Plan discovery below makes a live call before resource.Test's PreCheck
	// runs; without this guard a missing token surfaces as a confusing 401
	// from the SDK's fallback token instead of the usual clear message.
	testAccTokenCheck(t)

	plan := testAccVMPlan(t) // skips in VCR replay mode

	const (
		source   = "latitudesh_virtual_machine.source"
		backup   = "latitudesh_virtual_machine_backup.source"
		restored = "latitudesh_virtual_machine.restored"
	)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy: resource.ComposeTestCheckFunc(
			testAccCheckVirtualMachineDestroy,
			testAccCheckVirtualMachineBackupDestroy,
		),
		Steps: []resource.TestStep{
			{
				Config: testAccVirtualMachineFromBackupLiveConfig(plan),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair(backup, "virtual_machine", source, "id"),
					resource.TestCheckResourceAttrPair(restored, "backup_id", backup, "id"),
					resource.TestCheckResourceAttrSet(restored, "id"),
					resource.TestCheckResourceAttr(restored, "status", vmStatusRunning),
					resource.TestCheckResourceAttrSet(restored, "primary_ipv4"),
					// Inherited from the backup and read back from the adopted VM.
					resource.TestCheckResourceAttrSet(restored, "plan"),
					resource.TestCheckResourceAttrSet(restored, "operating_system"),
					resource.TestCheckResourceAttrSet(restored, "project"),
					// The restore endpoint accepts a name; this is the first place
					// that verifies the API honors it on a restore.
					resource.TestCheckResourceAttr(restored, "name", testVMName+"-restored"),
				),
			},
			{
				ResourceName:      restored,
				ImportState:       true,
				ImportStateVerify: true,
				// backup_id is not returned by the read API, so it is null after
				// import (documented); ssh_keys are write-only. plan and project
				// already hold the API's own values on this path (id and slug),
				// so they round-trip and need no exclusion.
				ImportStateVerifyIgnore: []string{"backup_id", "ssh_keys"},
			},
		},
	})
}

func testAccVirtualMachineFromBackupLiveConfig(plan string) string {
	return fmt.Sprintf(`
resource "latitudesh_virtual_machine" "source" {
  name    = %q
  site    = %q
  plan    = %q
  project = "`+testAccProjectID()+`"
}

resource "latitudesh_virtual_machine_backup" "source" {
  virtual_machine = latitudesh_virtual_machine.source.id
}

resource "latitudesh_virtual_machine" "restored" {
  name      = %q
  backup_id = latitudesh_virtual_machine_backup.source.id
}
`, testVMName+"-source", testVMSite, plan, testVMName+"-restored")
}
