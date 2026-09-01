resource "latitudesh_virtual_machine" "app" {
  name    = "app-01"
  site    = "ASH"
  plan    = "vm-small"
  project = latitudesh_project.project.id
}

# Run it on demand, with no resource change to hang it off:
#
#   terraform apply -invoke=action.latitudesh_virtual_machine_power.reboot
#
# A reboot returns as soon as the API accepts it: a restart ends at the status
# the machine started from, so there is nothing to wait on.
action "latitudesh_virtual_machine_power" "reboot" {
  config {
    virtual_machine_id = latitudesh_virtual_machine.app.id
    power_action       = "reboot"
  }
}

# power_on and power_off do wait: the action blocks until the machine reports
# the status it drives it to ("Stopped" here). Set wait_for_status = false to
# return as soon as the API accepts the request instead.
action "latitudesh_virtual_machine_power" "power_off" {
  config {
    virtual_machine_id = latitudesh_virtual_machine.app.id
    power_action       = "power_off"
  }
}

# Or trigger it during a normal apply from something that tracks *when* to act.
variable "reboot_generation" {
  description = "Bump to reboot latitudesh_virtual_machine.app on the next apply."
  type        = number
  default     = 1
}

resource "terraform_data" "reboot_generation" {
  input = var.reboot_generation

  lifecycle {
    action_trigger {
      events  = [after_update]
      actions = [action.latitudesh_virtual_machine_power.reboot]
    }
  }
}
