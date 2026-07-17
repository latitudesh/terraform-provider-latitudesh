package latitudesh

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
)

func TestAccVlanAssignment_Basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	projectID, site, servers := testAccSharedServers(t, 1)

	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckVlanAssignmentDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccVlanAssignmentConfig(projectID, site, servers[0]),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckVlanAssignmentExists("latitudesh_vlan_assignment.test"),
					resource.TestCheckResourceAttr("latitudesh_vlan_assignment.test", "server_id", servers[0]),
					resource.TestCheckResourceAttrPair(
						"latitudesh_vlan_assignment.test", "virtual_network_id",
						"latitudesh_virtual_network.test", "id"),
					// Create must wait for the assignment to reach
					// "connected" before succeeding, so a completed apply must
					// report that status (with an allocated vid) — not a
					// still-"connecting" phantom.
					resource.TestCheckResourceAttr("latitudesh_vlan_assignment.test", "status", "connected"),
					resource.TestCheckResourceAttrSet("latitudesh_vlan_assignment.test", "vid"),
				),
			},
			{
				// Import using the documented "<PROJECT_ID>:<VLAN_ASSIGNMENT_ID>" format.
				ResourceName: "latitudesh_vlan_assignment.test",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["latitudesh_vlan_assignment.test"]
					if !ok {
						return "", fmt.Errorf("resource not found in state")
					}
					return projectID + ":" + rs.Primary.ID, nil
				},
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccVlanAssignment_CustomTimeout exercises the timeouts { create } block:
// an operator-supplied window must be accepted and drive the connect wait, and
// the assignment must still reach "connected". Consistent with
// TestAccServer_CustomTimeout.
func TestAccVlanAssignment_CustomTimeout(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	projectID, site, servers := testAccSharedServers(t, 1)

	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckVlanAssignmentDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccVlanAssignmentCustomTimeoutConfig(projectID, site, servers[0]),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckVlanAssignmentExists("latitudesh_vlan_assignment.test"),
					resource.TestCheckResourceAttr("latitudesh_vlan_assignment.test", "timeouts.create", "5m"),
					resource.TestCheckResourceAttr("latitudesh_vlan_assignment.test", "status", "connected"),
					resource.TestCheckResourceAttrSet("latitudesh_vlan_assignment.test", "vid"),
				),
			},
		},
	})
}

// TestAccVlanAssignment_AddTimeoutsNoRecreate: adding a timeouts block to an
// already-applied assignment must be an in-place update, not a destroy/create.
// It applies the assignment without timeouts, then re-applies with
// `timeouts { create = "5m" }` added and asserts the remote ID is unchanged (a
// recreate would allocate a new assignment with a new ID).
func TestAccVlanAssignment_AddTimeoutsNoRecreate(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	projectID, site, servers := testAccSharedServers(t, 1)

	recorder, teardown := createTestRecorder(t)
	defer teardown()

	const resourceName = "latitudesh_vlan_assignment.test"
	var originalID string

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckVlanAssignmentDestroy,
		Steps: []resource.TestStep{
			{
				// Apply without a timeouts block and capture the assignment ID.
				Config: testAccVlanAssignmentConfig(projectID, site, servers[0]),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckVlanAssignmentExists(resourceName),
					func(s *terraform.State) error {
						originalID = s.RootModule().Resources[resourceName].Primary.ID
						return nil
					},
				),
			},
			{
				// Add timeouts { create = "5m" } to the same assignment. This must
				// be applied in place: the ID must not change.
				Config: testAccVlanAssignmentCustomTimeoutConfig(projectID, site, servers[0]),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckVlanAssignmentExists(resourceName),
					resource.TestCheckResourceAttr(resourceName, "timeouts.create", "5m"),
					resource.TestCheckResourceAttr(resourceName, "status", "connected"),
					func(s *terraform.State) error {
						if got := s.RootModule().Resources[resourceName].Primary.ID; got != originalID {
							return fmt.Errorf("assignment was recreated: id changed from %s to %s", originalID, got)
						}
						return nil
					},
				),
			},
		},
	})
}

func testAccCheckVlanAssignmentDestroy(s *terraform.State) error {
	client := createVCRClient(nil)
	ctx := context.Background()

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_vlan_assignment" {
			continue
		}

		listRequest := operations.GetVirtualNetworksAssignmentsRequest{}
		if serverID := rs.Primary.Attributes["server_id"]; serverID != "" {
			listRequest.FilterServer = &serverID
		}
		response, err := client.PrivateNetworks.ListAssignments(ctx, listRequest)
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

		client := createVCRClient(nil)
		ctx := context.Background()
		listRequest := operations.GetVirtualNetworksAssignmentsRequest{}
		if serverID := rs.Primary.Attributes["server_id"]; serverID != "" {
			listRequest.FilterServer = &serverID
		}
		response, err := client.PrivateNetworks.ListAssignments(ctx, listRequest)
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

func testAccVlanAssignmentConfig(projectID, site, serverID string) string {
	return fmt.Sprintf(`
resource "latitudesh_virtual_network" "test" {
	project     = "%s"
	site        = "%s"
	description = "tf-acc-vlan-assignment"
}

resource "latitudesh_vlan_assignment" "test" {
	server_id          = "%s"
	virtual_network_id = latitudesh_virtual_network.test.id
}
`, projectID, site, serverID)
}

func testAccVlanAssignmentCustomTimeoutConfig(projectID, site, serverID string) string {
	return fmt.Sprintf(`
resource "latitudesh_virtual_network" "test" {
	project     = "%s"
	site        = "%s"
	description = "tf-acc-vlan-assignment"
}

resource "latitudesh_vlan_assignment" "test" {
	server_id          = "%s"
	virtual_network_id = latitudesh_virtual_network.test.id

	timeouts {
		create = "5m"
	}
}
`, projectID, site, serverID)
}
