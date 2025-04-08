package latitudesh

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	api "github.com/latitudesh/latitudesh-go"
)

// Define constants for testing
const (
	testFirewallName     = "test-firewall-assignment"
	testMockFirewallID   = "fw_123456789ABC"
	testMockAssignmentID = "fwasg_987654321ZYX"
	testMockServerID     = "sv_BDXM5Ek1m0rpk"
)

func TestAccLatitudeFirewallAssignment_Basic(t *testing.T) {
	var assignment api.FirewallAssignment

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
					testAccCheckFirewallAssignmentExists("latitudesh_firewall_assignment.test", &assignment),
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
	client := testAccProvider.Meta().(*api.Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_firewall_assignment" {
			continue
		}

		firewallID := rs.Primary.Attributes["firewall_id"]

		// Try to get assignments for the firewall
		assignments, resp, err := client.Firewalls.ListAssignments(firewallID, nil)

		// If the firewall itself is gone, that's okay
		if resp != nil && resp.StatusCode == 404 {
			continue
		}

		if err == nil {
			// Check if our specific assignment still exists
			for _, assignment := range assignments {
				if assignment.ID == rs.Primary.ID {
					return fmt.Errorf("Firewall assignment %s still exists", rs.Primary.ID)
				}
			}
		}
	}

	return nil
}

func testAccCheckFirewallAssignmentExists(n string, assignment *api.FirewallAssignment) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		client := testAccProvider.Meta().(*api.Client)
		firewallID := rs.Primary.Attributes["firewall_id"]

		// Try to get assignments for the firewall
		assignments, resp, err := client.Firewalls.ListAssignments(firewallID, nil)
		if err != nil {
			// If we get a 404 for the firewall, the assignment can't exist
			if resp != nil && resp.StatusCode == 404 {
				return fmt.Errorf("Record not found: firewall %s does not exist", firewallID)
			}
			return err
		}

		for _, a := range assignments {
			if a.ID == rs.Primary.ID {
				*assignment = a
				return nil
			}
		}

		return fmt.Errorf("Record not found: assignment %s not found in firewall %s", rs.Primary.ID, firewallID)
	}
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
