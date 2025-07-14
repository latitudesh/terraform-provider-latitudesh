# Contributing to terraform-provider-latitudesh

First off, thanks for taking the time to contribute! 🎉

Whether it's reporting a bug, suggesting a feature, improving documentation, or submitting a pull request, every contribution is welcome and appreciated.

---

## 🧩 Ways to Contribute

### 1. Report Bugs

If you find a bug, please [open an issue](https://github.com/latitudesh/terraform-provider-latitudesh/issues) and include:

- A clear description of the problem
- Steps to reproduce it (ideally with a minimal Terraform config)
- What you expected to happen
- What actually happened
- Your environment (Terraform version, OS, provider version)

### 2. Suggest Features

Have an idea for a new feature or improvement? [Open an issue](https://github.com/latitudesh/terraform-provider-latitudesh/issues/new) and let’s discuss!

### 3. Submit Pull Requests

We ❤️ PRs! Here's how to get started:

---

## 🛠️ Local Development Setup

1. **Fork and Clone the Repo**

```sh
git clone https://github.com/your-username/terraform-provider-latitudesh.git
cd terraform-provider-latitudesh
```

2. **Install Dependencies**

- [Go](https://go.dev/dl/) >= 1.23.x
- [Terraform](https://developer.hashicorp.com/terraform) >= 1.3.x

3. **Build the Provider**

```sh
go build -o terraform-provider-latitudesh
```

# For Apple Silicon (darwin_arm64), build and install the provider locally:

```sh
GOOS=darwin GOARCH=arm64 go build -o terraform-provider-latitudesh
mkdir -p ~/.terraform.d/plugins/latitude.sh/iac/latitudesh/0.0.0/darwin_arm64
mv terraform-provider-latitudesh ~/.terraform.d/plugins/latitude.sh/iac/latitudesh/0.0.0/darwin_arm64/
chmod +x ~/.terraform.d/plugins/latitude.sh/iac/latitudesh/0.0.0/darwin_arm64/terraform-provider-latitudesh
```

# For Intel Macs (darwin_amd64), use:

```sh
GOOS=darwin GOARCH=amd64 go build -o terraform-provider-latitudesh
mkdir -p ~/.terraform.d/plugins/latitude.sh/iac/latitudesh/0.0.0/darwin_amd64
mv terraform-provider-latitudesh ~/.terraform.d/plugins/latitude.sh/iac/latitudesh/0.0.0/darwin_amd64/
chmod +x ~/.terraform.d/plugins/latitude.sh/iac/latitudesh/0.0.0/darwin_amd64/terraform-provider-latitudesh
```

# Use the provider in your Terraform project (main.tf):

```hcl
terraform {
  required_providers {
    latitudesh = {
      source = "latitude.sh/iac/latitudesh"
    }
  }
}

provider "latitudesh" {
  auth_token = var.LATITUDESH_AUTH_TOKEN
}
```

# Remove any `version` line from the required_providers block to force Terraform to use your local build.

# In your test project, run:

```sh
rm -rf .terraform .terraform.lock.hcl
terraform init
```

Terraform will now use your local provider build for development and testing.

