package latitudesh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
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
	str := func(v string) *string { return &v }

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"generic 404", &components.APIError{StatusCode: 404}, true},
		{"generic 403", &components.APIError{StatusCode: 403}, false},
		{"generic 500", &components.APIError{StatusCode: 500}, false},

		// The numeric spelling JSON:API prescribes, and what the mocks send.
		{"jsonapi 404", &components.ErrorObject{Errors: []components.Errors{{Status: str("404")}}}, true},

		// What the live API actually sent on a destroy, verbatim (2026-09-18).
		// Matching only "404" made destroy fail on a cluster it had just
		// successfully deleted.
		{"jsonapi not_found", &components.ErrorObject{Errors: []components.Errors{{
			Code: str("NOT_FOUND"), Status: str("not_found"),
			Title: str("Cluster Not Found"), Detail: str("Cluster 'lksc_87c0ff4d8d024f' not found"),
		}}}, true},

		// The code alone is enough, in case status is omitted.
		{"code only", &components.ErrorObject{Errors: []components.Errors{{Code: str("NOT_FOUND")}}}, true},
		{"mixed case", &components.ErrorObject{Errors: []components.Errors{{Status: str("Not_Found")}}}, true},

		// Anything else is a real error and must not be swallowed as "gone" —
		// especially NOT_READY, which is a 409 the kubeconfig endpoint returns
		// and which looks superficially similar.
		{"jsonapi 403", &components.ErrorObject{Errors: []components.Errors{{Status: str("403")}}}, false},
		{"jsonapi not_ready", &components.ErrorObject{Errors: []components.Errors{{
			Code: str("NOT_READY"), Status: str("conflict"),
		}}}, false},
		{"jsonapi 422", &components.ErrorObject{Errors: []components.Errors{{Status: str("unprocessable_entity")}}}, false},
		{"empty", &components.ErrorObject{}, false},
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

	// status the GET reports; "" means "ready".
	status string

	// readyNeedsNodePool mirrors the platform: the cluster stays
	// "provisioning" until a node pool exists.
	readyNeedsNodePool bool

	poolID      string
	poolPlan    string
	poolCount   int64
	poolVersion string

	// Raw PATCH bodies, so a test can assert on what actually went over the
	// wire rather than on what the mock chose to do with it.
	poolPatchBodies []string

	poolTaints []map[string]any
	poolLabels map[string]string

	// The real API declares optional(:description).filled(:string), so an
	// empty string is a 422 rather than a clear.
	rejectEmptyDescription bool
	clusterPatchBodies     []string
}

// The envelope starts from the body the live API answered with (see
// lks_live_payloads_test.go) and overwrites only what this mock's own state
// owns, so every field the platform reports and the provider maps —
// message, reason, control_plane_endpoint, kubeconfig_url, platform_version,
// the timestamps — is present here too. Building the envelope by hand is what
// let those seven go unasserted.
func (m *mockLksAPI) envelope() map[string]any {
	status := m.status
	if status == "" {
		status = "ready"
	}
	if m.readyNeedsNodePool && m.poolID == "" {
		status = "provisioning"
	}
	attrs := lksLiveAttrsMap(lksLiveClusterBody)
	attrs["name"] = m.name
	attrs["project_id"] = m.projectID
	attrs["site"] = m.site
	attrs["kubernetes_version"] = m.version
	attrs["status"] = status
	attrs["network"] = map[string]any{
		"pod_cidrs":     []string{"10.0.0.0/16"},
		"service_cidrs": []string{"10.1.0.0/16"},
		"node_cidrs":    []string{"10.2.0.0/16"},
	}
	// Mirrors the real API: a description that was never set is omitted
	// entirely, not echoed back as an empty string.
	if m.description != "" {
		attrs["description"] = m.description
	} else {
		delete(attrs, "description")
	}
	return map[string]any{
		"data": map[string]any{
			"id":         m.id,
			"type":       "lks_clusters",
			"attributes": attrs,
		},
	}
}

// Nodes come up immediately here: the cluster's readiness is what this mock
// exists to gate, and lksWaitForNodesReady has its own coverage in
// resource_lks_node_pool_test.go. What the pool is "ready" WITH comes from the
// live capture, including the null ready_nodes the platform actually reports
// on a pool whose node is up — the previous hand-built envelope echoed the
// node count there, which is the one value the API never sent.
func (m *mockLksAPI) poolEnvelope() map[string]any {
	attrs := lksLiveAttrsMap(lksLiveNodePoolBody)
	attrs["plan"] = m.poolPlan
	attrs["count"] = m.poolCount
	attrs["status"] = "ready"
	// The pool carries its own version: upgrading the control plane does not
	// move it, only a PATCH on the pool does.
	attrs["kubernetes_version"] = m.poolVersion

	delete(attrs, "labels")
	delete(attrs, "taints")
	if len(m.poolTaints) > 0 {
		attrs["taints"] = m.poolTaints
	}
	if len(m.poolLabels) > 0 {
		attrs["labels"] = m.poolLabels
	}
	return map[string]any{
		"data": map[string]any{
			"id":         m.poolID,
			"type":       "lks_node_pools",
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
		clusterBody, _ := io.ReadAll(r.Body)
		m.clusterPatchBodies = append(m.clusterPatchBodies, string(clusterBody))
		r.Body = io.NopCloser(bytes.NewReader(clusterBody))

		if m.rejectEmptyDescription && strings.Contains(strings.ReplaceAll(string(clusterBody), " ", ""), `"description":""`) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"errors":[{"code":"VALIDATION_ERROR","status":"unprocessable_entity","detail":"must be filled","source":{"pointer":"description"}}]}`))
			return
		}

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

	case r.Method == http.MethodPost && r.URL.Path == "/lks/clusters/lks_mock_1/nodepools":
		var payload struct {
			Data struct {
				Attributes struct {
					Plan              string            `json:"plan"`
					Count             int64             `json:"count"`
					KubernetesVersion *string           `json:"kubernetes_version"`
					Taints            []map[string]any  `json:"taints"`
					Labels            map[string]string `json:"labels"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		m.poolID = "np_mock_1"
		m.poolPlan = payload.Data.Attributes.Plan
		m.poolCount = payload.Data.Attributes.Count
		m.poolVersion = m.version
		if v := payload.Data.Attributes.KubernetesVersion; v != nil && *v != "" {
			m.poolVersion = *v
		}
		m.poolTaints = payload.Data.Attributes.Taints
		m.poolLabels = payload.Data.Attributes.Labels
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(m.poolEnvelope())

	case r.Method == http.MethodGet && r.URL.Path == "/lks/clusters/lks_mock_1/nodepools":
		pools := []any{}
		if m.poolID != "" {
			pools = append(pools, m.poolEnvelope()["data"])
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": pools})

	case r.Method == http.MethodPatch && r.URL.Path == "/lks/clusters/lks_mock_1/nodepools/np_mock_1":
		body, _ := io.ReadAll(r.Body)
		m.poolPatchBodies = append(m.poolPatchBodies, string(body))

		// Faithful to a JSON:API PATCH: a field the request omits is left
		// alone. Modelling that is the whole point — a mock that cleared on
		// absence would hide the case where the provider cannot express a
		// clear at all.
		var present map[string]json.RawMessage
		var envelope struct {
			Data struct {
				Attributes map[string]json.RawMessage `json:"attributes"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		present = envelope.Data.Attributes

		if raw, ok := present["count"]; ok {
			var v int64
			if json.Unmarshal(raw, &v) == nil {
				m.poolCount = v
			}
		}
		if raw, ok := present["kubernetes_version"]; ok {
			var v string
			if json.Unmarshal(raw, &v) == nil && v != "" {
				m.poolVersion = v
			}
		}
		if raw, ok := present["taints"]; ok {
			var v []map[string]any
			if json.Unmarshal(raw, &v) == nil {
				m.poolTaints = v
			}
		}
		if raw, ok := present["labels"]; ok {
			var v map[string]string
			if json.Unmarshal(raw, &v) == nil {
				m.poolLabels = v
			}
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.poolEnvelope())

	case r.Method == http.MethodDelete && r.URL.Path == "/lks/clusters/lks_mock_1/nodepools/np_mock_1":
		m.poolID = ""
		w.WriteHeader(http.StatusNoContent)

	case r.Method == http.MethodGet && r.URL.Path == "/lks/clusters/lks_mock_1/nodepools/np_mock_1":
		if m.poolID == "" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.poolEnvelope())

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

  default_node_pool = {
    plan       = "c2-medium-x86"
    node_count = 1
  }
}
`, version)
}

func TestLks_CreateUpdateImport(t *testing.T) {
	mock := &mockLksAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	prevReady, prevPool := lksReadyPollInterval, lksNodePoolReadyPollInterval
	lksReadyPollInterval = 5 * time.Millisecond
	lksNodePoolReadyPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		lksReadyPollInterval = prevReady
		lksNodePoolReadyPollInterval = prevPool
	})

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

					// Every computed attribute the API reports has to reach
					// state. Checking only the ones the config also sets is
					// how a mapping regression on the rest went unnoticed:
					// the values below come straight from the captured live
					// envelope the mock now answers with.
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "message", "the cluster is ready"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "reason", ""),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "control_plane_endpoint", "https://lksc-8d12b878420d45.lks.lsh.io:6443"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "kubeconfig_url", "/lks/clusters/lksc_8d12b878420d45/kubeconfig"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "platform_version", "lks-v1.36.1-007"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "created_at", "2026-09-18T15:39:51.019044Z"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "updated_at", "2026-09-18T15:46:15.110843Z"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "network.pod_cidrs.0", "10.0.0.0/16"),

					// Same for the pool folded into the cluster.
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "default_node_pool.plan", "c2-medium-x86"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "default_node_pool.node_count", "1"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "default_node_pool.kubernetes_version", "1.31.0"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "default_node_pool.name", "np-b55eea9b6036"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "default_node_pool.type", "bare_metal"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "default_node_pool.mode", "on_demand"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "default_node_pool.status", "ready"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "default_node_pool.message", "the node pool is ready"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "default_node_pool.platform_version", "lks-v1.36.1-007"),
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "default_node_pool.created_at", "2026-09-18T15:39:52.045214Z"),

					// Null in the live payload, and null is not zero: the
					// platform reports neither on a pool that is up.
					resource.TestCheckNoResourceAttr("latitudesh_lks.test_item", "default_node_pool.ready_nodes"),
					resource.TestCheckNoResourceAttr("latitudesh_lks.test_item", "default_node_pool.max_pods_per_node"),
				),
			},
			{
				// In-place upgrade: kubernetes_version has no RequiresReplace, so
				// this must PATCH rather than replace.
				Config: testAccLksConfig("1.32.0"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "kubernetes_version", "1.32.0"),
					// The config never sets default_node_pool.kubernetes_version,
					// so it follows the cluster: bumping the control plane bumps
					// the pool too, in the same apply.
					resource.TestCheckResourceAttr("latitudesh_lks.test_item", "default_node_pool.kubernetes_version", "1.32.0"),
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

// The deadlock this design avoids: a cluster only converges once it has a node
// pool, and POST /lks/clusters takes none inline. Expressed as a separate
// resource the pool could only be created AFTER the cluster's Create returned,
// so Create could not wait for "ready" without waiting on itself. Folding the
// first pool into the cluster is what makes the wait legitimate — and this
// test is the proof: the mock only flips the cluster to "ready" once a pool
// has been POSTed, so an implementation that skipped creating it, or waited
// before creating it, times out here.
func TestLks_CreateWaitsForReadyViaDefaultNodePool(t *testing.T) {
	mock := &mockLksAPI{readyNeedsNodePool: true}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	prevReady := lksReadyPollInterval
	prevPool := lksNodePoolReadyPollInterval
	lksReadyPollInterval = 5 * time.Millisecond
	lksNodePoolReadyPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		lksReadyPollInterval = prevReady
		lksNodePoolReadyPollInterval = prevPool
	})

	const rn = "latitudesh_lks.test_item"

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockLksDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccLksConfigWithTimeout("1.31.0", `
  default_node_pool = {
    plan       = "c2-medium-x86"
    node_count = 2
  }

  timeouts = {
    create = "20s"
  }
`),
				Check: resource.ComposeTestCheckFunc(
					// Reached only because the pool was created first.
					resource.TestCheckResourceAttr(rn, "status", "ready"),
					resource.TestCheckResourceAttrSet(rn, "default_node_pool.id"),
					resource.TestCheckResourceAttr(rn, "default_node_pool.node_count", "2"),
					resource.TestCheckResourceAttr(rn, "default_node_pool.status", "ready"),
					// The wait settles on the pool's own status: the live API
					// leaves ready_nodes null even with the nodes up, so state
					// holds no count to compare against.
					resource.TestCheckNoResourceAttr(rn, "default_node_pool.ready_nodes"),
				),
			},
		},
	})
}

func testAccLksConfigWithTimeout(version, extra string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_lks" "test_item" {
  project             = "proj_mock_1"
  name                = "tf-test-cluster"
  site                = "ASH"
  kubernetes_version  = %q
%s
}
`, version, extra)
}

// Removing a taint is an in-place edit — taints carries no RequiresReplace
// anywhere — and it has to actually take effect. The mock is faithful to a
// JSON:API PATCH: a field the request omits is left alone, which is what made
// this fail before lks_clear_transport.go existed. The payload assertion is
// the load-bearing one: without `"taints": []` on the wire the apply "succeeds"
// while the taint stays attached.
func TestLks_UpdateClearingTaintsIsInPlace(t *testing.T) {
	mock := &mockLksAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	prevReady, prevPool := lksReadyPollInterval, lksNodePoolReadyPollInterval
	lksReadyPollInterval = 5 * time.Millisecond
	lksNodePoolReadyPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		lksReadyPollInterval = prevReady
		lksNodePoolReadyPollInterval = prevPool
	})

	const rn = "latitudesh_lks.test_item"
	var firstClusterID, firstPoolID string

	withTaint := `
  default_node_pool = {
    plan       = "c2-medium-x86"
    node_count = 1

    labels = {
      workload = "general"
    }

    taints = [{
      key    = "dedicated"
      value  = "manual-test"
      effect = "PreferNoSchedule"
    }]
  }
`
	withoutTaint := `
  default_node_pool = {
    plan       = "c2-medium-x86"
    node_count = 1
  }
`

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockLksDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccLksConfigWithTimeout("1.31.0", withTaint),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(rn, "default_node_pool.taints.#", "1"),
					resource.TestCheckResourceAttr(rn, "default_node_pool.labels.workload", "general"),
					func(s *terraform.State) error {
						rs := s.RootModule().Resources[rn]
						firstClusterID = rs.Primary.ID
						firstPoolID = rs.Primary.Attributes["default_node_pool.id"]
						return nil
					},
				),
			},
			{
				Config: testAccLksConfigWithTimeout("1.31.0", withoutTaint),
				Check: resource.ComposeTestCheckFunc(
					func(s *terraform.State) error {
						rs := s.RootModule().Resources[rn]
						if rs.Primary.ID != firstClusterID {
							return fmt.Errorf("cluster was replaced: %s -> %s", firstClusterID, rs.Primary.ID)
						}
						if got := rs.Primary.Attributes["default_node_pool.id"]; got != firstPoolID {
							return fmt.Errorf("node pool was rebuilt for a taint removal: %s -> %s", firstPoolID, got)
						}
						return nil
					},
					resource.TestCheckNoResourceAttr(rn, "default_node_pool.taints.0.key"),
					resource.TestCheckNoResourceAttr(rn, "default_node_pool.labels.workload"),
				),
			},
		},
	})

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.poolPatchBodies) == 0 {
		t.Fatal("no PATCH reached the node pool, so nothing was updated in place")
	}
	last := mock.poolPatchBodies[len(mock.poolPatchBodies)-1]
	for _, want := range []string{`"taints":[]`, `"labels":{}`} {
		if !strings.Contains(strings.ReplaceAll(last, " ", ""), want) {
			t.Errorf("clearing PATCH did not carry %s, so the API would keep the old value: %s", want, last)
		}
	}
}

// The LKS API cannot clear a description, and the provider must not pretend
// otherwise. Both update contracts declare `optional(:description).filled(:string)`,
// so `""` is a 422 ("must be filled"), and an explicit null is dropped by the
// controller before it reaches the writer. The mock enforces the 422 so this
// stays honest: an implementation that sends "" on every update where the
// attribute is null — which `omitempty` on a *string makes tempting, since the
// pointer does reach the wire — would fail EVERY update, not just a clearing
// one.
func TestLks_DescriptionIsNeverSentEmpty(t *testing.T) {
	mock := &mockLksAPI{rejectEmptyDescription: true}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	prevReady, prevPool := lksReadyPollInterval, lksNodePoolReadyPollInterval
	lksReadyPollInterval = 5 * time.Millisecond
	lksNodePoolReadyPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		lksReadyPollInterval = prevReady
		lksNodePoolReadyPollInterval = prevPool
	})

	pool := `
  default_node_pool = {
    plan       = "c2-medium-x86"
    node_count = 1
  }
`

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockLksDestroyed(mock),
		Steps: []resource.TestStep{
			// Never had a description: the update below must not invent one.
			{Config: testAccLksConfigWithTimeout("1.31.0", pool)},
			{Config: testAccLksConfigWithTimeout("1.32.0", pool)},
		},
	})

	mock.mu.Lock()
	defer mock.mu.Unlock()
	for i, body := range mock.clusterPatchBodies {
		if strings.Contains(strings.ReplaceAll(body, " ", ""), `"description":""`) {
			t.Errorf("PATCH #%d sent an empty description, which the API rejects with 422: %s", i, body)
		}
	}
}

// A lowered kubernetes_version has to fail at plan, not an hour into the apply
// where the platform answers 422 DOWNGRADE_NOT_ALLOWED. Unknown or unparseable
// versions are the API's problem, not this check's — it must never invent a
// verdict from a string it cannot compare.
func TestLksCheckNoDowngrade(t *testing.T) {
	p := path.Root("kubernetes_version")
	cases := []struct {
		name      string
		prior     basetypes.StringValue
		planned   basetypes.StringValue
		wantError bool
	}{
		{"downgrade patch", types.StringValue("1.36.1"), types.StringValue("1.36.0"), true},
		{"downgrade minor", types.StringValue("1.36.1"), types.StringValue("1.35.9"), true},
		{"same version", types.StringValue("1.36.1"), types.StringValue("1.36.1"), false},
		{"upgrade patch", types.StringValue("1.36.1"), types.StringValue("1.36.2"), false},
		{"upgrade minor", types.StringValue("1.35.0"), types.StringValue("1.36.0"), false},
		{"prior null (create)", types.StringNull(), types.StringValue("1.36.0"), false},
		{"planned unknown", types.StringValue("1.36.1"), types.StringUnknown(), false},
		{"unparseable planned left to API", types.StringValue("1.36.1"), types.StringValue("garbage"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var diags diag.Diagnostics
			lksCheckNoDowngrade(tc.prior, tc.planned, p, &diags)
			if got := diags.HasError(); got != tc.wantError {
				t.Fatalf("HasError = %v, want %v (diags: %v)", got, tc.wantError, diags.Errors())
			}
		})
	}
}
