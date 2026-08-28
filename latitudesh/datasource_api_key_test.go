package latitudesh

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestAccAPIKeyDataSource_ByID(t *testing.T) {
	name := fmt.Sprintf("%s-ds-id-%s", testAPIKeyName, testRunID)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckAPIKeyDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccConfigAPIKeyDataSourceByID(name),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair(
						"data.latitudesh_api_key.by_id", "id",
						"latitudesh_api_key.test_item", "id",
					),
					resource.TestCheckResourceAttr("data.latitudesh_api_key.by_id", "name", name),
				),
			},
		},
	})
}

func TestAccAPIKeyDataSource_ByName(t *testing.T) {
	name := fmt.Sprintf("%s-ds-name-%s", testAPIKeyName, testRunID)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckAPIKeyDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccConfigAPIKeyDataSourceByName(name),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair(
						"data.latitudesh_api_key.by_name", "id",
						"latitudesh_api_key.test_item", "id",
					),
				),
			},
		},
	})
}

func testAccConfigAPIKeyDataSourceByID(name string) string {
	return fmt.Sprintf(`
resource "latitudesh_api_key" "test_item" {
  name = "%s"
}

data "latitudesh_api_key" "by_id" {
  id = latitudesh_api_key.test_item.id
}
`, name)
}

func testAccConfigAPIKeyDataSourceByName(name string) string {
	return fmt.Sprintf(`
resource "latitudesh_api_key" "test_item" {
  name = "%s"
}

data "latitudesh_api_key" "by_name" {
  name = latitudesh_api_key.test_item.name
}
`, name)
}
