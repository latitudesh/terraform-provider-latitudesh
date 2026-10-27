package latitudesh

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

const testRegionSlug = "ASH"

func TestAccRegion_Basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		Steps: []resource.TestStep{
			{
				// Slug lookup for a region that reports capabilities: exercises
				// the paginated list path and the features conversion.
				Config: testAccCheckRegionBasic(testRegionSlug),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"data.latitudesh_region.test", "slug", testRegionSlug),
					resource.TestCheckResourceAttr(
						"data.latitudesh_region.test", "features.#", "2"),
					resource.TestCheckResourceAttr(
						"data.latitudesh_region.test", "features.0", "public_network"),
					resource.TestCheckResourceAttr(
						"data.latitudesh_region.test", "features.1", "elastic_ip_bgp"),
				),
			},
			{
				// Slug lookup for a region with no capabilities: features must
				// resolve to a known empty list rather than null.
				Config: testAccCheckRegionBasic("NYC"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"data.latitudesh_region.test", "slug", "NYC"),
					resource.TestCheckResourceAttr(
						"data.latitudesh_region.test", "features.#", "0"),
				),
			},
			{
				// ID lookup: exercises the Fetch path and confirms features are
				// mapped there too.
				Config: testAccCheckRegionByID("reg_ash_001"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"data.latitudesh_region.test", "slug", testRegionSlug),
					resource.TestCheckResourceAttr(
						"data.latitudesh_region.test", "features.#", "2"),
					resource.TestCheckResourceAttr(
						"data.latitudesh_region.test", "features.0", "public_network"),
					resource.TestCheckResourceAttr(
						"data.latitudesh_region.test", "features.1", "elastic_ip_bgp"),
				),
			},
		},
	})
}

func testAccCheckRegionBasic(slug string) string {
	return fmt.Sprintf(`
data "latitudesh_region" "test" {
	slug = "%s"
}
`,
		slug,
	)
}

func testAccCheckRegionByID(id string) string {
	return fmt.Sprintf(`
data "latitudesh_region" "test" {
	id = "%s"
}
`,
		id,
	)
}
