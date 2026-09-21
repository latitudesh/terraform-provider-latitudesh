package latitudesh

// Mock-backed tests reuse mockBlockStorageAPI and
// testAccProtoV6ProviderFactoriesWithMock from resource_block_storage_test.go
// / resource_virtual_machine_site_test.go.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestBlockStoragesDataSource_ProjectFilterNewestFirst(t *testing.T) {
	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	config := `
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_block_storage" "a" {
  project    = "test-project"
  name       = "vol-a"
  region     = "ASH"
  size_in_gb = 50
}

resource "latitudesh_block_storage" "b" {
  project    = "test-project"
  name       = "vol-b"
  region     = "ASH"
  size_in_gb = 75

  depends_on = [latitudesh_block_storage.a]
}

# A volume in a different project must not appear in the project-scoped list.
resource "latitudesh_block_storage" "other_project" {
  project    = "other-project"
  name       = "vol-other"
  region     = "ASH"
  size_in_gb = 25

  depends_on = [latitudesh_block_storage.b]
}

data "latitudesh_block_storages" "by_project" {
  project = "test-project"

  depends_on = [
    latitudesh_block_storage.a,
    latitudesh_block_storage.b,
    latitudesh_block_storage.other_project,
  ]
}
`

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_block_storages.by_project", "volumes.#", "2"),
					// b was created after a, so newest-first puts b at index 0.
					resource.TestCheckResourceAttrPair("data.latitudesh_block_storages.by_project", "volumes.0.id", "latitudesh_block_storage.b", "id"),
					resource.TestCheckResourceAttrPair("data.latitudesh_block_storages.by_project", "volumes.1.id", "latitudesh_block_storage.a", "id"),
					resource.TestCheckResourceAttr("data.latitudesh_block_storages.by_project", "id", "test-project"),
				),
			},
		},
	})
}

func TestBlockStoragesDataSource_AllWhenProjectOmitted(t *testing.T) {
	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	config := `
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_block_storage" "a" {
  project    = "test-project"
  name       = "vol-a"
  region     = "ASH"
  size_in_gb = 50
}

resource "latitudesh_block_storage" "b" {
  project    = "other-project"
  name       = "vol-b"
  region     = "ASH"
  size_in_gb = 75

  depends_on = [latitudesh_block_storage.a]
}

data "latitudesh_block_storages" "all" {
  depends_on = [latitudesh_block_storage.a, latitudesh_block_storage.b]
}
`

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_block_storages.all", "volumes.#", "2"),
					resource.TestCheckResourceAttr("data.latitudesh_block_storages.all", "id", "all"),
				),
			},
		},
	})
}

func TestAccBlockStoragesDataSource_Basic(t *testing.T) {
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
  name       = "tf-acc-block-storages-ds-%s"
  region     = %q
  size_in_gb = 50
}

data "latitudesh_block_storages" "by_project" {
  project = %q

  depends_on = [latitudesh_block_storage.source]
}
`, projectSlug, testRunID, testBlockStorageSite, projectSlug),
				Check: resource.TestCheckResourceAttrPair(
					"data.latitudesh_block_storages.by_project", "volumes.0.id",
					"latitudesh_block_storage.source", "id",
				),
			},
		},
	})
}
