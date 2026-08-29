variable "project" {
  type    = string
  default = "proj_..."
}

data "latitudesh_billing" "usage" {
  project = var.project
}

output "billing_amount" {
  value = data.latitudesh_billing.usage.amount
}

output "billing_products" {
  value = data.latitudesh_billing.usage.products
}
