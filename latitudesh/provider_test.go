package latitudesh

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

// Backward compatibility: Create a SDK v2 provider for tests
func Provider() *schema.Provider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"auth_token": {
				Type:        schema.TypeString,
				Optional:    true,
				Sensitive:   true,
				Description: "Latitude.sh API authentication token",
			},
		},
		ConfigureContextFunc: func(ctx context.Context, d *schema.ResourceData) (interface{}, diag.Diagnostics) {
			authToken := d.Get("auth_token").(string)

			if authToken != "" {
				sdkClient := latitudeshgosdk.New(
					latitudeshgosdk.WithSecurity(authToken),
				)
				return sdkClient, nil
			}

			sdkClient := latitudeshgosdk.New(
				latitudeshgosdk.WithSecurity(""),
			)
			return sdkClient, nil
		},
		ResourcesMap: map[string]*schema.Resource{
			// Empty for now since we're using Framework
		},
		DataSourcesMap: map[string]*schema.Resource{
			// Empty for now since we're using Framework
		},
	}
}

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

// Test Framework provider
func TestFrameworkProvider(t *testing.T) {
	ctx := context.Background()

	// Test that the Framework provider can be created
	frameworkProvider := New("dev")()

	// Test metadata
	metadataReq := provider.MetadataRequest{}
	metadataResp := &provider.MetadataResponse{}
	frameworkProvider.Metadata(ctx, metadataReq, metadataResp)

	if metadataResp.TypeName != "latitudesh" {
		t.Errorf("Expected provider type name 'latitudesh', got %s", metadataResp.TypeName)
	}
}

// Helper function to get providers for Framework testing
func GetTestProviders() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"latitudesh": providerserver.NewProtocol6WithError(New("dev")()),
	}
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
