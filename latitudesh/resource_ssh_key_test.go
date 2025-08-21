package latitudesh

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const (
	testSSHKeyName = "qa-terraform-ssh-key"
)

func TestAccSSHKey_Basic(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	pub := os.Getenv("LATITUDESH_TEST_SSH_PUBLIC_KEY")
	if pub == "" {
		t.Skip("LATITUDESH_TEST_SSH_PUBLIC_KEY not set")
	}

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckSSHKeyDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccSSHKeyBasic(pub),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckSSHKeyExists("latitudesh_ssh_key.test_item"),
					resource.TestCheckResourceAttr("latitudesh_ssh_key.test_item", "name", testSSHKeyName),
					resource.TestCheckResourceAttr("latitudesh_ssh_key.test_item", "public_key", pub),
					resource.TestCheckResourceAttrSet("latitudesh_ssh_key.test_item", "fingerprint"),
					resource.TestCheckResourceAttrSet("latitudesh_ssh_key.test_item", "created_at"),
				),
			},
		},
	})
}

func TestAccSSHKey_Import(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	pub := os.Getenv("LATITUDESH_TEST_SSH_PUBLIC_KEY")
	if pub == "" {
		t.Skip("LATITUDESH_TEST_SSH_PUBLIC_KEY not set")
	}

	resourceName := "latitudesh_ssh_key.test_item"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckSSHKeyDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccSSHKeyBasic(pub),
				Check:  testAccCheckSSHKeyExists(resourceName),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"tags"},
			},
			{
				Config:             testAccSSHKeyBasicNoTags(pub),
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}

func testAccSSHKeyBasicNoTags(pub string) string {
	return fmt.Sprintf(`
  resource "latitudesh_ssh_key" "test_item" {
	name       = %q
	public_key = %q
  }
  `, testSSHKeyName, pub)
}

func TestAccSSHKey_ImportNotFound(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	pub := os.Getenv("LATITUDESH_TEST_SSH_PUBLIC_KEY")
	if pub == "" {
		t.Skip("LATITUDESH_TEST_SSH_PUBLIC_KEY not set")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckSSHKeyDestroy,
		Steps: []resource.TestStep{
			{Config: testAccSSHKeyPlaceholder(pub)},
			{
				ResourceName:  "latitudesh_ssh_key.placeholder",
				ImportState:   true,
				ImportStateId: "ssh_invalid",
				ExpectError:   regexp.MustCompile(`(?i)ssh key not found|no ssh key exists`),
			},
		},
	})
}

func testAccSSHKeyPlaceholder(pub string) string {
	return fmt.Sprintf(`
resource "latitudesh_ssh_key" "placeholder" {
  name       = "placeholder"
  public_key = %q
}
`, pub)
}

func TestAccSSHKey_MissingRequiredFields(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckSSHKeyDestroy,
		Steps: []resource.TestStep{
			{
				Config:      testAccSSHKeyMissing(),
				ExpectError: regexp.MustCompile(`(?i)(name|public_key).*required`),
			},
		},
	})
}

func testAccCheckSSHKeyDestroy(s *terraform.State) error {
	return nil
}

func testAccCheckSSHKeyExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		client := createVCRClient(nil)
		ctx := context.Background()

		resp, err := client.SSHKeys.Retrieve(ctx, rs.Primary.ID)
		if err != nil {
			return fmt.Errorf("error retrieving ssh key: %w", err)
		}
		if resp.Object == nil || resp.Object.Data == nil {
			return fmt.Errorf("ssh key not found")
		}
		return nil
	}
}

func testAccSSHKeyBasic(pub string) string {
	return fmt.Sprintf(`
resource "latitudesh_ssh_key" "test_item" {
  name       = %q
  public_key = %q
  tags       = ["qa-terraform-tag"]
}
`, testSSHKeyName, pub)
}

func testAccSSHKeyMissing() string {
	return `
resource "latitudesh_ssh_key" "test_item" {}
`
}
