package latitudesh

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

// Define constants for testing
const (
	testFirewallName     = "test-firewall-assignment"
	testMockFirewallID   = "fw_123456789ABC"
	testMockAssignmentID = "fwasg_987654321ZYX"
	testMockServerID     = "sv_BDXM5Ek1m0rpk"
)

func TestAccLatitudeFirewallAssignment_Basic(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()
	testAccProviders["latitudesh"].ConfigureContextFunc = testProviderConfigure(recorder)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
			testAccProjectCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckFirewallAssignmentDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckLatitudeFirewallAssignmentConfig(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(
						"latitudesh_firewall_assignment.test", "firewall_id"),
					resource.TestCheckResourceAttrSet(
						"latitudesh_firewall_assignment.test", "server_id"),
				),
			},
		},
	})
}

func testAccCheckFirewallAssignmentDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*latitudeshgosdk.Latitudesh)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_firewall_assignment" {
			continue
		}

		// Parse the composite ID: firewallID:serverID
		idParts := strings.Split(rs.Primary.ID, ":")
		if len(idParts) != 2 {
			continue
		}

		firewallID := idParts[0]

		// Check if the firewall assignment still exists
		response, err := client.Firewalls.GetFirewallAssignments(context.Background(), firewallID, nil, nil)
		if err != nil {
			// If we get an error, assume it's deleted
			continue
		}

		if response.FirewallServer != nil {
			return fmt.Errorf("Firewall assignment still exists")
		}
	}

	return nil
}

func testAccCheckLatitudeFirewallAssignmentConfig() string {
	// Get required environment variables
	projectID := os.Getenv("LATITUDESH_TEST_PROJECT")
	if projectID == "" {
		projectID = "test-project-id" // fallback for VCR mode
	}

	serverID := os.Getenv("LATITUDESH_TEST_SERVER")
	if serverID == "" {
		serverID = "sv_BDXM5Ek1m0rpk" // fallback for VCR mode
	}

	// Check if LATITUDESH_FIREWALL_ID is provided
	firewallID := os.Getenv("LATITUDESH_FIREWALL_ID")
	if firewallID != "" {
		// If firewall ID is provided, use it directly
		return fmt.Sprintf(`
resource "latitudesh_firewall_assignment" "test" {
  firewall_id = "%s"
  server_id = "%s"
}
`,
			firewallID,
			serverID,
		)
	}

	// Otherwise, create a new firewall
	return fmt.Sprintf(`
resource "latitudesh_firewall" "test" {
  name = "%s"
  project = "%s"
  
  # Default rule - API will automatically add this
  rules {
    from = "ANY"
    to = "ANY"
    port = "22"
    protocol = "TCP"
  }
  
  # Custom rule
  rules {
    from = "0.0.0.0"
    to = "0.0.0.0"
    port = "22"
    protocol = "TCP"
  }
}

resource "latitudesh_firewall_assignment" "test" {
  firewall_id = latitudesh_firewall.test.id
  server_id = "%s"
}
`,
		testFirewallName,
		projectID,
		serverID,
	)
}
