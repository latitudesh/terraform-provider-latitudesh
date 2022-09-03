package latitude

import (
	"fmt"
	"os"
	"testing"

	api "github.com/capturealpha/latitude-api-client"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const (
	testServerHostname        = "test"
	testServerPlan            = "c2-medium-x86"
	testServerSite            = "MI2"
	testServerOperatingSystem = "ubuntu_20_04_x64_lts"
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
	return fmt.Sprintf(`
resource "latitude_server" "test_item" {
	project_id = "%s"
  hostname = "%s"
	plan     = "%s"
	site     = "%s"
	operating_system = "%s"
}
`,
		os.Getenv("LATITUDE_TEST_PROJECT_ID"),
		testServerHostname,
		testServerPlan,
		testServerSite,
		testServerOperatingSystem,
	)
}
