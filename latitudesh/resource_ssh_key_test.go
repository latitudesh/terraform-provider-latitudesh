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
	testSSHKeyName = "test"
)

func TestAccSSHKey_Basic(t *testing.T) {
	var sshKey api.SSHKey

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
			testAccProjectCheck(t)
			testAccSSHKeyCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckSSHKeyDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckSSHKeyBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckSSHKeyExists("latitudesh_ssh_key.test_item", &sshKey),
					resource.TestCheckResourceAttr(
						"latitudesh_ssh_key.test_item", "name", testSSHKeyName),
					resource.TestCheckResourceAttr(
						"latitudesh_ssh_key.test_item", "public_key", os.Getenv("LATITUDESH_TEST_SSH_PUBLIC_KEY")),
				),
			},
		},
	})
}

func testAccCheckSSHKeyDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*api.Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_ssh_key" {
			continue
		}
		if _, _, err := client.SSHKeys.Get(rs.Primary.ID, rs.Primary.Attributes["project"], nil); err == nil {
			return fmt.Errorf("SSH key still exists")
		}
	}

	return nil
}

func testAccCheckSSHKeyExists(n string, sshKey *api.SSHKey) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		client := testAccProvider.Meta().(*api.Client)

		foundSSHKey, _, err := client.SSHKeys.Get(rs.Primary.ID, rs.Primary.Attributes["project"], nil)
		if err != nil {
			return err
		}

		if foundSSHKey.ID != rs.Primary.ID {
			return fmt.Errorf("Record not found: %v - %v", rs.Primary.ID, foundSSHKey)
		}

		*sshKey = *foundSSHKey

		return nil
	}
}

func testAccCheckSSHKeyBasic() string {
	return fmt.Sprintf(`
resource "latitudesh_ssh_key" "test_item" {
	project  	= "%s"
  	name        = "%s"
  	public_key  = "%s"
}
`,
		os.Getenv("LATITUDESH_TEST_PROJECT"),
		testSSHKeyName,
		os.Getenv("LATITUDESH_TEST_SSH_PUBLIC_KEY"),
	)
}
