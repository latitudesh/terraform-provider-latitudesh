resource "latitudesh_elastic_ip" "example" {
  server_id = latitudesh_server.server.id
}

output "elastic_ip_address" {
  value = latitudesh_elastic_ip.example.address
}
