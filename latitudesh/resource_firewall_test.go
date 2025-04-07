package latitudesh

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	api "github.com/latitudesh/latitudesh-go"
)

func TestAccLatitudeFirewall_Basic(t *testing.T) {
	var firewall api.Firewall

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
	client := testAccProvider.Meta().(*api.Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_firewall" {
			continue
		}

		_, resp, err := client.Firewalls.Get(rs.Primary.ID, nil)
		if err == nil {
			return fmt.Errorf("Firewall still exists")
		}

		// If we get a 404, the resource is gone
		if resp != nil && resp.StatusCode == 404 {
			continue
		}

		return err
	}

	return nil
}

func testAccCheckFirewallExists(n string, firewall *api.Firewall) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		client := testAccProvider.Meta().(*api.Client)

		foundFirewall, _, err := client.Firewalls.Get(rs.Primary.ID, nil)
		if err != nil {
			return err
		}

		if foundFirewall.ID != rs.Primary.ID {
			return fmt.Errorf("Record not found: %v - %v", rs.Primary.ID, foundFirewall)
		}

		*firewall = *foundFirewall

		return nil
	}
}

func testAccCheckLatitudeFirewallConfig_basic() string {
	return `
resource "latitudesh_project" "test" {
  name = "test-project-for-firewall"
  environment = "Development"
  provisioning_type = "on_demand"
}

resource "latitudesh_firewall" "test" {
  name = "test-firewall"
  project = latitudesh_project.test.id
  rules {
    from = "0.0.0.0/0"
    to = "server"
    port = "22"
    protocol = "tcp"
  }
  rules {
    from = "0.0.0.0/0" 
    to = "server"
    port = "80"
    protocol = "tcp"
  }
  rules {
    from = "0.0.0.0/0"
    to = "server" 
    port = "443"
    protocol = "tcp"
  }
}
`
}
