resource "latitudesh_member" "member" {
  first_name    = "Name"
  last_name     = "Surname"
  email         = "namesurname@example.com"
  role          = data.latitudesh_role.role.name
}
