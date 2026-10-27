# Attach a volume to a server, all in one apply. Both must live in the same
# project and site, and the server must be eligible for storage (cabled for a
# bond). Deploying it onto a public network guarantees that, because that
# deploy only picks servers that pass the same check as the attachment.
resource "latitudesh_public_network" "volume_host" {
  project = "proj_..."
  site    = "CHI"
  size    = 28
}

resource "latitudesh_server" "volume_host" {
  hostname          = "db-01"
  operating_system  = "ubuntu_24_04_x64_lts"
  plan              = "c2-small-x86"
  project           = "proj_..."
  site              = "CHI"
  public_network_id = latitudesh_public_network.volume_host.id
}

resource "latitudesh_block_storage" "attached" {
  project    = "proj_..." # project ID or slug
  name       = "db-data"
  region     = "CHI"
  size_in_gb = 500
}

resource "latitudesh_block_storage_attachment" "example" {
  block_storage_id = latitudesh_block_storage.attached.id
  server_id        = latitudesh_server.volume_host.id
}

# The server is deployed before the volume is attached, so its OS is not set up
# for the volume: configure the storage VLAN and the NVMe-TCP connection from
# these values, or reinstall the server once the volume is attached.
output "block_storage_network" {
  value = latitudesh_block_storage_attachment.example.storage_network
}
