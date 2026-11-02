# The Kubernetes versions available for LKS clusters.
data "latitudesh_lks_versions" "all" {}

output "lks_versions_for_creation" {
  value = [
    for version in data.latitudesh_lks_versions.all.versions :
    version.version if version.available_for_creation
  ]
}

output "lks_versions_for_upgrade" {
  value = [
    for version in data.latitudesh_lks_versions.all.versions :
    version.version if version.available_for_upgrade
  ]
}

output "lks_default_version" {
  value = data.latitudesh_lks_versions.all.default_version
}
