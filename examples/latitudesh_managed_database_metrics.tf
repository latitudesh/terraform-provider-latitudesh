data "latitudesh_managed_database_metrics" "example" {
  managed_database_id = "managed_database_..."
  period              = 3600
  queries             = "cpuUsage,memoryUsage"
}

output "managed_database_metrics" {
  value = data.latitudesh_managed_database_metrics.example.metrics
}
