resource "latitudesh_project" "example" {
  name              = "Example Project"
  environment       = "Development"
  provisioning_type = "on_demand"
}

resource "latitudesh_firewall" "example" {
  name    = "Web Server Firewall"
  project = latitudesh_project.example.id
  
  # SSH Access
  rules {
    from     = "0.0.0.0/0"  # From anywhere on the internet
    to       = "server"     # To the server
    port     = "22"         # SSH port
    protocol = "tcp"
    # default will be set to false automatically
  }
  
  # HTTP Access
  rules {
    from     = "0.0.0.0/0"  # From anywhere on the internet
    to       = "server"     # To the server
    port     = "80"         # HTTP port
    protocol = "tcp"
  }
  
  # HTTPS Access
  rules {
    from     = "0.0.0.0/0"  # From anywhere on the internet
    to       = "server"     # To the server
    port     = "443"        # HTTPS port
    protocol = "tcp"
  }
  
  # Allow a range of ports for a specific network
  rules {
    from     = "192.168.1.0/24"  # From internal network
    to       = "server"          # To the server
    port     = "8000-9000"       # Custom application ports
    protocol = "tcp"
  }
} 