package latitudesh

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// mdbRecord is one managed database in the mock store.
type mdbRecord struct {
	ID             string
	Name           string
	ProjectID      string
	Region         string
	Plan           string
	Engine         string
	Billing        string
	TrustedSources []string
	Parameters     map[string]string
	Status         string // overrides the provisioning -> running walk when set
	Gets           int
	Deleted        bool
}

type mdbRequest struct {
	Method string
	Path   string
	Body   string
}

// mdbMock is the in-memory managed database API shared by every
// TestManagedDatabase_Mock* test. Its zero value answers a POST with
// "provisioning" and no credentials, then "provisioning" for readyAfterGETs
// GETs, then "running" with credentials. "running" is deliberately in none of
// the status sets, which exercises the credentials-based readiness rule.
type mdbMock struct {
	mu      sync.Mutex
	records map[string]*mdbRecord
	nextID  int
	firstID string
	lastID  string
	log     []mdbRequest

	// Switches.
	patchStatus    int               // non-zero: PATCH answers this status with an error body
	postCreds      bool              // the POST response carries credentials
	noShowCreds    bool              // Show never returns credentials
	omitBilling    bool              // Show omits plan.billing
	extraParams    map[string]string // keys the server adds to parameters
	readyAfterGETs int               // GETs answered "provisioning" before "running"
	projects       map[string]string // slug -> project ID, for GET /projects/{slug}
}

func newMdbMock(t *testing.T) (*mdbMock, *httptest.Server) {
	t.Helper()
	m := &mdbMock{records: map[string]*mdbRecord{}, readyAfterGETs: 1, projects: map[string]string{}}
	server := httptest.NewServer(http.HandlerFunc(m.handler))
	t.Cleanup(server.Close)
	mdbFastPolling(t)
	return m, server
}

// mdbFastPolling shortens the poll intervals for one test and restores them
// afterwards. Tests that use it must not call t.Parallel: the intervals are
// package variables.
func mdbFastPolling(t *testing.T) {
	t.Helper()
	prevReady, prevDelete, prevBudget := mdbReadyPollInterval, mdbDeletePollInterval, mdbErrorBudget
	mdbReadyPollInterval = 5 * time.Millisecond
	mdbDeletePollInterval = 5 * time.Millisecond
	mdbErrorBudget = 200 * time.Millisecond
	t.Cleanup(func() {
		mdbReadyPollInterval, mdbDeletePollInterval, mdbErrorBudget = prevReady, prevDelete, prevBudget
	})
}

// seed adds a database that already exists (for import-first tests).
func (m *mdbMock) seed(rec mdbRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec.Status == "" {
		rec.Status = "running"
	}
	if rec.Parameters == nil {
		rec.Parameters = map[string]string{}
	}
	m.records[rec.ID] = &rec
	if m.firstID == "" {
		m.firstID = rec.ID
	}
	m.lastID = rec.ID
}

// set changes the mock's switches under its lock.
func (m *mdbMock) set(f func(*mdbMock)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f(m)
}

func (m *mdbMock) resetLog() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.log = nil
}

func (m *mdbMock) requests() []mdbRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mdbRequest(nil), m.log...)
}

// methods returns the methods of the logged managed-database requests, in
// order, e.g. "POST GET GET PATCH".
func (m *mdbMock) methods() string {
	var out []string
	for _, r := range m.requests() {
		if strings.HasPrefix(r.Path, "/managed_databases") {
			out = append(out, r.Method)
		}
	}
	return strings.Join(out, " ")
}

func (m *mdbMock) count(method string) int {
	n := 0
	for _, r := range m.requests() {
		if r.Method == method && strings.HasPrefix(r.Path, "/managed_databases") {
			n++
		}
	}
	return n
}

func (m *mdbMock) ids() (first, last string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.firstID, m.lastID
}

func (m *mdbMock) deleteOutOfBand(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.records[id]; ok {
		rec.Deleted = true
	}
}

func (m *mdbMock) record(id string) *mdbRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.records[id]
}

func (m *mdbMock) status(rec *mdbRecord) string {
	if rec.Status != "" {
		return rec.Status
	}
	if rec.Gets > m.readyAfterGETs {
		return "running"
	}
	return "provisioning"
}

func (m *mdbMock) envelope(rec *mdbRecord, status string, withCreds bool) map[string]any {
	params := map[string]any{}
	for k, v := range rec.Parameters {
		params[k] = v
	}
	for k, v := range m.extraParams {
		if _, ok := params[k]; !ok {
			params[k] = v
		}
	}
	plan := map[string]any{"slug": rec.Plan}
	if !m.omitBilling {
		plan["billing"] = rec.Billing
	}
	attrs := map[string]any{
		"name":       rec.Name,
		"engine":     rec.Engine,
		"status":     status,
		"parameters": params,
		"plan":       plan,
		"region":     map[string]any{"city": "Ashburn", "site": map[string]any{"slug": rec.Region}},
		"project":    map[string]any{"id": rec.ProjectID},
	}
	if rec.TrustedSources != nil {
		attrs["trusted_sources"] = rec.TrustedSources
	}
	if withCreds {
		attrs["credentials"] = map[string]any{
			"host":     "db.mock.internal",
			"port":     5432,
			"database": "app",
			"username": "admin",
			"password": "mock-secret",
			"url":      "postgres://admin:mock-secret@db.mock.internal:5432/app",
		}
	}
	return map[string]any{"data": map[string]any{"id": rec.ID, "type": "managed_databases", "attributes": attrs}}
}

func (m *mdbMock) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.log = append(m.log, mdbRequest{Method: r.Method, Path: r.URL.Path, Body: string(body)})
	w.Header().Set("Content-Type", "application/vnd.api+json")

	writeJSON := func(status int, v any) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	notFound := func() {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404","detail":"not found"}]}`))
	}

	if strings.HasPrefix(r.URL.Path, "/projects/") {
		slug := strings.TrimPrefix(r.URL.Path, "/projects/")
		id, ok := m.projects[slug]
		if r.Method != http.MethodGet || !ok {
			notFound()
			return
		}
		writeJSON(http.StatusOK, map[string]any{"data": map[string]any{"id": id, "type": "projects", "attributes": map[string]any{"slug": slug}}})
		return
	}

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/managed_databases":
		var payload struct {
			Data struct {
				Attributes struct {
					Name       string            `json:"name"`
					ProjectID  string            `json:"project_id"`
					Region     string            `json:"region"`
					Plan       string            `json:"plan"`
					Engine     string            `json:"engine"`
					Billing    string            `json:"billing"`
					Parameters map[string]string `json:"parameters"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		a := payload.Data.Attributes
		m.nextID++
		rec := &mdbRecord{
			ID: fmt.Sprintf("mdb_mock_%d", m.nextID), Name: a.Name, ProjectID: a.ProjectID, Region: a.Region,
			Plan: a.Plan, Engine: a.Engine, Billing: a.Billing, Parameters: map[string]string{},
		}
		if rec.Billing == "" {
			rec.Billing = "monthly"
		}
		for k, v := range a.Parameters {
			rec.Parameters[k] = v
		}
		m.records[rec.ID] = rec
		if m.firstID == "" {
			m.firstID = rec.ID
		}
		m.lastID = rec.ID
		writeJSON(http.StatusCreated, m.envelope(rec, "provisioning", m.postCreds))

	case strings.HasPrefix(r.URL.Path, "/managed_databases/"):
		id := strings.TrimPrefix(r.URL.Path, "/managed_databases/")
		rec, ok := m.records[id]
		if !ok || rec.Deleted {
			notFound()
			return
		}
		switch r.Method {
		case http.MethodGet:
			rec.Gets++
			status := m.status(rec)
			writeJSON(http.StatusOK, m.envelope(rec, status, status == "running" && !m.noShowCreds))
		case http.MethodPatch:
			if m.patchStatus != 0 {
				w.WriteHeader(m.patchStatus)
				_, _ = w.Write([]byte(`{"errors":[{"status":"` + fmt.Sprint(m.patchStatus) + `","detail":"mock rejected the change"}]}`))
				return
			}
			var payload struct {
				Data struct {
					Attributes struct {
						TrustedSources []string          `json:"trusted_sources"`
						Parameters     map[string]string `json:"parameters"`
					} `json:"attributes"`
				} `json:"data"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if ts := payload.Data.Attributes.TrustedSources; ts != nil {
				rec.TrustedSources = ts
			}
			for k, v := range payload.Data.Attributes.Parameters {
				rec.Parameters[k] = v
			}
			status := m.status(rec)
			writeJSON(http.StatusOK, m.envelope(rec, status, status == "running" && !m.noShowCreds))
		case http.MethodDelete:
			rec.Deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}

	default:
		notFound()
	}
}

// testAccCheckMdbMockDestroyed checks that every database the mock created
// was deleted.
func testAccCheckMdbMockDestroyed(m *mdbMock) func(*terraform.State) error {
	return func(*terraform.State) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		for id, rec := range m.records {
			if !rec.Deleted {
				return fmt.Errorf("mock managed database %s still exists after destroy", id)
			}
		}
		return nil
	}
}
