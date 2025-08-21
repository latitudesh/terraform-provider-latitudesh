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

		_, err := client.PrivateNetworks.Get(ctx, rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("virtual network still exists")
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

		response, err := client.PrivateNetworks.Get(ctx, rs.Primary.ID)
		if err != nil {
			return err
		}

		if response.Object == nil || response.Object.Data == nil {
			return fmt.Errorf("virtual network not found")
		}

		vnet := response.Object.Data
		vnData := vnet.GetData()

		if vnData == nil || vnData.GetID() == nil || *vnData.GetID() != rs.Primary.ID {
			return fmt.Errorf("Record not found: %v", rs.Primary.ID)
		}

		return nil
	}
}

func testAccCheckVirtualNetworkBasic() string {
	return fmt.Sprintf(`
resource "latitudesh_virtual_network" "test_item" {
	project     = "%s"
	description = "%s"
	site        = "%s"
}
`,
		os.Getenv("LATITUDESH_TEST_PROJECT"),
		testVirtualNetworkDescription,
		testVirtualNetworkSite,
	)
}
