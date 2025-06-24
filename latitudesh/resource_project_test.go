package latitudesh

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
)

func TestAccProject_Basic(t *testing.T) {
	var project components.Project

	recorder, teardown := createTestRecorder(t)
	defer teardown()
	testAccProviders["latitudesh"].ConfigureContextFunc = testProviderConfigure(recorder)

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccTokenCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckProjectDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckProjectBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckProjectExists("latitudesh_project.test_item", &project),
					resource.TestCheckResourceAttr(
						"latitudesh_project.test_item", "name", "test"),
					resource.TestCheckResourceAttr(
						"latitudesh_project.test_item", "description", "hello"),
				),
			},
		},
	})
}

func testAccCheckProjectDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*latitudeshgosdk.Latitudesh)
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

		client := testAccProvider.Meta().(*latitudeshgosdk.Latitudesh)
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
	return `
resource "latitudesh_project" "test_item" {
  name        = "test"
  description = "hello"
  environment = "Development"
  provisioning_type = "on_demand"
}
`
}
