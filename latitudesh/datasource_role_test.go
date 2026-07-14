package latitudesh

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

const testRoleName = "collaborator"

func TestAccRole_Basic(t *testing.T) {

	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		Steps: []resource.TestStep{
			{
				Config: testAccCheckRoleBasic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"data.latitudesh_role.test", "name", testRoleName),
				),
			},
		},
	})
}

func testAccCheckRoleBasic() string {
	return fmt.Sprintf(`
data "latitudesh_role" "test" {
	name = "%s"
}
`,
		testRoleName,
	)
}
