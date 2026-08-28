resource "latitudesh_baselines_preview" "ubuntu_default" {
  name             = "ubuntu-default"
  description      = "Default baseline for all servers"
  target_type      = "all_servers"
  operating_system = "ubuntu_24_04_x64_lts"
  ssh_key_ids      = ["sshk_xxxxx"]

  disk_layout = [
    {
      role  = "os"
      count = 1
    },
    {
      role        = "storage"
      count       = 2
      raid_level  = "raid-1"
      filesystem  = "ext4"
      mount_point = "/data"
    },
  ]
}

data "latitudesh_baselines_preview" "by_name" {
  name = "ubuntu-default"
}
