package latitudesh

// These tests drive the reinstall action end to end — Terraform binary, protocol,
// provider, action, HTTP, status polling — against a local mock of the
// Latitude.sh API injected through the provider's httpClient, the same hook the
// virtual machine site tests use. They run under TF_ACC without credentials and
// without provisioning anything.
//
// What they cover that the unit tests cannot: that Terraform actually invokes the
// action, the exact request body the API receives, and how the poll loop reacts to
// a sequence of statuses.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const mockReinstallServerID = "sv_mock123"

// reinstallMock answers the two routes the action touches and records what it was
// asked to do. statuses is consumed one entry per GET; the last entry repeats, so
// a sequence ending in a non-terminal status makes the loop run until it times out.
type reinstallMock struct {
	mu        sync.Mutex
	statuses  []string
	getCount  int
	postCount int
	bodies    []map[string]any
}

func (m *reinstallMock) nextStatus() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.getCount++
	idx := m.getCount - 1
	if idx >= len(m.statuses) {
		idx = len(m.statuses) - 1
	}
	return m.statuses[idx]
}

func (m *reinstallMock) counts() (gets, posts int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getCount, m.postCount
}

func (m *reinstallMock) lastBody(t *testing.T) map[string]any {
	t.Helper()

	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.bodies) == 0 {
		t.Fatal("the reinstall endpoint was never called")
	}
	return m.bodies[len(m.bodies)-1]
}

func (m *reinstallMock) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.api+json")

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/servers/"+mockReinstallServerID+"/reinstall":
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

		// The SDK treats only 201 as success on this route; 200 or 202 would come
		// back as "unknown status code returned".
		w.WriteHeader(http.StatusCreated)

	case r.Method == http.MethodGet && r.URL.Path == "/servers/"+mockReinstallServerID:
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"data":{"id":%q,"type":"servers","attributes":{"hostname":"mock-01","status":%q}}}`,
			mockReinstallServerID, m.nextStatus())

	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

// newReinstallMock starts the mock and shortens the poll interval, so a scripted
// status sequence costs milliseconds instead of one 30s sleep per transition.
func newReinstallMock(t *testing.T, statuses ...string) (*reinstallMock, *httptest.Server) {
	t.Helper()

	mock := &reinstallMock{statuses: statuses}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	previous := serverPollInterval
	serverPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { serverPollInterval = previous })

	return mock, server
}

// testAccReinstallActionConfig triggers the action from a terraform_data marker
// rather than from a latitudesh_server, so the configuration contains no
// Latitude resource at all and the mock only ever sees the action's own calls.
func testAccReinstallActionConfig(actionBody string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "terraform_data" "trigger" {
  input = "v1"

  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.latitudesh_server_reinstall.test]
    }
  }
}

action "latitudesh_server_reinstall" "test" {
  config {
    server_id = %q
%s
  }
}
`, mockReinstallServerID, actionBody)
}

func TestAccServerReinstallAction_WaitsForOn(t *testing.T) {
	mock, server := newReinstallMock(t, "deploying", "deploying", "on")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccReinstallActionConfig(""),
				Check: func(*terraform.State) error {
					gets, posts := mock.counts()
					if posts != 1 {
						return fmt.Errorf("reinstall called %d times, want exactly 1", posts)
					}
					// The loop must poll past "deploying" instead of accepting the
					// first reading, so it cannot have stopped before the third GET.
					if gets < 3 {
						return fmt.Errorf("polled %d times, want at least 3 (deploying, deploying, on)", gets)
					}
					return nil
				},
			},
		},
	})
}

func TestAccServerReinstallAction_OmittedAttributesStayOutOfThePayload(t *testing.T) {
	mock, server := newReinstallMock(t, "deploying", "on")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccReinstallActionConfig(""),
				Check: func(*terraform.State) error {
					body := mock.lastBody(t)

					data, ok := body["data"].(map[string]any)
					if !ok {
						return fmt.Errorf("payload has no data object: %v", body)
					}
					if data["type"] != "reinstalls" {
						return fmt.Errorf(`data.type = %v, want "reinstalls"`, data["type"])
					}

					// This is the guarantee the whole design rests on: a bare config
					// must not tell the API to change anything, so the server keeps
					// the operating system, hostname and keys it already runs.
					attrs, _ := data["attributes"].(map[string]any)
					if len(attrs) != 0 {
						return fmt.Errorf("attributes = %v, want empty when only server_id is set", attrs)
					}
					return nil
				},
			},
		},
	})
}

func TestAccServerReinstallAction_OverridesReachThePayload(t *testing.T) {
	mock, server := newReinstallMock(t, "deploying", "on")

	overrides := strings.Join([]string{
		`    operating_system = "debian_12"`,
		`    hostname         = "mock-01"`,
		`    ssh_keys         = ["ssh_1", "ssh_2"]`,
	}, "\n")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccReinstallActionConfig(overrides),
				Check: func(*terraform.State) error {
					data := mock.lastBody(t)["data"].(map[string]any)
					attrs, ok := data["attributes"].(map[string]any)
					if !ok {
						return fmt.Errorf("payload has no attributes object: %v", data)
					}

					if attrs["operating_system"] != "debian_12" {
						return fmt.Errorf(`operating_system = %v, want "debian_12"`, attrs["operating_system"])
					}
					if attrs["hostname"] != "mock-01" {
						return fmt.Errorf(`hostname = %v, want "mock-01"`, attrs["hostname"])
					}
					keys, _ := attrs["ssh_keys"].([]any)
					if len(keys) != 2 || keys[0] != "ssh_1" {
						return fmt.Errorf(`ssh_keys = %v, want ["ssh_1","ssh_2"]`, attrs["ssh_keys"])
					}
					return nil
				},
			},
		},
	})
}

func TestAccServerReinstallAction_FailedDeploymentFails(t *testing.T) {
	_, server := newReinstallMock(t, "deploying", "failed_deployment")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config:      testAccReinstallActionConfig(""),
				ExpectError: regexp.MustCompile(`Server entered failed state: failed_deployment`),
			},
		},
	})
}

func TestAccServerReinstallAction_NoWaitSkipsPolling(t *testing.T) {
	mock, server := newReinstallMock(t, "deploying")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccReinstallActionConfig(`    wait_for_ready = false`),
				Check: func(*terraform.State) error {
					gets, posts := mock.counts()
					if posts != 1 {
						return fmt.Errorf("reinstall called %d times, want exactly 1", posts)
					}
					// With wait_for_ready = false the action must return as soon as
					// the API accepts the request. A single GET would mean it waited.
					if gets != 0 {
						return fmt.Errorf("polled %d times, want 0 with wait_for_ready = false", gets)
					}
					return nil
				},
			},
		},
	})
}
