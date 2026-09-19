package latitudesh

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func TestLksSiteItemValue(t *testing.T) {
	id := "ASH"
	name := "Ashburn"
	slug := "ASH"
	facility := "IAD1"
	countryName := "United States"
	countrySlug := "US"

	item := lksSiteItemValue(&components.LksSiteData{
		ID: &id,
		Attributes: &components.LksSiteDataAttributes{
			Name:     &name,
			Slug:     &slug,
			Facility: &facility,
			Country:  &components.Country{Name: &countryName, Slug: &countrySlug},
		},
	})

	if item.ID.ValueString() != id {
		t.Errorf("ID = %q, want %q", item.ID.ValueString(), id)
	}
	if item.Slug.ValueString() != slug {
		t.Errorf("Slug = %q, want %q", item.Slug.ValueString(), slug)
	}
	if item.Facility.ValueString() != facility {
		t.Errorf("Facility = %q, want %q", item.Facility.ValueString(), facility)
	}
	if item.Country.ValueString() != countryName {
		t.Errorf("Country = %q, want %q", item.Country.ValueString(), countryName)
	}
	if item.CountryCode.ValueString() != countrySlug {
		t.Errorf("CountryCode = %q, want %q", item.CountryCode.ValueString(), countrySlug)
	}
}

// A site with no attributes envelope must map to nulls, not panic: every field
// on the SDK model is a pointer and the API is free to omit the whole block.
func TestLksSiteItemValue_NilAttributes(t *testing.T) {
	id := "ASH"

	item := lksSiteItemValue(&components.LksSiteData{ID: &id})

	if item.ID.ValueString() != id {
		t.Errorf("ID = %q, want %q", item.ID.ValueString(), id)
	}
	if !item.Name.IsNull() || !item.Slug.IsNull() || !item.Facility.IsNull() ||
		!item.Country.IsNull() || !item.CountryCode.IsNull() {
		t.Errorf("expected every attribute null for a nil envelope, got %+v", item)
	}
}

// Reads the data source end to end through the provider against a mock, which
// is what proves the schema and the state round-trip agree — the mapper test
// above only covers the conversion.
func TestLksSites_Read(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lks/sites" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"data":[
			{"id":"ASH","type":"lks_sites","attributes":{"name":"Ashburn","slug":"ASH","facility":"IAD1","country":{"name":"United States","slug":"US"}}},
			{"id":"SAO","type":"lks_sites","attributes":{"name":"Sao Paulo","slug":"SAO"}}
		],"meta":{"total":2}}`)
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

data "latitudesh_lks_sites" "test" {}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_lks_sites.test", "id", "all"),
					resource.TestCheckResourceAttr("data.latitudesh_lks_sites.test", "sites.#", "2"),
					resource.TestCheckResourceAttr("data.latitudesh_lks_sites.test", "sites.0.slug", "ASH"),
					resource.TestCheckResourceAttr("data.latitudesh_lks_sites.test", "sites.0.facility", "IAD1"),
					resource.TestCheckResourceAttr("data.latitudesh_lks_sites.test", "sites.0.country_code", "US"),
					// A site that omits country must keep the field null rather
					// than inventing an empty string.
					resource.TestCheckNoResourceAttr("data.latitudesh_lks_sites.test", "sites.1.country_code"),
				),
			},
		},
	})
}

// TestAccLksSites_Basic reads the real site list. Skipped unless TF_ACC and
// LATITUDESH_AUTH_TOKEN are set. This is the test that finally answers what
// `latitudesh_lks.site` actually accepts — the scaffold guessed "ASH".
func TestAccLksSites_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `data "latitudesh_lks_sites" "test" {}`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_lks_sites.test", "id", "all"),
					resource.TestCheckResourceAttrSet("data.latitudesh_lks_sites.test", "sites.#"),
					resource.TestCheckResourceAttrSet("data.latitudesh_lks_sites.test", "sites.0.slug"),
				),
			},
		},
	})
}
