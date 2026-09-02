package latitudesh

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const (
	testObjectStorageName   = "tf-acc-object-storage"
	testObjectStorageRegion = "ASH"
)

func TestAccObjectStorage_Basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	resourceName := "latitudesh_object_storage.test_item"
	projectID := testAccProjectID()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckObjectStorageDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccConfigObjectStorage(projectID, testObjectStorageName, testObjectStorageRegion),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "name", testObjectStorageName),
					resource.TestCheckResourceAttr(resourceName, "region", testObjectStorageRegion),
					resource.TestCheckResourceAttr(resourceName, "project", projectID),
					resource.TestCheckResourceAttrSet(resourceName, "bucket_name"),
					resource.TestCheckResourceAttrSet(resourceName, "endpoint"),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"project"},
			},
		},
	})
}

func testAccConfigObjectStorage(projectID, name, region string) string {
	return fmt.Sprintf(`
resource "latitudesh_object_storage" "test_item" {
  project = %q
  name    = %q
  region  = %q
}
`, projectID, name, region)
}

func testAccCheckObjectStorageDestroy(s *terraform.State) error {
	ctx := context.Background()
	client, err := newSDKClientFromEnv()
	if err != nil {
		return err
	}

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_object_storage" {
			continue
		}
		id := rs.Primary.ID
		if id == "" {
			continue
		}

		resp, err := client.ObjectStorage.GetStorageBucket(ctx, id)
		if err == nil && resp != nil && resp.Object != nil && resp.Object.Data != nil &&
			resp.Object.Data.ID != nil && *resp.Object.Data.ID == id {
			return fmt.Errorf("object storage still exists: %s", id)
		}
	}
	return nil
}
