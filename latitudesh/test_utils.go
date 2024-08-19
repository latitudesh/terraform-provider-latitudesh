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
	api "github.com/latitudesh/latitudesh-go"
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
			c := api.NewClientWithAuth("latitudesh", authToken, &httpClient)
			c.UserAgent = fmt.Sprintf("%s/%s", userAgentForProvider, currentVersion)

			return c, diags
		}
		c := api.NewClientWithAuth("latitudesh", " ", &httpClient)

		return c, diags
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
