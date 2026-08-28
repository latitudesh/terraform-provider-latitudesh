resource "latitudesh_api_key" "ci" {
  name        = "ci-deploy-key"
  read_only   = false
  allowed_ips = ["203.0.113.10", "198.51.100.0/24"]
}

data "latitudesh_api_key" "by_id" {
  id = latitudesh_api_key.ci.id
}

output "ci_api_key_token" {
  value     = latitudesh_api_key.ci.token
  sensitive = true
}

output "ci_api_key_last_slice" {
  value = data.latitudesh_api_key.by_id.token_last_slice
}
