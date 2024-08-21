package latitudesh

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

const testRegionSlug = "ASH"

func TestAccRegion_Basic(t *testing.T) {

	recorder, teardown := createTestRecorder(t)
	defer teardown()
	testAccProviders["latitudesh"].ConfigureContextFunc = testProviderConfigure(recorder)

	resource.Test(t, resource.TestCase{
		PreCheck:  func() { testAccTokenCheck(t) },
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckRegionBasic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"data.latitudesh_region.test", "slug", testRegionSlug),
				),
			},
		},
	})
}

func testAccCheckRegionBasic() string {
	return fmt.Sprintf(`
data "latitudesh_region" "test" {
	slug = "%s"
}
`,
		testRegionSlug,
	)
}
