package latitudesh

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	api "github.com/latitudesh/latitudesh-go"
)

func TestAccVlanAssignment_Basic(t *testing.T) {
	serverID := "sv_ZWr75Zbjr5A91"
	vlanID := "vlan_BDXM5E1Yo5rpk"

	recorder, teardown := createTestRecorder(t)
	defer teardown()
	testAccProviders["latitudesh"].ConfigureContextFunc = testProviderConfigure(recorder)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckVlanAssignmentDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccVlanAssignmentConfig(serverID, vlanID),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckVlanAssignmentExists("latitudesh_vlan_assignment.test"),
					resource.TestCheckResourceAttr("latitudesh_vlan_assignment.test", "server_id", serverID),
					resource.TestCheckResourceAttr("latitudesh_vlan_assignment.test", "virtual_network_id", vlanID),
				),
			},
		},
	})
}

func testAccCheckVlanAssignmentDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*api.Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_vlan_assignment" {
			continue
		}

		assignments, _, err := client.VlanAssignments.List(nil)
		if err != nil {
			return fmt.Errorf("error fetching VLAN assignments: %s", err)
		}

		for _, assignment := range assignments {
			if assignment.ID == rs.Primary.ID {
				return fmt.Errorf("VLAN assignment %s still exists", rs.Primary.ID)
			}
		}
	}

	return nil
}

func testAccCheckVlanAssignmentExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("no ID is set")
		}

		client := testAccProvider.Meta().(*api.Client)
		assignments, _, err := client.VlanAssignments.List(nil)
		if err != nil {
			return fmt.Errorf("error fetching VLAN assignments: %s", err)
		}

		for _, assignment := range assignments {
			if assignment.ID == rs.Primary.ID {
				return nil
			}
		}

		return fmt.Errorf("VLAN assignment %s not found", rs.Primary.ID)
	}
}

func testAccVlanAssignmentConfig(serverID, vlanID string) string {
	return fmt.Sprintf(`
resource "latitudesh_vlan_assignment" "test" {
	server_id          = "%s"
	virtual_network_id = "%s"
}
`, serverID, vlanID)
}
