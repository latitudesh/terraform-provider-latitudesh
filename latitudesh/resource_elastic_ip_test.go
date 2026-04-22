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
	resourceName := "latitudesh_elastic_ip.test_item"
	serverID := os.Getenv("LATITUDESH_TEST_SERVER_ID")
	project := os.Getenv("LATITUDESH_TEST_PROJECT")

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
			testAccProjectCheck(t)
			testAccServerCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckElasticIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccConfigElasticIPBasic(project, serverID),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttrSet(resourceName, "address"),
					resource.TestCheckResourceAttr(resourceName, "status", "active"),
					resource.TestCheckResourceAttr(resourceName, "server_id", serverID),
					resource.TestCheckResourceAttr(resourceName, "project", project),
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

func testAccConfigElasticIPBasic(project, serverID string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  project = "%s"
}

resource "latitudesh_elastic_ip" "test_item" {
  server_id = "%s"
}
`, project, serverID)
}
