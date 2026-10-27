package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

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

// projectTeardown422Body is the response the API returned when `terraform
// destroy` removed a block storage volume and its project in one pass: the
// volume was gone, its storage VLAN and IPs were not released yet.
const projectTeardown422Body = `{"errors":[` +
	`{"code":"VALIDATION_ERROR","status":"422","title":"Validation Error","detail":"cannot be deleted because it has IPs attached. Please reach out our support to remove all IPs from this project before deleting it.","source":{"pointer":"project"}},` +
	`{"code":"VALIDATION_ERROR","status":"422","title":"Validation Error","detail":"cannot be deleted because it still has associated vlans: [\"vlan_bBmw0KyD759VR\"]. ","source":{"pointer":"project"}}` +
	`]}`

func TestProjectDeleteBlockedByTeardown(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"pending teardown", &components.APIError{StatusCode: http.StatusUnprocessableEntity, Body: projectTeardown422Body}, true},
		{
			"unrelated 422",
			&components.APIError{StatusCode: http.StatusUnprocessableEntity, Body: `{"errors":[{"detail":"name has already been taken"}]}`},
			false,
		},
		{"403", &components.APIError{StatusCode: http.StatusForbidden, Body: projectTeardown422Body}, false},
		{"500", &components.APIError{StatusCode: http.StatusInternalServerError, Body: "boom"}, false},
		{"plain error", fmt.Errorf("boom"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := projectDeleteBlockedByTeardown(tc.err); got != tc.want {
				t.Errorf("projectDeleteBlockedByTeardown() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDeleteProjectWithRetry(t *testing.T) {
	restore := func(timeout, interval time.Duration) func() {
		prevTimeout, prevInterval := projectDeleteRetryTimeout, projectDeleteRetryInterval
		projectDeleteRetryTimeout, projectDeleteRetryInterval = timeout, interval
		return func() { projectDeleteRetryTimeout, projectDeleteRetryInterval = prevTimeout, prevInterval }
	}

	teardownErr := &components.APIError{StatusCode: http.StatusUnprocessableEntity, Body: projectTeardown422Body}

	t.Run("retries until the attached resources are released", func(t *testing.T) {
		defer restore(time.Minute, time.Millisecond)()

		calls := 0
		err := deleteProjectWithRetry(context.Background(), func(context.Context) error {
			calls++
			if calls < 3 {
				return teardownErr
			}
			return nil
		})
		if err != nil {
			t.Fatalf("deleteProjectWithRetry() = %v, want nil", err)
		}
		if calls != 3 {
			t.Errorf("del called %d times, want 3", calls)
		}
	})

	t.Run("gives up with the API's own error when the window closes", func(t *testing.T) {
		defer restore(20*time.Millisecond, time.Millisecond)()

		calls := 0
		err := deleteProjectWithRetry(context.Background(), func(context.Context) error {
			calls++
			return teardownErr
		})
		if !errors.Is(err, error(teardownErr)) {
			t.Fatalf("deleteProjectWithRetry() = %v, want the API error", err)
		}
		if calls < 2 {
			t.Errorf("del called %d times, want at least 2 before giving up", calls)
		}
	})

	t.Run("any other failure is returned immediately", func(t *testing.T) {
		defer restore(time.Minute, time.Millisecond)()

		calls := 0
		want := fmt.Errorf("boom")
		err := deleteProjectWithRetry(context.Background(), func(context.Context) error {
			calls++
			return want
		})
		if !errors.Is(err, want) {
			t.Fatalf("deleteProjectWithRetry() = %v, want %v", err, want)
		}
		if calls != 1 {
			t.Errorf("del called %d times, want 1", calls)
		}
	})

	t.Run("a cancelled context stops the wait", func(t *testing.T) {
		defer restore(time.Minute, time.Hour)()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		done := make(chan error, 1)
		go func() {
			done <- deleteProjectWithRetry(ctx, func(context.Context) error { return teardownErr })
		}()

		select {
		case err := <-done:
			if err == nil {
				t.Fatal("deleteProjectWithRetry() = nil, want the API error")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("deleteProjectWithRetry did not return after the context was cancelled")
		}
	})
}
