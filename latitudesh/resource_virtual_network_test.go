package latitudesh

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const (
	testVirtualNetworkDescription = "test"
	testVirtualNetworkSite        = "SAO2"
)

func TestAccVirtualNetwork_Basic(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
			testAccProjectCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckVirtualNetworkDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckVirtualNetworkBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckVirtualNetworkExists("latitudesh_virtual_network.test_item"),
					resource.TestCheckResourceAttr("latitudesh_virtual_network.test_item", "description", testVirtualNetworkDescription),
					resource.TestCheckResourceAttr("latitudesh_virtual_network.test_item", "site", testVirtualNetworkSite),
				),
			},
		},
	})
}

func testAccCheckVirtualNetworkDestroy(s *terraform.State) error {
	client := createVCRClient(nil)
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
			return fmt.Errorf("not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("no record ID is set")
		}

		client := createVCRClient(nil)
		ctx := context.Background()

		resp, err := client.PrivateNetworks.Get(ctx, rs.Primary.ID)
		if err != nil {
			return fmt.Errorf("error getting virtual network: %v", err)
		}
		if resp == nil {
			return fmt.Errorf("response is nil")
		}
		if resp.Object == nil {
			return fmt.Errorf("response.Object is nil")
		}
		if resp.Object.Data == nil {
			return fmt.Errorf("response.Object.Data is nil")
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
`, os.Getenv("LATITUDESH_TEST_PROJECT"), testVirtualNetworkDescription, testVirtualNetworkSite)
}
