package latitudesh

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

// mdbScriptStep is one scripted answer to GET /managed_databases/mdb_1.
type mdbScriptStep struct {
	code       int    // HTTP status; 0 means 200
	status     string // database status, for a 200
	creds      bool   // include credentials, for a 200
	retryAfter string // Retry-After header
}

// mdbScript answers GETs from a fixed script; the last step repeats.
type mdbScript struct {
	mu    sync.Mutex
	steps []mdbScriptStep
	gets  int
}

func (s *mdbScript) handler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.gets
	if i >= len(s.steps) {
		i = len(s.steps) - 1
	}
	s.gets++
	step := s.steps[i]
	w.Header().Set("Content-Type", "application/vnd.api+json")
	if step.retryAfter != "" {
		w.Header().Set("Retry-After", step.retryAfter)
	}
	if step.code != 0 && step.code != http.StatusOK {
		w.WriteHeader(step.code)
		_, _ = w.Write([]byte(`{"errors":[{"status":"error"}]}`))
		return
	}
	attrs := map[string]any{"status": step.status}
	if step.creds {
		attrs["credentials"] = map[string]any{"host": "db.mock.internal"}
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": "mdb_1", "type": "managed_databases", "attributes": attrs}})
}

func (s *mdbScript) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gets
}

func newMdbScript(t *testing.T, steps ...mdbScriptStep) (*mdbScript, *ManagedDatabaseResource) {
	t.Helper()
	script := &mdbScript{steps: steps}
	server := httptest.NewServer(http.HandlerFunc(script.handler))
	t.Cleanup(server.Close)
	mdbFastPolling(t)
	r := &ManagedDatabaseResource{
		client: latitudeshgosdk.New(
			latitudeshgosdk.WithSecurity("test"),
			latitudeshgosdk.WithServerURL(server.URL),
		),
	}
	return script, r
}

func ready(script *mdbScript, r *ManagedDatabaseResource, deadline time.Duration, opts mdbWaitOpts) mdbWaitResult {
	return r.waitForManagedDatabaseReady(context.Background(), "mdb_1", time.Now().Add(deadline), opts)
}

func TestWaitForManagedDatabaseReady(t *testing.T) {
	cases := []struct {
		name      string
		steps     []mdbScriptStep
		opts      mdbWaitOpts
		deadline  time.Duration
		wantKind  mdbWaitKind
		minGets   int
		wantLast  string
		checkErrs bool
	}{
		{
			name:     "ready with credentials",
			steps:    []mdbScriptStep{{status: "provisioning"}, {status: "running", creds: true}},
			opts:     mdbWaitOpts{RequireCredentials: true},
			wantKind: mdbWaitReady, minGets: 2, wantLast: "running",
		},
		{
			name:     "unknown settled status without credentials keeps polling until timeout",
			steps:    []mdbScriptStep{{status: "running"}},
			opts:     mdbWaitOpts{RequireCredentials: true},
			deadline: 60 * time.Millisecond,
			wantKind: mdbWaitTimeout, minGets: 2, wantLast: "running",
		},
		{
			name:     "credentials seen on the POST: a settled Show without credentials is done",
			steps:    []mdbScriptStep{{status: "running"}},
			opts:     mdbWaitOpts{RequireCredentials: true, CredentialsSeen: true},
			wantKind: mdbWaitReady, minGets: 1,
		},
		{
			name:     "POST said running, but only a polled Show's status counts",
			steps:    []mdbScriptStep{{status: "provisioning", creds: true}, {status: "running", creds: true}},
			opts:     mdbWaitOpts{RequireCredentials: true, CredentialsSeen: true},
			wantKind: mdbWaitReady, minGets: 2,
		},
		{
			name:     "failed status fails fast",
			steps:    []mdbScriptStep{{status: "Failed"}},
			wantKind: mdbWaitFailedStatus, minGets: 1, wantLast: "Failed",
		},
		{
			name:     "failed is settled for the pre-PATCH wait",
			steps:    []mdbScriptStep{{status: "failed"}},
			opts:     mdbWaitOpts{FailedIsSettled: true},
			wantKind: mdbWaitReady, minGets: 1,
		},
		{
			name:     "post-PATCH wait with a prior failed status keeps polling",
			steps:    []mdbScriptStep{{status: "failed"}, {status: "failed"}, {status: "running"}},
			opts:     mdbWaitOpts{PriorStatus: "failed"},
			wantKind: mdbWaitReady, minGets: 3,
		},
		{
			name:     "deleting is unreachable",
			steps:    []mdbScriptStep{{status: "deleting"}},
			wantKind: mdbWaitGone, minGets: 1,
		},
		{
			name:     "empty status is not done",
			steps:    []mdbScriptStep{{status: ""}, {status: "running"}},
			wantKind: mdbWaitReady, minGets: 2,
		},
		{
			name:     "transient 404 then ready",
			steps:    []mdbScriptStep{{code: 404}, {status: "running"}},
			wantKind: mdbWaitReady, minGets: 2,
		},
		{
			name:     "5xx then ready",
			steps:    []mdbScriptStep{{code: 503}, {code: 500}, {status: "running"}},
			wantKind: mdbWaitReady, minGets: 3,
		},
		{
			name:     "403 fails fast",
			steps:    []mdbScriptStep{{code: 403}},
			wantKind: mdbWaitError, minGets: 1, checkErrs: true,
		},
		{
			name:     "persistent 5xx exhausts the error budget",
			steps:    []mdbScriptStep{{code: 502}},
			deadline: 5 * time.Second,
			wantKind: mdbWaitError, minGets: 2, checkErrs: true,
		},
		{
			name:     "429 doesn't count against the error budget",
			steps:    []mdbScriptStep{{code: 429}, {code: 429}, {code: 429}, {code: 429}, {code: 429}, {status: "running"}},
			wantKind: mdbWaitReady, minGets: 6,
		},
		{
			name:     "deadline exceeded gives a timeout",
			steps:    []mdbScriptStep{{status: "provisioning"}},
			deadline: 40 * time.Millisecond,
			wantKind: mdbWaitTimeout, minGets: 1, wantLast: "provisioning",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			script, r := newMdbScript(t, tc.steps...)
			if tc.name == "429 doesn't count against the error budget" {
				// The budget would run out after one error-free interval.
				mdbErrorBudget = time.Millisecond
			}
			deadline := tc.deadline
			if deadline == 0 {
				deadline = 5 * time.Second
			}
			got := ready(script, r, deadline, tc.opts)
			if got.Kind != tc.wantKind {
				t.Fatalf("kind = %v, want %v (err %v, last %q)", got.Kind, tc.wantKind, got.Err, got.LastStatus)
			}
			if script.count() < tc.minGets {
				t.Errorf("gets = %d, want at least %d", script.count(), tc.minGets)
			}
			if tc.wantLast != "" && got.LastStatus != tc.wantLast {
				t.Errorf("last status = %q, want %q", got.LastStatus, tc.wantLast)
			}
			if tc.checkErrs && got.Err == nil {
				t.Error("want the error that ended the wait")
			}
		})
	}
}

func TestWaitForManagedDatabaseReady_RetryAfter(t *testing.T) {
	script, r := newMdbScript(t, mdbScriptStep{code: 429, retryAfter: "1"}, mdbScriptStep{status: "running"})
	start := time.Now()
	got := ready(script, r, 5*time.Second, mdbWaitOpts{})
	if got.Kind != mdbWaitReady {
		t.Fatalf("kind = %v, want ready", got.Kind)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Errorf("waited %v, want Retry-After's 1s to be honoured", elapsed)
	}

	// Retry-After is bounded by the deadline.
	script, r = newMdbScript(t, mdbScriptStep{code: 429, retryAfter: "30"})
	start = time.Now()
	got = ready(script, r, 100*time.Millisecond, mdbWaitOpts{})
	if got.Kind != mdbWaitTimeout {
		t.Fatalf("kind = %v, want timeout", got.Kind)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("waited %v, past the deadline", elapsed)
	}
}

func TestWaitForManagedDatabaseDeleted(t *testing.T) {
	cases := []struct {
		name     string
		steps    []mdbScriptStep
		prior    string
		deadline time.Duration
		wantKind mdbWaitKind
		minGets  int
	}{
		{"404 is done", []mdbScriptStep{{code: 404}}, "running", 0, mdbWaitReady, 1},
		{"deleted status is done", []mdbScriptStep{{status: "deleted"}}, "running", 0, mdbWaitReady, 1},
		{"keeps polling while deleting", []mdbScriptStep{{status: "deleting"}, {status: "deleting"}, {code: 404}}, "running", 0, mdbWaitReady, 3},
		{"prior failed: keeps polling on failed, then 404", []mdbScriptStep{{status: "failed"}, {status: "failed"}, {code: 404}}, "failed", 0, mdbWaitReady, 3},
		{"prior running: fails fast on failed", []mdbScriptStep{{status: "failed"}}, "running", 0, mdbWaitFailedStatus, 1},
		{"timeout names the last status", []mdbScriptStep{{status: "deleting"}}, "running", 40 * time.Millisecond, mdbWaitTimeout, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			script, r := newMdbScript(t, tc.steps...)
			deadline := tc.deadline
			if deadline == 0 {
				deadline = 5 * time.Second
			}
			got := r.waitForManagedDatabaseDeleted(context.Background(), "mdb_1", time.Now().Add(deadline), tc.prior)
			if got.Kind != tc.wantKind {
				t.Fatalf("kind = %v, want %v (err %v)", got.Kind, tc.wantKind, got.Err)
			}
			if script.count() < tc.minGets {
				t.Errorf("gets = %d, want at least %d", script.count(), tc.minGets)
			}
			if tc.wantKind == mdbWaitTimeout && got.LastStatus != "deleting" {
				t.Errorf("last status = %q, want deleting", got.LastStatus)
			}
		})
	}
}

func TestMdbIDFromBody(t *testing.T) {
	if got := mdbIDFromBody(`{"data":{"id":"mdb_9","type":"unexpected_type","attributes":{}}}`); got != "mdb_9" {
		t.Errorf("id = %q, want mdb_9 even with an unexpected type", got)
	}
	if got := mdbIDFromBody(`not json`); got != "" {
		t.Errorf("id = %q, want empty", got)
	}
}
