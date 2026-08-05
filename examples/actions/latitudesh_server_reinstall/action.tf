resource "latitudesh_server" "web" {
  hostname         = "web-01"
  operating_system = "ubuntu_24_04_x64_lts"
  plan             = data.latitudesh_plan.plan.slug
  project          = latitudesh_project.project.id
  site             = data.latitudesh_region.region.slug
  ssh_keys         = [latitudesh_ssh_key.ssh_key.id]
}

# A bare config reinstalls with the settings the server already has: same OS, same
# hostname, same keys. Nothing about latitudesh_server.web changes, so nothing drifts.
#
# Run it on demand, with no resource change to hang it off:
#
#   terraform apply -invoke=action.latitudesh_server_reinstall.rebuild
action "latitudesh_server_reinstall" "rebuild" {
  config {
    server_id = latitudesh_server.web.id
  }
}

# To rebuild during a normal apply, trigger the action from something that tracks
# *when* to rebuild rather than from the server itself. Bumping the variable
# reinstalls the machine; the server's own configuration stays put.
resource "terraform_data" "reimage_generation" {
  input = var.reimage_generation

  lifecycle {
    action_trigger {
      events  = [after_update]
      actions = [action.latitudesh_server_reinstall.rebuild]
    }
  }
}

# Overrides are available, but each one is a value latitudesh_server does not know
# about. Change the OS here and the resource still believes it runs the old one, so
# the next plan shows a diff — and on a server with allow_reinstall = true, that diff
# reinstalls the machine a second time. Keep the two in agreement.
action "latitudesh_server_reinstall" "reimage_to_debian" {
  config {
    server_id        = latitudesh_server.web.id
    operating_system = "debian_12" # also update latitudesh_server.web.operating_system
    ssh_keys         = [latitudesh_ssh_key.ssh_key.id]
    wait_timeout     = "45m"
  }
}
