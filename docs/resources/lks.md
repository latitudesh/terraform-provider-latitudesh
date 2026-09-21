---
page_title: "latitudesh_lks Resource - latitudesh"
subcategory: ""
description: |-
  LKS (Latitude Kubernetes Service) cluster resource. Provisions the control plane and the cluster's first node pool.
  A cluster needs at least one node pool, which is why default_node_pool is required here: until one exists the control plane never finishes converging and status stays provisioning — permanently, not slowly. Create builds that pool and waits for the whole thing to reach ready, so an apply that succeeds leaves a usable cluster. Every pool beyond the first is a latitudesh_lks_node_pool.
---

# latitudesh_lks (Resource)

Provisions an LKS (Latitude Kubernetes Service) cluster: the control plane **and its first node pool**, in one resource. Apply does not return until both are up and `status` is `ready`, so a create that succeeds leaves a cluster you can use.

## `default_node_pool` is required

A cluster with no node pool never finishes converging: `status` stays `provisioning` permanently, not slowly. And `POST /lks/clusters` takes no pool inline, so the pool is necessarily a second call.

That combination rules out expressing the first pool as a separate resource: Terraform would not start it until this resource finished creating, so waiting for `ready` here would deadlock — the cluster waiting for a pool waiting for the cluster. Folding it into the schema as `default_node_pool` is what makes the requirement enforceable at plan time *and* lets create legitimately wait for a working cluster. It is the same split [`azurerm_kubernetes_cluster`](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/kubernetes_cluster) and [`digitalocean_kubernetes_cluster`](https://registry.terraform.io/providers/digitalocean/digitalocean/latest/docs/resources/kubernetes_cluster) arrived at.

Every pool **beyond the first** is a [`latitudesh_lks_node_pool`](lks_node_pool.md), with its own lifecycle, timeouts and import. This resource manages only the pool whose `id` it recorded in `default_node_pool`, and ignores any other pool in the cluster — so the two never fight over the same object.

Changing `plan` or `max_pods_per_node` inside the block replaces **the pool, not the cluster**: a new pool is built before the old one is removed, so the cluster is never left without one. That is the one place this deliberately improves on `azurerm_kubernetes_cluster`, where editing `default_node_pool` rebuilds the whole cluster.

## Updating

`project`, `site` and `network` have no update endpoint and force a new resource if changed. `name`, `description` and `kubernetes_version` update in place; setting a newer `kubernetes_version` consents to an in-place control-plane upgrade, and apply does not return until the new version is live. Lowering `kubernetes_version` is rejected at plan time — the platform allows no in-place downgrade (422 `DOWNGRADE_NOT_ALLOWED`), so the plan fails immediately instead of the apply an hour in.

Inside `default_node_pool`, `node_count` (a scale), `name`, `description`, `kubernetes_version`, `labels` and `taints` update in place. A scale and an upgrade are issued as two sequential requests, because the API rejects them in one.

Leave `default_node_pool.kubernetes_version` unset and the pool **tracks the cluster's version**: a control-plane upgrade upgrades this pool too, in the same apply, control plane first (so `VERSION_SKEW` cannot happen). Set it explicitly to pin the pool and opt out. Either way a version change recycles the pool's nodes.

Apply waits until the platform reports no operation in progress, rather than until `status` equals a particular word — so a status this provider has never seen still ends the wait instead of hanging it. A cluster that is `paused`, `deleting` or `deleted` fails immediately, since waiting cannot reach ready from there.

`default_node_pool` rejects `NoSchedule` and `NoExecute` taints. The cluster's own system workloads have nowhere to run but this pool, so a hard taint keeps them from scheduling and the cluster never leaves `provisioning` — the API accepts it, the apply reports success, and then the readiness wait times out on a cluster that was never going to converge. Use `PreferNoSchedule`, or put the taint on a separate [`latitudesh_lks_node_pool`](lks_node_pool.md), which is the usual way to reserve a pool for specific workloads.

`description` — on the cluster and on the pool alike — cannot be **removed** once set: the API declares it `filled`, so an empty string is rejected and a null is ignored. Removing it from configuration leaves a diff that cannot converge; set a new value instead.

## Example Usage

```terraform
# A cluster is a control plane plus its required first node pool, created
# together. Site, Kubernetes version and plan all come from lookups — none of
# them is guessable, and the lookups keep this runnable on any account.
data "latitudesh_lks_sites" "for_cluster" {}

data "latitudesh_lks_versions" "for_cluster" {}

locals {
  # Pick a site deliberately in real configuration; the first one keeps this
  # example runnable on any account.
  site = data.latitudesh_lks_sites.for_cluster.sites[0].slug
}

# Filtering by site narrows the catalog to plans that can actually be built
# there right now, and fills `stock` with slug -> nodes available.
data "latitudesh_lks_plans" "for_cluster" {
  site = local.site
}

resource "latitudesh_project" "lks" {
  name = "lks-example"
}

resource "latitudesh_lks" "example" {
  project            = latitudesh_project.lks.id
  name               = "example-cluster"
  site               = local.site
  kubernetes_version = data.latitudesh_lks_versions.for_cluster.default_version

  # Note: `description` cannot be removed once set — the API requires a
  # non-empty value and ignores a null. Change it instead of clearing it.
  description = "managed by terraform"

  # kubernetes_version is omitted on the pool, so it tracks the control
  # plane: upgrading the cluster upgrades this pool in the same apply. Set it
  # explicitly to pin the pool instead.
  default_node_pool = {
    node_count = 2
    plan       = keys(data.latitudesh_lks_plans.for_cluster.stock)[0]

    labels = {
      "workload" = "general"
    }

    # `NoSchedule` and `NoExecute` are rejected on the default pool: the
    # cluster's own system workloads have nowhere else to run, and a hard
    # taint would leave it provisioning forever. Put those on an additional
    # latitudesh_lks_node_pool instead.
    taints = [{
      key    = "dedicated"
      value  = "general"
      effect = "PreferNoSchedule"
    }]
  }

  # Bare metal: create covers the cluster and its first pool coming up.
  timeouts = {
    create = "90m"
    update = "90m"
    delete = "30m"
  }
}
```

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `default_node_pool` (Attributes) The cluster's first node pool, created with the cluster and required: a cluster with no node pool never leaves `provisioning`, and `POST /lks/clusters` takes none inline, so it cannot be a separate resource without deadlocking. Additional pools go in `latitudesh_lks_node_pool`, which owns its own lifecycle; this resource only ever manages the pool whose `id` it recorded here and ignores any other pool in the cluster. (see [below for nested schema](#nestedatt--default_node_pool))
- `kubernetes_version` (String) Kubernetes patch version, exactly as listed by `GET /lks/available_versions`. Setting a newer patch consents to a control-plane upgrade in place. Lowering it is rejected at plan time: the platform allows no in-place downgrade (422 `DOWNGRADE_NOT_ALLOWED`), so this fails before the apply rather than an hour into it.
- `name` (String) Display name for the cluster.
- `site` (String) Site slug the cluster is deployed to (single site per cluster; one of the slugs returned by `GET /lks/sites`). Changing this forces a new resource; there is no update endpoint.

### Optional

- `description` (String) Optional customer description. It cannot be removed once set — the API rejects an empty value and ignores a null — so removing it from configuration leaves a diff that cannot converge; change it instead.
- `network` (Attributes) Cluster CIDR overrides. Any field left unset takes the platform default. Changing this forces a new resource; there is no update endpoint. (see [below for nested schema](#nestedatt--network))
- `project` (String) The project (ID or slug) to create the cluster in. Optional here only if `project` is set on the provider block; one of the two is required. Changing it forces a new resource.
- `timeouts` (Attributes) (see [below for nested schema](#nestedatt--timeouts))

### Read-Only

- `control_plane_endpoint` (String) Kubernetes API server endpoint.
- `created_at` (String) Timestamp when the cluster was created.
- `id` (String) LKS cluster identifier.
- `kubeconfig_url` (String) URL to fetch the cluster kubeconfig from once the control plane is ready. `GET /lks/clusters/{id}/kubeconfig` answers 409 `NOT_READY` until `status` is `ready` — and the URL is published moments *after* the status flips, so it can still be null in state right after a create; the next refresh fills it.
- `message` (String) Human-readable detail behind the current `status`.
- `platform_version` (String) Platform (LKS controller) version managing this cluster.
- `reason` (String) Machine-readable status reason (open enum).
- `status` (String) Cluster lifecycle status. Open enum sourced from the platform controller; values in use today are `provisioning`, `ready`, `updating`, `scaling`, `upgrading`, `paused`, `deleting` and `deleted`, and new ones may appear without notice.

After a successful apply this reads `ready`: create builds `default_node_pool` and waits for the control plane to converge, so a cluster that never got there fails the apply instead of being recorded half-built.
- `updated_at` (String) Timestamp when the cluster was last updated.

<a id="nestedatt--default_node_pool"></a>
### Nested Schema for `default_node_pool`

Required:

- `node_count` (Number) Number of nodes. Changing it scales the pool in place.
- `plan` (String) Plan the pool's nodes are provisioned from, as listed by `latitudesh_lks_plans` — not a `latitudesh_plan` server slug. It must have stock at the cluster's `site` (`in_stock_sites`). Changing it replaces the pool, not the cluster: a new one is built before the old is removed, so the cluster is never left without one.

Optional:

- `description` (String) Optional customer description for the pool. Like the cluster's, it cannot be removed once set; change it instead.
- `kubernetes_version` (String) Kubernetes patch version for the pool's nodes. Omit it and the pool tracks the cluster's `kubernetes_version` — a control-plane upgrade upgrades this pool too, in the same apply. Set it explicitly to pin the pool to one version. Never newer than the control plane (422 `VERSION_SKEW`); tracking keeps it in step by construction.
- `labels` (Map of String) Kubernetes labels applied to every node in the pool (max 50). Reserved prefixes are rejected — see `latitudesh_lks_node_pool`.
- `max_pods_per_node` (Number) kubelet `--max-pods` for the pool's nodes. Create-only on the API, so changing it replaces the pool. Omit for the platform default (110).
- `name` (String) Display name for the pool. Generated by the platform when omitted.
- `taints` (Attributes Set) Kubernetes taints applied to every node in the pool (max 50). Each `(key, effect)` pair must be unique.

`NoSchedule` and `NoExecute` are rejected here: the cluster's own system workloads have nowhere to run but this pool, so a hard taint keeps them from scheduling and the cluster never leaves `provisioning`. Use `PreferNoSchedule`, or put the taint on a separate `latitudesh_lks_node_pool`. (see [below for nested schema](#nestedatt--default_node_pool--taints))

Read-Only:

- `created_at` (String) Timestamp when the pool was created.
- `id` (String) Node pool identifier, assigned on creation. This is how the cluster recognizes its own pool on refresh.
- `message` (String) Human-readable detail behind the pool's current `status` — this is what explains a pool that is not coming up.
- `mode` (String) Provisioning mode the platform assigned to the pool's nodes, e.g. `on_demand`.
- `platform_version` (String) Platform (LKS controller) version running on the pool's nodes.
- `ready_nodes` (Number) Nodes currently ready. The platform does not always report it: it is absent while the pool is still building, and stays `null` on pools it never counts, so a null here does not mean zero.
- `reason` (String) Machine-readable status reason for the pool (open enum).
- `status` (String) Pool lifecycle status.
- `type` (String) Node type backing the pool. Read-only here: `POST /lks/clusters/{id}/nodepools` only accepts `bare_metal` today, so there is nothing to choose. `latitudesh_lks_node_pool` exposes it as a write once that changes.
- `updated_at` (String) Timestamp when the pool was last updated.

<a id="nestedatt--default_node_pool--taints"></a>
### Nested Schema for `default_node_pool.taints`

Required:

- `effect` (String) Taint effect: `NoSchedule`, `PreferNoSchedule` or `NoExecute`.
- `key` (String) Taint key.

Optional:

- `value` (String) Taint value.



<a id="nestedatt--network"></a>
### Nested Schema for `network`

Optional:

- `node_cidrs` (Set of String) Node CIDR ranges.
- `pod_cidrs` (Set of String) Pod CIDR ranges.
- `service_cidrs` (Set of String) Service CIDR ranges.


<a id="nestedatt--timeouts"></a>
### Nested Schema for `timeouts`

Optional:

- `create` (String) Budget for the entire create: the cluster record becoming readable, the nodes of default_node_pool coming up, and the control plane reaching status "ready". The nodes are the slow part — they are physical machines. Default: 30 minutes. Example: "45m", "1h"
- `delete` (String) Timeout for the cluster to be fully deleted. Default: 15 minutes.
- `update` (String) Budget for the entire update: a control-plane upgrade reaching "ready" again, plus whatever default_node_pool needs on top — a scale, a version change following the cluster, or a full pool replacement, which builds new bare metal before releasing the old. The pool's version change is only started while budget remains; otherwise the next apply picks it up. Default: 30 minutes.

## Import

You can import an LKS cluster resource using either the CLI method or the [experimental import block](https://developer.hashicorp.com/terraform/language/import).

**CLI Import**

The `latitudesh_lks` resource can be imported by specifying the cluster ID:

```sh
terraform import latitudesh_lks.example <LKS_CLUSTER_ID>
```

Nothing marks a pool as "the default" server-side, so import adopts the **first** pool the cluster reports as `default_node_pool` and leaves the rest to be imported as [`latitudesh_lks_node_pool`](lks_node_pool.md). Check the result before the next apply. A cluster with no pool at all imports with a warning — `default_node_pool` is required, so the next apply creates one.

**Import Block (Experimental)**

Terraform v1.5.0 and later supports the experimental import block, which allows you to define imports in your configuration.

```hcl
import {
  to = latitudesh_lks.example
  id = "<LKS_CLUSTER_ID>"
}
```

Then run:

```sh
terraform plan -generate-config-out=generated_lks.tf
```

> **Note:** The import block feature is experimental and its syntax or behavior may change in future Terraform versions.
