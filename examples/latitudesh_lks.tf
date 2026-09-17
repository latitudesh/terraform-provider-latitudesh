# NOTE: "ASH" and the kubernetes_version below are not confirmed against this
# account's GET /lks/sites and GET /lks/available_versions — check both
# before applying. See the provider's scaffold handoff for why.
resource "latitudesh_project" "lks" {
  name = "lks-list-example"
}

resource "latitudesh_lk" "list_example" {
  project            = latitudesh_project.lks.id
  name               = "example-cluster"
  site               = "ASH"
  kubernetes_version = "1.31.0"
}

data "latitudesh_lks" "by_project" {
  project = latitudesh_project.lks.id
  status  = "ready"

  depends_on = [latitudesh_lk.list_example]
}
