variable "latitudesh_token" {
  description = "Latitude.sh API token"
}

variable "plan" {
  description = "Latitude.sh server plan"
  default     = "c2-small-x86"
}

variable "region" {
  description = "Latitude.sh server region slug"
  default     = "ASH"
}

variable "ssh_public_key" {
  description = "Latitude.sh SSH public key"
}
