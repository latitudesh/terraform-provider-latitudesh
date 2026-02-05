package latitudesh

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
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

func testProviderConfigure(rec *recorder.Recorder) func(ctx context.Context, d *schema.ResourceData) (interface{}, diag.Diagnostics) {
	return func(ctx context.Context, d *schema.ResourceData) (interface{}, diag.Diagnostics) {
		authToken := d.Get("auth_token").(string)

		var diags diag.Diagnostics

		httpClient := *http.DefaultClient
		httpClient.Transport = rec

		if authToken != "" {
			sdkClient := latitudeshgosdk.New(
				latitudeshgosdk.WithSecurity(authToken),
				latitudeshgosdk.WithClient(&httpClient),
			)
			return sdkClient, diags
		}

		sdkClient := latitudeshgosdk.New(
			latitudeshgosdk.WithSecurity(""),
			latitudeshgosdk.WithClient(&httpClient),
		)

		return sdkClient, diags
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
