package latitudesh

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

// TestAccBilling_Basic reads the usage of the shared acceptance-test project.
// Billing usage is a read-only report (no create/delete lifecycle), so there
// is no CheckDestroy — the test only asserts the data source populates the
// expected top-level attributes.
func TestAccBilling_Basic(t *testing.T) {
	dataSourceName := "data.latitudesh_billing.usage"
	projectID := testAccProjectID()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testAccConfigBilling(projectID),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(dataSourceName, "id"),
					resource.TestCheckResourceAttr(dataSourceName, "project", projectID),
					resource.TestCheckResourceAttrSet(dataSourceName, "amount"),
					resource.TestCheckResourceAttrSet(dataSourceName, "products.#"),
				),
			},
		},
	})
}

func testAccConfigBilling(project string) string {
	return fmt.Sprintf(`
data "latitudesh_billing" "usage" {
  project = %q
}
`, project)
}

// TestAccBilling_ProviderDefaultProject exercises the resource-attr-over-
// provider-block defaulting: the data source omits `project` and must fall
// back to the provider-level default instead of erroring "Missing project".
func TestAccBilling_ProviderDefaultProject(t *testing.T) {
	dataSourceName := "data.latitudesh_billing.usage"
	projectID := testAccProjectID()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "latitudesh" {
  project = %q
}

data "latitudesh_billing" "usage" {}
`, projectID),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(dataSourceName, "id"),
					resource.TestCheckResourceAttr(dataSourceName, "project", projectID),
				),
			},
		},
	})
}
