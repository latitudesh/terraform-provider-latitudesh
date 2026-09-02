package latitudesh

// These tests exercise the retention_period sanitizer against a local mock of
// the Latitude.sh API returning the malformed payloads observed live (the API
// emits "" / "30d" strings for a field the swagger declares as integer),
// injected through the provider's httpClient like the other mock suites. They
// run under TF_ACC without requiring credentials.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestParseRetentionDays(t *testing.T) {
	cases := []struct {
		in     string
		want   int64
		wantOK bool
	}{
		{"", 0, false},
		{"0", 0, false},
		{"0d", 0, false},
		{"30", 30, true},
		{"30d", 30, true},
		{"30D", 30, true},
		{" 30d ", 30, true},
		{"1y", 0, false},
		{"abc", 0, false},
		{"-5", 0, false},
	}

	for _, tc := range cases {
		got, ok := parseRetentionDays(tc.in)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("parseRetentionDays(%q) = (%d, %v), want (%d, %v)", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestIsObjectStorageBucketPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/storage/buckets", true},
		{"/storage/buckets/", true},
		{"/storage/buckets/bkt_123", true},
		{"/storage/buckets/bkt_123/access_keys", false},
		{"/storage/buckets/bkt_123/lifecycle_rules", false},
		{"/storage/access_keys", false},
		{"/servers", false},
	}

	for _, tc := range cases {
		if got := isObjectStorageBucketPath(tc.path); got != tc.want {
			t.Errorf("isObjectStorageBucketPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestSanitizeObjectStorageBody(t *testing.T) {
	t.Run("empty string is dropped", func(t *testing.T) {
		body := []byte(`{"data":{"id":"bkt_1","type":"object_storages","attributes":{"locking":false,"retention_mode":"NONE","retention_period":""}}}`)
		fixed, changed := sanitizeObjectStorageBody(body)
		if !changed {
			t.Fatal("expected body to change")
		}
		attrs := decodeAttrs(t, fixed)
		if _, present := attrs["retention_period"]; present {
			t.Errorf("retention_period should have been dropped, got %v", attrs["retention_period"])
		}
	})

	t.Run("duration string is coerced to days", func(t *testing.T) {
		body := []byte(`{"data":{"id":"bkt_1","attributes":{"retention_period":"30d"}}}`)
		fixed, changed := sanitizeObjectStorageBody(body)
		if !changed {
			t.Fatal("expected body to change")
		}
		attrs := decodeAttrs(t, fixed)
		if got := attrs["retention_period"]; got != float64(30) {
			t.Errorf("retention_period = %v, want 30", got)
		}
	})

	t.Run("numeric value passes through untouched", func(t *testing.T) {
		body := []byte(`{"data":{"id":"bkt_1","attributes":{"retention_period":30}}}`)
		if _, changed := sanitizeObjectStorageBody(body); changed {
			t.Error("numeric retention_period should not trigger a rewrite")
		}
	})

	t.Run("list payloads fix every entry", func(t *testing.T) {
		body := []byte(`{"data":[
			{"id":"bkt_1","attributes":{"retention_period":""}},
			{"id":"bkt_2","attributes":{"retention_period":"15"}},
			{"id":"bkt_3","attributes":{"locking":false}}
		]}`)
		fixed, changed := sanitizeObjectStorageBody(body)
		if !changed {
			t.Fatal("expected body to change")
		}
		var payload struct {
			Data []struct {
				Attributes map[string]any `json:"attributes"`
			} `json:"data"`
		}
		if err := json.Unmarshal(fixed, &payload); err != nil {
			t.Fatalf("unmarshal fixed body: %v", err)
		}
		if _, present := payload.Data[0].Attributes["retention_period"]; present {
			t.Error("bkt_1 retention_period should have been dropped")
		}
		if got := payload.Data[1].Attributes["retention_period"]; got != float64(15) {
			t.Errorf("bkt_2 retention_period = %v, want 15", got)
		}
	})

	t.Run("invalid json passes through", func(t *testing.T) {
		body := []byte(`not json`)
		if _, changed := sanitizeObjectStorageBody(body); changed {
			t.Error("invalid JSON should not be rewritten")
		}
	})

	t.Run("region site slug is lifted into region id", func(t *testing.T) {
		body := []byte(`{"data":{"id":"bkt_1","attributes":{"region":{"city":"Ashburn","country":"United States","site":{"id":"loc_1","slug":"ASH"}}}}}`)
		fixed, changed := sanitizeObjectStorageBody(body)
		if !changed {
			t.Fatal("expected body to change")
		}
		attrs := decodeAttrs(t, fixed)
		region, _ := attrs["region"].(map[string]any)
		if got := region["id"]; got != "ASH" {
			t.Errorf("region.id = %v, want ASH", got)
		}
	})

	t.Run("existing region id is untouched", func(t *testing.T) {
		body := []byte(`{"data":{"id":"bkt_1","attributes":{"region":{"id":"DAL","site":{"slug":"ASH"}}}}}`)
		if _, changed := sanitizeObjectStorageBody(body); changed {
			t.Error("a present region.id should not trigger a rewrite")
		}
	})

	t.Run("region without site passes through", func(t *testing.T) {
		body := []byte(`{"data":{"id":"bkt_1","attributes":{"region":{"city":"Ashburn"}}}}`)
		if _, changed := sanitizeObjectStorageBody(body); changed {
			t.Error("region without a site should not trigger a rewrite")
		}
	})
}

func decodeAttrs(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var payload struct {
		Data struct {
			Attributes map[string]any `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal sanitized body: %v", err)
	}
	return payload.Data.Attributes
}

// mockObjectStorageAPI reproduces the live payload that broke the data source:
// a VAST-backed bucket whose retention_period comes back as "" when locking is
// off, plus a locked one echoing a "30d" duration string.
type mockObjectStorageAPI struct{}

func (m *mockObjectStorageAPI) bucketEnvelope() map[string]any {
	return map[string]any{
		"data": m.bucketData("bkt_mock_1", "dark-star-mock", ""),
	}
}

func (m *mockObjectStorageAPI) listEnvelope() map[string]any {
	return map[string]any{
		"data": []any{
			m.bucketData("bkt_mock_1", "dark-star-mock", ""),
			m.bucketData("bkt_mock_2", "locked-mock", "30d"),
		},
	}
}

func (m *mockObjectStorageAPI) bucketData(id, name string, retention any) map[string]any {
	return map[string]any{
		"id":   id,
		"type": "object_storages",
		"attributes": map[string]any{
			"name":             name,
			"storage_type":     "object",
			"storage_class":    "high_performance",
			"created_at":       "2026-09-01T16:02:59.403Z",
			"bucket_name":      name,
			"endpoint":         "https://objects.ash.storage.sh",
			"versioning":       false,
			"locking":          retention != "",
			"retention_mode":   "NONE",
			"retention_period": retention,
			"source":           "default",
			"region": map[string]any{
				"city":    "Ashburn",
				"country": "United States",
				"site":    map[string]any{"id": "loc_mock", "name": "Ashburn", "slug": "ASH", "facility": "DEFT IAD2"},
			},
			"project": map[string]any{"id": "proj_mock", "name": "Mock", "slug": "mock"},
		},
	}
}

func (m *mockObjectStorageAPI) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.api+json")

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/storage/buckets/bkt_mock_1":
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.bucketEnvelope())

	case r.Method == http.MethodGet && r.URL.Path == "/storage/buckets":
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.listEnvelope())

	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

func testAccObjectStorageDataSourceConfig(selector string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

data "latitudesh_object_storage" "test" {
%s
}
`, selector)
}

// Regression: GET /storage/buckets/{id} returning `"retention_period": ""`
// used to fail the whole read with "cannot unmarshal string into Go value of
// type int64".
func TestAccObjectStorage_EmptyRetentionByID(t *testing.T) {
	mock := &mockObjectStorageAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccObjectStorageDataSourceConfig(`  id = "bkt_mock_1"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_object_storage.test", "name", "dark-star-mock"),
					resource.TestCheckResourceAttr("data.latitudesh_object_storage.test", "endpoint", "https://objects.ash.storage.sh"),
					resource.TestCheckNoResourceAttr("data.latitudesh_object_storage.test", "retention_period"),
					// region comes from region.site.slug lifted into region.id
					resource.TestCheckResourceAttr("data.latitudesh_object_storage.test", "region", "ASH"),
				),
			},
		},
	})
}

// Regression: the list endpoint used by name lookups carried the same
// malformed field and failed with the same unmarshal error.
func TestAccObjectStorage_RetentionStringsByName(t *testing.T) {
	mock := &mockObjectStorageAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccObjectStorageDataSourceConfig(`  name = "dark-star-mock"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_object_storage.test", "id", "bkt_mock_1"),
					resource.TestCheckNoResourceAttr("data.latitudesh_object_storage.test", "retention_period"),
				),
			},
			{
				Config: testAccObjectStorageDataSourceConfig(`  name = "locked-mock"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_object_storage.test", "id", "bkt_mock_2"),
					resource.TestCheckResourceAttr("data.latitudesh_object_storage.test", "retention_period", "30"),
				),
			},
		},
	})
}
