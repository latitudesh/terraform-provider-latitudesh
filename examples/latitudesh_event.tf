data "latitudesh_event" "recent" {
  filter_action = "server.power_on"
  page_size     = 10
}

output "event_total_count" {
  value = data.latitudesh_event.recent.total_count
}

output "event_actions" {
  value = [for e in data.latitudesh_event.recent.events : e.action]
}
