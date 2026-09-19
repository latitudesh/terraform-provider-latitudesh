# Looks up one cluster by id — the only selector the API offers. Take the id
# from the cluster list, from a latitudesh_lks resource in the same
# configuration, or paste it in.
data "latitudesh_lks_clusters" "lookup_source" {
  project = "<project-id-or-slug>"
  status  = "ready"
}

data "latitudesh_lks" "by_id" {
  id = data.latitudesh_lks_clusters.lookup_source.clusters[0].id
}

output "lks_cluster_endpoint" {
  value = data.latitudesh_lks.by_id.control_plane_endpoint
}
