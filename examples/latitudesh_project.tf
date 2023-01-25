resource "latitudesh_project" "project" {
  name        = "Project name"
  description = "Description of project"
  environment = "Development" # Development, Production or Staging
}
