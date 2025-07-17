# Create a firewall with common rules
resource "latitudesh_firewall" "web_firewall" {
  name    = "Web Server Firewall"
  project = latitudesh_project.project.id
  
  # SSH Access
  rules {
    from     = "ANY"
    to       = "ANY"
    port     = "22"
    protocol = "TCP"
  }
  
  # HTTP Access
  rules {
    from     = "ANY"
    to       = "ANY"
    port     = "80"
    protocol = "TCP"
  }
  
  # HTTPS Access
  rules {
    from     = "ANY"
    to       = "ANY"
    port     = "443"
    protocol = "TCP"
  }
} 