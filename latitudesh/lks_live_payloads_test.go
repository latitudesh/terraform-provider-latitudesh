package latitudesh

import (
	"encoding/json"
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// What the live API actually answers.
//
// Both bodies below were captured on 2026-09-18 from the cluster the manual
// validation scenario built (GET /lks/clusters/{id} and GET
// .../nodepools/{id}, kept verbatim in .context/lks-live/). They exist because
// the offline mocks had been written from the schema rather than from the
// wire, and the two disagreed on sixteen fields: the cluster envelope omitted
// message, reason, control_plane_endpoint, kubeconfig_url, platform_version
// and both timestamps, and the pool envelope reported ready_nodes as the node
// count where the API reports null. A mapping regression on any of them went
// green.
//
// Every offline test that claims to model the API builds on these, so "the
// mock says so" and "the API says so" cannot drift apart again without this
// file changing.
const lksLiveClusterBody = `{
  "data": {
    "id": "lksc_8d12b878420d45",
    "type": "lks_clusters",
    "attributes": {
      "name": "tf-manual-lks",
      "description": "created by terraform",
      "project_id": "proj_M3Beabq3l5Lnb",
      "site": "LAX2",
      "status": "ready",
      "message": "the cluster is ready",
      "reason": "",
      "control_plane_endpoint": "https://lksc-8d12b878420d45.lks.lsh.io:6443",
      "kubeconfig_url": "/lks/clusters/lksc_8d12b878420d45/kubeconfig",
      "kubernetes_version": "1.36.1",
      "platform_version": "lks-v1.36.1-007",
      "network": {
        "pod_cidrs": ["10.0.0.0/12"],
        "service_cidrs": ["10.96.0.0/15"],
        "node_cidrs": ["10.16.0.0/21"]
      },
      "created_at": "2026-09-18T15:39:51.019044Z",
      "updated_at": "2026-09-18T15:46:15.110843Z"
    }
  }
}`

// Note what the platform reports here and what it does not: `ready_nodes` and
// `max_pods_per_node` are null on a pool that is `ready` with its node up, and
// `description` comes back as "" rather than being omitted.
const lksLiveNodePoolBody = `{
  "data": {
    "id": "lksnp_7341ea2c4ab843",
    "type": "lks_node_pools",
    "attributes": {
      "name": "np-b55eea9b6036",
      "type": "bare_metal",
      "mode": "on_demand",
      "plan": "f4-metal-small",
      "description": "",
      "count": 1,
      "max_pods_per_node": null,
      "kubernetes_version": "1.36.1",
      "platform_version": "lks-v1.36.1-007",
      "status": "ready",
      "message": "the node pool is ready",
      "reason": "",
      "ready_nodes": null,
      "labels": {"workload": "general"},
      "taints": [{"key": "dedicated", "value": "manual-test", "effect": "PreferNoSchedule"}],
      "created_at": "2026-09-18T15:39:52.045214Z",
      "updated_at": "2026-09-18T15:42:14.740745Z"
    }
  }
}`

// liveLksClusterAttributes decodes the captured cluster body through the SDK's
// own types, which is the path a real response takes.
func liveLksClusterAttributes() *components.LksClusterDataAttributes {
	var envelope struct {
		Data components.LksClusterData `json:"data"`
	}
	if err := json.Unmarshal([]byte(lksLiveClusterBody), &envelope); err != nil {
		panic("decoding the captured LKS cluster body: " + err.Error())
	}
	return envelope.Data.Attributes
}

func liveLksNodePoolAttributes() *components.LksNodePoolDataAttributes {
	var envelope struct {
		Data components.LksNodePoolData `json:"data"`
	}
	if err := json.Unmarshal([]byte(lksLiveNodePoolBody), &envelope); err != nil {
		panic("decoding the captured LKS node pool body: " + err.Error())
	}
	return envelope.Data.Attributes
}

// lksLiveAttrsMap returns a fresh, mutable copy of a captured body's
// attributes, for mocks that answer with the live shape and overwrite only the
// fields their own state owns.
func lksLiveAttrsMap(body string) map[string]any {
	var envelope struct {
		Data struct {
			Attributes map[string]any `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		panic("decoding captured LKS body: " + err.Error())
	}
	return envelope.Data.Attributes
}

// The fixtures are only worth anything if the SDK can still parse them: a
// field the SDK renames or drops would silently stop being asserted anywhere.
func TestLiveLksFixturesDecode(t *testing.T) {
	cluster := liveLksClusterAttributes()
	if cluster == nil {
		t.Fatal("captured cluster body decoded to nil attributes")
	}
	if cluster.Message == nil || *cluster.Message != "the cluster is ready" {
		t.Errorf("cluster message did not survive decoding: %v", cluster.Message)
	}
	if cluster.Network == nil || len(cluster.Network.GetNodeCidrs()) != 1 {
		t.Errorf("cluster network did not survive decoding: %+v", cluster.Network)
	}

	pool := liveLksNodePoolAttributes()
	if pool == nil {
		t.Fatal("captured node pool body decoded to nil attributes")
	}
	if pool.ReadyNodes != nil {
		t.Errorf("ready_nodes = %v, want nil — the live API leaves it out", *pool.ReadyNodes)
	}
	if pool.Description == nil || *pool.Description != "" {
		t.Errorf("description = %v, want an empty string", pool.Description)
	}
	if len(pool.Taints) != 1 || pool.Taints[0].Key != "dedicated" {
		t.Errorf("taints did not survive decoding: %+v", pool.Taints)
	}
}
