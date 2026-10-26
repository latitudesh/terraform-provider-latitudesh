---
page_title: "latitudesh_lks_node_pool Resource - latitudesh"
subcategory: ""
description: |-
  A node pool for an LKS (Latitude Kubernetes Service) cluster. latitudesh_lks provisions a control plane only: the cluster has no workers, and does not become ready, until at least one pool has nodes the platform's own components (CNI, CoreDNS) can schedule on.
  Create waits for the pool's nodes and then for the cluster to settle on them, so an apply that adds a cluster's first pool ends on a ready cluster. If the cluster does not get there within the create timeout, the apply still succeeds, with a warning: the pool itself is up.
---

# latitudesh_lks_node_pool (Resource)

A node pool for an LKS (Latitude Kubernetes Service) cluster. [`latitudesh_lks`](lks.md) is the control plane alone; every pool, the first one included, is one of these, so each scales, upgrades and is destroyed without touching the cluster.

A cluster becomes `ready` only once a pool has nodes. Its create returns as soon as the control plane is up (`reason` `WaitingForNodePool`), so a pool that references the cluster — `cluster_id = latitudesh_lks.example.id` — is created right after it. This resource's create waits for its own nodes and **then for the cluster** to finish installing the platform's components on them, so the apply that adds a cluster's first pool ends on a `ready` cluster. On a cluster that is already ready, that second wait is a single read.

If the cluster does not reach `ready` within `timeouts.create`, the apply still succeeds, with a warning naming the cluster's last status and reason. It is a warning on purpose: the pool is up, and failing its create would taint it, so the next apply would rebuild healthy bare metal over a problem that belongs to the cluster.

At least one pool in the cluster must stay free of `NoSchedule` and `NoExecute` taints, or the platform's components (CNI, CoreDNS) have nowhere to run and the cluster never leaves `provisioning`. The API accepts the taint, and a pool cannot tell whether it is the only one, so this is not checked at plan time — keep a general-purpose pool, as the example does, and taint the specialized ones.

Two lookups feed a pool: [`latitudesh_lks_plans`](../data-sources/lks_plans.md) for `plan` and [`latitudesh_lks_versions`](../data-sources/lks_versions.md) for `kubernetes_version`. A pool has no site of its own — it is built wherever its cluster is — so filter the plan catalog by the **cluster's** `site`: a plan that is merely *available* there is not enough, it has to be in `stock`, or the pool has nothing to build on.

`cluster_id`, `plan` and `max_pods_per_node` force a new resource; the API offers no update for the first two and rejects a PATCH carrying the third with 422. Everything else updates in place, including `node_count`, which is how a pool scales.

## `kubernetes_version` tracks the cluster unless you pin it

Leave `kubernetes_version` unset and the pool follows the cluster's control-plane version: upgrade `latitudesh_lks` and this pool upgrades to match. Because a pool only knows its `cluster_id`, it learns the new version by reading the cluster during planning — so the follow lands on the **next** apply after the control plane is actually on the new version, not the same one that bumps it. Set `kubernetes_version` explicitly to pin the pool to a single version and opt out of tracking. Following recycles the pool's nodes, the same as any version change.

Two API rules worth knowing before you meet them as a 422:

* `kubernetes_version` must never be **newer** than the cluster's control plane (`VERSION_SKEW`). Upgrade `latitudesh_lks` first, then the pool — which tracking does for you.
* Label and taint keys under `kubernetes.io`, `k8s.io`, `cluster.x-k8s.io`, `lks.latitude.sh` or their subdomains are reserved, and each collection is capped at 50 entries. This provider rejects both at plan time rather than letting the apply fail. Key and value *format* rules (63-character names, DNS-label prefixes) are still only enforced server-side.

`labels` and `taints` are a declarative replace, not a merge: what you write is what the pool ends up with, and removing them all clears them.

## Example Usage

```terraform
# A cluster's workers. latitudesh_lks is the control plane alone; every pool,
# including the first, is a latitudesh_lks_node_pool that scales, upgrades and
# is destroyed independently of the cluster.
data "latitudesh_lks_sites" "for_pool" {}

data "latitudesh_lks_versions" "for_pool" {}

locals {
  pool_site = data.latitudesh_lks_sites.for_pool.sites[0].slug
}

# `stock` only lists plans with capacity at this site right now. A plan that is
# merely *available* there has nothing to build on — check `regions[]` on the
# unfiltered catalog if you need that wider view.
data "latitudesh_lks_plans" "for_pool" {
  site = local.pool_site
}

resource "latitudesh_project" "lks_node_pool" {
  name = "lks-node-pool-example"
}

resource "latitudesh_lks" "node_pool_example" {
  project            = latitudesh_project.lks_node_pool.id
  name               = "example-cluster"
  site               = local.pool_site
  kubernetes_version = data.latitudesh_lks_versions.for_pool.default_version
}

# The general-purpose pool. It carries no hard taint, so the platform's own
# components (CNI, CoreDNS) can run on it — a cluster needs at least one pool
# like this to become ready.
resource "latitudesh_lks_node_pool" "general" {
  cluster_id = latitudesh_lks.node_pool_example.id
  node_count = 1
  plan       = keys(data.latitudesh_lks_plans.for_pool.stock)[0]

  labels = {
    "workload" = "general"
  }
}

resource "latitudesh_lks_node_pool" "gpu" {
  cluster_id = latitudesh_lks.node_pool_example.id
  node_count = 2
  plan       = keys(data.latitudesh_lks_plans.for_pool.stock)[0]

  # Three ways to hold this attribute, from loosest to tightest:
  #   omitted                    -> tracks the control plane; a cluster upgrade
  #                                 reaches this pool on the NEXT apply
  #   reference (as done here)   -> follows the cluster in the SAME apply that
  #                                 upgrades it
  #   literal "1.36.1"           -> pinned until you change it
  # Whatever the form, it may never be NEWER than the control plane
  # (422 VERSION_SKEW), and every version change recycles the pool's nodes.
  kubernetes_version = latitudesh_lks.node_pool_example.kubernetes_version

  labels = {
    "workload" = "gpu"
  }

  # A hard taint reserves this pool for workloads that tolerate it. Fine here,
  # because the general pool above stays schedulable for everything else.
  taints = [{
    key    = "dedicated"
    value  = "gpu"
    effect = "NoSchedule"
  }]

  # Bare metal: create and scale wait until the platform reports the pool
  # settled (and for ready_nodes to reach node_count, when it reports one);
  # create also waits for the cluster to settle on the new nodes.
  timeouts = {
    create = "90m"
    update = "90m"
    delete = "30m"
  }
}

data "latitudesh_lks_node_pools" "example" {
  cluster_id = latitudesh_lks.node_pool_example.id

  depends_on = [latitudesh_lks_node_pool.general, latitudesh_lks_node_pool.gpu]
}
```

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `cluster_id` (String) ID of the `latitudesh_lks` cluster the pool belongs to. Changing it forces a new resource; a pool cannot move between clusters.
- `node_count` (Number) Number of nodes in the pool. Changing it scales the pool in place; apply waits for the new count to be ready.
- `plan` (String) Plan the pool's nodes are provisioned from, as listed by `latitudesh_lks_plans` — not interchangeable with a `latitudesh_plan` server slug. Check the plan has stock at the cluster's site (`in_stock_sites`) before applying. There is no update endpoint for it, so changing it forces a new resource.

### Optional

- `description` (String) Optional customer description. It cannot be removed once set — the API rejects an empty value and ignores a null — so removing it from configuration leaves a diff that cannot converge; change it instead.
- `kubernetes_version` (String) Kubernetes patch version for the pool's nodes, from `latitudesh_lks_versions`. **Omit it and the pool tracks the cluster's control-plane version**: a control-plane upgrade brings the pool with it (on the next apply, since the pool learns the new version only once the cluster is on it). Set it explicitly to pin the pool to one version instead. Either way it must never be **newer** than the control plane, which the API rejects with 422 `VERSION_SKEW` — tracking guarantees this by construction, upgrade the cluster first.
- `labels` (Map of String) Kubernetes labels applied to every node in the pool (max 50). Keys follow Kubernetes label-key syntax; values may be empty. Keys under `kubernetes.io`, `k8s.io`, `cluster.x-k8s.io`, `lks.latitude.sh` or their subdomains are reserved and rejected with 422.
- `max_pods_per_node` (Number) kubelet `--max-pods` for every node in the pool. Create-only: the API rejects a PATCH that carries it with 422, so changing it forces a new resource. Omit for the platform default (110).
- `name` (String) Display name for the pool. Generated by the platform when omitted.
- `taints` (Attributes Set) Kubernetes taints applied to every node in the pool (max 50). A set rather than a list: the API imposes no ordering, and each `(key, effect)` pair must be unique.

Keep at least one pool in the cluster free of `NoSchedule` and `NoExecute` taints: the platform's own components do not tolerate them, so a cluster whose every pool carries one never leaves `provisioning`. The API accepts the taint, and a single pool cannot tell whether it is the only one, so this is not checked at plan time. (see [below for nested schema](#nestedatt--taints))
- `timeouts` (Attributes) (see [below for nested schema](#nestedatt--timeouts))
- `type` (String) Node type. `bare_metal` is the only value the API accepts today. Set at creation; changing it forces a new resource.

### Read-Only

- `created_at` (String) Timestamp when the pool was created.
- `id` (String) Node pool identifier.
- `message` (String) Human-readable detail behind the current `status`.
- `mode` (String) Pool mode reported by the platform.
- `platform_version` (String) Platform (LKS controller) version managing this pool.
- `ready_nodes` (Number) Nodes currently ready. The platform does not always report it: it is absent while the pool is still building, and stays `null` on pools it never counts — so a null here does not mean zero. When it is reported, apply additionally waits for it to reach `node_count`.
- `reason` (String) Machine-readable status reason (open enum).
- `status` (String) Pool lifecycle status. Open enum sourced from the platform controller — new values may appear without notice, which is why apply waits until the platform reports no operation in progress rather than for one particular word.
- `updated_at` (String) Timestamp when the pool was last updated.

<a id="nestedatt--taints"></a>
### Nested Schema for `taints`

Required:

- `effect` (String) Taint effect: `NoSchedule`, `PreferNoSchedule` or `NoExecute`.
- `key` (String) Taint key. Same syntax and reserved-prefix rules as a label key.

Optional:

- `value` (String) Taint value.


<a id="nestedatt--timeouts"></a>
### Nested Schema for `timeouts`

Optional:

- `create` (String) Budget for the entire create: the pool settling (the platform reporting no operation in progress, and ready_nodes reaching node_count when it reports one), then the cluster settling on the new nodes — normally "ready". The second part only warns when it runs out. Bare metal, so allow for a real deploy. Default: 60 minutes.
- `delete` (String) Timeout for the pool to be fully removed. Default: 30 minutes.
- `update` (String) Timeout for a scale or version change to settle. A scale and a version change in one apply are two sequential operations, each waited on, within this one budget; if the scale uses it up, the version change is not started and the next apply picks it up. Default: 60 minutes.

## Import

A node pool is addressed by its cluster **and** its own ID, so the import ID is both, separated by a colon. A bare pool ID is not enough — the API has no endpoint that reads a pool without its cluster.

**CLI Import**

```sh
terraform import latitudesh_lks_node_pool.example <LKS_CLUSTER_ID>:<NODE_POOL_ID>
```

**Import Block (Experimental)**

```hcl
import {
  to = latitudesh_lks_node_pool.example
  id = "<LKS_CLUSTER_ID>:<NODE_POOL_ID>"
}
```

Then run:

```sh
terraform plan -generate-config-out=generated_lks_node_pool.tf
```
