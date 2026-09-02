package latitudesh

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// objectStorageSanitizerTransport works around an API contract violation on
// the object storage endpoints: the swagger declares `retention_period` as a
// nullable integer, but the backend persists whatever the storage provider
// echoes back and can emit "" (unset on VAST), "30" or "30d" instead of a
// number. Any of those strings makes the generated SDK fail to unmarshal the
// whole response ("cannot unmarshal string into Go value of type int64"), so
// bucket payloads are rewritten here before the SDK sees them: day counts are
// coerced to integers and unparseable or unset values are dropped, matching
// the documented `integer | null` contract.
type objectStorageSanitizerTransport struct {
	base http.RoundTripper
}

func (t *objectStorageSanitizerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil || !isObjectStorageBucketPath(req.URL.Path) {
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

	if fixed, changed := sanitizeObjectStorageBody(body); changed {
		body = fixed
		resp.ContentLength = int64(len(body))
		resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))

	return resp, nil
}

// isObjectStorageBucketPath matches the collection and single-resource bucket
// endpoints (/storage/buckets and /storage/buckets/{id}), whose payloads carry
// the malformed attribute. Nested endpoints (access keys, lifecycle rules)
// have their own schemas and are left untouched.
func isObjectStorageBucketPath(p string) bool {
	const marker = "/storage/buckets"
	i := strings.Index(p, marker)
	if i < 0 {
		return false
	}
	rest := strings.Trim(p[i+len(marker):], "/")
	return rest == "" || !strings.Contains(rest, "/")
}

// sanitizeObjectStorageBody rewrites string retention_period values in a
// bucket payload (single resource or list). It reports whether the body was
// modified; anything unparseable passes through unchanged so the SDK surfaces
// the original error.
func sanitizeObjectStorageBody(body []byte) ([]byte, bool) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body, false
	}

	changed := false
	switch data := payload["data"].(type) {
	case map[string]any:
		changed = sanitizeObjectStorageEntry(data)
	case []any:
		for _, item := range data {
			if entry, ok := item.(map[string]any); ok && sanitizeObjectStorageEntry(entry) {
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

func sanitizeObjectStorageEntry(entry map[string]any) bool {
	attrs, ok := entry["attributes"].(map[string]any)
	if !ok {
		return false
	}
	raw, ok := attrs["retention_period"].(string)
	if !ok {
		return false
	}
	if days, ok := parseRetentionDays(raw); ok {
		attrs["retention_period"] = days
	} else {
		delete(attrs, "retention_period")
	}
	return true
}

// parseRetentionDays extracts the day count from the string shapes the
// backend is known to emit: "30" (echo of a client-sent string) and "30d"
// (VAST duration). Zero means no retention, so "0"/"0d" are treated as unset
// alongside "" and anything unrecognized.
func parseRetentionDays(raw string) (int64, bool) {
	s := strings.TrimSpace(raw)
	s = strings.TrimSuffix(s, "d")
	s = strings.TrimSuffix(s, "D")
	if s == "" {
		return 0, false
	}
	days, err := strconv.ParseInt(s, 10, 64)
	if err != nil || days <= 0 {
		return 0, false
	}
	return days, true
}
