package latitudesh

// TestAccLks_Basic exercises the cluster and a node pool against the live API. It is
// skipped unless TF_ACC and LATITUDESH_AUTH_TOKEN are set (via
// testAccTokenCheck), so `go test ./latitudesh` never reaches the network.
//
// The site and Kubernetes version are discovered from GET /lks/sites and
// GET /lks/available_versions rather than hardcoded. The scaffold guessed
// "ASH" and "1.31.0" with neither endpoint mapped; both are now data sources,
// and a guess that goes stale would fail this test for a reason that has
// nothing to do with the resource — the same argument that moved the VM tests
// off a hardcoded plan slug (resolveVMPlan).

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"gopkg.in/dnaeon/go-vcr.v3/recorder"
)

var (
	testLksTargetOnce sync.Once
	testLksSite       string
	testLksVersion    string
	testLksPlan       string
	testLksTargetErr  error
)

// resolveLksTarget discovers a site, a Kubernetes version and a node pool plan
// that can actually be built together, caching the result (and any error) once.
//
// The site is chosen FROM the plan catalog rather than independently: a plan
// must have stock at the cluster's site or the pool cannot be built, so the
// first (plan, site) pair with stock decides both. Picking a site first and
// hoping a plan has capacity there is how this test would start failing for
// reasons that have nothing to do with the provider.
//
// Version selection prefers the platform's own default and otherwise takes the
// first entry flagged available_for_creation: a version can be listed for
// upgrades only, and creating with one of those fails.
func resolveLksTarget() (site, version, plan string, err error) {
	testLksTargetOnce.Do(func() {
		client := createVCRClient(nil)
		ctx := context.Background()

		sites, sErr := client.Lks.ListLksSites(ctx)
		if sErr != nil {
			testLksTargetErr = fmt.Errorf("listing LKS sites: %w", sErr)
			return
		}
		if sites == nil || sites.LksSites == nil || len(sites.LksSites.Data) == 0 {
			testLksTargetErr = fmt.Errorf("no LKS sites returned")
			return
		}
		knownSites := map[string]bool{}
		for _, s := range sites.LksSites.Data {
			if s.Attributes != nil && s.Attributes.Slug != nil && *s.Attributes.Slug != "" {
				knownSites[*s.Attributes.Slug] = true
			}
		}
		if len(knownSites) == 0 {
			testLksTargetErr = fmt.Errorf("no LKS site carried a slug")
			return
		}

		lksPlans, pErr := client.Plans.GetLksPlans(ctx)
		if pErr != nil {
			testLksTargetErr = fmt.Errorf("listing LKS plans: %w", pErr)
			return
		}
		if lksPlans == nil || lksPlans.LksPlans == nil {
			testLksTargetErr = fmt.Errorf("no LKS plans returned")
			return
		}
	planSearch:
		for _, p := range lksPlans.LksPlans.Data {
			if p.Attributes == nil || p.Attributes.Slug == nil || *p.Attributes.Slug == "" {
				continue
			}
			for _, region := range p.Attributes.Regions {
				if region.Locations == nil {
					continue
				}
				for _, inStock := range region.Locations.InStock {
					if knownSites[inStock] {
						testLksPlan = *p.Attributes.Slug
						testLksSite = inStock
						break planSearch
					}
				}
			}
		}
		if testLksPlan == "" {
			testLksTargetErr = fmt.Errorf("no LKS plan has stock at any LKS site")
			return
		}

		versions, vErr := client.Lks.ListLksAvailableVersions(ctx)
		if vErr != nil {
			testLksTargetErr = fmt.Errorf("listing LKS versions: %w", vErr)
			return
		}
		if versions == nil || versions.LksKubernetesVersions == nil {
			testLksTargetErr = fmt.Errorf("no LKS versions returned")
			return
		}
		for _, v := range versions.LksKubernetesVersions.Data {
			attrs := v.Attributes
			if attrs == nil || attrs.Version == nil || attrs.AvailableForCreation == nil || !*attrs.AvailableForCreation {
				continue
			}
			if attrs.Default != nil && *attrs.Default {
				testLksVersion = *attrs.Version
				break
			}
			if testLksVersion == "" {
				testLksVersion = *attrs.Version
			}
		}
		if testLksVersion == "" {
			testLksTargetErr = fmt.Errorf("no LKS version available for creation")
		}
	})
	return testLksSite, testLksVersion, testLksPlan, testLksTargetErr
}

// testAccLksTarget returns a buildable (site, version, plan) triple from the
// live API, failing the test if discovery fails. Mirrors testAccVMPlan: it runs
// before resource.Test builds the case, so it gates on TF_ACC itself and skips
// in VCR replay mode, where a raw-client call cannot be served from a cassette.
func testAccLksTarget(t *testing.T) (site, version, plan string) {
	t.Helper()

	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}
	if mode, err := testRecordMode(); err == nil && mode == recorder.ModeReplayOnly {
		t.Skip("LKS site/version discovery requires live API access; not available in VCR replay mode")
	}

	site, version, plan, err := resolveLksTarget()
	if err != nil {
		t.Fatalf("discovering an LKS site, version and plan: %s", err)
	}
	return site, version, plan
}

func TestAccLks_Basic(t *testing.T) {
	resourceName := "latitudesh_lks_cluster.test_item"
	poolName := "latitudesh_lks_node_pool.test_item"
	projectID := testAccProjectID()
	site, version, plan := testAccLksTarget(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             testAccCheckLksDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccLksConfigBasic(projectID, site, version, plan),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "project", projectID),
					resource.TestCheckResourceAttr(resourceName, "site", site),
					resource.TestCheckResourceAttr(resourceName, "kubernetes_version", version),
					// Recorded when the cluster's Create returned: the control
					// plane is up and no pool exists yet.
					resource.TestCheckResourceAttr(resourceName, "reason", "WaitingForNodePool"),
					resource.TestCheckResourceAttr(poolName, "plan", plan),
					resource.TestCheckResourceAttr(poolName, "node_count", "1"),
					// The pool's own status, not ready_nodes: the live API
					// leaves that count null even on a pool whose node is up
					// (observed 2026-09-18), so asserting it against
					// node_count would fail on a perfectly healthy cluster.
					resource.TestCheckResourceAttr(poolName, "status", "ready"),
					// The claim this design rests on: a pool created after
					// the cluster is what takes it to ready.
					testAccCheckLksBecomesReady(resourceName, 20*time.Minute),
				),
			},
			{
				RefreshState: true,
				Check:        resource.TestCheckResourceAttr(resourceName, "status", "ready"),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
				// Platform-owned and live: the operator can rewrite them
				// between the refresh above and the import.
				ImportStateVerifyIgnore: []string{"status", "message", "reason", "updated_at"},
			},
		},
	})
}

// One node because each is billable bare metal. The pool references the
// cluster, so Terraform orders it after the cluster's Create returns — which is
// as soon as the control plane is up.
func testAccLksConfigBasic(project, site, version, plan string) string {
	return fmt.Sprintf(`
resource "latitudesh_lks_cluster" "test_item" {
  project             = %q
  name                = "tf-acc-lks-%s"
  site                = %q
  kubernetes_version  = %q

  timeouts = {
    delete = "30m"
  }
}

resource "latitudesh_lks_node_pool" "test_item" {
  cluster_id = latitudesh_lks_cluster.test_item.id
  plan       = %q
  node_count = 1

  timeouts = {
    create = "90m"
    delete = "30m"
  }
}
`, project, testRunID, site, version, plan)
}

// testAccCheckLksBecomesReady polls the live cluster until it reports "ready".
// The pool's create already waits for that, but runs out into a warning rather
// than an error, so this confirms it independently of the apply's own wait.
func testAccCheckLksBecomesReady(resourceName string, within time.Duration) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("%s not found in state", resourceName)
		}
		client, err := newSDKClientFromEnv()
		if err != nil {
			return err
		}

		lastStatus, lastReason := "", ""
		for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(15 * time.Second) {
			res, err := client.Lks.GetLksCluster(context.Background(), rs.Primary.ID)
			if err != nil {
				return fmt.Errorf("reading LKS cluster %s: %w", rs.Primary.ID, err)
			}
			if res == nil || res.LksCluster == nil || res.LksCluster.Data == nil || res.LksCluster.Data.Attributes == nil {
				continue
			}
			attrs := res.LksCluster.Data.Attributes
			if attrs.Status != nil {
				lastStatus = *attrs.Status
			}
			lastReason = ""
			if attrs.Reason != nil {
				lastReason = *attrs.Reason
			}
			if lastStatus == "ready" {
				return nil
			}
		}
		return fmt.Errorf("LKS cluster %s did not become ready within %s of its node pool (last status %q, reason %q)",
			rs.Primary.ID, within, lastStatus, lastReason)
	}
}

func testAccCheckLksDestroy(s *terraform.State) error {
	client, err := newSDKClientFromEnv()
	if err != nil {
		return err
	}

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "latitudesh_lks_cluster" {
			continue
		}
		id := rs.Primary.ID
		if id == "" {
			continue
		}

		_, err := client.Lks.GetLksCluster(context.Background(), id)
		if err == nil {
			return fmt.Errorf("LKS cluster still exists: %s", id)
		}
		if !lksClusterNotFound(err) {
			return fmt.Errorf("unexpected error checking LKS cluster %s destroyed: %w", id, err)
		}
	}
	return nil
}
