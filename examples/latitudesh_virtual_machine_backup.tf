resource "latitudesh_virtual_machine" "backup_source" {
  name             = "backup-source"
  plan             = "vm-small"
  operating_system = "ubuntu_24_04_x64_lts"
}

resource "latitudesh_virtual_machine_backup" "backup_source" {
  virtual_machine = latitudesh_virtual_machine.backup_source.id
}

data "latitudesh_virtual_machine_backup" "backup_source" {
  id = latitudesh_virtual_machine_backup.backup_source.id
}
