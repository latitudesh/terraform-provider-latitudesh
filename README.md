Latitude.sh Terraform Provider
==================

- Documentation: https://registry.terraform.io/providers/latitudesh/latitudesh/latest/docs 

Requirements for running the provider
------------

-	[Terraform](https://www.terraform.io/downloads.html) >= 1.3.x

Requirements for developing the provider
------------

-	[Terraform](https://www.terraform.io/downloads.html) >= 1.3.x
-	[Go](https://golang.org/doc/install) >= 1.19.x (to build the provider plugin)

Developing the provider locally
------------

- Download the latest [release](https://github.com/latitudesh/terraform-provider-latitudesh/releases) for your architecture

Mac

```sh
$ mkdir -p ~/.terraform.d/plugins/latitude.sh/iac/latitudesh/[VERSION]/darwin_amd64
$ mv terraform-provider-latitude ~/.terraform.d/plugins/latitude.sh/iac/latitudesh/[VERSION]/darwin_amd64
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
source  = "latitude.sh/iac/latitudesh"
```

Create `variables.tf` and add your Latitude.sh token. Finally, initialize the project with `terraform init`
