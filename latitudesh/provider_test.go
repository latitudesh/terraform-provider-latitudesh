package latitudesh

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"gopkg.in/dnaeon/go-vcr.v3/recorder"
)

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

func TestAccEnvVarAuthTokenSet(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless env 'TF_ACC' set")
	}
	if v := os.Getenv("LATITUDESH_AUTH_TOKEN"); v == "" {
		t.Fatal("LATITUDESH_AUTH_TOKEN must be set for acceptance tests")
	}
}

// Helper function to get providers for Framework testing
func testAccProtoV6ProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"latitudesh": providerserver.NewProtocol6WithError(New("dev")()),
	}
}

// Helper function to get providers for Framework testing with VCR
func testAccProtoV6ProviderFactoriesWithVCR(rec *recorder.Recorder) map[string]func() (tfprotov6.ProviderServer, error) {
	httpClient := &http.Client{Transport: rec}
	return map[string]func() (tfprotov6.ProviderServer, error){
		"latitudesh": providerserver.NewProtocol6WithError(&latitudeshProvider{
			version:    "dev",
			httpClient: httpClient,
		}),
	}
}

func testAccTokenCheck(t *testing.T) {
	if v := os.Getenv("LATITUDESH_AUTH_TOKEN"); v == "" {
		t.Fatal("LATITUDESH_AUTH_TOKEN must be set for acceptance tests")
	}
}
