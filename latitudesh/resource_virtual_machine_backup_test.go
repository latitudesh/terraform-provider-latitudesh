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

// TestAccVirtualMachineBackup_Basic provisions a virtual machine and backs it
// up. It runs against the live API (LATITUDESH_AUTH_TOKEN, TF_ACC=1) and is
// not VCR-backed: per sdk-coverage.yaml, this SDK method group was verified
// against an API checkout with no HTTP routes at all for this domain, so
// there is no cassette to record from yet. A human must confirm the
// endpoints exist in the target environment before running this test.
func TestAccVirtualMachineBackup_Basic(t *testing.T) {
	plan := testAccVMPlan(t)

	resourceName := "latitudesh_virtual_machine_backup.test_item"
	vmResourceName := "latitudesh_virtual_machine.test_item"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckVirtualMachineBackupDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccVirtualMachineBackupBasic(plan),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttrPair(resourceName, "virtual_machine", vmResourceName, "id"),
					resource.TestCheckResourceAttrSet(resourceName, "status"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccCheckVirtualMachineBackupDestroy(s *terraform.State) error {
	client, err := newSDKClientFromEnv()
	if err != nil {
		return err
	}
	ctx := context.Background()

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_virtual_machine_backup" {
			continue
		}
		id := rs.Primary.ID
		if id == "" {
			continue
		}

		_, err := client.VirtualMachineBackups.Get(ctx, id)
		if err == nil {
			return fmt.Errorf("virtual machine backup %s still exists", id)
		}
		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			continue
		}
		return fmt.Errorf("error checking virtual machine backup %s destroy: %w", id, err)
	}
	return nil
}

func testAccVirtualMachineBackupBasic(plan string) string {
	return fmt.Sprintf(`
resource "latitudesh_virtual_machine" "test_item" {
  name    = %q
  site    = %q
  plan    = %q
  project = "`+testAccProjectID()+`"
}

resource "latitudesh_virtual_machine_backup" "test_item" {
  virtual_machine = latitudesh_virtual_machine.test_item.id
}
`, testVMName, testVMSite, plan)
}
