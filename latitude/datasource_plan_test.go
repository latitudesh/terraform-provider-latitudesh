package latitude

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

const testPlanName = "c2.large.x86"

func TestAccPlan_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:  func() { testAccPreCheck(t) },
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckPlanBasic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"data.latitude_plan.test", "name", testPlanName),
				),
			},
		},
	})
}

func testAccCheckPlanBasic() string {
	return fmt.Sprintf(`
data "latitude_plan" "test" {
	name = "%s"
}
`,
		testPlanName,
	)
}
