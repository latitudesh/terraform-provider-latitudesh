package latitudesh

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func TestAccLatitudeFirewall_Basic(t *testing.T) {
	// Skip if LATITUDESH_FIREWALL_ID is set
	if os.Getenv("LATITUDESH_FIREWALL_ID") != "" {
		t.Skip("Skipping TestAccLatitudeFirewall_Basic because LATITUDESH_FIREWALL_ID is set")
	}

	var firewall components.Firewall

	recorder, teardown := createTestRecorder(t)
	defer teardown()
	testAccProviders["latitudesh"].ConfigureContextFunc = testProviderConfigure(recorder)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
			testAccProjectCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckFirewallDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckLatitudeFirewallConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckFirewallExists("latitudesh_firewall.test", &firewall),
					resource.TestCheckResourceAttr("latitudesh_firewall.test", "name", "test-firewall"),
					resource.TestCheckResourceAttrSet("latitudesh_firewall.test", "project"),
				),
			},
		},
	})
}

func testAccCheckFirewallDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*latitudeshgosdk.Latitudesh)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_firewall" {
			continue
		}
		if _, err := client.Firewalls.GetFirewall(context.Background(), rs.Primary.ID); err == nil {
			return fmt.Errorf("Firewall still exists")
		}
	}

	return nil
}

func testAccCheckFirewallExists(n string, firewall *components.Firewall) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		client := testAccProvider.Meta().(*latitudeshgosdk.Latitudesh)

		response, err := client.Firewalls.GetFirewall(context.Background(), rs.Primary.ID)
		if err != nil {
			return err
		}

		if response.Firewall == nil {
			return fmt.Errorf("Firewall not found in response")
		}

		foundFirewall := response.Firewall
		if foundFirewall.ID == nil || *foundFirewall.ID != rs.Primary.ID {
			return fmt.Errorf("Record not found: %v - %v", rs.Primary.ID, foundFirewall)
		}

		*firewall = *foundFirewall

		return nil
	}
}

func testAccCheckLatitudeFirewallConfig_basic() string {
	projectID := os.Getenv("LATITUDESH_TEST_PROJECT")
	if projectID == "" {
		projectID = "test-project-id" // fallback for VCR mode
	}

	return fmt.Sprintf(`
resource "latitudesh_firewall" "test" {
  name = "test-firewall"
  project = "%s"
  # Default rule - API will automatically add this
  rules {
    from = "ANY"
    to = "ANY"
    port = "22"
    protocol = "TCP"
  }
  # Custom rules
  rules {
    from = "0.0.0.0" 
    to = "0.0.0.0"
    port = "80"
    protocol = "TCP"
  }
  rules {
    from = "0.0.0.0"
    to = "0.0.0.0" 
    port = "443"
    protocol = "TCP"
  }
}
`, projectID)
}
