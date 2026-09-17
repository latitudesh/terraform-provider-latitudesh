package latitudesh

// The mock-backed tests exercise the BlockStorage resource against a local
// mock of the Latitude.sh API, using the same httpClient injection hook as
// the virtual machine site tests (testAccProtoV6ProviderFactoriesWithMock /
// mockRedirectTransport, defined in resource_virtual_machine_site_test.go).
// They run under plain `go test` (IsUnitTest: true), no credentials needed.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// mockVolume is one volume tracked by the mock BlockStorage API.
type mockVolume struct {
	id          string
	project     string
	name        string
	region      string
	sizeInGb    int64
	namespaceID string
	connectorID string
	createdAt   string
	deleted     bool
}

type mockBlockStorageAPI struct {
	mu      sync.Mutex
	volumes map[string]*mockVolume
	nextID  int
}

func newMockBlockStorageAPI() *mockBlockStorageAPI {
	return &mockBlockStorageAPI{volumes: map[string]*mockVolume{}}
}

func (m *mockBlockStorageAPI) envelope(v *mockVolume) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"id":   v.id,
			"type": "volumes",
			"attributes": map[string]any{
				"name":         v.name,
				"size_in_gb":   v.sizeInGb,
				"created_at":   v.createdAt,
				"namespace_id": v.namespaceID,
				"connector_id": v.connectorID,
				"region": map[string]any{
					"city":    "Ashburn",
					"country": "US",
					"site": map[string]any{
						"id":       "site_" + v.region,
						"name":     v.region,
						"slug":     v.region,
						"facility": "AM1",
					},
				},
				"project": map[string]any{
					"id":   v.project,
					"slug": "test-project",
				},
			},
		},
	}
}

func (m *mockBlockStorageAPI) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/storage/volumes":
		var payload struct {
			Data struct {
				Attributes struct {
					Project  string `json:"project"`
					Name     string `json:"name"`
					Region   string `json:"region"`
					SizeInGb int64  `json:"size_in_gb"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		m.nextID++
		v := &mockVolume{
			id:          "vol_mock_" + strconv.Itoa(m.nextID),
			project:     payload.Data.Attributes.Project,
			name:        payload.Data.Attributes.Name,
			region:      payload.Data.Attributes.Region,
			sizeInGb:    payload.Data.Attributes.SizeInGb,
			namespaceID: "ns_mock_" + strconv.Itoa(m.nextID),
			connectorID: "conn_mock_" + strconv.Itoa(m.nextID),
			createdAt:   fmt.Sprintf("2026-01-%02dT00:00:00Z", m.nextID),
		}
		m.volumes[v.id] = v
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(m.envelope(v))

	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/storage/volumes/"):
		id := strings.TrimPrefix(r.URL.Path, "/storage/volumes/")
		v, ok := m.volumes[id]
		if !ok || v.deleted {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.envelope(v))

	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/storage/volumes/"):
		id := strings.TrimPrefix(r.URL.Path, "/storage/volumes/")
		v, ok := m.volumes[id]
		if !ok || v.deleted {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		v.deleted = true
		w.WriteHeader(http.StatusNoContent)

	case r.Method == http.MethodGet && r.URL.Path == "/storage/volumes":
		filterProject := r.URL.Query().Get("filter[project]")
		data := make([]map[string]any, 0)
		for _, v := range m.volumes {
			if v.deleted {
				continue
			}
			if filterProject != "" && v.project != filterProject {
				continue
			}
			data = append(data, m.envelope(v)["data"].(map[string]any))
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})

	default:
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

func (m *mockBlockStorageAPI) exists(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.volumes[id]
	return ok && !v.deleted
}

func testAccCheckMockBlockStorageDestroyed(mock *mockBlockStorageAPI) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "latitudesh_block_storage" {
				continue
			}
			if mock.exists(rs.Primary.ID) {
				return fmt.Errorf("mock volume %s still exists after destroy", rs.Primary.ID)
			}
		}
		return nil
	}
}

func testAccBlockStorageConfig(project, name, region string, sizeInGb int) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_block_storage" "test_item" {
  project    = %q
  name       = %q
  region     = %q
  size_in_gb = %d
}
`, project, name, region, sizeInGb)
}

func TestBlockStorage_CreateReadDelete(t *testing.T) {
	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBlockStorageDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccBlockStorageConfig("test-project", "app-data", "ASH", 100),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "project", "test-project"),
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "name", "app-data"),
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "region", "ASH"),
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "size_in_gb", "100"),
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "namespace_id", "ns_mock_1"),
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "connector_id", "conn_mock_1"),
				),
			},
			{
				// Idempotency — same config, plan must be empty.
				Config:   testAccBlockStorageConfig("test-project", "app-data", "ASH", 100),
				PlanOnly: true,
			},
		},
	})
}

// Every Required/RequiresReplace input must survive import, or the first
// plan after import proposes a replacement.
func TestBlockStorage_Import(t *testing.T) {
	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBlockStorageDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccBlockStorageConfig("test-project", "app-data", "ASH", 50),
			},
			{
				ResourceName:      "latitudesh_block_storage.test_item",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// Changing size_in_gb (or any other input) must force a replacement, since
// the SDK exposes no update call for volumes.
func TestBlockStorage_SizeChangeForcesReplacement(t *testing.T) {
	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBlockStorageDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccBlockStorageConfig("test-project", "app-data", "ASH", 50),
				Check:  resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "size_in_gb", "50"),
			},
			{
				Config: testAccBlockStorageConfig("test-project", "app-data", "ASH", 100),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "size_in_gb", "100"),
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "namespace_id", "ns_mock_2"),
				),
			},
		},
	})
}

// blockStorageNotFound must accept only a 404 APIError, never a 403 or a
// transient 5xx (which would otherwise read as "gone" and drop a live volume
// from state, or make destroy report false success).
func TestBlockStorageNotFound(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"404 APIError", &components.APIError{StatusCode: http.StatusNotFound, Message: "not found"}, true},
		{"403 APIError", &components.APIError{StatusCode: http.StatusForbidden, Message: "forbidden"}, false},
		{"500 APIError", &components.APIError{StatusCode: http.StatusInternalServerError, Message: "boom"}, false},
		{"plain error", fmt.Errorf("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := blockStorageNotFound(tc.err); got != tc.want {
				t.Errorf("blockStorageNotFound(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// mapBlockStorageVolume must null purely-computed fields on absence, and only
// backfill project/region when state has nothing (import), never clobbering a
// configured slug with the API's canonical form.
func TestMapBlockStorageVolume(t *testing.T) {
	t.Run("nil attributes leave id set and everything else untouched", func(t *testing.T) {
		var data BlockStorageResourceModel
		id := "vol_1"
		mapBlockStorageVolume(&data, &components.VolumeData{ID: &id})
		if data.ID.ValueString() != "vol_1" {
			t.Fatalf("ID = %v, want vol_1", data.ID)
		}
		if !data.Name.IsNull() {
			t.Fatalf("Name = %v, want null (no attributes)", data.Name)
		}
	})

	t.Run("purely computed fields null on absence", func(t *testing.T) {
		data := BlockStorageResourceModel{
			NamespaceID: types.StringValue("stale"),
			ConnectorID: types.StringValue("stale"),
		}
		id := "vol_1"
		name := "app-data"
		size := int64(100)
		mapBlockStorageVolume(&data, &components.VolumeData{
			ID: &id,
			Attributes: &components.VolumeDataAttributes{
				Name:     &name,
				SizeInGb: &size,
			},
		})
		if !data.NamespaceID.IsNull() {
			t.Errorf("NamespaceID = %v, want null", data.NamespaceID)
		}
		if !data.ConnectorID.IsNull() {
			t.Errorf("ConnectorID = %v, want null", data.ConnectorID)
		}
	})

	t.Run("project and region only backfilled when unset", func(t *testing.T) {
		configuredProject := types.StringValue("my-slug")
		configuredRegion := types.StringValue("ash")
		data := BlockStorageResourceModel{Project: configuredProject, Region: configuredRegion}

		apiProjectID := "proj_mock_1"
		apiSlug := "canonical-slug"
		apiSiteSlug := "ASH"
		mapBlockStorageVolume(&data, &components.VolumeData{
			Attributes: &components.VolumeDataAttributes{
				Project: &components.ProjectInclude{ID: &apiProjectID, Slug: &apiSlug},
				Region:  &components.VolumeDataRegion{Site: &components.VolumeDataSite{Slug: &apiSiteSlug}},
			},
		})
		if data.Project.ValueString() != "my-slug" {
			t.Errorf("Project = %v, want unchanged 'my-slug'", data.Project)
		}
		if data.Region.ValueString() != "ash" {
			t.Errorf("Region = %v, want unchanged 'ash'", data.Region)
		}

		// Now with nothing configured (import path): both backfill from the API.
		var imported BlockStorageResourceModel
		mapBlockStorageVolume(&imported, &components.VolumeData{
			Attributes: &components.VolumeDataAttributes{
				Project: &components.ProjectInclude{ID: &apiProjectID, Slug: &apiSlug},
				Region:  &components.VolumeDataRegion{Site: &components.VolumeDataSite{Slug: &apiSiteSlug}},
			},
		})
		if imported.Project.ValueString() != apiSlug {
			t.Errorf("imported Project = %v, want %v (slug preferred over id)", imported.Project, apiSlug)
		}
		if imported.Region.ValueString() != apiSiteSlug {
			t.Errorf("imported Region = %v, want %v", imported.Region, apiSiteSlug)
		}
	})
}

// TestAccBlockStorage_Basic exercises create, read and import against the
// live API. The site (testBlockStorageSite) is not documented anywhere on
// the BlockStorage group itself; ASH is reused from latitudesh_object_storage,
// whose `region` attribute has the identical "site slug" doc comment - a
// human should confirm ASH actually has block storage capacity before
// relying on this test. The project is configured by slug (not ID) so the
// value round-trips exactly through import: readBlockStorageInto backfills
// `project` from the API's echoed slug, and an ID-configured project would
// otherwise read as a mismatch on ImportStateVerify (the trap documented for
// latitudesh_object_storage's own live test, which papers over it with
// ImportStateVerifyIgnore instead of configuring around it).
const testBlockStorageSite = "ASH"

func TestAccBlockStorage_Basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	resourceName := "latitudesh_block_storage.test_item"
	projectSlug := testAccProjectSlug(t)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckBlockStorageDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccConfigBlockStorage(projectSlug, "tf-acc-block-storage-"+testRunID, testBlockStorageSite, 50),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "name", "tf-acc-block-storage-"+testRunID),
					resource.TestCheckResourceAttr(resourceName, "region", testBlockStorageSite),
					resource.TestCheckResourceAttr(resourceName, "project", projectSlug),
					resource.TestCheckResourceAttr(resourceName, "size_in_gb", "50"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccConfigBlockStorage(project, name, region string, sizeInGb int) string {
	return fmt.Sprintf(`
resource "latitudesh_block_storage" "test_item" {
  project    = %q
  name       = %q
  region     = %q
  size_in_gb = %d
}
`, project, name, region, sizeInGb)
}

func testAccCheckBlockStorageDestroy(s *terraform.State) error {
	ctx := context.Background()
	client, err := newSDKClientFromEnv()
	if err != nil {
		return err
	}

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_block_storage" {
			continue
		}
		id := rs.Primary.ID
		if id == "" {
			continue
		}

		resp, err := client.BlockStorage.GetStorageVolume(ctx, id)
		if err == nil && resp != nil && resp.Object != nil && resp.Object.Data != nil &&
			resp.Object.Data.ID != nil && *resp.Object.Data.ID == id {
			return fmt.Errorf("block storage volume still exists: %s", id)
		}
		if err != nil && !blockStorageNotFound(err) {
			return fmt.Errorf("checking block storage volume %s destroyed: %w", id, err)
		}
	}
	return nil
}
