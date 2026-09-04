data "latitudesh_virtual_machine_restore" "example" {
  id = "vmr_..."
}

output "restored_virtual_machine_id" {
  value = data.latitudesh_virtual_machine_restore.example.virtual_machine_id
}

output "restore_status" {
  value = data.latitudesh_virtual_machine_restore.example.status
}
