package latitudesh

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func TestLksVersionItemValue(t *testing.T) {
	id := "1.32.0"
	version := "1.32.0"
	isDefault := true
	forCreation := true
	forUpgrade := false

	item := lksVersionItemValue(&components.LksKubernetesVersionData{
		ID: &id,
		Attributes: &components.LksKubernetesVersionDataAttributes{
			Version:              &version,
			Default:              &isDefault,
			AvailableForCreation: &forCreation,
			AvailableForUpgrade:  &forUpgrade,
		},
	})

	if item.Version.ValueString() != version {
		t.Errorf("Version = %q, want %q", item.Version.ValueString(), version)
	}
	if !item.Default.ValueBool() {
		t.Error("Default = false, want true")
	}
	if !item.AvailableForCreation.ValueBool() {
		t.Error("AvailableForCreation = false, want true")
	}
	// false and null are different answers here: false means "the platform
	// says no", null means "the platform said nothing".
	if item.AvailableForUpgrade.IsNull() || item.AvailableForUpgrade.ValueBool() {
		t.Errorf("AvailableForUpgrade = %s, want false", item.AvailableForUpgrade)
	}
}

func TestLksVersionItemValue_NilAttributes(t *testing.T) {
	id := "1.32.0"

	item := lksVersionItemValue(&components.LksKubernetesVersionData{ID: &id})

	if item.ID.ValueString() != id {
		t.Errorf("ID = %q, want %q", item.ID.ValueString(), id)
	}
	if !item.Version.IsNull() || !item.Default.IsNull() ||
		!item.AvailableForCreation.IsNull() || !item.AvailableForUpgrade.IsNull() {
		t.Errorf("expected every attribute null for a nil envelope, got %+v", item)
	}
}

func TestLksVersions_Read(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lks/available_versions" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"data":[
			{"id":"1.30.0","type":"lks_kubernetes_versions","attributes":{"version":"1.30.0","default":false,"available_for_creation":false,"available_for_upgrade":true}},
			{"id":"1.31.0","type":"lks_kubernetes_versions","attributes":{"version":"1.31.0","default":true,"available_for_creation":true,"available_for_upgrade":true}}
		]}`)
	}))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: `
provider "latitudesh" {
  auth_token = "mock-token"
}

data "latitudesh_lks_versions" "test" {}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_lks_versions.test", "id", "all"),
					resource.TestCheckResourceAttr("data.latitudesh_lks_versions.test", "versions.#", "2"),
					// The default is picked out of the list, not assumed to be
					// the first entry.
					resource.TestCheckResourceAttr("data.latitudesh_lks_versions.test", "default_version", "1.31.0"),
					resource.TestCheckResourceAttr("data.latitudesh_lks_versions.test", "versions.0.version", "1.30.0"),
					resource.TestCheckResourceAttr("data.latitudesh_lks_versions.test", "versions.0.available_for_creation", "false"),
					resource.TestCheckResourceAttr("data.latitudesh_lks_versions.test", "versions.0.available_for_upgrade", "true"),
					resource.TestCheckResourceAttr("data.latitudesh_lks_versions.test", "versions.1.default", "true"),
				),
			},
		},
	})
}

// No entry flagged default must leave default_version null rather than falling
// back to an arbitrary element.
func TestLksVersions_ReadWithoutDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"data":[
			{"id":"1.30.0","type":"lks_kubernetes_versions","attributes":{"version":"1.30.0","default":false}}
		]}`)
	}))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: `
provider "latitudesh" {
  auth_token = "mock-token"
}

data "latitudesh_lks_versions" "test" {}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_lks_versions.test", "versions.#", "1"),
					resource.TestCheckNoResourceAttr("data.latitudesh_lks_versions.test", "default_version"),
				),
			},
		},
	})
}

// TestAccLksVersions_Basic reads the real version list. Skipped unless TF_ACC
// and LATITUDESH_AUTH_TOKEN are set. This is what confirms which versions
// `latitudesh_lks.kubernetes_version` accepts — the scaffold guessed "1.31.0".
func TestAccLksVersions_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `data "latitudesh_lks_versions" "test" {}`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_lks_versions.test", "id", "all"),
					resource.TestCheckResourceAttrSet("data.latitudesh_lks_versions.test", "versions.#"),
					resource.TestCheckResourceAttrSet("data.latitudesh_lks_versions.test", "versions.0.version"),
				),
			},
		},
	})
}
