terraform {
  required_version = ">= 1.10"

  backend "s3" {
    bucket = "your-state-bucket"
    key    = "infra/terraform.tfstate"

    # Standard buckets use AWS-style region slugs (e.g. us-east-1).
    region = "us-east-1"

    endpoints = {
      s3 = "https://objects.us-east-1.storage.sh"
    }

    use_path_style              = true
    skip_credentials_validation = true
    skip_metadata_api_check     = true
    skip_region_validation      = true
    skip_requesting_account_id  = true

    # State locking is not available on Standard buckets.
    # Use a High-performance bucket if you need locking.
  }
}
