resource "latitudesh_elastic_ip" "example" {
  server_id = var.elastic_ip_server_id
}

output "elastic_ip_address" {
  value = latitudesh_elastic_ip.example.address
}
