package latitudesh

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type captureTransport struct {
	body   string
	method string
	path   string
}

func (c *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.method, c.path = req.Method, req.URL.Path
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		c.body = string(b)
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Header:     http.Header{},
		Request:    req,
	}, nil
}

func lksPatchRequest(t *testing.T, ctx context.Context, path, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, "https://api.latitude.sh"+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("building request: %s", err)
	}
	return req
}

const lksPatchBody = `{"data":{"type":"lks_node_pools","attributes":{"count":2}}}`

func TestLksClearTransport(t *testing.T) {
	const poolPath = "/lks/clusters/lksc_1/nodepools/lksnp_1"

	cases := []struct {
		name        string
		ctx         context.Context
		path        string
		body        string
		wantTaints  bool
		wantLabels  bool
		wantUnknown []string
	}{
		{
			name:       "clears both when asked",
			ctx:        withLksClear(context.Background(), lksClear{Labels: true, Taints: true}),
			path:       poolPath,
			body:       lksPatchBody,
			wantTaints: true, wantLabels: true,
		},
		{
			name:       "clears only what was asked",
			ctx:        withLksClear(context.Background(), lksClear{Taints: true}),
			path:       poolPath,
			body:       lksPatchBody,
			wantTaints: true,
		},
		// No signal means no rewrite: the overwhelming majority of PATCHes.
		{
			name: "untouched without the signal",
			ctx:  context.Background(),
			path: poolPath,
			body: lksPatchBody,
		},
		// A stray context value must not reach an unrelated endpoint.
		{
			name: "untouched on another path",
			ctx:  withLksClear(context.Background(), lksClear{Labels: true, Taints: true}),
			path: "/lks/clusters/lksc_1",
			body: lksPatchBody,
		},
		// A PATCH that sets taints is setting them to something; an empty list
		// must never overwrite that.
		{
			name: "never overwrites a value already present",
			ctx:  withLksClear(context.Background(), lksClear{Taints: true}),
			path: poolPath,
			body: `{"data":{"type":"lks_node_pools","attributes":{"taints":[{"key":"a","effect":"NoSchedule"}]}}}`,
			wantUnknown: []string{
				`"key":"a"`,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := &captureTransport{}
			transport := &lksClearCollectionsTransport{base: capture}

			if _, err := transport.RoundTrip(lksPatchRequest(t, tc.ctx, tc.path, tc.body)); err != nil {
				t.Fatalf("round trip: %s", err)
			}

			var envelope struct {
				Data struct {
					Attributes map[string]json.RawMessage `json:"attributes"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(capture.body), &envelope); err != nil {
				t.Fatalf("body is not valid JSON after rewriting: %s (%s)", capture.body, err)
			}
			attrs := envelope.Data.Attributes

			if got := string(attrs["taints"]) == "[]"; got != tc.wantTaints {
				t.Errorf(`"taints":[] present = %v, want %v — body: %s`, got, tc.wantTaints, capture.body)
			}
			if got := string(attrs["labels"]) == "{}"; got != tc.wantLabels {
				t.Errorf(`"labels":{} present = %v, want %v — body: %s`, got, tc.wantLabels, capture.body)
			}
			for _, fragment := range tc.wantUnknown {
				if !strings.Contains(capture.body, fragment) {
					t.Errorf("rewriting dropped %s — body: %s", fragment, capture.body)
				}
			}
			// count is untouched in every case: the rewrite only ever adds a
			// key the payload lacks.
			if _, ok := attrs["count"]; !ok && strings.Contains(tc.body, "count") {
				t.Errorf("rewriting dropped an unrelated field — body: %s", capture.body)
			}
		})
	}
}

// Retries re-read the body, so a rewritten request has to stay replayable or
// the second attempt sends nothing.
func TestLksClearTransportBodyIsReplayable(t *testing.T) {
	capture := &captureTransport{}
	transport := &lksClearCollectionsTransport{base: capture}

	req := lksPatchRequest(t,
		withLksClear(context.Background(), lksClear{Taints: true}),
		"/lks/clusters/lksc_1/nodepools/lksnp_1", lksPatchBody)

	if _, err := transport.RoundTrip(req); err != nil {
		t.Fatalf("round trip: %s", err)
	}
	if req.GetBody == nil {
		t.Fatal("GetBody is nil after rewriting, so a retry would send an empty body")
	}
	replay, err := req.GetBody()
	if err != nil {
		t.Fatalf("GetBody: %s", err)
	}
	again, _ := io.ReadAll(replay)
	if string(again) != capture.body {
		t.Errorf("replayed body differs:\n got %s\nwant %s", again, capture.body)
	}
}

// A malformed body is passed through untouched rather than swallowed: the API
// gets to reject it, which is a clearer failure than a mangled payload.
func TestLksClearTransportLeavesMalformedBodyAlone(t *testing.T) {
	capture := &captureTransport{}
	transport := &lksClearCollectionsTransport{base: capture}

	if _, err := transport.RoundTrip(lksPatchRequest(t,
		withLksClear(context.Background(), lksClear{Taints: true}),
		"/lks/clusters/lksc_1/nodepools/lksnp_1", `not json`)); err != nil {
		t.Fatalf("round trip: %s", err)
	}
	if capture.body != "not json" {
		t.Errorf("body = %q, want it passed through untouched", capture.body)
	}
}
