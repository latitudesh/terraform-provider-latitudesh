package latitudesh

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

const (
	testVNDesc = "tf-acc-virtual-network"
	testVNSite = "SAO2"
)

func TestAccVirtualNetwork_Basic(t *testing.T) {
	resourceName := "latitudesh_virtual_network.test_item"

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
			testAccProjectCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckVirtualNetworkDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccConfigVirtualNetworkWithProviderProject(
					os.Getenv("LATITUDESH_TEST_PROJECT"),
					testVNDesc,
					testVNSite,
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "description", testVNDesc),
					resource.TestCheckResourceAttr(resourceName, "site", testVNSite),
					resource.TestCheckResourceAttr(resourceName, "project", os.Getenv("LATITUDESH_TEST_PROJECT")),
					resource.TestCheckResourceAttrSet(resourceName, "vid"),
					resource.TestCheckResourceAttrSet(resourceName, "region"),
				),
			},
		},
	})
}

func testAccCheckVirtualNetworkDestroy(s *terraform.State) error {
	ctx := context.Background()
	client, err := newSDKClientFromEnv()
	if err != nil {
		return err
	}

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_virtual_network" {
			continue
		}
		id := rs.Primary.ID
		if id == "" {
			continue
		}

		resp, err := client.PrivateNetworks.Get(ctx, id)
		if err == nil && resp != nil && resp.VirtualNetwork != nil && resp.VirtualNetwork.Data != nil &&
			resp.VirtualNetwork.Data.ID != nil && *resp.VirtualNetwork.Data.ID == id {
			return fmt.Errorf("virtual network still exists: %s", id)
		}
	}
	return nil
}

func newSDKClientFromEnv() (*latitudeshgosdk.Latitudesh, error) {
	token := os.Getenv("LATITUDESH_AUTH_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("LATITUDESH_AUTH_TOKEN not set")
	}
	return latitudeshgosdk.New(latitudeshgosdk.WithSecurity(token)), nil
}

func testAccConfigVirtualNetworkWithProviderProject(project, desc, site string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  project = "%s"
}

resource "latitudesh_virtual_network" "test_item" {
  description = "%s"
  site        = "%s"
}
`, project, desc, site)
}

func testAccCheckVirtualNetworkBasic(project, desc, site string) string {
	return fmt.Sprintf(`
terraform {
  required_providers {
    latitudesh = {
      source = "local/iac/latitudesh"
    }
  }
}

provider "latitudesh" {
  project = "%s"
}

resource "latitudesh_virtual_network" "test_item" {
  description = "%s"
  site        = "%s"
}
`, project, desc, site)
}

func TestAccVirtualNetwork_UnknownProject(t *testing.T) {
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
  name        = "tf-acc-unknown-project-vn"
  environment = "Development"
}

resource "latitudesh_virtual_network" "test_item" {
  description = "tf-acc-unknown-project"
  site        = "SAO2"
  project     = latitudesh_project.test.id
}
`,
			},
		},
	})
}
