package latitudesh

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccElasticIP_Basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	resourceName := "latitudesh_elastic_ip.test_item"
	projectID, _, servers := testAccSharedServers(t, 1)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckElasticIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccConfigElasticIPBasic(projectID, servers[0]),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttrSet(resourceName, "address"),
					resource.TestCheckResourceAttr(resourceName, "status", "active"),
					resource.TestCheckResourceAttr(resourceName, "server_id", servers[0]),
					resource.TestCheckResourceAttr(resourceName, "project", projectID),
				),
			},
		},
	})
}

func testAccCheckElasticIPDestroy(s *terraform.State) error {
	ctx := context.Background()
	client, err := newSDKClientFromEnv()
	if err != nil {
		return err
	}

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_elastic_ip" {
			continue
		}
		id := rs.Primary.ID
		if id == "" {
			continue
		}

		resp, err := client.ElasticIps.GetElasticIP(ctx, id)
		if err != nil {
			if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not_found") {
				continue
			}
			return fmt.Errorf("unexpected error checking elastic ip destroy: %s", err)
		}
		if resp != nil && resp.ElasticIP != nil && resp.ElasticIP.Data != nil && resp.ElasticIP.Data.ID != nil && *resp.ElasticIP.Data.ID == id {
			return fmt.Errorf("elastic ip still exists: %s", id)
		}
	}
	return nil
}

func testAccConfigElasticIPBasic(projectID, serverID string) string {
	return fmt.Sprintf(`
resource "latitudesh_elastic_ip" "test_item" {
  project   = "%s"
  server_id = "%s"
}
`, projectID, serverID)
}

func TestAccElasticIP_Move(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	resourceName := "latitudesh_elastic_ip.test_item"
	projectID, _, servers := testAccSharedServers(t, 2)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckElasticIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccConfigElasticIPBasic(projectID, servers[0]),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "server_id", servers[0]),
					resource.TestCheckResourceAttr(resourceName, "status", "active"),
					resource.TestCheckResourceAttrSet(resourceName, "address"),
				),
			},
			{
				Config: testAccConfigElasticIPBasic(projectID, servers[1]),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "server_id", servers[1]),
					resource.TestCheckResourceAttr(resourceName, "status", "active"),
					resource.TestCheckResourceAttrSet(resourceName, "address"),
				),
			},
		},
	})
}

func TestAccElasticIP_UnknownProject(t *testing.T) {
	// PlanOnly test: the server is never provisioned, so a placeholder ID works.
	serverID := "sv_placeholder000"
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				Config: fmt.Sprintf(`
provider "latitudesh" {}

resource "latitudesh_project" "test" {
  name        = "tf-acc-unknown-project-eip"
  environment = "Development"
}

resource "latitudesh_elastic_ip" "test_item" {
  server_id = "%s"
  project   = latitudesh_project.test.id
}
`, serverID),
			},
		},
	})
}
