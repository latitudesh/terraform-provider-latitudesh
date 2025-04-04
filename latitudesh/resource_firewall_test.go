package latitudesh

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestAccLatitudeFirewall_Basic(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()
	testAccProviders["latitudesh"].ConfigureContextFunc = testProviderConfigure(recorder)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
			testAccProjectCheck(t)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckLatitudeFirewallConfig(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"latitudesh_firewall.test", "name", "test-firewall"),
					resource.TestCheckResourceAttrSet(
						"latitudesh_firewall.test", "project"),
				),
			},
		},
	})
}

func testAccCheckLatitudeFirewallConfig() string {
	return fmt.Sprintf(`
resource "latitudesh_project" "test" {
  name = "test-project-for-firewall"
  environment = "Development"
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
`)
}
