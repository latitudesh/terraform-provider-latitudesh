package latitudesh

// Regression test for the refresh null-vs-default defect: the API can omit
// fields that carry schema defaults (storage_class, versioning, locking,
// retention_mode). The read path must keep the concrete defaulted value on
// absence instead of writing null, which would fail the create with an
// inconsistent-state error or cause recurring drift on refresh. This mock omits
// those fields from both the create and the read responses; the plan must stay
// empty and the defaults must survive. Runs under TF_ACC without credentials.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

type mockBucketDefaultsAPI struct {
	// full mimics the real API (every attribute present); false deliberately
	// omits the defaulted attributes (storage_class, versioning, locking,
	// retention_mode) to exercise the preserve-on-absence read path.
	full bool
}

// bucketData mirrors the live payload shape: region carries the slug only
// under region.site.slug (no region-level id), exactly like the real API.
func (m *mockBucketDefaultsAPI) bucketData() map[string]any {
	attrs := map[string]any{
		"name":         "defaults-mock",
		"bucket_name":  "defaults-mock",
		"storage_type": "object",
		"endpoint":     "https://objects.ash.storage.sh",
		"created_at":   "2026-09-02T00:00:00.000Z",
		"source":       "default",
		"region":       map[string]any{"city": "Ashburn", "country": "United States", "site": map[string]any{"id": "loc_mock", "slug": "ASH"}},
		"project":      map[string]any{"id": "proj_mock", "slug": "lanusse"},
	}
	if m.full {
		attrs["storage_class"] = "standard"
		attrs["versioning"] = false
		attrs["locking"] = false
		attrs["retention_mode"] = "NONE"
	}
	return map[string]any{
		"id":         "bkt_defaults_mock",
		"type":       "object_storages",
		"attributes": attrs,
	}
}

func (m *mockBucketDefaultsAPI) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.api+json")
	env := map[string]any{"data": m.bucketData()}

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/storage/buckets":
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(env)
	case r.Method == http.MethodGet && r.URL.Path == "/storage/buckets/bkt_defaults_mock":
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(env)
	case r.Method == http.MethodDelete && r.URL.Path == "/storage/buckets/bkt_defaults_mock":
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

func testAccBucketDefaultsConfig() string {
	return `
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_object_storage" "test" {
  project = "proj_mock"
  name    = "defaults-mock"
  region  = "ASH"
}
`
}

func TestAccObjectStorage_DefaultsSurviveOmittedFields(t *testing.T) {
	mock := &mockBucketDefaultsAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccBucketDefaultsConfig(),
				Check: resource.ComposeTestCheckFunc(
					// Defaults must be the concrete schema values, not null,
					// even though the API response omitted them.
					resource.TestCheckResourceAttr("latitudesh_object_storage.test", "storage_class", "standard"),
					resource.TestCheckResourceAttr("latitudesh_object_storage.test", "versioning", "false"),
					resource.TestCheckResourceAttr("latitudesh_object_storage.test", "locking", "false"),
					resource.TestCheckResourceAttr("latitudesh_object_storage.test", "retention_mode", "NONE"),
				),
			},
			{
				// Refresh reads the same omitting response; the plan must be
				// empty (defaults preserved, no drift to null).
				Config:   testAccBucketDefaultsConfig(),
				PlanOnly: true,
			},
		},
	})
}

// Regression for the e2e import failure: the live API sends the region slug
// only under region.site.slug (no region-level id), so without the sanitizer
// lift an imported bucket lost its `region` attribute. Import against a
// realistic full payload must round-trip every attribute.
func TestAccObjectStorage_ImportRoundTripsRegion(t *testing.T) {
	mock := &mockBucketDefaultsAPI{full: true}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccBucketDefaultsConfig(),
				Check: resource.TestCheckResourceAttr(
					"latitudesh_object_storage.test", "region", "ASH"),
			},
			{
				ResourceName:      "latitudesh_object_storage.test",
				ImportState:       true,
				ImportStateVerify: true,
				// project is identified by slug on read but configured by id;
				// the live test ignores it for the same reason.
				ImportStateVerifyIgnore: []string{"project"},
			},
		},
	})
}
