resource "latitudesh_server" "server" {
  hostname         = "terraform-latitudesh"
  operating_system = "ubuntu_22_04_x64_lts"
  plan             = data.latitudesh_plan.plan.slug
  project          = latitudesh_project.project.id      # You can use the project id or slug
  site             = data.latitudesh_region.region.slug # You can use the site id or slug
  ssh_keys         = [latitudesh_ssh_key.ssh_key.id]
  billing          = "hourly" # hourly, monthly (default) or yearly (reserved projects)
  allow_reinstall  = true     # Reinstall on OS/hostname/SSH/user_data/RAID/disk_layout/iPXE changes; defaults to false (hostname then PATCHes in place)

  # Custom disk layout (alternative to `raid`, mutually exclusive with `raid` and `ipxe`).
  # One group per disk role; the OS filesystem is always ext4 (managed by the API).
  # disk_layout = [
  #   { count = 2, role = "os", raid_level = "raid-1" },
  #   { count = 2, role = "storage", raid_level = "raid-0", mount_point = "/data" },
  # ]

  timeouts = {
    create = "45m" # Timeout for server creation (default: 30m)
    update = "60m" # Timeout for server updates/reinstalls (default: 30m)
  }
}

output "server_state" {
  value = latitudesh_server.server
}
