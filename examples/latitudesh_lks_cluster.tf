data "latitudesh_lks_sites" "for_cluster" {}

data "latitudesh_lks_versions" "for_cluster" {}

locals {
  site = data.latitudesh_lks_sites.for_cluster.sites[0].slug
}

# Plans in stock at the chosen site; `stock` maps plan slug -> available nodes.
data "latitudesh_lks_plans" "for_cluster" {
  site = local.site
}

resource "latitudesh_project" "lks" {
  name = "lks-example"
}

resource "latitudesh_lks_cluster" "example" {
  project            = latitudesh_project.lks.id
  name               = "example-cluster"
  site               = local.site
  kubernetes_version = data.latitudesh_lks_versions.for_cluster.default_version
  description        = "managed by terraform"

  timeouts = {
    create = "30m"
    update = "60m"
    delete = "30m"
  }
}

# A cluster needs at least one node pool to become ready.
resource "latitudesh_lks_node_pool" "example" {
  cluster_id = latitudesh_lks_cluster.example.id
  node_count = 2
  plan       = keys(data.latitudesh_lks_plans.for_cluster.stock)[0]

  # Bare metal nodes take a while to provision.
  timeouts = {
    create = "90m"
    update = "90m"
    delete = "30m"
  }
}
