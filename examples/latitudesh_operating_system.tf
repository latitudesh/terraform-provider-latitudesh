data "latitudesh_operating_system" "ubuntu" {
  slug = "ubuntu_22_04_x64_lts"
}

output "operating_system_provisionable_on" {
  value = data.latitudesh_operating_system.ubuntu.provisionable_on
}

resource "latitudesh_server" "web" {
  project          = "proj_..."
  hostname         = "web-01"
  plan             = "c2-small-x86"
  site             = "SAO2"
  operating_system = data.latitudesh_operating_system.ubuntu.slug
}
