package latitudesh

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestAccDataSourcePlanVM(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	// testAccVMPlan discovers a real, in-stock VM plan slug via Plans.VM.List
	// (self-skips without TF_ACC or live API access), so this test always
	// exercises a plan that actually exists on the backend.
	planSlug := testAccVMPlan(t)

	recorder, teardown := createTestRecorder(t)
	defer teardown()

	notFoundRe := regexp.MustCompile(`(?i)(virtual\s*machine\s*plan\s*not\s*found|no\s*virtual\s*machine\s*plan\s*matches)`)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		Steps: []resource.TestStep{
			{
				Config: testAccConfigPlanVMBySlug(planSlug),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"data.latitudesh_plan_vm.test", "slug", planSlug,
					),
					resource.TestCheckResourceAttrSet(
						"data.latitudesh_plan_vm.test", "id",
					),
					resource.TestCheckResourceAttrSet(
						"data.latitudesh_plan_vm.test", "name",
					),
					resource.TestMatchResourceAttr(
						"data.latitudesh_plan_vm.test", "memory",
						regexp.MustCompile(`^\d+$`),
					),
					resource.TestMatchResourceAttr(
						"data.latitudesh_plan_vm.test", "available_operating_systems.#",
						regexp.MustCompile(`^[0-9]\d*$`),
					),
				),
			},
			{
				Config:      testAccConfigPlanVMBySlug("definitely-not-a-real-vm-plan-slug"),
				ExpectError: notFoundRe,
			},
		},
	})
}

func testAccConfigPlanVMBySlug(slug string) string {
	return fmt.Sprintf(`
data "latitudesh_plan_vm" "test" {
  slug = "%s"
}
`, slug)
}
