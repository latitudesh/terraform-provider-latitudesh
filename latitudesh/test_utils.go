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

// Helper functions for testing - these are placeholder implementations
// since the actual API methods might not exist or work exactly as expected

// Mock member type for testing
type MockMember struct {
	ID *string `json:"id"`
}

// Mock tag type for testing
type MockTag struct {
	ID *string `json:"id"`
}

func GetMember(ctx context.Context, client *latitudeshgosdk.Latitudesh, memberID string) (*MockMember, error) {
	// This is a placeholder function for testing
	// In a real implementation, you would call the actual API method
	// For now, we'll return a simple error to indicate the member doesn't exist
	return nil, fmt.Errorf("member not found")
}

func GetTag(ctx context.Context, client *latitudeshgosdk.Latitudesh, tagID string) (*MockTag, error) {
	// This is a placeholder function for testing
	// In a real implementation, you would call the actual API method
	// For now, we'll return a simple error to indicate the tag doesn't exist
	return nil, fmt.Errorf("tag not found")
}
