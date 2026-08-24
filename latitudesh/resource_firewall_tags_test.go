package latitudesh

// These tests exercise the firewall `tags` attribute against a local mock of
// the Latitude.sh API, injected through the provider's httpClient (the same
// hook the VCR tests use). They run under TF_ACC without requiring credentials
// or creating real resources.
//
// Unlike server tags, the firewall API returns tags on read, so the provider
// round-trips them: they are sent on create/update and read back on refresh and
// import. Because the payload uses `omitempty`, an empty set cannot be sent, so
// the attribute is Optional+Computed and removing every tag leaves them in place
// rather than clearing them.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const (
	testFirewallMockID      = "fw_mock_1"
	testFirewallMockName    = "test-fw-tags"
	testFirewallMockProject = "proj_mock_1"
)

type mockFirewallAPI struct {
	mu             sync.Mutex
	createdTags    []string // tags in the last create payload (nil = key absent)
	lastUpdateTags []string // tags in the last update payload (nil = key absent)
	tags           []string // tags the mock currently reports on reads
	exists         bool
	deleted        bool
}

func (m *mockFirewallAPI) tagObjects() []map[string]any {
	out := make([]map[string]any, 0, len(m.tags))
	for _, id := range m.tags {
		out = append(out, map[string]any{"id": id})
	}
	return out
}

func (m *mockFirewallAPI) firewallAttributes() map[string]any {
	return map[string]any{
		"name":    testFirewallMockName,
		"project": map[string]any{"id": testFirewallMockProject, "slug": testFirewallMockProject},
		"tags":    m.tagObjects(),
		"rules": []map[string]any{
			{"from": "192.168.1.1", "to": "192.168.1.2", "port": "80", "protocol": "TCP"},
		},
	}
}

func (m *mockFirewallAPI) firewallEnvelope() map[string]any {
	return map[string]any{
		"data": map[string]any{
			"id":         testFirewallMockID,
			"type":       "firewalls",
			"attributes": m.firewallAttributes(),
		},
	}
}

// decodeTags pulls data.attributes.tags out of a request body, distinguishing an
// absent key (nil) from an empty/populated array.
func decodeTags(r *http.Request) ([]string, bool) {
	var payload struct {
		Data struct {
			Attributes struct {
				Tags *[]string `json:"tags"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return nil, false
	}
	if payload.Data.Attributes.Tags == nil {
		return nil, false
	}
	return *payload.Data.Attributes.Tags, true
}

func (m *mockFirewallAPI) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	writeJSON := func(status int, body any) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
		if tags, ok := decodeTags(r); ok {
			m.createdTags = tags
			m.tags = tags
		} else {
			m.createdTags = nil
		}
		m.exists = true
		writeJSON(http.StatusCreated, m.firewallEnvelope())

	case r.Method == http.MethodGet && r.URL.Path == "/firewalls":
		writeJSON(http.StatusOK, map[string]any{
			"data": []map[string]any{
				{
					"id":         testFirewallMockID,
					"type":       "firewalls",
					"attributes": m.firewallAttributes(),
				},
			},
		})

	case r.Method == http.MethodGet && r.URL.Path == "/firewalls/"+testFirewallMockID:
		if !m.exists || m.deleted {
			writeJSON(http.StatusNotFound, map[string]any{"errors": []any{map[string]any{"status": "404"}}})
			return
		}
		writeJSON(http.StatusOK, m.firewallEnvelope())

	case r.Method == http.MethodPatch && r.URL.Path == "/firewalls/"+testFirewallMockID:
		if tags, ok := decodeTags(r); ok {
			m.lastUpdateTags = tags
			m.tags = tags
		} else {
			m.lastUpdateTags = nil
			// omitempty dropped the field: the API leaves tags unchanged.
		}
		writeJSON(http.StatusOK, m.firewallEnvelope())

	case r.Method == http.MethodDelete && r.URL.Path == "/firewalls/"+testFirewallMockID:
		m.deleted = true
		w.WriteHeader(http.StatusNoContent)

	default:
		writeJSON(http.StatusNotFound, map[string]any{"errors": []any{map[string]any{"status": "404"}}})
	}
}

func testAccFirewallTagsConfig(tagsLine string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_firewall" "test_item" {
  name    = %q
  project = %q
%s
  rules {
    from     = "192.168.1.1"
    to       = "192.168.1.2"
    port     = "80"
    protocol = "TCP"
  }
}
`, testFirewallMockName, testFirewallMockProject, tagsLine)
}

func testAccCheckMockFirewallDestroyed(m *mockFirewallAPI) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.exists && !m.deleted {
			return fmt.Errorf("mock firewall still exists after destroy")
		}
		return nil
	}
}

// Setting tags forwards them on create, stores them in state, and — because the
// API echoes them back — still produces an empty plan on the next run. Changing
// the set forwards the new set on update. Removing every tag must NOT clear them
// (the omitempty payload can't express an empty set) and must not churn the plan.
func TestAccFirewall_TagsLifecycle(t *testing.T) {
	mock := &mockFirewallAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockFirewallDestroyed(mock),
		Steps: []resource.TestStep{
			// Create with two tags.
			{
				Config: testAccFirewallTagsConfig("  tags = [\"tag_a\", \"tag_b\"]"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_firewall.test_item", "tags.#", "2"),
					resource.TestCheckResourceAttr("latitudesh_firewall.test_item", "tags.0", "tag_a"),
					resource.TestCheckResourceAttr("latitudesh_firewall.test_item", "tags.1", "tag_b"),
					func(s *terraform.State) error {
						mock.mu.Lock()
						defer mock.mu.Unlock()
						if got := strings.Join(mock.createdTags, ","); got != "tag_a,tag_b" {
							return fmt.Errorf("create payload tags = %q, want %q", got, "tag_a,tag_b")
						}
						return nil
					},
				),
			},
			{
				Config:   testAccFirewallTagsConfig("  tags = [\"tag_a\", \"tag_b\"]"),
				PlanOnly: true,
			},
			// Change the set to a single tag: update must forward exactly it.
			{
				Config: testAccFirewallTagsConfig("  tags = [\"tag_a\"]"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_firewall.test_item", "tags.#", "1"),
					resource.TestCheckResourceAttr("latitudesh_firewall.test_item", "tags.0", "tag_a"),
					func(s *terraform.State) error {
						mock.mu.Lock()
						defer mock.mu.Unlock()
						if got := strings.Join(mock.lastUpdateTags, ","); got != "tag_a" {
							return fmt.Errorf("update payload tags = %q, want %q", got, "tag_a")
						}
						return nil
					},
				),
			},
			{
				Config:   testAccFirewallTagsConfig("  tags = [\"tag_a\"]"),
				PlanOnly: true,
			},
			// Remove tags entirely: the API cannot clear them, so state keeps the
			// prior tag and the plan stays empty (no perpetual diff).
			{
				Config:   testAccFirewallTagsConfig(""),
				PlanOnly: true,
			},
		},
	})
}

// With no tags configured on create, the payload must omit the tags key and the
// attribute settles to an empty list (Computed), yielding an empty follow-up plan.
func TestAccFirewall_TagsOmittedByDefault(t *testing.T) {
	mock := &mockFirewallAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockFirewallDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallTagsConfig(""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_firewall.test_item", "tags.#", "0"),
					func(s *terraform.State) error {
						mock.mu.Lock()
						defer mock.mu.Unlock()
						if mock.createdTags != nil {
							return fmt.Errorf("create payload included tags = %v; want the key omitted", mock.createdTags)
						}
						return nil
					},
				),
			},
			{
				Config:   testAccFirewallTagsConfig(""),
				PlanOnly: true,
			},
		},
	})
}
