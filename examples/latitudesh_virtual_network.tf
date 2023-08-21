resource "latitudesh_virtual_network" "virtual_network" {
  description      = "Virtual Network description"
  site             = data.latitudesh_region.region.slug # You can use the site id or slug
  project          = latitudesh_project.project.id      # You can use the project id or slug
}
