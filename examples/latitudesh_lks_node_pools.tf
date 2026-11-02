# Every node pool in a cluster, including pools created outside Terraform.
data "latitudesh_lks_clusters" "pool_source" {
  project = "<project-id-or-slug>"
  status  = "ready"
}

data "latitudesh_lks_node_pools" "by_cluster" {
  cluster_id = data.latitudesh_lks_clusters.pool_source.clusters[0].id
}

output "lks_pools_still_working" {
  value = [
    for pool in data.latitudesh_lks_node_pools.by_cluster.node_pools :
    pool.id if pool.status != "ready"
  ]
}

# A pool with no labels reports `labels` as null, so index inside try().
output "lks_pools_by_workload" {
  value = {
    for pool in data.latitudesh_lks_node_pools.by_cluster.node_pools :
    pool.id => try(pool.labels["workload"], "unlabelled")
  }
}
