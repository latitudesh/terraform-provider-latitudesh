package latitudesh

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const testFirewallName = "test-firewall-assignment"

func TestAccLatitudeFirewallAssignment_Basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	projectID, _, servers := testAccSharedServers(t, 1)

	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckFirewallAssignmentDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckLatitudeFirewallAssignmentConfig(projectID, servers[0]),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair(
						"latitudesh_firewall_assignment.test", "firewall_id",
						"latitudesh_firewall.test", "id"),
					resource.TestCheckResourceAttr(
						"latitudesh_firewall_assignment.test", "server_id", servers[0]),
				),
			},
		},
	})
}

func testAccCheckFirewallAssignmentDestroy(s *terraform.State) error {
	client := createVCRClient(nil)

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

func testAccCheckLatitudeFirewallAssignmentConfig(projectID, serverID string) string {
	return fmt.Sprintf(`
resource "latitudesh_firewall" "test" {
	name    = "%s"
	project = "%s"

	# Default rule - API will automatically add this
	rules {
		from     = "ANY"
		to       = "ANY"
		port     = "22"
		protocol = "TCP"
	}

	# Custom rule
	rules {
		from     = "0.0.0.0"
		to       = "0.0.0.0"
		port     = "22"
		protocol = "TCP"
	}
}

resource "latitudesh_firewall_assignment" "test" {
	firewall_id = latitudesh_firewall.test.id
	server_id   = "%s"
}
`, testFirewallName, projectID, serverID)
}
