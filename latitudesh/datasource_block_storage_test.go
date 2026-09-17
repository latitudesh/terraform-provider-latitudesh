package latitudesh

// Mock-backed tests reuse mockBlockStorageAPI and
// testAccProtoV6ProviderFactoriesWithMock from resource_block_storage_test.go
// / resource_virtual_machine_site_test.go.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func testAccBlockStorageDataSourceByIDConfig(project, name, region string, sizeInGb int) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_block_storage" "source" {
  project    = %q
  name       = %q
  region     = %q
  size_in_gb = %d
}

data "latitudesh_block_storage" "by_id" {
  id = latitudesh_block_storage.source.id
}
`, project, name, region, sizeInGb)
}

func TestBlockStorageDataSource_ByID(t *testing.T) {
	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccBlockStorageDataSourceByIDConfig("test-project", "app-data", "ASH", 100),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.latitudesh_block_storage.by_id", "id", "latitudesh_block_storage.source", "id"),
					resource.TestCheckResourceAttr("data.latitudesh_block_storage.by_id", "name", "app-data"),
					resource.TestCheckResourceAttr("data.latitudesh_block_storage.by_id", "region", "ASH"),
					resource.TestCheckResourceAttr("data.latitudesh_block_storage.by_id", "size_in_gb", "100"),
					resource.TestCheckResourceAttr("data.latitudesh_block_storage.by_id", "namespace_id", "ns_mock_1"),
				),
			},
		},
	})
}

func testAccBlockStorageDataSourceByNameConfig(project, name, region string, sizeInGb int) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_block_storage" "source" {
  project    = %q
  name       = %q
  region     = %q
  size_in_gb = %d
}

data "latitudesh_block_storage" "by_name" {
  name    = latitudesh_block_storage.source.name
  project = %q
}
`, project, name, region, sizeInGb, project)
}

func TestBlockStorageDataSource_ByName(t *testing.T) {
	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccBlockStorageDataSourceByNameConfig("test-project", "app-data", "ASH", 100),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.latitudesh_block_storage.by_name", "id", "latitudesh_block_storage.source", "id"),
					resource.TestCheckResourceAttr("data.latitudesh_block_storage.by_name", "region", "ASH"),
					resource.TestCheckResourceAttr("data.latitudesh_block_storage.by_name", "project", "test-project"),
				),
			},
		},
	})
}

// A name lookup that matches nothing must fail with a clear error, not a
// silent empty result.
func TestBlockStorageDataSource_NameNotFound(t *testing.T) {
	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: `
provider "latitudesh" {
  auth_token = "mock-token"
}

data "latitudesh_block_storage" "missing" {
  name = "does-not-exist"
}
`,
				ExpectError: regexp.MustCompile(`Block storage volume not found`),
			},
		},
	})
}

func TestAccBlockStorageDataSource_Basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	projectSlug := testAccProjectSlug(t)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckBlockStorageDestroy,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "latitudesh_block_storage" "source" {
  project    = %q
  name       = "tf-acc-block-storage-ds-%s"
  region     = %q
  size_in_gb = 50
}

data "latitudesh_block_storage" "by_id" {
  id = latitudesh_block_storage.source.id
}
`, projectSlug, testRunID, testBlockStorageSite),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.latitudesh_block_storage.by_id", "id", "latitudesh_block_storage.source", "id"),
					resource.TestCheckResourceAttrPair("data.latitudesh_block_storage.by_id", "size_in_gb", "latitudesh_block_storage.source", "size_in_gb"),
				),
			},
		},
	})
}
