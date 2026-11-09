package latitudesh

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

// Regression tests for the firewall assignment lookups. Create, Read and
// ImportState find an assignment by walking GET /firewalls/{id}/assignments
// (and, on import, GET /firewalls). They used to read only the first page,
// which the API caps at 20 items by default, so on a firewall assigned to more
// than 20 targets the lookup missed the assignment.

const (
	pagFirewallID = "fw_PAGED"
	pagTargetID   = "fwasg_TARGET"
	pagServerID   = "sv_TARGET"
)

// pageParams returns the page the client asked for, applying the API defaults
// (page 1, 20 items) when a parameter is missing.
func pageParams(r *http.Request) (number, size int) {
	number, size = 1, 20
	if n, err := strconv.Atoi(r.URL.Query().Get("page[number]")); err == nil && n > 0 {
		number = n
	}
	if s, err := strconv.Atoi(r.URL.Query().Get("page[size]")); err == nil && s > 0 {
		size = s
	}
	return number, size
}

// paginate serves the slice of items for the requested page.
func paginate(r *http.Request, items []string) string {
	number, size := pageParams(r)
	start := (number - 1) * size
	if start > len(items) {
		start = len(items)
	}
	end := start + size
	if end > len(items) {
		end = len(items)
	}
	return `{"data":[` + strings.Join(items[start:end], ",") + `],"meta":{}}`
}

func assignmentEntry(firewallID, id, serverID string) string {
	return fmt.Sprintf(`{"id":%q,"type":"firewall_assignments","attributes":{`+
		`"firewall_id":%q,"server":{"id":%q,"hostname":"h-%s"}}}`, id, firewallID, serverID, serverID)
}

// assignmentsMock serves one firewall per entry of firewalls, each with the
// given number of assignments. The target assignment, when targetIndex >= 0,
// is the targetIndex-th assignment of the last firewall.
type assignmentsMock struct {
	firewalls   []string
	assignments int
	targetIndex int
}

func (m *assignmentsMock) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if r.URL.Path == "/firewalls" {
			var items []string
			for _, id := range m.firewalls {
				items = append(items, fmt.Sprintf(`{"id":%q,"type":"firewalls","attributes":{"name":%q}}`, id, id))
			}
			_, _ = w.Write([]byte(paginate(r, items)))
			return
		}

		firewallID, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/firewalls/"), "/assignments")
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		items := make([]string, 0, m.assignments)
		for i := 0; i < m.assignments; i++ {
			id, server := fmt.Sprintf("fwasg_%s_%d", firewallID, i), fmt.Sprintf("sv_%s_%d", firewallID, i)
			if firewallID == m.firewalls[len(m.firewalls)-1] && i == m.targetIndex {
				id, server = pagTargetID, pagServerID
			}
			items = append(items, assignmentEntry(firewallID, id, server))
		}
		_, _ = w.Write([]byte(paginate(r, items)))
	}))
	t.Cleanup(s.Close)
	return s
}

func newTestFirewallAssignmentResource(serverURL string) *FirewallAssignmentResource {
	return &FirewallAssignmentResource{
		client: latitudeshgosdk.New(
			latitudeshgosdk.WithSecurity("test"),
			latitudeshgosdk.WithServerURL(serverURL),
		),
	}
}

func runFirewallAssignmentRead(t *testing.T, r *FirewallAssignmentResource) *resource.ReadResponse {
	t.Helper()
	ctx := context.Background()

	schemaResp := &resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, schemaResp)
	sch := schemaResp.Schema
	objType := sch.Type().TerraformType(ctx).(tftypes.Object)

	stateVal := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":                 tftypes.NewValue(tftypes.String, pagTargetID),
		"firewall_id":        tftypes.NewValue(tftypes.String, pagFirewallID),
		"server_id":          tftypes.NewValue(tftypes.String, pagServerID),
		"virtual_machine_id": tftypes.NewValue(tftypes.String, nil),
	})

	req := resource.ReadRequest{State: tfsdk.State{Raw: stateVal, Schema: sch}}
	resp := &resource.ReadResponse{State: tfsdk.State{Raw: stateVal, Schema: sch}}
	r.Read(ctx, req, resp)
	return resp
}

func TestFirewallAssignmentRead_BeyondFirstPage(t *testing.T) {
	m := &assignmentsMock{firewalls: []string{pagFirewallID}, assignments: 130, targetIndex: 120}
	resp := runFirewallAssignmentRead(t, newTestFirewallAssignmentResource(m.server(t).URL))

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics.Errors())
	}
	if resp.State.Raw.IsNull() {
		t.Fatal("assignment exists past the first page but Read removed it from state")
	}
	var out FirewallAssignmentResourceModel
	if diags := resp.State.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("reading state: %v", diags.Errors())
	}
	if out.ID.ValueString() != pagTargetID || out.ServerID.ValueString() != pagServerID {
		t.Fatalf("expected id %q and server_id %q, got id=%v server_id=%v", pagTargetID, pagServerID, out.ID, out.ServerID)
	}
}

func TestFirewallAssignmentRead_DeletedOutOfBand(t *testing.T) {
	m := &assignmentsMock{firewalls: []string{pagFirewallID}, assignments: 130, targetIndex: -1}
	resp := runFirewallAssignmentRead(t, newTestFirewallAssignmentResource(m.server(t).URL))

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics.Errors())
	}
	if !resp.State.Raw.IsNull() {
		var out FirewallAssignmentResourceModel
		resp.State.Get(context.Background(), &out)
		t.Fatalf("expected a deleted assignment to be removed from state, got id=%v", out.ID)
	}
}

// After Create the ID is looked up by target; the new assignment is usually the
// last one, so on a busy firewall it is past the first page.
func TestFirewallAssignmentCreateLookup_BeyondFirstPage(t *testing.T) {
	m := &assignmentsMock{firewalls: []string{pagFirewallID}, assignments: 45, targetIndex: 44}
	r := newTestFirewallAssignmentResource(m.server(t).URL)

	data := FirewallAssignmentResourceModel{
		FirewallID:       types.StringValue(pagFirewallID),
		ServerID:         types.StringValue(pagServerID),
		VirtualMachineID: types.StringNull(),
	}
	var diags diag.Diagnostics
	r.findAssignmentByTargetAndFirewall(context.Background(), &data, &diags)

	if diags.HasError() {
		t.Fatalf("lookup failed: %v", diags.Errors())
	}
	if data.ID.ValueString() != pagTargetID {
		t.Fatalf("expected id %q, got %v", pagTargetID, data.ID)
	}
}

// terraform import only has the assignment ID, so every firewall is searched.
func TestFirewallAssignmentImportLookup_BeyondFirstPage(t *testing.T) {
	firewalls := make([]string, 0, 25)
	for i := 0; i < 24; i++ {
		firewalls = append(firewalls, fmt.Sprintf("fw_%d", i))
	}
	firewalls = append(firewalls, pagFirewallID)
	m := &assignmentsMock{firewalls: firewalls, assignments: 30, targetIndex: 25}
	r := newTestFirewallAssignmentResource(m.server(t).URL)

	data := FirewallAssignmentResourceModel{ID: types.StringValue(pagTargetID)}
	var diags diag.Diagnostics
	r.findFirewallForAssignment(context.Background(), &data, &diags)

	if diags.HasError() {
		t.Fatalf("lookup failed: %v", diags.Errors())
	}
	if data.FirewallID.ValueString() != pagFirewallID {
		t.Fatalf("expected firewall_id %q, got %v", pagFirewallID, data.FirewallID)
	}
}
