package latitudesh

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

// Regression tests for the virtual network Read path.
//
// Read looks the network up by walking GET /virtual_networks. That walk used to
// request a single unfiltered page, so a network that sat beyond the first page
// was reported as missing. The "missing" branch then nulled the ID in place
// without removing the resource, leaving state with every attribute intact but
// `id = null`. Terraform surfaced that as phantom drift
// (`- id = "vlan_..." -> null`), and any config referencing the network's id
// failed to plan:
//
//	Error: Missing Configuration for Required Attribute
//	Must set a configuration value for the virtual_network_id attribute
//
// Both tests drive Read() directly against a mock API so no live credentials or
// VCR fixtures are needed.

const (
	testPagVNetID   = "vlan_PAGE2"
	testPagProject  = "proj_TEST"
	testPagVNetDesc = "test VLAN"
	testPagVNetVid  = 2145
)

// vnetPage renders one JSON:API page of virtual networks. Only the target
// network carries meaningful attributes; the fillers exist to pad the page.
func vnetPage(entries []string) string {
	return `{"data":[` + strings.Join(entries, ",") + `],"meta":{}}`
}

func vnetEntry(id, description string, vid int) string {
	return fmt.Sprintf(`{"id":%q,"type":"virtual_networks","attributes":{`+
		`"vid":%d,"description":%q,"assignments_count":3,`+
		`"project":{"id":%q},`+
		`"region":{"city":"Test City","country":"TC","site":{"id":"site_TEST","slug":"TEST1"}}}}`,
		id, vid, description, testPagProject)
}

// vnetMock stands up a mock API for GET /virtual_networks. It serves
// totalPages pages of pageSize entries each; the target network is placed on
// targetPage. targetPage == 0 means the network is absent entirely, simulating
// one deleted outside of Terraform. It records the pages actually requested so
// tests can assert the walk happened.
type vnetMock struct {
	totalPages int
	targetPage int
	pageSize   int
	requests   int32
	lastPage   int32
}

func (m *vnetMock) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		atomic.AddInt32(&m.requests, 1)

		page, _ := strconv.Atoi(r.URL.Query().Get("page[number]"))
		if page == 0 {
			page = 1
		}
		atomic.StoreInt32(&m.lastPage, int32(page))

		// Honour the page size the provider asked for; the walk relies on a
		// short page to know it has reached the end.
		size := m.pageSize
		if requested, err := strconv.Atoi(r.URL.Query().Get("page[size]")); err == nil && requested > 0 {
			size = requested
		}

		if page > m.totalPages {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(vnetPage(nil)))
			return
		}

		entries := make([]string, 0, size)
		for i := 0; i < size; i++ {
			entries = append(entries, vnetEntry(fmt.Sprintf("vlan_FILL%d_%d", page, i), "filler", 1000+i))
		}
		if page == m.targetPage {
			// Replace one filler so the page still has exactly `size` entries
			// and the walk does not stop early on a short page.
			entries[0] = vnetEntry(testPagVNetID, testPagVNetDesc, testPagVNetVid)
		}
		if page == m.totalPages {
			// Last page is short, which is how the walk detects the end.
			entries = entries[:len(entries)-1]
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(vnetPage(entries)))
	}))
	t.Cleanup(s.Close)
	return s
}

func newTestVirtualNetworkResource(serverURL string) *VirtualNetworkResource {
	return &VirtualNetworkResource{
		client: latitudeshgosdk.New(
			latitudeshgosdk.WithSecurity("test"),
			latitudeshgosdk.WithServerURL(serverURL),
		),
	}
}

// runVirtualNetworkRead drives r.Read() with a prior state describing an
// already-applied virtual network, and returns the response.
func runVirtualNetworkRead(t *testing.T, r *VirtualNetworkResource) *resource.ReadResponse {
	t.Helper()
	ctx := context.Background()

	schemaResp := &resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, schemaResp)
	sch := schemaResp.Schema
	objType := sch.Type().TerraformType(ctx).(tftypes.Object)

	stateVal := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":                tftypes.NewValue(tftypes.String, testPagVNetID),
		"project":           tftypes.NewValue(tftypes.String, testPagProject),
		"site":              tftypes.NewValue(tftypes.String, "TEST1"),
		"description":       tftypes.NewValue(tftypes.String, testPagVNetDesc),
		"tags":              tftypes.NewValue(objType.AttributeTypes["tags"], nil),
		"vid":               tftypes.NewValue(tftypes.Number, testPagVNetVid),
		"region":            tftypes.NewValue(tftypes.String, "TEST1"),
		"assignments_count": tftypes.NewValue(tftypes.Number, 3),
	})

	req := resource.ReadRequest{State: tfsdk.State{Raw: stateVal, Schema: sch}}
	resp := &resource.ReadResponse{State: tfsdk.State{Raw: stateVal, Schema: sch}}
	r.Read(ctx, req, resp)
	return resp
}

// TestVirtualNetworkRead_BeyondFirstPage: a virtual network that lives past the
// first page of GET /virtual_networks must still be found. Before the fix the
// lookup read only page 1, concluded the network was gone, and nulled its ID.
func TestVirtualNetworkRead_BeyondFirstPage(t *testing.T) {
	// Small pages so the test stays cheap; the walk is page-size agnostic.
	m := &vnetMock{totalPages: 3, targetPage: 3, pageSize: 100}
	server := m.server(t)

	resp := runVirtualNetworkRead(t, newTestVirtualNetworkResource(server.URL))

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics.Errors())
	}
	if resp.State.Raw.IsNull() {
		t.Fatalf("network exists on page %d but Read removed it from state", m.targetPage)
	}

	var out VirtualNetworkResourceModel
	if diags := resp.State.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("reading state: %v", diags.Errors())
	}
	if out.ID.IsNull() {
		t.Fatalf("id was nulled for a network that exists — this is the phantom `id -> null` drift")
	}
	if got := out.ID.ValueString(); got != testPagVNetID {
		t.Fatalf("expected id %q, got %q", testPagVNetID, got)
	}
	if got := out.Vid.ValueInt64(); got != testPagVNetVid {
		t.Fatalf("expected vid %d, got %d", testPagVNetVid, got)
	}
	if got := out.Description.ValueString(); got != testPagVNetDesc {
		t.Fatalf("expected description %q, got %q", testPagVNetDesc, got)
	}
	if got := atomic.LoadInt32(&m.lastPage); got < 3 {
		t.Fatalf("expected the lookup to walk to page 3, highest page requested was %d", got)
	}
}

// TestVirtualNetworkRead_DeletedOutOfBand: a network that genuinely no longer
// exists must be removed from state so Terraform plans a fresh create. Nulling
// the ID in place instead leaves a half-populated state entry whose `id` is
// null, which breaks every config that references it.
func TestVirtualNetworkRead_DeletedOutOfBand(t *testing.T) {
	m := &vnetMock{totalPages: 2, targetPage: 0, pageSize: 100} // never present
	server := m.server(t)

	resp := runVirtualNetworkRead(t, newTestVirtualNetworkResource(server.URL))

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics.Errors())
	}
	if !resp.State.Raw.IsNull() {
		var out VirtualNetworkResourceModel
		resp.State.Get(context.Background(), &out)
		t.Fatalf("expected a deleted network to be removed from state, got a retained entry with id=%v", out.ID)
	}
}

// TestVirtualNetworkRead_ScopedToProject: when the project is known from state
// the walk must filter by it, so a token that can see many projects does not
// have to page through all of them.
func TestVirtualNetworkRead_ScopedToProject(t *testing.T) {
	var gotFilter string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFilter = r.URL.Query().Get("filter[project]")
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(vnetPage([]string{vnetEntry(testPagVNetID, testPagVNetDesc, testPagVNetVid)})))
	}))
	t.Cleanup(s.Close)

	resp := runVirtualNetworkRead(t, newTestVirtualNetworkResource(s.URL))

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics.Errors())
	}
	if gotFilter != testPagProject {
		t.Fatalf("expected the lookup to be scoped to filter[project]=%s, got %q", testPagProject, gotFilter)
	}
}
