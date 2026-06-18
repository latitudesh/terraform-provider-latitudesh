resource "latitudesh_virtual_machine" "example" {
  name             = "bastion"
  plan             = "vm-shared-1c-1g" # VM plan slug or ID
  operating_system = "ubuntu_24_04_x64_lts"    # Optional; API picks a sensible default if unset
  ssh_keys         = [latitudesh_ssh_key.ssh_key.id]
}
