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
			// Legacy SDK resources for compatibility testing
			"latitudesh_ssh_key":             &schema.Resource{Schema: map[string]*schema.Schema{"id": {Type: schema.TypeString, Computed: true}, "name": {Type: schema.TypeString, Required: true}, "public_key": {Type: schema.TypeString, Required: true}, "tags": {Type: schema.TypeList, Elem: &schema.Schema{Type: schema.TypeString}, Optional: true}, "fingerprint": {Type: schema.TypeString, Computed: true}, "created_at": {Type: schema.TypeString, Computed: true}, "updated_at": {Type: schema.TypeString, Computed: true}}},
			"latitudesh_user_data":           &schema.Resource{Schema: map[string]*schema.Schema{"id": {Type: schema.TypeString, Computed: true}, "description": {Type: schema.TypeString, Required: true}, "content": {Type: schema.TypeString, Required: true}, "created_at": {Type: schema.TypeString, Computed: true}, "updated_at": {Type: schema.TypeString, Computed: true}}},
			"latitudesh_project":             &schema.Resource{Schema: map[string]*schema.Schema{"id": {Type: schema.TypeString, Computed: true}, "name": {Type: schema.TypeString, Required: true}, "description": {Type: schema.TypeString, Optional: true}, "environment": {Type: schema.TypeString, Optional: true}, "provisioning_type": {Type: schema.TypeString, Required: true}, "slug": {Type: schema.TypeString, Computed: true}}},
			"latitudesh_server":              &schema.Resource{Schema: map[string]*schema.Schema{"id": {Type: schema.TypeString, Computed: true}, "project": {Type: schema.TypeString, Required: true}, "site": {Type: schema.TypeString, Required: true}, "plan": {Type: schema.TypeString, Required: true}, "operating_system": {Type: schema.TypeString, Required: true}, "hostname": {Type: schema.TypeString, Optional: true}, "ssh_keys": {Type: schema.TypeList, Elem: &schema.Schema{Type: schema.TypeString}, Optional: true}, "user_data": {Type: schema.TypeString, Optional: true}, "raid": {Type: schema.TypeString, Optional: true}, "ipxe": {Type: schema.TypeString, Optional: true}, "billing": {Type: schema.TypeString, Optional: true}, "tags": {Type: schema.TypeList, Elem: &schema.Schema{Type: schema.TypeString}, Optional: true}, "primary_ipv4": {Type: schema.TypeString, Computed: true}, "status": {Type: schema.TypeString, Computed: true}, "locked": {Type: schema.TypeBool, Computed: true}, "created_at": {Type: schema.TypeString, Computed: true}, "region": {Type: schema.TypeString, Computed: true}}},
			"latitudesh_virtual_network":     &schema.Resource{Schema: map[string]*schema.Schema{"id": {Type: schema.TypeString, Computed: true}, "project": {Type: schema.TypeString, Required: true}, "site": {Type: schema.TypeString, Required: true}, "description": {Type: schema.TypeString, Optional: true}, "tags": {Type: schema.TypeList, Elem: &schema.Schema{Type: schema.TypeString}, Optional: true}, "vid": {Type: schema.TypeInt, Computed: true}, "name": {Type: schema.TypeString, Computed: true}, "region": {Type: schema.TypeString, Computed: true}, "assignments_count": {Type: schema.TypeInt, Computed: true}, "created_at": {Type: schema.TypeString, Computed: true}}},
			"latitudesh_vlan_assignment":     &schema.Resource{Schema: map[string]*schema.Schema{"id": {Type: schema.TypeString, Computed: true}, "server_id": {Type: schema.TypeString, Required: true}, "virtual_network_id": {Type: schema.TypeString, Required: true}, "status": {Type: schema.TypeString, Computed: true}, "virtual_network_vid": {Type: schema.TypeInt, Computed: true}, "virtual_network_name": {Type: schema.TypeString, Computed: true}}},
			"latitudesh_tag":                 &schema.Resource{Schema: map[string]*schema.Schema{"id": {Type: schema.TypeString, Computed: true}, "name": {Type: schema.TypeString, Required: true}, "description": {Type: schema.TypeString, Optional: true}, "color": {Type: schema.TypeString, Optional: true}}},
			"latitudesh_member":              &schema.Resource{Schema: map[string]*schema.Schema{"id": {Type: schema.TypeString, Computed: true}, "first_name": {Type: schema.TypeString, Optional: true}, "last_name": {Type: schema.TypeString, Optional: true}, "email": {Type: schema.TypeString, Required: true}, "role": {Type: schema.TypeString, Required: true}, "mfa_enabled": {Type: schema.TypeBool, Computed: true}, "created_at": {Type: schema.TypeString, Computed: true}, "updated_at": {Type: schema.TypeString, Computed: true}, "last_login_at": {Type: schema.TypeString, Computed: true}}},
			"latitudesh_firewall":            &schema.Resource{Schema: map[string]*schema.Schema{"id": {Type: schema.TypeString, Computed: true}, "name": {Type: schema.TypeString, Required: true}, "rules": {Type: schema.TypeList, Elem: &schema.Resource{Schema: map[string]*schema.Schema{"protocol": {Type: schema.TypeString, Required: true}, "port": {Type: schema.TypeString, Required: true}, "sources": {Type: schema.TypeList, Elem: &schema.Schema{Type: schema.TypeString}, Required: true}}}, Required: true}}},
			"latitudesh_firewall_assignment": &schema.Resource{Schema: map[string]*schema.Schema{"id": {Type: schema.TypeString, Computed: true}, "firewall_id": {Type: schema.TypeString, Required: true}, "server_id": {Type: schema.TypeString, Required: true}}},
		},
		DataSourcesMap: map[string]*schema.Resource{
			"latitudesh_plan":   &schema.Resource{Schema: map[string]*schema.Schema{"id": {Type: schema.TypeString, Optional: true}, "slug": {Type: schema.TypeString, Optional: true}, "name": {Type: schema.TypeString, Computed: true}, "features": {Type: schema.TypeList, Elem: &schema.Schema{Type: schema.TypeString}, Computed: true}, "cpu_type": {Type: schema.TypeString, Computed: true}, "cpu_cores": {Type: schema.TypeFloat, Computed: true}, "cpu_clock": {Type: schema.TypeFloat, Computed: true}, "cpu_count": {Type: schema.TypeFloat, Computed: true}, "memory": {Type: schema.TypeString, Computed: true}, "has_gpu": {Type: schema.TypeBool, Computed: true}, "gpu_type": {Type: schema.TypeString, Computed: true}, "gpu_count": {Type: schema.TypeFloat, Computed: true}}},
			"latitudesh_region": &schema.Resource{Schema: map[string]*schema.Schema{"id": {Type: schema.TypeString, Optional: true}, "name": {Type: schema.TypeString, Optional: true}, "slug": {Type: schema.TypeString, Optional: true}, "country": {Type: schema.TypeString, Computed: true}, "location": {Type: schema.TypeString, Computed: true}}},
			"latitudesh_role":   &schema.Resource{Schema: map[string]*schema.Schema{"id": {Type: schema.TypeString, Optional: true}, "name": {Type: schema.TypeString, Optional: true}}},
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
