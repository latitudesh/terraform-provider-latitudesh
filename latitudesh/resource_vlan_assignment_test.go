package latitudesh

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	api "github.com/latitudesh/latitudesh-go"
)

var (
	server_id          = os.Getenv("LATITUDESH_TEST_SERVER_ID")
	virtual_network_id = os.Getenv("LATITUDESH_TEST_VIRTUAL_NETWORK_ID")
)

func TestAccVlanAssignment_Basic(t *testing.T) {
	var VlanAssignment api.VlanAssignment

	recorder, teardown := createTestRecorder(t)
	defer teardown()
	testAccProviders["latitudesh"].ConfigureContextFunc = testProviderConfigure(recorder)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
			testAccServerCheck(t)
			testAccVirtualNetworkCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckVlanAssignmentDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckVlanAssignmentBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckVlanAssignmentExists("latitudesh_vlan_assignment.test_item", &VlanAssignment),
					resource.TestCheckResourceAttr(
						"latitudesh_vlan_assignment.test_item", "server_id", server_id),
					resource.TestCheckResourceAttr(
						"latitudesh_vlan_assignment.test_item", "virtual_network_id", virtual_network_id),
				),
			},
			// Test idempotency: apply the same config - should have no changes
			{
				Config:   testAccCheckVlanAssignmentBasic(),
				PlanOnly: true,
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
			return err
		}

		for _, assignment := range assignments {
			if assignment.ID == rs.Primary.ID {
				return fmt.Errorf("VLAN assignment %s still exists", rs.Primary.ID)
			}
		}
	}

	return nil
}

func testAccCheckVlanAssignmentExists(n string, vlanAssignment *api.VlanAssignment) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		client := testAccProvider.Meta().(*api.Client)

		assignments, _, err := client.VlanAssignments.List(nil)
		if err != nil {
			return err
		}

		var found bool
		for _, assignment := range assignments {
			if assignment.ID == rs.Primary.ID {
				*vlanAssignment = assignment
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("VLAN assignment with ID %s not found", rs.Primary.ID)
		}

		return nil
	}
}

func testAccCheckVlanAssignmentBasic() string {
	return fmt.Sprintf(`
resource "latitudesh_vlan_assignment" "test_item" {
  	server_id          = "%s"
  	virtual_network_id = "%s"
}
`,
		os.Getenv("LATITUDESH_TEST_SERVER_ID"),
		os.Getenv("LATITUDESH_TEST_VIRTUAL_NETWORK_ID"),
	)
}
