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
	testFirewallName        = "test-firewall"
	testServerHostnameForFw = "fw-test-server"
	testServerPlanForFw     = "c2-small-x86"
	testServerSiteForFw     = "SAO"
	testServerOSForFw       = "ubuntu_22_04_x64_lts"
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
		assignments, _, err := client.Firewalls.ListAssignments(firewallID, nil)
		if err != nil {
			return err
		}

		for _, assignment := range assignments {
			if assignment.ID == rs.Primary.ID {
				return fmt.Errorf("Firewall assignment still exists")
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

		assignments, _, err := client.Firewalls.ListAssignments(firewallID, nil)
		if err != nil {
			return err
		}

		for _, a := range assignments {
			if a.ID == rs.Primary.ID {
				*assignment = a
				return nil
			}
		}

		return fmt.Errorf("Record not found")
	}
}

func testAccCheckLatitudeFirewallAssignmentConfig() string {
	return fmt.Sprintf(`
resource "latitudesh_project" "test" {
  name = "test-project-for-firewall-assignment"
  environment = "Development"
  provisioning_type = "on_demand"
}

resource "latitudesh_server" "test" {
  project = "%s"
  site = "%s"
  plan = "%s"
  hostname = "%s"
  operating_system = "%s"
}

resource "latitudesh_firewall" "test" {
  name = "%s"
  project = "%s"
  rules {
    from = "0.0.0.0/0"
    to = "server"
    port = "22"
    protocol = "tcp"
  }
}

resource "latitudesh_firewall_assignment" "test" {
  firewall_id = latitudesh_firewall.test.id
  server_id = latitudesh_server.test.id
}
`,
		os.Getenv("LATITUDESH_TEST_PROJECT"),
		testServerSiteForFw,
		testServerPlanForFw,
		testServerHostnameForFw,
		testServerOSForFw,
		testFirewallName,
		os.Getenv("LATITUDESH_TEST_PROJECT"),
	)
}
