package latitudesh

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// TestAccServerPowerAction_Live power-cycles a real server through the action:
// power_off first, then power_on, so the shared fixture is handed back in the
// state every other test expects.
//
// A reboot is deliberately not tested live: the API reports "on" throughout a
// warm reset, so a live reboot proves nothing the mock tests do not. The power
// cycle is what exercises the wait loop against real status transitions — the
// one thing the mocks cannot establish, since the cached power_state the API
// serves is refreshed by an external sync whose latency only exists in
// production.
func TestAccServerPowerAction_Live(t *testing.T) {
	// testAccSharedServers provisions real infrastructure the moment it is called,
	// which happens before resource.Test can skip on TF_ACC. Without this guard an
	// offline `go test ./...` would deploy a server.
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}
	testAccTokenCheck(t)

	_, _, serverIDs := testAccSharedServers(t, 1)
	serverID := serverIDs[0]

	// Production polls every 30 seconds; the external power_state sync adds its
	// own lag on top. Polling faster makes transitions observable sooner without
	// changing what is being tested.
	previousInterval := serverPollInterval
	serverPollInterval = 5 * time.Second
	t.Cleanup(func() { serverPollInterval = previousInterval })

	checkStatus := func(want string) func(*terraform.State) error {
		return func(*terraform.State) error {
			// Reaching here means the action returned without diagnostics, so the
			// wait already saw the target status. Re-read it independently to prove
			// the wait did not fool itself.
			client := createVCRClient(nil)
			response, err := client.Servers.Get(context.Background(), serverID, nil)
			if err != nil {
				return fmt.Errorf("reading server %s: %w", serverID, err)
			}
			if response == nil || response.Server == nil || response.Server.Data == nil ||
				response.Server.Data.Attributes == nil || response.Server.Data.Attributes.Status == nil {
				return fmt.Errorf("server %s returned no status", serverID)
			}
			if status := string(*response.Server.Data.Attributes.Status); status != want {
				return fmt.Errorf("server %s is %q after the action returned, want %q", serverID, status, want)
			}
			return nil
		}
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testAccServerPowerActionLiveConfig(serverID, "cycle-1", "after_create", "power_off"),
				Check:  checkStatus("off"),
			},
			{
				Config: testAccServerPowerActionLiveConfig(serverID, "cycle-2", "after_update", "power_on"),
				Check:  checkStatus("on"),
			},
		},
	})
}

// testAccServerPowerActionLiveConfig triggers the action from a terraform_data
// marker so the configuration manages no Latitude resource: the server under
// test is the fixture's, referenced by ID, and nothing here can create or
// destroy it. Changing `input` between steps is what re-fires the trigger.
func testAccServerPowerActionLiveConfig(serverID, input, event, powerAction string) string {
	return fmt.Sprintf(`
resource "terraform_data" "power_trigger" {
  input = %q

  lifecycle {
    action_trigger {
      events  = [%s]
      actions = [action.latitudesh_server_power.test]
    }
  }
}

action "latitudesh_server_power" "test" {
  config {
    server_id    = %q
    power_action = %q
    wait_timeout = "20m"
  }
}
`, input, event, serverID, powerAction)
}
