package latitudesh

// These tests drive the asynchronous create/update/delete pollers
// (waitForControlPlane / waitForClusterReady / waitForClusterDeleted) directly
// against a local mock
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
	"encoding/json"
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
// is 200 the mock returns a cluster with the given status, and with the given
// reason and kubernetes_version when they are set (the control-plane cases
// script the reason, the upgrade cases the version, independently of the
// status); otherwise it returns that HTTP error code, shaped as the typed
// JSON:API ErrorObject that GetLksCluster declares for 403/404/502 (and the
// generic shape otherwise).
type lksGetStep struct {
	httpStatus int
	status     string
	reason     string
	version    string
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

	attrs := map[string]any{"status": step.status}
	if step.reason != "" {
		attrs["reason"] = step.reason
	}
	if step.version != "" {
		attrs["kubernetes_version"] = step.version
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"id": mockLksClusterID, "type": "lks_clusters", "attributes": attrs},
	})
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

// --- waitForControlPlane -----------------------------------------------------

// The operator's own sequence for a new cluster with no pool. It never reaches
// "ready" — it cannot, without nodes — so Create has to stop at the reason that
// says the control plane is done.
func TestWaitForControlPlane_ReturnsOnWaitingForNodePool(t *testing.T) {
	mock, r := newLksClusterMock(t,
		lksGetStep{httpStatus: 200, status: "provisioning", reason: "CreatingNetwork"},
		lksGetStep{httpStatus: 200, status: "provisioning", reason: "CreatingControlPlane"},
		lksGetStep{httpStatus: 200, status: "provisioning", reason: "WaitingForNodePool"},
	)

	var diags diag.Diagnostics
	r.waitForControlPlane(context.Background(), mockLksClusterID, time.Now().Add(5*time.Second), &diags)

	if diags.HasError() {
		t.Fatalf("expected no error, got: %v", diags.Errors())
	}
	if got := mock.gets(); got != 3 {
		t.Fatalf("expected 3 polls (network, control plane, waiting for a pool), got %d", got)
	}
}

// A pool that joined while the wait ran moves the cluster past
// WaitingForNodePool; the control plane is just as up.
func TestWaitForControlPlane_FinishingSetupCountsAsUp(t *testing.T) {
	mock, r := newLksClusterMock(t,
		lksGetStep{httpStatus: 200, status: "provisioning", reason: "CreatingControlPlane"},
		lksGetStep{httpStatus: 200, status: "provisioning", reason: "FinishingSetup"},
	)

	var diags diag.Diagnostics
	r.waitForControlPlane(context.Background(), mockLksClusterID, time.Now().Add(5*time.Second), &diags)

	if diags.HasError() {
		t.Fatalf("expected no error, got: %v", diags.Errors())
	}
	if got := mock.gets(); got != 2 {
		t.Fatalf("expected 2 polls, got %d", got)
	}
}

func TestWaitForControlPlane_ReadyCountsAsUp(t *testing.T) {
	mock, r := newLksClusterMock(t, lksGetStep{httpStatus: 200, status: "ready"})

	var diags diag.Diagnostics
	r.waitForControlPlane(context.Background(), mockLksClusterID, time.Now().Add(time.Second), &diags)

	if diags.HasError() {
		t.Fatalf("expected no error, got: %v", diags.Errors())
	}
	if got := mock.gets(); got != 1 {
		t.Fatalf("expected exactly 1 poll, got %d", got)
	}
}

// The POST answers before the record is necessarily readable; Create relies on
// this wait to absorb that window.
func TestWaitForControlPlane_TransientNotFoundRightAfterCreate(t *testing.T) {
	_, r := newLksClusterMock(t,
		lksGetStep{httpStatus: 404},
		lksGetStep{httpStatus: 200, status: "provisioning", reason: "WaitingForNodePool"},
	)

	var diags diag.Diagnostics
	r.waitForControlPlane(context.Background(), mockLksClusterID, time.Now().Add(5*time.Second), &diags)

	if diags.HasError() {
		t.Fatalf("a 404 right after create must be retried, got: %v", diags.Errors())
	}
}

// A control plane that never comes up times out, and the diagnostic carries the
// last reason: it is what tells "still building" apart from a reason this
// provider does not know.
func TestWaitForControlPlane_TimeoutNamesTheLastReason(t *testing.T) {
	_, r := newLksClusterMock(t, lksGetStep{httpStatus: 200, status: "provisioning", reason: "CreatingControlPlane"})

	var diags diag.Diagnostics
	r.waitForControlPlane(context.Background(), mockLksClusterID, time.Now().Add(30*time.Millisecond), &diags)

	if !diags.HasError() {
		t.Fatal("expected a timeout")
	}
	if summary := diags.Errors()[0].Summary(); summary != "Timeout waiting for LKS control plane" {
		t.Fatalf("unexpected summary: %q", summary)
	}
	if detail := diags.Errors()[0].Detail(); !strings.Contains(detail, "CreatingControlPlane") {
		t.Fatalf("timeout detail should name the last reason, got: %s", detail)
	}
}

func TestWaitForControlPlane_PausedFailsFast(t *testing.T) {
	mock, r := newLksClusterMock(t, lksGetStep{httpStatus: 200, status: "paused"})

	var diags diag.Diagnostics
	r.waitForControlPlane(context.Background(), mockLksClusterID, time.Now().Add(time.Minute), &diags)

	if !diags.HasError() {
		t.Fatal("expected a paused cluster to fail immediately")
	}
	if got := mock.gets(); got != 1 {
		t.Fatalf("expected exactly 1 poll before failing fast, got %d", got)
	}
}

// --- lksWaitForClusterToSettle (run by a node pool's Create) -------------------

// The first pool's nodes are up; the cluster is still installing its
// components on them. The pool's create waits that out.
func TestLksWaitForClusterToSettle_WaitsThroughFinishingSetup(t *testing.T) {
	mock, r := newLksClusterMock(t,
		lksGetStep{httpStatus: 200, status: "provisioning", reason: "WaitingForNodePool"},
		lksGetStep{httpStatus: 200, status: "provisioning", reason: "FinishingSetup"},
		lksGetStep{httpStatus: 200, status: "ready"},
	)

	var diags diag.Diagnostics
	lksWaitForClusterToSettle(context.Background(), r.client, mockLksClusterID, time.Now().Add(5*time.Second), &diags)

	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got: %v", diags)
	}
	if got := mock.gets(); got != 3 {
		t.Fatalf("expected 3 polls (waiting, finishing, ready), got %d", got)
	}
}

// A cluster that was ready and lost every node reports "degraded" with reason
// NoReadyNodes. That is settled as far as lksSettled is concerned, but a new
// pool is exactly what ends it, so the wait carries on to "ready" instead of
// stopping there with a warning.
func TestLksWaitForClusterToSettle_WaitsThroughNoReadyNodes(t *testing.T) {
	mock, r := newLksClusterMock(t,
		lksGetStep{httpStatus: 200, status: "degraded", reason: "NoReadyNodes"},
		lksGetStep{httpStatus: 200, status: "degraded", reason: "NoReadyNodes"},
		lksGetStep{httpStatus: 200, status: "ready"},
	)

	var diags diag.Diagnostics
	lksWaitForClusterToSettle(context.Background(), r.client, mockLksClusterID, time.Now().Add(5*time.Second), &diags)

	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got: %v", diags)
	}
	if got := mock.gets(); got != 3 {
		t.Fatalf("expected 3 polls (degraded twice, then ready), got %d", got)
	}
}

// An additional pool on a cluster that is already up costs one read.
func TestLksWaitForClusterToSettle_AlreadyReadyIsOneRead(t *testing.T) {
	mock, r := newLksClusterMock(t, lksGetStep{httpStatus: 200, status: "ready"})

	var diags diag.Diagnostics
	lksWaitForClusterToSettle(context.Background(), r.client, mockLksClusterID, time.Now().Add(time.Second), &diags)

	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got: %v", diags)
	}
	if got := mock.gets(); got != 1 {
		t.Fatalf("expected exactly 1 poll, got %d", got)
	}
}

// Every way the wait can fall short is a warning, never an error: an error
// from the pool's Create would taint a pool that is up.
func TestLksWaitForClusterToSettle_ShortfallsAreWarnings(t *testing.T) {
	cases := []struct {
		name        string
		step        lksGetStep
		wantSummary string
		wantDetail  string
	}{
		{"never ready", lksGetStep{httpStatus: 200, status: "provisioning", reason: "FinishingSetup"}, "LKS cluster not ready yet", "FinishingSetup"},
		// Degraded for a reason nodes do not explain (a core addon down): the
		// pool cannot fix it, so the wait stops and says so.
		{"settled elsewhere", lksGetStep{httpStatus: 200, status: "degraded", reason: "Degraded"}, "LKS cluster settled without becoming ready", "degraded"},
		{"lost nodes and never recovered", lksGetStep{httpStatus: 200, status: "degraded", reason: "NoReadyNodes"}, "LKS cluster not ready yet", "NoReadyNodes"},
		{"hopeless", lksGetStep{httpStatus: 200, status: "paused"}, "LKS cluster cannot become ready", "paused"},
		{"non-retryable error", lksGetStep{httpStatus: 403}, "Client Error", "403"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, r := newLksClusterMock(t, tc.step)

			var diags diag.Diagnostics
			lksWaitForClusterToSettle(context.Background(), r.client, mockLksClusterID, time.Now().Add(30*time.Millisecond), &diags)

			if diags.HasError() {
				t.Fatalf("must never error, got: %v", diags.Errors())
			}
			warnings := diags.Warnings()
			if len(warnings) != 1 {
				t.Fatalf("expected exactly 1 warning, got %d: %v", len(warnings), warnings)
			}
			if got := warnings[0].Summary(); got != tc.wantSummary {
				t.Fatalf("summary = %q, want %q", got, tc.wantSummary)
			}
			if detail := warnings[0].Detail(); !strings.Contains(detail, tc.wantDetail) || !strings.Contains(detail, "node pool itself is up") {
				t.Fatalf("detail should name %q and say the pool is up, got: %s", tc.wantDetail, detail)
			}
		})
	}
}

// --- waitForClusterReady -----------------------------------------------------

func TestWaitForClusterReady_ReadyImmediately(t *testing.T) {
	mock, r := newLksClusterMock(t, lksGetStep{httpStatus: 200, status: "ready"})

	var diags diag.Diagnostics
	r.waitForClusterReady(context.Background(), mockLksClusterID, "", time.Now().Add(time.Second), &diags)

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
	r.waitForClusterReady(context.Background(), mockLksClusterID, "", time.Now().Add(5*time.Second), &diags)

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
	r.waitForClusterReady(context.Background(), mockLksClusterID, "", time.Now().Add(5*time.Second), &diags)

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
	r.waitForClusterReady(context.Background(), mockLksClusterID, "", time.Now().Add(5*time.Second), &diags)

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
	r.waitForClusterReady(context.Background(), mockLksClusterID, "", time.Now().Add(5*time.Second), &diags)

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
	r.waitForClusterReady(context.Background(), mockLksClusterID, "", time.Now().Add(time.Minute), &diags)

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
	r.waitForClusterReady(context.Background(), mockLksClusterID, "", time.Now().Add(30*time.Millisecond), &diags)

	if !diags.HasError() {
		t.Fatal("expected a timeout error when the cluster never reaches ready")
	}
	if mock.gets() < 2 {
		t.Fatalf("expected more than 1 poll before timing out, got %d", mock.gets())
	}
}

// An accepted upgrade PATCH does not flip the status synchronously: the first
// poll still sees a "ready" cluster on the old version. Waiting on status
// alone returned there and left the pre-upgrade version in state, so the wait
// must keep polling until the requested version is the one reported.
func TestWaitForClusterReady_UpgradeLagsBehindStatus(t *testing.T) {
	mock, r := newLksClusterMock(t,
		lksGetStep{httpStatus: 200, status: "ready", version: "1.31.0"},
		lksGetStep{httpStatus: 200, status: "upgrading", version: "1.31.0"},
		lksGetStep{httpStatus: 200, status: "ready", version: "1.32.0"},
	)

	var diags diag.Diagnostics
	r.waitForClusterReady(context.Background(), mockLksClusterID, "1.32.0", time.Now().Add(5*time.Second), &diags)

	if diags.HasError() {
		t.Fatalf("expected the wait to succeed once the upgrade lands, got: %v", diags.Errors())
	}
	if got := mock.gets(); got != 3 {
		t.Fatalf("expected 3 polls (ready-on-old, upgrading, ready-on-new), got %d", got)
	}
}

// The same sequence without a wanted version is what create does, and there
// the first "ready" is the answer.
func TestWaitForClusterReady_NoWantedVersionReturnsOnFirstReady(t *testing.T) {
	mock, r := newLksClusterMock(t,
		lksGetStep{httpStatus: 200, status: "ready", version: "1.31.0"},
		lksGetStep{httpStatus: 200, status: "upgrading", version: "1.31.0"},
		lksGetStep{httpStatus: 200, status: "ready", version: "1.32.0"},
	)

	var diags diag.Diagnostics
	r.waitForClusterReady(context.Background(), mockLksClusterID, "", time.Now().Add(5*time.Second), &diags)

	if diags.HasError() {
		t.Fatalf("expected no error, got: %v", diags.Errors())
	}
	if got := mock.gets(); got != 1 {
		t.Fatalf("expected exactly 1 poll when no version is awaited, got %d", got)
	}
}

// A cluster that stays ready on the old version is an upgrade that never
// happened, and must time out rather than report success.
func TestWaitForClusterReady_UpgradeNeverLandsTimesOut(t *testing.T) {
	mock, r := newLksClusterMock(t, lksGetStep{httpStatus: 200, status: "ready", version: "1.31.0"})

	var diags diag.Diagnostics
	r.waitForClusterReady(context.Background(), mockLksClusterID, "1.32.0", time.Now().Add(30*time.Millisecond), &diags)

	if !diags.HasError() {
		t.Fatal("expected a timeout when the requested version never lands")
	}
	if summary := diags.Errors()[0].Summary(); summary != "Timeout waiting for LKS cluster upgrade" {
		t.Fatalf("expected the upgrade-specific timeout diagnostic, got %q", summary)
	}
	if detail := diags.Errors()[0].Detail(); !strings.Contains(detail, "1.32.0") || !strings.Contains(detail, "1.31.0") {
		t.Fatalf("expected the timeout detail to name both the wanted and the last version, got %q", detail)
	}
	if mock.gets() < 2 {
		t.Fatalf("expected more than 1 poll before timing out, got %d", mock.gets())
	}
}

// --- waitForClusterDeleted ---------------------------------------------------

func TestWaitForClusterDeleted_NotFound(t *testing.T) {
	mock, r := newLksClusterMock(t, lksGetStep{httpStatus: 404})

	var diags diag.Diagnostics
	r.waitForClusterDeleted(context.Background(), mockLksClusterID, time.Now().Add(time.Second), &diags)

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
	r.waitForClusterDeleted(context.Background(), mockLksClusterID, time.Now().Add(5*time.Second), &diags)

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
	r.waitForClusterDeleted(context.Background(), mockLksClusterID, time.Now().Add(30*time.Millisecond), &diags)

	if !diags.HasError() {
		t.Fatal("expected a timeout error when the cluster is never removed or marked deleted")
	}
	if mock.gets() < 2 {
		t.Fatalf("expected more than 1 poll before timing out, got %d", mock.gets())
	}
}

// The platform decides when it has finished, and the provider must not second-
// guess it with a hardcoded terminal string. lksSettled mirrors the API's
// LKSService::MutationAdmission, which is the same rule the backend uses to
// answer 422 "operation in progress".
func TestLksSettled(t *testing.T) {
	cases := []struct {
		status   string
		done     bool
		hopeless bool
	}{
		// Nothing reported yet: keep polling, the record may be young.
		{"", false, false},

		// IN_PROGRESS_STATES — the platform is still working.
		{"provisioning", false, false},
		{"updating", false, false},
		{"scaling", false, false},
		{"upgrading", false, false},

		// Waiting can never succeed from here, so fail fast instead of
		// burning the whole timeout.
		{"paused", false, true},
		{"deleting", false, true},
		{"deleted", false, true},

		// Settled. "ready" is the one in use today, but anything outside the
		// in-progress set counts — a new terminal state must not hang the
		// poller on a perfectly healthy cluster.
		{"ready", true, false},
		{"active", true, false},
		{"running", true, false},
	}

	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			done, hopeless := lksSettled(tc.status)
			if done != tc.done || hopeless != tc.hopeless {
				t.Errorf("lksSettled(%q) = (done=%v, hopeless=%v), want (%v, %v)",
					tc.status, done, hopeless, tc.done, tc.hopeless)
			}
		})
	}
}

// A paused cluster is not going to become ready, and a poller that keeps
// asking just delays the error by its whole timeout.
func TestWaitForClusterReady_PausedFailsFast(t *testing.T) {
	mock, r := newLksClusterMock(t, lksGetStep{httpStatus: 200, status: "paused"})

	var diags diag.Diagnostics
	r.waitForClusterReady(context.Background(), mockLksClusterID, "", time.Now().Add(time.Minute), &diags)

	if !diags.HasError() {
		t.Fatal("expected a paused cluster to fail immediately")
	}
	if got := mock.gets(); got != 1 {
		t.Fatalf("expected exactly 1 poll before failing fast, got %d", got)
	}
}

// A terminal state the provider has never heard of must end the wait, not hang
// it. This is the failure the literal "ready" comparison produced: a cluster
// the dashboard showed as active, with apply still counting minutes.
func TestWaitForClusterReady_UnknownTerminalStateEndsTheWait(t *testing.T) {
	mock, r := newLksClusterMock(t, lksGetStep{httpStatus: 200, status: "active"})

	var diags diag.Diagnostics
	r.waitForClusterReady(context.Background(), mockLksClusterID, "", time.Now().Add(time.Second), &diags)

	if diags.HasError() {
		t.Fatalf("an unrecognized settled state must end the wait, got: %v", diags.Errors())
	}
	if got := mock.gets(); got != 1 {
		t.Fatalf("expected exactly 1 poll, got %d", got)
	}
}

// --- one deadline per operation ----------------------------------------------

// Every wait inside a single Create or Update takes the SAME absolute deadline,
// so the configured timeout bounds the operation rather than each wait. A
// deadline one wait has spent is spent for every wait after it — no further
// polls, straight to the timeout diagnostic.
func TestWaits_ShareOneDeadlinePerOperation(t *testing.T) {
	mock, r := newLksClusterMock(t, lksGetStep{httpStatus: 200, status: "provisioning", reason: "CreatingControlPlane"})

	deadline := time.Now().Add(50 * time.Millisecond)

	// This one polls "CreatingControlPlane" until the deadline passes.
	var diags diag.Diagnostics
	r.waitForControlPlane(context.Background(), mockLksClusterID, deadline, &diags)
	if !diags.HasError() {
		t.Fatal("expected the control-plane wait to time out: the mock never gets past CreatingControlPlane")
	}
	if summary := diags.Errors()[0].Summary(); summary != "Timeout waiting for LKS control plane" {
		t.Fatalf("expected the timeout diagnostic, got %q", summary)
	}

	// The deadline is gone. A later wait in the same operation must say so
	// immediately, without a single further GET.
	polls := mock.gets()
	var later diag.Diagnostics
	r.waitForClusterReady(context.Background(), mockLksClusterID, "", deadline, &later)
	if !later.HasError() {
		t.Fatal("a spent deadline must fail the next wait too")
	}
	if got := mock.gets(); got != polls {
		t.Fatalf("a wait past its deadline must not poll; GET count went %d -> %d", polls, got)
	}
}
