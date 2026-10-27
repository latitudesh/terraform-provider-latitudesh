# Every block storage volume in one project, newest first.
data "latitudesh_block_storages" "by_project" {
  project = "proj_..." # project ID or slug; omit to list every volume in the team
}

output "latest_volume_id" {
  value = length(data.latitudesh_block_storages.by_project.volumes) > 0 ? data.latitudesh_block_storages.by_project.volumes[0].id : null
}
