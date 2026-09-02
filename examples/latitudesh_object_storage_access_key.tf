# The key exists only for the duration of the Terraform run: it is created
# when the ephemeral resource is opened and revoked when the run finishes,
# so the secret never persists in state or plan files.
ephemeral "latitudesh_object_storage_access_key" "ci" {
  name          = "ci-upload"
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

# Feed the temporary credentials to any S3-compatible provider or tool,
# for example an S3 provider pointed at the bucket's endpoint:
#
#   access_key = ephemeral.latitudesh_object_storage_access_key.ci.access_key_id
#   secret_key = ephemeral.latitudesh_object_storage_access_key.ci.secret_access_key
#   endpoint   = latitudesh_object_storage.backups.endpoint
