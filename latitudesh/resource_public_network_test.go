package latitudesh

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// testPublicNetworkSite must be a site where the `public_network` preview
// feature is enabled. Verified live on 2026-09-11 via
// GET /regions?filter[features]=public_network (CHI, NYC, LAX2, MIA2, TOR,
// LON2, TYO3, TYO4, SYD2 — SAO2 is not on the list); re-check if Create
// starts failing with a feature-not-enabled error.
const testPublicNetworkSite = "CHI"

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
		if err != nil {
			// Only a 404 proves the network is gone. Auth, permission or
			// transient API failures must surface rather than pass the check
			// while billed infrastructure may still exist.
			if publicNetworkNotFound(err) {
				continue
			}
			return fmt.Errorf("error checking public network %s destroy: %w", id, err)
		}
		if resp != nil && resp.PublicNetwork != nil && resp.PublicNetwork.Data != nil {
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
  site    = "` + testPublicNetworkSite + `"
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

// TestAccPublicNetworkDataSource_IDConflictsWithFilters: `id` is documented as
// mutually exclusive with `project`/`site`. The schema must reject the
// combination before any API call instead of silently ignoring the filters and
// returning a network outside the requested project or site.
func TestAccPublicNetworkDataSource_IDConflictsWithFilters(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "unexpected call", http.StatusInternalServerError)
	}))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: `
data "latitudesh_public_network" "conflict" {
  id      = "pn_123"
  project = "proj_123"
}
`,
				ExpectError: regexp.MustCompile(`(?i)Invalid Attribute Combination`),
			},
			{
				Config: `
data "latitudesh_public_network" "conflict" {
  id   = "pn_123"
  site = "` + testPublicNetworkSite + `"
}
`,
				ExpectError: regexp.MustCompile(`(?i)Invalid Attribute Combination`),
			},
		},
	})

	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("conflicting selectors reached the API %d time(s); want 0", got)
	}
}
