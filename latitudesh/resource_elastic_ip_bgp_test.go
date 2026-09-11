package latitudesh

// These tests exercise latitudesh_elastic_ip_bgp against a local mock of the
// Latitude.sh API, injected through the provider's httpClient (the same hook the
// VCR tests use). They run under TF_ACC without credentials and without creating
// real resources — which matters here, because a live run needs a metal server
// deployed with bgp_ready in a BGP-enabled site.
//
// The mock reproduces the three API behaviours the resource is built around:
//
//   - POST /elastic_ips rejects server_id in bgp mode and takes site instead;
//   - a session is accepted asynchronously (pending, then active);
//   - DELETE /elastic_ips answers 422 ELASTIC_IP_HAS_BGP_SESSIONS while any
//     session is still up, so the sessions have to be closed first.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const (
	testBgpEIPID      = "ueip_mock_1"
	testBgpEIPAddress = "152.236.12.86"
	testBgpEIPSite    = "DAL"
)

type mockBgpSession struct {
	id          string
	serverID    string
	serverIP    string
	peerAddress string
	// polled records whether the session has been read since it was accepted.
	// The first read reports `pending`, later ones `active`, so the resource's
	// wait loop is actually exercised.
	polled  bool
	removed bool
}

type mockBgpEIPAPI struct {
	mu sync.Mutex

	createAttributes map[string]any
	created          bool
	released         bool

	// omitCreateID reproduces a create that succeeds without returning an id.
	// The address is allocated and billable, so the provider has to find it.
	omitCreateID bool

	// omitCreateAttributes additionally drops the attributes, so the response
	// identifies the allocation in no way at all and only the listing diff is
	// left.
	omitCreateAttributes bool

	// extraListedID adds a second BGP address to the project listing, as a
	// concurrent allocation would. Recovery must refuse to guess between them.
	extraListedID string

	// preExisting are addresses that were already in the project before this
	// apply. With pageCap set they land on later pages, where a snapshot that
	// only read page one would miss them.
	preExisting []string

	// failSessionList makes the session listing fail, so the recovery refresh
	// cannot rebuild state and the provider has to fall back on what it recorded
	// as it went. failListAfterStuckOpen arms it at the right moment — the moment
	// the stuck session is accepted — so the refresh that precedes the update
	// still works.
	failSessionList        bool
	failListAfterStuckOpen bool

	// failNextGetAfterRelease answers the first read that follows a successful
	// release with a 403, then heals. A provider that swallowed it would keep
	// polling, hit the healed 404 and destroy cleanly — so the test only passes
	// if the error is surfaced on the spot.
	failNextGetAfterRelease bool

	// stuckServer never lets its session leave `pending`, so the readiness poll
	// runs out the operation deadline after the API already accepted it.
	stuckServer string

	// pageCap is the largest page the mock will serve regardless of the
	// requested page[size], as a real API capping pagination would. Zero means
	// no cap.
	pageCap int

	sessions    map[string]*mockBgpSession
	nextSession int

	// ops is an ordered log of the mutations the provider issued, so a test can
	// assert on sequencing (sessions closed before the address is released,
	// announcers removed before new ones are added).
	ops []string

	// releaseBlocked counts release attempts refused because sessions were still
	// up. Any hit means the provider tried to release too early.
	releaseBlocked int
}

// withFastBgpPolling shrinks the resource's poll interval for the duration of
// one test. The mock answers instantly, so the production 10s interval would be
// nothing but dead time — but it has to be opted into per test, never inferred
// from the deadline, so a short timeout in a real configuration cannot turn into
// a request storm.
func withFastBgpPolling(t *testing.T) {
	t.Helper()
	previous := bgpPollInterval
	bgpPollInterval = 100 * time.Millisecond
	t.Cleanup(func() { bgpPollInterval = previous })
}

func newMockBgpEIPAPI() *mockBgpEIPAPI {
	return &mockBgpEIPAPI{sessions: map[string]*mockBgpSession{}}
}

func (m *mockBgpEIPAPI) liveSessions() []*mockBgpSession {
	out := make([]*mockBgpSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		if !session.removed {
			out = append(out, session)
		}
	}
	return out
}

func (m *mockBgpEIPAPI) elasticIPEnvelope() map[string]any {
	return map[string]any{
		"data": map[string]any{
			"id":   testBgpEIPID,
			"type": "elastic_ips",
			"attributes": map[string]any{
				"address":       testBgpEIPAddress,
				"family":        "IPv4",
				"prefix_length": 32,
				"mode":          "bgp",
				"status":        "active",
				"created_at":    "2026-09-08T00:52:04.801Z",
				"server":        nil,
				"project": map[string]any{
					"id":   "proj_mock_1",
					"name": "mock",
					"slug": "mock",
				},
				"region": map[string]any{
					"id":   "reg_mock_1",
					"name": "United States",
					"location": map[string]any{
						"id":   "loc_mock_1",
						"name": "Dallas",
						"slug": testBgpEIPSite,
					},
				},
			},
		},
	}
}

func (m *mockBgpEIPAPI) sessionEnvelope(session *mockBgpSession) map[string]any {
	status := "active"
	if !session.polled {
		status = "pending"
		session.polled = true
	}
	if session.serverID == m.stuckServer {
		status = "pending"
	}
	return map[string]any{
		"id":   session.id,
		"type": "bgp_sessions",
		"attributes": map[string]any{
			"status":         status,
			"status_message": nil,
			"server_ip":      session.serverIP,
			"peer_address":   session.peerAddress,
			"asn":            65512,
			"created_at":     "2026-09-08T00:53:00.000Z",
			"server": map[string]any{
				"id":               session.serverID,
				"hostname":         session.serverID + ".example",
				"primary_ipv4":     session.serverIP,
				"operating_system": "ubuntu_24_04_x64_lts",
			},
			"region": map[string]any{
				"id":   "reg_mock_1",
				"name": "United States",
				"location": map[string]any{
					"id":   "loc_mock_1",
					"name": "Dallas",
					"slug": testBgpEIPSite,
				},
			},
		},
	}
}

// listedAddresses is the project listing, newest first: the address this apply
// created, then anything that was already there.
func (m *mockBgpEIPAPI) listedAddresses() []map[string]any {
	out := []map[string]any{}
	if m.created && !m.released {
		// extraListedID stands for an address allocated concurrently, just after
		// this one, so it sorts ahead of it in the newest-first listing.
		if m.extraListedID != "" {
			other := m.elasticIPEnvelope()["data"].(map[string]any)
			other["id"] = m.extraListedID
			other["attributes"].(map[string]any)["address"] = "152.236.12.99"
			out = append(out, other)
		}
		out = append(out, m.elasticIPEnvelope()["data"].(map[string]any))
	}
	for i, id := range m.preExisting {
		old := m.elasticIPEnvelope()["data"].(map[string]any)
		old["id"] = id
		old["attributes"].(map[string]any)["address"] = fmt.Sprintf("152.236.13.%d", i+1)
		out = append(out, old)
	}
	return out
}

// listPage serves one page of the listing, honouring page[number] and capping
// page[size] at pageCap the way a real API does.
func (m *mockBgpEIPAPI) listPage(r *http.Request) []map[string]any {
	all := m.listedAddresses()

	size := len(all)
	if requested, err := strconv.Atoi(r.URL.Query().Get("page[size]")); err == nil && requested > 0 {
		size = requested
	}
	if m.pageCap > 0 && size > m.pageCap {
		size = m.pageCap
	}
	if size <= 0 {
		size = len(all)
	}

	number := 1
	if requested, err := strconv.Atoi(r.URL.Query().Get("page[number]")); err == nil && requested > 0 {
		number = requested
	}

	start := (number - 1) * size
	if start >= len(all) {
		return []map[string]any{}
	}
	end := start + size
	if end > len(all) {
		end = len(all)
	}
	return all[start:end]
}

func (m *mockBgpEIPAPI) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/vnd.api+json")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func (m *mockBgpEIPAPI) writeError(w http.ResponseWriter, status int, code, detail string) {
	m.writeJSON(w, status, map[string]any{
		"errors": []map[string]any{{
			"code":   code,
			"status": fmt.Sprintf("%d", status),
			"detail": detail,
		}},
	})
}

func (m *mockBgpEIPAPI) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sessionsPath := "/elastic_ips/" + testBgpEIPID + "/bgp_sessions"

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/elastic_ips":
		var payload struct {
			Data struct {
				Attributes map[string]any `json:"attributes"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		m.createAttributes = payload.Data.Attributes
		m.created = true
		m.ops = append(m.ops, "create_elastic_ip")
		envelope := m.elasticIPEnvelope()
		if m.omitCreateID {
			envelope["data"].(map[string]any)["id"] = nil
		}
		if m.omitCreateAttributes {
			delete(envelope["data"].(map[string]any), "attributes")
		}
		m.writeJSON(w, http.StatusAccepted, envelope)

	case r.Method == http.MethodGet && r.URL.Path == "/elastic_ips":
		m.writeJSON(w, http.StatusOK, map[string]any{"data": m.listPage(r)})

	case r.Method == http.MethodGet && r.URL.Path == "/elastic_ips/"+testBgpEIPID:
		if m.released && m.failNextGetAfterRelease {
			m.failNextGetAfterRelease = false
			m.writeError(w, http.StatusForbidden, "FORBIDDEN", "token expired")
			return
		}
		if !m.created || m.released {
			m.writeError(w, http.StatusNotFound, "NOT_FOUND", "not found")
			return
		}
		m.writeJSON(w, http.StatusOK, m.elasticIPEnvelope())

	case r.Method == http.MethodDelete && r.URL.Path == "/elastic_ips/"+testBgpEIPID:
		if len(m.liveSessions()) > 0 {
			m.releaseBlocked++
			m.writeError(w, http.StatusUnprocessableEntity, "ELASTIC_IP_HAS_BGP_SESSIONS",
				"This elastic IP still announces BGP sessions. Remove them before releasing it.")
			return
		}
		m.released = true
		m.ops = append(m.ops, "release_elastic_ip")
		w.WriteHeader(http.StatusNoContent)

	case r.Method == http.MethodGet && r.URL.Path == sessionsPath:
		if m.failSessionList {
			m.writeError(w, http.StatusInternalServerError, "INTERNAL", "sessions unavailable")
			return
		}
		data := make([]map[string]any, 0, len(m.sessions))
		for _, session := range m.liveSessions() {
			data = append(data, m.sessionEnvelope(session))
		}
		m.writeJSON(w, http.StatusOK, map[string]any{"data": data})

	case r.Method == http.MethodPost && r.URL.Path == sessionsPath:
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
		serverID := payload.Data.Attributes.ServerID
		for _, existing := range m.liveSessions() {
			if existing.serverID == serverID {
				m.writeError(w, http.StatusUnprocessableEntity, "CONFLICT",
					"a BGP session already exists for this server")
				return
			}
		}
		m.nextSession++
		session := &mockBgpSession{
			id:          fmt.Sprintf("bgps_mock_%d", m.nextSession),
			serverID:    serverID,
			serverIP:    fmt.Sprintf("206.223.227.%d", 80+m.nextSession*4),
			peerAddress: fmt.Sprintf("206.223.227.%d", 81+m.nextSession*4),
		}
		m.sessions[session.id] = session
		m.ops = append(m.ops, "open_session:"+serverID)
		if m.failListAfterStuckOpen && serverID == m.stuckServer {
			m.failSessionList = true
		}
		m.writeJSON(w, http.StatusAccepted, map[string]any{"data": m.sessionEnvelope(session)})

	case strings.HasPrefix(r.URL.Path, sessionsPath+"/"):
		sessionID := strings.TrimPrefix(r.URL.Path, sessionsPath+"/")
		session, ok := m.sessions[sessionID]
		if !ok || session.removed {
			m.writeError(w, http.StatusNotFound, "NOT_FOUND", "BGP session not found")
			return
		}
		switch r.Method {
		case http.MethodGet:
			m.writeJSON(w, http.StatusOK, map[string]any{"data": m.sessionEnvelope(session)})
		case http.MethodDelete:
			session.removed = true
			m.ops = append(m.ops, "close_session:"+session.serverID)
			w.WriteHeader(http.StatusAccepted)
		default:
			m.writeError(w, http.StatusNotFound, "NOT_FOUND", "not found")
		}

	default:
		m.writeError(w, http.StatusNotFound, "NOT_FOUND", "not found")
	}
}

func (m *mockBgpEIPAPI) snapshotOps() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.ops...)
}

func testAccElasticIPBgpConfig(serverIDs string) string {
	return testAccElasticIPBgpConfigWithTimeouts(serverIDs, "")
}

func testAccElasticIPBgpConfigWithTimeouts(serverIDs, timeoutsBlock string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_elastic_ip_bgp" "test_item" {
  project    = "proj_mock_1"
  site       = %q
  server_ids = %s
%s
}
`, testBgpEIPSite, serverIDs, timeoutsBlock)
}

// testAccCheckMockBgpEIPReleased asserts the address was released, and that it
// was never attempted while a session was still up.
func testAccCheckMockBgpEIPReleased(m *mockBgpEIPAPI) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.created && !m.released {
			return fmt.Errorf("mock elastic IP still allocated after destroy")
		}
		if m.releaseBlocked > 0 {
			return fmt.Errorf("provider tried to release the address %d time(s) while sessions were still up", m.releaseBlocked)
		}
		return nil
	}
}

// Create must allocate the address with mode=bgp and site (never server_id,
// which the API rejects in this mode, and never server_ids, which would open the
// sessions inside the allocate call), then open one session per announcer.
func TestAccElasticIPBgp_CreateOpensOneSessionPerServer(t *testing.T) {
	withFastBgpPolling(t)
	mock := newMockBgpEIPAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resourceName := "latitudesh_elastic_ip_bgp.test_item"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBgpEIPReleased(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccElasticIPBgpConfig(`["sv_b", "sv_a"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "id", testBgpEIPID),
					resource.TestCheckResourceAttr(resourceName, "address", testBgpEIPAddress),
					resource.TestCheckResourceAttr(resourceName, "status", "active"),
					resource.TestCheckResourceAttr(resourceName, "prefix_length", "32"),
					resource.TestCheckResourceAttr(resourceName, "server_ids.#", "2"),
					resource.TestCheckTypeSetElemAttr(resourceName, "server_ids.*", "sv_a"),
					resource.TestCheckTypeSetElemAttr(resourceName, "server_ids.*", "sv_b"),
					// bgp_sessions is ordered by server_id, so the indices are stable.
					resource.TestCheckResourceAttr(resourceName, "bgp_sessions.#", "2"),
					resource.TestCheckResourceAttr(resourceName, "bgp_sessions.0.server_id", "sv_a"),
					resource.TestCheckResourceAttr(resourceName, "bgp_sessions.0.asn", "65512"),
					resource.TestCheckResourceAttrSet(resourceName, "bgp_sessions.0.peer_address"),
					resource.TestCheckResourceAttr(resourceName, "bgp_sessions.0.status", "active"),
					resource.TestCheckResourceAttr(resourceName, "bgp_sessions.1.server_id", "sv_b"),
					func(s *terraform.State) error {
						mock.mu.Lock()
						defer mock.mu.Unlock()
						if got := mock.createAttributes["mode"]; got != "bgp" {
							return fmt.Errorf("create payload mode = %v, want bgp", got)
						}
						if got := mock.createAttributes["site"]; got != testBgpEIPSite {
							return fmt.Errorf("create payload site = %v, want %s", got, testBgpEIPSite)
						}
						if _, ok := mock.createAttributes["server_id"]; ok {
							return fmt.Errorf("create payload carried server_id, which the API rejects in bgp mode")
						}
						if _, ok := mock.createAttributes["server_ids"]; ok {
							return fmt.Errorf("create payload carried server_ids; sessions must be opened one by one after allocation")
						}
						return nil
					},
				),
			},
			{
				Config:   testAccElasticIPBgpConfig(`["sv_b", "sv_a"]`),
				PlanOnly: true,
			},
		},
	})
}

// A VIP with no announcers is a valid state: the address is allocated and simply
// not announced yet.
func TestAccElasticIPBgp_AllocatesWithoutAnnouncers(t *testing.T) {
	withFastBgpPolling(t)
	mock := newMockBgpEIPAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resourceName := "latitudesh_elastic_ip_bgp.test_item"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBgpEIPReleased(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccElasticIPBgpConfig(`[]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "server_ids.#", "0"),
					resource.TestCheckResourceAttr(resourceName, "bgp_sessions.#", "0"),
					resource.TestCheckResourceAttr(resourceName, "address", testBgpEIPAddress),
				),
			},
		},
	})
}

// Changing server_ids must open and close sessions in place — the address is
// never reallocated — and must close before it opens, so swapping an announcer
// at the per-IP session limit does not fail on the add.
func TestAccElasticIPBgp_UpdateSwapsAnnouncersRemovalFirst(t *testing.T) {
	withFastBgpPolling(t)
	mock := newMockBgpEIPAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resourceName := "latitudesh_elastic_ip_bgp.test_item"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBgpEIPReleased(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccElasticIPBgpConfig(`["sv_a", "sv_b"]`),
				Check:  resource.TestCheckResourceAttr(resourceName, "server_ids.#", "2"),
			},
			{
				Config: testAccElasticIPBgpConfig(`["sv_b", "sv_c"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "id", testBgpEIPID),
					resource.TestCheckResourceAttr(resourceName, "server_ids.#", "2"),
					resource.TestCheckTypeSetElemAttr(resourceName, "server_ids.*", "sv_b"),
					resource.TestCheckTypeSetElemAttr(resourceName, "server_ids.*", "sv_c"),
					resource.TestCheckResourceAttr(resourceName, "bgp_sessions.0.server_id", "sv_b"),
					resource.TestCheckResourceAttr(resourceName, "bgp_sessions.1.server_id", "sv_c"),
					func(s *terraform.State) error {
						ops := mock.snapshotOps()
						closeAt, openAt := -1, -1
						for i, op := range ops {
							if op == "close_session:sv_a" {
								closeAt = i
							}
							if op == "open_session:sv_c" {
								openAt = i
							}
						}
						if closeAt < 0 {
							return fmt.Errorf("sv_a session was never closed; ops = %v", ops)
						}
						if openAt < 0 {
							return fmt.Errorf("sv_c session was never opened; ops = %v", ops)
						}
						if closeAt > openAt {
							return fmt.Errorf("removal must run before addition; ops = %v", ops)
						}
						for _, op := range ops {
							if op == "release_elastic_ip" {
								return fmt.Errorf("the address was released during an in-place update; ops = %v", ops)
							}
						}
						return nil
					},
				),
			},
		},
	})
}

// Destroy must close every session before releasing the address: the API
// refuses the release while any is still up.
func TestAccElasticIPBgp_DestroyClosesSessionsFirst(t *testing.T) {
	withFastBgpPolling(t)
	mock := newMockBgpEIPAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy: func(s *terraform.State) error {
			if err := testAccCheckMockBgpEIPReleased(mock)(s); err != nil {
				return err
			}
			ops := mock.snapshotOps()
			releaseAt := -1
			for i, op := range ops {
				if op == "release_elastic_ip" {
					releaseAt = i
				}
			}
			if releaseAt < 0 {
				return fmt.Errorf("the address was never released; ops = %v", ops)
			}
			for i, op := range ops {
				if strings.HasPrefix(op, "close_session:") && i > releaseAt {
					return fmt.Errorf("a session was closed after the release; ops = %v", ops)
				}
			}
			for _, want := range []string{"close_session:sv_a", "close_session:sv_b"} {
				found := false
				for _, op := range ops {
					if op == want {
						found = true
					}
				}
				if !found {
					return fmt.Errorf("%s missing; ops = %v", want, ops)
				}
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: testAccElasticIPBgpConfig(`["sv_a", "sv_b"]`),
				Check:  resource.TestCheckResourceAttr("latitudesh_elastic_ip_bgp.test_item", "bgp_sessions.#", "2"),
			},
		},
	})
}

// A create that answers 200 without an id still allocated a billable address.
// The response still names the address, which is unique, so the provider must
// find it rather than leave it orphaned outside state.
func TestAccElasticIPBgp_RecoversIDByAddressWhenCreateOmitsIt(t *testing.T) {
	withFastBgpPolling(t)
	mock := newMockBgpEIPAPI()
	mock.omitCreateID = true
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resourceName := "latitudesh_elastic_ip_bgp.test_item"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBgpEIPReleased(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccElasticIPBgpConfig(`["sv_a"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "id", testBgpEIPID),
					resource.TestCheckResourceAttr(resourceName, "address", testBgpEIPAddress),
					resource.TestCheckResourceAttr(resourceName, "bgp_sessions.0.server_id", "sv_a"),
				),
			},
		},
	})
}

// A create response that identifies the allocation in no way at all leaves
// nothing to match on. Diffing the project listing would be a guess — an address
// allocated concurrently is indistinguishable from ours — so the provider must
// refuse rather than adopt one.
func TestAccElasticIPBgp_RefusesWhenCreateIdentifiesNothing(t *testing.T) {
	withFastBgpPolling(t)
	mock := newMockBgpEIPAPI()
	mock.omitCreateID = true
	mock.omitCreateAttributes = true
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config:      testAccElasticIPBgpConfig(`["sv_a"]`),
				ExpectError: regexp.MustCompile(`created without an identifier`),
			},
		},
	})
}

// The project listing is paginated, and an address allocated concurrently sorts
// ahead of this one. Looking the address up has to walk past the first page to
// find it — stopping early would fail the create and orphan a billable /32.
func TestAccElasticIPBgp_FindsAddressBeyondTheFirstPage(t *testing.T) {
	withFastBgpPolling(t)
	mock := newMockBgpEIPAPI()
	mock.omitCreateID = true
	mock.extraListedID = "ueip_mock_concurrent"
	mock.preExisting = []string{"ueip_mock_old_1", "ueip_mock_old_2"}
	mock.pageCap = 1
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resourceName := "latitudesh_elastic_ip_bgp.test_item"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBgpEIPReleased(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccElasticIPBgpConfig(`["sv_a"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "id", testBgpEIPID),
					resource.TestCheckResourceAttr(resourceName, "address", testBgpEIPAddress),
				),
			},
		},
	})
}

// A session the API accepted exists even if the readiness poll then runs out the
// clock. What must not happen is the next apply trying to open it again: the API
// answers 422 CONFLICT for a server that already has one, and the resource would
// never converge. The third step asserts exactly that — sv_b is opened once
// across the whole run, and the mock answers CONFLICT if it is not.
//
// Two mechanisms keep that true and this covers them end to end rather than
// isolating either: the provider records a session as soon as it is accepted,
// and Read rebuilds server_ids from the live sessions on the next refresh.
func TestAccElasticIPBgp_TimedOutSessionIsStillRecorded(t *testing.T) {
	withFastBgpPolling(t)
	mock := newMockBgpEIPAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resourceName := "latitudesh_elastic_ip_bgp.test_item"
	shortUpdate := "  timeouts = {\n    update = \"5s\"\n  }"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBgpEIPReleased(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccElasticIPBgpConfig(`["sv_a"]`),
				Check:  resource.TestCheckResourceAttr(resourceName, "server_ids.#", "1"),
			},
			{
				PreConfig: func() {
					mock.mu.Lock()
					defer mock.mu.Unlock()
					mock.stuckServer = "sv_b"
				},
				Config:      testAccElasticIPBgpConfigWithTimeouts(`["sv_a", "sv_b"]`, shortUpdate),
				ExpectError: regexp.MustCompile(`did not become active`),
			},
			{
				PreConfig: func() {
					mock.mu.Lock()
					defer mock.mu.Unlock()
					mock.stuckServer = ""
				},
				Config: testAccElasticIPBgpConfigWithTimeouts(`["sv_a", "sv_b"]`, shortUpdate),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "server_ids.#", "2"),
					resource.TestCheckTypeSetElemAttr(resourceName, "server_ids.*", "sv_b"),
					func(s *terraform.State) error {
						opens := 0
						for _, op := range mock.snapshotOps() {
							if op == "open_session:sv_b" {
								opens++
							}
						}
						if opens != 1 {
							return fmt.Errorf("sv_b session was opened %d times, want 1 — the timed-out session was not recorded", opens)
						}
						return nil
					},
				),
			},
		},
	})
}

// The same timeout with the session listing failing underneath it, so the
// recovery refresh cannot rebuild state either. The apply must still fail with
// the timeout (not a hang: a read that retried on the provider-wide five-minute
// budget used to sit there), write state, and leave the resource reconcilable —
// sv_b is still opened exactly once across the run.
func TestAccElasticIPBgp_TimedOutSessionSurvivesFailedRefresh(t *testing.T) {
	withFastBgpPolling(t)
	mock := newMockBgpEIPAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resourceName := "latitudesh_elastic_ip_bgp.test_item"
	shortUpdate := "  timeouts = {\n    update = \"5s\"\n  }"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBgpEIPReleased(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccElasticIPBgpConfig(`["sv_a"]`),
				Check:  resource.TestCheckResourceAttr(resourceName, "server_ids.#", "1"),
			},
			{
				PreConfig: func() {
					mock.mu.Lock()
					defer mock.mu.Unlock()
					mock.stuckServer = "sv_b"
					mock.failListAfterStuckOpen = true
				},
				Config:      testAccElasticIPBgpConfigWithTimeouts(`["sv_a", "sv_b"]`, shortUpdate),
				ExpectError: regexp.MustCompile(`did not become active`),
			},
			{
				PreConfig: func() {
					mock.mu.Lock()
					defer mock.mu.Unlock()
					mock.stuckServer = ""
					mock.failSessionList = false
					mock.failListAfterStuckOpen = false
				},
				Config: testAccElasticIPBgpConfigWithTimeouts(`["sv_a", "sv_b"]`, shortUpdate),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "server_ids.#", "2"),
					func(s *terraform.State) error {
						opens := 0
						for _, op := range mock.snapshotOps() {
							if op == "open_session:sv_b" {
								opens++
							}
						}
						if opens != 1 {
							return fmt.Errorf("sv_b session was opened %d times, want 1 — the accepted session was not recorded", opens)
						}
						return nil
					},
				),
			},
		},
	})
}

// Waiting for the address to disappear must not treat every non-404 as "not gone
// yet". An expired token or any other 4xx will still be there when the timeout
// expires, and reporting it as "the address is still present" sends the reader
// after the wrong problem.
func TestAccElasticIPBgp_DestroySurfacesPollingErrors(t *testing.T) {
	withFastBgpPolling(t)
	mock := newMockBgpEIPAPI()
	mock.failNextGetAfterRelease = true
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBgpEIPReleased(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccElasticIPBgpConfig(`["sv_a"]`),
				Check:  resource.TestCheckResourceAttr("latitudesh_elastic_ip_bgp.test_item", "server_ids.#", "1"),
			},
			{
				Config:      testAccElasticIPBgpConfig(`["sv_a"]`),
				Destroy:     true,
				ExpectError: regexp.MustCompile(`poll the release of the BGP Elastic IP`),
			},
		},
	})
}
