package latitudesh

// Mock-backed tests reuse mockBlockStorageAPI (resource_block_storage_test.go),
// whose map/unmap endpoints model the asynchronous transitions the live API
// reports through the volume's `block` object.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// shrinkAttachmentPolling makes the waiters spin instead of sleeping 10s
// between reads.
func shrinkAttachmentPolling(t *testing.T) {
	t.Helper()
	previous := blockStorageAttachmentPollInterval
	blockStorageAttachmentPollInterval = time.Millisecond
	t.Cleanup(func() { blockStorageAttachmentPollInterval = previous })
}

func testAccBlockStorageAttachmentConfig(serverID string) string {
	return testAccBlockStorageAttachmentConfigSized(serverID, 100)
}

func testAccBlockStorageAttachmentConfigSized(serverID string, sizeInGb int) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_block_storage" "test_item" {
  project    = "test-project"
  name       = "app-data"
  region     = "ASH"
  size_in_gb = %d
}

resource "latitudesh_block_storage_attachment" "test_item" {
  block_storage_id = latitudesh_block_storage.test_item.id
  server_id        = %q
}
`, sizeInGb, serverID)
}

func testAccCheckMockBlockStorageDetached(mock *mockBlockStorageAPI) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "latitudesh_block_storage_attachment" {
				continue
			}
			if mapping := mock.mapping(rs.Primary.ID); mapping != nil {
				return fmt.Errorf("volume %s is still mapped to %s after destroy", rs.Primary.ID, mapping.serverID)
			}
		}
		return nil
	}
}

// The attachment must wait out the transient "mapping" state and then publish
// everything the server needs to reach the volume.
func TestBlockStorageAttachment_MapAndUnmap(t *testing.T) {
	shrinkAttachmentPolling(t)

	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	const name = "latitudesh_block_storage_attachment.test_item"

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy: resource.ComposeTestCheckFunc(
			testAccCheckMockBlockStorageDetached(mock),
			testAccCheckMockBlockStorageDestroyed(mock),
		),
		Steps: []resource.TestStep{
			{
				Config: testAccBlockStorageAttachmentConfig("sv_mock_1"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair(name, "id", "latitudesh_block_storage.test_item", "id"),
					resource.TestCheckResourceAttrPair(name, "block_storage_id", "latitudesh_block_storage.test_item", "id"),
					resource.TestCheckResourceAttr(name, "server_id", "sv_mock_1"),
					resource.TestCheckResourceAttr(name, "status", "mapped"),
					resource.TestCheckResourceAttr(name, "nqn", "nqn.2014-08.sh.latitude:id:sv_mock_1"),
					resource.TestCheckResourceAttr(name, "nsid", "1"),
					resource.TestCheckResourceAttr(name, "storage_network.vid", "2090"),
					resource.TestCheckResourceAttr(name, "storage_network.host_cidr", "10.248.6.4/24"),
					resource.TestCheckResourceAttr(name, "storage_network.block_port", "4420"),
					resource.TestCheckResourceAttr(name, "storage_network.routes.#", "2"),
				),
			},
			{
				// Idempotency — same config, plan must be empty.
				Config:   testAccBlockStorageAttachmentConfig("sv_mock_1"),
				PlanOnly: true,
			},
		},
	})
}

// A mapping that never settles must fail the apply instead of hanging or
// silently recording a half-attached volume.
func TestBlockStorageAttachment_MapFailure(t *testing.T) {
	shrinkAttachmentPolling(t)

	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	// Settle straight into "failed" rather than "mapped".
	mock.mapPendingReads = 0
	mock.failMapping = true

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config:      testAccBlockStorageAttachmentConfig("sv_mock_1"),
				ExpectError: regexp.MustCompile(`Attaching block storage volume .* failed`),
			},
		},
	})
}

// The API rejects a server that is not cabled for a bond up front, with a 422
// whose own message does not say how to get one that is. The error must point
// at public_network_id, the deploy option that only picks eligible servers.
func TestBlockStorageAttachment_ServerNotEligible(t *testing.T) {
	shrinkAttachmentPolling(t)

	mock := newMockBlockStorageAPI()
	mock.ineligibleServers = map[string]bool{"sv_unbondable": true}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config:      testAccBlockStorageAttachmentConfig("sv_unbondable"),
				ExpectError: regexp.MustCompile(`(?s)SERVER_NOT_ELIGIBLE_FOR_VOLUME_MAPPING.*public_network_id`),
			},
		},
	})
}

// A detach that fails comes back as "mapped", not "failed". The destroy must
// report it straight away instead of polling a mapping that will never clear
// until the delete timeout runs out, and must keep the attachment in state.
func TestBlockStorageAttachment_UnmapFailure(t *testing.T) {
	shrinkAttachmentPolling(t)

	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	setFailUnmapping := func(fail bool) func() {
		return func() {
			mock.mu.Lock()
			defer mock.mu.Unlock()
			mock.failUnmapping = fail
		}
	}

	// The same volume, with the attachment removed from the configuration.
	const volumeOnly = `
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_block_storage" "test_item" {
  project    = "test-project"
  name       = "app-data"
  region     = "ASH"
  size_in_gb = 100
}
`

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy: resource.ComposeTestCheckFunc(
			testAccCheckMockBlockStorageDetached(mock),
			testAccCheckMockBlockStorageDestroyed(mock),
		),
		Steps: []resource.TestStep{
			{Config: testAccBlockStorageAttachmentConfig("sv_mock_1")},
			{
				PreConfig:   setFailUnmapping(true),
				Config:      volumeOnly,
				ExpectError: regexp.MustCompile(`Detaching block storage volume .* failed and the volume is still`),
			},
			{
				// The failed destroy kept the attachment, so a retry detaches it.
				PreConfig: setFailUnmapping(false),
				Config:    volumeOnly,
				Check: func(s *terraform.State) error {
					if _, ok := s.RootModule().Resources["latitudesh_block_storage_attachment.test_item"]; ok {
						return fmt.Errorf("attachment is still in state after the retried destroy")
					}
					return nil
				},
			},
		},
	})
}

// Growing an attached volume resizes it in place, so the attachment must not
// be touched: no unmap and no new mapping, only the resize.
func TestBlockStorageAttachment_SurvivesVolumeGrowth(t *testing.T) {
	shrinkAttachmentPolling(t)

	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	const name = "latitudesh_block_storage_attachment.test_item"

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy: resource.ComposeTestCheckFunc(
			testAccCheckMockBlockStorageDetached(mock),
			testAccCheckMockBlockStorageDestroyed(mock),
		),
		Steps: []resource.TestStep{
			{Config: testAccBlockStorageAttachmentConfigSized("sv_mock_1", 100)},
			{
				Config: testAccBlockStorageAttachmentConfigSized("sv_mock_1", 200),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_block_storage.test_item", "size_in_gb", "200"),
					resource.TestCheckResourceAttr(name, "status", "mapped"),
					func(*terraform.State) error {
						if n := countCalls(mock, "PATCH "); n != 1 {
							return fmt.Errorf("expected one resize, got %d: %v", n, mock.callLog())
						}
						for _, call := range mock.callLog() {
							if strings.HasSuffix(call, "/unmap") || strings.HasPrefix(call, "DELETE ") {
								return fmt.Errorf("growing the volume disturbed the attachment: %v", mock.callLog())
							}
						}
						if n := countCalls(mock, "POST /storage/volumes/"); n != 1 {
							return fmt.Errorf("expected the volume to be mapped once, got %d: %v", n, mock.callLog())
						}
						return nil
					},
				),
			},
		},
	})
}

func testAccBlockStorageAttachmentConfigTimeouts(serverID, createTimeout string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

resource "latitudesh_block_storage" "test_item" {
  project    = "test-project"
  name       = "app-data"
  region     = "ASH"
  size_in_gb = 100
}

resource "latitudesh_block_storage_attachment" "test_item" {
  block_storage_id = latitudesh_block_storage.test_item.id
  server_id        = %q

  timeouts = {
    create = %q
  }
}
`, serverID, createTimeout)
}

// settleMapping makes the volume's in-flight mapping finish as status on the
// next read, and later mappings settle normally again.
func settleMapping(mock *mockBlockStorageAPI, status string) func() {
	return func() {
		mock.mu.Lock()
		defer mock.mu.Unlock()
		mock.mapPendingReads = 1
		for _, v := range mock.volumes {
			if v.block != nil {
				v.block.pending = 0
				v.block.status = status
			}
		}
	}
}

// Changing only `timeouts` reaches Update, where the computed attributes are
// unknown in the plan. They must be carried over from state, not written as
// unknown (which Terraform rejects), and nothing may be sent to the API.
func TestBlockStorageAttachment_TimeoutsChangeUpdatesInPlace(t *testing.T) {
	shrinkAttachmentPolling(t)

	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	const name = "latitudesh_block_storage_attachment.test_item"

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{Config: testAccBlockStorageAttachmentConfig("sv_mock_1")},
			{
				Config: testAccBlockStorageAttachmentConfigTimeouts("sv_mock_1", "20m"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(name, "status", "mapped"),
					resource.TestCheckResourceAttr(name, "nqn", "nqn.2014-08.sh.latitude:id:sv_mock_1"),
					resource.TestCheckResourceAttr(name, "storage_network.vid", "2090"),
					resource.TestCheckResourceAttr(name, "timeouts.create", "20m"),
					func(*terraform.State) error {
						if n := countCalls(mock, "POST /storage/volumes/"); n != 1 {
							return fmt.Errorf("a timeouts change re-mapped the volume: %v", mock.callLog())
						}
						return nil
					},
				),
			},
		},
	})
}

// A mapping that is still in flight when the create timeout runs out may yet
// finish. The attachment must stay in state (tainted) so the next apply
// detaches and re-attaches it, instead of trying a fresh map the API refuses
// with a 409 because the volume is already mapped.
func TestBlockStorageAttachment_PollTimeoutKeepsMappingTainted(t *testing.T) {
	shrinkAttachmentPolling(t)

	mock := newMockBlockStorageAPI()
	mock.mapPendingReads = 1 << 30
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	const name = "latitudesh_block_storage_attachment.test_item"

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy: resource.ComposeTestCheckFunc(
			testAccCheckMockBlockStorageDetached(mock),
			testAccCheckMockBlockStorageDestroyed(mock),
		),
		Steps: []resource.TestStep{
			{
				Config:      testAccBlockStorageAttachmentConfigTimeouts("sv_mock_1", "1s"),
				ExpectError: regexp.MustCompile(`was still not attached after`),
			},
			{
				PreConfig: settleMapping(mock, "mapped"),
				Config:    testAccBlockStorageAttachmentConfigTimeouts("sv_mock_1", "1s"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(name, "status", "mapped"),
					func(*terraform.State) error {
						if n := countCalls(mock, "POST /storage/volumes/vol_mock_1/unmap"); n != 1 {
							return fmt.Errorf("expected the tainted attachment to be detached once, got %d: %v", n, mock.callLog())
						}
						if n := countCalls(mock, "POST /storage/volumes/vol_mock_1/map"); n != 2 {
							return fmt.Errorf("expected the volume to be mapped twice, got %d: %v", n, mock.callLog())
						}
						return nil
					},
				),
			},
		},
	})
}

// The same in-flight mapping may instead end as `failed`, which the API rolls
// back. Replacing the tainted attachment must then treat the unmap's 409 as
// "nothing to detach" and attach again, rather than getting stuck.
func TestBlockStorageAttachment_TaintedMappingThatFailedIsReplaced(t *testing.T) {
	shrinkAttachmentPolling(t)

	mock := newMockBlockStorageAPI()
	mock.mapPendingReads = 1 << 30
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy: resource.ComposeTestCheckFunc(
			testAccCheckMockBlockStorageDetached(mock),
			testAccCheckMockBlockStorageDestroyed(mock),
		),
		Steps: []resource.TestStep{
			{
				Config:      testAccBlockStorageAttachmentConfigTimeouts("sv_mock_1", "1s"),
				ExpectError: regexp.MustCompile(`was still not attached after`),
			},
			{
				PreConfig: settleMapping(mock, "failed"),
				Config:    testAccBlockStorageAttachmentConfigTimeouts("sv_mock_1", "1s"),
				Check:     resource.TestCheckResourceAttr("latitudesh_block_storage_attachment.test_item", "status", "mapped"),
			},
		},
	})
}

// Every input must survive import, or the first plan after import proposes a
// replacement. The import id is the volume's, since the mapping has none.
func TestBlockStorageAttachment_Import(t *testing.T) {
	shrinkAttachmentPolling(t)

	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBlockStorageDetached(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccBlockStorageAttachmentConfig("sv_mock_1"),
			},
			{
				ResourceName:            "latitudesh_block_storage_attachment.test_item",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})
}

// Moving the volume to another server has to replace the resource: the API
// only ever maps a volume to one server, and there is no move verb.
func TestBlockStorageAttachment_ServerChangeForcesReplacement(t *testing.T) {
	shrinkAttachmentPolling(t)

	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	const name = "latitudesh_block_storage_attachment.test_item"

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBlockStorageDetached(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccBlockStorageAttachmentConfig("sv_mock_1"),
				Check:  resource.TestCheckResourceAttr(name, "server_id", "sv_mock_1"),
			},
			{
				Config: testAccBlockStorageAttachmentConfig("sv_mock_2"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(name, "server_id", "sv_mock_2"),
					resource.TestCheckResourceAttr(name, "status", "mapped"),
				),
			},
		},
	})
}

// A volume unmapped outside Terraform must drop out of state, not linger as a
// mapping that no longer exists.
func TestBlockStorageAttachment_DetachedOutOfBand(t *testing.T) {
	shrinkAttachmentPolling(t)

	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockBlockStorageDetached(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccBlockStorageAttachmentConfig("sv_mock_1"),
			},
			{
				PreConfig:          func() { mock.clearMappings() },
				Config:             testAccBlockStorageAttachmentConfig("sv_mock_1"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// TestAccBlockStorageAttachment_Basic attaches a real volume to one of the
// shared fixture's servers. Block storage is only sold in a handful of sites,
// and the fixture picks the first site with stock for its plan, so the test
// skips (rather than fails) when the two do not line up.
func TestAccBlockStorageAttachment_Basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	projectID, site, serverIDs := testAccSharedServers(t, 1)
	if !siteSellsBlockStorage(t, site) {
		t.Skipf("site %s has no block storage volume plan; skipping the attachment test", site)
	}

	const name = "latitudesh_block_storage_attachment.test_item"

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccTokenCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckBlockStorageDestroy,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "latitudesh_block_storage" "test_item" {
  project    = %q
  name       = "tf-acc-block-storage-attach-%s"
  region     = %q
  size_in_gb = 50
}

resource "latitudesh_block_storage_attachment" "test_item" {
  block_storage_id = latitudesh_block_storage.test_item.id
  server_id        = %q
}
`, projectID, testRunID, site, serverIDs[0]),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(name, "server_id", serverIDs[0]),
					resource.TestCheckResourceAttr(name, "status", "mapped"),
					resource.TestCheckResourceAttrSet(name, "nqn"),
					resource.TestCheckResourceAttrSet(name, "storage_network.vid"),
				),
			},
		},
	})
}

// siteSellsBlockStorage reports whether the volume plan lists site among its
// locations, so the live test can skip instead of failing on a site that has
// no block storage at all.
func siteSellsBlockStorage(t *testing.T, site string) bool {
	t.Helper()

	client := createVCRClient(nil)
	storageType := "volume"
	res, err := client.Plans.ListStorage(context.Background(), &storageType, nil)
	if err != nil {
		t.Logf("unable to list storage plans (%s); assuming %s has block storage", err, site)
		return true
	}
	if res == nil || res.StoragePlans == nil {
		return true
	}

	for _, plan := range res.StoragePlans.Data {
		if plan.Attributes == nil {
			continue
		}
		for _, region := range plan.Attributes.Regions {
			for _, location := range region.Locations {
				if location == site {
					return true
				}
			}
		}
	}
	return false
}

// Destroying a volume and its attachment in one run must unmap before deleting
// the volume. Terraform derives that order from the `block_storage_id`
// reference, and the unmap waiter holds the delete back until the mapping is
// actually gone — not just until the request was accepted.
func TestBlockStorageAttachment_DestroyUnmapsBeforeDeletingVolume(t *testing.T) {
	shrinkAttachmentPolling(t)

	mock := newMockBlockStorageAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	assertOrder := func(*terraform.State) error {
		var unmapAt, deleteAt = -1, -1
		for i, call := range mock.callLog() {
			switch {
			case strings.HasSuffix(call, "/unmap") && unmapAt < 0:
				unmapAt = i
			case strings.HasPrefix(call, "DELETE /storage/volumes/") && deleteAt < 0:
				deleteAt = i
			}
		}
		if unmapAt < 0 {
			return fmt.Errorf("destroy never unmapped the volume: %v", mock.callLog())
		}
		if deleteAt < 0 {
			return fmt.Errorf("destroy never deleted the volume: %v", mock.callLog())
		}
		if unmapAt > deleteAt {
			return fmt.Errorf("volume was deleted before being unmapped: %v", mock.callLog())
		}
		return nil
	}

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy: resource.ComposeTestCheckFunc(
			assertOrder,
			testAccCheckMockBlockStorageDetached(mock),
			testAccCheckMockBlockStorageDestroyed(mock),
		),
		Steps: []resource.TestStep{
			{Config: testAccBlockStorageAttachmentConfig("sv_mock_1")},
		},
	})
}
