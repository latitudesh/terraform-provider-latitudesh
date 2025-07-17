# Create a firewall with common rules
resource "latitudesh_firewall" "web_firewall" {
  name    = "Web Server Firewall"
  project = latitudesh_project.project.id
  
  # SSH Access
  rules {
    from     = "0.0.0.0/0"
    to       = "server"
    port     = "22"
    protocol = "tcp"
  }
  
  # HTTP Access
  rules {
    from     = "0.0.0.0/0"
    to       = "server"
    port     = "80"
    protocol = "tcp"
  }
  
  # HTTPS Access
  rules {
    from     = "0.0.0.0/0"
    to       = "server"
    port     = "443"
    protocol = "tcp"
  }
} 