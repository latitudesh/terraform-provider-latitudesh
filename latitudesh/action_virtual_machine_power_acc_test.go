package latitudesh

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// TestAccVirtualMachinePowerAction_Live provisions a virtual machine and powers
// it off through the action in the same apply. There is no shared VM fixture to
// borrow, so the test pays for its own machine — hourly billing, destroyed at
// the end of the step.
//
// power_off is the variant worth paying for live: it exercises the status wait
// against the real KubeVirt watcher pipeline (Running → Stopping → Stopped),
// which the mock tests can only script. The VM resource's create already waits
// until the machine reports Running with an IP, so the action lands on a
// machine that can take it.
func TestAccVirtualMachinePowerAction_Live(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}
	testAccTokenCheck(t)

	plan := testAccVMPlan(t)

	// Watcher events land in seconds; polling faster than the production 10s
	// keeps the test snappy without changing what is being tested.
	previousInterval := vmPowerPollInterval
	vmPowerPollInterval = 5 * time.Second
	t.Cleanup(func() { vmPowerPollInterval = previousInterval })

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckVirtualMachineDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccVMPowerActionLiveConfig(plan),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckVirtualMachineExists("latitudesh_virtual_machine.power_target"),
					func(s *terraform.State) error {
						// The action returning without diagnostics means the wait saw
						// Stopped. Re-read it independently to prove the wait did not
						// fool itself.
						rs, ok := s.RootModule().Resources["latitudesh_virtual_machine.power_target"]
						if !ok {
							return fmt.Errorf("latitudesh_virtual_machine.power_target not in state")
						}
						vmID := rs.Primary.ID

						client := createVCRClient(nil)
						result, err := client.VirtualMachines.Get(context.Background(), vmID, nil)
						if err != nil {
							return fmt.Errorf("reading virtual machine %s after power_off: %w", vmID, err)
						}
						if result.VirtualMachine == nil || result.VirtualMachine.Data == nil ||
							result.VirtualMachine.Data.Attributes == nil ||
							result.VirtualMachine.Data.Attributes.Status == nil {
							return fmt.Errorf("virtual machine %s returned no status", vmID)
						}
						if status := *result.VirtualMachine.Data.Attributes.Status; status != vmStatusStopped {
							return fmt.Errorf("virtual machine %s is %q after the action returned, want %q",
								vmID, status, vmStatusStopped)
						}
						return nil
					},
				),
			},
		},
	})
}

func testAccVMPowerActionLiveConfig(plan string) string {
	return fmt.Sprintf(`
resource "latitudesh_virtual_machine" "power_target" {
  name    = %q
  site    = %q
  plan    = %q
  billing = %q
  project = %q
}

resource "terraform_data" "power_trigger" {
  input = latitudesh_virtual_machine.power_target.id

  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.latitudesh_virtual_machine_power.test]
    }
  }
}

action "latitudesh_virtual_machine_power" "test" {
  config {
    virtual_machine_id = latitudesh_virtual_machine.power_target.id
    power_action       = "power_off"
    wait_timeout       = "10m"
  }
}
`, testVMName+"-power", testVMSite, plan, testVMBilling, testAccProjectID())
}
