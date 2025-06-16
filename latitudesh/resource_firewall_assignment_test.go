package latitudesh

import (
	"context"
	"fmt"
	"os"
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

		// Get the firewall ID from the resource attributes
		firewallID := rs.Primary.Attributes["firewall_id"]
		if firewallID == "" {
			continue
		}

		// Check if the firewall assignment still exists
		response, err := client.Firewalls.ListAssignments(context.Background(), firewallID, nil, nil)
		if err != nil {
			// If we get an error, assume it's deleted
			continue
		}

		// Check if our assignment ID is still in the response
		assignmentID := rs.Primary.ID
		if response.FirewallAssignments != nil && response.FirewallAssignments.Data != nil {
			for _, assignment := range response.FirewallAssignments.Data {
				if assignment.ID != nil && *assignment.ID == assignmentID {
					return fmt.Errorf("firewall assignment still exists")
				}
			}
		}

		// If not found in the data array, it's deleted
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
