package latitudesh

// These tests drive the asynchronous create/delete pollers
// (waitForClusterReady / waitForClusterDeleted) directly against a local mock
// of the LKS API, so no live credentials are needed. The mock serves a
// scripted sequence of GET /lks/clusters/{id} responses (the last entry
// repeats, so a non-terminal tail runs the poller to its timeout), which lets
// each case exercise a specific branch: reaching "ready" immediately or after
// a few non-terminal statuses, transient 404/5xx retries, a fatal client
// error that must not burn the retry budget, the consecutive-error ceiling,
// the timeout diagnostic, and both terminal shapes for delete (404 and
// status "deleted"). Only the live TestAcc can confirm which of those two
// delete shapes the real API actually uses.

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

const mockLksClusterID = "lks_lifecycle1"

// lksGetStep is one scripted answer to a GET of the cluster. When httpStatus
// is 200 the mock returns a cluster with the given status; otherwise it
// returns that HTTP error code, shaped as the typed JSON:API ErrorObject that
// GetLksCluster declares for 403/404/502 (and the generic shape otherwise).
type lksGetStep struct {
	httpStatus int
	status     string
}

// lksClusterMock answers GET /lks/clusters/{id} from a scripted sequence, one
// step per GET, repeating the last step so a non-terminal tail makes the
// poller run until it times out.
type lksClusterMock struct {
	mu       sync.Mutex
	steps    []lksGetStep
	getCount int
}

func (m *lksClusterMock) next() lksGetStep {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.getCount
	m.getCount++
	if idx >= len(m.steps) {
		idx = len(m.steps) - 1
	}
	return m.steps[idx]
}

func (m *lksClusterMock) gets() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getCount
}

func (m *lksClusterMock) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.api+json")

	if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/lks/clusters/") {
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
	_, _ = fmt.Fprintf(w, `{"data":{"id":%q,"type":"lks_clusters","attributes":{"status":%q}}}`,
		mockLksClusterID, step.status)
}

// newLksClusterMock starts the mock, wires a resource whose client points at
// it, and shortens both poll intervals so a scripted sequence costs
// milliseconds instead of one 10s/5s sleep per transition.
func newLksClusterMock(t *testing.T, steps ...lksGetStep) (*lksClusterMock, *LksResource) {
	t.Helper()

	if len(steps) == 0 {
		t.Fatal("newLksClusterMock requires at least one step")
	}

	mock := &lksClusterMock{steps: steps}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	prevReady, prevDelete := lksReadyPollInterval, lksDeletePollInterval
	lksReadyPollInterval = 5 * time.Millisecond
	lksDeletePollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		lksReadyPollInterval = prevReady
		lksDeletePollInterval = prevDelete
	})

	r := &LksResource{
		client: latitudeshgosdk.New(
			latitudeshgosdk.WithSecurity("test"),
			latitudeshgosdk.WithServerURL(server.URL),
		),
	}
	return mock, r
}

// --- waitForClusterReady -----------------------------------------------------

func TestWaitForClusterReady_ReadyImmediately(t *testing.T) {
	mock, r := newLksClusterMock(t, lksGetStep{httpStatus: 200, status: "ready"})

	var diags diag.Diagnostics
	r.waitForClusterReady(context.Background(), mockLksClusterID, time.Second, &diags)

	if diags.HasError() {
		t.Fatalf("expected no error when the cluster is already ready, got: %v", diags.Errors())
	}
	if got := mock.gets(); got != 1 {
		t.Fatalf("expected exactly 1 poll for an already-ready cluster, got %d", got)
	}
}

func TestWaitForClusterReady_ProvisioningThenReady(t *testing.T) {
	mock, r := newLksClusterMock(t,
		lksGetStep{httpStatus: 200, status: "provisioning"},
		lksGetStep{httpStatus: 200, status: "provisioning"},
		lksGetStep{httpStatus: 200, status: "ready"},
	)

	var diags diag.Diagnostics
	r.waitForClusterReady(context.Background(), mockLksClusterID, 5*time.Second, &diags)

	if diags.HasError() {
		t.Fatalf("expected success once the cluster reaches ready, got: %v", diags.Errors())
	}
	if got := mock.gets(); got != 3 {
		t.Fatalf("expected 3 polls, got %d", got)
	}
}

func TestWaitForClusterReady_TransientNotFoundThenReady(t *testing.T) {
	mock, r := newLksClusterMock(t,
		lksGetStep{httpStatus: 404},
		lksGetStep{httpStatus: 200, status: "ready"},
	)

	var diags diag.Diagnostics
	r.waitForClusterReady(context.Background(), mockLksClusterID, 5*time.Second, &diags)

	if diags.HasError() {
		t.Fatalf("expected a 404 right after create to be treated as transient, got: %v", diags.Errors())
	}
	if got := mock.gets(); got != 2 {
		t.Fatalf("expected 2 polls, got %d", got)
	}
}

func TestWaitForClusterReady_TransientServerErrorThenReady(t *testing.T) {
	mock, r := newLksClusterMock(t,
		lksGetStep{httpStatus: 502},
		lksGetStep{httpStatus: 200, status: "ready"},
	)

	var diags diag.Diagnostics
	r.waitForClusterReady(context.Background(), mockLksClusterID, 5*time.Second, &diags)

	if diags.HasError() {
		t.Fatalf("expected a 502 to be treated as transient, got: %v", diags.Errors())
	}
	if got := mock.gets(); got != 2 {
		t.Fatalf("expected 2 polls, got %d", got)
	}
}

func TestWaitForClusterReady_FatalErrorStopsImmediately(t *testing.T) {
	mock, r := newLksClusterMock(t,
		lksGetStep{httpStatus: 422},
		lksGetStep{httpStatus: 200, status: "ready"},
	)

	var diags diag.Diagnostics
	r.waitForClusterReady(context.Background(), mockLksClusterID, 5*time.Second, &diags)

	if !diags.HasError() {
		t.Fatal("expected a 422 to fail immediately instead of retrying")
	}
	if got := mock.gets(); got != 1 {
		t.Fatalf("expected exactly 1 poll before failing fast, got %d", got)
	}
}

func TestWaitForClusterReady_ConsecutiveErrorCeiling(t *testing.T) {
	mock, r := newLksClusterMock(t,
		lksGetStep{httpStatus: 502},
		lksGetStep{httpStatus: 502},
		lksGetStep{httpStatus: 502},
		lksGetStep{httpStatus: 502},
		lksGetStep{httpStatus: 502},
	)

	var diags diag.Diagnostics
	r.waitForClusterReady(context.Background(), mockLksClusterID, time.Minute, &diags)

	if !diags.HasError() {
		t.Fatal("expected the consecutive-error ceiling to fail the wait")
	}
	if got := mock.gets(); got != 5 {
		t.Fatalf("expected exactly 5 polls (the ceiling), got %d", got)
	}
}

func TestWaitForClusterReady_Timeout(t *testing.T) {
	mock, r := newLksClusterMock(t, lksGetStep{httpStatus: 200, status: "provisioning"})

	var diags diag.Diagnostics
	r.waitForClusterReady(context.Background(), mockLksClusterID, 30*time.Millisecond, &diags)

	if !diags.HasError() {
		t.Fatal("expected a timeout error when the cluster never reaches ready")
	}
	if mock.gets() < 2 {
		t.Fatalf("expected more than 1 poll before timing out, got %d", mock.gets())
	}
}

// --- waitForClusterDeleted ---------------------------------------------------

func TestWaitForClusterDeleted_NotFound(t *testing.T) {
	mock, r := newLksClusterMock(t, lksGetStep{httpStatus: 404})

	var diags diag.Diagnostics
	r.waitForClusterDeleted(context.Background(), mockLksClusterID, time.Second, &diags)

	if diags.HasError() {
		t.Fatalf("expected no error once the cluster 404s, got: %v", diags.Errors())
	}
	if got := mock.gets(); got != 1 {
		t.Fatalf("expected exactly 1 poll, got %d", got)
	}
}

func TestWaitForClusterDeleted_StatusDeleted(t *testing.T) {
	mock, r := newLksClusterMock(t,
		lksGetStep{httpStatus: 200, status: "deleting"},
		lksGetStep{httpStatus: 200, status: "deleted"},
	)

	var diags diag.Diagnostics
	r.waitForClusterDeleted(context.Background(), mockLksClusterID, 5*time.Second, &diags)

	if diags.HasError() {
		t.Fatalf("expected status \"deleted\" to be treated as terminal, got: %v", diags.Errors())
	}
	if got := mock.gets(); got != 2 {
		t.Fatalf("expected 2 polls, got %d", got)
	}
}

func TestWaitForClusterDeleted_Timeout(t *testing.T) {
	mock, r := newLksClusterMock(t, lksGetStep{httpStatus: 200, status: "deleting"})

	var diags diag.Diagnostics
	r.waitForClusterDeleted(context.Background(), mockLksClusterID, 30*time.Millisecond, &diags)

	if !diags.HasError() {
		t.Fatal("expected a timeout error when the cluster is never removed or marked deleted")
	}
	if mock.gets() < 2 {
		t.Fatalf("expected more than 1 poll before timing out, got %d", mock.gets())
	}
}
