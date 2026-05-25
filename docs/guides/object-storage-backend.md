---
page_title: "Using Latitude.sh Object Storage as a Terraform Remote State Backend"
---

# Using Latitude.sh Object Storage as a Terraform Remote State Backend

Latitude.sh Object Storage is S3-compatible and works as a Terraform remote state backend through Terraform's built-in [`s3` backend](https://developer.hashicorp.com/terraform/language/backend/s3). This guide shows the configuration for each bucket tier and the trade-offs between them.

For bucket creation, access keys, and endpoint lookup, see the [Object Storage documentation](https://www.latitude.sh/docs/storage/object-storage).

## Bucket tiers

Two tiers are available, with different state-locking behavior:

| Tier             | Endpoint pattern                 | State locking                  | Recommended for                                       |
|------------------|----------------------------------|--------------------------------|-------------------------------------------------------|
| Standard         | `objects.<region>.storage.sh`    | Not available                  | Single-user setups, manually coordinated runs         |
| High-performance | `objects.<slug>.storage.sh`      | Yes (`use_lockfile = true`)    | CI, teams, anywhere concurrent runs are possible      |

If multiple operators or pipelines may run Terraform against the same state, choose a **High-performance** bucket so state locking is available.

## Prerequisites

- A bucket created in the [Latitude.sh dashboard](https://latitude.sh/dashboard).
- An access key and secret for that bucket.
- **Terraform 1.10** or later (required for `use_lockfile`).

## Authentication

Terraform's `s3` backend reads credentials from the standard AWS environment variables:

```sh
export AWS_ACCESS_KEY_ID=<latitudesh-object-storage-access-key>
export AWS_SECRET_ACCESS_KEY=<latitudesh-object-storage-secret-key>
```

The `AWS_*` names come from Terraform's built-in `s3` backend — the credentials themselves are your Latitude.sh Object Storage keys.

## Configuration

### Standard tier (no locking)

```terraform
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
```

~> **Warning:** Standard buckets do not currently support state locking. Concurrent `terraform apply` runs against the same state can corrupt it silently. Coordinate runs out-of-band, or use a High-performance bucket.

### High-performance tier (with locking)

```terraform
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
```

`use_lockfile = true` enables native S3 state locking using conditional writes. Terraform creates a `<state>.tflock` object during operations and removes it when finished. No DynamoDB table is required.

## Initialize the backend

```sh
terraform init
```

If you are migrating from a local backend, use `-migrate-state`:

```sh
terraform init -migrate-state
```

## A note on region values

Region naming differs from the rest of the Latitude.sh platform — do not paste a `latitudesh_region.slug` value here.

- **Standard** buckets use AWS-style region slugs (e.g. `us-east-1`).
- **High-performance** buckets use Latitude.sh location slugs (e.g. `nyc`).

The region must match the value used by the storage endpoint shown on the bucket detail page in the dashboard.

## Troubleshooting

**`SignatureDoesNotMatch`** — the `region` set in the backend block does not match what the endpoint expects. Confirm the region against the bucket detail page.

**`PreconditionFailed` on lock acquisition** — another Terraform process already holds the lock. Wait for it to finish, or run `terraform force-unlock <lock-id>` if you are certain the previous run is dead.

**State writes succeed but locking seems to do nothing** — verify that you are pointing at a High-performance bucket.
