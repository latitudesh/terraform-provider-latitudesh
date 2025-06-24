Latitude.sh Terraform Provider
==================

- Documentation: https://registry.terraform.io/providers/latitudesh/latitudesh/latest/docs 

Requirements for running the provider
------------

-	[Terraform](https://www.terraform.io/downloads.html) >= 1.3.x

Requirements for developing the provider
------------

-	[Terraform](https://www.terraform.io/downloads.html) >= 1.3.x
-	[Go](https://golang.org/doc/install) >= 1.23.x (to build the provider plugin)

Migration Guide
------------

**Upgrading to v2?** Please read the [Migration Guide to v2](https://github.com/latitudesh/terraform-provider-latitudesh/blob/main/MIGRATION_GUIDE_v2.md) for details on breaking changes and how to safely upgrade.


Developing the provider locally
------------

- Download the latest [release](https://github.com/latitudesh/terraform-provider-latitudesh/releases) for your architecture

Mac

```sh
$ mkdir -p ~/.terraform.d/plugins/latitude.sh/iac/latitudesh/[VERSION]/darwin_amd64
$ mv terraform-provider-latitudesh ~/.terraform.d/plugins/latitude.sh/iac/latitudesh/[VERSION]/darwin_amd64
```

Linux

```sh
$ export OS_ARCH="$(go env GOHOSTOS)_$(go env GOHOSTARCH)"
$ mkdir -p ~/.terraform.d/plugins/latitude.sh/iac/latitudesh/[VERSION]/$OS_ARCH
```

Windows

```sh
$ mkdir -p %APPDATA%\terraform.d\plugins\latitude.sh\iac\latitudesh\[VERSION]\[OS_ARCH]
$ # Move the binary to the appropriate subdirectory within your user plugins directory, replacing [OS_ARCH] with your system's OS_ARCH
```

After installing the provider locally, create a Terraform project and on `main.tf` replace source with:

```sh
terraform {
  required_providers {
    latitudesh = {
      source  = "latitude.sh/iac/latitudesh"
      version = "2.0.0"
    }
  }
}
```

Create `variables.tf` and add your Latitude.sh token. Finally, initialize the project with `terraform init`
