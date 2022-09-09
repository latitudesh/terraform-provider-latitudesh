variable "auth_token" {
  description = "Latitude API auth token"
}
variable "plan" {
  description = "Latitude server plan"
  default = "c2.small.x86"
}
variable "region" {
  description = "Latitude server region slug"
  default = "ASH"
}

variable "ssh_public_key" {
  description = "Latitude SSH public key"
}