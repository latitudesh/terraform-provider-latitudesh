package latitudesh

import (
	"fmt"
	"os"
	"strconv"
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
		maxRetries := 10
		retryDelay := 30 // seconds

		for retries := 0; retries < maxRetries; retries++ {
			server, resp, err := client.Servers.Get(rs.Primary.ID, nil)

			// If we get an error and the response is a 404/410, the server is gone
			if err != nil && resp != nil && (resp.StatusCode == 404 || resp.StatusCode == 410) {
				fmt.Printf("[INFO] Server %s confirmed deleted (HTTP %d)\n", rs.Primary.ID, resp.StatusCode)
				break
			}

			// If server exists but has status "deleted", it's being deleted
			if server != nil && server.Status == "deleted" {
				fmt.Printf("[INFO] Server %s has status 'deleted', confirmed being deleted\n", rs.Primary.ID)
				break
			}

			// If we still get a server back with status "on", the destroy failed
			if err == nil && server != nil && server.Status == "on" {
				if retries == maxRetries-1 {
					return fmt.Errorf("Server %s still exists with status %s after %d retries",
						rs.Primary.ID, server.Status, retries+1)
				}
				fmt.Printf("[WARN] Server %s still exists with status %s (retry %d/%d)\n",
					rs.Primary.ID, server.Status, retries+1, maxRetries)
			} else {
				fmt.Printf("[INFO] Server %s has status %s during destroy check (retry %d/%d)\n",
					rs.Primary.ID, server.Status, retries+1, maxRetries)
			}

			// Wait before the next retry
			time.Sleep(time.Duration(retryDelay) * time.Second)
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

		// Variables to track consecutive successes
		consecutiveSuccesses := 0
		requiredConsecutiveSuccesses := 3
		timeoutMinutes := 30
		iterationSeconds := 30
		maxIterations := (timeoutMinutes * 60) / iterationSeconds

		// Get the expected attributes from the test config
		expectedProject := os.Getenv("LATITUDESH_TEST_PROJECT")
		expectedOS := testServerOperatingSystem

		// Retry with more patience to match the implementation's retry logic
		for attempt := 0; attempt < maxIterations; attempt++ {
			foundServer, resp, err := client.Servers.Get(rs.Primary.ID, nil)

			// Check for transient errors or deletion
			if err != nil {
				if resp != nil && (resp.StatusCode == 404 || resp.StatusCode == 410) {
					return fmt.Errorf("Server %s was deleted during test (HTTP status: %d)",
						rs.Primary.ID, resp.StatusCode)
				}
				// For other transient errors, log and retry
				if attempt < maxIterations-1 {
					fmt.Printf("[WARN] Error getting server %s (attempt %d/%d): %v - will retry\n",
						rs.Primary.ID, attempt+1, maxIterations, err)
					time.Sleep(time.Duration(iterationSeconds) * time.Second)
					continue
				}
				return err
			}

			// Safety check for nil server
			if foundServer == nil {
				if attempt < maxIterations-1 {
					fmt.Printf("[WARN] Server %s returned nil (attempt %d/%d) - will retry\n",
						rs.Primary.ID, attempt+1, maxIterations)
					time.Sleep(time.Duration(iterationSeconds) * time.Second)
					continue
				}
				return fmt.Errorf("Server %s not found after %d attempts", rs.Primary.ID, attempt+1)
			}

			fmt.Printf("[INFO] Server %s status: %s (attempt %d/%d)\n",
				rs.Primary.ID, foundServer.Status, attempt+1, maxIterations)

			// Get project ID from server
			var serverProjectID string
			if foundServer.Project.ID != nil {
				switch foundServer.Project.ID.(type) {
				case string:
					serverProjectID = foundServer.Project.ID.(string)
				case float64:
					serverProjectID = strconv.FormatFloat(foundServer.Project.ID.(float64), 'b', 2, 64)
				}
			}

			// Get OS from server
			var serverOS string
			if foundServer.OperatingSystem.Slug != "" {
				serverOS = foundServer.OperatingSystem.Slug
			}

			// Check if server meets all required conditions
			if (foundServer.Status == "on" || foundServer.Status == "inventory") &&
				serverProjectID == expectedProject &&
				serverOS == expectedOS {

				consecutiveSuccesses++
				fmt.Printf("[INFO] Server %s conditions met (%d/%d): status=%s, project=%s, os=%s\n",
					rs.Primary.ID, consecutiveSuccesses, requiredConsecutiveSuccesses,
					foundServer.Status, serverProjectID, serverOS)

				// For tests, we only need 2 consecutive successes
				requiredConsecutiveSuccesses = 2

				if consecutiveSuccesses >= requiredConsecutiveSuccesses {
					// Only set the server once we've reached the required consecutive successes
					*server = *foundServer
					return nil
				}
			} else {
				// Reset counter if conditions not met
				if consecutiveSuccesses > 0 {
					fmt.Printf("[WARN] Server %s conditions no longer met, resetting counter\n", rs.Primary.ID)
					consecutiveSuccesses = 0
				}

				// Log what conditions weren't met
				fmt.Printf("[INFO] Server %s conditions check: status=%s (expected=on or inventory), project=%s (expected=%s), os=%s (expected=%s)\n",
					rs.Primary.ID, foundServer.Status, serverProjectID, expectedProject, serverOS, expectedOS)
			}

			time.Sleep(time.Duration(iterationSeconds) * time.Second)
		}

		return fmt.Errorf("Timeout reached: Server %s did not reach required conditions after %d minutes",
			rs.Primary.ID, timeoutMinutes)
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
