# The sites an LKS cluster can be deployed to, and the authoritative source for
# latitudesh_lks's `site`. LKS runs in its own subset of locations, so
# latitudesh_region is not a substitute and there is no fixed list to hardcode.
data "latitudesh_lks_sites" "all" {}

output "lks_site_slugs" {
  value = [for site in data.latitudesh_lks_sites.all.sites : site.slug]
}

output "lks_sites_by_country" {
  value = {
    for site in data.latitudesh_lks_sites.all.sites :
    site.slug => "${site.name} (${site.country})"
  }
}

# A site is only usable if some plan has capacity there — pair this with
# latitudesh_lks_plans before committing to one.
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
