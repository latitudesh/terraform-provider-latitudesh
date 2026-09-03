package latitudesh

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

// TestAccManagedDatabas_Basic reads metrics for a pre-existing managed
// database. ManagedDatabases exposes no create method (crud shape -R--), so
// unlike testAccSharedServers this cannot provision its own fixture: a human
// must export LATITUDESH_TEST_MANAGED_DATABASE_ID for an existing managed
// database before running this test live.
func TestAccManagedDatabas_Basic(t *testing.T) {
	dataSourceName := "data.latitudesh_managed_databas.test"

	managedDatabaseID := os.Getenv("LATITUDESH_TEST_MANAGED_DATABASE_ID")

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
			if managedDatabaseID == "" {
				t.Skip("LATITUDESH_TEST_MANAGED_DATABASE_ID must be set to an existing managed database ID for this test")
			}
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: testAccConfigManagedDatabasMetrics(managedDatabaseID),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(dataSourceName, "managed_database_id", managedDatabaseID),
					resource.TestCheckResourceAttrSet(dataSourceName, "from"),
					resource.TestCheckResourceAttrSet(dataSourceName, "to"),
					resource.TestCheckResourceAttrSet(dataSourceName, "metrics.%"),
				),
			},
		},
	})
}

func testAccConfigManagedDatabasMetrics(managedDatabaseID string) string {
	return fmt.Sprintf(`
data "latitudesh_managed_databas" "test" {
  managed_database_id = %q
  period               = 3600
}
`, managedDatabaseID)
}
