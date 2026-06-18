resource "latitudesh_ssh_key" "ssh_key" {
  name       = "bastion-key"
  public_key = "ssh-ed25519 AAAA..." # Your public key
}

# A small virtual machine intended to act as an SSH bastion / OpenVPN endpoint.
# OpenVPN install and tunnelling are configured separately, after the VM exists.
resource "latitudesh_virtual_machine" "bastion" {
  name             = "bastion"
  plan             = "vm-small" # VM plan slug or ID
  operating_system = "ubuntu_24_04_x64_lts"
  project          = latitudesh_project.project.id # ID or slug
  ssh_keys         = [latitudesh_ssh_key.ssh_key.id]
}

# The public entry point for SSH tunnelling / VPN.
output "bastion_ip" {
  value = latitudesh_virtual_machine.bastion.primary_ipv4
}

output "bastion_ssh_user" {
  value = latitudesh_virtual_machine.bastion.ssh_user
}
