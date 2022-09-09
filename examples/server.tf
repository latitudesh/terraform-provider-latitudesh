resource "latitude_server" "server" {
  hostname = "foo"
  operating_system = "ubuntu_20_04_x64_lts"
  plan = data.latitude_plan.plan.name
  project_id = latitude_project.project.id
  site = data.latitude_region.region.slug
  ssh_keys = [latitude_ssh_key.ssh_key.id]
}