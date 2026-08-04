package latitudesh

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/acctest"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	"gopkg.in/dnaeon/go-vcr.v3/recorder"
)

// testRunID is appended to created resource names: the API rejects duplicate
// project names, so fixed names collide with parallel runs or with leftovers
// from aborted ones.
var testRunID = acctest.RandString(6)

// defaultTestProjectID is the pre-existing project the recorded VCR cassettes were
// captured against. It is used only in replay mode, where no project is created.
const defaultTestProjectID = "proj_jv6m5JyZDNLPe"

var (
	testProjectOnce    sync.Once
	testProjectID      string
	testProjectCreated bool
)

// testAccProjectID returns the one project every acceptance test shares,
// creating it on first use so the whole run is self-contained and leaves nothing
// behind (testSharedFixtureTeardown deletes it). Resolution, in order:
//
//   - replay mode      → defaultTestProjectID, the cassette's project, not created
//     (no live call).
//   - otherwise (live) → a fresh `tf-acc-shared-<runID>` project.
//
// It returns "" if creation fails; callers that have a *testing.T should fail on
// that, and the underlying error is logged to stderr since most call sites embed
// this in an HCL string and have no way to report it.
func testAccProjectID() string {
	testProjectOnce.Do(func() {
		if mode, err := testRecordMode(); err == nil && mode == recorder.ModeReplayOnly {
			testProjectID = defaultTestProjectID
			return
		}

		name := fmt.Sprintf("tf-acc-shared-%s", testRunID)
		client := createVCRClient(nil)
		result, err := client.Projects.Create(context.Background(), operations.CreateProjectProjectsRequestBody{
			Data: &operations.CreateProjectProjectsData{
				Type: operations.CreateProjectProjectsTypeProjects,
				Attributes: &operations.CreateProjectProjectsAttributes{
					Name:             name,
					ProvisioningType: operations.CreateProjectProvisioningTypeOnDemand,
				},
			},
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "shared project: creating %q: %s\n", name, err)
			return
		}
		if result.Object == nil || result.Object.Data == nil || result.Object.Data.ID == nil {
			fmt.Fprintln(os.Stderr, "shared project: create response missing ID")
			return
		}
		testProjectID = *result.Object.Data.ID
		testProjectCreated = true
	})
	return testProjectID
}

var (
	testProjectSlugOnce sync.Once
	testProjectSlug     string
	testProjectSlugErr  error
)

// resolveProjectSlug looks up the shared project's slug from the API by
// matching testAccProjectID(), caching the result (and any error) once.
func resolveProjectSlug() (string, error) {
	testProjectSlugOnce.Do(func() {
		client := createVCRClient(nil)
		ctx := context.Background()
		id := testAccProjectID()
		size := int64(100)

		resp, err := client.Projects.List(ctx, operations.GetProjectsRequest{PageSize: &size})
		for err == nil && resp != nil && resp.Projects != nil {
			for _, p := range resp.Projects.Data {
				if p.ID != nil && *p.ID == id && p.Attributes != nil && p.Attributes.Slug != nil {
					testProjectSlug = *p.Attributes.Slug
					return
				}
			}
			if resp.Next == nil {
				break
			}
			resp, err = resp.Next()
		}
		if err != nil {
			testProjectSlugErr = err
			return
		}
		testProjectSlugErr = fmt.Errorf("project %s not found while resolving its slug", id)
	})
	return testProjectSlug, testProjectSlugErr
}

// testAccProjectSlug returns the shared project's slug, failing the test if it
// can't be resolved. Needed by tests that exercise the project-by-slug path
// (which can't derive a slug from a bare ID).
func testAccProjectSlug(t *testing.T) string {
	t.Helper()

	// Unlike resource.Test, this helper runs before the test case is built,
	// so it must gate on TF_ACC itself to keep plain `go test` runs offline.
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	// The slug is resolved with a raw SDK client (no recorder), so it can't be
	// served from a cassette. Skip in replay mode, mirroring the other
	// live-only helpers (testAccSharedServers), so we don't make a live call
	// before the cassette-backed provider is installed.
	if mode, err := testRecordMode(); err == nil && mode == recorder.ModeReplayOnly {
		t.Skip("resolving project slug requires live API access; not available in VCR replay mode")
	}

	slug, err := resolveProjectSlug()
	if err != nil {
		t.Fatalf("resolving shared project slug: %s", err)
	}
	return slug
}

// testAccProjectSlugBestEffort returns the shared project's slug, or "" if it
// can't be resolved. For check helpers that have no *testing.T to fail.
func testAccProjectSlugBestEffort() string {
	slug, _ := resolveProjectSlug()
	return slug
}

var (
	testVMPlanOnce sync.Once
	testVMPlanSlug string
	testVMPlanErr  error
)

// resolveVMPlan discovers the smallest VM plan with stock at testVMSite via
// Plans.VM.List, caching the result (and any error) once. Discovering the plan
// (rather than hardcoding a slug) is what PD-6519 unblocked: SDK versions
// before v1.19.0 could not unmarshal this endpoint's response at all.
func resolveVMPlan() (string, error) {
	testVMPlanOnce.Do(func() {
		client := createVCRClient(nil)
		ctx := context.Background()

		resp, err := client.Plans.VM.List(ctx, nil)
		if err != nil {
			testVMPlanErr = err
			return
		}
		if resp == nil || resp.VirtualMachinePlans == nil {
			testVMPlanErr = fmt.Errorf("empty VM plans response")
			return
		}

		slug := pickVMPlan(resp.VirtualMachinePlans.Data, testVMSite)
		if slug == "" {
			testVMPlanErr = fmt.Errorf("no VM plan with stock at %s", testVMSite)
			return
		}
		testVMPlanSlug = slug
	})
	return testVMPlanSlug, testVMPlanErr
}

// pickVMPlan returns the slug of the smallest plan (by memory) with stock at
// the site, or "" when none qualifies.
func pickVMPlan(plans []components.VirtualMachinePlansData, site string) string {
	var bestSlug string
	var bestMemory int64
	for _, plan := range plans {
		attrs := plan.Attributes
		if attrs == nil || attrs.Slug == nil || attrs.Specs == nil || attrs.Specs.Memory == nil {
			continue
		}
		if !vmPlanInStockAt(attrs.Regions, site) {
			continue
		}
		if bestSlug == "" || *attrs.Specs.Memory < bestMemory {
			bestSlug = *attrs.Slug
			bestMemory = *attrs.Specs.Memory
		}
	}
	return bestSlug
}

// vmPlanInStockAt reports whether any region of the plan lists the site among
// its in-stock locations.
func vmPlanInStockAt(regions []components.VirtualMachinePlansRegions, site string) bool {
	for _, region := range regions {
		if region.Locations == nil {
			continue
		}
		for _, s := range region.Locations.InStock {
			if s == site {
				return true
			}
		}
	}
	return false
}

// testAccVMPlan returns the slug of a VM plan deployable at testVMSite,
// discovered from the live API, failing the test if discovery fails.
func testAccVMPlan(t *testing.T) string {
	t.Helper()

	// Unlike resource.Test, this helper runs before the test case is built,
	// so it must gate on TF_ACC itself to keep plain `go test` runs offline.
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}

	// Discovery uses a raw SDK client (no recorder), so it can't be served
	// from a cassette. Skip in replay mode, mirroring the other live-only
	// helpers (testAccProjectSlug, testAccSharedServers).
	if mode, err := testRecordMode(); err == nil && mode == recorder.ModeReplayOnly {
		t.Skip("VM plan discovery requires live API access; not available in VCR replay mode")
	}

	slug, err := resolveVMPlan()
	if err != nil {
		t.Fatalf("discovering VM plan via Plans.VM.List: %s", err)
	}
	return slug
}

// Shared acceptance-test fixture: attachment-style tests (VLAN/firewall
// assignments, elastic IPs) only need "a server to attach things to", so one
// project and its servers are provisioned lazily once per `go test` run and
// reused across tests instead of paying a bare-metal deploy per test.
// TestMain tears the fixture down after the run. Server lifecycle tests
// (TestAccServer_*) keep provisioning their own servers — create/update/
// destroy is their test subject.

var testSharedFixture struct {
	mu        sync.Mutex
	projectID string
	site      string
	serverIDs []string

	// hostnames records what was requested for each server in serverIDs, by index,
	// so a test can compare it against what the API reports back without
	// provisioning anything of its own.
	hostnames []string
}

// testAccSharedServers returns the shared project, its site, and n server IDs,
// provisioning whatever is still missing. All servers live in the same project
// and site so networks and IPs can colocate with them. Callers must only
// invoke it when TF_ACC is set.
func testAccSharedServers(t *testing.T, n int) (projectID, site string, serverIDs []string) {
	t.Helper()

	// The fixture provisions real infrastructure with a raw SDK client, which
	// cannot be served from VCR cassettes.
	if mode, err := testRecordMode(); err == nil && mode == recorder.ModeReplayOnly {
		t.Skip("shared server fixture requires live API access; not available in VCR replay mode")
	}

	f := &testSharedFixture
	f.mu.Lock()
	defer f.mu.Unlock()

	client := createVCRClient(nil)
	ctx := context.Background()

	if f.projectID == "" {
		f.projectID = testAccProjectID()
		if f.projectID == "" {
			t.Fatal("shared fixture: no project available (creation may have failed; see stderr)")
		}
	}

	var created []string
	for len(f.serverIDs) < n {
		// Deliberately mixed case: the hostname attribute is Required and not
		// Computed, so the provider would produce an inconsistent result if the API
		// normalized case. Every test that borrows this fixture exercises that
		// round-trip for free, and TestAccServer_HostnameCase asserts it explicitly.
		hostname := fmt.Sprintf("tf-Acc-Shared-%d.latitude.sh", len(f.serverIDs)+1)

		sites := testServerSiteFallbackOrder
		if f.site != "" {
			// Later servers must colocate with the first one.
			sites = []string{f.site}
		}

		var serverID string
		var lastErr error
		for _, candidate := range sites {
			plan := operations.CreateServerPlan(testServerPlan)
			siteAttr := operations.CreateServerSite(candidate)
			osAttr := operations.CreateServerOperatingSystem(testServerOperatingSystem)
			billing := operations.CreateServerBilling("hourly")

			result, err := client.Servers.Create(ctx, operations.CreateServerServersRequestBody{
				Data: &operations.CreateServerServersData{
					Type: operations.CreateServerServersTypeServers,
					Attributes: &operations.CreateServerServersAttributes{
						Project:         f.projectID,
						Plan:            plan,
						Site:            siteAttr,
						OperatingSystem: osAttr,
						Hostname:        hostname,
						Billing:         &billing,
					},
				},
			})
			if err != nil {
				lastErr = err
				if isServersOutOfStockError(err) {
					t.Logf("shared fixture: %s out of stock for %s, trying next site", candidate, testServerPlan)
					continue
				}
				t.Fatalf("shared fixture: creating server at %s: %s", candidate, err)
			}
			if result.Server == nil || result.Server.Data == nil || result.Server.Data.ID == nil {
				t.Fatal("shared fixture: server create response missing ID")
			}
			serverID = *result.Server.Data.ID
			f.site = candidate
			break
		}
		if serverID == "" {
			t.Fatalf("shared fixture: no stock for %s in any of %v: %s", testServerPlan, sites, lastErr)
		}

		f.serverIDs = append(f.serverIDs, serverID)
		f.hostnames = append(f.hostnames, hostname)
		created = append(created, serverID)
	}

	for _, id := range created {
		testAccWaitServerReady(t, id)
	}

	return f.projectID, f.site, append([]string(nil), f.serverIDs[:n]...)
}

// testAccSharedServerHostname returns the hostname requested for the shared server
// at index i, so a test can compare it against what the API reports.
func testAccSharedServerHostname(i int) string {
	f := &testSharedFixture
	f.mu.Lock()
	defer f.mu.Unlock()

	if i < 0 || i >= len(f.hostnames) {
		return ""
	}
	return f.hostnames[i]
}

// testAccWaitServerReady polls the server until it reports status "on".
func testAccWaitServerReady(t *testing.T, serverID string) {
	t.Helper()

	client := createVCRClient(nil)
	ctx := context.Background()
	deadline := time.Now().Add(20 * time.Minute)
	notFound := 0

	for {
		response, err := client.Servers.Get(ctx, serverID, nil)
		if err != nil {
			// A vanished server means the platform gave up on the deploy;
			// waiting any longer is pointless.
			if strings.Contains(err.Error(), "404") || strings.Contains(strings.ToLower(err.Error()), "not_found") {
				notFound++
				if notFound >= 3 {
					t.Fatalf("shared fixture: server %s disappeared while deploying (deploy failed on the platform side)", serverID)
				}
			}
		} else if response.Server != nil && response.Server.Data != nil &&
			response.Server.Data.Attributes != nil && response.Server.Data.Attributes.Status != nil {
			notFound = 0
			status := string(*response.Server.Data.Attributes.Status)
			if status == "on" {
				return
			}
			t.Logf("shared fixture: server %s status %s, waiting...", serverID, status)
		}
		if time.Now().After(deadline) {
			t.Fatalf("shared fixture: server %s not ready after 20m", serverID)
		}
		time.Sleep(15 * time.Second)
	}
}

func testSharedFixtureTeardown() {
	client := createVCRClient(nil)
	ctx := context.Background()

	f := &testSharedFixture
	f.mu.Lock()
	serverIDs := append([]string(nil), f.serverIDs...)
	f.mu.Unlock()

	for _, id := range serverIDs {
		if _, err := client.Servers.Delete(ctx, id, nil); err != nil {
			fmt.Fprintf(os.Stderr, "shared fixture teardown: failed to delete server %s: %s\n", id, err)
		}
	}

	// Only a project this run created is torn down; the replay-mode default is left
	// untouched. The project delete is driven by the singleton state, not the
	// server fixture, because a test can create the project via testAccProjectID()
	// without ever touching the shared servers.
	if !testProjectCreated {
		return
	}

	// Bare-metal deletion is asynchronous, and deleting a project that still holds
	// servers is rejected. Wait for the fixture's servers to disappear before
	// deleting the project, so the run leaves nothing behind. Servers created by
	// individual tests are expected to be gone already via their own CheckDestroy;
	// if one leaked, the project delete below logs the failure rather than hanging.
	testAccWaitServersGone(ctx, client, serverIDs)

	if _, err := client.Projects.Delete(ctx, testProjectID); err != nil {
		fmt.Fprintf(os.Stderr, "shared fixture teardown: failed to delete project %s: %s\n", testProjectID, err)
	}
}

// testAccWaitServersGone blocks until each server 404s or a deadline passes. It
// never fails the run — teardown is best-effort cleanup — but a project delete
// attempted before its servers are gone would be rejected, so it is worth waiting.
func testAccWaitServersGone(ctx context.Context, client *latitudeshgosdk.Latitudesh, serverIDs []string) {
	deadline := time.Now().Add(20 * time.Minute)
	for _, id := range serverIDs {
		for {
			_, err := client.Servers.Get(ctx, id, nil)
			if err != nil && (strings.Contains(err.Error(), "404") ||
				strings.Contains(strings.ToLower(err.Error()), "not_found")) {
				break
			}
			if time.Now().After(deadline) {
				fmt.Fprintf(os.Stderr, "shared fixture teardown: server %s still present after 20m; "+
					"project delete may fail\n", id)
				break
			}
			time.Sleep(15 * time.Second)
		}
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	testSharedFixtureTeardown()
	os.Exit(code)
}
