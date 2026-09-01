resource "latitudesh_server" "web" {
  hostname         = "web-01"
  operating_system = "ubuntu_24_04_x64_lts"
  plan             = data.latitudesh_plan.plan.slug
  project          = latitudesh_project.project.id
  site             = data.latitudesh_region.region.slug
}

# Run it on demand, with no resource change to hang it off:
#
#   terraform apply -invoke=action.latitudesh_server_power.reboot
#
# A reboot returns as soon as the API accepts it: it is a warm reset, so the
# server's status reads "on" the whole time and there is nothing to wait on.
action "latitudesh_server_power" "reboot" {
  config {
    server_id    = latitudesh_server.web.id
    power_action = "reboot"
  }
}

# power_on and power_off do wait: the action blocks until the server reports
# the status it drives it to ("off" here). Set wait_for_status = false to
# return as soon as the API accepts the request instead.
action "latitudesh_server_power" "power_off" {
  config {
    server_id    = latitudesh_server.web.id
    power_action = "power_off"
  }
}

# Or trigger it during a normal apply from something that tracks *when* to act.
# Bumping this variable reboots the machine; the server's own configuration
# stays put, so nothing drifts.
variable "reboot_generation" {
  description = "Bump to reboot latitudesh_server.web on the next apply."
  type        = number
  default     = 1
}

resource "terraform_data" "reboot_generation" {
  input = var.reboot_generation

  lifecycle {
    action_trigger {
      events  = [after_update]
      actions = [action.latitudesh_server_power.reboot]
    }
  }
}
