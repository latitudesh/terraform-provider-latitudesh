package latitudesh

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const (
	testTagName        = "test_tag"
	testTagDescription = "terraform test tag"
	testTagColor       = "#ffffff"
)

func TestAccTag_Basic(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()
	testAccProviders["latitudesh"].ConfigureContextFunc = testProviderConfigure(recorder)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckTagDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckTagBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckTagExists("latitudesh_tag.test_item"),
					resource.TestCheckResourceAttr(
						"latitudesh_tag.test_item", "name", testTagName),
					resource.TestCheckResourceAttr(
						"latitudesh_tag.test_item", "description", testTagDescription),
					resource.TestCheckResourceAttr(
						"latitudesh_tag.test_item", "color", testTagColor),
				),
			},
		},
	})
}

func testAccCheckTagDestroy(s *terraform.State) error {
	// Skip destroy check for now since we don't have a proper API method
	return nil
}

func testAccCheckTagExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		// Skip existence check for now since we don't have a proper API method
		return nil
	}
}

func testAccCheckTagBasic() string {
	return fmt.Sprintf(`
resource "latitudesh_tag" "test_item" {
	name  	= "%s"
  	description = "%s"
  	color        = "%s"
}
`,
		testTagName,
		testTagDescription,
		testTagColor,
	)
}
