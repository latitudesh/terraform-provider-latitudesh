# List every published marketplace app.
data "latitudesh_marketplace_apps" "all" {}

output "marketplace_app_slugs" {
  value = [for app in data.latitudesh_marketplace_apps.all.apps : app.slug]
}

# Filter the catalog by category (case-insensitive).
data "latitudesh_marketplace_apps" "cms" {
  category = "cms"
}

output "cms_apps" {
  value = { for app in data.latitudesh_marketplace_apps.cms.apps : app.slug => app.name }
}
