package latitudesh

// These tests drive the asynchronous create/delete pollers
// (waitForBackupReady / waitForBackupDeleted) directly against a local mock of
// the Latitude.sh API, so no live credentials or VCR fixtures are needed. The
// mock serves a scripted sequence of GET /virtual_machine_backups/{id}
// responses (the last entry repeats, so a non-terminal tail runs the poller to
// its timeout), which lets each case exercise a specific branch: the Ready and
// Failed terminal states, transient 404/5xx retries, a fatal client error that
// must not burn the retry budget, the consecutive-error ceiling, and the
// timeout diagnostic. The plain acceptance test can only cover this lifecycle
// live, against endpoints this PR notes are still unverified.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

const mockBackupID = "vmb_lifecycle1"

// vmBackupGetStep is one scripted answer to a GET of the backup. When httpStatus
// is 200 the mock returns a backup with the given status (and failureReason,
// when set); otherwise it returns that HTTP error code.
type vmBackupGetStep struct {
	httpStatus    int
	status        string
	failureReason string
}

// vmBackupMock answers GET /virtual_machine_backups/{id} from a scripted
// sequence, one step per GET, repeating the last step so a non-terminal tail
// makes the poller run until it times out.
type vmBackupMock struct {
	mu       sync.Mutex
	steps    []vmBackupGetStep
	getCount int
}

func (m *vmBackupMock) next() vmBackupGetStep {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.getCount
	m.getCount++
	if idx >= len(m.steps) {
		idx = len(m.steps) - 1
	}
	return m.steps[idx]
}

func (m *vmBackupMock) gets() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getCount
}

func (m *vmBackupMock) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.api+json")

	if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/virtual_machine_backups/") {
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
	if step.failureReason != "" {
		_, _ = fmt.Fprintf(w, `{"data":{"id":%q,"type":"virtual_machine_backups","attributes":{"status":%q,"failure_reason":%q}}}`,
			mockBackupID, step.status, step.failureReason)
		return
	}
	_, _ = fmt.Fprintf(w, `{"data":{"id":%q,"type":"virtual_machine_backups","attributes":{"status":%q}}}`,
		mockBackupID, step.status)
}

// newVMBackupMock starts the mock, wires a resource whose client points at it,
// and shortens both poll intervals so a scripted sequence costs milliseconds
// instead of one 10s/5s sleep per transition.
func newVMBackupMock(t *testing.T, steps ...vmBackupGetStep) (*vmBackupMock, *VirtualMachineBackupResource) {
	t.Helper()

	if len(steps) == 0 {
		t.Fatal("newVMBackupMock requires at least one step")
	}

	mock := &vmBackupMock{steps: steps}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	prevReady, prevDelete := vmBackupReadyPollInterval, vmBackupDeletePollInterval
	vmBackupReadyPollInterval = 5 * time.Millisecond
	vmBackupDeletePollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		vmBackupReadyPollInterval = prevReady
		vmBackupDeletePollInterval = prevDelete
	})

	r := &VirtualMachineBackupResource{
		client: latitudeshgosdk.New(
			latitudeshgosdk.WithSecurity("test"),
			latitudeshgosdk.WithServerURL(server.URL),
		),
	}
	return mock, r
}

// --- waitForBackupReady -----------------------------------------------------

func TestWaitForBackupReady_ReadyImmediately(t *testing.T) {
	mock, r := newVMBackupMock(t, vmBackupGetStep{httpStatus: 200, status: "Ready"})

	var diags diag.Diagnostics
	r.waitForBackupReady(context.Background(), mockBackupID, time.Second, &diags)

	if diags.HasError() {
		t.Fatalf("expected no error when the backup is already Ready, got: %v", diags.Errors())
	}
	if got := mock.gets(); got != 1 {
		t.Fatalf("expected exactly 1 poll for an already-Ready backup, got %d", got)
	}
}

func TestWaitForBackupReady_CreatingThenReady(t *testing.T) {
	mock, r := newVMBackupMock(t,
		vmBackupGetStep{httpStatus: 200, status: "Creating"},
		vmBackupGetStep{httpStatus: 200, status: "Creating"},
		vmBackupGetStep{httpStatus: 200, status: "Ready"},
	)

	var diags diag.Diagnostics
	r.waitForBackupReady(context.Background(), mockBackupID, 5*time.Second, &diags)

	if diags.HasError() {
		t.Fatalf("expected success once the backup reaches Ready, got: %v", diags.Errors())
	}
	if got := mock.gets(); got < 3 {
		t.Fatalf("expected the poller to wait through Creating (at least 3 polls), got %d", got)
	}
}

func TestWaitForBackupReady_FailedSurfacesReason(t *testing.T) {
	_, r := newVMBackupMock(t, vmBackupGetStep{
		httpStatus:    200,
		status:        "Failed",
		failureReason: "insufficient disk space on host",
	})

	var diags diag.Diagnostics
	r.waitForBackupReady(context.Background(), mockBackupID, time.Second, &diags)

	if !diags.HasError() {
		t.Fatal("expected an error when the backup enters Failed")
	}
	if !hasDiagContaining(diags, "insufficient disk space on host") {
		t.Fatalf("expected the failure_reason to surface in the diagnostic, got: %v", diags.Errors())
	}
}

func TestWaitForBackupReady_FailedUnknownReason(t *testing.T) {
	_, r := newVMBackupMock(t, vmBackupGetStep{httpStatus: 200, status: "Failed"})

	var diags diag.Diagnostics
	r.waitForBackupReady(context.Background(), mockBackupID, time.Second, &diags)

	if !diags.HasError() {
		t.Fatal("expected an error when the backup enters Failed")
	}
	if !hasDiagContaining(diags, "unknown reason") {
		t.Fatalf("expected an 'unknown reason' fallback when no failure_reason is present, got: %v", diags.Errors())
	}
}

func TestWaitForBackupReady_TransientNotFoundThenReady(t *testing.T) {
	// A 404 right after create (the backup is not queryable yet) is transient:
	// the poller must keep going, not treat it as gone.
	mock, r := newVMBackupMock(t,
		vmBackupGetStep{httpStatus: 404},
		vmBackupGetStep{httpStatus: 404},
		vmBackupGetStep{httpStatus: 200, status: "Ready"},
	)

	var diags diag.Diagnostics
	r.waitForBackupReady(context.Background(), mockBackupID, 5*time.Second, &diags)

	if diags.HasError() {
		t.Fatalf("expected a transient 404 to be retried, got: %v", diags.Errors())
	}
	if got := mock.gets(); got < 3 {
		t.Fatalf("expected the poller to retry past the 404s, got %d polls", got)
	}
}

func TestWaitForBackupReady_TransientServerErrorThenReady(t *testing.T) {
	mock, r := newVMBackupMock(t,
		vmBackupGetStep{httpStatus: 500},
		vmBackupGetStep{httpStatus: 200, status: "Ready"},
	)

	var diags diag.Diagnostics
	r.waitForBackupReady(context.Background(), mockBackupID, 5*time.Second, &diags)

	if diags.HasError() {
		t.Fatalf("expected a transient 5xx to be retried, got: %v", diags.Errors())
	}
	if got := mock.gets(); got < 2 {
		t.Fatalf("expected the poller to retry past the 5xx, got %d polls", got)
	}
}

func TestWaitForBackupReady_FatalClientErrorStopsImmediately(t *testing.T) {
	// A 401 will not resolve by waiting, so the poller must fail on the first
	// reading instead of burning the whole timeout budget.
	mock, r := newVMBackupMock(t, vmBackupGetStep{httpStatus: 401})

	var diags diag.Diagnostics
	r.waitForBackupReady(context.Background(), mockBackupID, time.Hour, &diags)

	if !diags.HasError() {
		t.Fatal("expected a fatal client error to stop the poll")
	}
	if got := mock.gets(); got != 1 {
		t.Fatalf("expected a fatal 4xx to stop after 1 poll, got %d", got)
	}
}

func TestWaitForBackupReady_ConsecutiveErrorCeiling(t *testing.T) {
	// A persistent 5xx must give up after the consecutive-error ceiling rather
	// than poll for the full timeout.
	mock, r := newVMBackupMock(t, vmBackupGetStep{httpStatus: 500})

	var diags diag.Diagnostics
	r.waitForBackupReady(context.Background(), mockBackupID, time.Hour, &diags)

	if !diags.HasError() {
		t.Fatal("expected the poll to give up after repeated 5xx errors")
	}
	if got := mock.gets(); got != 5 {
		t.Fatalf("expected the poller to stop at the 5-error ceiling, got %d polls", got)
	}
	if !hasDiagContaining(diags, "consecutive") {
		t.Fatalf("expected a consecutive-error diagnostic, got: %v", diags.Errors())
	}
}

func TestWaitForBackupReady_Timeout(t *testing.T) {
	// A backup stuck in Creating must surface the timeout diagnostic.
	_, r := newVMBackupMock(t, vmBackupGetStep{httpStatus: 200, status: "Creating"})

	var diags diag.Diagnostics
	r.waitForBackupReady(context.Background(), mockBackupID, 60*time.Millisecond, &diags)

	if !diags.HasError() {
		t.Fatal("expected a timeout error for a backup stuck in Creating")
	}
	if !hasDiagContaining(diags, "did not reach a terminal status") {
		t.Fatalf("expected a timeout diagnostic, got: %v", diags.Errors())
	}
}

// --- waitForBackupDeleted ---------------------------------------------------

func TestWaitForBackupDeleted_GoneImmediately(t *testing.T) {
	mock, r := newVMBackupMock(t, vmBackupGetStep{httpStatus: 404})

	var diags diag.Diagnostics
	r.waitForBackupDeleted(context.Background(), mockBackupID, time.Second, &diags)

	if diags.HasError() {
		t.Fatalf("expected a 404 to be read as deletion complete, got: %v", diags.Errors())
	}
	if got := mock.gets(); got != 1 {
		t.Fatalf("expected exactly 1 poll for an already-gone backup, got %d", got)
	}
}

func TestWaitForBackupDeleted_PresentThenGone(t *testing.T) {
	mock, r := newVMBackupMock(t,
		vmBackupGetStep{httpStatus: 200, status: "Ready"},
		vmBackupGetStep{httpStatus: 200, status: "Ready"},
		vmBackupGetStep{httpStatus: 404},
	)

	var diags diag.Diagnostics
	r.waitForBackupDeleted(context.Background(), mockBackupID, 5*time.Second, &diags)

	if diags.HasError() {
		t.Fatalf("expected success once the backup disappears, got: %v", diags.Errors())
	}
	if got := mock.gets(); got < 3 {
		t.Fatalf("expected the poller to wait for the backup to disappear, got %d polls", got)
	}
}

func TestWaitForBackupDeleted_TransientServerErrorThenGone(t *testing.T) {
	mock, r := newVMBackupMock(t,
		vmBackupGetStep{httpStatus: 500},
		vmBackupGetStep{httpStatus: 404},
	)

	var diags diag.Diagnostics
	r.waitForBackupDeleted(context.Background(), mockBackupID, 5*time.Second, &diags)

	if diags.HasError() {
		t.Fatalf("expected a transient 5xx during deletion to be retried, got: %v", diags.Errors())
	}
	if got := mock.gets(); got < 2 {
		t.Fatalf("expected the poller to retry past the 5xx, got %d polls", got)
	}
}

func TestWaitForBackupDeleted_FatalClientErrorStopsImmediately(t *testing.T) {
	mock, r := newVMBackupMock(t, vmBackupGetStep{httpStatus: 401})

	var diags diag.Diagnostics
	r.waitForBackupDeleted(context.Background(), mockBackupID, time.Hour, &diags)

	if !diags.HasError() {
		t.Fatal("expected a fatal client error to stop the deletion poll")
	}
	if got := mock.gets(); got != 1 {
		t.Fatalf("expected a fatal 4xx to stop after 1 poll, got %d", got)
	}
}

func TestWaitForBackupDeleted_ConsecutiveErrorCeiling(t *testing.T) {
	mock, r := newVMBackupMock(t, vmBackupGetStep{httpStatus: 500})

	var diags diag.Diagnostics
	r.waitForBackupDeleted(context.Background(), mockBackupID, time.Hour, &diags)

	if !diags.HasError() {
		t.Fatal("expected the deletion poll to give up after repeated 5xx errors")
	}
	if got := mock.gets(); got != 5 {
		t.Fatalf("expected the poller to stop at the 5-error ceiling, got %d polls", got)
	}
}

func TestWaitForBackupDeleted_Timeout(t *testing.T) {
	// A backup that never disappears must surface the deletion timeout.
	_, r := newVMBackupMock(t, vmBackupGetStep{httpStatus: 200, status: "Ready"})

	var diags diag.Diagnostics
	r.waitForBackupDeleted(context.Background(), mockBackupID, 60*time.Millisecond, &diags)

	if !diags.HasError() {
		t.Fatal("expected a timeout error for a backup that never disappears")
	}
	if !hasDiagContaining(diags, "still present") {
		t.Fatalf("expected a deletion-timeout diagnostic, got: %v", diags.Errors())
	}
}

func hasDiagContaining(diags diag.Diagnostics, substr string) bool {
	for _, d := range diags.Errors() {
		if strings.Contains(d.Summary(), substr) || strings.Contains(d.Detail(), substr) {
			return true
		}
	}
	return false
}
