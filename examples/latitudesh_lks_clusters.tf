# The LKS clusters in a project. `status` is optional.
data "latitudesh_lks_clusters" "in_project" {
  project = "<project-id-or-slug>"
  status  = "ready"
}

output "lks_cluster_endpoints" {
  value = {
    for cluster in data.latitudesh_lks_clusters.in_project.clusters :
    cluster.name => cluster.control_plane_endpoint
  }
}
