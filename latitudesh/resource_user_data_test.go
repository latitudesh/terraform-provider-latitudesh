package latitudesh

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

const (
	testUserDataDescription = "test description"
)

func TestAccUserDataBasic(t *testing.T) {
	var userData components.UserDataProperties

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
					resource.TestCheckResourceAttrSet(
						"latitudesh_user_data.test_item", "created_at"),
					resource.TestCheckResourceAttrSet(
						"latitudesh_user_data.test_item", "updated_at"),
				),
			},
		},
	})
}

func testAccCheckUserDataDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*latitudeshgosdk.Latitudesh)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_user_data" {
			continue
		}
		if _, err := client.UserData.Get(context.Background(), rs.Primary.Attributes["project"], rs.Primary.ID, nil); err == nil {
			return fmt.Errorf("User data still exists")
		}
	}

	return nil
}

func testAccCheckUserDataExists(n string, userData *components.UserDataProperties) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		client := testAccProvider.Meta().(*latitudeshgosdk.Latitudesh)

		response, err := client.UserData.Get(context.Background(), rs.Primary.Attributes["project"], rs.Primary.ID, nil)
		if err != nil {
			return err
		}

		if response.UserData == nil || response.UserData.Data == nil {
			return fmt.Errorf("User data not found in response")
		}

		foundUserData := response.UserData.Data
		if foundUserData.ID == nil || *foundUserData.ID != rs.Primary.ID {
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
