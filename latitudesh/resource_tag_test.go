package latitudesh

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	api "github.com/latitudesh/latitudesh-go"
)

const (
	testTagName        = "test_tag"
	testTagDescription = "terraform test tag"
	testTagColor       = "#ffffff"
)

func TestAccTag_Basic(t *testing.T) {
	var Tag api.Tag

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
					testAccCheckTagExists("latitudesh_tag.test_item", &Tag),
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
	client := testAccProvider.Meta().(*api.Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_tag" {
			continue
		}
		if _, _, err := GetTag(client, rs.Primary.ID); err == nil {
			return fmt.Errorf("Tag still exists")
		}
	}

	return nil
}

func testAccCheckTagExists(n string, tag *api.Tag) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		client := testAccProvider.Meta().(*api.Client)

		foundTag, _, err := GetTag(client, rs.Primary.ID)
		if err != nil {
			return err
		}

		if foundTag.ID != rs.Primary.ID {
			return fmt.Errorf("Record not found: %v - %v", rs.Primary.ID, foundTag)
		}

		*tag = *foundTag

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
