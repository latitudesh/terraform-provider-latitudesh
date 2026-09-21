# Reads a cluster's kubeconfig for the duration of the run — into memory, never
# into state or a plan file. Requires Terraform 1.10 or later.
data "latitudesh_lks_clusters" "kubeconfig_source" {
  project = "<project-id-or-slug>"
  status  = "ready"
}

ephemeral "latitudesh_lks_kubeconfig" "example" {
  cluster_id = data.latitudesh_lks_clusters.kubeconfig_source.clusters[0].id
}

# Creating the cluster in the same configuration? Reference the resource
# instead — `cluster_id = latitudesh_lks.example.id` — so the read is ordered
# after its create returns, which is only once the control plane is ready.
#
# The resource exposes the document and nothing else. Pull fields out with
# yamldecode(); see "Using it with the kubernetes provider" on this page.
