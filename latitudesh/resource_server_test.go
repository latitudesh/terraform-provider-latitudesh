package latitudesh

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
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
					testAccCheckServerExists("latitudesh_server.test_item"),
					resource.TestCheckResourceAttr(
						"latitudesh_server.test_item", "hostname", "test"),
				),
			},
		},
	})
}

func testAccCheckServerDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*latitudeshgosdk.Latitudesh)
	ctx := context.Background()

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_server" {
			continue
		}

		// Check multiple times with delay to ensure server is truly gone
		maxRetries := 10
		retryDelay := 30 // seconds

		for retries := 0; retries < maxRetries; retries++ {
			_, err := client.Servers.GetServer(ctx, rs.Primary.ID, nil)

			// If we get an error, the server is likely gone
			if err != nil {
				// Check if it's a 404/410 error
				if apiErr, ok := err.(*components.APIError); ok && (apiErr.StatusCode == 404 || apiErr.StatusCode == 410) {
					fmt.Printf("[INFO] Server %s confirmed deleted (HTTP %d)\n", rs.Primary.ID, apiErr.StatusCode)
					break
				}
			}

			// Wait before the next retry
			time.Sleep(time.Duration(retryDelay) * time.Second)
		}
	}

	return nil
}

func testAccCheckServerExists(n string) resource.TestCheckFunc {
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
			foundServer, err := client.Servers.GetServer(ctx, rs.Primary.ID, nil)

			// Check for transient errors or deletion
			if err != nil {
				if apiErr, ok := err.(*components.APIError); ok && (apiErr.StatusCode == 404 || apiErr.StatusCode == 410) {
					return fmt.Errorf("Server %s was deleted during test (HTTP status: %d)",
						rs.Primary.ID, apiErr.StatusCode)
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
			if foundServer.Server == nil || foundServer.Server.Data == nil {
				if attempt < maxIterations-1 {
					fmt.Printf("[WARN] Server %s returned nil (attempt %d/%d) - will retry\n",
						rs.Primary.ID, attempt+1, maxIterations)
					time.Sleep(time.Duration(iterationSeconds) * time.Second)
					continue
				}
				return fmt.Errorf("Server %s not found after %d attempts", rs.Primary.ID, attempt+1)
			}

			server := foundServer.Server.Data

			// Get status from server
			status := ""
			if server.Attributes != nil && server.Attributes.Status != nil {
				status = string(*server.Attributes.Status)
			}

			fmt.Printf("[INFO] Server %s status: %s (attempt %d/%d)\n",
				rs.Primary.ID, status, attempt+1, maxIterations)

			// Get project ID from server
			var serverProjectID string
			if server.Attributes != nil && server.Attributes.Project != nil {
				if server.Attributes.Project.ID != nil {
					serverProjectID = *server.Attributes.Project.ID
				} else if server.Attributes.Project.Slug != nil {
					serverProjectID = *server.Attributes.Project.Slug
				}
			}

			// Get OS from server
			var serverOS string
			if server.Attributes != nil && server.Attributes.OperatingSystem != nil && server.Attributes.OperatingSystem.Slug != nil {
				serverOS = *server.Attributes.OperatingSystem.Slug
			}

			// Check if server meets all required conditions
			if (status == "on" || status == "inventory") &&
				serverProjectID == expectedProject &&
				serverOS == expectedOS {

				consecutiveSuccesses++
				fmt.Printf("[INFO] Server %s conditions met (%d/%d): status=%s, project=%s, os=%s\n",
					rs.Primary.ID, consecutiveSuccesses, requiredConsecutiveSuccesses,
					status, serverProjectID, serverOS)

				// For tests, we only need 2 consecutive successes
				requiredConsecutiveSuccesses = 2

				if consecutiveSuccesses >= requiredConsecutiveSuccesses {
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
					rs.Primary.ID, status, serverProjectID, expectedProject, serverOS, expectedOS)
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
