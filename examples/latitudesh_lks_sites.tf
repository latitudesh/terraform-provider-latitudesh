# The sites an LKS cluster can be deployed to.
data "latitudesh_lks_sites" "all" {}

output "lks_site_slugs" {
  value = [for site in data.latitudesh_lks_sites.all.sites : site.slug]
}

# Pair with latitudesh_lks_plans to keep only sites where a plan has stock.
data "latitudesh_lks_plans" "per_site" {
  for_each = toset([for site in data.latitudesh_lks_sites.all.sites : site.slug])

  site = each.value
}

output "lks_sites_with_capacity" {
  value = [
    for slug, plans in data.latitudesh_lks_plans.per_site :
    slug if length(plans.stock) > 0
  ]
}
