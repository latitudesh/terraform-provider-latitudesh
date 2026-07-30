# Contributing to terraform-provider-latitudesh

Thank you for contributing to the Terraform Provider for Latitude.sh.

Whether you're reporting a bug, suggesting a feature, improving documentation, or submitting a pull request, your help is always appreciated.

## Ways to Contribute

### Report Bugs

If you find a bug, please [open an issue](https://github.com/latitudesh/terraform-provider-latitudesh/issues) and include:

- A clear description of the problem
- Steps to reproduce it (ideally with a minimal Terraform config)
- What you expected to happen
- What actually happened
- Your environment (Terraform version, OS, provider version)

### Suggest Features

Have an idea for a new feature or improvement? [Open an issue](https://github.com/latitudesh/terraform-provider-latitudesh/issues/new) and let’s discuss!

## Local Development Setup

1. **Fork and Clone the Repo**

```sh
git clone https://github.com/your-username/terraform-provider-latitudesh.git
cd terraform-provider-latitudesh
```

2. **Install Dependencies**

- [Go](https://go.dev/dl/) >= 1.23.x
- [Terraform](https://developer.hashicorp.com/terraform) >= 1.6

3. **Configure Terraform for Local Development**

Create or edit your `~/.terraformrc` file with a dev override pointing to your local build path:

```sh
provider_installation {
  dev_overrides {
    "local/iac/latitudesh" = "/Users/your-username/Developer/latitudesh/terraform-provider-latitudesh"
  }
  direct {}
}
```

This tells Terraform to use your local provider build instead of downloading it from the registry.

4. **Build the Provider**
```sh
make build
```

5. **Running tests**
```sh
make test
```

## Using Your Local Build

In your Terraform project, configure the provider like this:

```hcl
terraform {
  required_providers {
    latitudesh = {
      source = "local/iac/latitudesh"
    }
  }
}

provider "latitudesh" {}
```

Now run:

```sh
rm -rf .terraform .terraform.lock.hcl
terraform init
```

Terraform will pick up your local build through the override configured in `~/.terraformrc`.

> **Note:** Remove any `version` line from `required_providers` to ensure Terraform always uses your local build.

## Coverage of the Latitude.sh SDK

[`sdk-coverage.yaml`](sdk-coverage.yaml) is an **exception list**, not a work queue.
An SDK service group with no entry is one we intend to expose, and scaffolding gets
generated for it automatically — a resource, a data source, or both, according to the
CRUD shape derived from its method names.

So a new service group in an SDK bump does **not** fail CI. It produces a draft PR
instead, and reviewing that real code is where the keep-or-drop decision gets made:

- **Keep it** — merge, and the entry lands in `implemented_by`.
- **Drop it** — close the PR and record the decision here with a `ceiling` and a
  `rationale`, so it stops being regenerated.

`ceiling` caps how much is generated (`none`, or `datasource` to allow a data source
but no resource). `rationale` says why (`api_constraint`, `product_decision`,
`deprecated`). Both or neither — a cap without a reason is unauditable. The reason
matters because it decides whether the exclusion expires: an `api_constraint` is
re-checked every run and reported once the API grows past it, while a
`product_decision` is never revisited. Neither ever fails CI.

What `TestProviderSDKCoverage` does fail on is contradiction — most often a resource
added or renamed without updating `implemented_by`. So if you write a resource by
hand, add its Terraform type name to the group it calls.

To see the current state:

```sh
make coverage-report   # coverage, the generation queue, and recorded exclusions
make coverage-check    # what CI enforces
```

Both run offline against the module cache and need no API token. The file's header
comment explains every field.
