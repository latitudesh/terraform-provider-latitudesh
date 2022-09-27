# terraform-provider-latitude
Latitude Terraform Provider


## Installation
- Download latest [release](https://github.com/maxihost/terraform-provider-latitudesh/releases) for your architecture
- Mac
  - `mkdir -p ~/.terraform.d/plugins/latitudesh/iac/latitudesh/[VERSION]/darwin_amd64` 
  - `mv terraform-provider-latitude ~/.terraform.d/plugins/latitudesh/iac/latitudesh/[VERSION]/darwin_amd64`
- Linux
  - ` export OS_ARCH="$(go env GOHOSTOS)_$(go env GOHOSTARCH)"`
  - `mkdir -p ~/.terraform.d/plugins/latitudesh/iac/latitudesh/[VERSION]/$OS_ARCH`
- Windows
  - `mkdir -p %APPDATA%\terraform.d\plugins\latitudesh\iac\latitudesh\[VERSION]\[OS_ARCH]`
  - Move the binary to the appropriate subdirectory within your user plugins directory, replacing [OS_ARCH] with your system's OS_ARCH
- `terraform init` 
