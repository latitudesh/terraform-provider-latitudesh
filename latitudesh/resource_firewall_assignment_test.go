package latitudesh

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
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
		Providers: testAccProviders,
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

func testAccCheckLatitudeFirewallAssignmentConfig() string {
	return fmt.Sprintf(`
resource "latitudesh_project" "test" {
  name = "test-project-for-firewall-assignment"
  environment = "Development"
}

resource "latitudesh_server" "test" {
  project = latitudesh_project.test.id
  site = "NY1"
  plan = "c2-medium-x86"
  hostname = "test-server-for-firewall"
  operating_system = "ubuntu_22_04_x64"
}

resource "latitudesh_firewall" "test" {
  name = "test-firewall-for-assignment"
  project = latitudesh_project.test.id
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
`)
}
