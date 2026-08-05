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

// TestAccServerReinstallAction_Live reinstalls a real server through the action.
//
// It borrows the shared fixture rather than deploying its own machine: the point
// under test is the reinstall, not a provision, and the fixture server is one
// another test in the suite already paid for. The server is handed back running
// the same operating system and hostname it had — only its disks are wiped, which
// no other test depends on.
//
// This is the one thing the mock-backed tests in
// action_server_reinstall_mock_test.go cannot establish: that the API accepts the
// payload we build and drives the server through a status sequence our wait loop
// actually recognizes. The mock only ever confirms our own assumptions.
func TestAccServerReinstallAction_Live(t *testing.T) {
	// testAccSharedServers provisions real infrastructure the moment it is called,
	// which happens before resource.Test can skip on TF_ACC. Without this guard an
	// offline `go test ./...` would deploy a server.
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}
	testAccTokenCheck(t)

	_, _, serverIDs := testAccSharedServers(t, 1)
	serverID := serverIDs[0]

	hostnameBefore := testAccSharedServerHostname(0)
	if hostnameBefore == "" {
		t.Fatal("shared fixture did not record a hostname")
	}

	// Production polls every 30 seconds. A reinstall on the fixture's plan has been
	// observed going on -> deploying -> on inside a single interval, and the wait
	// only accepts a terminal status after it has seen the server leave the state it
	// started in — so at 30s this test could time out on a reinstall that worked.
	// Polling faster makes the transition observable; it does not change what is
	// being tested.
	previousInterval := serverPollInterval
	serverPollInterval = 5 * time.Second
	t.Cleanup(func() { serverPollInterval = previousInterval })

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testAccReinstallActionLiveConfig(serverID),
				Check: func(*terraform.State) error {
					// Reaching here means the action returned without diagnostics, so
					// the wait already saw the server come back up. What is left to
					// prove is that a bare payload preserved the deploy config rather
					// than resetting it.
					client := createVCRClient(nil)
					response, err := client.Servers.Get(context.Background(), serverID, nil)
					if err != nil {
						return fmt.Errorf("reading server %s after reinstall: %w", serverID, err)
					}
					if response == nil || response.Server == nil || response.Server.Data == nil ||
						response.Server.Data.Attributes == nil {
						return fmt.Errorf("server %s returned no attributes after reinstall", serverID)
					}

					attrs := response.Server.Data.Attributes
					if attrs.Status == nil {
						return fmt.Errorf("server %s reported no status after reinstall", serverID)
					}
					if status := string(*attrs.Status); status != "on" {
						return fmt.Errorf("server %s is %q after the action returned, want \"on\"", serverID, status)
					}
					if attrs.Hostname == nil || *attrs.Hostname != hostnameBefore {
						return fmt.Errorf("reinstall changed the hostname: was %q, now %v.\n"+
							"A config with only server_id must not tell the API to change anything.",
							hostnameBefore, attrs.Hostname)
					}
					return nil
				},
			},
		},
	})
}

// testAccReinstallActionLiveConfig triggers the action from a terraform_data
// marker so the configuration manages no Latitude resource: the server under test
// is the fixture's, referenced by ID, and nothing here can create or destroy it.
func testAccReinstallActionLiveConfig(serverID string) string {
	return fmt.Sprintf(`
resource "terraform_data" "reinstall_trigger" {
  input = %q

  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.latitudesh_server_reinstall.test]
    }
  }
}

action "latitudesh_server_reinstall" "test" {
  config {
    server_id    = %q
    wait_timeout = "45m"
  }
}
`, serverID, serverID)
}
