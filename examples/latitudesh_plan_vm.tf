data "latitudesh_plan_vm" "small" {
  slug = "vm-small"
}

output "plan_vm_available_operating_systems" {
  value = data.latitudesh_plan_vm.small.available_operating_systems
}

# `project` is inherited from the provider block:
#   provider "latitudesh" { project = "<project id or slug>" }
resource "latitudesh_virtual_machine" "example" {
  name             = "vm-01"
  plan             = data.latitudesh_plan_vm.small.slug
  site             = "ASH"
  operating_system = data.latitudesh_plan_vm.small.available_operating_systems[0]
}
