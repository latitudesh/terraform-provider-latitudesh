package latitudesh

// TestAccLks_Basic exercises the resource against the live API. It is
// skipped unless TF_ACC and LATITUDESH_AUTH_TOKEN are set (via
// testAccTokenCheck), so `go test ./latitudesh` never reaches the network.
//
// testAccLksSite and testAccLksKubernetesVersion are not confirmed against a
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
	testAccLksSite              = "ASH"
	testAccLksKubernetesVersion = "1.31.0"
)

func TestAccLks_Basic(t *testing.T) {
	resourceName := "latitudesh_lks.test_item"
	projectID := testAccProjectID()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckLksDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccLksConfigBasic(projectID, testAccLksSite, testAccLksKubernetesVersion),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "project", projectID),
					resource.TestCheckResourceAttr(resourceName, "site", testAccLksSite),
					resource.TestCheckResourceAttr(resourceName, "kubernetes_version", testAccLksKubernetesVersion),
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

func testAccLksConfigBasic(project, site, version string) string {
	return fmt.Sprintf(`
resource "latitudesh_lks" "test_item" {
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

func testAccCheckLksDestroy(s *terraform.State) error {
	client, err := newSDKClientFromEnv()
	if err != nil {
		return err
	}

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_lks" {
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
		if !lksClusterNotFound(err) {
			return fmt.Errorf("unexpected error checking LKS cluster %s destroyed: %w", id, err)
		}
	}
	return nil
}
