package latitudesh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"testing"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	"golang.org/x/crypto/ssh"
	"gopkg.in/dnaeon/go-vcr.v3/cassette"
	"gopkg.in/dnaeon/go-vcr.v3/recorder"
)

const (
	testRecorderEnv = "LATITUDE_TEST_RECORDER"

	testRecorderRecord   = "record"
	testRecorderPlay     = "play"
	testRecorderDisabled = "disabled"
	recorderDefaultMode  = recorder.ModePassthrough
)

func testRecordMode() (recorder.Mode, error) {
	modeRaw := os.Getenv(testRecorderEnv)
	mode := recorderDefaultMode

	switch strings.ToLower(modeRaw) {
	case testRecorderRecord:
		mode = recorder.ModeRecordOnly
	case testRecorderPlay:
		mode = recorder.ModeReplayOnly
	case "":
		// no-op
	case testRecorderDisabled:
		// no-op
	default:
		return mode, fmt.Errorf("invalid %s mode: %s", testRecorderEnv, modeRaw)
	}
	return mode, nil
}

func testRecorder(name string, mode recorder.Mode) (*recorder.Recorder, func()) {
	rOptions := recorder.Options{
		CassetteName:  path.Join("fixtures", name),
		Mode:          mode,
		RealTransport: nil,
	}

	r, err := recorder.NewWithOptions(&rOptions)
	if err != nil {
		log.Fatal(err)
	}

	r.AddHook(func(i *cassette.Interaction) error {
		if i.Request.Headers.Get("Authorization") != "" {
			i.Request.Headers.Set("Authorization", "[REDACTED]")
		}

		return nil
	}, recorder.BeforeSaveHook)

	return r, func() {
		if err := r.Stop(); err != nil {
			log.Fatal(err)
		}
	}
}

func createTestRecorder(t *testing.T) (*recorder.Recorder, func()) {
	name := t.Name()
	mode, err := testRecordMode()
	if err != nil {
		t.Fatal(err)
	}
	return testRecorder(name, mode)
}

// createTestRecorderWithSite cria recorder VCR com nome específico incluindo site
func createTestRecorderWithSite(t *testing.T, site string) (*recorder.Recorder, func()) {
	baseName := t.Name()
	nameWithSite := fmt.Sprintf("%s_%s", baseName, site)

	mode, err := testRecordMode()
	if err != nil {
		t.Fatal(err)
	}

	return testRecorder(nameWithSite, mode)
}

// testGenerateSSHPublicKey generates a throwaway ed25519 public key in
// authorized_keys format so SSH key tests never depend on external key material.
func testGenerateSSHPublicKey(t *testing.T) string {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating ed25519 key: %s", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("converting to SSH public key: %s", err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
}

// testAccCreateProject provisions a project via the API for tests that need a
// pre-existing project (e.g. to exercise the provider-level default project,
// which cannot reference resources created in the same config). The returned
// cleanup function deletes it. Callers must only invoke it when TF_ACC is set.
func testAccCreateProject(t *testing.T, name string) (string, func()) {
	t.Helper()

	// This helper provisions a real project with a raw SDK client, which
	// cannot be served from VCR cassettes.
	if mode, err := testRecordMode(); err == nil && mode == recorder.ModeReplayOnly {
		t.Skip("testAccCreateProject requires live API access; not available in VCR replay mode")
	}

	client := createVCRClient(nil)
	env := operations.CreateProjectEnvironmentDevelopment

	result, err := client.Projects.Create(context.Background(), operations.CreateProjectProjectsRequestBody{
		Data: &operations.CreateProjectProjectsData{
			Type: operations.CreateProjectProjectsTypeProjects,
			Attributes: &operations.CreateProjectProjectsAttributes{
				Name:             name,
				ProvisioningType: operations.CreateProjectProvisioningTypeOnDemand,
				Environment:      &env,
			},
		},
	})
	if err != nil {
		t.Fatalf("creating test project: %s", err)
	}
	if result.Object == nil || result.Object.Data == nil || result.Object.Data.ID == nil {
		t.Fatal("test project create response missing ID")
	}

	id := *result.Object.Data.ID
	return id, func() {
		if _, err := client.Projects.Delete(context.Background(), id); err != nil {
			t.Logf("cleanup: failed to delete test project %s: %s", id, err)
		}
	}
}

// createVCRClient creates a Latitude.sh SDK client with VCR recording/playback
func createVCRClient(recorder *recorder.Recorder) *latitudeshgosdk.Latitudesh {
	authToken := os.Getenv("LATITUDESH_AUTH_TOKEN")
	if authToken == "" {
		authToken = "test" // Use test token for VCR playback
	}

	if recorder != nil {
		httpClient := *http.DefaultClient
		httpClient.Transport = recorder

		return latitudeshgosdk.New(
			latitudeshgosdk.WithSecurity(authToken),
			latitudeshgosdk.WithClient(&httpClient),
		)
	}

	// Use default client when no recorder is provided
	return latitudeshgosdk.New(
		latitudeshgosdk.WithSecurity(authToken),
	)
}
