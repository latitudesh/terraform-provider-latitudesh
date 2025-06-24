resource "latitudesh_ssh_key" "ssh_key" {
  # Note: project attribute removed in v2.0.0 (now team-scoped)
  # Only kept in test schemas for backwards compatibility
  name       = "John's Key"
  public_key = var.ssh_public_key
}
