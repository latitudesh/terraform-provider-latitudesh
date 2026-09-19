# Node pools beyond the first. The first one is required and lives inside
# latitudesh_lks as `default_node_pool`; use this resource to add capacity that
# should scale, upgrade or be destroyed independently of the cluster — a GPU
# pool, a batch pool, a pool on a different plan.
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

  default_node_pool = {
    node_count = 1
    plan       = keys(data.latitudesh_lks_plans.for_pool.stock)[0]
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

  # A hard taint belongs here rather than on `default_node_pool`, which has to
  # stay schedulable for the cluster's own components. Reserving a pool this
  # way is the usual reason to add one.
  taints = [{
    key    = "dedicated"
    value  = "gpu"
    effect = "NoSchedule"
  }]

  # Bare metal: create and scale wait until the platform reports the pool
  # settled (and for ready_nodes to reach node_count, when it reports one).
  timeouts = {
    create = "90m"
    update = "90m"
    delete = "30m"
  }
}

data "latitudesh_lks_node_pools" "example" {
  cluster_id = latitudesh_lks.node_pool_example.id

  depends_on = [latitudesh_lks_node_pool.gpu]
}
