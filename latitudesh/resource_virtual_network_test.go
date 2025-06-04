package latitudesh

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

const (
	testVirtualNetworkDescription = "test"
	testVirtualNetworkSite        = "SAO2"
)

func TestAccVirtualNetwork_Basic(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()
	testAccProviders["latitudesh"].ConfigureContextFunc = testProviderConfigure(recorder)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
			testAccProjectCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckVirtualNetworkDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckVirtualNetworkBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckVirtualNetworkExists("latitudesh_virtual_network.test_item"),
					resource.TestCheckResourceAttr(
						"latitudesh_virtual_network.test_item", "description", testVirtualNetworkDescription),
					resource.TestCheckResourceAttr(
						"latitudesh_virtual_network.test_item", "site", testVirtualNetworkSite),
				),
			},
		},
	})
}

func testAccCheckVirtualNetworkDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*latitudeshgosdk.Latitudesh)
	ctx := context.Background()

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_virtual_network" {
			continue
		}

		// Try to get the virtual network
		_, err := client.PrivateNetworks.GetVirtualNetwork(ctx, rs.Primary.ID)

		// If no error is returned, the resource still exists
		if err == nil {
			return fmt.Errorf("Virtual network still exists")
		}
	}

	return nil
}

func testAccCheckVirtualNetworkExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		client := testAccProvider.Meta().(*latitudeshgosdk.Latitudesh)
		ctx := context.Background()

		// Try to get the virtual network
		result, err := client.PrivateNetworks.GetVirtualNetwork(ctx, rs.Primary.ID)
		if err != nil {
			return err
		}

		// Check if the returned virtual network has the expected ID
		if result.Object != nil && result.Object.Data != nil && result.Object.Data.ID != nil {
			if *result.Object.Data.ID != rs.Primary.ID {
				return fmt.Errorf("Record not found: %v", rs.Primary.ID)
			}
		} else {
			return fmt.Errorf("Invalid response or missing ID in the response")
		}

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
