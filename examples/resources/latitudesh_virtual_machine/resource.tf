resource "latitudesh_virtual_machine" "example" {
  name             = "bastion"
  plan             = "vm-shared-1c-1g" # VM plan slug or ID
  operating_system = "ubuntu-24-04"    # Optional; API picks a sensible default if unset
  ssh_keys         = [latitudesh_ssh_key.ssh_key.id]

  # Optional: falls back to the provider-level project if unset.
  # project = "proj_abc123"
}
