# Every LKS cluster in a project. `status` is optional: drop it to see clusters
# still provisioning, or being deleted, alongside the ready ones.
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
