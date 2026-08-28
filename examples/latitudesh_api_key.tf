resource "latitudesh_api_key" "ci" {
  name       = "ci-deploy-key"
  read_only  = false
  allowed_ips = ["203.0.113.10/32"]
}

data "latitudesh_api_key" "ci" {
  name = latitudesh_api_key.ci.name
}
