package latitudesh

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
)

const (
	testVNDesc = "tf-acc-virtual-network"
	testVNSite = "SAO2"
)

func TestAccVirtualNetwork_Basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	resourceName := "latitudesh_virtual_network.test_item"

	// This test exercises the provider-level default project. Use the shared
	// pre-existing project instead of creating a throwaway one.
	projectID := testAccProjectID()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckVirtualNetworkDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccConfigVirtualNetworkWithProviderProject(
					projectID,
					testVNDesc,
					testVNSite,
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "description", testVNDesc),
					resource.TestCheckResourceAttr(resourceName, "site", testVNSite),
					resource.TestCheckResourceAttr(resourceName, "project", projectID),
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

// TestAccVirtualNetwork_WithTags exercises the previously-broken path where
// a virtual network is created with tags, then re-planned (must be empty),
// then has its tag set updated in-place. Before the fix in PD-6027, the
// provider silently dropped tags on Create and hard-coded Update to error.
func TestAccVirtualNetwork_WithTags(t *testing.T) {
	resourceName := "latitudesh_virtual_network.test_item"

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckVirtualNetworkDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccConfigVirtualNetworkWithTags(testVNDesc, testVNSite, []string{"latitudesh_tag.a.id"}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "tags.#", "1"),
					resource.TestCheckResourceAttrPair(resourceName, "tags.0", "latitudesh_tag.a", "id"),
				),
			},
			{
				// Idempotency — same config, plan must be empty.
				Config:             testAccConfigVirtualNetworkWithTags(testVNDesc, testVNSite, []string{"latitudesh_tag.a.id"}),
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
			{
				// In-place tag update must succeed (was the hard-error path).
				Config: testAccConfigVirtualNetworkWithTags(testVNDesc, testVNSite, []string{"latitudesh_tag.a.id", "latitudesh_tag.b.id"}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "tags.#", "2"),
				),
			},
		},
	})
}

func testAccConfigVirtualNetworkWithTags(desc, site string, tagExprs []string) string {
	return fmt.Sprintf(`
resource "latitudesh_tag" "a" {
  name  = "tf-acc-vn-tag-a-%[1]s"
  color = "#ff0000"
}

resource "latitudesh_tag" "b" {
  name  = "tf-acc-vn-tag-b-%[1]s"
  color = "#00ff00"
}

resource "latitudesh_virtual_network" "test_item" {
  project     = "`+testAccProjectID()+`"
  description = "%[2]s"
  site        = "%[3]s"
  tags        = [%[4]s]
}
`, testRunID, desc, site, joinComma(tagExprs))
}

func joinComma(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}

// TestAccVirtualNetwork_InvalidTagFailsBeforePOST verifies that PD-6028's fix
// validates tag IDs before the Create POST and does not leave an orphan VNet
// in the backend when validation fails.
func TestAccVirtualNetwork_InvalidTagFailsBeforePOST(t *testing.T) {
	// Suffix per-run so parallel CI jobs don't false-trigger each other's
	// orphan check.
	desc := fmt.Sprintf("tf-acc-pd6028-orphan-check-%s", acctest.RandString(6))
	bogusTagID := "tag_pd6028_definitely_not_a_real_tag"

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      testAccConfigVirtualNetworkBogusTag(desc, testVNSite, bogusTagID),
				ExpectError: regexp.MustCompile(`Tag Validation Error`),
			},
		},
		CheckDestroy: func(s *terraform.State) error {
			// After the failed apply, the backend must not contain a vnet
			// with our description — otherwise the orphan-prevention regressed.
			// The vnet's project is created (and destroyed) in-config, so the
			// lookup scans the team's vnets by the run-unique description.
			ctx := context.Background()
			client, err := newSDKClientFromEnv()
			if err != nil {
				return err
			}
			resp, err := client.PrivateNetworks.List(ctx, operations.GetVirtualNetworksRequest{})
			if err != nil {
				return fmt.Errorf("listing vnets to check for orphan: %w", err)
			}
			if resp.VirtualNetworks == nil || resp.VirtualNetworks.Data == nil {
				return nil
			}
			for _, vn := range resp.VirtualNetworks.Data {
				attrs := vn.GetAttributes()
				if attrs == nil || attrs.GetDescription() == nil {
					continue
				}
				if *attrs.GetDescription() == desc {
					id := ""
					if vn.GetID() != nil {
						id = *vn.GetID()
					}
					return fmt.Errorf("orphan vnet %s left behind with description %q (PD-6028 regression)", id, desc)
				}
			}
			return nil
		},
	})
}

func testAccConfigVirtualNetworkBogusTag(desc, site, bogusTagID string) string {
	return fmt.Sprintf(`
resource "latitudesh_virtual_network" "test_item" {
  project     = "`+testAccProjectID()+`"
  description = "%s"
  site        = "%s"
  tags        = ["%s"]
}
`, desc, site, bogusTagID)
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
