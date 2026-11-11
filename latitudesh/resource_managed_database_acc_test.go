package latitudesh

// TestAccManagedDatabase_Basic exercises latitudesh_managed_database against
// the live API. It is skipped unless TF_ACC and LATITUDESH_AUTH_TOKEN are set
// (via testAccTokenCheck), so `go test ./latitudesh` never reaches the
// network. It has not been run: the region (ASH) and plan (db.psql.small) come
// from the SDK's own doc examples, and the behaviour it confirms (status
// values, credentials on Show, delete semantics) is unverified until it is.

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccManagedDatabase_Basic(t *testing.T) {
	resourceName := "latitudesh_managed_database.test_item"
	projectID := testAccProjectID()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckManagedDatabaseDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccManagedDatabaseConfig(projectID, `["203.0.113.10/32"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "project", projectID),
					resource.TestCheckResourceAttr(resourceName, "engine", "postgres"),
					resource.TestCheckResourceAttr(resourceName, "trusted_sources.#", "1"),
					resource.TestCheckResourceAttrSet(resourceName, "credentials.host"),
				),
			},
			{
				Config: testAccManagedDatabaseConfig(projectID, `["203.0.113.10/32", "198.51.100.0/24"]`),
				Check:  resource.TestCheckResourceAttr(resourceName, "trusted_sources.#", "2"),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts", "parameters"},
			},
		},
	})
}

func testAccManagedDatabaseConfig(projectID, sources string) string {
	return fmt.Sprintf(`
resource "latitudesh_managed_database" "test_item" {
  project         = %q
  name            = "tf-acc-managed-database"
  region          = "ASH"
  plan            = "db.psql.small"
  engine          = "postgres"
  trusted_sources = %s
}
`, projectID, sources)
}

// testAccCheckManagedDatabaseDestroy accepts only not-found as proof of
// deletion; any other error is a failure.
func testAccCheckManagedDatabaseDestroy(s *terraform.State) error {
	client, err := newSDKClientFromEnv()
	if err != nil {
		return err
	}
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_managed_database" {
			continue
		}
		res, err := client.ManagedDatabases.ShowManagedDatabase(context.Background(), rs.Primary.ID)
		if err != nil {
			if apiNotFound(err) {
				continue
			}
			return fmt.Errorf("checking managed database %s: %s", rs.Primary.ID, managedDatabaseErrorDetail(err))
		}
		if res != nil && res.ManagedDatabase != nil && res.ManagedDatabase.Data != nil && res.ManagedDatabase.Data.Attributes != nil {
			if st := res.ManagedDatabase.Data.Attributes.Status; st != nil && mdbStatusIn(*st, mdbDeleteTerminalStates) {
				continue
			}
		}
		return fmt.Errorf("managed database %s still exists", rs.Primary.ID)
	}
	return nil
}
