package latitudesh

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestAccDataSourceSSHKey_ByName(t *testing.T) {
	t.Parallel()

	pub := os.Getenv("LATITUDESH_TEST_SSH_PUBLIC_KEY")
	if pub == "" {
		t.Skip("LATITUDESH_TEST_SSH_PUBLIC_KEY not set; skipping acceptance test")
	}

	name := fmt.Sprintf("acc-ds-sshkey-name-%s", acctest.RandString(6))

	cfg := fmt.Sprintf(`
resource "latitudesh_ssh_key" "test" {
  name       = %q
  public_key = %q
}

data "latitudesh_ssh_key" "by_name" {
  name = latitudesh_ssh_key.test.name
}

output "ssh_key_id" {
  value = data.latitudesh_ssh_key.by_name.id
}
`, name, pub)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_ssh_key.by_name", "name", name),
					resource.TestCheckResourceAttrSet("data.latitudesh_ssh_key.by_name", "id"),
					resource.TestCheckResourceAttrSet("data.latitudesh_ssh_key.by_name", "public_key"),
					resource.TestCheckResourceAttrSet("data.latitudesh_ssh_key.by_name", "fingerprint"),
					resource.TestCheckResourceAttrSet("data.latitudesh_ssh_key.by_name", "created_at"),
					resource.TestCheckResourceAttrSet("data.latitudesh_ssh_key.by_name", "updated_at"),
				),
			},
		},
	})
}

func testAccPreCheck(t *testing.T) {
	if v := os.Getenv("LATITUDESH_TEST_SSH_PUBLIC_KEY"); v == "" {
		t.Fatal("LATITUDESH_TEST_SSH_PUBLIC_KEY must be set for acceptance tests")
	}
}

func TestAccDataSourceSSHKey_ByID(t *testing.T) {
	t.Parallel()

	pub := os.Getenv("LATITUDESH_TEST_SSH_PUBLIC_KEY")
	if pub == "" {
		t.Skip("LATITUDESH_TEST_SSH_PUBLIC_KEY not set; skipping acceptance test")
	}

	name := fmt.Sprintf("acc-ds-sshkey-id-%s", acctest.RandString(6))

	cfg1 := fmt.Sprintf(`
resource "latitudesh_ssh_key" "test" {
  name       = %q
  public_key = %q
}
`, name, pub)

	cfg2 := fmt.Sprintf(`
resource "latitudesh_ssh_key" "test" {
  name       = %q
  public_key = %q
}

data "latitudesh_ssh_key" "by_id" {
  id = latitudesh_ssh_key.test.id
}
`, name, pub)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: cfg1},
			{
				Config: cfg2,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_ssh_key.test", "name", name),
					resource.TestCheckResourceAttrPair("data.latitudesh_ssh_key.by_id", "id", "latitudesh_ssh_key.test", "id"),
					resource.TestCheckResourceAttr("data.latitudesh_ssh_key.by_id", "name", name),
					resource.TestCheckResourceAttrSet("data.latitudesh_ssh_key.by_id", "public_key"),
					resource.TestCheckResourceAttrSet("data.latitudesh_ssh_key.by_id", "fingerprint"),
				),
			},
		},
	})
}

func TestAccDataSourceSSHKey_ByFingerprint(t *testing.T) {
	t.Parallel()

	pub := os.Getenv("LATITUDESH_TEST_SSH_PUBLIC_KEY")
	if pub == "" {
		t.Skip("LATITUDESH_TEST_SSH_PUBLIC_KEY not set; skipping acceptance test")
	}

	name := fmt.Sprintf("acc-ds-sshkey-fp-%s", acctest.RandString(6))

	cfg1 := fmt.Sprintf(`
resource "latitudesh_ssh_key" "test" {
  name       = %q
  public_key = %q
}
`, name, pub)

	cfg2 := fmt.Sprintf(`
resource "latitudesh_ssh_key" "test" {
  name       = %q
  public_key = %q
}

data "latitudesh_ssh_key" "by_fp" {
  fingerprint = latitudesh_ssh_key.test.fingerprint
}
`, name, pub)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: cfg1},
			{
				Config: cfg2,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.latitudesh_ssh_key.by_fp", "fingerprint", "latitudesh_ssh_key.test", "fingerprint"),
					resource.TestCheckResourceAttrSet("data.latitudesh_ssh_key.by_fp", "id"),
					resource.TestCheckResourceAttrSet("data.latitudesh_ssh_key.by_fp", "name"),
					resource.TestCheckResourceAttrSet("data.latitudesh_ssh_key.by_fp", "public_key"),
				),
			},
		},
	})
}
