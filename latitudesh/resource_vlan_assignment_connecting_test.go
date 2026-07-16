package latitudesh

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

// Regression tests for issue #190.
//
// The API accepts an assignment with HTTP 201 but provisioning runs
// asynchronously; until it completes the assignment sits at status
// "connecting" and shows nothing in the console. Create must wait for
// "connected" instead of reporting a false success. Both tests drive Create()
// directly against a mock API so no live credentials or VCR fixtures are
// needed.

// assignmentBody returns the JSON for a single assignment with the given status.
func assignmentBody(status string) string {
	return `{"id":"vnasg_STUCK01","type":"virtual_network_assignment",` +
		`"attributes":{"virtual_network_id":"vlan_TEST","vid":2043,` +
		`"description":"test VLAN","status":"` + status + `",` +
		`"server":{"id":"sv_TEST","hostname":"test-server-01","label":"","status":"on"}}}`
}

// vlanMockServer stands up a mock API. POST always returns 201 with status
// "connecting". GET (ListAssignments) returns "connecting" until connectAfter
// GETs have been served, then "connected". connectAfter == 0 means it never
// connects (the stuck case). failFirstGets makes the first N GETs return 503,
// to simulate a transient lookup error. It also counts DELETEs so tests can
// assert the rollback of an assignment that never connected.
type vlanMock struct {
	connectAfter  int32
	failFirstGets int32
	failDeletes   bool
	gets          int32
	deletes       int32
}

func (m *vlanMock) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":` + assignmentBody("connecting") + `}`))
		case http.MethodGet:
			n := atomic.AddInt32(&m.gets, 1)
			if n <= m.failFirstGets {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"errors":[{"status":"503"}]}`))
				return
			}
			status := "connecting"
			if m.connectAfter > 0 && n >= m.connectAfter {
				status = statusConnected
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[` + assignmentBody(status) + `],"meta":{}}`))
		case http.MethodDelete:
			atomic.AddInt32(&m.deletes, 1)
			if m.failDeletes {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"errors":[{"status":"503"}]}`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// runCreate drives r.Create() with a plan for server sv_TEST / vlan vlan_TEST
// and returns the response so callers can assert diagnostics and state.
func runCreate(t *testing.T, r *VlanAssignmentResource) *resource.CreateResponse {
	t.Helper()
	ctx := context.Background()

	schemaResp := &resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, schemaResp)
	sch := schemaResp.Schema
	objType := sch.Type().TerraformType(ctx).(tftypes.Object)

	planVal := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":                 tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"server_id":          tftypes.NewValue(tftypes.String, "sv_TEST"),
		"virtual_network_id": tftypes.NewValue(tftypes.String, "vlan_TEST"),
		"vid":                tftypes.NewValue(tftypes.Number, tftypes.UnknownValue),
		"description":        tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"status":             tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		// No timeouts block configured; the resource pins connectTimeout directly.
		"timeouts": tftypes.NewValue(objType.AttributeTypes["timeouts"], nil),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Raw: planVal, Schema: sch}}
	resp := &resource.CreateResponse{
		State: tfsdk.State{Raw: tftypes.NewValue(objType, nil), Schema: sch},
	}
	r.Create(ctx, req, resp)
	return resp
}

func newTestVlanResource(serverURL string) *VlanAssignmentResource {
	return &VlanAssignmentResource{
		client: latitudeshgosdk.New(
			latitudeshgosdk.WithSecurity("test"),
			latitudeshgosdk.WithServerURL(serverURL),
		),
		connectTimeout:      150 * time.Millisecond,
		connectPollInterval: 10 * time.Millisecond,
	}
}

// TestVlanAssignmentCreate_StuckConnecting: an assignment that never leaves
// "connecting" must fail the apply (not report success) and roll back the
// assignment it created so nothing is left unmanaged.
func TestVlanAssignmentCreate_StuckConnecting(t *testing.T) {
	m := &vlanMock{connectAfter: 0} // never connects
	server := m.server(t)

	resp := runCreate(t, newTestVlanResource(server.URL))

	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected Create to error when assignment is stuck 'connecting', but it succeeded")
	}
	found := false
	for _, d := range resp.Diagnostics.Errors() {
		if d.Summary() == "Assignment Not Connected" {
			found = true
			t.Logf("got expected diagnostic: %s: %s", d.Summary(), d.Detail())
		}
	}
	if !found {
		t.Fatalf("expected an 'Assignment Not Connected' diagnostic, got: %v", resp.Diagnostics.Errors())
	}
	if got := atomic.LoadInt32(&m.deletes); got == 0 {
		t.Fatalf("expected the stuck assignment to be rolled back with a DELETE, got %d deletes", got)
	}
}

// TestVlanAssignmentCreate_ReachesConnected: when the assignment transitions
// connecting -> connected within the timeout, Create succeeds, commits the
// connected status, and does not delete anything.
func TestVlanAssignmentCreate_ReachesConnected(t *testing.T) {
	m := &vlanMock{connectAfter: 2} // connects on the 2nd GET
	server := m.server(t)

	resp := runCreate(t, newTestVlanResource(server.URL))

	if resp.Diagnostics.HasError() {
		t.Fatalf("expected Create to succeed once connected, got: %v", resp.Diagnostics.Errors())
	}

	var out VlanAssignmentResourceModel
	resp.State.Get(context.Background(), &out)
	if got := out.Status.ValueString(); got != statusConnected {
		t.Fatalf("expected committed status %q, got %q", statusConnected, got)
	}
	if out.ID.ValueString() != "vnasg_STUCK01" || out.Vid.ValueInt64() != 2043 {
		t.Fatalf("unexpected committed state: id=%q vid=%d", out.ID.ValueString(), out.Vid.ValueInt64())
	}
	if got := atomic.LoadInt32(&m.deletes); got != 0 {
		t.Fatalf("expected no rollback on success, got %d deletes", got)
	}
}

// TestVlanAssignmentCreate_TransientLookupError: a few transient 503s from the
// status poll must not abandon the assignment — Create keeps polling within the
// deadline and still succeeds once it reaches "connected".
func TestVlanAssignmentCreate_TransientLookupError(t *testing.T) {
	m := &vlanMock{connectAfter: 3, failFirstGets: 2} // GET 1&2 -> 503, GET 3 -> connected
	server := m.server(t)

	resp := runCreate(t, newTestVlanResource(server.URL))

	if resp.Diagnostics.HasError() {
		t.Fatalf("expected Create to survive transient lookup errors and succeed, got: %v", resp.Diagnostics.Errors())
	}

	var out VlanAssignmentResourceModel
	resp.State.Get(context.Background(), &out)
	if got := out.Status.ValueString(); got != statusConnected {
		t.Fatalf("expected committed status %q, got %q", statusConnected, got)
	}
	if got := atomic.LoadInt32(&m.deletes); got != 0 {
		t.Fatalf("expected no rollback once connected, got %d deletes", got)
	}
}

// TestVlanAssignmentCreate_RollbackFailurePersistsState: if the assignment
// never connects AND the rollback DELETE keeps failing, Create must not leak
// the assignment — it retries the delete, then persists the assignment to state
// so Terraform tracks (and replaces) it on the next apply.
func TestVlanAssignmentCreate_RollbackFailurePersistsState(t *testing.T) {
	m := &vlanMock{connectAfter: 0, failDeletes: true} // never connects; rollback always 503
	server := m.server(t)

	resp := runCreate(t, newTestVlanResource(server.URL))

	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected Create to error when the assignment never connects")
	}
	if got := atomic.LoadInt32(&m.deletes); got < 2 {
		t.Fatalf("expected the rollback DELETE to be retried, got %d attempts", got)
	}

	// Rollback failed, so the still-existing assignment must be tracked in state
	// (create-with-error taints it → replaced next apply) rather than leaked.
	var out VlanAssignmentResourceModel
	resp.State.Get(context.Background(), &out)
	if out.ID.ValueString() != "vnasg_STUCK01" {
		t.Fatalf("expected the un-rolled-back assignment to be persisted in state, got id=%q", out.ID.ValueString())
	}
}
