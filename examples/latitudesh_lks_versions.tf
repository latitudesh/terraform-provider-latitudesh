# The Kubernetes versions the platform offers, and the authoritative source for
# latitudesh_lks's `kubernetes_version`. A version can be listed but usable
# only for upgrades, or only for new clusters, so read the flags rather than
# taking the whole list.
data "latitudesh_lks_versions" "all" {}

# What a new cluster can be created on.
output "lks_versions_for_creation" {
  value = [
    for version in data.latitudesh_lks_versions.all.versions :
    version.version if version.available_for_creation
  ]
}

# What an existing cluster can be upgraded to. Upgrades are in place; a
# downgrade is not allowed, and latitudesh_lks rejects one at plan time. A node
# pool's version may never be newer than its cluster's — a pool that omits its
# version tracks the cluster's, which keeps that true by construction.
output "lks_versions_for_upgrade" {
  value = [
    for version in data.latitudesh_lks_versions.all.versions :
    version.version if version.available_for_upgrade
  ]
}

# The platform's own default — a reasonable choice when you have no opinion.
output "lks_default_version" {
  value = data.latitudesh_lks_versions.all.default_version
}
