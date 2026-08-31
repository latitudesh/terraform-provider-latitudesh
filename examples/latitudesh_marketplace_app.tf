data "latitudesh_marketplace_app" "by_slug" {
  slug = "wordpress"
}

output "marketplace_app_version" {
  value = data.latitudesh_marketplace_app.by_slug.version
}

output "marketplace_app_compatible_plans" {
  value = data.latitudesh_marketplace_app.by_slug.compatible_plans
}

data "latitudesh_marketplace_app" "by_name" {
  name = "WordPress"
}

data "latitudesh_marketplace_app" "by_id" {
  id = "mkapp_..."
}
