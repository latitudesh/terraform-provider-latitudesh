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
				Config: testAccCheckRegionBasic(testRegionSlug),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"data.latitudesh_region.test", "slug", testRegionSlug),
				),
			},
			{
				Config: testAccCheckRegionBasic("NYC"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"data.latitudesh_region.test", "slug", "NYC"),
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
