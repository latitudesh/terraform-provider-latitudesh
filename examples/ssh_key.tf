resource "latitude_ssh_key" "ssh_key" {
  project_id = latitude_project.project.id
  name = "bar"
  public_key = var.ssh_public_key
}