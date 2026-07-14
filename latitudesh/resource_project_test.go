package latitudesh

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
)

func TestAccProject_Basic(t *testing.T) {
	var project components.Project

	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckProjectDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckProjectBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckProjectExists("latitudesh_project.test_item", &project),
					resource.TestCheckResourceAttr(
						"latitudesh_project.test_item", "name", "tf-acc-project-"+testRunID),
					resource.TestCheckResourceAttr(
						"latitudesh_project.test_item", "description", "terraform acceptance test project"),
				),
			},
		},
	})
}

func testAccCheckProjectDestroy(s *terraform.State) error {
	client := createVCRClient(nil)
	ctx := context.Background()

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_project" {
			continue
		}

		response, err := client.Projects.List(ctx, operations.GetProjectsRequest{})
		if err != nil {
			continue
		}

		if response.Projects != nil && response.Projects.Data != nil {
			for _, p := range response.Projects.Data {
				if p.ID != nil && *p.ID == rs.Primary.ID {
					return fmt.Errorf("project still exists")
				}
			}
		}
	}

	return nil
}

func testAccCheckProjectExists(n string, project *components.Project) resource.TestCheckFunc {
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

		response, err := client.Projects.List(ctx, operations.GetProjectsRequest{})
		if err != nil {
			return err
		}

		if response.Projects == nil || response.Projects.Data == nil {
			return fmt.Errorf("project not found")
		}

		// Find our project
		for _, p := range response.Projects.Data {
			if p.ID != nil && *p.ID == rs.Primary.ID {
				*project = p
				return nil
			}
		}

		return fmt.Errorf("project not found")
	}
}

func testAccCheckProjectBasic() string {
	return fmt.Sprintf(`
resource "latitudesh_project" "test_item" {
  name        = "tf-acc-project-%s"
  description = "terraform acceptance test project"
  environment = "Development"
  provisioning_type = "on_demand"
}
`, testRunID)
}
