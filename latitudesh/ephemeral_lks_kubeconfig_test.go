package latitudesh

// Drives the kubeconfig ephemeral end to end — Terraform binary (1.10+),
// protocol, Open, HTTP — against a local mock. The echo test provider persists
// the ephemeral result into state so it can be asserted; nothing the real
// provider does ever writes it there.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

// A kubeconfig is passed through verbatim. The document below is a client-cert
// one purely because it has to be something: the provider never looks inside,
// which is the point — the API specifies "Full kubeconfig YAML" and nothing
// about which authentication method LKS issues.
const testKubeconfigYAML = `apiVersion: v1
kind: Config
current-context: lks-prod
clusters:
- name: lks-prod-cluster
  cluster:
    server: https://1.2.3.4:6443
    certificate-authority-data: Y2EtZGF0YQ==
users:
- name: lks-prod-user
  user:
    client-certificate-data: Y2VydA==
    client-key-data: a2V5
contexts:
- name: lks-prod
  context:
    cluster: lks-prod-cluster
    user: lks-prod-user
`

func testAccLksKubeconfigConfig(clusterID string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

ephemeral "latitudesh_lks_kubeconfig" "test" {
  cluster_id = %q
}

provider "echo" {
  data = ephemeral.latitudesh_lks_kubeconfig.test
}

resource "echo" "test" {}
`, clusterID)
}

func TestAccLksKubeconfig_ReturnsDocumentVerbatim(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lks/clusters/lks_kc_1/kubeconfig" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"data":{"id":"lks_kc_1","type":"lks_cluster_kubeconfigs","attributes":{"kubeconfig":%q}}}`, testKubeconfigYAML)
	}))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6FactoriesWithMockAndEcho(server),
		Steps: []resource.TestStep{
			{
				Config: testAccLksKubeconfigConfig("lks_kc_1"),
				Check: resource.ComposeTestCheckFunc(
					// Byte for byte: no reformatting, no re-serialization.
					resource.TestCheckResourceAttr("echo.test", "data.kubeconfig", testKubeconfigYAML),
					resource.TestCheckResourceAttr("echo.test", "data.cluster_id", "lks_kc_1"),
				),
			},
		},
	})
}

// The control plane answers 409 NOT_READY until it is up. That has to surface
// as an actionable error rather than an empty kubeconfig.
func TestAccLksKubeconfig_NotReady(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"errors":[{"status":"409","code":"NOT_READY","detail":"cluster is not ready"}]}`))
	}))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6FactoriesWithMockAndEcho(server),
		Steps: []resource.TestStep{
			{
				Config:      testAccLksKubeconfigConfig("lks_kc_1"),
				ExpectError: regexp.MustCompile(`NOT_READY|control plane is still coming up`),
			},
		},
	})
}

// A 200 that carries no document is a broken API response, not an empty
// kubeconfig to hand downstream.
func TestAccLksKubeconfig_MissingDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"lks_kc_1","type":"lks_cluster_kubeconfigs","attributes":{}}}`))
	}))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6FactoriesWithMockAndEcho(server),
		Steps: []resource.TestStep{
			{
				Config:      testAccLksKubeconfigConfig("lks_kc_1"),
				ExpectError: regexp.MustCompile(`did not include a kubeconfig document`),
			},
		},
	})
}
