package latitudesh

// These tests exercise the namespace_id sanitizer against the payload the live
// API actually returns for volumes provisioned through the initiators/mount
// flow (`"namespace_id": 1`, a number for a field the swagger declares as a
// nullable string). They run under plain `go test`, no credentials needed.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// legacyVolumeAttributes is trimmed from a live GET /storage/volumes response
// (volume test-pery-bs, 2026-09-21): namespace_id is a number, connector_id a
// string, and the mapping-era fields are absent.
const legacyVolumeAttributes = `{
  "name": "test-pery-bs",
  "size_in_gb": 32,
  "created_at": "2025-11-17T19:34:26.725Z",
  "namespace_id": 1,
  "connector_id": "nqn.2001-07.com.ceph:jv6m5JyZDNLPe-test-pery-bs",
  "initiators": [{"nqn": "nqn.2014-08.org.nvmexpress:uuid:6e839400", "mounted_at": "2025-11-17T19:45:11Z"}],
  "block": null,
  "region": {"city": "Dallas", "country": "United States", "site": {"id": "loc_XZWr75ZqNA91O", "name": "Dallas", "slug": "DAL", "facility": "Cologix"}},
  "project": {"id": "proj_jv6m5JyZDNLPe", "name": "DevTools QA", "slug": "devtools-qa"}
}`

func legacyVolumeEnvelope(t *testing.T, id string, list bool) []byte {
	t.Helper()

	entry := fmt.Sprintf(`{"id": %q, "type": "volumes", "attributes": %s}`, id, legacyVolumeAttributes)
	if list {
		return []byte(fmt.Sprintf(`{"data": [%s]}`, entry))
	}
	return []byte(fmt.Sprintf(`{"data": %s}`, entry))
}

func TestIsBlockStorageVolumePath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/storage/volumes", true},
		{"/storage/volumes/", true},
		{"/storage/volumes/vol_123", true},
		{"/storage/volumes/vol_123/map", true},
		{"/storage/volumes/vol_123/mount", true},
		{"/storage/buckets", false},
		{"/storage/filesystems", false},
		{"/servers", false},
	}

	for _, tc := range cases {
		if got := isBlockStorageVolumePath(tc.path); got != tc.want {
			t.Errorf("isBlockStorageVolumePath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// Without the sanitizer the SDK cannot unmarshal the payload at all, so this
// asserts both halves: the raw body fails, the sanitized one parses.
func TestSanitizeBlockStorageBody(t *testing.T) {
	t.Run("numeric namespace_id is coerced to a string", func(t *testing.T) {
		raw := legacyVolumeEnvelope(t, "vol_zrPm0dmlNKOw3", false)

		var unsanitized struct {
			Data components.VolumeData `json:"data"`
		}
		if err := json.Unmarshal(raw, &unsanitized); err == nil {
			t.Fatal("expected the raw payload to fail unmarshaling; the sanitizer is no longer needed")
		}

		fixed, changed := sanitizeBlockStorageBody(raw)
		if !changed {
			t.Fatal("sanitizeBlockStorageBody reported no change")
		}

		var sanitized struct {
			Data components.VolumeData `json:"data"`
		}
		if err := json.Unmarshal(fixed, &sanitized); err != nil {
			t.Fatalf("sanitized payload still fails to unmarshal: %v", err)
		}
		if sanitized.Data.Attributes == nil || sanitized.Data.Attributes.NamespaceID == nil {
			t.Fatal("namespace_id is missing after sanitizing")
		}
		if got := *sanitized.Data.Attributes.NamespaceID; got != "1" {
			t.Errorf("namespace_id = %q, want \"1\"", got)
		}
		// Untouched numbers must survive verbatim, not via float64.
		if sanitized.Data.Attributes.SizeInGb == nil || *sanitized.Data.Attributes.SizeInGb != 32 {
			t.Errorf("size_in_gb = %v, want 32", sanitized.Data.Attributes.SizeInGb)
		}
	})

	t.Run("lists sanitize every entry", func(t *testing.T) {
		fixed, changed := sanitizeBlockStorageBody(legacyVolumeEnvelope(t, "vol_1", true))
		if !changed {
			t.Fatal("sanitizeBlockStorageBody reported no change")
		}
		var sanitized struct {
			Data []components.VolumeData `json:"data"`
		}
		if err := json.Unmarshal(fixed, &sanitized); err != nil {
			t.Fatalf("sanitized list still fails to unmarshal: %v", err)
		}
		if len(sanitized.Data) != 1 || sanitized.Data[0].Attributes.NamespaceID == nil {
			t.Fatalf("unexpected sanitized list: %+v", sanitized.Data)
		}
	})

	t.Run("already-valid payloads pass through untouched", func(t *testing.T) {
		body := []byte(`{"data":{"id":"vol_1","type":"volumes","attributes":{"name":"x","namespace_id":null,"size_in_gb":32}}}`)
		if _, changed := sanitizeBlockStorageBody(body); changed {
			t.Error("sanitizeBlockStorageBody rewrote a payload that needed no fixing")
		}
	})

	t.Run("non-JSON bodies pass through untouched", func(t *testing.T) {
		if _, changed := sanitizeBlockStorageBody([]byte("not json")); changed {
			t.Error("sanitizeBlockStorageBody rewrote a non-JSON body")
		}
	})
}

// End to end through the provider's transport chain: a volume the live API
// reports with a numeric namespace_id must be readable by both data sources.
func TestBlockStorageDataSource_LegacyNumericNamespaceID(t *testing.T) {
	const volumeID = "vol_zrPm0dmlNKOw3"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		switch r.URL.Path {
		case "/storage/volumes":
			_, _ = w.Write(legacyVolumeEnvelope(t, volumeID, true))
		case "/storage/volumes/" + volumeID:
			_, _ = w.Write(legacyVolumeEnvelope(t, volumeID, false))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
		}
	}))
	defer server.Close()

	config := fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

data "latitudesh_block_storage" "by_id" {
  id = %q
}

data "latitudesh_block_storages" "all" {}
`, volumeID)

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_block_storage.by_id", "namespace_id", "1"),
					resource.TestCheckResourceAttr("data.latitudesh_block_storage.by_id", "name", "test-pery-bs"),
					resource.TestCheckResourceAttr("data.latitudesh_block_storage.by_id", "region", "DAL"),
					resource.TestCheckResourceAttr("data.latitudesh_block_storages.all", "volumes.#", "1"),
					resource.TestCheckResourceAttr("data.latitudesh_block_storages.all", "volumes.0.namespace_id", "1"),
				),
			},
		},
	})
}
