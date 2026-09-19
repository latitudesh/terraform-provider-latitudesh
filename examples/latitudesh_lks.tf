# A cluster is a control plane plus its required first node pool, created
# together. Site, Kubernetes version and plan all come from lookups — none of
# them is guessable, and the lookups keep this runnable on any account.
data "latitudesh_lks_sites" "for_cluster" {}

data "latitudesh_lks_versions" "for_cluster" {}

locals {
  # Pick a site deliberately in real configuration; the first one keeps this
  # example runnable on any account.
  site = data.latitudesh_lks_sites.for_cluster.sites[0].slug
}

# Filtering by site narrows the catalog to plans that can actually be built
# there right now, and fills `stock` with slug -> nodes available.
data "latitudesh_lks_plans" "for_cluster" {
  site = local.site
}

resource "latitudesh_project" "lks" {
  name = "lks-example"
}

resource "latitudesh_lks" "example" {
  project            = latitudesh_project.lks.id
  name               = "example-cluster"
  site               = local.site
  kubernetes_version = data.latitudesh_lks_versions.for_cluster.default_version

  # Note: `description` cannot be removed once set — the API requires a
  # non-empty value and ignores a null. Change it instead of clearing it.
  description = "managed by terraform"

  # kubernetes_version is omitted on the pool, so it tracks the control
  # plane: upgrading the cluster upgrades this pool in the same apply. Set it
  # explicitly to pin the pool instead.
  default_node_pool = {
    node_count = 2
    plan       = keys(data.latitudesh_lks_plans.for_cluster.stock)[0]

    labels = {
      "workload" = "general"
    }

    # `NoSchedule` and `NoExecute` are rejected on the default pool: the
    # cluster's own system workloads have nowhere else to run, and a hard
    # taint would leave it provisioning forever. Put those on an additional
    # latitudesh_lks_node_pool instead.
    taints = [{
      key    = "dedicated"
      value  = "general"
      effect = "PreferNoSchedule"
    }]
  }

  # Bare metal: create covers the cluster and its first pool coming up.
  timeouts = {
    create = "90m"
    update = "90m"
    delete = "30m"
  }
}
