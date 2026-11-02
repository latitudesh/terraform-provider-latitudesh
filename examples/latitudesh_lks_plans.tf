data "latitudesh_lks_sites" "for_plans" {}

locals {
  plans_site = data.latitudesh_lks_sites.for_plans.sites[0].slug
}

# Filtered by site, `stock` maps plan slug -> nodes available there.
data "latitudesh_lks_plans" "at_site" {
  site = local.plans_site
}

output "lks_stock_at_site" {
  value = data.latitudesh_lks_plans.at_site.stock
}

output "lks_buildable_slugs_at_site" {
  value = keys(data.latitudesh_lks_plans.at_site.stock)
}

# Unfiltered: the whole catalog, with specs and per-region availability.
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
