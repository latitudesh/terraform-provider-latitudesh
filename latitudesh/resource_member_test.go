package latitudesh

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

const (
	testMemberFirstName = "Test"
	testMemberLastName  = "User"
	testMemberEmail     = "testuser@latitude.sh"
	testMemberRole      = "collaborator"
)

func TestAccMember_Basic(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()
	testAccProviders["latitudesh"].ConfigureContextFunc = testProviderConfigure(recorder)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckMemberDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckMemberBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckMemberExists("latitudesh_member.test_item"),
					resource.TestCheckResourceAttr(
						"latitudesh_member.test_item", "first_name", testMemberFirstName),
					resource.TestCheckResourceAttr(
						"latitudesh_member.test_item", "last_name", testMemberLastName),
					resource.TestCheckResourceAttr(
						"latitudesh_member.test_item", "email", testMemberEmail),
					resource.TestCheckResourceAttr(
						"latitudesh_member.test_item", "role", testMemberRole),
				),
			},
		},
	})
}

func testAccCheckMemberDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*latitudeshgosdk.Latitudesh)
	ctx := context.Background()

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_member" {
			continue
		}

		// Note: The actual implementation to check if a member exists would go here
		// but since the SDK doesn't have a direct way to get a member by ID yet,
		// we just return nil (success) for the destroy check

		// Placeholder for future implementation
		_, err := GetMember(ctx, client, rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("Member still exists")
		}
	}

	return nil
}

func testAccCheckMemberExists(n string) resource.TestCheckFunc {
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

		// Note: The actual implementation to check if a member exists would go here
		// but since the SDK doesn't have a direct way to get a member by ID yet,
		// we just skip this check and return success

		// Placeholder for future implementation
		_, err := GetMember(ctx, client, rs.Primary.ID)
		if err != nil {
			return fmt.Errorf("Error finding member: %s", err)
		}

		return nil
	}
}

func testAccCheckMemberBasic() string {
	return fmt.Sprintf(`
resource "latitudesh_member" "test_item" {
	first_name  = "%s"
	last_name  	= "%s"
  	email 		= "%s"
  	role        = "%s"
}
`,
		testMemberFirstName,
		testMemberLastName,
		testMemberEmail,
		testMemberRole,
	)
}
