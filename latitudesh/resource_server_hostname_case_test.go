package latitudesh

import (
	"context"
	"os"
	"testing"
)

// TestAccServer_HostnameCase asserts the API preserves hostname letter case.
//
// This matters because `hostname` is Required and not Computed: if the API
// normalized case, Terraform would reject every apply with "provider produced
// inconsistent result after apply" — the config would say `My-Server` and the
// refreshed state would say `my-server`. Before this test, nothing covered it and
// the schema change rested on observing that servers with uppercase hostnames
// already exist in the account.
//
// It provisions nothing. The shared fixture (see test_utils_shared_test.go) already
// deploys a server for the attachment-style tests and hands out mixed-case
// hostnames on purpose; this test borrows that server and compares what was
// requested against what the API reports. Asking for 1 server means it reuses one
// that another test already paid for whenever the suite runs as a whole.
func TestAccServer_HostnameCase(t *testing.T) {
	// This test drives the SDK directly rather than going through resource.Test, so
	// nothing gates it on TF_ACC for us. Without this guard it would fail an offline
	// `go test ./...` instead of skipping, the way TestAccServer_ProjectSlugConsistency
	// currently does.
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}
	testAccTokenCheck(t)

	_, _, serverIDs := testAccSharedServers(t, 1)

	requested := testAccSharedServerHostname(0)
	if requested == "" {
		t.Fatal("shared fixture did not record a hostname")
	}
	if requested == lower(requested) {
		t.Fatalf("this test is only meaningful with a mixed-case hostname, got %q", requested)
	}

	client := createVCRClient(nil)
	response, err := client.Servers.Get(context.Background(), serverIDs[0], nil)
	if err != nil {
		t.Fatalf("reading shared server %s: %v", serverIDs[0], err)
	}
	if response == nil || response.Server == nil || response.Server.Data == nil ||
		response.Server.Data.Attributes == nil || response.Server.Data.Attributes.Hostname == nil {
		t.Fatal("server response carried no hostname")
	}

	if got := *response.Server.Data.Attributes.Hostname; got != requested {
		t.Errorf("hostname round-trip changed the value: requested %q, API reports %q.\n"+
			"If this is a deliberate API change, `hostname` can no longer be Required "+
			"without a case-insensitive plan modifier — see internal/planmodifiers.",
			requested, got)
	}
}

// lower avoids pulling strings into this file for a single call.
func lower(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'A' && r <= 'Z' {
			out[i] = r + ('a' - 'A')
		}
	}
	return string(out)
}
