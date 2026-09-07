# Every Ready backup of one virtual machine, newest first.
data "latitudesh_virtual_machine_backups" "by_vm" {
  virtual_machine = "vm_..." # virtual machine ID
  status          = "Ready"
}

# The most recent Ready backup is backups[0]; it is what a new VM restores from.
output "latest_ready_backup_id" {
  value = length(data.latitudesh_virtual_machine_backups.by_vm.backups) > 0 ? data.latitudesh_virtual_machine_backups.by_vm.backups[0].id : null
}
