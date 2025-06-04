package latitudesh

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

const (
	testSSHKeyName = "test"
)

func TestAccSSHKey_Basic(t *testing.T) {
	var sshKey components.SSHKeyData

	recorder, teardown := createTestRecorder(t)
	defer teardown()
	testAccProviders["latitudesh"].ConfigureContextFunc = testProviderConfigure(recorder)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
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
					resource.TestCheckResourceAttrSet("latitudesh_ssh_key.test_item", "fingerprint"),
					resource.TestCheckResourceAttrSet("latitudesh_ssh_key.test_item", "created_at"),
				),
			},
		},
	})
}

func testAccCheckSSHKeyDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*latitudeshgosdk.Latitudesh)
	ctx := context.Background()

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_ssh_key" {
			continue
		}

		_, err := client.SSHKeys.GetSSHKey(ctx, rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("SSH key still exists")
		}

		// If we get a 404, the resource is gone
		if apiErr, ok := err.(*components.APIError); ok && apiErr.StatusCode == 404 {
			continue
		}

		return err
	}

	return nil
}

func testAccCheckSSHKeyExists(n string, sshKey *components.SSHKeyData) resource.TestCheckFunc {
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

		result, err := client.SSHKeys.GetSSHKey(ctx, rs.Primary.ID)
		if err != nil {
			return err
		}

		if result.Object == nil || result.Object.Data == nil || result.Object.Data.ID == nil || *result.Object.Data.ID != rs.Primary.ID {
			return fmt.Errorf("Record not found: %v - %v", rs.Primary.ID, result.Object)
		}

		*sshKey = *result.Object.Data

		return nil
	}
}

func testAccCheckSSHKeyBasic() string {
	return fmt.Sprintf(`
resource "latitudesh_ssh_key" "test_item" {
  	name        = "%s"
  	public_key  = "%s"
}
`,
		testSSHKeyName,
		os.Getenv("LATITUDESH_TEST_SSH_PUBLIC_KEY"),
	)
}
