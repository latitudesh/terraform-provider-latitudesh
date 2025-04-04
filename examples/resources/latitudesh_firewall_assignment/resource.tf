resource "latitudesh_project" "example" {
  name              = "Example Project"
  environment       = "Development"
  provisioning_type = "on_demand"
}

# Create a server
resource "latitudesh_server" "example" {
  project          = latitudesh_project.example.id
  site             = "NY1"
  plan             = "c2-medium-x86"
  hostname         = "web-server-01"
  operating_system = "ubuntu_22_04_x64_lts"
}

# Create a firewall
resource "latitudesh_firewall" "example" {
  name    = "Web Server Firewall"
  project = latitudesh_project.example.id
  
  # SSH Access
  rules {
    from     = "0.0.0.0/0"  # From anywhere on the internet
    to       = "server"     # To the server
    port     = "22"         # SSH port
    protocol = "tcp"
  }
  
  # HTTP & HTTPS Access
  rules {
    from     = "0.0.0.0/0"  # From anywhere on the internet
    to       = "server"     # To the server
    port     = "80,443"     # HTTP and HTTPS ports
    protocol = "tcp"
  }
}

# Assign the firewall to the server
resource "latitudesh_firewall_assignment" "example" {
  firewall_id = latitudesh_firewall.example.id
  server_id   = latitudesh_server.example.id
} 