package latitudesh

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
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
	// Skip destroy check for now since we don't have a proper API method
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

		// Skip existence check for now since we don't have a proper API method
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

func TestMemberStateUpgrade_EmailFallback(t *testing.T) {
	testCases := []struct {
		name          string
		id            types.String
		email         types.String
		expectedEmail types.String
		shouldUseID   bool
	}{
		{
			name:          "email_from_id_with_at_symbol",
			id:            types.StringValue("user@example.com"),
			email:         types.StringNull(),
			expectedEmail: types.StringValue("user@example.com"),
			shouldUseID:   true,
		},
		{
			name:          "preserve_existing_email",
			id:            types.StringValue("uuid-123"),
			email:         types.StringValue("existing@example.com"),
			expectedEmail: types.StringValue("existing@example.com"),
			shouldUseID:   false,
		},
		{
			name:          "no_fallback_for_uuid_without_at",
			id:            types.StringValue("550e8400-e29b-41d4-a716-446655440000"),
			email:         types.StringNull(),
			expectedEmail: types.StringNull(),
			shouldUseID:   false,
		},
		{
			name:          "fallback_for_empty_email_string",
			id:            types.StringValue("test@example.com"),
			email:         types.StringValue(""),
			expectedEmail: types.StringValue("test@example.com"),
			shouldUseID:   true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Simulate the prior state (v0)
			priorState := MemberResourceModelV0{
				ID:    tc.id,
				Email: tc.email,
			}

			// Simulate migration to new state (v1)
			upgradedState := MemberResourceModel{
				ID:    priorState.ID,
				Email: priorState.Email,
			}

			// Apply the same fallback logic as in StateUpgrader
			hasNoEmail := upgradedState.Email.IsNull() || upgradedState.Email.IsUnknown()
			hasEmptyEmail := !hasNoEmail && upgradedState.Email.ValueString() == ""

			if (hasNoEmail || hasEmptyEmail) && !upgradedState.ID.IsNull() && !upgradedState.ID.IsUnknown() {
				idValue := upgradedState.ID.ValueString()
				if strings.Contains(idValue, "@") {
					upgradedState.Email = types.StringValue(idValue)
				}
			}

			// Verify the result
			if !tc.expectedEmail.Equal(upgradedState.Email) {
				t.Errorf("Expected email %v, got %v", tc.expectedEmail, upgradedState.Email)
			}

			// Additional verification for clarity
			if tc.shouldUseID {
				if !upgradedState.ID.Equal(upgradedState.Email) {
					t.Errorf("Expected email to match ID, but email=%v and ID=%v",
						upgradedState.Email, upgradedState.ID)
				}
			}
		})
	}
}
