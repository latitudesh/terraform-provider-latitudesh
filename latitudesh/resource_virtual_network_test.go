package latitudesh

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	api "github.com/latitudesh/latitudesh-go"
)

const (
	testVirtualNetworkDescription = "test"
	testVirtualNetworkSite        = "SAO2"
)

func TestAccVirtualNetwork_Basic(t *testing.T) {
	var VirtualNetwork api.VirtualNetwork

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckVirtualNetworkDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckVirtualNetworkBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckVirtualNetworkExists("latitudesh_virtual_network.test_item", &VirtualNetwork),
					resource.TestCheckResourceAttr(
						"latitudesh_virtual_network.test_item", "description", testVirtualNetworkDescription),
				),
			},
		},
	})
}

func testAccCheckVirtualNetworkDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*api.Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_virtual_network" {
			continue
		}
		if _, _, err := client.VirtualNetworks.Get(rs.Primary.ID, nil); err == nil {
			return fmt.Errorf("Virtual network still exists")
		}
	}

	return nil
}

func testAccCheckVirtualNetworkExists(n string, virtualNetwork *api.VirtualNetwork) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		client := testAccProvider.Meta().(*api.Client)

		foundVirtualNetwork, _, err := client.VirtualNetworks.Get(rs.Primary.ID, nil)
		if err != nil {
			return err
		}

		if foundVirtualNetwork.ID != rs.Primary.ID {
			return fmt.Errorf("Record not found: %v - %v", rs.Primary.ID, foundVirtualNetwork)
		}

		*virtualNetwork = *foundVirtualNetwork

		return nil
	}
}

func testAccCheckVirtualNetworkBasic() string {
	return fmt.Sprintf(`
resource "latitudesh_virtual_network" "test_item" {
	project  	= "%s"
  	description = "%s"
  	site        = "%s"
}
`,
		os.Getenv("LATITUDESH_TEST_PROJECT"),
		testVirtualNetworkDescription,
		testVirtualNetworkSite,
	)
}
