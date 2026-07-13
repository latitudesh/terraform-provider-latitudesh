package latitudesh

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

const (
	testUserDataDescription = "test description"
	// base64 of a cloud-config YAML hash (the API requires the decoded
	// content to be a valid YAML hash):
	//   #cloud-config
	//   runcmd:
	//     - echo "Hello from Terraform acceptance test"
	testUserDataContent = "I2Nsb3VkLWNvbmZpZwpydW5jbWQ6CiAgLSBlY2hvICJIZWxsbyBmcm9tIFRlcnJhZm9ybSBhY2NlcHRhbmNlIHRlc3QiCg=="
)

func TestAccUserDataBasic(t *testing.T) {
	var userData components.UserDataProperties

	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckUserDataDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckUserDataBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckUserDataExists("latitudesh_user_data.test_item", &userData),
					resource.TestCheckResourceAttr(
						"latitudesh_user_data.test_item", "description", testUserDataDescription),
					resource.TestCheckResourceAttr(
						"latitudesh_user_data.test_item", "content", testUserDataContent),
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
	client := createVCRClient(nil)
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

		client := createVCRClient(nil)

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
			description = "%s"
			content     = "%s"
		}
	`,
		testUserDataDescription,
		testUserDataContent,
	)
}
