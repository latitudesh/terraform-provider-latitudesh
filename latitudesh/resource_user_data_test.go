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
	ctx := context.Background()

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_user_data" {
			continue
		}

		_, err := client.UserData.Retrieve(ctx, rs.Primary.ID, nil)
		if err == nil {
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

		ctx := context.Background()
		response, err := client.UserData.Retrieve(ctx, rs.Primary.ID, nil)
		if err != nil {
			return err
		}

		if response.UserDataObject == nil || response.UserDataObject.Data == nil {
			return fmt.Errorf("user data not found")
		}

		*userData = *response.UserDataObject.Data

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
