resource "latitudesh_server" "server" {
  hostname         = "terraform-latitudesh"
  operating_system = "ubuntu_22_04_x64_lts"
  plan             = data.latitudesh_plan.plan.slug
  project          = latitudesh_project.project.id      # You can use the project id or slug
  site             = data.latitudesh_region.region.slug # You can use the site id or slug
  ssh_keys         = [latitudesh_ssh_key.ssh_key.id]
  billing          = "hourly"                           # Required for on_demand projects: hourly, monthly
  allow_reinstall  = true                               # Allow reinstall for OS/SSH/UserData/RAID/iPXE changes

  timeouts {
    create = "45m"  # Timeout for server creation (default: 30m)
    update = "60m"  # Timeout for server updates/reinstalls (default: 30m)
  }
}

output "server_state" {
  value = latitudesh_server.server
}
