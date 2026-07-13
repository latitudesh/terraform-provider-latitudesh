package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

const (
	testVMName = "qa-terraform-vm"
	testVMPlan = "vm-small"
)

func TestAccVirtualMachine_Basic(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resourceName := "latitudesh_virtual_machine.test_item"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckVirtualMachineDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccVirtualMachineBasic(testVMPlan),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckVirtualMachineExists(resourceName),
					resource.TestCheckResourceAttr(resourceName, "name", testVMName),
					resource.TestCheckResourceAttr(resourceName, "plan", testVMPlan),
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttrSet(resourceName, "primary_ipv4"),
					resource.TestCheckResourceAttrSet(resourceName, "status"),
				),
			},
		},
	})
}

func TestAccVirtualMachine_Import(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resourceName := "latitudesh_virtual_machine.test_item"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckVirtualMachineDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccVirtualMachineBasic(testVMPlan),
				Check:  testAccCheckVirtualMachineExists(resourceName),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
				// ssh_keys are write-only (not returned by the read API); the read API
				// echoes plan as its ID rather than the configured slug; and it echoes
				// project as its slug rather than a configured ID. None of these
				// round-trip through import, so they are excluded from verification.
				ImportStateVerifyIgnore: []string{"ssh_keys", "plan", "project"},
			},
		},
	})
}

func testAccCheckVirtualMachineDestroy(s *terraform.State) error {
	client := createVCRClient(nil)
	ctx := context.Background()

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_virtual_machine" {
			continue
		}

		resp, err := client.VirtualMachines.Get(ctx, rs.Primary.ID)
		if err != nil {
			// A 404 means the VM is gone, as expected. Any other error must be
			// surfaced rather than silently treated as a successful destroy.
			var apiErr *components.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				continue
			}
			return fmt.Errorf("error checking virtual machine %s destroy: %w", rs.Primary.ID, err)
		}
		if resp.VirtualMachine != nil && resp.VirtualMachine.Data != nil {
			return fmt.Errorf("virtual machine %s still exists", rs.Primary.ID)
		}
	}

	return nil
}

func testAccCheckVirtualMachineExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		client := createVCRClient(nil)
		ctx := context.Background()

		resp, err := client.VirtualMachines.Get(ctx, rs.Primary.ID)
		if err != nil {
			return fmt.Errorf("error retrieving virtual machine: %w", err)
		}
		if resp.VirtualMachine == nil || resp.VirtualMachine.Data == nil {
			return fmt.Errorf("virtual machine not found")
		}
		return nil
	}
}

func testAccVirtualMachineBasic(plan string) string {
	return fmt.Sprintf(`
resource "latitudesh_project" "test" {
  name              = "tf-acc-virtual-machine"
  environment       = "Development"
  provisioning_type = "on_demand"
}

resource "latitudesh_virtual_machine" "test_item" {
  name    = %q
  plan    = %q
  project = latitudesh_project.test.id
}
`, testVMName, plan)
}
