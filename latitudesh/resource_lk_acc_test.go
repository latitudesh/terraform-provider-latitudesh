package latitudesh

// TestAccLk_Basic exercises the resource against the live API. It is
// skipped unless TF_ACC and LATITUDESH_AUTH_TOKEN are set (via
// testAccTokenCheck), so `go test ./latitudesh` never reaches the network.
//
// testAccLkSite and testAccLkKubernetesVersion are not confirmed against a
// live account's GET /lks/sites / GET /lks/available_versions (neither is
// mapped by this scaffold) — see the handoff for why. Update them before
// running this test live.

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const (
	testAccLkSite              = "ASH"
	testAccLkKubernetesVersion = "1.31.0"
)

func TestAccLk_Basic(t *testing.T) {
	resourceName := "latitudesh_lk.test_item"
	projectID := testAccProjectID()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckLkDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccLkConfigBasic(projectID, testAccLkSite, testAccLkKubernetesVersion),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "project", projectID),
					resource.TestCheckResourceAttr(resourceName, "site", testAccLkSite),
					resource.TestCheckResourceAttr(resourceName, "kubernetes_version", testAccLkKubernetesVersion),
					resource.TestCheckResourceAttr(resourceName, "status", "ready"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccLkConfigBasic(project, site, version string) string {
	return fmt.Sprintf(`
resource "latitudesh_lk" "test_item" {
  project             = %q
  name                = "tf-acc-lks-%s"
  site                = %q
  kubernetes_version  = %q

  timeouts = {
    create = "30m"
    delete = "15m"
  }
}
`, project, testRunID, site, version)
}

func testAccCheckLkDestroy(s *terraform.State) error {
	client, err := newSDKClientFromEnv()
	if err != nil {
		return err
	}

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_lk" {
			continue
		}
		id := rs.Primary.ID
		if id == "" {
			continue
		}

		_, err := client.Lks.GetLksCluster(context.Background(), id)
		if err == nil {
			return fmt.Errorf("LKS cluster still exists: %s", id)
		}
		if !lkClusterNotFound(err) {
			return fmt.Errorf("unexpected error checking LKS cluster %s destroyed: %w", id, err)
		}
	}
	return nil
}
