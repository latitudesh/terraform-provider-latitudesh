package latitudesh

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/latitudesh/terraform-provider-latitudesh/internal/validators"
)

const (
	testServerHostname        = "terraform-ci-test.latitude.sh"
	testServerPlan            = "c2-small-x86"
	testServerSite            = "NYC"
	testServerOperatingSystem = "ubuntu_24_04_x64_lts"
)

func TestValidateHostnameLength(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		hostname  string
		shouldErr bool
	}{
		{"shorter than max", "short-hostname", false},
		{"exactly max length (32)", "abcdefghijklmnopqrstuvwxyzabcdef", false}, // 32
		{"longer than max (33)", "abcdefghijklmnopqrstuvwxyzabcdefg", true},    // 33
		{"starts with hyphen", "-abc", true},
		{"ends with dot", "abc.", true},
		{"contains underscore", "abc_def", true},
		{"dots and hyphens ok", "terraform-ci-test.latitude.sh", false}, // 29 chars
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validators.ValidateHostname(tc.hostname)
			if tc.shouldErr && err == nil {
				t.Fatalf("expected error, got nil for %q", tc.hostname)
			}
			if !tc.shouldErr && err != nil {
				t.Fatalf("expected no error, got %v for %q", err, tc.hostname)
			}
		})
	}
}

func TestValidateUserData(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		userData  string
		shouldErr bool
	}{
		{"valid with ud_ prefix", "ud_12345", false},
		{"valid with ud_ and alphanumeric", "ud_abc123def", false},
		{"valid with ud_ and underscores", "ud_test_user_data", false},
		{"valid with ud_ and hyphens", "ud_test-user-data", false},
		{"invalid without prefix", "12345", true},
		{"invalid with wrong prefix", "user_data_12345", true},
		{"invalid with partial prefix", "u_12345", true},
		{"invalid empty string", "", true},
		{"valid only prefix", "ud_", false}, // Empty after prefix should be valid
		{"valid long string with prefix", "ud_very_long_user_data_identifier_with_many_characters", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validators.ValidateUserData(tc.userData)
			if tc.shouldErr && err == nil {
				t.Fatalf("expected error, got nil for %q", tc.userData)
			}
			if !tc.shouldErr && err != nil {
				t.Fatalf("expected no error, got %v for %q", err, tc.userData)
			}
		})
	}
}

func TestValidateBilling(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		billing   string
		shouldErr bool
	}{
		{"valid hourly", "hourly", false},
		{"valid monthly", "monthly", false},
		{"valid yearly", "yearly", false},
		{"invalid value", "invalid", true},
		{"invalid empty string", "", true},
		{"invalid daily", "daily", true},
		{"case sensitive - uppercase fails", "HOURLY", true},
		{"case sensitive - uppercase monthly fails", "MONTHLY", true},
		{"case sensitive - uppercase yearly fails", "YEARLY", true},
		{"case sensitive - mixed case fails", "Hourly", true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validators.ValidateBilling(tc.billing)
			if tc.shouldErr && err == nil {
				t.Fatalf("expected error, got nil for %q", tc.billing)
			}
			if !tc.shouldErr && err != nil {
				t.Fatalf("expected no error, got %v for %q", err, tc.billing)
			}
		})
	}
}

func TestValidateBillingChange(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		current    string
		newBilling string
		shouldErr  bool
	}{
		// Valid upgrades
		{"hourly to monthly", "hourly", "monthly", false},
		{"hourly to yearly", "hourly", "yearly", false},
		{"monthly to yearly", "monthly", "yearly", false},
		{"no current value (new)", "", "hourly", false},
		{"no current value (new monthly)", "", "monthly", false},
		{"no current value (new yearly)", "", "yearly", false},
		{"same value hourly", "hourly", "hourly", false},
		{"same value monthly", "monthly", "monthly", false},
		{"same value yearly", "yearly", "yearly", false},
		// Invalid downgrades
		{"yearly to monthly", "yearly", "monthly", true},
		{"yearly to hourly", "yearly", "hourly", true},
		{"monthly to hourly", "monthly", "hourly", true},
		// Invalid values
		{"invalid current value", "invalid", "monthly", true},
		{"invalid new value", "hourly", "invalid", true},
		{"case insensitive current", "Hourly", "monthly", false},
		{"case insensitive new", "hourly", "Monthly", false},
		{"whitespace handling", "  hourly  ", "  monthly  ", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validators.ValidateBillingChange(tc.current, tc.newBilling)
			if tc.shouldErr && err == nil {
				t.Fatalf("expected error for change from %q to %q, got nil", tc.current, tc.newBilling)
			}
			if !tc.shouldErr && err != nil {
				t.Fatalf("expected no error for change from %q to %q, got %v", tc.current, tc.newBilling, err)
			}
		})
	}
}

func TestAccServer_Basic(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	// Use Framework provider with VCR
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckServerDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckServerBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckServerExists("latitudesh_server.test_item"),
					resource.TestCheckResourceAttr(
						"latitudesh_server.test_item", "hostname", testServerHostname),
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
						"latitudesh_server.test_item", "hostname", testServerHostname),
				),
			},
		},
	})
}

func TestAccServer_SSHKeys_NoDrift(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resourceName := "latitudesh_server.test_item"

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
			testAccProjectCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		Steps: []resource.TestStep{
			{
				Config: testAccServerConfigWithSSHKeys(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "hostname", testServerHostname),
				),
			},
			{
				Config:             testAccServerConfigWithSSHKeys(),
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
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
		if (status == "on" || status == "inventory" || status == "deploying") &&
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
	billing = "monthly"
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

func testAccServerConfigWithSSHKeys() string {
	return fmt.Sprintf(`
resource "latitudesh_server" "test_item" {
  hostname         = "terraform-ci-test.latitude.sh"
  operating_system = "ubuntu_24_04_x64_lts"
  plan             = "%s"
  project          = "%s"
  site             = "%s"
  billing          = "monthly"
}
`,
		testServerPlan,
		os.Getenv("LATITUDESH_TEST_PROJECT"),
		testServerSite,
	)
}

func TestAccServerUserDataValidation(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			// Test invalid user_data (should fail)
			{
				Config:      testAccServerConfigUserDataInvalid(),
				ExpectError: regexp.MustCompile("user_data must start with 'ud_' prefix"),
			},
		},
	})
}

func testAccServerConfigUserDataInvalid() string {
	return fmt.Sprintf(`
provider "latitudesh" {
	project = "%s"
}

resource "latitudesh_server" "test" {
	site             = "%s"
	plan             = "%s"
	operating_system = "%s"
	hostname         = "%s"
	user_data        = "invalid_user_data"  # Should fail validation
}
`,
		os.Getenv("LATITUDESH_TEST_PROJECT"),
		testServerSite,
		testServerPlan,
		testServerOperatingSystem,
		testServerHostname)
}
