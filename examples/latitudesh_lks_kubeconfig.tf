# Reads a cluster's kubeconfig into memory for the duration of the run — never
# into state or a plan file. Requires Terraform 1.10 or later.
data "latitudesh_lks_clusters" "kubeconfig_source" {
  project = "<project-id-or-slug>"
  status  = "ready"
}

ephemeral "latitudesh_lks_kubeconfig" "example" {
  cluster_id = data.latitudesh_lks_clusters.kubeconfig_source.clusters[0].id
}
