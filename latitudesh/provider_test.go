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
	"gopkg.in/dnaeon/go-vcr.v3/recorder"
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
		ResourcesMap: map[string]*schema.Resource{},
		DataSourcesMap: map[string]*schema.Resource{
			"latitudesh_plan": {
				Schema: map[string]*schema.Schema{
					"id": {
						Type:     schema.TypeString,
						Optional: true,
					},
					"slug": {
						Type:     schema.TypeString,
						Optional: true,
					},
					"name": {
						Type:     schema.TypeString,
						Computed: true,
					},
				},
			},
			"latitudesh_region": {
				Schema: map[string]*schema.Schema{
					"id": {
						Type:     schema.TypeString,
						Optional: true,
					},
					"name": {
						Type:     schema.TypeString,
						Optional: true,
					},
					"slug": {
						Type:     schema.TypeString,
						Optional: true,
					},
					"country": {
						Type:     schema.TypeString,
						Computed: true,
					},
					"location": {
						Type:     schema.TypeString,
						Computed: true,
					},
				},
			},
			"latitudesh_role": {
				Schema: map[string]*schema.Schema{
					"id": {
						Type:     schema.TypeString,
						Optional: true,
					},
					"name": {
						Type:     schema.TypeString,
						Optional: true,
					},
				},
			},
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
func testAccProtoV6ProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"latitudesh": providerserver.NewProtocol6WithError(New("dev")()),
	}
}

// Helper function to get providers for Framework testing with VCR
func testAccProtoV6ProviderFactoriesWithVCR(recorder *recorder.Recorder) map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"latitudesh": func() (tfprotov6.ProviderServer, error) {
			// Create a provider instance with VCR-enabled client
			provider := New("dev")()
			// Configure the provider with VCR client
			// This will be handled in the provider's Configure method
			return providerserver.NewProtocol6(provider)(), nil
		},
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
