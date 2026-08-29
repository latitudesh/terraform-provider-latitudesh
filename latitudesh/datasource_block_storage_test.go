package latitudesh

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestAccBlockStorageDataSource_ByID(t *testing.T) {
	// See TestAccBlockStorage_Basic: testAccProjectID() below reaches the
	// network eagerly, so this must gate on TF_ACC itself.
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	resourceName := "latitudesh_block_storage.test_item"
	dataSourceName := "data.latitudesh_block_storage.by_id"

	projectID := testAccProjectID()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckBlockStorageDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccBlockStorageDataSourceByIDConfig(projectID, testBlockStorageName, testBlockStorageRegion),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair(dataSourceName, "id", resourceName, "id"),
					resource.TestCheckResourceAttrPair(dataSourceName, "name", resourceName, "name"),
					resource.TestCheckResourceAttrPair(dataSourceName, "size_in_gb", resourceName, "size_in_gb"),
				),
			},
		},
	})
}

func testAccBlockStorageDataSourceByIDConfig(project, name, region string) string {
	return testAccBlockStorageConfig(project, name, region) + `
data "latitudesh_block_storage" "by_id" {
  id = latitudesh_block_storage.test_item.id
}
`
}
