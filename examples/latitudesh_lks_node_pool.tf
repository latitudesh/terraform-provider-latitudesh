data "latitudesh_lks_sites" "for_pool" {}

data "latitudesh_lks_versions" "for_pool" {}

locals {
  pool_site = data.latitudesh_lks_sites.for_pool.sites[0].slug
}

data "latitudesh_lks_plans" "for_pool" {
  site = local.pool_site
}

resource "latitudesh_project" "lks_node_pool" {
  name = "lks-node-pool-example"
}

resource "latitudesh_lks_cluster" "node_pool_example" {
  project            = latitudesh_project.lks_node_pool.id
  name               = "example-cluster"
  site               = local.pool_site
  kubernetes_version = data.latitudesh_lks_versions.for_pool.default_version
}

# Untainted general-purpose pool; every cluster needs at least one.
resource "latitudesh_lks_node_pool" "general" {
  cluster_id = latitudesh_lks_cluster.node_pool_example.id
  node_count = 1
  plan       = keys(data.latitudesh_lks_plans.for_pool.stock)[0]

  labels = {
    "workload" = "general"
  }
}

# A tainted pool is reserved for workloads that tolerate the taint. Referencing
# the cluster's kubernetes_version keeps the pool on the control-plane version.
resource "latitudesh_lks_node_pool" "gpu" {
  cluster_id         = latitudesh_lks_cluster.node_pool_example.id
  node_count         = 2
  plan               = keys(data.latitudesh_lks_plans.for_pool.stock)[0]
  kubernetes_version = latitudesh_lks_cluster.node_pool_example.kubernetes_version

  labels = {
    "workload" = "gpu"
  }

  taints = [{
    key    = "dedicated"
    value  = "gpu"
    effect = "NoSchedule"
  }]

  # Bare metal nodes take a while to provision.
  timeouts = {
    create = "90m"
    update = "90m"
    delete = "30m"
  }
}
