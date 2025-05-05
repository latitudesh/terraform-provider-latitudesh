package latitudesh

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	api "github.com/latitudesh/latitudesh-go"
)

const (
	testServerHostname        = "test"
	testServerPlan            = "c2-small-x86"
	testServerSite            = "SAO"
	testServerOperatingSystem = "ubuntu_24_04_x64_lts"
	testMaxRetries            = 10 // Maximum number of retries
	testRetryDelay            = 30 // Delay between retries in seconds
)

func TestAccServer_Basic(t *testing.T) {
	var server api.Server

	recorder, teardown := createTestRecorder(t)
	defer teardown()
	testAccProviders["latitudesh"].ConfigureContextFunc = testProviderConfigure(recorder)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
			testAccProjectCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckServerDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckServerBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckServerExists("latitudesh_server.test_item", &server),
					resource.TestCheckResourceAttr(
						"latitudesh_server.test_item", "hostname", "test"),
				),
			},
		},
	})
}

func testAccCheckServerDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*api.Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_server" {
			continue
		}

		// Check multiple times with delay to ensure server is truly gone
		for retries := 0; retries < 5; retries++ {
			server, resp, err := client.Servers.Get(rs.Primary.ID, nil)

			// If we get an error and the response is a 404/410, the server is gone
			if err != nil && resp != nil && (resp.StatusCode == 404 || resp.StatusCode == 410) {
				break
			}

			// If server exists but has status "deleted", it's being deleted
			if server != nil && server.Status == "deleted" {
				break
			}

			// If we still get a server back with status "on", the destroy failed
			if err == nil && server != nil && server.Status == "on" {
				return fmt.Errorf("Server %s still exists with status %s", rs.Primary.ID, server.Status)
			}

			// Otherwise wait a bit and retry
			time.Sleep(time.Duration(testRetryDelay) * time.Second)
		}
	}

	return nil
}

func testAccCheckServerExists(n string, server *api.Server) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		client := testAccProvider.Meta().(*api.Client)

		// Retry a few times in case the server is still provisioning
		var foundServer *api.Server
		var err error

		for retries := 0; retries < testMaxRetries; retries++ {
			foundServer, _, err = client.Servers.Get(rs.Primary.ID, nil)
			if err != nil {
				return err
			}

			// If server is found and status is "on", we're good
			if foundServer != nil && foundServer.Status == "on" {
				break
			}

			// Otherwise wait a bit and retry
			time.Sleep(time.Duration(testRetryDelay) * time.Second)
		}

		if foundServer.ID != rs.Primary.ID {
			return fmt.Errorf("Record not found: %v - %v", rs.Primary.ID, foundServer)
		}

		*server = *foundServer

		return nil
	}
}

func testAccCheckServerBasic() string {
	return fmt.Sprintf(`
resource "latitudesh_server" "test_item" {
	project = "%s"
  	hostname = "%s"
	plan     = "%s"
	site     = "%s"
	operating_system = "%s"
}
`,
		os.Getenv("LATITUDESH_TEST_PROJECT"),
		testServerHostname,
		testServerPlan,
		testServerSite,
		testServerOperatingSystem,
	)
}
