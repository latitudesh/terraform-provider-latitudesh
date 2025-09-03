//go:build tfplugintesting

package latitudesh

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

const (
	testVNDesc = "tf-acc-virtual-network"
	testVNSite = "SAO2"
)

// Provider factories (ProtoV6 + framework)
var testAccProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"latitudesh": providerserver.NewProtocol6WithError(New("test")()),
}

func testAccPreCheck(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Fatalf("TF_ACC must be set for acceptance tests")
	}
	if os.Getenv("LATITUDESH_AUTH_TOKEN") == "" {
		t.Fatalf("LATITUDESH_AUTH_TOKEN must be set for acceptance tests")
	}
	if os.Getenv("LATITUDESH_TEST_PROJECT") == "" {
		t.Fatalf("LATITUDESH_TEST_PROJECT must be set (project id/slug used in provider block)")
	}
}

func newSDKClientFromEnv() (*latitudeshgosdk.Latitudesh, error) {
	token := os.Getenv("LATITUDESH_AUTH_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("LATITUDESH_AUTH_TOKEN not set")
	}
	return latitudeshgosdk.New(latitudeshgosdk.WithSecurity(token)), nil
}

func TestAccVirtualNetwork_Basic(t *testing.T) {
	resourceName := "latitudesh_virtual_network.test_item"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
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
		if err == nil && resp != nil && resp.Object != nil && resp.Object.Data != nil &&
			resp.Object.Data.ID != nil && *resp.Object.Data.ID == id {
			return fmt.Errorf("virtual network still exists: %s", id)
		}
	}
	return nil
}

func testAccCheckVirtualNetworkExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		client := testAccProvider.Meta().(*latitudeshgosdk.Latitudesh)
		ctx := context.Background()

		response, err := client.PrivateNetworks.Get(ctx, rs.Primary.ID)
		if err != nil {
			return err
		}

		if response.Object == nil || response.Object.Data == nil {
			return fmt.Errorf("virtual network not found")
		}

		vnet := response.Object.Data
		vnData := vnet.GetData()

		if vnData == nil || vnData.GetID() == nil || *vnData.GetID() != rs.Primary.ID {
			return fmt.Errorf("Record not found: %v", rs.Primary.ID)
		}

		return nil
	}
}

func testAccCheckVirtualNetworkBasic() string {
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
