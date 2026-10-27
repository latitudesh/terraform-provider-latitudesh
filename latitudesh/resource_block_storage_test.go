package latitudesh

// The mock-backed tests exercise the BlockStorage resource against a local
// mock of the Latitude.sh API, using the same httpClient injection hook as
// the virtual machine site tests (testAccProtoV6ProviderFactoriesWithMock /
// mockRedirectTransport, defined in resource_virtual_machine_site_test.go).
// They run under plain `go test` (IsUnitTest: true), no credentials needed.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// mockVolume is one volume tracked by the mock BlockStorage API.
type mockVolume struct {
	id          string
	project     string
	name        string
	region      string
	sizeInGb    int64
	namespaceID int64
	connectorID string
	createdAt   string
	deleted     bool

	// block is the NVMe-TCP mapping, nil when the volume is not attached.
	block *mockBlockMapping
}

// mockBlockMapping models the asynchronous map/unmap state machine: each read
// burns one `pending` tick, and the transition happens on the read after that
// ("mapping" becomes "mapped", "unmapping" clears the mapping entirely).
type mockBlockMapping struct {
	status   string
	serverID string
	pending  int
}

type mockBlockStorageAPI struct {
	mu      sync.Mutex
	volumes map[string]*mockVolume
	nextID  int

	// hideFirstGets makes the first N single-volume GETs of each volume answer
	// 404, modelling an API that accepts a create before the volume is
	// readable. Delayed *removal* on delete is deliberately not modelled: the
	// provider does not poll after DELETE, and whether it needs to is an open
	// question only the live suite can settle.
	hideFirstGets int
	getMisses     map[string]int

	// failFirstGets makes the first N single-volume GETs of each volume fail
	// with an error other than a 404 (a 403 here, so the client does not retry).
	failFirstGets int
	getFailures   map[string]int

	// mapPendingReads is how many reads each mapping spends in its transient
	// state before settling. Zero means the very first read already settles it.
	mapPendingReads int

	// failMapping settles new mappings into "failed" instead of "mapped".
	failMapping bool

	// rejectResize answers resizes with the 422 the API returns for volumes it
	// cannot resize (anything but high performance).
	rejectResize bool

	// failUnmapping settles unmaps back into "mapped", which is how the API
	// reports a detach that failed.
	failUnmapping bool

	// ineligibleServers are rejected by the map request with the API's 422, as
	// servers that are not cabled for a bond are.
	ineligibleServers map[string]bool

	// calls records every mutating request in order, so tests can assert the
	// sequence Terraform drove (unmap before the volume delete, say).
	calls []string
}

func newMockBlockStorageAPI() *mockBlockStorageAPI {
	return &mockBlockStorageAPI{volumes: map[string]*mockVolume{}, getMisses: map[string]int{}, getFailures: map[string]int{}, mapPendingReads: 1}
}

// advance moves a mapping one read closer to its terminal state. Callers must
// hold the lock.
func (m *mockBlockStorageAPI) advance(v *mockVolume) {
	if v.block == nil {
		return
	}
	if v.block.pending > 0 {
		v.block.pending--
		return
	}
	switch v.block.status {
	case "mapping":
		if m.failMapping {
			v.block.status = "failed"
			return
		}
		v.block.status = "mapped"
	case "unmapping":
		if m.failUnmapping {
			v.block.status = "mapped"
			return
		}
		v.block = nil
	}
}

// clearMappings unmaps every volume behind Terraform's back.
func (m *mockBlockStorageAPI) clearMappings() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.volumes {
		v.block = nil
	}
}

// callLog returns the mutating requests received so far, in order.
func (m *mockBlockStorageAPI) callLog() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...)
}

// mapping returns the volume's current mapping, for assertions in tests.
func (m *mockBlockStorageAPI) mapping(id string) *mockBlockMapping {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.volumes[id]
	if !ok {
		return nil
	}
	return v.block
}

func (m *mockBlockStorageAPI) envelope(v *mockVolume) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"id":   v.id,
			"type": "volumes",
			"attributes": map[string]any{
				"name":         v.name,
				"size_in_gb":   v.sizeInGb,
				"created_at":   v.createdAt,
				"namespace_id": v.namespaceID,
				"connector_id": v.connectorID,
				"region": map[string]any{
					"city":    "Ashburn",
					"country": "US",
					"site": map[string]any{
						"id":       "site_" + v.region,
						"name":     v.region,
						"slug":     v.region,
						"facility": "AM1",
					},
				},
				"project": map[string]any{
					"id":   v.project,
					"slug": "test-project",
				},
				"block": mockBlockEnvelope(v.block),
			},
		},
	}
}

func mockBlockEnvelope(b *mockBlockMapping) any {
	if b == nil {
		return nil
	}
	return map[string]any{
		"status":    b.status,
		"nqn":       "nqn.2014-08.sh.latitude:id:" + b.serverID,
		"nsid":      1,
		"server_id": b.serverID,
		"storage_network": map[string]any{
			"vid":           2090,
			"host_cidr":     "10.248.6.4/24",
			"gateway":       "10.248.6.1",
			"routes":        []string{"10.248.213.0/24", "10.248.214.0/24"},
			"block_gateway": "10.248.213.180",
			"block_port":    4420,
		},
	}
}

func (m *mockBlockStorageAPI) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if r.Method != http.MethodGet {
		m.calls = append(m.calls, r.Method+" "+r.URL.Path)
	}

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/storage/volumes":
		var payload struct {
			Data struct {
				Attributes struct {
					Project  string `json:"project"`
					Name     string `json:"name"`
					Region   string `json:"region"`
					SizeInGb int64  `json:"size_in_gb"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		m.nextID++
		v := &mockVolume{
			id:          "vol_mock_" + strconv.Itoa(m.nextID),
			project:     payload.Data.Attributes.Project,
			name:        payload.Data.Attributes.Name,
			region:      payload.Data.Attributes.Region,
			sizeInGb:    payload.Data.Attributes.SizeInGb,
			namespaceID: int64(m.nextID),
			connectorID: "conn_mock_" + strconv.Itoa(m.nextID),
			createdAt:   fmt.Sprintf("2026-01-%02dT00:00:00Z", m.nextID),
		}
		m.volumes[v.id] = v
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(m.envelope(v))

	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/storage/volumes/"):
		id := strings.TrimPrefix(r.URL.Path, "/storage/volumes/")
		v, ok := m.volumes[id]
		if ok && m.getFailures[id] < m.failFirstGets {
			m.getFailures[id]++
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":[{"status":"403","title":"Forbidden"}]}`))
			return
		}
		if ok && m.getMisses[id] < m.hideFirstGets {
			m.getMisses[id]++
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		if !ok || v.deleted {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		m.advance(v)
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.envelope(v))

	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/storage/volumes/"):
		id := strings.TrimPrefix(r.URL.Path, "/storage/volumes/")
		v, ok := m.volumes[id]
		if !ok || v.deleted {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		v.deleted = true
		w.WriteHeader(http.StatusNoContent)

	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/storage/volumes/"):
		id := strings.TrimPrefix(r.URL.Path, "/storage/volumes/")
		v, ok := m.volumes[id]
		if !ok || v.deleted {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		var payload struct {
			Data struct {
				Attributes struct {
					SizeInGb int64 `json:"size_in_gb"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if m.rejectResize {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"errors":[{"code":"STORAGE_OPERATION_UNSUPPORTED","status":"422","title":"Storage operation unsupported","detail":"Resizing a volume is only available for high performance volumes."}]}`))
			return
		}
		// Like the API, refuse anything but growth (a resize to the same size too).
		if payload.Data.Attributes.SizeInGb <= v.sizeInGb {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"errors":[{"code":"STORAGE_VALIDATION_FAILED","status":"422","title":"Storage Validation Failed","detail":"Volumes can only be increased in size."}]}`))
			return
		}
		v.sizeInGb = payload.Data.Attributes.SizeInGb
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.envelope(v))

	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/map"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/storage/volumes/"), "/map")
		v, ok := m.volumes[id]
		if !ok || v.deleted {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		var payload struct {
			Data struct {
				Attributes struct {
					ServerID string `json:"server_id"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// Like the API, refuse a second mapping while one is active; a failed
		// one has been rolled back and does not count.
		if v.block != nil && v.block.status != "failed" {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"errors":[{"code":"STORAGE_CONFLICT","status":"409","detail":"This volume is already mapped to a server."}]}`))
			return
		}
		if m.ineligibleServers[payload.Data.Attributes.ServerID] {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"errors":[{"code":"SERVER_NOT_ELIGIBLE_FOR_VOLUME_MAPPING","status":"422","title":"Server not eligible for volume mapping","detail":"This server is not eligible to be mapped to a volume."}]}`))
			return
		}
		v.block = &mockBlockMapping{status: "mapping", serverID: payload.Data.Attributes.ServerID, pending: m.mapPendingReads}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(m.envelope(v))

	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/unmap"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/storage/volumes/"), "/unmap")
		v, ok := m.volumes[id]
		if !ok || v.deleted {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		// Like the API, only a settled mapping can be unmapped.
		if v.block == nil || v.block.status != "mapped" {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"errors":[{"code":"STORAGE_CONFLICT","status":"409","detail":"This volume is not currently mapped to the server."}]}`))
			return
		}
		v.block.status = "unmapping"
		v.block.pending = m.mapPendingReads
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(m.envelope(v))

	case r.Method == http.MethodGet && r.URL.Path == "/storage/volumes":
		filterProject := r.URL.Query().Get("filter[project]")
		data := make([]map[string]any, 0)
		for _, v := range m.volumes {
			if v.deleted {
				continue
			}
			if filterProject != "" && v.project != filterProject {
				continue
			}
			data = append(data, m.envelope(v)["data"].(map[string]any))
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})

	default:
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

func (m *mockBlockStorageAPI) exists(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.volumes[id]
	return ok && !v.deleted
}

func testAccCheckMockBlockStorageDestroyed(mock *mockBlockStorageAPI) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "latitudesh_block_storage" {
				continue
			}
			if mock.exists(rs.Primary.ID) {
				return fmt.Errorf("mock volume %s still exists after destroy", rs.Primary.ID)
			}
		}
		return nil
	}
}

func testAccBlockStorageConfig(project, name, region string, sizeInGb int) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_block_storage" "test_item" {
  project    = %q
  name       = %q
  region     = %q
  size_in_gb = %d
}
`, project, name, region, sizeInGb)
}

func TestBlockStorage_CreateReadDelete(t *testing.T) {
	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBlockStorageDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccBlockStorageConfig("test-project", "app-data", "ASH", 100),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "project", "test-project"),
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "name", "app-data"),
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "region", "ASH"),
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "size_in_gb", "100"),
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "namespace_id", "1"),
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "connector_id", "conn_mock_1"),
				),
			},
			{
				// Idempotency — same config, plan must be empty.
				Config:   testAccBlockStorageConfig("test-project", "app-data", "ASH", 100),
				PlanOnly: true,
			},
		},
	})
}

// Every Required/RequiresReplace input must survive import, or the first
// plan after import proposes a replacement.
func TestBlockStorage_Import(t *testing.T) {
	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBlockStorageDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccBlockStorageConfig("test-project", "app-data", "ASH", 50),
			},
			{
				ResourceName:      "latitudesh_block_storage.test_item",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// testAccCaptureVolumeID records the volume's id, so a later step can tell an
// in-place update from a replacement.
func testAccCaptureVolumeID(id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources["latitudesh_block_storage.test_item"]
		if !ok {
			return fmt.Errorf("latitudesh_block_storage.test_item not in state")
		}
		*id = rs.Primary.ID
		return nil
	}
}

// countCalls reports how many mutating requests start with prefix.
func countCalls(mock *mockBlockStorageAPI, prefix string) int {
	n := 0
	for _, call := range mock.callLog() {
		if strings.HasPrefix(call, prefix) {
			n++
		}
	}
	return n
}

// Growing size_in_gb must resize the same volume in place (UpdateVolume),
// keeping its id and its data, instead of deleting it for a new one.
func TestBlockStorage_GrowResizesInPlace(t *testing.T) {
	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	const name = "latitudesh_block_storage.test_item"
	var firstID string

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBlockStorageDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccBlockStorageConfig("test-project", "app-data", "ASH", 50),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(name, "size_in_gb", "50"),
					testAccCaptureVolumeID(&firstID),
				),
			},
			{
				Config: testAccBlockStorageConfig("test-project", "app-data", "ASH", 100),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(name, "size_in_gb", "100"),
					func(s *terraform.State) error {
						if got := s.RootModule().Resources[name].Primary.ID; got != firstID {
							return fmt.Errorf("growing the volume replaced it: id %s became %s", firstID, got)
						}
						if n := countCalls(mock, "DELETE "); n != 0 {
							return fmt.Errorf("growing the volume deleted it: %v", mock.callLog())
						}
						if n := countCalls(mock, "PATCH /storage/volumes/"+firstID); n != 1 {
							return fmt.Errorf("expected one resize of %s, got %d: %v", firstID, n, mock.callLog())
						}
						return nil
					},
				),
			},
			{
				// Idempotency — the resized volume must read back without drift.
				Config:   testAccBlockStorageConfig("test-project", "app-data", "ASH", 100),
				PlanOnly: true,
			},
		},
	})
}

// Volumes cannot shrink, so a smaller size_in_gb must replace the volume
// rather than send a resize the API would reject.
func TestBlockStorage_ShrinkForcesReplacement(t *testing.T) {
	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	const name = "latitudesh_block_storage.test_item"
	var firstID string

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBlockStorageDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccBlockStorageConfig("test-project", "app-data", "ASH", 100),
				Check:  testAccCaptureVolumeID(&firstID),
			},
			{
				Config: testAccBlockStorageConfig("test-project", "app-data", "ASH", 50),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(name, "size_in_gb", "50"),
					func(s *terraform.State) error {
						if got := s.RootModule().Resources[name].Primary.ID; got == firstID {
							return fmt.Errorf("shrinking kept volume %s instead of replacing it", firstID)
						}
						if n := countCalls(mock, "PATCH "); n != 0 {
							return fmt.Errorf("shrinking sent a resize: %v", mock.callLog())
						}
						if mock.exists(firstID) {
							return fmt.Errorf("the replaced volume %s was not deleted", firstID)
						}
						return nil
					},
				),
			},
		},
	})
}

// A resize the API refuses (it only resizes high performance volumes, for
// one) must fail the apply with the API's message and leave the size in
// state untouched, rather than recording a resize that never happened.
func TestBlockStorage_ResizeFailureFailsApply(t *testing.T) {
	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	const name = "latitudesh_block_storage.test_item"

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBlockStorageDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccBlockStorageConfig("test-project", "app-data", "ASH", 50),
			},
			{
				PreConfig: func() {
					mock.mu.Lock()
					defer mock.mu.Unlock()
					mock.rejectResize = true
				},
				Config:      testAccBlockStorageConfig("test-project", "app-data", "ASH", 100),
				ExpectError: regexp.MustCompile(`(?s)Unable to resize block storage volume.*STORAGE_OPERATION_UNSUPPORTED`),
			},
			{
				// The failed resize must not have been written to state.
				PreConfig: func() {
					mock.mu.Lock()
					defer mock.mu.Unlock()
					mock.rejectResize = false
				},
				Config: testAccBlockStorageConfig("test-project", "app-data", "ASH", 50),
				Check:  resource.TestCheckResourceAttr(name, "size_in_gb", "50"),
			},
		},
	})
}

// A volume that is not readable yet must still land in state from the create
// response. Nulling the id there would make Terraform reject the apply as an
// inconsistent result and leave a live, billing volume outside state.
func TestBlockStorage_CreateSurvivesUnreadableVolume(t *testing.T) {
	mock := newMockBlockStorageAPI()
	mock.hideFirstGets = 1
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBlockStorageDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccBlockStorageConfig("test-project", "app-data", "ASH", 100),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("latitudesh_block_storage.test_item", "id"),
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "name", "app-data"),
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "size_in_gb", "100"),
					// Filled from the create response, not the read-back.
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "namespace_id", "1"),
				),
			},
		},
	})
}

// blockStorageNotFound must accept only a 404 APIError, never a 403 or a
// transient 5xx (which would otherwise read as "gone" and drop a live volume
// from state, or make destroy report false success).
// A read-back that fails with anything but a 404 must not cost the volume its
// place in state either: it was created and is billing, so the apply saves it
// from the create response (with a warning) instead of leaking it.
func TestBlockStorage_CreateSurvivesReadBackError(t *testing.T) {
	mock := newMockBlockStorageAPI()
	mock.failFirstGets = 1
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBlockStorageDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccBlockStorageConfig("test-project", "app-data", "ASH", 100),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "id", "vol_mock_1"),
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "size_in_gb", "100"),
					func(*terraform.State) error {
						if n := countCalls(mock, "POST /storage/volumes"); n != 1 {
							return fmt.Errorf("expected exactly one volume to be created, got %d: %v", n, mock.callLog())
						}
						return nil
					},
				),
			},
		},
	})
}

func TestBlockStorageNotFound(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"404 APIError", &components.APIError{StatusCode: http.StatusNotFound, Message: "not found"}, true},
		{"403 APIError", &components.APIError{StatusCode: http.StatusForbidden, Message: "forbidden"}, false},
		{"500 APIError", &components.APIError{StatusCode: http.StatusInternalServerError, Message: "boom"}, false},
		{"plain error", fmt.Errorf("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := blockStorageNotFound(tc.err); got != tc.want {
				t.Errorf("blockStorageNotFound(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// mapBlockStorageVolume must null purely-computed fields on absence, and only
// backfill project/region when state has nothing (import), never clobbering a
// configured slug with the API's canonical form.
func TestMapBlockStorageVolume(t *testing.T) {
	t.Run("nil attributes leave id set and everything else untouched", func(t *testing.T) {
		var data BlockStorageResourceModel
		id := "vol_1"
		mapBlockStorageVolume(&data, &components.VolumeData{ID: &id})
		if data.ID.ValueString() != "vol_1" {
			t.Fatalf("ID = %v, want vol_1", data.ID)
		}
		if !data.Name.IsNull() {
			t.Fatalf("Name = %v, want null (no attributes)", data.Name)
		}
	})

	t.Run("purely computed fields null on absence", func(t *testing.T) {
		data := BlockStorageResourceModel{
			NamespaceID: types.Int64Value(99),
			ConnectorID: types.StringValue("stale"),
		}
		id := "vol_1"
		name := "app-data"
		size := int64(100)
		mapBlockStorageVolume(&data, &components.VolumeData{
			ID: &id,
			Attributes: &components.VolumeDataAttributes{
				Name:     &name,
				SizeInGb: &size,
			},
		})
		if !data.NamespaceID.IsNull() {
			t.Errorf("NamespaceID = %v, want null", data.NamespaceID)
		}
		if !data.ConnectorID.IsNull() {
			t.Errorf("ConnectorID = %v, want null", data.ConnectorID)
		}
	})

	t.Run("project and region only backfilled when unset", func(t *testing.T) {
		configuredProject := types.StringValue("my-slug")
		configuredRegion := types.StringValue("ash")
		data := BlockStorageResourceModel{Project: configuredProject, Region: configuredRegion}

		apiProjectID := "proj_mock_1"
		apiSlug := "canonical-slug"
		apiSiteSlug := "ASH"
		mapBlockStorageVolume(&data, &components.VolumeData{
			Attributes: &components.VolumeDataAttributes{
				Project: &components.ProjectInclude{ID: &apiProjectID, Slug: &apiSlug},
				Region:  &components.VolumeDataRegion{Site: &components.VolumeDataSite{Slug: &apiSiteSlug}},
			},
		})
		if data.Project.ValueString() != "my-slug" {
			t.Errorf("Project = %v, want unchanged 'my-slug'", data.Project)
		}
		if data.Region.ValueString() != "ash" {
			t.Errorf("Region = %v, want unchanged 'ash'", data.Region)
		}

		// Now with nothing configured (import path): both backfill from the API.
		var imported BlockStorageResourceModel
		mapBlockStorageVolume(&imported, &components.VolumeData{
			Attributes: &components.VolumeDataAttributes{
				Project: &components.ProjectInclude{ID: &apiProjectID, Slug: &apiSlug},
				Region:  &components.VolumeDataRegion{Site: &components.VolumeDataSite{Slug: &apiSiteSlug}},
			},
		})
		if imported.Project.ValueString() != apiSlug {
			t.Errorf("imported Project = %v, want %v (slug preferred over id)", imported.Project, apiSlug)
		}
		if imported.Region.ValueString() != apiSiteSlug {
			t.Errorf("imported Region = %v, want %v", imported.Region, apiSiteSlug)
		}
	})
}

// TestAccBlockStorage_Basic exercises create, read and import against the
// live API. The site (testBlockStorageSite) is not documented anywhere on
// the BlockStorage group itself; ASH is reused from latitudesh_object_storage,
// whose `region` attribute has the identical "site slug" doc comment - a
// human should confirm ASH actually has block storage capacity before
// relying on this test. The project is configured by slug (not ID) so the
// value round-trips exactly through import: readBlockStorageInto backfills
// `project` from the API's echoed slug, and an ID-configured project would
// otherwise read as a mismatch on ImportStateVerify (the trap documented for
// latitudesh_object_storage's own live test, which papers over it with
// ImportStateVerifyIgnore instead of configuring around it).
const testBlockStorageSite = "ASH"

func TestAccBlockStorage_Basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	resourceName := "latitudesh_block_storage.test_item"
	projectSlug := testAccProjectSlug(t)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckBlockStorageDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccConfigBlockStorage(projectSlug, "tf-acc-block-storage-"+testRunID, testBlockStorageSite, 50),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "name", "tf-acc-block-storage-"+testRunID),
					resource.TestCheckResourceAttr(resourceName, "region", testBlockStorageSite),
					resource.TestCheckResourceAttr(resourceName, "project", projectSlug),
					resource.TestCheckResourceAttr(resourceName, "size_in_gb", "50"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccConfigBlockStorage(project, name, region string, sizeInGb int) string {
	return fmt.Sprintf(`
resource "latitudesh_block_storage" "test_item" {
  project    = %q
  name       = %q
  region     = %q
  size_in_gb = %d
}
`, project, name, region, sizeInGb)
}

func testAccCheckBlockStorageDestroy(s *terraform.State) error {
	ctx := context.Background()
	client, err := newSDKClientFromEnv()
	if err != nil {
		return err
	}

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_block_storage" {
			continue
		}
		id := rs.Primary.ID
		if id == "" {
			continue
		}

		resp, err := client.BlockStorage.RetrieveVolume(ctx, id)
		if err == nil && resp != nil && resp.Object != nil && resp.Object.Data != nil &&
			resp.Object.Data.ID != nil && *resp.Object.Data.ID == id {
			return fmt.Errorf("block storage volume still exists: %s", id)
		}
		if err != nil && !blockStorageNotFound(err) {
			return fmt.Errorf("checking block storage volume %s destroyed: %w", id, err)
		}
	}
	return nil
}

// Both shapes the live API returns must unmarshal with the pinned SDK. The
// integer `namespace_id` below is what volumes attached through the deprecated
// mount flow report; it used to need a response-rewriting transport, because
// the SDK typed the field as a string until v1.19.27 corrected it to an
// integer. This guards the regression if the type ever flips back.
func TestVolumePayloadsUnmarshalNatively(t *testing.T) {
	cases := map[string]struct {
		payload         string
		wantNamespaceID *int64
	}{
		"deprecated mount flow, integer namespace_id": {
			payload: `{"id":"vol_zrPm0dmlNKOw3","type":"volumes","attributes":{"name":"test-pery-bs","size_in_gb":32,` +
				`"namespace_id":1,"connector_id":"nqn.2001-07.com.ceph:jv6m5JyZDNLPe-test-pery-bs",` +
				`"initiators":[{"nqn":"nqn.2014-08.org.nvmexpress:uuid:6e839400","mounted_at":"2025-11-17T19:45:11Z"}],"block":null,` +
				`"region":{"site":{"slug":"DAL"}},"project":{"id":"proj_jv6m5JyZDNLPe","slug":"devtools-qa"}}}`,
			wantNamespaceID: func() *int64 { v := int64(1); return &v }(),
		},
		"mapped volume, null namespace_id": {
			payload: `{"id":"vol_x3egaQB77046Q","type":"volumes","attributes":{"name":"chi-file001","size_in_gb":32,` +
				`"namespace_id":null,"connector_id":null,"initiators":null,` +
				`"block":{"status":"mapped","nqn":"nqn.2014-08.sh.latitude:id:sv-dYjv5rEp1Nqp7","nsid":null,"server_id":"sv_dYjv5rEp1Nqp7",` +
				`"storage_network":{"vid":2090,"host_cidr":"10.248.6.4/24","gateway":"10.248.6.1","block_port":4420}},` +
				`"region":{"site":{"slug":"CHI"}},"project":{"id":"proj_XDO7NYlD80Pgw","slug":"new-test-project"}}}`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var volume components.VolumeData
			if err := json.Unmarshal([]byte(tc.payload), &volume); err != nil {
				t.Fatalf("live payload no longer unmarshals: %v", err)
			}
			got := volume.Attributes.NamespaceID
			switch {
			case tc.wantNamespaceID == nil && got != nil:
				t.Errorf("namespace_id = %d, want null", *got)
			case tc.wantNamespaceID != nil && (got == nil || *got != *tc.wantNamespaceID):
				t.Errorf("namespace_id = %v, want %d", got, *tc.wantNamespaceID)
			}
		})
	}
}
