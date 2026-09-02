package latitudesh

import (
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func TestMatchObjectStorageByName(t *testing.T) {
	data := []components.ObjectStorageData{
		{ID: strPtr("bkt_1"), Attributes: &components.ObjectStorageDataAttributes{Name: strPtr("app-backups")}},
		{ID: strPtr("bkt_2"), Attributes: &components.ObjectStorageDataAttributes{Name: strPtr(" logs ")}},
		{ID: strPtr("bkt_3"), Attributes: nil},
	}

	cases := []struct {
		name   string
		query  string
		wantID string
	}{
		{"exact match", "app-backups", "bkt_1"},
		{"match ignores surrounding whitespace on stored name", "logs", "bkt_2"},
		{"no match", "does-not-exist", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchObjectStorageByName(data, tc.query)
			if tc.wantID == "" {
				if got != nil {
					t.Fatalf("matchObjectStorageByName(%q) = %v, want nil", tc.query, got)
				}
				return
			}
			if got == nil || got.ID == nil || *got.ID != tc.wantID {
				t.Fatalf("matchObjectStorageByName(%q) = %v, want ID %q", tc.query, got, tc.wantID)
			}
		})
	}
}

// A bucket with no Attributes (envelope-only payload) must be skipped rather
// than dereferencing a nil pointer.
func TestMatchObjectStorageByNameNilAttributes(t *testing.T) {
	data := []components.ObjectStorageData{
		{ID: strPtr("bkt_3"), Attributes: nil},
	}

	if got := matchObjectStorageByName(data, "anything"); got != nil {
		t.Fatalf("matchObjectStorageByName with nil Attributes = %v, want nil", got)
	}
}
