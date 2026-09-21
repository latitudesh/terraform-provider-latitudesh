package latitudesh

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// blockStorageSanitizerTransport works around an API contract violation on the
// block storage volume endpoints by rewriting volume payloads before the SDK
// unmarshals them.
//
// The swagger declares `namespace_id` as a nullable string, but the live API
// emits the NVMe namespace id as a number (`"namespace_id": 1`) on volumes
// provisioned through the initiators/mount flow. The SDK models the field as
// *string, so a single such volume fails the whole unmarshal ("cannot
// unmarshal number into Go struct field VolumeData.attributes of type
// string") — and because the collection endpoint returns one body, that takes
// down every volume in the project, not just the offending one. Numbers are
// coerced to their string form so the generated model can carry them.
//
// Volumes created through the current flow return `namespace_id` (and
// `connector_id`) as null and carry mapping state under `block` instead, so
// this only ever fires on older volumes — which are still readable, listable
// and importable, hence the workaround rather than dropping the attribute.
type blockStorageSanitizerTransport struct {
	base http.RoundTripper
}

func (t *blockStorageSanitizerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil || !isBlockStorageVolumePath(req.URL.Path) {
		return resp, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, nil
	}

	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}

	if fixed, changed := sanitizeBlockStorageBody(body); changed {
		body = fixed
		resp.ContentLength = int64(len(body))
		resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))

	return resp, nil
}

// isBlockStorageVolumePath matches every endpoint that answers with a volume
// payload: the collection and single-resource paths (/storage/volumes and
// /storage/volumes/{id}) plus the map/mount sub-resources, which echo the same
// envelope back.
func isBlockStorageVolumePath(p string) bool {
	return strings.Contains(p, "/storage/volumes")
}

// sanitizeBlockStorageBody coerces numeric namespace_id values in a volume
// payload (single resource or list) to strings. It reports whether the body
// was modified; anything it cannot parse passes through unchanged so the SDK
// surfaces the original error.
func sanitizeBlockStorageBody(body []byte) ([]byte, bool) {
	// UseNumber keeps every other number in its original literal form, so
	// re-marshaling cannot reformat sizes or ids we are not touching.
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()

	var payload map[string]any
	if err := dec.Decode(&payload); err != nil {
		return body, false
	}

	changed := false
	switch data := payload["data"].(type) {
	case map[string]any:
		changed = sanitizeBlockStorageEntry(data)
	case []any:
		for _, item := range data {
			if entry, ok := item.(map[string]any); ok && sanitizeBlockStorageEntry(entry) {
				changed = true
			}
		}
	}
	if !changed {
		return body, false
	}

	fixed, err := json.Marshal(payload)
	if err != nil {
		return body, false
	}
	return fixed, true
}

func sanitizeBlockStorageEntry(entry map[string]any) bool {
	attrs, ok := entry["attributes"].(map[string]any)
	if !ok {
		return false
	}

	if raw, ok := attrs["namespace_id"].(json.Number); ok {
		attrs["namespace_id"] = raw.String()
		return true
	}
	return false
}
