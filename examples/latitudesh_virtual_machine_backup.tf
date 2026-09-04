resource "latitudesh_virtual_machine" "bastion" {
  name             = "bastion"
  plan             = "vm-small"
  operating_system = "ubuntu_24_04_x64_lts"
}

resource "latitudesh_virtual_machine_backup" "bastion" {
  virtual_machine = latitudesh_virtual_machine.bastion.id
}

data "latitudesh_virtual_machine_backup" "bastion" {
  id = latitudesh_virtual_machine_backup.bastion.id
}
