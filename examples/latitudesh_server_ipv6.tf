# Example showing how to access both IPv4 and IPv6 addresses
resource "latitudesh_server" "server_with_ipv6" {
  hostname         = "ipv6-example.latitude.sh"
  operating_system = "ubuntu_22_04_x64_lts"
  plan             = data.latitudesh_plan.plan.slug
  project          = latitudesh_project.project.id
  site             = data.latitudesh_region.region.slug
  ssh_keys         = [latitudesh_ssh_key.ssh_key.id]
}

# Output both IPv4 and IPv6 addresses
output "server_ipv4_address" {
  description = "The server's primary IPv4 address"
  value       = latitudesh_server.server_with_ipv6.primary_ipv4
}

output "server_ipv6_address" {
  description = "The server's primary IPv6 address"
  value       = latitudesh_server.server_with_ipv6.primary_ipv6
}

# Example of using both addresses in a combined output
output "server_addresses" {
  description = "Both IPv4 and IPv6 addresses for the server"
  value = {
    ipv4 = latitudesh_server.server_with_ipv6.primary_ipv4
    ipv6 = latitudesh_server.server_with_ipv6.primary_ipv6
  }
} 