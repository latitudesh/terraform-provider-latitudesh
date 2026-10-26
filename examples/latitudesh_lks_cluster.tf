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
