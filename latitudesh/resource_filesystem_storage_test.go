package latitudesh

// The mock-backed tests exercise the FilesystemStorage resource against a
// local mock of the Latitude.sh API, using the same httpClient injection hook
// as the virtual machine site tests (testAccProtoV6ProviderFactoriesWithMock /
// mockRedirectTransport, defined in resource_virtual_machine_site_test.go).
// They run under plain `go test` (IsUnitTest: true), no credentials needed.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// mockFilesystem is one filesystem tracked by the mock FilesystemStorage API.
type mockFilesystem struct {
	id           string
	project      string
	name         string
	region       string
	sizeInGb     int64
	protocols    []string
	storageClass string
	deleted      bool
}

type mockFilesystemStorageAPI struct {
	mu     sync.Mutex
	items  map[string]*mockFilesystem
	nextID int

	// rejectResize answers resizes with the 422 the API returns when it will
	// not grow a filesystem, independent of the shrink-is-a-replace rule the
	// provider itself enforces client-side.
	rejectResize bool

	// calls records every mutating request in order, so tests can assert the
	// sequence Terraform drove.
	calls []string
}

func newMockFilesystemStorageAPI() *mockFilesystemStorageAPI {
	return &mockFilesystemStorageAPI{items: map[string]*mockFilesystem{}}
}

func (m *mockFilesystemStorageAPI) callLog() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...)
}

func (m *mockFilesystemStorageAPI) exists(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.items[id]
	return ok && !v.deleted
}

func (m *mockFilesystemStorageAPI) envelope(v *mockFilesystem) map[string]any {
	return map[string]any{
		"id":   v.id,
		"type": "filesystems",
		"attributes": map[string]any{
			"name":           v.name,
			"size_in_gb":     v.sizeInGb,
			"storage_class":  v.storageClass,
			"protocols":      v.protocols,
			"nfs_mount_path": nil,
			"file_endpoint":  nil,
			"keyring":        nil,
			"cluster_user":   nil,
			"volume_path":    nil,
			"created_at":     "2026-01-01T00:00:00Z",
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
	}
}

func (m *mockFilesystemStorageAPI) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if r.Method != http.MethodGet {
		m.calls = append(m.calls, r.Method+" "+r.URL.Path)
	}

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/storage/filesystems":
		var payload struct {
			Data struct {
				Attributes struct {
					Project   string   `json:"project"`
					Name      string   `json:"name"`
					Region    string   `json:"region"`
					SizeInGb  int64    `json:"size_in_gb"`
					Protocols []string `json:"protocols"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		m.nextID++
		v := &mockFilesystem{
			id:           "fs_mock_" + strconv.Itoa(m.nextID),
			project:      payload.Data.Attributes.Project,
			name:         payload.Data.Attributes.Name,
			region:       payload.Data.Attributes.Region,
			sizeInGb:     payload.Data.Attributes.SizeInGb,
			protocols:    payload.Data.Attributes.Protocols,
			storageClass: "standard",
		}
		m.items[v.id] = v
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": m.envelope(v)})

	case r.Method == http.MethodGet && r.URL.Path == "/storage/filesystems":
		filterProject := r.URL.Query().Get("filter[project]")
		data := make([]map[string]any, 0)
		for _, v := range m.items {
			if v.deleted {
				continue
			}
			if filterProject != "" && v.project != filterProject {
				continue
			}
			data = append(data, m.envelope(v))
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})

	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/storage/filesystems/"):
		id := strings.TrimPrefix(r.URL.Path, "/storage/filesystems/")
		v, ok := m.items[id]
		if !ok || v.deleted {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		var payload struct {
			Data struct {
				Attributes struct {
					SizeInGb int64 `json:"size_in_gb"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if m.rejectResize {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"errors":[{"code":"STORAGE_OPERATION_UNSUPPORTED","status":"422","title":"Storage operation unsupported","detail":"Resizing is not available for this filesystem."}]}`))
			return
		}
		// Like the API, refuse anything but growth (a resize to the same size too).
		if payload.Data.Attributes.SizeInGb <= v.sizeInGb {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"errors":[{"code":"STORAGE_VALIDATION_FAILED","status":"422","title":"Storage Validation Failed","detail":"Filesystems can only be increased in size."}]}`))
			return
		}
		v.sizeInGb = payload.Data.Attributes.SizeInGb
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": m.envelope(v)})

	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/storage/filesystems/"):
		id := strings.TrimPrefix(r.URL.Path, "/storage/filesystems/")
		v, ok := m.items[id]
		if !ok || v.deleted {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		v.deleted = true
		w.WriteHeader(http.StatusNoContent)

	default:
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

func testAccCheckMockFilesystemStorageDestroyed(mock *mockFilesystemStorageAPI) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "latitudesh_filesystem_storage" {
				continue
			}
			if mock.exists(rs.Primary.ID) {
				return fmt.Errorf("mock filesystem %s still exists after destroy", rs.Primary.ID)
			}
		}
		return nil
	}
}

func testAccFilesystemStorageConfig(project, name, region string, sizeInGb int, protocols string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_filesystem_storage" "test_item" {
  project    = %q
  name       = %q
  region     = %q
  size_in_gb = %d
  protocols  = %s
}
`, project, name, region, sizeInGb, protocols)
}

func TestFilesystemStorage_CreateReadDelete(t *testing.T) {
	mock := newMockFilesystemStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockFilesystemStorageDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccFilesystemStorageConfig("test-project", "shared-data", "ASH", 1500, `["nfs4"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_filesystem_storage.test_item", "project", "test-project"),
					resource.TestCheckResourceAttr("latitudesh_filesystem_storage.test_item", "name", "shared-data"),
					resource.TestCheckResourceAttr("latitudesh_filesystem_storage.test_item", "region", "ASH"),
					resource.TestCheckResourceAttr("latitudesh_filesystem_storage.test_item", "size_in_gb", "1500"),
					resource.TestCheckResourceAttr("latitudesh_filesystem_storage.test_item", "storage_class", "standard"),
					resource.TestCheckResourceAttr("latitudesh_filesystem_storage.test_item", "protocols.#", "1"),
				),
			},
			{
				// Idempotency — same config, plan must be empty.
				Config:   testAccFilesystemStorageConfig("test-project", "shared-data", "ASH", 1500, `["nfs4"]`),
				PlanOnly: true,
			},
		},
	})
}

// Every Required/RequiresReplace input must survive import, or the first plan
// after import proposes a replacement.
func TestFilesystemStorage_Import(t *testing.T) {
	mock := newMockFilesystemStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockFilesystemStorageDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccFilesystemStorageConfig("test-project", "shared-data", "ASH", 1500, `["nfs3","nfs4"]`),
			},
			{
				ResourceName:      "latitudesh_filesystem_storage.test_item",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// countFilesystemCalls reports how many mutating requests start with prefix.
func countFilesystemCalls(mock *mockFilesystemStorageAPI, prefix string) int {
	n := 0
	for _, call := range mock.callLog() {
		if strings.HasPrefix(call, prefix) {
			n++
		}
	}
	return n
}

// Growing size_in_gb must resize the same filesystem in place
// (UpdateFilesystem), keeping its id and its data, instead of deleting it for
// a new one.
func TestFilesystemStorage_GrowResizesInPlace(t *testing.T) {
	mock := newMockFilesystemStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	const name = "latitudesh_filesystem_storage.test_item"
	var firstID string

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockFilesystemStorageDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccFilesystemStorageConfig("test-project", "shared-data", "ASH", 1500, `["nfs4"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(name, "size_in_gb", "1500"),
					func(s *terraform.State) error {
						rs, ok := s.RootModule().Resources[name]
						if !ok {
							return fmt.Errorf("%s not in state", name)
						}
						firstID = rs.Primary.ID
						return nil
					},
				),
			},
			{
				Config: testAccFilesystemStorageConfig("test-project", "shared-data", "ASH", 2000, `["nfs4"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(name, "size_in_gb", "2000"),
					func(s *terraform.State) error {
						if got := s.RootModule().Resources[name].Primary.ID; got != firstID {
							return fmt.Errorf("growing the filesystem replaced it: id %s became %s", firstID, got)
						}
						if n := countFilesystemCalls(mock, "DELETE "); n != 0 {
							return fmt.Errorf("growing the filesystem deleted it: %v", mock.callLog())
						}
						if n := countFilesystemCalls(mock, "PATCH /storage/filesystems/"+firstID); n != 1 {
							return fmt.Errorf("expected one resize of %s, got %d: %v", firstID, n, mock.callLog())
						}
						return nil
					},
				),
			},
			{
				// Idempotency — the resized filesystem must read back without drift.
				Config:   testAccFilesystemStorageConfig("test-project", "shared-data", "ASH", 2000, `["nfs4"]`),
				PlanOnly: true,
			},
		},
	})
}

// Filesystems cannot shrink, so a smaller size_in_gb must replace the
// filesystem rather than send a resize the API would reject.
func TestFilesystemStorage_ShrinkForcesReplacement(t *testing.T) {
	mock := newMockFilesystemStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	const name = "latitudesh_filesystem_storage.test_item"
	var firstID string

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockFilesystemStorageDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccFilesystemStorageConfig("test-project", "shared-data", "ASH", 2000, `["nfs4"]`),
				Check: func(s *terraform.State) error {
					rs, ok := s.RootModule().Resources[name]
					if !ok {
						return fmt.Errorf("%s not in state", name)
					}
					firstID = rs.Primary.ID
					return nil
				},
			},
			{
				Config: testAccFilesystemStorageConfig("test-project", "shared-data", "ASH", 1500, `["nfs4"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(name, "size_in_gb", "1500"),
					func(s *terraform.State) error {
						if got := s.RootModule().Resources[name].Primary.ID; got == firstID {
							return fmt.Errorf("shrinking kept filesystem %s instead of replacing it", firstID)
						}
						if n := countFilesystemCalls(mock, "PATCH "); n != 0 {
							return fmt.Errorf("shrinking sent a resize: %v", mock.callLog())
						}
						if mock.exists(firstID) {
							return fmt.Errorf("the replaced filesystem %s was not deleted", firstID)
						}
						return nil
					},
				),
			},
		},
	})
}

// A resize the API refuses must fail the apply with the API's message and
// leave the size in state untouched, rather than recording a resize that
// never happened.
func TestFilesystemStorage_ResizeFailureFailsApply(t *testing.T) {
	mock := newMockFilesystemStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	const name = "latitudesh_filesystem_storage.test_item"

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockFilesystemStorageDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccFilesystemStorageConfig("test-project", "shared-data", "ASH", 1500, `["nfs4"]`),
			},
			{
				PreConfig: func() {
					mock.mu.Lock()
					defer mock.mu.Unlock()
					mock.rejectResize = true
				},
				Config:      testAccFilesystemStorageConfig("test-project", "shared-data", "ASH", 2000, `["nfs4"]`),
				ExpectError: regexp.MustCompile(`(?s)Unable to resize filesystem storage.*STORAGE_OPERATION_UNSUPPORTED`),
			},
			{
				// The failed resize must not have been written to state.
				PreConfig: func() {
					mock.mu.Lock()
					defer mock.mu.Unlock()
					mock.rejectResize = false
				},
				Config: testAccFilesystemStorageConfig("test-project", "shared-data", "ASH", 1500, `["nfs4"]`),
				Check:  resource.TestCheckResourceAttr(name, "size_in_gb", "1500"),
			},
		},
	})
}

func TestFilesystemStorageNotFound(t *testing.T) {
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
			if got := filesystemStorageNotFound(tc.err); got != tc.want {
				t.Errorf("filesystemStorageNotFound(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// mapFilesystemStorage must null purely-computed fields on absence, only
// backfill project when state has nothing (import), never clobbering a
// configured slug with the API's canonical form, and only overwrite Required
// fields (name, region, protocols) when the API actually returns a value.
func TestMapFilesystemStorage(t *testing.T) {
	ctx := context.Background()

	t.Run("nil attributes leave id set and everything else untouched", func(t *testing.T) {
		var data FilesystemStorageResourceModel
		id := "fs_1"
		diags := mapFilesystemStorage(ctx, &data, &components.FilesystemData{ID: &id})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if data.ID.ValueString() != "fs_1" {
			t.Fatalf("ID = %v, want fs_1", data.ID)
		}
		if !data.Name.IsNull() {
			t.Fatalf("Name = %v, want null (no attributes)", data.Name)
		}
	})

	t.Run("purely computed fields null on absence", func(t *testing.T) {
		data := FilesystemStorageResourceModel{
			StorageClass: types.StringValue("stale"),
			NfsMountPath: types.StringValue("stale"),
			FileEndpoint: types.StringValue("stale"),
		}
		id := "fs_1"
		name := "shared-data"
		size := int64(1500)
		diags := mapFilesystemStorage(ctx, &data, &components.FilesystemData{
			ID: &id,
			Attributes: &components.FilesystemDataAttributes{
				Name:     &name,
				SizeInGb: &size,
			},
		})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if !data.StorageClass.IsNull() {
			t.Errorf("StorageClass = %v, want null", data.StorageClass)
		}
		if !data.NfsMountPath.IsNull() {
			t.Errorf("NfsMountPath = %v, want null", data.NfsMountPath)
		}
		if !data.FileEndpoint.IsNull() {
			t.Errorf("FileEndpoint = %v, want null", data.FileEndpoint)
		}
	})

	t.Run("project only backfilled when unset", func(t *testing.T) {
		configuredProject := types.StringValue("my-slug")
		data := FilesystemStorageResourceModel{Project: configuredProject}

		apiProjectID := "proj_mock_1"
		apiSlug := "canonical-slug"
		diags := mapFilesystemStorage(ctx, &data, &components.FilesystemData{
			Attributes: &components.FilesystemDataAttributes{
				Project: &components.ProjectInclude{ID: &apiProjectID, Slug: &apiSlug},
			},
		})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if data.Project.ValueString() != "my-slug" {
			t.Errorf("Project = %v, want unchanged 'my-slug'", data.Project)
		}

		// Now with nothing configured (import path): backfills from the API.
		var imported FilesystemStorageResourceModel
		diags = mapFilesystemStorage(ctx, &imported, &components.FilesystemData{
			Attributes: &components.FilesystemDataAttributes{
				Project: &components.ProjectInclude{ID: &apiProjectID, Slug: &apiSlug},
			},
		})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if imported.Project.ValueString() != apiSlug {
			t.Errorf("imported Project = %v, want %v (slug preferred over id)", imported.Project, apiSlug)
		}
	})

	t.Run("protocols round-trip into a set", func(t *testing.T) {
		var data FilesystemStorageResourceModel
		diags := mapFilesystemStorage(ctx, &data, &components.FilesystemData{
			Attributes: &components.FilesystemDataAttributes{
				Protocols: []components.Protocols{components.ProtocolsNfs3, components.ProtocolsNfs4},
			},
		})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		var values []string
		diags = data.Protocols.ElementsAs(ctx, &values, false)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if len(values) != 2 {
			t.Fatalf("protocols = %v, want 2 elements", values)
		}
	})
}

// TestAccFilesystemStorage_Basic exercises create, read and import against the
// live API. The site (testFilesystemStorageSite) is not documented anywhere on
// the FilesystemStorage group itself; ASH is reused from
// latitudesh_block_storage / latitudesh_object_storage, whose `region`
// attribute carries the identical "site slug" doc comment - a human should
// confirm ASH actually has filesystem storage capacity, and that the test's
// team has filesystem storage access enabled (gated per team per
// sdk-coverage.yaml), before relying on this test.
const testFilesystemStorageSite = "ASH"

func TestAccFilesystemStorage_Basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	resourceName := "latitudesh_filesystem_storage.test_item"
	projectSlug := testAccProjectSlug(t)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckFilesystemStorageDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccConfigFilesystemStorage(projectSlug, "tf-acc-filesystem-storage-"+testRunID, testFilesystemStorageSite, 1500, `["nfs4"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "name", "tf-acc-filesystem-storage-"+testRunID),
					resource.TestCheckResourceAttr(resourceName, "region", testFilesystemStorageSite),
					resource.TestCheckResourceAttr(resourceName, "project", projectSlug),
					resource.TestCheckResourceAttr(resourceName, "size_in_gb", "1500"),
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

func testAccConfigFilesystemStorage(project, name, region string, sizeInGb int, protocols string) string {
	return fmt.Sprintf(`
resource "latitudesh_filesystem_storage" "test_item" {
  project    = %q
  name       = %q
  region     = %q
  size_in_gb = %d
  protocols  = %s
}
`, project, name, region, sizeInGb, protocols)
}

func testAccCheckFilesystemStorageDestroy(s *terraform.State) error {
	ctx := context.Background()
	client, err := newSDKClientFromEnv()
	if err != nil {
		return err
	}

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_filesystem_storage" {
			continue
		}
		id := rs.Primary.ID
		if id == "" {
			continue
		}

		resp, err := client.FilesystemStorage.ListFilesystems(ctx, nil)
		if err != nil {
			if filesystemStorageNotFound(err) {
				continue
			}
			return fmt.Errorf("checking filesystem storage %s destroyed: %w", id, err)
		}
		if resp != nil && resp.Filesystems != nil {
			for _, fs := range resp.Filesystems.Data {
				if fs.ID != nil && *fs.ID == id {
					return fmt.Errorf("filesystem storage still exists: %s", id)
				}
			}
		}
	}
	return nil
}
