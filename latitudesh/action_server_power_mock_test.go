package latitudesh

// These tests drive the server power action end to end — Terraform binary,
// protocol, provider, action, HTTP, status polling — against a local mock of the
// Latitude.sh API injected through the provider's httpClient, the same hook the
// reinstall action tests use. They run under TF_ACC without credentials and
// without provisioning anything.
//
// What they cover that the unit tests cannot: that Terraform actually invokes the
// action, the exact request body the API receives, and how the poll loop reacts
// to a sequence of statuses — including the per-action target ("on" vs "off").

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const mockPowerServerID = "sv_powermock1"

// serverPowerMock answers the two routes the action touches and records what it
// was asked to do. statuses is consumed one entry per GET; the last entry
// repeats, so a sequence ending in a non-terminal status makes the loop run
// until it times out.
type serverPowerMock struct {
	mu        sync.Mutex
	statuses  []string
	getCount  int
	postCount int
	bodies    []map[string]any
}

func (m *serverPowerMock) nextStatus() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.getCount++
	idx := m.getCount - 1
	if idx >= len(m.statuses) {
		idx = len(m.statuses) - 1
	}
	return m.statuses[idx]
}

func (m *serverPowerMock) counts() (gets, posts int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getCount, m.postCount
}

func (m *serverPowerMock) lastBody(t *testing.T) map[string]any {
	t.Helper()

	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.bodies) == 0 {
		t.Fatal("the actions endpoint was never called")
	}
	return m.bodies[len(m.bodies)-1]
}

func (m *serverPowerMock) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.api+json")

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/servers/"+mockPowerServerID+"/actions":
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		m.mu.Lock()
		m.postCount++
		m.bodies = append(m.bodies, body)
		m.mu.Unlock()

		// The SDK treats only 201 as success on this route and unmarshals a
		// ServerAction envelope from it.
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"id":"act_1","type":"actions","attributes":{"status":"pending"}}}`))

	case r.Method == http.MethodGet && r.URL.Path == "/servers/"+mockPowerServerID:
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"data":{"id":%q,"type":"servers","attributes":{"hostname":"mock-01","status":%q}}}`,
			mockPowerServerID, m.nextStatus())

	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

// newServerPowerMock starts the mock and shortens the poll interval, so a
// scripted status sequence costs milliseconds instead of one 30s sleep per
// transition.
func newServerPowerMock(t *testing.T, statuses ...string) (*serverPowerMock, *httptest.Server) {
	t.Helper()

	mock := &serverPowerMock{statuses: statuses}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	previous := serverPollInterval
	serverPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { serverPollInterval = previous })

	return mock, server
}

// testAccServerPowerActionConfig triggers the action from a terraform_data
// marker rather than from a latitudesh_server, so the configuration contains no
// Latitude resource at all and the mock only ever sees the action's own calls.
func testAccServerPowerActionConfig(powerAction, extraBody string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "terraform_data" "trigger" {
  input = "v1"

  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.latitudesh_server_power.test]
    }
  }
}

action "latitudesh_server_power" "test" {
  config {
    server_id    = %q
    power_action = %q
%s
  }
}
`, mockPowerServerID, powerAction, extraBody)
}

func TestAccServerPowerAction_PowerOnWaitsForOn(t *testing.T) {
	mock, server := newServerPowerMock(t, "off", "off", "on")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccServerPowerActionConfig("power_on", ""),
				Check: func(*terraform.State) error {
					gets, posts := mock.counts()
					if posts != 1 {
						return fmt.Errorf("actions endpoint called %d times, want exactly 1", posts)
					}
					// The loop must poll past "off" instead of accepting the first
					// reading, so it cannot have stopped before the third GET.
					if gets < 3 {
						return fmt.Errorf("polled %d times, want at least 3 (off, off, on)", gets)
					}

					data, ok := mock.lastBody(t)["data"].(map[string]any)
					if !ok {
						return fmt.Errorf("payload has no data object")
					}
					if data["type"] != "actions" {
						return fmt.Errorf(`data.type = %v, want "actions"`, data["type"])
					}
					attrs, _ := data["attributes"].(map[string]any)
					if attrs["action"] != "power_on" {
						return fmt.Errorf(`attributes.action = %v, want "power_on"`, attrs["action"])
					}
					return nil
				},
			},
		},
	})
}

func TestAccServerPowerAction_PowerOffWaitsForOff(t *testing.T) {
	mock, server := newServerPowerMock(t, "on", "on", "off")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccServerPowerActionConfig("power_off", ""),
				Check: func(*terraform.State) error {
					gets, posts := mock.counts()
					if posts != 1 {
						return fmt.Errorf("actions endpoint called %d times, want exactly 1", posts)
					}
					// "on" must not terminate a wait that targets "off": the loop has
					// to poll through both "on" readings before it may stop.
					if gets < 3 {
						return fmt.Errorf("polled %d times, want at least 3 (on, on, off)", gets)
					}

					data := mock.lastBody(t)["data"].(map[string]any)
					attrs, _ := data["attributes"].(map[string]any)
					if attrs["action"] != "power_off" {
						return fmt.Errorf(`attributes.action = %v, want "power_off"`, attrs["action"])
					}
					return nil
				},
			},
		},
	})
}

// A reboot cannot be observed through the status endpoint (the API reports "on"
// throughout a warm reset), so the action must return on acceptance and never
// poll — a wait here would be theater.
func TestAccServerPowerAction_RebootReturnsOnAcceptance(t *testing.T) {
	mock, server := newServerPowerMock(t, "on")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccServerPowerActionConfig("reboot", ""),
				Check: func(*terraform.State) error {
					gets, posts := mock.counts()
					if posts != 1 {
						return fmt.Errorf("actions endpoint called %d times, want exactly 1", posts)
					}
					if gets != 0 {
						return fmt.Errorf("polled %d times, want 0: reboot has nothing to wait on", gets)
					}

					data := mock.lastBody(t)["data"].(map[string]any)
					attrs, _ := data["attributes"].(map[string]any)
					if attrs["action"] != "reboot" {
						return fmt.Errorf(`attributes.action = %v, want "reboot"`, attrs["action"])
					}
					return nil
				},
			},
		},
	})
}

func TestAccServerPowerAction_NoWaitSkipsPolling(t *testing.T) {
	mock, server := newServerPowerMock(t, "off")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccServerPowerActionConfig("power_on", `    wait_for_status = false`),
				Check: func(*terraform.State) error {
					gets, posts := mock.counts()
					if posts != 1 {
						return fmt.Errorf("actions endpoint called %d times, want exactly 1", posts)
					}
					// With wait_for_status = false the action must return as soon as
					// the API accepts the request. A single GET would mean it waited.
					if gets != 0 {
						return fmt.Errorf("polled %d times, want 0 with wait_for_status = false", gets)
					}
					return nil
				},
			},
		},
	})
}
