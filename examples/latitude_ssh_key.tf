resource "latitudesh_ssh_key" "ssh_key" {
  project    = latitudesh_project.project.id
  name       = "John's Key"
  public_key = var.ssh_public_key
}
