package latitudesh

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

// TestAccTraffic_Basic reads traffic consumption and quota for the shared
// acceptance-test project over a fixed date window. Traffic is a read-only
// report (no create/delete lifecycle), so there is no CheckDestroy.
func TestAccTraffic_Basic(t *testing.T) {
	dataSourceName := "data.latitudesh_traffic.usage"
	projectID := testAccProjectID()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testAccConfigTraffic(projectID),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(dataSourceName, "id"),
					resource.TestCheckResourceAttr(dataSourceName, "project", projectID),
					resource.TestCheckResourceAttrSet(dataSourceName, "total_outbound_gb"),
					resource.TestCheckResourceAttrSet(dataSourceName, "regions.#"),
					resource.TestCheckResourceAttrSet(dataSourceName, "quota_per_project.#"),
				),
			},
		},
	})
}

func testAccConfigTraffic(project string) string {
	return fmt.Sprintf(`
data "latitudesh_traffic" "usage" {
  project  = %q
  date_gte = "2024-04-01T00:00:00Z"
  date_lte = "2024-04-30T23:59:59Z"
}
`, project)
}

// TestAccTraffic_ProviderDefaultProject exercises the resource-attr-over-
// provider-block defaulting: the data source omits `project` and must fall
// back to the provider-level default instead of querying every project.
func TestAccTraffic_ProviderDefaultProject(t *testing.T) {
	dataSourceName := "data.latitudesh_traffic.usage"
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

data "latitudesh_traffic" "usage" {
  date_gte = "2024-04-01T00:00:00Z"
  date_lte = "2024-04-30T23:59:59Z"
}
`, projectID),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(dataSourceName, "id"),
					resource.TestCheckResourceAttr(dataSourceName, "project", projectID),
				),
			},
		},
	})
}
