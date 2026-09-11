# A BGP-mode Elastic IP is a /32 allocated in a site and announced over iBGP by
# one or more servers, so the address can be shared or failed over between them.
# Every announcer must be deployed with bgp_ready = true, in the same site as the
# address, and BGP must be enabled for that site.
resource "latitudesh_server" "bgp_announcer" {
  count = 2

  hostname         = "tf-bgp-announcer-${count.index}"
  operating_system = "ubuntu_24_04_x64_lts"
  plan             = "c2-small-x86"
  project          = latitudesh_project.project.id
  site             = "DAL"
  bgp_ready        = true
}

resource "latitudesh_elastic_ip_bgp" "vip" {
  project    = latitudesh_project.project.id
  site       = "DAL"
  server_ids = latitudesh_server.bgp_announcer[*].id
}

# The address each server announces from its loopback.
output "bgp_vip_address" {
  value = latitudesh_elastic_ip_bgp.vip.address
}

# The BGP neighbor to configure on each announcer — `neighbor` in BIRD,
# `peerAddress` in MetalLB.
output "bgp_neighbors" {
  value = {
    for session in latitudesh_elastic_ip_bgp.vip.bgp_sessions :
    session.server_id => session.peer_address
  }
}
