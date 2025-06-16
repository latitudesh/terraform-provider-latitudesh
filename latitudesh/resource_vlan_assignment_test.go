package latitudesh

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
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
	client := testAccProvider.Meta().(*latitudeshgosdk.Latitudesh)
	ctx := context.Background()

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_vlan_assignment" {
			continue
		}

		response, err := client.PrivateNetworks.ListAssignments(ctx, operations.GetVirtualNetworksAssignmentsRequest{})
		if err != nil {
			continue
		}

		if response.VirtualNetworkAssignments != nil && response.VirtualNetworkAssignments.Data != nil {
			for _, assignment := range response.VirtualNetworkAssignments.Data {
				if assignment.ID != nil && *assignment.ID == rs.Primary.ID {
					return fmt.Errorf("VLAN assignment still exists")
				}
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

		client := testAccProvider.Meta().(*latitudeshgosdk.Latitudesh)
		ctx := context.Background()
		response, err := client.PrivateNetworks.ListAssignments(ctx, operations.GetVirtualNetworksAssignmentsRequest{})
		if err != nil {
			return fmt.Errorf("error fetching VLAN assignments: %s", err)
		}

		if response.VirtualNetworkAssignments == nil || response.VirtualNetworkAssignments.Data == nil {
			return fmt.Errorf("VLAN assignment not found")
		}

		// Find our assignment
		for _, assignment := range response.VirtualNetworkAssignments.Data {
			if assignment.ID != nil && *assignment.ID == rs.Primary.ID {
				return nil
			}
		}

		return fmt.Errorf("VLAN assignment not found")
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
