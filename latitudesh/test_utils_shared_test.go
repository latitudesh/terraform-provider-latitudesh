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
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	"gopkg.in/dnaeon/go-vcr.v3/recorder"
)

// testRunID is appended to created resource names: the API rejects duplicate
// project names, so fixed names collide with parallel runs or with leftovers
// from aborted ones.
var testRunID = acctest.RandString(6)

// defaultTestProjectID is the pre-existing project (DevTools QA) that all
// acceptance tests share. Concentrating tests in one project avoids creating a
// throwaway project per test — those leak on aborted runs and then collide on
// the API's unique-project-name constraint.
const defaultTestProjectID = "proj_jv6m5JyZDNLPe"

// testAccProjectID returns the shared project ID. Override with
// LATITUDESH_TEST_PROJECT to run the suite against a different account.
func testAccProjectID() string {
	if v := os.Getenv("LATITUDESH_TEST_PROJECT"); v != "" {
		return v
	}
	return defaultTestProjectID
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
	}

	var created []string
	for len(f.serverIDs) < n {
		hostname := fmt.Sprintf("tf-acc-shared-%d.latitude.sh", len(f.serverIDs)+1)

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
						Project:         &f.projectID,
						Plan:            &plan,
						Site:            &siteAttr,
						OperatingSystem: &osAttr,
						Hostname:        &hostname,
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
		created = append(created, serverID)
	}

	for _, id := range created {
		testAccWaitServerReady(t, id)
	}

	return f.projectID, f.site, append([]string(nil), f.serverIDs[:n]...)
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
	f := &testSharedFixture
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.projectID == "" {
		return
	}

	client := createVCRClient(nil)
	ctx := context.Background()

	// Only the servers this fixture provisioned are torn down. The project is
	// pre-existing and shared (see testAccProjectID) — it must survive the run.
	for _, id := range f.serverIDs {
		if _, err := client.Servers.Delete(ctx, id, nil); err != nil {
			fmt.Fprintf(os.Stderr, "shared fixture teardown: failed to delete server %s: %s\n", id, err)
		}
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	testSharedFixtureTeardown()
	os.Exit(code)
}
