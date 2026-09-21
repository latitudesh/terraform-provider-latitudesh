package latitudesh

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
)

// The LKS node pool endpoints accept an empty collection as "remove all of
// these" — but the SDK cannot send one. UpdateLksNodePoolAttributes tags both
// Labels and Taints `omitempty`, and Go omits a nil map/slice and an empty one
// alike, so no value the provider puts in the struct reaches the wire as
// `"taints": []`. Removing a taint therefore planned as an in-place update,
// PATCHed nothing, and failed the apply on the read-back ("inconsistent result
// after apply"), with the taint still attached.
//
// This rewrites the outgoing PATCH to carry the empty collection the API wants.
// The signal travels in the request context rather than a header, so nothing
// provider-internal leaks onto the wire and no SDK surface has to exist for it.
//
// The real fix is upstream: mark the fields nullable in the swagger so the
// generated model can express the difference between "unchanged" and "empty".
// TestLksNodePoolUpdateCannotClearCollections fails the day that lands, which
// is the signal to delete this file.

type lksClearKey struct{}

// lksClear says which collections the pending update means to empty.
type lksClear struct {
	Labels bool
	Taints bool
}

func (c lksClear) any() bool { return c.Labels || c.Taints }

// withLksClear marks a context so the transport rewrites the PATCH it carries.
// A no-op clear returns the context untouched, so the common path allocates
// nothing and the rewrite only ever runs when it is needed.
func withLksClear(ctx context.Context, c lksClear) context.Context {
	if !c.any() {
		return ctx
	}
	return context.WithValue(ctx, lksClearKey{}, c)
}

func lksClearFromContext(ctx context.Context) (lksClear, bool) {
	c, ok := ctx.Value(lksClearKey{}).(lksClear)
	return c, ok
}

// lksNodePoolUpdatePath matches PATCH /lks/clusters/{id}/nodepools/{id}. Scoped
// deliberately: a stray context value must not be able to rewrite an unrelated
// request.
var lksNodePoolUpdatePath = regexp.MustCompile(`^/lks/clusters/[^/]+/nodepools/[^/]+$`)

type lksClearCollectionsTransport struct {
	base http.RoundTripper
}

func (t *lksClearCollectionsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clear, ok := lksClearFromContext(req.Context())
	if !ok || !clear.any() ||
		req.Method != http.MethodPatch ||
		req.Body == nil ||
		!lksNodePoolUpdatePath.MatchString(req.URL.Path) {
		return t.base.RoundTrip(req)
	}

	body, err := io.ReadAll(req.Body)
	req.Body.Close()
	if err != nil {
		return nil, err
	}

	rewritten, changed := lksInjectEmptyCollections(body, clear)
	if !changed {
		// Put the body back regardless: it has already been consumed.
		req.Body = io.NopCloser(bytes.NewReader(body))
		return t.base.RoundTrip(req)
	}

	req.Body = io.NopCloser(bytes.NewReader(rewritten))
	req.ContentLength = int64(len(rewritten))
	req.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
	// Retries re-read the body, so it has to be replayable.
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(rewritten)), nil
	}

	return t.base.RoundTrip(req)
}

// lksInjectEmptyCollections adds the empty collections the SDK dropped. It only
// ever ADDS a key the payload lacks: a PATCH that already carries taints is
// setting them to something, and must not be overwritten with an empty list.
func lksInjectEmptyCollections(body []byte, clear lksClear) ([]byte, bool) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return body, false
	}
	rawData, ok := envelope["data"]
	if !ok {
		return body, false
	}

	var data map[string]json.RawMessage
	if err := json.Unmarshal(rawData, &data); err != nil {
		return body, false
	}

	attrs := map[string]json.RawMessage{}
	if rawAttrs, ok := data["attributes"]; ok {
		if err := json.Unmarshal(rawAttrs, &attrs); err != nil {
			return body, false
		}
	}

	changed := false
	if clear.Taints {
		if _, present := attrs["taints"]; !present {
			attrs["taints"] = json.RawMessage(`[]`)
			changed = true
		}
	}
	if clear.Labels {
		if _, present := attrs["labels"]; !present {
			attrs["labels"] = json.RawMessage(`{}`)
			changed = true
		}
	}
	if !changed {
		return body, false
	}

	encodedAttrs, err := json.Marshal(attrs)
	if err != nil {
		return body, false
	}
	data["attributes"] = encodedAttrs

	encodedData, err := json.Marshal(data)
	if err != nil {
		return body, false
	}
	envelope["data"] = encodedData

	out, err := json.Marshal(envelope)
	if err != nil {
		return body, false
	}
	return out, true
}
