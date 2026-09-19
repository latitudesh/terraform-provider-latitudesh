# The machine catalog an LKS node pool is built from. `GET /plans/lks` is its
# own catalog: a server plan slug from latitudesh_plan is not interchangeable
# with one of these.
data "latitudesh_lks_sites" "for_plans" {}

locals {
  plans_site = data.latitudesh_lks_sites.for_plans.sites[0].slug
}

# Filtering by site is what makes this directly usable: `stock` comes back as
# plan slug -> nodes available there, already narrowed to plans that can be
# built right now. No loop and no filtering in configuration.
data "latitudesh_lks_plans" "at_site" {
  site = local.plans_site
}

output "lks_stock_at_site" {
  value = data.latitudesh_lks_plans.at_site.stock
}

output "lks_buildable_slugs_at_site" {
  value = keys(data.latitudesh_lks_plans.at_site.stock)
}

# An empty map means nothing can be built at that site today — the cluster
# would come up and its node pool would not.
output "lks_site_has_capacity" {
  value = length(data.latitudesh_lks_plans.at_site.stock) > 0
}

# Unfiltered, the catalog carries the full detail: specs, per-region stock
# levels, and `available_sites` — where a plan can run at all, a larger set
# than where it can run right now.
data "latitudesh_lks_plans" "all" {}

output "lks_plan_specs" {
  value = {
    for plan in data.latitudesh_lks_plans.all.plans :
    plan.slug => {
      cpu    = try(plan.specs.cpu.type, null)
      cores  = try(plan.specs.cpu.cores, null)
      memory = try(plan.specs.memory_total, null)
      gpu    = try(plan.specs.gpu.type, null)
    }
  }
}
