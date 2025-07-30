package latitudesh

import (
	"context"
	"fmt"
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

	// Use Framework provider with VCR
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

	// Use Framework provider with VCR
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

func testAccCheckTagDestroy(s *terraform.State) error {
	// Use the VCR client for destroy check
	client := createVCRClient(nil) // We'll use environment variables for auth
	ctx := context.Background()

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_tag" {
			continue
		}

		// Check if tag still exists by listing all tags and looking for the ID
		response, err := client.Tags.List(ctx)
		if err != nil {
			return fmt.Errorf("error listing tags during destroy check: %w", err)
		}

		if response.CustomTags != nil && response.CustomTags.Data != nil {
			for _, tag := range response.CustomTags.Data {
				if tag.ID != nil && *tag.ID == rs.Primary.ID {
					return fmt.Errorf("tag %s still exists", rs.Primary.ID)
				}
			}
		}
	}

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
				// Verify the tag has the expected attributes
				if tag.Attributes != nil {
					if tag.Attributes.Name != nil && *tag.Attributes.Name != testTagName {
						return fmt.Errorf("tag name mismatch: expected %s, got %s", testTagName, *tag.Attributes.Name)
					}
					if tag.Attributes.Description != nil && *tag.Attributes.Description != testTagDescription {
						return fmt.Errorf("tag description mismatch: expected %s, got %s", testTagDescription, *tag.Attributes.Description)
					}
					if tag.Attributes.Color != nil && *tag.Attributes.Color != testTagColor {
						return fmt.Errorf("tag color mismatch: expected %s, got %s", testTagColor, *tag.Attributes.Color)
					}
				}
				return nil // Tag found and attributes match
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
