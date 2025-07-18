# Assign firewall to a server
resource "latitudesh_firewall_assignment" "web_assignment" {
  firewall_id = latitudesh_firewall.web_firewall.id
  server_id   = latitudesh_server.server.id
}

# Import existing assignment using:
# import {
#   to = latitudesh_firewall_assignment.web_assignment
#   id = "fwasg_your_assignment_id"
# } 