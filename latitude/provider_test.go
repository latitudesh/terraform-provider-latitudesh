package latitude

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

var testAccProviders map[string]*schema.Provider
var testAccProvider *schema.Provider

func init() {
	testAccProvider = Provider()
	testAccProviders = map[string]*schema.Provider{
		"latitude": testAccProvider,
	}
}

func TestProvider(t *testing.T) {
	if err := Provider().InternalValidate(); err != nil {
		t.Fatalf("err: %s", err)
	}
}

func TestProvider_impl(t *testing.T) {
	var _ *schema.Provider = Provider()
}

func testAccPreCheck(t *testing.T) {
	if v := os.Getenv("LATITUDE_AUTH_TOKEN"); v == "" {
		t.Fatal("LATITUDE_AUTH_TOKEN must be set for acceptance tests")
	}
	if v := os.Getenv("LATITUDE_TEST_PROJECT_ID"); v == "" {
		t.Fatal("LATITUDE_TEST_PROJECT_ID must be set for acceptance tests")
	}
	if v := os.Getenv("LATITUDE_TEST_SSH_PUBLIC_KEY"); v == "" {
		t.Fatal("LATITUDE_TEST_SSH_PUBLIC_KEY must be set for acceptance tests")
	}
}
