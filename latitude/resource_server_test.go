package latitude

import (
	"fmt"
	"testing"

	api "github.com/capturealpha/latitude-api-client"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccServer_Basic(t *testing.T) {
	var server api.ServerGetResponse

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckServerDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckServerBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckServerExists("latitude_server.test_item", &server),
					resource.TestCheckResourceAttr(
						"latitude_server.test_item", "hostname", "test"),
				),
			},
		},
	})

}

func testAccCheckServerDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*api.Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitude_server" {
			continue
		}
		if _, _, err := client.Servers.Get(rs.Primary.ID, nil); err == nil {
			return fmt.Errorf("Server still exists")
		}
	}

	return nil
}

func testAccCheckServerExists(n string, server *api.ServerGetResponse) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		client := testAccProvider.Meta().(*api.Client)

		foundServer, _, err := client.Servers.Get(rs.Primary.ID, nil)
		if err != nil {
			return err
		}

		if foundServer.Data.ID != rs.Primary.ID {
			return fmt.Errorf("Record not found: %v - %v", rs.Primary.ID, foundServer)
		}

		*server = *foundServer

		return nil
	}
}

func testAccCheckServerBasic() string {
	return `
resource "latitude_server" "test_item" {
	project_id = "4167"
  hostname = "test"
	plan     = "c3-medium-x86"
	site     = "NY2"
	operating_system = "ubuntu_20_04_x64_lts"
}
`
}
