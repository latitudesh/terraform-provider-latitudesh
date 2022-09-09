# terraform-provider-latitude
Latitude Terraform Provider


## Installation
- Download latest [release](https://github.com/capturealpha/terraform-provider-latitude/releases) for your architecture
- Mac
  - `mkdir -p ~/.terraform.d/plugins/capturealpha.com/iac/latitude/0.0.1/darwin_amd64` 
  - `mv terraform-provider-latitude ~/.terraform.d/plugins/capturealpha.com/iac/latitude/0.0.1/darwin_amd64`
- Linux
  - ` export OS_ARCH="$(go env GOHOSTOS)_$(go env GOHOSTARCH)"`
  - `mkdir -p ~/.terraform.d/plugins/capturealpha.com/iac/latitude/0.0.1/$OS_ARCH`
- Windows
  - `mkdir -p %APPDATA%\terraform.d\plugins\capturealpha.com\iac\latitude\0.0.1\[OS_ARCH]`
  - Move the binary to the appropriate subdirectory within your user plugins directory, replacing [OS_ARCH] with your system's OS_ARCH
- `terraform init` 
