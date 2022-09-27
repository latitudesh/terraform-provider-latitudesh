package latitude

import (
	"fmt"
	"os"
	"testing"

	api "github.com/maxihost/latitudesh-go"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const (
	testSSHKeyName = "test"
)

func TestAccSSHKey_Basic(t *testing.T) {
	var sshKey api.SSHKeyGetResponse

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckSSHKeyDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckSSHKeyBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckSSHKeyExists("latitude_ssh_key.test_item", &sshKey),
					resource.TestCheckResourceAttr(
						"latitude_ssh_key.test_item", "name", testSSHKeyName),
					resource.TestCheckResourceAttr(
						"latitude_ssh_key.test_item", "public_key", os.Getenv("LATITUDE_TEST_SSH_PUBLIC_KEY")),
				),
			},
		},
	})
}

func testAccCheckSSHKeyDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*api.Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitude_ssh_key" {
			continue
		}
		if _, _, err := client.SSHKeys.Get(rs.Primary.ID, rs.Primary.Attributes["project_id"], nil); err == nil {
			return fmt.Errorf("SSH key still exists")
		}
	}

	return nil
}

func testAccCheckSSHKeyExists(n string, sshKey *api.SSHKeyGetResponse) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		client := testAccProvider.Meta().(*api.Client)

		foundSSHKey, _, err := client.SSHKeys.Get(rs.Primary.ID, rs.Primary.Attributes["project_id"], nil)
		if err != nil {
			return err
		}

		if foundSSHKey.Data.ID != rs.Primary.ID {
			return fmt.Errorf("Record not found: %v - %v", rs.Primary.ID, foundSSHKey)
		}

		*sshKey = *foundSSHKey

		return nil
	}
}

func testAccCheckSSHKeyBasic() string {
	return fmt.Sprintf(`
resource "latitude_ssh_key" "test_item" {
	project_id  = "%s"
  name        = "%s"
  public_key  = "%s"
}
`,
		os.Getenv("LATITUDE_TEST_PROJECT_ID"),
		testSSHKeyName,
		os.Getenv("LATITUDE_TEST_SSH_PUBLIC_KEY"),
	)
}
