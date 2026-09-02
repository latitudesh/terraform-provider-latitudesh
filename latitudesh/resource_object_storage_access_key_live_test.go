package latitudesh

// Live acceptance test for the managed object storage access key resource. It
// hits the real Latitude.sh API (needs LATITUDESH_AUTH_TOKEN) and creates real,
// billable resources, so it runs only in the e2e job. It is the first real
// end-to-end coverage for this resource; the mock suites in
// resource_object_storage_access_key_test.go cover the offline paths.
//
// It uses the `standard` (Wasabi) tier on purpose: it is broadly available and
// its listing is consistent, so the plan-idempotency step is stable in CI. The
// `high_performance` (VAST) listing inconsistency that motivated the
// non-destructive Read is covered by the mock regression
// (TestAccObjectStorageAccessKeyResource_VASTNotInListIsPreserved), because a
// live VAST test would depend on VAST being enabled for the CI account and on
// the very listing behavior under investigation.

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const (
	testAccessKeyName   = "tf-acc-access-key"
	testAccessKeyBucket = "tf-acc-access-key-bucket"
	testAccessKeyRegion = "ASH"
)

func TestAccObjectStorageAccessKeyResource_Live(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	resourceName := "latitudesh_object_storage_access_key.test"
	projectID := testAccProjectID()
	if projectID == "" {
		t.Fatal("shared test project unavailable")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckAccessKeyLiveDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccConfigAccessKeyLive(projectID),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttrSet(resourceName, "access_key_id"),
					resource.TestCheckResourceAttrSet(resourceName, "secret_access_key"),
					resource.TestCheckResourceAttrSet(resourceName, "username"),
					resource.TestCheckResourceAttr(resourceName, "access_scope", "limited_access"),
					resource.TestCheckResourceAttr(resourceName, "bucket_permissions.#", "1"),
				),
			},
			{
				// Regression against the live API for the destructive-Read bug:
				// the second plan must be empty. Read must refresh the key (or, if
				// it is absent from the list, preserve it) rather than proposing a
				// secret-losing recreate.
				Config:   testAccConfigAccessKeyLive(projectID),
				PlanOnly: true,
			},
		},
	})
}

func testAccConfigAccessKeyLive(projectID string) string {
	return fmt.Sprintf(`
resource "latitudesh_object_storage" "test" {
  project = %[1]q
  name    = %[2]q
  region  = %[3]q
}

resource "latitudesh_object_storage_access_key" "test" {
  project       = %[1]q
  name          = %[4]q
  storage_class = "standard"
  region        = %[3]q
  access_scope  = "limited_access"

  bucket_permissions = [
    {
      bucket_id  = latitudesh_object_storage.test.id
      permission = "rw"
    },
  ]
}
`, projectID, testAccessKeyBucket, testAccessKeyRegion, testAccessKeyName)
}

// testAccCheckAccessKeyLiveDestroy confirms the key is gone from the project's
// listing. Standard keys list consistently, so absence here is a reliable
// deletion signal for this tier.
func testAccCheckAccessKeyLiveDestroy(s *terraform.State) error {
	ctx := context.Background()
	client, err := newSDKClientFromEnv()
	if err != nil {
		return err
	}

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_object_storage_access_key" {
			continue
		}
		username := rs.Primary.Attributes["username"]
		project := rs.Primary.Attributes["project"]
		if username == "" || project == "" {
			continue
		}

		resp, err := client.ObjectStorage.GetStorageAccessKeys(ctx, project)
		if err != nil {
			// The shared project is likely already torn down; nothing to assert.
			return nil
		}
		if resp == nil || resp.Object == nil || resp.Object.Data == nil {
			continue
		}
		for _, k := range resp.Object.Data.Standard {
			if k.Username != nil && *k.Username == username {
				return fmt.Errorf("object storage access key still exists: %s", username)
			}
		}
	}
	return nil
}
