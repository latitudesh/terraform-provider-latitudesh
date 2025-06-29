package latitudesh

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
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

	// Use Framework provider with VCR
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
			testAccProjectCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckServerDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckServerBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckServerExists("latitudesh_server.test_item"),
					resource.TestCheckResourceAttr(
						"latitudesh_server.test_item", "hostname", "test"),
					resource.TestCheckResourceAttrSet(
						"latitudesh_server.test_item", "primary_ipv4"),
					resource.TestCheckResourceAttrSet(
						"latitudesh_server.test_item", "primary_ipv6"),
				),
			},
		},
	})
}

func TestAccServer_Update(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	// Use Framework provider with VCR
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
			testAccProjectCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckServerDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckServerUpdateInitial(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckServerExists("latitudesh_server.test_item"),
					resource.TestCheckResourceAttr(
						"latitudesh_server.test_item", "hostname", "test-initial"),
					resource.TestCheckResourceAttr(
						"latitudesh_server.test_item", "billing", "hourly"),
				),
			},
			{
				Config: testAccCheckServerUpdateChanged(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckServerExists("latitudesh_server.test_item"),
					resource.TestCheckResourceAttr(
						"latitudesh_server.test_item", "hostname", "test-initial"), // hostname should be preserved
					resource.TestCheckResourceAttr(
						"latitudesh_server.test_item", "billing", "monthly"), // billing should be updated
				),
			},
			{
				Config: testAccCheckServerUpdateHostname(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckServerExists("latitudesh_server.test_item"),
					resource.TestCheckResourceAttr(
						"latitudesh_server.test_item", "hostname", "test-updated"), // hostname should be updated
					resource.TestCheckResourceAttr(
						"latitudesh_server.test_item", "billing", "monthly"), // billing should be preserved
				),
			},
		},
	})
}

func TestAccServer_IPv6Support(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	// Use Framework provider with VCR
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
			testAccProjectCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckServerDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckServerBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckServerExists("latitudesh_server.test_item"),
					// Verify that both IPv4 and IPv6 fields are present in the schema
					resource.TestCheckResourceAttrSet(
						"latitudesh_server.test_item", "primary_ipv4"),
					resource.TestCheckResourceAttrSet(
						"latitudesh_server.test_item", "primary_ipv6"),
					// Verify the field names are correct
					resource.TestCheckResourceAttr(
						"latitudesh_server.test_item", "hostname", "test"),
				),
			},
		},
	})
}

func testAccCheckServerDestroy(s *terraform.State) error {
	// Use the VCR client for destroy check
	client := createVCRClient(nil) // We'll use environment variables for auth
	ctx := context.Background()

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_server" {
			continue
		}

		_, err := client.Servers.Get(ctx, rs.Primary.ID, nil)
		if err == nil {
			return fmt.Errorf("server still exists")
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

		// Use the VCR client for existence check
		client := createVCRClient(nil) // We'll use environment variables for auth
		ctx := context.Background()

		response, err := client.Servers.Get(ctx, rs.Primary.ID, nil)
		if err != nil {
			return err
		}

		if response.Server == nil || response.Server.Data == nil {
			return fmt.Errorf("server not found")
		}

		server := response.Server.Data

		// Get status from server
		status := ""
		if server.Attributes != nil && server.Attributes.Status != nil {
			status = string(*server.Attributes.Status)
		}

		fmt.Printf("[INFO] Server %s status: %s\n", rs.Primary.ID, status)

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
			serverProjectID == os.Getenv("LATITUDESH_TEST_PROJECT") &&
			serverOS == testServerOperatingSystem {
			return nil
		}

		return fmt.Errorf("Server %s does not meet the required conditions", rs.Primary.ID)
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

func testAccCheckServerUpdateInitial() string {
	return fmt.Sprintf(`
resource "latitudesh_server" "test_item" {
	project = "%s"
  	hostname = "test-initial"
	plan     = "%s"
	site     = "%s"
	operating_system = "%s"
	billing = "hourly"
}
`,
		os.Getenv("LATITUDESH_TEST_PROJECT"),
		testServerPlan,
		testServerSite,
		testServerOperatingSystem,
	)
}

func testAccCheckServerUpdateChanged() string {
	return fmt.Sprintf(`
resource "latitudesh_server" "test_item" {
	project = "%s"
  	hostname = "test-initial"
	plan     = "%s"
	site     = "%s"
	operating_system = "%s"
	billing = "monthly"
}
`,
		os.Getenv("LATITUDESH_TEST_PROJECT"),
		testServerPlan,
		testServerSite,
		testServerOperatingSystem,
	)
}

func testAccCheckServerUpdateHostname() string {
	return fmt.Sprintf(`
resource "latitudesh_server" "test_item" {
	project = "%s"
  	hostname = "test-updated"
	plan     = "%s"
	site     = "%s"
	operating_system = "%s"
	billing = "monthly"
}
`,
		os.Getenv("LATITUDESH_TEST_PROJECT"),
		testServerPlan,
		testServerSite,
		testServerOperatingSystem,
	)
}
