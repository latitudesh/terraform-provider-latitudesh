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
	testAccProviders["latitudesh"].ConfigureContextFunc = testProviderConfigure(recorder)

	resource.Test(t, resource.TestCase{
		PreCheck:  func() { testAccTokenCheck(t) },
		Providers: testAccProviders,
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
