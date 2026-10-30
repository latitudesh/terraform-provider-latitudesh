package latitudesh

// Mock-backed tests reuse mockFilesystemStorageAPI and
// testAccProtoV6ProviderFactoriesWithMock from
// resource_filesystem_storage_test.go / resource_virtual_machine_site_test.go.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func testAccFilesystemStorageDataSourceByIDConfig(project, name, region string, sizeInGb int) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_filesystem_storage" "source" {
  project    = %q
  name       = %q
  region     = %q
  size_in_gb = %d
  protocols  = ["nfs4"]
}

data "latitudesh_filesystem_storage" "by_id" {
  id = latitudesh_filesystem_storage.source.id
}
`, project, name, region, sizeInGb)
}

func TestFilesystemStorageDataSource_ByID(t *testing.T) {
	mock := newMockFilesystemStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccFilesystemStorageDataSourceByIDConfig("test-project", "shared-data", "ASH", 1500),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.latitudesh_filesystem_storage.by_id", "id", "latitudesh_filesystem_storage.source", "id"),
					resource.TestCheckResourceAttr("data.latitudesh_filesystem_storage.by_id", "name", "shared-data"),
					resource.TestCheckResourceAttr("data.latitudesh_filesystem_storage.by_id", "region", "ASH"),
					resource.TestCheckResourceAttr("data.latitudesh_filesystem_storage.by_id", "size_in_gb", "1500"),
					resource.TestCheckResourceAttr("data.latitudesh_filesystem_storage.by_id", "storage_class", "standard"),
				),
			},
		},
	})
}

// An `id` lookup must report the owning project the API returns, not whatever
// `project` the config happens to carry: the value is a filter for the `name`
// branch only, and echoing it back would hand callers a project the
// filesystem does not belong to.
func TestFilesystemStorageDataSource_ByIDReportsOwningProject(t *testing.T) {
	mock := newMockFilesystemStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	// The mock answers with project {id: <configured>, slug: "test-project"},
	// so the owner's label is the slug either way.
	config := `
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_filesystem_storage" "source" {
  project    = "proj_abc"
  name       = "shared-data"
  region     = "ASH"
  size_in_gb = 1500
  protocols  = ["nfs4"]
}

data "latitudesh_filesystem_storage" "by_id" {
  id      = latitudesh_filesystem_storage.source.id
  project = "not-the-owning-project"
}
`

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.TestCheckResourceAttr(
					"data.latitudesh_filesystem_storage.by_id", "project", "test-project",
				),
			},
		},
	})
}

func testAccFilesystemStorageDataSourceByNameConfig(project, name, region string, sizeInGb int) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_filesystem_storage" "source" {
  project    = %q
  name       = %q
  region     = %q
  size_in_gb = %d
  protocols  = ["nfs4"]
}

data "latitudesh_filesystem_storage" "by_name" {
  name    = latitudesh_filesystem_storage.source.name
  project = %q
}
`, project, name, region, sizeInGb, project)
}

func TestFilesystemStorageDataSource_ByName(t *testing.T) {
	mock := newMockFilesystemStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccFilesystemStorageDataSourceByNameConfig("test-project", "shared-data", "ASH", 1500),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.latitudesh_filesystem_storage.by_name", "id", "latitudesh_filesystem_storage.source", "id"),
					resource.TestCheckResourceAttr("data.latitudesh_filesystem_storage.by_name", "region", "ASH"),
					resource.TestCheckResourceAttr("data.latitudesh_filesystem_storage.by_name", "project", "test-project"),
				),
			},
		},
	})
}

// A name lookup that matches nothing must fail with a clear error, not a
// silent empty result.
func TestFilesystemStorageDataSource_NameNotFound(t *testing.T) {
	mock := newMockFilesystemStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	config := `
provider "latitudesh" {
  auth_token = "mock-token"
}

data "latitudesh_filesystem_storage" "missing" {
  name = "does-not-exist"
}
`

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`(?s)Filesystem storage not found`),
			},
		},
	})
}
