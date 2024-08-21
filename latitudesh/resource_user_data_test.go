package latitudesh

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	api "github.com/latitudesh/latitudesh-go"
)

const (
	testUserDataDescription = "test description"
)

func TestAccUserDataBasic(t *testing.T) {
	var userData api.UserData

	recorder, teardown := createTestRecorder(t)
	defer teardown()
	testAccProviders["latitudesh"].ConfigureContextFunc = testProviderConfigure(recorder)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
			testAccProjectCheck(t)
			testAccUserDataCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckUserDataDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckUserDataBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckUserDataExists("latitudesh_user_data.test_item", &userData),
					resource.TestCheckResourceAttr(
						"latitudesh_user_data.test_item", "description", testUserDataDescription),
					resource.TestCheckResourceAttr(
						"latitudesh_user_data.test_item", "content", os.Getenv("LATITUDESH_TEST_USER_DATA_CONTENT")),
				),
			},
		},
	})
}

func testAccCheckUserDataDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*api.Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_user_data" {
			continue
		}
		if _, _, err := client.UserData.Get(rs.Primary.ID, rs.Primary.Attributes["project"], nil); err == nil {
			return fmt.Errorf("User data still exists")
		}
	}

	return nil
}

func testAccCheckUserDataExists(n string, userData *api.UserData) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		client := testAccProvider.Meta().(*api.Client)

		foundUserData, _, err := client.UserData.Get(rs.Primary.ID, rs.Primary.Attributes["project"], nil)
		if err != nil {
			return err
		}

		if foundUserData.ID != rs.Primary.ID {
			return fmt.Errorf("Record not found: %v - %v", rs.Primary.ID, foundUserData)
		}

		*userData = *foundUserData

		return nil
	}
}

func testAccCheckUserDataBasic() string {
	return fmt.Sprintf(`
		resource "latitudesh_user_data" "test_item" {
			project     = "%s"
			description = "%s"
			content     = "%s"
		}
	`,
		os.Getenv("LATITUDESH_TEST_PROJECT"),
		testUserDataDescription,
		os.Getenv("LATITUDESH_TEST_USER_DATA_CONTENT"),
	)
}
