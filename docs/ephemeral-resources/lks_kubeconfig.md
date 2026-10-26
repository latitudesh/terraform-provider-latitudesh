---
page_title: "latitudesh_lks_kubeconfig Ephemeral Resource - latitudesh"
subcategory: ""
description: |-
  Reads an LKS (Latitude Kubernetes Service) cluster's kubeconfig for the duration of the Terraform run. The credentials never reach state or plan files, which is why this is an ephemeral resource rather than a data source — a kubeconfig grants cluster-admin. Requires Terraform 1.10 or later.
  The cluster must be ready: the platform publishes the kubeconfig only then, which takes a node pool with nodes, and until then GET /lks/clusters/{id}/kubeconfig answers 409 NOT_READY. For an existing cluster take the id from latitudesh_lks_clusters filtered on status = "ready"; for one created in the same configuration, order the read after its node pools with depends_on — a pool's create waits for the cluster to be ready.
  An LKS kubeconfig does not expire, so this needs no renewal and stays valid across a long apply — and a copy that leaks stays valid too, which is the reason not to persist one.
---

# latitudesh_lks_kubeconfig (Ephemeral Resource)

Reads an LKS (Latitude Kubernetes Service) cluster's kubeconfig for the duration of a Terraform run.

This is an [ephemeral resource](https://developer.hashicorp.com/terraform/language/resources/ephemeral) rather than a data source on purpose: a kubeconfig grants cluster-admin, and a data source would persist those credentials in state and in every plan file, where they are neither encrypted nor redacted. Nothing here is ever written to state. Requires Terraform 1.10 or later.

An LKS kubeconfig **does not expire**. That is why this resource needs no renewal and stays valid through an apply that runs for an hour — and equally why a copy of it must not be left lying in a state file, since a leaked one never stops working.

The cluster must be `ready`. The platform publishes the kubeconfig only then, and until it does `GET /lks/clusters/{id}/kubeconfig` answers 409 `NOT_READY`. A cluster becomes ready only once a node pool has nodes, so the kubeconfig does not exist while [`latitudesh_lks`](../resources/lks.md) is still `provisioning` with `reason` `WaitingForNodePool`.

For a cluster that already exists, read its id from [`latitudesh_lks_clusters`](../data-sources/lks_clusters.md) filtered on `status = "ready"`, as the example does. For a cluster created in the **same** configuration, reference the resource and order the read after its node pools:

```terraform
ephemeral "latitudesh_lks_kubeconfig" "new_cluster" {
  cluster_id = latitudesh_lks.example.id

  depends_on = [latitudesh_lks_node_pool.example]
}
```

That is enough: a pool's create waits for its nodes and then for the cluster to reach `ready`. One caveat, observed live: the platform can report `ready` a few seconds **before** it publishes the kubeconfig, and this resource performs a single read, with no retry. An open that lands inside that window fails the run with the 409, but fails it *cheaply*: the cluster and its pools were already created and recorded, nothing is tainted, and running apply again reads the now-published document.

## Example Usage

```terraform
# Reads a cluster's kubeconfig for the duration of the run — into memory, never
# into state or a plan file. Requires Terraform 1.10 or later.
data "latitudesh_lks_clusters" "kubeconfig_source" {
  project = "<project-id-or-slug>"
  status  = "ready"
}

ephemeral "latitudesh_lks_kubeconfig" "example" {
  cluster_id = data.latitudesh_lks_clusters.kubeconfig_source.clusters[0].id
}

# Creating the cluster in the same configuration? Reference it —
# `cluster_id = latitudesh_lks.example.id` — and add
# `depends_on = [latitudesh_lks_node_pool.example]`: the kubeconfig exists only
# once the cluster is ready, and the cluster is ready only once a pool has nodes.
#
# The resource exposes the document and nothing else. Pull fields out with
# yamldecode(); see "Using it with the kubernetes provider" on this page.
```

## Using it with the kubernetes provider

The endpoint exposes one thing — the document — and so does this resource. It is deliberately not decomposed into `host` / `cluster_ca_certificate` / `client_certificate` / `token` the way some providers do, because the API does not specify which authentication method a cluster's kubeconfig carries, and a schema promising both would be publishing a guess.

Pull the pieces out in configuration with [`yamldecode`](https://developer.hashicorp.com/terraform/language/functions/yamldecode), where the shape you are assuming is written down where you can see it:

```terraform
locals {
  # Ephemeral values are allowed in locals as long as the local itself is only
  # used somewhere ephemeral values are allowed — a provider block is.
  kubeconfig = yamldecode(ephemeral.latitudesh_lks_kubeconfig.example.kubeconfig)
}

provider "kubernetes" {
  host                   = local.kubeconfig.clusters[0].cluster.server
  cluster_ca_certificate = base64decode(local.kubeconfig.clusters[0].cluster["certificate-authority-data"])

  # Your cluster's document decides which of these two applies. Read it once
  # before committing either:
  #   terraform console
  #   > yamldecode(...).users[0].user
  client_certificate = base64decode(local.kubeconfig.users[0].user["client-certificate-data"])
  client_key         = base64decode(local.kubeconfig.users[0].user["client-key-data"])
  # token            = local.kubeconfig.users[0].user.token
}
```

Indexing `[0]` is safe only for a single-cluster document. Resolve through `current-context` if you want to be strict about it.

Any key ending in `-data` is base64 by the kubeconfig format's own definition, which is why those three are wrapped in `base64decode`; `token` is not.

To write the raw document out instead, pair it with a `local_sensitive_file` — never `local_file`. Note that this puts a non-expiring cluster-admin credential on disk, which is the thing the ephemeral resource exists to avoid.

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `cluster_id` (String) ID of the cluster to read the kubeconfig from.

### Read-Only

- `kubeconfig` (String, Sensitive) The kubeconfig document, verbatim as the API returns it. This is the whole of what the endpoint exposes; use `yamldecode()` to pull fields out of it.
