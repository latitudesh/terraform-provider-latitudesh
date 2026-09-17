# NOTE: "ASH" and the kubernetes_version below are not confirmed against this
# account's GET /lks/sites and GET /lks/available_versions — check both
# before applying. See the provider's scaffold handoff for why.
resource "latitudesh_project" "lk" {
  name = "lks-example"
}

resource "latitudesh_lk" "example" {
  project            = latitudesh_project.lk.id
  name               = "example-cluster"
  site               = "ASH"
  kubernetes_version = "1.31.0"
}

data "latitudesh_lk" "by_id" {
  id = latitudesh_lk.example.id
}
