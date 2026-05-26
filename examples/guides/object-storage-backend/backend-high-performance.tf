terraform {
  required_version = ">= 1.10"

  backend "s3" {
    bucket = "your-state-bucket"
    key    = "infra/terraform.tfstate"

    # High-performance buckets use Latitude.sh location slugs (e.g. nyc).
    region = "nyc"

    endpoints = {
      s3 = "https://objects.nyc.storage.sh"
    }

    use_path_style              = true
    skip_credentials_validation = true
    skip_metadata_api_check     = true
    skip_region_validation      = true
    skip_requesting_account_id  = true

    # Native state locking via S3 conditional writes (no DynamoDB required).
    # Requires Terraform 1.10+.
    use_lockfile = true
  }
}
