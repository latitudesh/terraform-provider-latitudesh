resource "latitudesh_project" "managed_database" {
  name = "managed-database-example"
}

# The application server that connects to the database.
resource "latitudesh_server" "app" {
  hostname         = "app-01"
  project          = latitudesh_project.managed_database.id
  plan             = "c3-small-x86"
  site             = "DAL"
  operating_system = "ubuntu_22_04_x64_lts"
}

resource "latitudesh_managed_database" "example" {
  project = latitudesh_project.managed_database.id
  name    = "app-db"
  region  = "DAL"
  plan    = "db.psql.small" # Changing the plan replaces the database and destroys its data.
  engine  = "postgres"
  billing = "monthly" # Set at create time; it can't be changed later.

  # Only the app server may connect.
  trusted_sources = ["${latitudesh_server.app.primary_ipv4}/32"]

  # Changing parameters triggers a rolling restart.
  parameters = {
    max_connections = "200"
  }

  lifecycle {
    prevent_destroy = true
  }
}

output "managed_database_url" {
  value     = latitudesh_managed_database.example.credentials.url
  sensitive = true
}
