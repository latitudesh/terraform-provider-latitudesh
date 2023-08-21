resource "latitudesh_vlan_assignment" "vlan_assignment" {
    server_id          = latitudesh_server.server.id
    virtual_network_id = latitudesh_virtual_network.id
}
