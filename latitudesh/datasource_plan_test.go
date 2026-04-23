package latitudesh

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
)

// testPlanCandidates are tried in order by TestAccDataSourcePlan; the first one
// that exists in the backend is used. Extend this list if the current entries
// all get sunsetted from the catalog.
var testPlanCandidates = []string{
	"c3-small-x86",
	"c2-small-x86",
	"f4-metal-medium",
	"c3-medium-x86",
	"s3-large-x86",
}

const (
	validSlugForNVME  = "f4-metal-medium"
	invalidSlugByName = "f4.metal.medium"
)

// findAvailableTestPlan queries the Plans API for each candidate slug and
// returns the first one that exists. Returns "" if none exist (caller should skip).
func findAvailableTestPlan(t *testing.T, candidates []string) string {
	t.Helper()
	client, err := newSDKClientFromEnv()
	if err != nil {
		t.Logf("findAvailableTestPlan: cannot build SDK client: %s", err)
		return ""
	}
	ctx := context.Background()
	for _, slug := range candidates {
		s := slug
		result, err := client.Plans.List(ctx, operations.GetPlansRequest{FilterSlug: &s})
		if err != nil {
			t.Logf("findAvailableTestPlan: list failed for %q: %s", slug, err)
			continue
		}
		if result != nil && result.Object != nil && result.Object.Data != nil && len(result.Object.Data) > 0 {
			return slug
		}
	}
	return ""
}

func TestAccDataSourcePlan(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Fatalf("TF_ACC must be set for acceptance tests")
	}

	planSlug := findAvailableTestPlan(t, testPlanCandidates)
	if planSlug == "" {
		t.Skipf("no candidate plan slug from %v exists on the backend; skipping", testPlanCandidates)
	}

	_, teardown := createTestRecorder(t)
	defer teardown()

	notFoundRe := regexp.MustCompile(`(?i)(plan\s*not\s*found|specified\s*plan\s*was\s*not\s*found|not\s*found|404)`)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testAccConfigPlanBasic(planSlug),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"data.latitudesh_plan.test", "slug", planSlug,
					),
					resource.TestCheckResourceAttrSet(
						"data.latitudesh_plan.test", "memory",
					),
					resource.TestMatchResourceAttr(
						"data.latitudesh_plan.test", "memory",
						regexp.MustCompile(`^\d+(\.\d+)?$`),
					),
				),
			},
			{
				Config:      testAccConfigPlanByName(),
				ExpectError: notFoundRe,
			},
			{
				Config:      testAccConfigPlanNameAsSlug(),
				ExpectError: notFoundRe,
			},
			{
				Config: testAccConfigPlanSlugWithNVME(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_plan.slug_with_nvme", "slug", validSlugForNVME),
					resource.TestCheckResourceAttrSet("data.latitudesh_plan.slug_with_nvme", "id"),
					resource.TestCheckResourceAttrSet("data.latitudesh_plan.slug_with_nvme", "name"),
					resource.TestCheckResourceAttrSet("data.latitudesh_plan.slug_with_nvme", "cpu_count"),
					resource.TestCheckResourceAttrSet("data.latitudesh_plan.slug_with_nvme", "cpu_cores"),
					resource.TestCheckResourceAttrSet("data.latitudesh_plan.slug_with_nvme", "cpu_clock"),
					resource.TestCheckResourceAttrSet("data.latitudesh_plan.slug_with_nvme", "memory"),
					resource.TestMatchResourceAttr("data.latitudesh_plan.slug_with_nvme", "features.#", regexp.MustCompile(`^[1-9]\d*$`)),
					resource.TestCheckResourceAttrSet("data.latitudesh_plan.slug_with_nvme", "has_gpu"),
				),
			},
		},
	})
}

// TestAccDataSourcePlan_Features tests that features are returned as string array (SDK v1.12.1)
func TestAccDataSourcePlan_Features(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	_, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testAccConfigPlanWithFeatures(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_plan.features_test", "slug", "c3-small-x86"),
					resource.TestMatchResourceAttr("data.latitudesh_plan.features_test", "features.#", regexp.MustCompile(`^[1-9]\d*$`)),
					resource.TestCheckResourceAttr("data.latitudesh_plan.features_test", "features.0", "ssh"),
				),
			},
		},
	})
}

func testAccConfigPlanWithFeatures() string {
	return `
data "latitudesh_plan" "features_test" {
  slug = "c3-small-x86"
}
`
}

func testAccConfigPlanBasic(slug string) string {
	return fmt.Sprintf(`
data "latitudesh_plan" "test" {
  slug = "%s"
}
`, slug)
}

func testAccConfigPlanByName() string {
	return `
data "latitudesh_plan" "by_name" {
  name = "f4.metal.medium"
}
`
}

func testAccConfigPlanNameAsSlug() string {
	return fmt.Sprintf(`
data "latitudesh_plan" "name_as_slug" {
  slug = "%s"
}
`, invalidSlugByName)
}

func testAccConfigPlanSlugWithNVME() string {
	return fmt.Sprintf(`
data "latitudesh_plan" "slug_with_nvme" {
  slug = "%s"
}
`, validSlugForNVME)
}
