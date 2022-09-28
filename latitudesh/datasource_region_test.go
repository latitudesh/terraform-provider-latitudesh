package latitude

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

const testRegionSlug = "ASH"

func TestAccRegion_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:  func() { testAccPreCheck(t) },
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckRegionBasic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"data.latitude_region.test", "slug", testRegionSlug),
				),
			},
		},
	})
}

func testAccCheckRegionBasic() string {
	return fmt.Sprintf(`
data "latitude_region" "test" {
	slug = "%s"
}
`,
		testRegionSlug,
	)
}
