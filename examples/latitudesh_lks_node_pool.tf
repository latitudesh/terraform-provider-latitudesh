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
