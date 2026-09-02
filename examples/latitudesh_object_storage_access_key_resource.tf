# Long-lived access key. The secret is returned by the API only once, at
# creation, and is stored in the Terraform state as a sensitive value.
resource "latitudesh_object_storage_access_key" "app" {
  name          = "app-writer"
  project       = "proj_..."
  storage_class = "high_performance"
  region        = "ASH"

  access_scope = "limited_access"
  bucket_permissions = [
    {
      bucket_id  = latitudesh_object_storage.backups.id
      permission = "rw"
    },
  ]
}

output "app_access_key_id" {
  value = latitudesh_object_storage_access_key.app.access_key_id
}

output "app_secret" {
  value     = latitudesh_object_storage_access_key.app.secret_access_key
  sensitive = true
}

# Variant: with pgp_key the plaintext secret never touches the state — only a
# PGP-encrypted copy is stored. Decrypt it with the matching private key:
#   terraform output -raw ci_encrypted_secret | base64 --decode | gpg --decrypt
resource "latitudesh_object_storage_access_key" "ci" {
  name          = "ci-reader"
  project       = "proj_..."
  storage_class = "standard"
  region        = "ASH"

  pgp_key = file("${path.module}/ci-public-key.asc")
}

output "ci_encrypted_secret" {
  value = latitudesh_object_storage_access_key.ci.encrypted_secret
}
