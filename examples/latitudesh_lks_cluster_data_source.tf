# Look up one cluster by id.
data "latitudesh_lks_clusters" "lookup_source" {
  project = "<project-id-or-slug>"
  status  = "ready"
}

data "latitudesh_lks_cluster" "by_id" {
  id = data.latitudesh_lks_clusters.lookup_source.clusters[0].id
}

output "lks_cluster_endpoint" {
  value = data.latitudesh_lks_cluster.by_id.control_plane_endpoint
}
