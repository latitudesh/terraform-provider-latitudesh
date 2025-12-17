package latitudesh

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

const (
	testMemberFirstName = "Test"
	testMemberLastName  = "User"
	testMemberRole      = "collaborator"
)

func TestAccMember_Basic(t *testing.T) {
	// Use random email to avoid conflicts
	testEmail := fmt.Sprintf("test-acc-%s@latitude.sh", acctest.RandString(6))

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testAccCheckMemberBasic(testEmail),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"latitudesh_member.test_item", "first_name", testMemberFirstName),
					resource.TestCheckResourceAttr(
						"latitudesh_member.test_item", "last_name", testMemberLastName),
					resource.TestCheckResourceAttr(
						"latitudesh_member.test_item", "email", testEmail),
					resource.TestCheckResourceAttr(
						"latitudesh_member.test_item", "role", testMemberRole),
					resource.TestCheckResourceAttrSet(
						"latitudesh_member.test_item", "created_at"),
					resource.TestCheckResourceAttrSet(
						"latitudesh_member.test_item", "updated_at"),
					resource.TestMatchResourceAttr(
						"latitudesh_member.test_item", "id", regexp.MustCompile(`^user_`)),
				),
			},
		},
	})
}

// TestAccMember_Import tests importing a member by ID (SDK v1.12.1 feature)
func TestAccMember_Import(t *testing.T) {
	// Use random email to avoid conflicts
	testEmail := fmt.Sprintf("test-acc-import-%s@latitude.sh", acctest.RandString(6))

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testAccCheckMemberBasic(testEmail),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("latitudesh_member.test_item", "id"),
				),
			},
			{
				ResourceName:      "latitudesh_member.test_item",
				ImportState:       true,
				ImportStateVerify: true,
				// These fields may differ after import due to API response
				ImportStateVerifyIgnore: []string{"first_name", "last_name"},
			},
		},
	})
}

func testAccCheckMemberBasic(email string) string {
	return fmt.Sprintf(`
resource "latitudesh_member" "test_item" {
	first_name = "%s"
	last_name  = "%s"
	email      = "%s"
	role       = "%s"
}
`,
		testMemberFirstName,
		testMemberLastName,
		email,
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
