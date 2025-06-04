resource "latitudesh_project" "project" {
  name             = "Project name"
  description      = "Description of project"
  environment      = "Development" # Development, Production or Staging
  provisioning_type = "on_demand"   # on_demand or reserved
}
