package latitudesh

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// mdbScriptStep is one scripted answer to GET /managed_databases/mdb_1.
type mdbScriptStep struct {
	code       int    // HTTP status; 0 means 200
	status     string // database status, for a 200
	creds      bool   // include credentials, for a 200
	retryAfter string // Retry-After header
	drop       bool   // close the connection without a response
	noData     bool   // answer 200 with {"data": null}
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
	if step.drop {
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
		return
	}
	w.Header().Set("Content-Type", "application/vnd.api+json")
	if step.noData {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":null}`))
		return
	}
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
	return script, newMdbTestResource(t, script.handler)
}

// newMdbTestResource starts a test server with h, shortens the poll intervals,
// and returns a resource whose client points at the server.
func newMdbTestResource(t *testing.T, h http.HandlerFunc) *ManagedDatabaseResource {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	mdbFastPolling(t)
	return &ManagedDatabaseResource{
		client: latitudeshgosdk.New(
			latitudeshgosdk.WithSecurity("test"),
			latitudeshgosdk.WithServerURL(server.URL),
		),
	}
}

func ready(r *ManagedDatabaseResource, deadline time.Duration, opts mdbWaitOpts) mdbWaitResult {
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
		budget    time.Duration // overrides mdbErrorBudget when non-zero
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
			// The budget would run out after one error-free interval.
			budget: time.Millisecond,
		},
		{
			name: "429s between 5xx errors don't use up the error budget",
			steps: append(append([]mdbScriptStep{{code: 502}}, mdbRepeat(mdbScriptStep{code: 429}, 60)...),
				mdbScriptStep{code: 502}, mdbScriptStep{status: "running"}),
			wantKind: mdbWaitReady, minGets: 63,
		},
		{
			name:     "a dropped connection is retried within the budget",
			steps:    []mdbScriptStep{{drop: true}, {status: "running"}},
			wantKind: mdbWaitReady, minGets: 2,
		},
		{
			name:     "a 404 is gone when the database must already exist",
			steps:    []mdbScriptStep{{code: 404}},
			opts:     mdbWaitOpts{NotFoundIsGone: true},
			wantKind: mdbWaitGone, minGets: 1,
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
			if tc.budget != 0 {
				mdbErrorBudget = tc.budget
			}
			deadline := tc.deadline
			if deadline == 0 {
				deadline = 5 * time.Second
			}
			got := ready(r, deadline, tc.opts)
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
	_, r := newMdbScript(t, mdbScriptStep{code: 429, retryAfter: "1"}, mdbScriptStep{status: "running"})
	start := time.Now()
	got := ready(r, 5*time.Second, mdbWaitOpts{})
	if got.Kind != mdbWaitReady {
		t.Fatalf("kind = %v, want ready", got.Kind)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Errorf("waited %v, want Retry-After's 1s to be honoured", elapsed)
	}

	// Retry-After is bounded by the deadline.
	_, r = newMdbScript(t, mdbScriptStep{code: 429, retryAfter: "30"})
	start = time.Now()
	got = ready(r, 100*time.Millisecond, mdbWaitOpts{})
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

func mdbRepeat(step mdbScriptStep, n int) []mdbScriptStep {
	out := make([]mdbScriptStep, n)
	for i := range out {
		out[i] = step
	}
	return out
}

// A 200 without data is an error, not "gone": treating it as gone would drop
// the database from state and let the next apply create a second one.
func TestReadManagedDatabaseInto_NoDataIsAnError(t *testing.T) {
	_, r := newMdbScript(t, mdbScriptStep{noData: true})
	data := ManagedDatabaseResourceModel{ID: types.StringValue("mdb_1")}
	var diags diag.Diagnostics
	gone := r.readManagedDatabaseInto(context.Background(), &data, nil, nil, &diags)
	if gone {
		t.Error("gone = true, want false")
	}
	if !diags.HasError() {
		t.Error("want an error diagnostic")
	}
}

// mdbSequence answers every request from a fixed list of (status, body)
// responses; the last one repeats.
type mdbSequence struct {
	mu        sync.Mutex
	responses []mdbResponse
	calls     int
}

type mdbResponse struct {
	code       int
	body       string
	retryAfter string
	drop       bool // close the connection without a response
}

func newMdbSequence(t *testing.T, responses ...mdbResponse) (*mdbSequence, *ManagedDatabaseResource) {
	t.Helper()
	seq := &mdbSequence{responses: responses}
	r := newMdbTestResource(t, func(w http.ResponseWriter, r *http.Request) {
		seq.mu.Lock()
		defer seq.mu.Unlock()
		i := seq.calls
		if i >= len(seq.responses) {
			i = len(seq.responses) - 1
		}
		seq.calls++
		resp := seq.responses[i]
		if resp.drop {
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					_ = conn.Close()
				}
			}
			return
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		if resp.retryAfter != "" {
			w.Header().Set("Retry-After", resp.retryAfter)
		}
		w.WriteHeader(resp.code)
		_, _ = w.Write([]byte(resp.body))
	})
	return seq, r
}

func (s *mdbSequence) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestCreateManagedDatabase_UnexpectedSuccess(t *testing.T) {
	payload := components.ManagedDatabasePayload{Data: &components.ManagedDatabasePayloadData{Attributes: &components.ManagedDatabasePayloadAttributes{Name: "api"}}}

	t.Run("a 200 with an ID recovers it with a warning", func(t *testing.T) {
		_, r := newMdbSequence(t, mdbResponse{code: 200, body: `{"data":{"id":"mdb_7","type":"managed_databases","attributes":{"credentials":{"password":"s3cr3t"}}}}`})
		var diags diag.Diagnostics
		id, _, ok := r.createManagedDatabase(context.Background(), payload, "api", "proj_1", &diags)
		if !ok || id != "mdb_7" {
			t.Fatalf("id = %q, ok = %v, diags %v", id, ok, diags)
		}
		if diags.WarningsCount() != 1 {
			t.Errorf("warnings = %d, want 1", diags.WarningsCount())
		}
		for _, d := range diags {
			if strings.Contains(d.Detail(), "s3cr3t") {
				t.Errorf("diagnostic leaks the body: %q", d.Detail())
			}
		}
	})

	t.Run("a 200 without an ID may have created a database", func(t *testing.T) {
		_, r := newMdbSequence(t, mdbResponse{code: 200, body: `{"data":{"type":"managed_databases"}}`})
		var diags diag.Diagnostics
		_, _, ok := r.createManagedDatabase(context.Background(), payload, "api", "proj_1", &diags)
		if ok || !diags.HasError() || diags.Errors()[0].Summary() != "Managed database may have been created" {
			t.Fatalf("ok = %v, diags %v", ok, diags)
		}
	})

	t.Run("a 5xx may have created a database, and is sent once", func(t *testing.T) {
		seq, r := newMdbSequence(t, mdbResponse{code: 502, body: `{}`})
		var diags diag.Diagnostics
		_, _, ok := r.createManagedDatabase(context.Background(), payload, "api", "proj_1", &diags)
		if ok || diags.Errors()[0].Summary() != "Managed database may have been created" {
			t.Fatalf("ok = %v, diags %v", ok, diags)
		}
		if seq.count() != 1 {
			t.Errorf("POST sent %d times, want exactly once", seq.count())
		}
	})
}

func TestPatchManagedDatabase(t *testing.T) {
	attrs := components.ManagedDatabaseUpdatePayloadAttributes{TrustedSources: []string{"203.0.113.10/32"}}

	t.Run("an unexpected 2xx is accepted", func(t *testing.T) {
		_, r := newMdbSequence(t, mdbResponse{code: 202, body: `{}`})
		if err := r.patchManagedDatabase(context.Background(), "mdb_1", attrs, time.Now().Add(5*time.Second)); err != nil {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("429 and 5xx are retried until it succeeds", func(t *testing.T) {
		seq, r := newMdbSequence(t,
			mdbResponse{code: 429, body: `{}`},
			mdbResponse{code: 502, body: `{}`},
			mdbResponse{code: 200, body: `{"data":{"id":"mdb_1","type":"managed_databases","attributes":{"status":"running"}}}`},
		)
		if err := r.patchManagedDatabase(context.Background(), "mdb_1", attrs, time.Now().Add(5*time.Second)); err != nil {
			t.Fatalf("err = %v", err)
		}
		if seq.count() != 3 {
			t.Errorf("PATCH sent %d times, want 3", seq.count())
		}
	})

	t.Run("a dropped connection is retried", func(t *testing.T) {
		seq, r := newMdbSequence(t,
			mdbResponse{drop: true},
			mdbResponse{code: 200, body: `{"data":{"id":"mdb_1","type":"managed_databases","attributes":{"status":"running"}}}`},
		)
		if err := r.patchManagedDatabase(context.Background(), "mdb_1", attrs, time.Now().Add(5*time.Second)); err != nil {
			t.Fatalf("err = %v", err)
		}
		if seq.count() != 2 {
			t.Errorf("PATCH sent %d times, want 2", seq.count())
		}
	})

	t.Run("a 422 is not retried", func(t *testing.T) {
		seq, r := newMdbSequence(t, mdbResponse{code: 422, body: `{"errors":[{"detail":"bad"}]}`})
		err := r.patchManagedDatabase(context.Background(), "mdb_1", attrs, time.Now().Add(5*time.Second))
		if err == nil || !mdbPatchRejected(err) {
			t.Fatalf("err = %v, want a rejected 422", err)
		}
		if seq.count() != 1 {
			t.Errorf("PATCH sent %d times, want 1", seq.count())
		}
	})
}

// A body the SDK can't decode fails the same way on every poll, so it must
// not be retried as if the connection had dropped.
func TestWaitForManagedDatabaseReady_DecodeErrorIsNotRetried(t *testing.T) {
	seq, r := newMdbSequence(t, mdbResponse{code: 200, body: `{"data":{"id":"mdb_1","attributes":"not an object"}}`})
	got := ready(r, 5*time.Second, mdbWaitOpts{})
	if got.Kind != mdbWaitError {
		t.Fatalf("kind = %v, want an error", got.Kind)
	}
	if seq.count() != 1 {
		t.Errorf("Show called %d times, want 1 (no retries)", seq.count())
	}
}
