package latitudesh

// These tests exercise the operating system data source against a local mock
// of the Latitude.sh API, injected through the provider's httpClient (the
// same hook the VCR tests use). They run under TF_ACC without requiring
// credentials.
//
// The mock paginates like the real API: it honours page[size] (the SDK always
// sends its default of 20) and page[number], and returns an empty page past the
// end. GET /plans/operating_systems has no single-item read, so findOne walks
// pages via the SDK's Next(), which — given no explicit page size — only stops
// when a page comes back empty. A mock that ignored page[number] and always
// returned the same page would therefore never terminate on a miss; that is
// exactly the regression these tests guard. The catalog is padded with fillers
// so that the last real entry lands on page 2 under the default page size.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

// mockOSDefaultPageSize mirrors the API's default page[size]; the SDK sends it
// explicitly, so the mock only falls back to it if the parameter is absent.
const mockOSDefaultPageSize = 20

type mockOperatingSystemAPI struct {
	calls    int32 // requests served, any path
	lastPage int32 // page[number] of the most recent request
}

func newMockOperatingSystemAPI() *mockOperatingSystemAPI {
	return &mockOperatingSystemAPI{}
}

// catalog is 21 entries long: two named entries and 18 fillers fill page 1 at
// the default page size, and the last named entry is the only one on page 2.
func (m *mockOperatingSystemAPI) catalog() []any {
	entries := []any{
		map[string]any{
			"id":   "os_mock_1",
			"type": "operating_system",
			"attributes": map[string]any{
				"name":             "Ubuntu 24.04",
				"slug":             "ubuntu_24_04_x64_lts",
				"distro":           "ubuntu",
				"user":             "ubuntu",
				"version":          "24.04",
				"provisionable_on": []string{"c3.small.x86", "m4.metal.small"},
				"features": map[string]any{
					"raid":       true,
					"ssh_keys":   true,
					"user_data":  true,
					"accelerate": false,
					"rescue":     true,
					"workflow":   false,
				},
			},
		},
		map[string]any{
			"id":   "os_mock_2",
			"type": "operating_system",
			"attributes": map[string]any{
				"name":    "CentOS 7.4",
				"slug":    "centos_7_4_x64",
				"distro":  "centos",
				"user":    "root",
				"version": "7.4",
			},
		},
	}
	for i := len(entries); i < mockOSDefaultPageSize; i++ {
		entries = append(entries, map[string]any{
			"id":   fmt.Sprintf("os_fill_%d", i),
			"type": "operating_system",
			"attributes": map[string]any{
				"name":    fmt.Sprintf("Filler %d", i),
				"slug":    fmt.Sprintf("filler_%d", i),
				"distro":  "filler",
				"user":    "root",
				"version": "0",
			},
		})
	}
	// Lives on page 2 only.
	return append(entries, map[string]any{
		"id":   "os_mock_3",
		"type": "operating_system",
		"attributes": map[string]any{
			"name":    "Debian 12",
			"slug":    "debian_12",
			"distro":  "debian",
			"user":    "debian",
			"version": "12",
		},
	})
}

func (m *mockOperatingSystemAPI) handler(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt32(&m.calls, 1)
	w.Header().Set("Content-Type", "application/vnd.api+json")

	if r.Method != http.MethodGet || r.URL.Path != "/plans/operating_systems" {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page[number]"))
	if page < 1 {
		page = 1
	}
	atomic.StoreInt32(&m.lastPage, int32(page))

	size := mockOSDefaultPageSize
	if requested, err := strconv.Atoi(r.URL.Query().Get("page[size]")); err == nil && requested > 0 {
		size = requested
	}

	all := m.catalog()
	start := (page - 1) * size
	end := start + size
	if start > len(all) {
		start = len(all)
	}
	if end > len(all) {
		end = len(all)
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": all[start:end],
		"meta": map[string]any{},
	})
}

func testAccOperatingSystemConfig(selector string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

data "latitudesh_operating_system" "test" {
%s
}
`, selector)
}

func TestAccOperatingSystem_BySlug(t *testing.T) {
	mock := newMockOperatingSystemAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccOperatingSystemConfig(`  slug = "ubuntu_24_04_x64_lts"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "id", "os_mock_1"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "name", "Ubuntu 24.04"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "distro", "ubuntu"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "user", "ubuntu"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "version", "24.04"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "provisionable_on.#", "2"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "features.raid", "true"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "features.accelerate", "false"),
				),
			},
		},
	})
}

func TestAccOperatingSystem_ByID(t *testing.T) {
	mock := newMockOperatingSystemAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccOperatingSystemConfig(`  id = "os_mock_2"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "slug", "centos_7_4_x64"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "user", "root"),
				),
			},
		},
	})
}

func TestAccOperatingSystem_ByName(t *testing.T) {
	mock := newMockOperatingSystemAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccOperatingSystemConfig(`  name = "CentOS 7.4"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "id", "os_mock_2"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "slug", "centos_7_4_x64"),
				),
			},
		},
	})
}

// TestAccOperatingSystem_BeyondFirstPage: an operating system that only exists
// on page 2 must still be found, proving the walk actually follows Next().
func TestAccOperatingSystem_BeyondFirstPage(t *testing.T) {
	mock := newMockOperatingSystemAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccOperatingSystemConfig(`  slug = "debian_12"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "id", "os_mock_3"),
					resource.TestCheckResourceAttr("data.latitudesh_operating_system.test", "distro", "debian"),
				),
			},
		},
	})

	if got := atomic.LoadInt32(&mock.lastPage); got < 2 {
		t.Fatalf("expected the lookup to reach page 2, highest page requested was %d", got)
	}
}

// TestAccOperatingSystem_NotFound: a miss must walk past the last page and
// terminate on the empty one instead of looping on page 1 forever.
func TestAccOperatingSystem_NotFound(t *testing.T) {
	mock := newMockOperatingSystemAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config:      testAccOperatingSystemConfig(`  slug = "does-not-exist"`),
				ExpectError: regexp.MustCompile(`(?i)not found`),
			},
		},
	})

	if got := atomic.LoadInt32(&mock.lastPage); got < 2 {
		t.Fatalf("expected the miss to walk beyond page 1, highest page requested was %d", got)
	}
}

// TestAccOperatingSystem_BlankSelector: a whitespace-only selector passes
// ExactlyOneOf (it is set, just empty) and must be rejected before any API
// call rather than scanned for and reported as "not found".
func TestAccOperatingSystem_BlankSelector(t *testing.T) {
	mock := newMockOperatingSystemAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config:      testAccOperatingSystemConfig(`  slug = "   "`),
				ExpectError: regexp.MustCompile(`(?i)blank selector`),
			},
		},
	})

	if got := atomic.LoadInt32(&mock.calls); got != 0 {
		t.Fatalf("blank selector reached the API %d time(s); want 0", got)
	}
}
