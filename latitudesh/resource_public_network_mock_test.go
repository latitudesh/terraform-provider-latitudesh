package latitudesh

// Exercises latitudesh_public_network against a local mock of the Latitude.sh
// API (the same httpClient hook the VCR tests use). Runs under TF_ACC without
// credentials or real resources, and pins two live-verified behaviours:
// POST /public_networks resolves project_id by ID only (slugs 404), and
// import must repopulate site/size so the post-import plan is empty.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

type mockPublicNetworkAPI struct {
	mu               sync.Mutex
	createdProjectID string // project_id received in the create payload
	projectLookups   int    // GET /projects/{selector} calls
	exists           bool
}

func (m *mockPublicNetworkAPI) envelope() map[string]any {
	return map[string]any{
		"data": map[string]any{
			"id":   "pn_mock_1",
			"type": "public_networks",
			"attributes": map[string]any{
				"ipv4":       "203.0.113.0/29",
				"ipv6":       "2001:db8:1234::/64",
				"size":       29,
				"activated":  false,
				"capacity":   3,
				"ips_used":   0,
				"ips_free":   3,
				"created_at": "2026-09-11T12:00:00Z",
				"project":    map[string]any{"id": "proj_mock_1", "name": "Mock", "slug": "mock-project"},
				"region": map[string]any{
					"id":       "reg_mock_1",
					"name":     "United States",
					"location": map[string]any{"id": "loc_mock_1", "name": "Chicago", "slug": "CHI"},
				},
			},
		},
	}
}

func (m *mockPublicNetworkAPI) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/vnd.api+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (m *mockPublicNetworkAPI) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Body shape the live API returns for an unknown record.
	notFound := map[string]any{"errors": []map[string]any{{
		"code": "not_found", "status": "404", "title": "Error", "detail": "Specified Record Not Found",
	}}}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/projects/mock-project":
		m.projectLookups++
		m.writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
			"id":         "proj_mock_1",
			"type":       "projects",
			"attributes": map[string]any{"name": "Mock", "slug": "mock-project"},
		}})

	case r.Method == http.MethodPost && r.URL.Path == "/public_networks":
		var payload struct {
			Data struct {
				Attributes struct {
					ProjectID string `json:"project_id"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		m.createdProjectID = payload.Data.Attributes.ProjectID
		if m.createdProjectID != "proj_mock_1" {
			// The live endpoint resolves project_id by ID only.
			m.writeJSON(w, http.StatusNotFound, notFound)
			return
		}
		m.exists = true
		m.writeJSON(w, http.StatusCreated, m.envelope())

	case r.Method == http.MethodGet && r.URL.Path == "/public_networks/pn_mock_1":
		if !m.exists {
			m.writeJSON(w, http.StatusNotFound, notFound)
			return
		}
		m.writeJSON(w, http.StatusOK, m.envelope())

	case r.Method == http.MethodDelete && r.URL.Path == "/public_networks/pn_mock_1":
		m.exists = false
		w.WriteHeader(http.StatusNoContent)

	default:
		m.writeJSON(w, http.StatusNotFound, notFound)
	}
}

// TestAccPublicNetwork_ProjectSlugResolvedAndImport: a `project` slug must be
// resolved to its ID before POST while state keeps the configured slug; import
// must repopulate site/size from the API so ImportStateVerify passes.
func TestAccPublicNetwork_ProjectSlugResolvedAndImport(t *testing.T) {
	mock := &mockPublicNetworkAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resourceName := "latitudesh_public_network.test_item"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: `
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_public_network" "test_item" {
  project = "mock-project"
  site    = "CHI"
  size    = 29
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "id", "pn_mock_1"),
					resource.TestCheckResourceAttr(resourceName, "project", "mock-project"),
					resource.TestCheckResourceAttr(resourceName, "site", "CHI"),
					resource.TestCheckResourceAttr(resourceName, "size", "29"),
					resource.TestCheckResourceAttr(resourceName, "ipv4", "203.0.113.0/29"),
					resource.TestCheckResourceAttr(resourceName, "region_slug", "CHI"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.createdProjectID != "proj_mock_1" {
		t.Fatalf("create payload project_id = %q, want the resolved ID proj_mock_1", mock.createdProjectID)
	}
	if mock.projectLookups == 0 {
		t.Fatal("expected the slug to be resolved via GET /projects/{slug}; no lookup happened")
	}
}
