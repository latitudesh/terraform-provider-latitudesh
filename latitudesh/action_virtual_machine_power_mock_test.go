package latitudesh

// These tests drive the virtual machine power action end to end against a local
// mock of the Latitude.sh API, the same way the server power action tests do.
// The mock answers the actions route and a scripted status sequence on the VM
// route, so they cover the exact request body, the per-action wait target
// (Running vs Stopped), the Failed terminal state, and the no-poll paths.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const mockPowerVMID = "vm_powermock1"

// vmPowerMock answers the two routes the action touches and records what it was
// asked to do. statuses is consumed one entry per GET; the last entry repeats,
// so a sequence ending in a non-terminal status makes the loop run until it
// times out.
type vmPowerMock struct {
	mu         sync.Mutex
	statusCode int
	statuses   []string
	getCount   int
	postCount  int
	bodies     []map[string]any
}

func (m *vmPowerMock) nextStatus() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.getCount++
	idx := m.getCount - 1
	if idx >= len(m.statuses) {
		idx = len(m.statuses) - 1
	}
	return m.statuses[idx]
}

func (m *vmPowerMock) counts() (gets, posts int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getCount, m.postCount
}

func (m *vmPowerMock) lastBody(t *testing.T) map[string]any {
	t.Helper()

	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.bodies) == 0 {
		t.Fatal("the actions endpoint was never called")
	}
	return m.bodies[len(m.bodies)-1]
}

func (m *vmPowerMock) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.api+json")

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/virtual_machines/"+mockPowerVMID+"/actions":
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		m.mu.Lock()
		m.postCount++
		m.bodies = append(m.bodies, body)
		code := m.statusCode
		m.mu.Unlock()

		if code == 0 {
			code = http.StatusCreated
		}
		w.WriteHeader(code)
		if code != http.StatusCreated {
			_, _ = w.Write([]byte(`{"errors":[{"status":"422","title":"unprocessable"}]}`))
		}

	case r.Method == http.MethodGet && r.URL.Path == "/virtual_machines/"+mockPowerVMID:
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"data":{"id":%q,"type":"virtual_machines","attributes":{"name":"mock-vm","status":%q}}}`,
			mockPowerVMID, m.nextStatus())

	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

// newVMPowerMock starts the mock and shortens the poll interval, so a scripted
// status sequence costs milliseconds instead of one 10s sleep per transition.
func newVMPowerMock(t *testing.T, statusCode int, statuses ...string) (*vmPowerMock, *httptest.Server) {
	t.Helper()

	if len(statuses) == 0 {
		statuses = []string{"Running"}
	}
	mock := &vmPowerMock{statusCode: statusCode, statuses: statuses}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	previous := vmPowerPollInterval
	vmPowerPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { vmPowerPollInterval = previous })

	return mock, server
}

func testAccVMPowerActionConfig(powerAction, extraBody string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "terraform_data" "trigger" {
  input = "v1"

  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.latitudesh_virtual_machine_power.test]
    }
  }
}

action "latitudesh_virtual_machine_power" "test" {
  config {
    virtual_machine_id = %q
    power_action       = %q
%s
  }
}
`, mockPowerVMID, powerAction, extraBody)
}

func TestAccVirtualMachinePowerAction_PowerOffWaitsForStopped(t *testing.T) {
	mock, server := newVMPowerMock(t, 0, "Running", "Stopping", "Stopped")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccVMPowerActionConfig("power_off", ""),
				Check: func(*terraform.State) error {
					gets, posts := mock.counts()
					if posts != 1 {
						return fmt.Errorf("actions endpoint called %d times, want exactly 1", posts)
					}
					// The status endpoint keeps serving the pre-action status until a
					// watcher event lands, so the loop must poll through Running and
					// Stopping instead of accepting the first reading.
					if gets < 3 {
						return fmt.Errorf("polled %d times, want at least 3 (Running, Stopping, Stopped)", gets)
					}

					body := mock.lastBody(t)
					if body["id"] != mockPowerVMID {
						return fmt.Errorf(`id = %v, want %q`, body["id"], mockPowerVMID)
					}
					if body["type"] != "virtual_machines" {
						return fmt.Errorf(`type = %v, want "virtual_machines"`, body["type"])
					}
					attrs, _ := body["attributes"].(map[string]any)
					if attrs["action"] != "power_off" {
						return fmt.Errorf(`attributes.action = %v, want "power_off"`, attrs["action"])
					}
					return nil
				},
			},
		},
	})
}

func TestAccVirtualMachinePowerAction_PowerOnWaitsForRunning(t *testing.T) {
	mock, server := newVMPowerMock(t, 0, "Stopped", "Starting", "Running")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccVMPowerActionConfig("power_on", ""),
				Check: func(*terraform.State) error {
					gets, posts := mock.counts()
					if posts != 1 {
						return fmt.Errorf("actions endpoint called %d times, want exactly 1", posts)
					}
					if gets < 3 {
						return fmt.Errorf("polled %d times, want at least 3 (Stopped, Starting, Running)", gets)
					}
					return nil
				},
			},
		},
	})
}

// Failed is terminal: the wait must surface it instead of polling until the
// timeout as if the machine were still in transition.
func TestAccVirtualMachinePowerAction_FailedStateSurfaces(t *testing.T) {
	_, server := newVMPowerMock(t, 0, "Stopping", "Failed")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccVMPowerActionConfig("power_off", ""),
				// Terraform wraps diagnostic text at terminal width, so the regex must
				// tolerate a line break landing inside the sentence.
				ExpectError: regexp.MustCompile(`entered\s+status\s+"Failed"`),
			},
		},
	})
}

// A restart ends at the status the machine started from, so the action must
// return on acceptance and never poll — a wait here could not distinguish
// "restarted" from "never went down".
func TestAccVirtualMachinePowerAction_RebootReturnsOnAcceptance(t *testing.T) {
	mock, server := newVMPowerMock(t, 0, "Running")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccVMPowerActionConfig("reboot", ""),
				Check: func(*terraform.State) error {
					gets, posts := mock.counts()
					if posts != 1 {
						return fmt.Errorf("actions endpoint called %d times, want exactly 1", posts)
					}
					if gets != 0 {
						return fmt.Errorf("polled %d times, want 0: reboot has nothing to wait on", gets)
					}

					data := mock.lastBody(t)
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

// A status that never reaches the target must surface the timeout diagnostic —
// and, with the poll sleep capped at the remaining time, a wait_timeout below
// the poll interval expires on schedule instead of one interval late.
func TestAccVirtualMachinePowerAction_TimeoutSurfaces(t *testing.T) {
	_, server := newVMPowerMock(t, 0, "Running")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config:      testAccVMPowerActionConfig("power_off", `    wait_timeout = "200ms"`),
				ExpectError: regexp.MustCompile(`Virtual\s+Machine\s+power_off\s+Timeout`),
			},
		},
	})
}

func TestAccVirtualMachinePowerAction_NoWaitSkipsPolling(t *testing.T) {
	mock, server := newVMPowerMock(t, 0, "Running")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccVMPowerActionConfig("power_off", `    wait_for_status = false`),
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

func TestAccVirtualMachinePowerAction_APIErrorSurfaces(t *testing.T) {
	_, server := newVMPowerMock(t, http.StatusUnprocessableEntity)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config:      testAccVMPowerActionConfig("power_off", ""),
				ExpectError: regexp.MustCompile(`Virtual Machine Power Action Error`),
			},
		},
	})
}
