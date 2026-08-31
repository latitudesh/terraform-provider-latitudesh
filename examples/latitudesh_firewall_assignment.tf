# Assign firewall to a server
resource "latitudesh_firewall_assignment" "web_assignment" {
  firewall_id = latitudesh_firewall.web_firewall.id
  server_id   = latitudesh_server.server.id
}

# Or assign it to a virtual machine (set exactly one of server_id /
# virtual_machine_id; a VM can be assigned to at most one firewall)
# resource "latitudesh_firewall_assignment" "vm_assignment" {
#   firewall_id        = latitudesh_firewall.web_firewall.id
#   virtual_machine_id = latitudesh_virtual_machine.vm.id
# }

# Import existing assignment using:
# import {
#   to = latitudesh_firewall_assignment.web_assignment
#   id = "fwasg_your_assignment_id"
# } 