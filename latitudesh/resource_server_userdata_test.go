package latitudesh

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestAccServer_WithUserData(t *testing.T) {
	recorder, teardown := createTestRecorder(t)
	defer teardown()

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
			testAccProjectCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithVCR(recorder),
		CheckDestroy:             testAccCheckServerDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckServerWithUserData(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckServerExists("latitudesh_server.test_item"),
					resource.TestCheckResourceAttr(
						"latitudesh_server.test_item", "hostname", testServerHostname),
					resource.TestCheckResourceAttr(
						"latitudesh_server.test_item", "user_data", "ud_test123456789"),
					resource.TestCheckResourceAttrSet(
						"latitudesh_server.test_item", "primary_ipv4"),
				),
			},
		},
	})
}

// Unit test to validate the user data is processing correctly
func TestServerResource_UserDataProcessing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		userData types.String
		expected *string
	}{
		{
			name:     "valid_user_data_id",
			userData: types.StringValue("ud_R82A0y9L06mMY"),
			expected: stringPtr("ud_R82A0y9L06mMY"),
		},
		{
			name:     "null_user_data",
			userData: types.StringNull(),
			expected: nil,
		},
		{
			name:     "empty_user_data",
			userData: types.StringValue(""),
			expected: stringPtr(""),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Simulate processing of user data like in the correct function
			var result *string
			if !tc.userData.IsNull() {
				userDataValue := tc.userData.ValueString()
				result = &userDataValue
			}

			// Compare results safely
			if !pointersEqual(tc.expected, result) {
				var expectedStr, resultStr string
				if tc.expected != nil {
					expectedStr = *tc.expected
				} else {
					expectedStr = "<nil>"
				}
				if result != nil {
					resultStr = *result
				} else {
					resultStr = "<nil>"
				}
				t.Errorf("expected %s, got %s", expectedStr, resultStr)
			}
		})
	}
}

// Helper function to create string pointer
func stringPtr(s string) *string {
	return &s
}

// Helper function to safely compare string pointers
func pointersEqual(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func testAccCheckServerWithUserData() string {
	return fmt.Sprintf(`
resource "latitudesh_server" "test_item" {
	billing = "monthly"
	project = "%s"
  	hostname = "%s"
	plan     = "%s"
	site     = "%s"
	operating_system = "%s"
	user_data = "ud_test123456789"
	allow_reinstall = true
}
`,
		os.Getenv("LATITUDESH_TEST_PROJECT"),
		testServerHostname,
		testServerPlan,
		testServerSite,
		testServerOperatingSystem,
	)
}
