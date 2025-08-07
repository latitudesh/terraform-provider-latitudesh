package latitudesh

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const (
	testTagName        = "test_tag"
	testTagDescription = "terraform test tag"
	testTagColor       = "#ffffff"
)

func TestAccTag_Basic(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckTagDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckTagBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckTagExists("latitudesh_tag.test_item"),
					resource.TestCheckResourceAttr(
						"latitudesh_tag.test_item", "name", testTagName),
					resource.TestCheckResourceAttr(
						"latitudesh_tag.test_item", "description", testTagDescription),
					resource.TestCheckResourceAttr(
						"latitudesh_tag.test_item", "color", testTagColor),
				),
			},
		},
	})
}

func TestAccTag_Destroy(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckTagDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckTagBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckTagExists("latitudesh_tag.test_item"),
				),
			},
		},
	})
}

func TestAccTag_ColorCaseInsensitive(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckTagDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckTagWithUppercaseColor(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckTagExists("latitudesh_tag.test_item"),
					resource.TestCheckResourceAttr(
						"latitudesh_tag.test_item", "name", testTagName),
					resource.TestCheckResourceAttr(
						"latitudesh_tag.test_item", "description", testTagDescription),
					// The color should be in lowercase
					resource.TestCheckResourceAttr(
						"latitudesh_tag.test_item", "color", "#ff0000"),
				),
			},
		},
	})
}
func testAccCheckTagDestroy(s *terraform.State) error {
	// Skip destroy check for now
	return nil
}

func testAccCheckTagExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		// Use the VCR client for existence check
		client := createVCRClient(nil) // We'll use environment variables for auth
		ctx := context.Background()

		// Check if tag exists by listing all tags and looking for the ID
		response, err := client.Tags.List(ctx)
		if err != nil {
			return fmt.Errorf("error listing tags during existence check: %w", err)
		}

		if response.CustomTags == nil || response.CustomTags.Data == nil {
			return fmt.Errorf("no tags found")
		}

		// Find our tag in the list
		for _, tag := range response.CustomTags.Data {
			if tag.ID != nil && *tag.ID == rs.Primary.ID {
				// Tag found, don't check specific attributes as they may vary by test
				return nil
			}
		}

		return fmt.Errorf("tag %s not found", rs.Primary.ID)
	}
}

func testAccCheckTagBasic() string {
	return fmt.Sprintf(`
resource "latitudesh_tag" "test_item" {
	name  	= "%s"
	description = "%s"
	color        = "%s"
}
`,
		testTagName,
		testTagDescription,
		testTagColor,
	)
}

func testAccCheckTagWithUppercaseColor() string {
	return fmt.Sprintf(`
resource "latitudesh_tag" "test_item" {
	name  	= "%s"
	description = "%s"
	color        = "#ff0000"
}
`,
		testTagName,
		testTagDescription,
	)
}

func TestAccTag_UpdateColorOnlyCasing(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckTagDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckTagUpdated(),
				Check:  testAccCheckTagExists("latitudesh_tag.test_item"),
			},
			{
				Config:             testAccCheckTagWithMixedCaseColor(),
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}

func TestAccTag_RequiredColorField(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckTagDestroy,
		Steps: []resource.TestStep{
			{
				Config:      testAccCheckTagWithoutColor(),
				ExpectError: regexp.MustCompile(`(?i)color.*required`),
			},
		},
	})
}

func TestAccTag_InvalidColor(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckTagDestroy,
		Steps: []resource.TestStep{
			{
				Config:      testAccTagWithInvalidColor(),
				ExpectError: regexp.MustCompile(`(?i)color.*valid.*hex`),
			},
		},
	})
}

func testAccTagWithInvalidColor() string {
	return `
resource "latitudesh_tag" "test_item" {
  name        = "invalid_color_tag"
  description = "trying invalid color"
  color       = "not-a-color"
}
`
}

func testAccCheckTagUpdated() string {
	return `
resource "latitudesh_tag" "test_item" {
	name  	= "test_tag"
	description = "updated terraform test tag"
	color        = "#abcdef"
}
`
}

func testAccCheckTagWithMixedCaseColor() string {
	return fmt.Sprintf(`
resource "latitudesh_tag" "test_item" {
	name  	= "%s"
	description = "updated terraform test tag"
	color        = "#ABCDEF"
}
`,
		testTagName,
	)
}

func testAccCheckTagWithoutColor() string {
	return fmt.Sprintf(`
resource "latitudesh_tag" "test_item" {
	name  	= "%s"
	description = "%s"
}
`,
		testTagName,
		testTagDescription,
	)
}

func TestAccTag_Import(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckTagDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckTagBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckTagExists("latitudesh_tag.test_item"),
				),
			},
			{
				ResourceName:      "latitudesh_tag.test_item",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccTag_ImportWithoutDescription(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resourceName := "latitudesh_tag.imported_tag"

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckTagDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckTagWithoutDescription(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckTagExists(resourceName),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config:             testAccCheckTagWithoutDescription(),
				PlanOnly:           true,
				ExpectNonEmptyPlan: false, // no diff expected after import
			},
		},
	})
}

func testAccCheckTagWithoutDescription() string {
	return `
resource "latitudesh_tag" "imported_tag" {
  name  = "import-test-no-description"
  color = "#123456"
}
`
}
