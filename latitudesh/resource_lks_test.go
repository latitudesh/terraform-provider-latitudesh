package latitudesh

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// --- offline mapping unit tests ---------------------------------------------

func TestMapLksAttributes_Nil(t *testing.T) {
	fields, diags := mapLksAttributes(nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags.Errors())
	}
	if !fields.Name.IsNull() || !fields.Status.IsNull() || !fields.Network.IsNull() {
		t.Fatalf("expected every field null for nil attributes, got %+v", fields)
	}
}

func TestMapLksAttributes_Full(t *testing.T) {
	name := "prod"
	projectID := "proj_abc"
	site := "ASH"
	version := "1.31.0"
	desc := "prod cluster"
	status := "ready"

	attrs := &components.LksClusterDataAttributes{
		Name:              &name,
		ProjectID:         &projectID,
		Site:              &site,
		KubernetesVersion: &version,
		Description:       &desc,
		Status:            &status,
		Network: &components.Network{
			PodCidrs:     []string{"10.0.0.0/16"},
			ServiceCidrs: []string{"10.1.0.0/16"},
			NodeCidrs:    []string{"10.2.0.0/16"},
		},
	}

	fields, diags := mapLksAttributes(attrs)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags.Errors())
	}
	if fields.Name.ValueString() != name {
		t.Errorf("Name = %q, want %q", fields.Name.ValueString(), name)
	}
	if fields.Project.ValueString() != projectID {
		t.Errorf("Project = %q, want %q", fields.Project.ValueString(), projectID)
	}
	if fields.Network.IsNull() {
		t.Fatal("expected a non-null network object")
	}

	ctx := context.Background()
	var netModel LksNetworkModel
	if d := fields.Network.As(ctx, &netModel, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatalf("decoding network object: %v", d.Errors())
	}
	pods, d := setToStrings(ctx, netModel.PodCidrs)
	if d.HasError() {
		t.Fatalf("reading pod_cidrs: %v", d.Errors())
	}
	if len(pods) != 1 || pods[0] != "10.0.0.0/16" {
		t.Errorf("pod_cidrs = %v, want [10.0.0.0/16]", pods)
	}
}

// readLksInto keeps `description` honest. It is Optional and not Computed, so
// the API owns it: a description cleared or changed outside Terraform has to
// land in state as drift instead of being masked by the value Terraform last
// wrote. The only value that must NOT overwrite a null is "", which some
// endpoints use for "unset" — collapsing it there keeps a config that omits
// `description` from failing with "inconsistent result after apply".
func TestReadLksInto_Description(t *testing.T) {
	cases := []struct {
		name  string
		prior basetypes.StringValue
		// body is the attributes fragment the API answers with.
		body string
		want basetypes.StringValue
	}{
		{
			name:  "cleared upstream shows as drift",
			prior: types.StringValue("old"),
			body:  `"status":"ready"`,
			want:  types.StringNull(),
		},
		{
			name:  "emptied upstream shows as drift",
			prior: types.StringValue("old"),
			body:  `"status":"ready","description":""`,
			want:  types.StringValue(""),
		},
		{
			name:  "changed upstream shows as drift",
			prior: types.StringValue("old"),
			body:  `"status":"ready","description":"new"`,
			want:  types.StringValue("new"),
		},
		{
			name:  "unset stays null when the API omits it",
			prior: types.StringNull(),
			body:  `"status":"ready"`,
			want:  types.StringNull(),
		},
		{
			name:  "unset stays null when the API answers empty string",
			prior: types.StringNull(),
			body:  `"status":"ready","description":""`,
			want:  types.StringNull(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := lksResourceServing(t, tc.body)

			data := LksResourceModel{
				ID:          types.StringValue("lks_read_1"),
				Description: tc.prior,
			}
			var diags diag.Diagnostics
			r.readLksInto(context.Background(), &data, &diags)

			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags.Errors())
			}
			if !data.Description.Equal(tc.want) {
				t.Fatalf("description = %s, want %s", data.Description, tc.want)
			}
		})
	}
}

// lksResourceServing returns an LksResource whose client answers every cluster
// GET with the given attributes fragment.
func lksResourceServing(t *testing.T, attributes string) *LksResource {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"data":{"id":"lks_read_1","type":"lks_clusters","attributes":{%s}}}`, attributes)
	}))
	t.Cleanup(server.Close)

	return &LksResource{
		client: latitudeshgosdk.New(
			latitudeshgosdk.WithSecurity("test"),
			latitudeshgosdk.WithServerURL(server.URL),
		),
	}
}

func TestMapLksNetwork_Nil(t *testing.T) {
	obj, diags := mapLksNetwork(nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags.Errors())
	}
	if !obj.IsNull() {
		t.Fatal("expected a null object for nil network")
	}
}

func TestLksClusterNotFound(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"api error 404", &components.APIError{StatusCode: http.StatusNotFound}, true},
		{"api error 403", &components.APIError{StatusCode: http.StatusForbidden}, false},
		{"error object 404", &components.ErrorObject{Errors: []components.Errors{{Status: strPtr("404")}}}, true},
		{"error object 403", &components.ErrorObject{Errors: []components.Errors{{Status: strPtr("403")}}}, false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lksClusterNotFound(tc.err); got != tc.want {
				t.Errorf("lksClusterNotFound(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestLksRetryableDuringPoll(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"404 not found", &components.APIError{StatusCode: http.StatusNotFound}, true},
		{"502 bad gateway", &components.APIError{StatusCode: http.StatusBadGateway}, true},
		{"422 unprocessable", &components.APIError{StatusCode: http.StatusUnprocessableEntity}, false},
		{"403 forbidden", &components.APIError{StatusCode: http.StatusForbidden}, false},
		{"error object 502", &components.ErrorObject{Errors: []components.Errors{{Status: strPtr("502")}}}, true},
		{"error object 409", &components.ErrorObject{Errors: []components.Errors{{Status: strPtr("409")}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lksRetryableDuringPoll(tc.err); got != tc.want {
				t.Errorf("lksRetryableDuringPoll(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// --- mock-backed resource.Test (IsUnitTest) ---------------------------------

// mockLksAPI is a minimal in-memory LKS cluster store backing a full
// create/read/update/delete cycle. The mock reaches "ready" immediately on
// create so the resource's ready-poller returns on its very first GET,
// keeping the test fast without touching the package-level poll intervals.
type mockLksAPI struct {
	mu          sync.Mutex
	exists      bool
	deleted     bool
	id          string
	name        string
	description string
	site        string
	version     string
	projectID   string
}

func (m *mockLksAPI) envelope() map[string]any {
	attrs := map[string]any{
		"name":               m.name,
		"project_id":         m.projectID,
		"site":               m.site,
		"kubernetes_version": m.version,
		"status":             "ready",
		"network": map[string]any{
			"pod_cidrs":     []string{"10.0.0.0/16"},
			"service_cidrs": []string{"10.1.0.0/16"},
			"node_cidrs":    []string{"10.2.0.0/16"},
		},
	}
	// Mirrors the real API: a description that was never set is omitted
	// entirely, not echoed back as an empty string.
	if m.description != "" {
		attrs["description"] = m.description
	}
	return map[string]any{
		"data": map[string]any{
			"id":         m.id,
			"type":       "lks_clusters",
			"attributes": attrs,
		},
	}
}

func (m *mockLksAPI) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.Header().Set("Content-Type", "application/vnd.api+json")

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/lks/clusters":
		var payload struct {
			Data struct {
				Attributes struct {
					Name              string  `json:"name"`
					ProjectID         string  `json:"project_id"`
					Site              string  `json:"site"`
					KubernetesVersion string  `json:"kubernetes_version"`
					Description       *string `json:"description"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		m.id = "lks_mock_1"
		m.deleted = false
		m.name = payload.Data.Attributes.Name
		m.projectID = payload.Data.Attributes.ProjectID
		m.site = payload.Data.Attributes.Site
		m.version = payload.Data.Attributes.KubernetesVersion
		if payload.Data.Attributes.Description != nil {
			m.description = *payload.Data.Attributes.Description
		}
		m.exists = true
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(m.envelope())

	case r.Method == http.MethodGet && r.URL.Path == "/lks/clusters/lks_mock_1":
		if !m.exists || m.deleted {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.envelope())

	case r.Method == http.MethodPatch && r.URL.Path == "/lks/clusters/lks_mock_1":
		var payload struct {
			Data struct {
				Attributes struct {
					Name              *string `json:"name"`
					Description       *string `json:"description"`
					KubernetesVersion *string `json:"kubernetes_version"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if payload.Data.Attributes.Name != nil {
			m.name = *payload.Data.Attributes.Name
		}
		if payload.Data.Attributes.Description != nil {
			m.description = *payload.Data.Attributes.Description
		}
		if payload.Data.Attributes.KubernetesVersion != nil {
			m.version = *payload.Data.Attributes.KubernetesVersion
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.envelope())

	case r.Method == http.MethodDelete && r.URL.Path == "/lks/clusters/lks_mock_1":
		m.deleted = true
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

func testAccCheckMockLksDestroyed(m *mockLksAPI) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.exists && !m.deleted {
			return fmt.Errorf("mock LKS cluster still exists after destroy")
		}
		return nil
	}
}

func testAccLksConfig(version string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_lks" "test_item" {
  project             = "proj_mock_1"
  name                = "tf-test-cluster"
  site                = "ASH"
  kubernetes_version  = %q
}
`, version)
}

func TestLks_CreateUpdateImport(t *testing.T) {
	mock := &mockLksAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockLksDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccLksConfig("1.31.0"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "name", "tf-test-cluster"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "site", "ASH"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "kubernetes_version", "1.31.0"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "status", "ready"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "project", "proj_mock_1"),
				),
			},
			{
				// In-place upgrade: kubernetes_version has no RequiresReplace, so
				// this must PATCH rather than replace.
				Config: testAccLksConfig("1.32.0"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "kubernetes_version", "1.32.0"),
				),
			},
			{
				ResourceName:      "latitudesh_lks.test_item",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
