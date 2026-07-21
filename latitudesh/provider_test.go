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

// roundTripperFunc adapts a function to http.RoundTripper for tests
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestUserAgentTransport(t *testing.T) {
	var gotUserAgent string
	transport := &userAgentTransport{
		base: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			gotUserAgent = req.Header.Get("User-Agent")
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
		}),
		userAgent: "terraform-provider-latitudesh/dev Terraform/1.9.0",
	}

	// SDK sets its own User-Agent before the transport runs: it must be kept as a suffix
	req, _ := http.NewRequest(http.MethodGet, "https://api.latitude.sh/servers", nil)
	req.Header.Set("User-Agent", "speakeasy-sdk/go 1.16.7")

	if _, err := transport.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip returned error: %v", err)
	}

	want := "terraform-provider-latitudesh/dev Terraform/1.9.0 speakeasy-sdk/go 1.16.7"
	if gotUserAgent != want {
		t.Errorf("Expected User-Agent %q, got %q", want, gotUserAgent)
	}

	// The original request must not be mutated
	if ua := req.Header.Get("User-Agent"); ua != "speakeasy-sdk/go 1.16.7" {
		t.Errorf("Original request User-Agent was mutated: %q", ua)
	}

	// Without an SDK User-Agent, only the provider identifier is sent
	reqNoUA, _ := http.NewRequest(http.MethodGet, "https://api.latitude.sh/servers", nil)
	if _, err := transport.RoundTrip(reqNoUA); err != nil {
		t.Fatalf("RoundTrip returned error: %v", err)
	}
	if gotUserAgent != "terraform-provider-latitudesh/dev Terraform/1.9.0" {
		t.Errorf("Expected User-Agent %q, got %q", "terraform-provider-latitudesh/dev Terraform/1.9.0", gotUserAgent)
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
