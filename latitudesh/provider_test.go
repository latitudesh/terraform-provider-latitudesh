package latitudesh

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
		"latitudesh": testAccProvider,
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

func testAccTokenCheck(t *testing.T) {
	if v := os.Getenv("LATITUDESH_AUTH_TOKEN"); v == "" {
		t.Fatal("LATITUDESH_AUTH_TOKEN must be set for acceptance tests")
	}
}

func testAccProjectCheck(t *testing.T) {
	if v := os.Getenv("LATITUDESH_TEST_PROJECT"); v == "" {
		t.Fatal("LATITUDESH_TEST_PROJECT must be set for acceptance tests")
	}
}

func testAccSSHKeyCheck(t *testing.T) {
	if v := os.Getenv("LATITUDESH_TEST_SSH_PUBLIC_KEY"); v == "" {
		t.Fatal("LATITUDESH_TEST_SSH_PUBLIC_KEY must be set for acceptance tests")
	}
}

func testAccUserDataCheck(t *testing.T) {
	if v := os.Getenv("LATITUDESH_TEST_USER_DATA_CONTENT"); v == "" {
		t.Fatal("LATITUDESH_TEST_USER_DATA_CONTENT must be set for acceptance tests")
	}
}

func testAccServerCheck(t *testing.T) {
	if v := os.Getenv("LATITUDESH_TEST_SERVER_ID"); v == "" {
		t.Fatal("LATITUDESH_TEST_SERVER_ID must be set for acceptance tests")
	}
}

func testAccVirtualNetworkCheck(t *testing.T) {
	if v := os.Getenv("LATITUDESH_TEST_VIRTUAL_NETWORK_ID"); v == "" {
		t.Fatal("LATITUDESH_TEST_VIRTUAL_NETWORK_ID must be set for acceptance tests")
	}
}
