# Example: Lookup tag by name
data "latitudesh_tag" "production" {
  name = "production"
}

# Example: Lookup tag by ID
data "latitudesh_tag" "by_id" {
  id = "tag_abc123"
}

# Example: Lookup tag by slug
data "latitudesh_tag" "by_slug" {
  slug = "production-env"
}

# Output the tag information
output "tag_color" {
  value = data.latitudesh_tag.production.color
}

output "tag_slug" {
  value = data.latitudesh_tag.production.slug
}

# Use the tag in a server resource
resource "latitudesh_server" "example" {
  hostname     = "example-server"
  project      = "my-project"
  plan         = "c2-small-x86"
  site         = "ASH"
  operating_system = "ubuntu_22_04_x64_lts"

  # Reference the tag by ID from the datasource
  tags = [data.latitudesh_tag.production.id]
}
