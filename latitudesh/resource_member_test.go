package latitudesh

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	api "github.com/latitudesh/latitudesh-go"
)

const (
	testMemberFirstName = "Test"
	testMemberLastName  = "User"
	testMemberEmail     = "testuser@latitude.sh"
	testMemberRole      = "collaborator"
)

func TestAccMember_Basic(t *testing.T) {
	var Member api.Member

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
					testAccCheckMemberExists("latitudesh_member.test_item", &Member),
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
	client := testAccProvider.Meta().(*api.Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_member" {
			continue
		}
		if _, _, err := GetMember(client, rs.Primary.ID); err == nil {
			return fmt.Errorf("Member still exists")
		}
	}

	return nil
}

func testAccCheckMemberExists(n string, member *api.Member) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		client := testAccProvider.Meta().(*api.Client)

		foundMember, _, err := GetMember(client, rs.Primary.ID)
		if err != nil {
			return err
		}

		if foundMember.ID != rs.Primary.ID {
			return fmt.Errorf("Record not found: %v - %v", rs.Primary.ID, foundMember)
		}

		*member = *foundMember

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
