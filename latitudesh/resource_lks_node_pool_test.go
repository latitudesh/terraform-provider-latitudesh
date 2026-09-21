package latitudesh

import (
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

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// --- offline mapping and rule unit tests ------------------------------------

func TestMapLksNodePoolAttributes_Nil(t *testing.T) {
	fields, diags := mapLksNodePoolAttributes(context.Background(), nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags.Errors())
	}
	if !fields.Plan.IsNull() || !fields.NodeCount.IsNull() || !fields.Labels.IsNull() || !fields.Taints.IsNull() {
		t.Fatalf("expected every field null for nil attributes, got %+v", fields)
	}
}

func TestMapLksNodePoolAttributes_Full(t *testing.T) {
	plan := "c2-medium-x86"
	count := int64(3)
	ready := int64(2)
	status := "provisioning"
	value := "gpu"

	fields, diags := mapLksNodePoolAttributes(context.Background(), &components.LksNodePoolDataAttributes{
		Plan:       &plan,
		Count:      &count,
		ReadyNodes: &ready,
		Status:     &status,
		Labels:     map[string]string{"team": "platform"},
		Taints: []components.LksNodePoolTaint{
			{Key: "dedicated", Value: &value, Effect: components.EffectNoSchedule},
		},
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags.Errors())
	}

	if fields.Plan.ValueString() != plan {
		t.Errorf("plan = %q, want %q", fields.Plan.ValueString(), plan)
	}
	if fields.NodeCount.ValueInt64() != count || fields.ReadyNodes.ValueInt64() != ready {
		t.Errorf("count/ready = %d/%d, want %d/%d", fields.NodeCount.ValueInt64(), fields.ReadyNodes.ValueInt64(), count, ready)
	}
	if got := len(fields.Labels.Elements()); got != 1 {
		t.Errorf("labels length = %d, want 1", got)
	}
	if got := len(fields.Taints.Elements()); got != 1 {
		t.Errorf("taints length = %d, want 1", got)
	}
}

// The hand-built case above picks its own values; this one is the payload the
// live API answered with (see lks_live_payloads_test.go), so every field the
// platform reports has to come through the mapper — including the two it
// reports as null on a pool that is fully up.
func TestMapLksNodePoolAttributes_LivePayload(t *testing.T) {
	fields, diags := mapLksNodePoolAttributes(context.Background(), liveLksNodePoolAttributes())
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags.Errors())
	}

	strings := map[string]struct{ got, want string }{
		"Plan":              {fields.Plan.ValueString(), "f4-metal-small"},
		"Type":              {fields.Type.ValueString(), "bare_metal"},
		"Mode":              {fields.Mode.ValueString(), "on_demand"},
		"Name":              {fields.Name.ValueString(), "np-b55eea9b6036"},
		"Description":       {fields.Description.ValueString(), ""},
		"KubernetesVersion": {fields.KubernetesVersion.ValueString(), "1.36.1"},
		"PlatformVersion":   {fields.PlatformVersion.ValueString(), "lks-v1.36.1-007"},
		"Status":            {fields.Status.ValueString(), "ready"},
		"Message":           {fields.Message.ValueString(), "the node pool is ready"},
		"Reason":            {fields.Reason.ValueString(), ""},
		"CreatedAt":         {fields.CreatedAt.ValueString(), "2026-09-18T15:39:52.045214Z"},
		"UpdatedAt":         {fields.UpdatedAt.ValueString(), "2026-09-18T15:42:14.740745Z"},
	}
	for field, v := range strings {
		if v.got != v.want {
			t.Errorf("%s = %q, want %q", field, v.got, v.want)
		}
	}

	if fields.NodeCount.ValueInt64() != 1 {
		t.Errorf("NodeCount = %d, want 1", fields.NodeCount.ValueInt64())
	}
	// Null, not zero: the pool below has its node up and the API still reports
	// neither value.
	if !fields.ReadyNodes.IsNull() {
		t.Errorf("ReadyNodes = %s, want null", fields.ReadyNodes)
	}
	if !fields.MaxPodsPerNode.IsNull() {
		t.Errorf("MaxPodsPerNode = %s, want null", fields.MaxPodsPerNode)
	}
	if got := len(fields.Labels.Elements()); got != 1 {
		t.Errorf("labels length = %d, want 1", got)
	}
	if got := len(fields.Taints.Elements()); got != 1 {
		t.Errorf("taints length = %d, want 1", got)
	}
}

// An empty labels map and an absent one mean the same thing to the API, so both
// have to land as null — otherwise a config that omits `labels` would see a
// permanent diff against an echoed empty map.
func TestMapLksNodePoolAttributes_EmptyCollectionsAreNull(t *testing.T) {
	fields, diags := mapLksNodePoolAttributes(context.Background(), &components.LksNodePoolDataAttributes{
		Labels: map[string]string{},
		Taints: []components.LksNodePoolTaint{},
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags.Errors())
	}
	if !fields.Labels.IsNull() {
		t.Errorf("labels = %s, want null", fields.Labels)
	}
	if !fields.Taints.IsNull() {
		t.Errorf("taints = %s, want null", fields.Taints)
	}
}

func TestLksReservedLabelKey(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		// A bare key has no prefix at all, so nothing is reserved.
		{"team", false},
		{"node-role", false},
		{"example.com/team", false},
		{"kubernetes.io/arch", true},
		{"k8s.io/whatever", true},
		{"cluster.x-k8s.io/role", true},
		{"lks.latitude.sh/pool", true},
		// Subdomains of a reserved prefix are reserved too.
		{"node.kubernetes.io/instance-type", true},
		{"sub.lks.latitude.sh/x", true},
		// ...but a domain that merely ends in the same letters is not.
		{"notkubernetes.io/x", false},
		{"mykubernetes.io/x", false},
	}

	for _, tc := range cases {
		if got := lksReservedLabelKey(tc.key); got != tc.want {
			t.Errorf("lksReservedLabelKey(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

// --- lifecycle against an in-memory mock ------------------------------------

// mockLksNodePoolAPI serves the cluster and node pool endpoints. Nodes do not
// come ready instantly: readyNodes trails count by `lag` polls, which is what
// makes the create/scale waits meaningful rather than no-ops.
type mockLksNodePoolAPI struct {
	mu sync.Mutex

	poolID  string
	plan    string
	count   int64
	ready   int64
	lag     int
	name    string
	descr   string
	version string
	// clusterVersion is what GET /lks/clusters/{id} reports; the pool's omitted
	// kubernetes_version follows it. A field, not a constant, so a test can bump
	// it between steps to stand in for a control-plane upgrade.
	clusterVersion string
	maxPods        *int64
	labels         map[string]string
	taints         []map[string]any
	deleted        bool
	exists         bool

	patchBodies []string
}

const (
	mockLksNodePoolClusterID = "lks_np_cluster"
	mockLksNodePoolID        = "lks_np_1"
)

func (m *mockLksNodePoolAPI) clusterVersionOrDefault() string {
	if m.clusterVersion == "" {
		return "1.31.0"
	}
	return m.clusterVersion
}

func (m *mockLksNodePoolAPI) envelope() map[string]any {
	attrs := map[string]any{
		"plan":               m.plan,
		"count":              m.count,
		"ready_nodes":        m.ready,
		"type":               "bare_metal",
		"mode":               "on_demand",
		"status":             "ready",
		"message":            "the node pool is ready",
		"reason":             "",
		"kubernetes_version": m.version,
		"platform_version":   "lks-v1.36.1-007",
		"created_at":         "2026-09-18T15:39:52.045214Z",
		"updated_at":         "2026-09-18T15:42:14.740745Z",
	}
	if m.name != "" {
		attrs["name"] = m.name
	}
	if m.descr != "" {
		attrs["description"] = m.descr
	}
	if m.maxPods != nil {
		attrs["max_pods_per_node"] = *m.maxPods
	}
	if len(m.labels) > 0 {
		attrs["labels"] = m.labels
	}
	if len(m.taints) > 0 {
		attrs["taints"] = m.taints
	}
	return map[string]any{
		"data": map[string]any{
			"id":         m.poolID,
			"type":       "lks_node_pools",
			"attributes": attrs,
		},
	}
}

// tick advances readyNodes toward count, so a create or scale takes `lag`
// polls to settle.
func (m *mockLksNodePoolAPI) tick() {
	if m.ready < m.count {
		m.lag--
		if m.lag <= 0 {
			m.ready = m.count
		}
	}
}

func (m *mockLksNodePoolAPI) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.Header().Set("Content-Type", "application/vnd.api+json")

	base := "/lks/clusters/" + mockLksNodePoolClusterID + "/nodepools"
	poolPath := base + "/" + mockLksNodePoolID

	switch {
	// The cluster the pool hangs off, so the config can reference a real one.
	case r.Method == http.MethodGet && r.URL.Path == "/lks/clusters/"+mockLksNodePoolClusterID:
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"id": mockLksNodePoolClusterID, "type": "lks_clusters",
			"attributes": map[string]any{"status": "ready", "kubernetes_version": m.clusterVersionOrDefault(), "site": "ASH", "project_id": "proj_mock_1", "name": "c"},
		}})

	case r.Method == http.MethodPost && r.URL.Path == base:
		var payload struct {
			Data struct {
				Attributes struct {
					Plan              string            `json:"plan"`
					Count             int64             `json:"count"`
					Name              *string           `json:"name"`
					Description       *string           `json:"description"`
					KubernetesVersion *string           `json:"kubernetes_version"`
					MaxPodsPerNode    *int64            `json:"max_pods_per_node"`
					Labels            map[string]string `json:"labels"`
					Taints            []map[string]any  `json:"taints"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		a := payload.Data.Attributes
		m.poolID = mockLksNodePoolID
		m.plan = a.Plan
		m.count = a.Count
		m.ready = 0
		m.lag = 2
		m.version = "1.31.0"
		if a.KubernetesVersion != nil {
			m.version = *a.KubernetesVersion
		}
		m.name = "generated-name"
		if a.Name != nil {
			m.name = *a.Name
		}
		if a.Description != nil {
			m.descr = *a.Description
		}
		m.maxPods = a.MaxPodsPerNode
		m.labels = a.Labels
		m.taints = a.Taints
		m.exists = true
		m.deleted = false
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(m.envelope())

	case r.Method == http.MethodGet && r.URL.Path == poolPath:
		if !m.exists || m.deleted {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		m.tick()
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.envelope())

	case r.Method == http.MethodPatch && r.URL.Path == poolPath:
		body, _ := io.ReadAll(r.Body)
		m.patchBodies = append(m.patchBodies, string(body))

		var payload struct {
			Data struct {
				Attributes struct {
					Count             *int64            `json:"count"`
					Name              *string           `json:"name"`
					Description       *string           `json:"description"`
					KubernetesVersion *string           `json:"kubernetes_version"`
					Labels            map[string]string `json:"labels"`
					Taints            []map[string]any  `json:"taints"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		a := payload.Data.Attributes
		if a.Count != nil && *a.Count != m.count {
			m.count = *a.Count
			m.lag = 2 // a scale does not settle on the first poll either
		}
		if a.Name != nil {
			m.name = *a.Name
		}
		if a.Description != nil {
			m.descr = *a.Description
		}
		if a.KubernetesVersion != nil {
			m.version = *a.KubernetesVersion
		}
		m.labels = a.Labels
		m.taints = a.Taints
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(m.envelope())

	case r.Method == http.MethodDelete && r.URL.Path == poolPath:
		m.deleted = true
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

func testAccCheckMockLksNodePoolDestroyed(m *mockLksNodePoolAPI) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.exists && !m.deleted {
			return fmt.Errorf("mock LKS node pool still exists after destroy")
		}
		return nil
	}
}

func testAccLksNodePoolConfig(count int64, extra string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_lks_node_pool" "test" {
  cluster_id = %q
  plan       = "c2-medium-x86"
  node_count = %d
%s
}
`, mockLksNodePoolClusterID, count, extra)
}

func TestLksNodePool_CreateScaleImport(t *testing.T) {
	mock := &mockLksNodePoolAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	prevReady, prevDelete := lksNodePoolReadyPollInterval, lksNodePoolDeletePollInterval
	lksNodePoolReadyPollInterval = 5 * time.Millisecond
	lksNodePoolDeletePollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		lksNodePoolReadyPollInterval = prevReady
		lksNodePoolDeletePollInterval = prevDelete
	})

	const rn = "latitudesh_lks_node_pool.test"

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockLksNodePoolDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccLksNodePoolConfig(2, `
  labels = {
    team = "platform"
  }

  taints = [{
    key    = "dedicated"
    value  = "gpu"
    effect = "NoSchedule"
  }]
`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(rn, "node_count", "2"),
					// Create must not return before the nodes exist.
					resource.TestCheckResourceAttr(rn, "ready_nodes", "2"),
					resource.TestCheckResourceAttr(rn, "plan", "c2-medium-x86"),
					resource.TestCheckResourceAttr(rn, "type", "bare_metal"),
					resource.TestCheckResourceAttr(rn, "name", "generated-name"),
					resource.TestCheckResourceAttr(rn, "labels.team", "platform"),
					resource.TestCheckResourceAttr(rn, "taints.#", "1"),
					// The read-only half of the response has to reach state
					// too — message and reason are what explain a pool that
					// is not coming up, and nothing asserted them before.
					resource.TestCheckResourceAttr(rn, "mode", "on_demand"),
					resource.TestCheckResourceAttr(rn, "status", "ready"),
					resource.TestCheckResourceAttr(rn, "message", "the node pool is ready"),
					resource.TestCheckResourceAttr(rn, "reason", ""),
					resource.TestCheckResourceAttr(rn, "platform_version", "lks-v1.36.1-007"),
					resource.TestCheckResourceAttr(rn, "created_at", "2026-09-18T15:39:52.045214Z"),
					resource.TestCheckResourceAttr(rn, "updated_at", "2026-09-18T15:42:14.740745Z"),
				),
			},
			{
				// Scale in place: count has no RequiresReplace, so this PATCHes.
				Config: testAccLksNodePoolConfig(4, ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(rn, "node_count", "4"),
					resource.TestCheckResourceAttr(rn, "ready_nodes", "4"),
					// Dropping labels/taints from the config must clear them.
					resource.TestCheckNoResourceAttr(rn, "labels.team"),
					resource.TestCheckResourceAttr(rn, "taints.#", "0"),
				),
			},
			{
				ResourceName: rn,
				ImportState:  true,
				// The API reads a pool by (cluster_id, id), so the bare ID is
				// not a valid import address.
				ImportStateId:     mockLksNodePoolClusterID + ":" + mockLksNodePoolID,
				ImportStateVerify: true,
			},
		},
	})

	// max_pods_per_node is create-only: the API rejects a PATCH carrying it, so
	// Update must never send it.
	mock.mu.Lock()
	defer mock.mu.Unlock()
	for i, body := range mock.patchBodies {
		if strings.Contains(body, "max_pods_per_node") {
			t.Errorf("PATCH #%d carried max_pods_per_node, which the API rejects with 422: %s", i, body)
		}
	}
}

// The SDK cannot express "clear this collection". Both labels and taints are
// tagged `omitempty` on UpdateLksNodePoolAttributes, and Go omits a nil AND an
// empty map/slice alike — so there is no value the provider can put in the
// struct that reaches the wire as `"taints": []`.
//
// The consequence was user-visible and hit live (2026-09-18): removing a taint
// from configuration planned as an in-place update, the PATCH silently carried
// no `taints` at all, the API kept the old ones, and the read-back failed the
// apply with "Provider produced inconsistent result after apply". The API
// itself accepts an empty collection as "remove all" — only the SDK could not
// send one.
//
// The provider works around it in lks_clear_transport.go, which rewrites the
// outgoing PATCH. This test pins the root cause rather than the symptom, so it
// stays true regardless — and fails the day the SDK marks these fields
// nullable, which is the real fix and the signal to delete the workaround.
func TestLksNodePoolUpdateCannotClearCollections(t *testing.T) {
	marshal := func(taints []components.LksNodePoolTaint, labels map[string]string) string {
		body, err := json.Marshal(components.UpdateLksNodePool{
			Data: components.UpdateLksNodePoolData{
				Type: components.UpdateLksNodePoolTypeLksNodePools,
				Attributes: &components.UpdateLksNodePoolAttributes{
					Taints: taints,
					Labels: labels,
				},
			},
		})
		if err != nil {
			t.Fatalf("marshalling the update payload: %s", err)
		}
		return string(body)
	}

	for _, tc := range []struct {
		name   string
		taints []components.LksNodePoolTaint
		labels map[string]string
	}{
		{"nil", nil, nil},
		{"empty", []components.LksNodePoolTaint{}, map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := marshal(tc.taints, tc.labels)
			if strings.Contains(body, "taints") || strings.Contains(body, "labels") {
				t.Fatalf("the SDK can now express an empty collection — delete the clearing workaround and this test: %s", body)
			}
		})
	}
}

// description looks clearable and is not, and the two halves of that are worth
// pinning separately because they point in opposite directions:
//
//   - The SDK CAN send it. `omitempty` on a *string drops only a nil pointer,
//     so a pointer to "" reaches the wire — unlike the map and slice fields,
//     which drop empty too and need lks_clear_transport.go.
//   - The API REJECTS it. Both V3::LKS update contracts declare
//     `optional(:description).filled(:string)`, so "" is a 422 ("must be
//     filled"), and an explicit null is dropped by the controller's `.compact`
//     before it reaches the writer — a silent no-op.
//
// Acting on the first half alone sends "" on every update where the attribute
// is null, which fails EVERY update rather than just a clearing one. So the
// provider sends nothing, and clearing an LKS description remains impossible
// until the contract is relaxed.
func TestLksDescriptionForUpdate(t *testing.T) {
	attrs := func(v types.String) string {
		body, err := json.Marshal(components.UpdateLksNodePool{
			Data: components.UpdateLksNodePoolData{
				Type:       components.UpdateLksNodePoolTypeLksNodePools,
				Attributes: &components.UpdateLksNodePoolAttributes{Description: lksDescriptionForUpdate(v)},
			},
		})
		if err != nil {
			t.Fatalf("marshalling: %s", err)
		}
		return string(body)
	}

	// Null or unknown must leave the key out entirely: the API reads an absent
	// key as "leave unchanged", and an empty string as an error.
	for _, v := range []types.String{types.StringNull(), types.StringUnknown()} {
		if got := attrs(v); strings.Contains(got, "description") {
			t.Errorf("description must not be sent when unset — the API 422s on \"\": %s", got)
		}
	}

	// A configured value goes through untouched.
	if got := attrs(types.StringValue("still here")); !strings.Contains(got, `"description":"still here"`) {
		t.Errorf("description = %s, want it preserved", got)
	}
}

// A scale and an upgrade may not share a PATCH: the API answers 422 when
// `count` and `kubernetes_version` arrive together. Nothing in the SDK or the
// swagger says so — the rule surfaced in the dashboard, which guards against
// it client-side ("Scale and upgrade must be separate operations").
//
// The obvious implementation sends both unconditionally, and because
// kubernetes_version is Optional+Computed with UseStateForUnknown the plan
// always carries a known value after create — so EVERY update would have
// failed, not just the combined one. This asserts on the wire bodies because
// that is the only place the bug is visible; the resource behaves identically
// either way against a mock that does not enforce the rule.
func TestLksNodePool_ScaleAndUpgradeNeverShareAPatch(t *testing.T) {
	mock := &mockLksNodePoolAPI{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	prevReady, prevDelete := lksNodePoolReadyPollInterval, lksNodePoolDeletePollInterval
	lksNodePoolReadyPollInterval = 5 * time.Millisecond
	lksNodePoolDeletePollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		lksNodePoolReadyPollInterval = prevReady
		lksNodePoolDeletePollInterval = prevDelete
	})

	config := func(count int64, version string) string {
		return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_lks_node_pool" "test" {
  cluster_id         = %q
  plan               = "c2-medium-x86"
  node_count         = %d
  kubernetes_version = %q
}
`, mockLksNodePoolClusterID, count, version)
	}

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockLksNodePoolDestroyed(mock),
		Steps: []resource.TestStep{
			{Config: config(1, "1.31.0")},
			// Both at once — the case the API rejects outright.
			{Config: config(3, "1.32.0")},
			// Scale alone, then upgrade alone: neither may smuggle the other in.
			{Config: config(5, "1.32.0")},
			{Config: config(5, "1.33.0")},
		},
	})

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.patchBodies) < 3 {
		t.Fatalf("expected several PATCHes across the steps, got %d", len(mock.patchBodies))
	}
	for i, body := range mock.patchBodies {
		hasCount := strings.Contains(body, `"count"`)
		hasVersion := strings.Contains(body, `"kubernetes_version"`)
		if hasCount && hasVersion {
			t.Errorf("PATCH #%d carries both count and kubernetes_version, which the API rejects with 422: %s", i, body)
		}
	}
}

// ready_nodes is a passthrough on the API side with no default, so it can be
// absent from the response entirely. Gating the wait only on that number means
// a pool the platform has finished building polls until its timeout because
// the count never arrived — an apply stuck for an hour on a healthy cluster.
// The settled status is the authority; the count refines it when reported.
func TestLksWaitForNodesReady_SettlesWithoutReadyNodes(t *testing.T) {
	var polls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		polls++
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		// No ready_nodes key at all, which is what NetBox can answer.
		_, _ = fmt.Fprint(w, `{"data":{"id":"np_1","type":"lks_node_pools","attributes":{"status":"ready","count":3}}}`)
	}))
	defer server.Close()

	prev := lksNodePoolReadyPollInterval
	lksNodePoolReadyPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { lksNodePoolReadyPollInterval = prev })

	client := latitudeshgosdk.New(
		latitudeshgosdk.WithSecurity("test"),
		latitudeshgosdk.WithServerURL(server.URL),
	)

	var diags diag.Diagnostics
	lksWaitForNodesReady(context.Background(), client, "lksc_1", "np_1", 3, time.Now().Add(time.Second), &diags)

	if diags.HasError() {
		t.Fatalf("a settled pool must end the wait even without ready_nodes, got: %v", diags.Errors())
	}
	if polls != 1 {
		t.Errorf("expected exactly 1 poll, got %d", polls)
	}
}

// When the count IS reported it still has to be satisfied: a settled status
// with fewer ready nodes than asked for is not done.
func TestLksWaitForNodesReady_ReadyNodesStillGates(t *testing.T) {
	var polls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		polls++
		ready := 1
		if polls > 2 {
			ready = 3
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"data":{"id":"np_1","type":"lks_node_pools","attributes":{"status":"ready","count":3,"ready_nodes":%d}}}`, ready)
	}))
	defer server.Close()

	prev := lksNodePoolReadyPollInterval
	lksNodePoolReadyPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { lksNodePoolReadyPollInterval = prev })

	client := latitudeshgosdk.New(
		latitudeshgosdk.WithSecurity("test"),
		latitudeshgosdk.WithServerURL(server.URL),
	)

	var diags diag.Diagnostics
	lksWaitForNodesReady(context.Background(), client, "lksc_1", "np_1", 3, time.Now().Add(5*time.Second), &diags)

	if diags.HasError() {
		t.Fatalf("unexpected error: %v", diags.Errors())
	}
	if polls != 3 {
		t.Errorf("expected 3 polls before ready_nodes caught up, got %d", polls)
	}
}

// A node pool the configuration never gave a description reports "" rather
// than nothing (seen live 2026-09-18). Copying that into state leaves a diff
// that can never converge: the plan wants null, the API rejects "" on the way
// back in and ignores null, so every apply ends in "inconsistent result after
// apply" or a plan that never empties.
func TestLksDescriptionFromAPI(t *testing.T) {
	cases := []struct {
		name       string
		configured types.String
		reported   types.String
		want       types.String
	}{
		// The case that produced the permanent diff.
		{"unset stays null when the API answers empty", types.StringNull(), types.StringValue(""), types.StringNull()},
		{"unset stays null when the API omits it", types.StringNull(), types.StringNull(), types.StringNull()},

		// Drift has to stay visible: a value the API reports wins over what is
		// configured, so an out-of-band edit shows up as a diff.
		{"changed upstream shows as drift", types.StringValue("old"), types.StringValue("new"), types.StringValue("new")},
		{"emptied upstream shows as drift", types.StringValue("old"), types.StringValue(""), types.StringValue("")},
		{"cleared upstream shows as drift", types.StringValue("old"), types.StringNull(), types.StringNull()},

		// A deliberately configured empty string round-trips untouched.
		{"configured empty round-trips", types.StringValue(""), types.StringValue(""), types.StringValue("")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lksDescriptionFromAPI(tc.configured, tc.reported); !got.Equal(tc.want) {
				t.Errorf("lksDescriptionFromAPI(%s, %s) = %s, want %s", tc.configured, tc.reported, got, tc.want)
			}
		})
	}
}

// An omitted kubernetes_version follows the cluster's control plane. Because a
// standalone pool only knows cluster_id, ModifyPlan reads the cluster's version
// through the API; a control-plane upgrade therefore lands on the pool at the
// NEXT apply (the plan that bumps the cluster still sees the old version). Here
// the mock's cluster version is bumped between steps to stand in for that
// already-applied upgrade, and the pool must follow.
func TestLksNodePool_OmittedVersionFollowsCluster(t *testing.T) {
	mock := &mockLksNodePoolAPI{clusterVersion: "1.31.0"}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	prevReady, prevDelete := lksNodePoolReadyPollInterval, lksNodePoolDeletePollInterval
	lksNodePoolReadyPollInterval = 5 * time.Millisecond
	lksNodePoolDeletePollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		lksNodePoolReadyPollInterval = prevReady
		lksNodePoolDeletePollInterval = prevDelete
	})

	const rn = "latitudesh_lks_node_pool.test"

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockLksNodePoolDestroyed(mock),
		Steps: []resource.TestStep{
			{
				// Config never sets kubernetes_version — it takes the cluster's.
				Config: testAccLksNodePoolConfig(1, ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(rn, "kubernetes_version", "1.31.0"),
				),
			},
			{
				// The control plane upgraded out of band; the next plan reads
				// the new version and the pool follows it.
				PreConfig: func() {
					mock.mu.Lock()
					mock.clusterVersion = "1.32.0"
					mock.mu.Unlock()
				},
				Config: testAccLksNodePoolConfig(1, ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(rn, "kubernetes_version", "1.32.0"),
				),
			},
		},
	})
}

// The opposite of the above: an explicitly set kubernetes_version is pinned and
// must NOT follow a cluster that moved on. Tracking is opt-out.
func TestLksNodePool_ExplicitVersionIsPinned(t *testing.T) {
	mock := &mockLksNodePoolAPI{clusterVersion: "1.31.0"}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	prevReady, prevDelete := lksNodePoolReadyPollInterval, lksNodePoolDeletePollInterval
	lksNodePoolReadyPollInterval = 5 * time.Millisecond
	lksNodePoolDeletePollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		lksNodePoolReadyPollInterval = prevReady
		lksNodePoolDeletePollInterval = prevDelete
	})

	const rn = "latitudesh_lks_node_pool.test"

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockLksNodePoolDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccLksNodePoolConfig(1, `  kubernetes_version = "1.31.0"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(rn, "kubernetes_version", "1.31.0"),
				),
			},
			{
				// Cluster moved to 1.32.0, but the pinned pool stays put: no diff.
				PreConfig: func() {
					mock.mu.Lock()
					mock.clusterVersion = "1.32.0"
					mock.mu.Unlock()
				},
				Config:   testAccLksNodePoolConfig(1, `  kubernetes_version = "1.31.0"`),
				PlanOnly: true,
			},
		},
	})
}
