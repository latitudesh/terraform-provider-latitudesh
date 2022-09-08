resource "latitude_server" "server" {
  hostname = "foo"
  operating_system = "ubuntu_20_04_x64_lts"
  plan = "c2.small.x86"//data.latitude_plan.plan.name
  project_id = "4070"//latitude_project.project.id
  site = "ASH"//data.latitude_region.region.slug
}