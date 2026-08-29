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
	testBlockStorageName   = "tf-acc-block-storage"
	testBlockStorageRegion = "SAO2"
)

func TestAccBlockStorage_Basic(t *testing.T) {
	// Unlike resource.Test, testAccProjectID() below runs before the test case
	// is built and reaches the network eagerly, so this must gate on TF_ACC
	// itself to keep plain `go test` runs offline (mirrors
	// TestAccVirtualNetwork_Basic).
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	resourceName := "latitudesh_block_storage.test_item"

	// This test exercises project-attribute-on-resource; use the shared
	// pre-existing project instead of creating a throwaway one.
	projectID := testAccProjectID()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckBlockStorageDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccBlockStorageConfig(projectID, testBlockStorageName, testBlockStorageRegion),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "name", testBlockStorageName),
					resource.TestCheckResourceAttr(resourceName, "project", projectID),
					resource.TestCheckResourceAttr(resourceName, "region", testBlockStorageRegion),
					resource.TestCheckResourceAttrSet(resourceName, "size_in_gb"),
				),
			},
			{
				// Idempotency — same config, plan must be empty.
				Config:             testAccBlockStorageConfig(projectID, testBlockStorageName, testBlockStorageRegion),
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}

func testAccCheckBlockStorageDestroy(s *terraform.State) error {
	ctx := context.Background()
	client, err := newSDKClientFromEnv()
	if err != nil {
		return err
	}

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_block_storage" {
			continue
		}
		id := rs.Primary.ID
		if id == "" {
			continue
		}

		resp, err := client.BlockStorage.GetStorageVolume(ctx, id)
		if err == nil && resp != nil && resp.Object != nil && resp.Object.Data != nil {
			return fmt.Errorf("block storage volume still exists: %s", id)
		}
	}
	return nil
}

func testAccBlockStorageConfig(project, name, region string) string {
	return fmt.Sprintf(`
resource "latitudesh_block_storage" "test_item" {
  project = %q
  name    = %q
  region  = %q
}
`, project, name, region)
}
