---
page_title: "latitudesh_lks_cluster Resource - latitudesh"
subcategory: ""
description: |-
  LKS (Latitude Kubernetes Service) cluster resource. Provisions the control plane only; worker capacity is added with latitudesh_lks_node_pool.
  A cluster needs at least one node pool to become ready. Without one, the platform's own components (CNI, CoreDNS) have nowhere to run, so status stays provisioning with reason WaitingForNodePool. Create therefore waits for the control plane, not for ready: it returns once the platform reports WaitingForNodePool, which is the point where a node pool can join. The create of that pool then waits for the cluster to reach ready.
---

# latitudesh_lks_cluster (Resource)

Provisions an LKS (Latitude Kubernetes Service) cluster's **control plane**. Worker capacity is a separate resource: every node pool, the first one included, is a [`latitudesh_lks_node_pool`](lks_node_pool.md).

## A cluster needs a node pool to become `ready`

A cluster with no node pool never finishes converging: the platform's own components (CNI, CoreDNS) have nowhere to run, so `status` stays `provisioning` with `reason` `WaitingForNodePool` — permanently, not slowly. And no pool can be created before the cluster exists.

So create waits for the **control plane**, not for `ready`. It returns as soon as the platform reports `WaitingForNodePool`, which is the point where a pool can join, and Terraform then creates the `latitudesh_lks_node_pool` resources that reference the cluster. Each pool's create waits for its nodes **and then for the cluster** to finish installing the platform's components on them (`reason` `FinishingSetup` in between), so the apply that adds the first pool ends on a `ready` cluster, with its kubeconfig published. This resource's own `status` still holds `provisioning` from its create until the next refresh.

At least one pool has to stay free of `NoSchedule` and `NoExecute` taints. The platform's components do not tolerate them, so a cluster whose every pool carries one never leaves `provisioning`. The API accepts the taint, and no single pool can tell whether it is the only one, so this is not checked at plan time.

## Updating

`project`, `site` and `network` have no update endpoint and force a new resource if changed. `name`, `description` and `kubernetes_version` update in place; setting a newer `kubernetes_version` consents to an in-place control-plane upgrade, and apply does not return until the new version is live. Lowering `kubernetes_version` is rejected at plan time — the platform allows no in-place downgrade (422 `DOWNGRADE_NOT_ALLOWED`), so the plan fails immediately instead of the apply.

Node pools upgrade separately and may never run a version newer than the control plane (`VERSION_SKEW`). A pool that omits `kubernetes_version` tracks the cluster; see [`latitudesh_lks_node_pool`](lks_node_pool.md) for how and when it follows.

The platform accepts changes only while the cluster is idle: a cluster that is still `provisioning` — for instance, one waiting for its first pool — rejects an update with 409.

Apply waits until the platform reports no operation in progress, rather than until `status` equals a particular word — so a status this provider has never seen still ends the wait instead of hanging it. A cluster that is `paused`, `deleting` or `deleted` fails immediately, since waiting cannot reach ready from there.

`description` cannot be **removed** once set: the API declares it `filled`, so an empty string is rejected and a null is ignored. Removing it from configuration leaves a diff that cannot converge; set a new value instead.

## Example Usage

```terraform
# A cluster is a control plane; its workers are node pools. Site, Kubernetes
# version and plan all come from lookups — none of them is guessable, and the
# lookups keep this runnable on any account.
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

resource "latitudesh_lks_cluster" "example" {
  project            = latitudesh_project.lks.id
  name               = "example-cluster"
  site               = local.site
  kubernetes_version = data.latitudesh_lks_versions.for_cluster.default_version

  # Note: `description` cannot be removed once set — the API requires a
  # non-empty value and ignores a null. Change it instead of clearing it.
  description = "managed by terraform"

  # Create returns once the control plane is up, with status "provisioning"
  # and reason "WaitingForNodePool": the cluster becomes ready only after the
  # pool below has nodes.
  timeouts = {
    create = "30m"
    update = "60m"
    delete = "30m"
  }
}

# The cluster's workers. Without at least one pool it never becomes ready, and
# at least one pool has to stay free of NoSchedule/NoExecute taints so the
# platform's own components (CNI, CoreDNS) have somewhere to run.
resource "latitudesh_lks_node_pool" "example" {
  cluster_id = latitudesh_lks_cluster.example.id
  node_count = 2
  plan       = keys(data.latitudesh_lks_plans.for_cluster.stock)[0]

  labels = {
    "workload" = "general"
  }

  # Bare metal: create waits for the nodes to come up, then for the cluster to
  # be ready on them — this is the pool that takes it there.
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
- `kubeconfig_url` (String) URL to fetch the cluster kubeconfig from. The platform publishes the kubeconfig only once `status` is `ready`, which takes a node pool, so this is null right after a create; the next refresh after the first pool is up fills it. Until then `GET /lks/clusters/{id}/kubeconfig` answers 409 `NOT_READY`.
- `message` (String) Human-readable detail behind the current `status`.
- `platform_version` (String) Platform (LKS controller) version managing this cluster.
- `reason` (String) Machine-readable status reason (open enum). `WaitingForNodePool` means the control plane is up and the cluster has no ready node yet; `FinishingSetup` means a node has joined and the platform is still installing its components.
- `status` (String) Cluster lifecycle status. Open enum sourced from the platform controller; values in use today are `provisioning`, `ready`, `updating`, `scaling`, `upgrading`, `paused`, `deleting` and `deleted`, and new ones may appear without notice.

Right after a create this reads `provisioning` with `reason` `WaitingForNodePool`: the control plane is up, and the cluster becomes `ready` only once a `latitudesh_lks_node_pool` has nodes and the platform has finished installing its components on them. This resource's create cannot wait for that, because no pool can be created before the cluster exists; the pool's create does, and this attribute catches up on the next refresh.
- `updated_at` (String) Timestamp when the cluster was last updated.

<a id="nestedatt--network"></a>
### Nested Schema for `network`

Optional:

- `node_cidrs` (Set of String) Node CIDR ranges.
- `pod_cidrs` (Set of String) Pod CIDR ranges.
- `service_cidrs` (Set of String) Service CIDR ranges.


<a id="nestedatt--timeouts"></a>
### Nested Schema for `timeouts`

Optional:

- `create` (String) Budget for the entire create: the cluster record becoming readable and the control plane coming up, which the platform signals with reason "WaitingForNodePool". Node pools are separate resources with their own timeouts. Default: 30 minutes. Example: "45m", "1h"
- `delete` (String) Timeout for the cluster to be fully deleted. Default: 15 minutes.
- `update` (String) Budget for an update: a control-plane upgrade reaching "ready" again on the new kubernetes_version. Node pools upgrade separately. Default: 30 minutes.

## Import

You can import an LKS cluster resource using either the CLI method or the [experimental import block](https://developer.hashicorp.com/terraform/language/import).

**CLI Import**

The `latitudesh_lks_cluster` resource can be imported by specifying the cluster ID:

```sh
terraform import latitudesh_lks_cluster.example <LKS_CLUSTER_ID>
```

Import covers the control plane only. Import each of the cluster's node pools as its own [`latitudesh_lks_node_pool`](lks_node_pool.md).

**Import Block (Experimental)**

Terraform v1.5.0 and later supports the experimental import block, which allows you to define imports in your configuration.

```hcl
import {
  to = latitudesh_lks_cluster.example
  id = "<LKS_CLUSTER_ID>"
}
```

Then run:

```sh
terraform plan -generate-config-out=generated_lks.tf
```

> **Note:** The import block feature is experimental and its syntax or behavior may change in future Terraform versions.
