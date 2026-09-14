variable "project" {
  type    = string
  default = "proj_..."
}

data "latitudesh_traffic" "usage" {
  project  = var.project
  date_gte = "2024-04-01T00:00:00Z"
  date_lte = "2024-04-30T23:59:59Z"
}

output "traffic_total_outbound_gb" {
  value = data.latitudesh_traffic.usage.total_outbound_gb
}

output "traffic_quota_per_project" {
  value = data.latitudesh_traffic.usage.quota_per_project
}
