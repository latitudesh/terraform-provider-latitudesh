# Every node pool in a cluster — the cluster's own default_node_pool, any
# latitudesh_lks_node_pool you manage, and anything created outside Terraform.
data "latitudesh_lks_clusters" "pool_source" {
  project = "<project-id-or-slug>"
  status  = "ready"
}

data "latitudesh_lks_node_pools" "by_cluster" {
  cluster_id = data.latitudesh_lks_clusters.pool_source.clusters[0].id
}

# `status` is what says whether a pool has settled. Do not compare ready_nodes
# against node_count for this: the platform does not always report the count,
# and it comes back null — not zero — on pools it does not track it for.
output "lks_pools_still_working" {
  value = [
    for pool in data.latitudesh_lks_node_pools.by_cluster.node_pools :
    pool.id if pool.status != "ready"
  ]
}

# Labels and taints are what tell one pool from another.
output "lks_pools_by_workload" {
  value = {
    for pool in data.latitudesh_lks_node_pools.by_cluster.node_pools :
    pool.id => lookup(pool.labels, "workload", "unlabelled")
  }
}
