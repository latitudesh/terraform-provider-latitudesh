package latitudesh

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// TestAccBaselinesPreview_Basic exercises the full CR-D lifecycle (there is no
// update endpoint for this preview group) against the live API. It requires
// the account running the acceptance suite to have the `baselines_api`
// feature flag enabled.
func TestAccBaselinesPreview_Basic(t *testing.T) {
	resourceName := "latitudesh_baselines_preview.test_item"
	name := fmt.Sprintf("tf-acc-baseline-%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckBaselinesPreviewDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccConfigBaselinesPreviewBasic(name),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "name", name),
					resource.TestCheckResourceAttr(resourceName, "target_type", "all_servers"),
					resource.TestCheckResourceAttr(resourceName, "operating_system", "ubuntu_24_04_x64_lts"),
					resource.TestCheckResourceAttr(resourceName, "disk_layout.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "disk_layout.0.role", "os"),
					resource.TestCheckResourceAttrSet(resourceName, "created_at"),
				),
			},
			{
				// Idempotency: re-applying the same config must be a no-op.
				Config:   testAccConfigBaselinesPreviewBasic(name),
				PlanOnly: true,
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccConfigBaselinesPreviewBasic(name string) string {
	return fmt.Sprintf(`
resource "latitudesh_baselines_preview" "test_item" {
  name             = %q
  target_type      = "all_servers"
  operating_system = "ubuntu_24_04_x64_lts"

  disk_layout = [
    {
      role  = "os"
      count = 1
    },
  ]
}
`, name)
}

func testAccCheckBaselinesPreviewDestroy(s *terraform.State) error {
	ctx := context.Background()
	client, err := newSDKClientFromEnv()
	if err != nil {
		return err
	}

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_baselines_preview" {
			continue
		}
		id := rs.Primary.ID
		if id == "" {
			continue
		}

		resp, err := client.BaselinesPreview.GetBaseline(ctx, id)
		if err == nil && resp != nil && resp.Baseline != nil && resp.Baseline.Data != nil &&
			resp.Baseline.Data.ID != nil && *resp.Baseline.Data.ID == id {
			return fmt.Errorf("baseline still exists: %s", id)
		}
	}
	return nil
}
