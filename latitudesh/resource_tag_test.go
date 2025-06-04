package latitudesh

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
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
	client := testAccProvider.Meta().(*latitudeshgosdk.Latitudesh)
	ctx := context.Background()

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_tag" {
			continue
		}
		tag, err := GetTag(ctx, client, rs.Primary.ID)
		if err == nil && tag != nil {
			return fmt.Errorf("Tag still exists")
		}
	}

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

		client := testAccProvider.Meta().(*latitudeshgosdk.Latitudesh)
		ctx := context.Background()

		tag, err := GetTag(ctx, client, rs.Primary.ID)
		if err != nil {
			return err
		}

		if tag.ID == nil || *tag.ID != rs.Primary.ID {
			return fmt.Errorf("Record not found: %v - %v", rs.Primary.ID, tag)
		}

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
