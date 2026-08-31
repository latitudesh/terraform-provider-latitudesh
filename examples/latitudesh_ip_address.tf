data "latitudesh_ip_address" "by_id" {
  id = "ip_XXXXXXXX"
}

data "latitudesh_ip_address" "by_address" {
  address = "203.0.113.10"
}

data "latitudesh_ip_address" "management" {
  server_id = "sv_XXXXXXXX"
}

output "management_ip_address" {
  value = data.latitudesh_ip_address.management.address
}
