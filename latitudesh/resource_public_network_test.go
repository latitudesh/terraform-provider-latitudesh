package latitudesh

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// testPublicNetworkSite is a placeholder site slug. Public networks are a
// preview feature only available where `public_network` is enabled; a human
// must confirm this value against a site that actually has the feature
// enabled before running this test live.
const testPublicNetworkSite = "SAO2"

func TestAccPublicNetwork_Basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	resourceName := "latitudesh_public_network.test_item"
	projectID := testAccProjectID()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckPublicNetworkDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccConfigPublicNetworkBasic(projectID, testPublicNetworkSite, 28),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "project", projectID),
					resource.TestCheckResourceAttr(resourceName, "site", testPublicNetworkSite),
					resource.TestCheckResourceAttr(resourceName, "size", "28"),
					resource.TestCheckResourceAttrSet(resourceName, "ipv4"),
					resource.TestCheckResourceAttrSet(resourceName, "ipv6"),
				),
			},
		},
	})
}

func testAccCheckPublicNetworkDestroy(s *terraform.State) error {
	ctx := context.Background()
	client, err := newSDKClientFromEnv()
	if err != nil {
		return err
	}

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_public_network" {
			continue
		}
		id := rs.Primary.ID
		if id == "" {
			continue
		}

		resp, err := client.PublicNetworks.GetPublicNetwork(ctx, id)
		if err == nil && resp != nil && resp.PublicNetwork != nil && resp.PublicNetwork.Data != nil &&
			resp.PublicNetwork.Data.ID != nil && *resp.PublicNetwork.Data.ID == id {
			return fmt.Errorf("public network still exists: %s", id)
		}
	}
	return nil
}

func testAccConfigPublicNetworkBasic(project, site string, size int) string {
	return fmt.Sprintf(`
resource "latitudesh_public_network" "test_item" {
  project = "%s"
  site    = "%s"
  size    = %d
}
`, project, site, size)
}

func TestAccPublicNetwork_UnknownProject(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				Config: `
provider "latitudesh" {}

resource "latitudesh_project" "test" {
  name        = "tf-acc-unknown-project-pn"
  environment = "Development"
}

resource "latitudesh_public_network" "test_item" {
  site    = "SAO2"
  size    = 28
  project = latitudesh_project.test.id
}
`,
			},
		},
	})
}

func TestAccPublicNetworkDataSource_ByID(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	dataSourceName := "data.latitudesh_public_network.by_id"
	projectID := testAccProjectID()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckPublicNetworkDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccConfigPublicNetworkDataSourceByID(projectID, testPublicNetworkSite),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair(dataSourceName, "id", "latitudesh_public_network.test_item", "id"),
					resource.TestCheckResourceAttrPair(dataSourceName, "ipv4", "latitudesh_public_network.test_item", "ipv4"),
				),
			},
		},
	})
}

func testAccConfigPublicNetworkDataSourceByID(project, site string) string {
	return fmt.Sprintf(`
resource "latitudesh_public_network" "test_item" {
  project = "%s"
  site    = "%s"
  size    = 28
}

data "latitudesh_public_network" "by_id" {
  id = latitudesh_public_network.test_item.id
}
`, project, site)
}
