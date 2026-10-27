package latitudesh

// These tests exercise deploy options of latitudesh_server against a local mock
// of the Latitude.sh API, injected through the provider's httpClient
// (the same hook the VCR tests use). They run under TF_ACC without credentials
// or real resources.
//
//   - persistent_netboot is sent on create and on reinstall but kept from config,
//     never refreshed, the same way ipxe is.
//   - raid-10 in disk_layout has to survive the create payload and the
//     deploy_config read. Before SDK v1.21.1, DeployConfigRaidLevel was a closed
//     enum without raid-10, so reading such a server failed refresh outright.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const (
	testServerDeployHostname = "test-deploy-config"
	testServerDeployIpxe     = "https://example.test/boot.ipxe"
)

type mockDeployConfigServerAPI struct {
	mu             sync.Mutex
	os             string
	exists         bool
	deleted        bool
	createAttrs    map[string]any   // attributes of the create payload
	reinstalls     []map[string]any // attributes of each reinstall payload
	deployConfig   map[string]any   // attributes served by GET deploy_config
	deployingPolls int              // GETs left that report "deploying"

	// Public network attachment, reported by GET /servers/{id} like the API.
	publicNetworkID   string // network the server is attached onto ("" = none)
	attachOnCreate    string // attach onto this network even if the payload did not ask (out-of-band attach)
	hidePublicNetwork bool   // omit public_network from GET, as the API does mid-setup
	creates           int
	deletes           int
}

func (m *mockDeployConfigServerAPI) serverEnvelope(status string) map[string]any {
	var publicNetwork any
	if m.publicNetworkID != "" && !m.hidePublicNetwork {
		publicNetwork = map[string]any{"id": m.publicNetworkID, "ipv4": "198.51.100.8/29"}
	}
	return map[string]any{
		"data": map[string]any{
			"id":   testServerMockID,
			"type": "servers",
			"attributes": map[string]any{
				"public_network":   publicNetwork,
				"hostname":         testServerDeployHostname,
				"status":           status,
				"locked":           false,
				"primary_ipv4":     "203.0.113.40",
				"operating_system": map[string]any{"slug": m.os},
				"plan": map[string]any{
					"id":      "plan_mock_1",
					"slug":    testServerMockPlan,
					"billing": "monthly",
				},
			},
		},
	}
}

// decodeAttributes reads data.attributes from a JSON:API request body.
func decodeAttributes(r *http.Request) (map[string]any, error) {
	var payload struct {
		Data struct {
			Attributes map[string]any `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Data.Attributes, nil
}

func (m *mockDeployConfigServerAPI) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	w.Header().Set("Content-Type", "application/vnd.api+json")
	serverPath := "/servers/" + testServerMockID

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/servers":
		attrs, err := decodeAttributes(r)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		m.createAttrs = attrs
		m.creates++
		m.deleted = false
		m.os, _ = attrs["operating_system"].(string)
		m.publicNetworkID = m.attachOnCreate
		if attached, _ := attrs["public_network"].(bool); attached {
			m.publicNetworkID, _ = attrs["public_network_id"].(string)
		}
		// Like the API, deploy_config stores what the deploy carried, with
		// null for options the request left out.
		m.deployConfig = map[string]any{
			"operating_system":   m.os,
			"hostname":           testServerDeployHostname,
			"ssh_keys":           []any{},
			"persistent_netboot": attrs["persistent_netboot"],
			"disk_layout":        attrs["disk_layout"],
		}
		m.exists = true
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(m.serverEnvelope("on"))

	case r.Method == http.MethodPost && r.URL.Path == serverPath+"/reinstall":
		attrs, err := decodeAttributes(r)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		m.reinstalls = append(m.reinstalls, attrs)
		if v, ok := attrs["persistent_netboot"]; ok {
			m.deployConfig["persistent_netboot"] = v
		}
		// One "deploying" reading gives the reinstall wait the transition it
		// requires before it accepts "on".
		m.deployingPolls = 1
		// The SDK treats only 201 as success on this route.
		w.WriteHeader(http.StatusCreated)

	case r.Method == http.MethodGet && r.URL.Path == serverPath:
		if !m.exists || m.deleted {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		status := "on"
		if m.deployingPolls > 0 {
			m.deployingPolls--
			status = "deploying"
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.serverEnvelope(status))

	case r.Method == http.MethodPatch && r.URL.Path == serverPath:
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.serverEnvelope("on"))

	case r.Method == http.MethodGet && r.URL.Path == serverPath+"/deploy_config":
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"id":         testServerMockID,
				"type":       "deploy_config",
				"attributes": m.deployConfig,
			},
		})

	case r.Method == http.MethodDelete && r.URL.Path == serverPath:
		m.deleted = true
		m.deletes++
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

// newDeployConfigServerMock starts the mock and shortens the status poll, so the
// reinstall wait costs milliseconds instead of a 30s sleep.
func newDeployConfigServerMock(t *testing.T) (*mockDeployConfigServerAPI, *httptest.Server) {
	t.Helper()

	mock := &mockDeployConfigServerAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	previous := serverPollInterval
	serverPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { serverPollInterval = previous })

	return mock, server
}

func testAccServerDeployConfig(operatingSystem, extra string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_server" "test_item" {
  hostname         = %q
  plan             = %q
  site             = "ASH"
  operating_system = %q
  project          = "proj_mock_1"
%s
}
`, testServerDeployHostname, testServerMockPlan, operatingSystem, extra)
}

func testAccServerIpxeConfig(extra string) string {
	return testAccServerDeployConfig("ipxe", fmt.Sprintf("  ipxe             = %q\n%s", testServerDeployIpxe, extra))
}

func testAccCheckMockDeployConfigServerDestroyed(m *mockDeployConfigServerAPI) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.exists && !m.deleted {
			return fmt.Errorf("mock server still exists after destroy")
		}
		return nil
	}
}

// checkCreatePersistentNetboot asserts the persistent_netboot value of the
// create payload. want == nil means the key must be absent.
func checkCreatePersistentNetboot(m *mockDeployConfigServerAPI, want *bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		got, ok := m.createAttrs["persistent_netboot"]
		if want == nil {
			if ok {
				return fmt.Errorf("create payload included persistent_netboot = %v; want it omitted", got)
			}
			return nil
		}
		if got != *want {
			return fmt.Errorf("create payload persistent_netboot = %v (present: %v), want %v", got, ok, *want)
		}
		return nil
	}
}

// checkLastReinstall asserts the number of reinstalls so far and the
// persistent_netboot value of the last one.
func checkLastReinstall(m *mockDeployConfigServerAPI, wantCount int, wantNetboot bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if len(m.reinstalls) != wantCount {
			return fmt.Errorf("got %d reinstall(s), want %d", len(m.reinstalls), wantCount)
		}
		got, ok := m.reinstalls[wantCount-1]["persistent_netboot"]
		if !ok || got != wantNetboot {
			return fmt.Errorf("reinstall payload persistent_netboot = %v (present: %v), want %v", got, ok, wantNetboot)
		}
		return nil
	}
}

func TestAccServer_PersistentNetbootSentOnCreateAndPreserved(t *testing.T) {
	mock, server := newDeployConfigServerMock(t)
	enabled := true

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockDeployConfigServerDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccServerIpxeConfig(`  persistent_netboot = true`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_server.test_item", "persistent_netboot", "true"),
					checkCreatePersistentNetboot(mock, &enabled),
				),
			},
			{
				Config:   testAccServerIpxeConfig(`  persistent_netboot = true`),
				PlanOnly: true,
			},
		},
	})
}

func TestAccServer_PersistentNetbootOmittedByDefault(t *testing.T) {
	mock, server := newDeployConfigServerMock(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockDeployConfigServerDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccServerIpxeConfig(""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("latitudesh_server.test_item", "persistent_netboot"),
					checkCreatePersistentNetboot(mock, nil),
				),
			},
			{
				Config:   testAccServerIpxeConfig(""),
				PlanOnly: true,
			},
		},
	})
}

// The API rejects persistent_netboot outside the ipxe OS; the plan has to
// catch it before anything is created.
func TestAccServer_PersistentNetbootRequiresIpxe(t *testing.T) {
	mock, server := newDeployConfigServerMock(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockDeployConfigServerDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config:      testAccServerDeployConfig(testServerMockOS, `  persistent_netboot = true`),
				ExpectError: regexp.MustCompile(`only supported with operating_system = "ipxe"`),
			},
		},
	})
}

// Changing persistent_netboot on an existing server is a reinstall, like ipxe:
// blocked without allow_reinstall, sent in the reinstall payload with it, and
// removing a true value is sent as an explicit false so the API stops
// netbooting instead of keeping the stored value.
func TestAccServer_PersistentNetbootChangeReinstalls(t *testing.T) {
	mock, server := newDeployConfigServerMock(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockDeployConfigServerDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccServerIpxeConfig(""),
			},
			{
				Config:      testAccServerIpxeConfig(`  persistent_netboot = true`),
				ExpectError: regexp.MustCompile(`persistent_netboot changes require a server reinstall`),
			},
			{
				Config: testAccServerIpxeConfig("  persistent_netboot = true\n  allow_reinstall    = true"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_server.test_item", "persistent_netboot", "true"),
					checkLastReinstall(mock, 1, true),
				),
			},
			{
				Config: testAccServerIpxeConfig(`  allow_reinstall    = true`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("latitudesh_server.test_item", "persistent_netboot"),
					checkLastReinstall(mock, 2, false),
				),
			},
			{
				Config:   testAccServerIpxeConfig(`  allow_reinstall    = true`),
				PlanOnly: true,
			},
		},
	})
}

// raid-10 has to reach the create payload and come back from deploy_config.
// The read is the regression: on SDKs older than v1.21.1 it failed to
// unmarshal, which broke refresh for any server with a raid-10 disk group.
func TestAccServer_RaidTenDiskLayoutRoundTrip(t *testing.T) {
	mock, server := newDeployConfigServerMock(t)

	layout := `
  disk_layout = [
    { count = 2, role = "os", raid_level = "raid-1" },
    { count = 4, role = "storage", raid_level = "raid-10", mount_point = "/data" },
  ]`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockDeployConfigServerDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccServerDeployConfig(testServerMockOS, layout),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_server.test_item", "disk_layout.1.raid_level", "raid-10"),
					resource.TestCheckResourceAttr("latitudesh_server.test_item", "disk_layout.1.count", "4"),
					func(s *terraform.State) error {
						mock.mu.Lock()
						defer mock.mu.Unlock()
						groups, _ := mock.createAttrs["disk_layout"].([]any)
						if len(groups) != 2 {
							return fmt.Errorf("create payload disk_layout = %v, want 2 groups", mock.createAttrs["disk_layout"])
						}
						storage, _ := groups[1].(map[string]any)
						if storage["raid_level"] != "raid-10" {
							return fmt.Errorf("create payload storage raid_level = %v, want raid-10", storage["raid_level"])
						}
						return nil
					},
				),
			},
			{
				Config:   testAccServerDeployConfig(testServerMockOS, layout),
				PlanOnly: true,
			},
		},
	})
}
