package latitudesh

// TestAccEvent_Mock exercises the latitudesh_event data source against a local
// mock of the Latitude.sh API, injected through the provider's httpClient (the
// same hook latitudesh_virtual_machine's site tests use). It runs under
// TF_ACC without requiring credentials or reaching the real API.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func mockEventsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	envelope := map[string]any{
		"data": []map[string]any{
			{
				"id":   "evt_mock_1",
				"type": "events",
				"attributes": map[string]any{
					"action":     "server.power_on",
					"created_at": "2026-07-01T12:00:00Z",
					"author":     map[string]any{"id": "user_1", "name": "Ada Lovelace", "email": "ada@example.com"},
					"project":    map[string]any{"id": "proj_1", "name": "Prod", "slug": "prod"},
					"target":     map[string]any{"id": "srv_1", "name": "web-01"},
					"properties": map[string]any{},
				},
			},
		},
		"meta": map[string]any{
			"stats": map[string]any{
				"total": map[string]any{"count": 1},
			},
		},
	}

	w.Header().Set("Content-Type", "application/vnd.api+json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(envelope)
}

func testAccEventConfig() string {
	return `
provider "latitudesh" {
  auth_token = "mock-token"
}

data "latitudesh_event" "test" {
  filter_action = "server.power_on"
}
`
}

func TestAccEvent_Mock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(mockEventsHandler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccEventConfig(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_event.test", "total_count", "1"),
					resource.TestCheckResourceAttr("data.latitudesh_event.test", "events.#", "1"),
					resource.TestCheckResourceAttr("data.latitudesh_event.test", "events.0.id", "evt_mock_1"),
					resource.TestCheckResourceAttr("data.latitudesh_event.test", "events.0.action", "server.power_on"),
					resource.TestCheckResourceAttr("data.latitudesh_event.test", "events.0.author.name", "Ada Lovelace"),
					resource.TestCheckResourceAttr("data.latitudesh_event.test", "events.0.project.slug", "prod"),
					resource.TestCheckResourceAttr("data.latitudesh_event.test", "events.0.target.name", "web-01"),
				),
			},
		},
	})
}
