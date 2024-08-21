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
		},
	})
}

func testAccCheckVlanAssignmentDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*api.Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_vlan_assignment" {
			continue
		}
		if _, _, err := client.VlanAssignments.Get(rs.Primary.ID); err == nil {
			return fmt.Errorf("Virtual network still exists")
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

		foundVlanAssignment, _, err := client.VlanAssignments.Get(rs.Primary.ID)
		if err != nil {
			return err
		}

		if foundVlanAssignment.ID != rs.Primary.ID {
			return fmt.Errorf("Record not found: %v - %v", rs.Primary.ID, foundVlanAssignment)
		}

		*vlanAssignment = *foundVlanAssignment

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
