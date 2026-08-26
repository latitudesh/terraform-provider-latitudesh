package sdkcoverage

import (
	"encoding/json"
	"testing"
)

// TestReportJSONContract pins the shape automation relies on: stable field names,
// a suggested type name on every pending entry, counts that match the buckets,
// and arrays that serialize as [] rather than null so jq filters stay simple.
func TestReportJSONContract(t *testing.T) {
	surface := surfaceOf(map[string][]string{
		"Servers":         {"Create", "Get", "Delete"},
		"ManagedDatabase": {"CreateDatabase", "GetDatabase", "DeleteDatabase"},
	})
	manifest := Manifest{Groups: map[string]Entry{
		"Servers": {ImplementedBy: []string{"latitudesh_server"}},
	}}
	report := Reconcile(surface, manifest, []string{"latitudesh_server"})

	data, err := report.JSON("v1.2.3", "latitudesh")
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}

	var doc struct {
		SDKModule  string `json:"sdk_module"`
		SDKVersion string `json:"sdk_version"`
		OK         bool   `json:"ok"`
		Counts     struct {
			Total   int `json:"total"`
			Covered int `json:"covered"`
			Pending int `json:"pending"`
		} `json:"counts"`
		Violations []any `json:"violations"`
		Pending    []struct {
			Group             string   `json:"group"`
			CRUD              string   `json:"crud"`
			Generates         []string `json:"generates"`
			Methods           []string `json:"methods"`
			SuggestedTypeName string   `json:"suggested_type_name"`
		} `json:"pending"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, data)
	}

	if doc.SDKModule != SDKModulePath {
		t.Errorf("sdk_module = %q, want %q", doc.SDKModule, SDKModulePath)
	}
	if doc.SDKVersion != "v1.2.3" {
		t.Errorf("sdk_version = %q, want v1.2.3", doc.SDKVersion)
	}
	if !doc.OK {
		t.Errorf("ok = false, want true for a clean reconcile")
	}
	if doc.Counts.Total != 2 || doc.Counts.Covered != 1 || doc.Counts.Pending != 1 {
		t.Errorf("counts = %+v, want total 2 / covered 1 / pending 1", doc.Counts)
	}
	if len(doc.Pending) != 1 {
		t.Fatalf("pending has %d entries, want 1", len(doc.Pending))
	}

	got := doc.Pending[0]
	if got.Group != "ManagedDatabase" {
		t.Errorf("pending group = %q, want ManagedDatabase", got.Group)
	}
	if got.SuggestedTypeName != "latitudesh_managed_database" {
		t.Errorf("suggested_type_name = %q, want latitudesh_managed_database", got.SuggestedTypeName)
	}
	if got.CRUD != "CR-D" {
		t.Errorf("crud = %q, want CR-D", got.CRUD)
	}
}

// A non-nil empty slice must marshal to [] so `jq '.pending | length'` never trips
// over null on a fully covered SDK.
func TestReportJSONEmptyArraysAreNotNull(t *testing.T) {
	surface := surfaceOf(map[string][]string{
		"Servers": {"Create", "Get", "Delete"},
	})
	manifest := Manifest{Groups: map[string]Entry{
		"Servers": {ImplementedBy: []string{"latitudesh_server"}},
	}}
	report := Reconcile(surface, manifest, []string{"latitudesh_server"})

	data, err := report.JSON("", "latitudesh")
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Require the key to be present AND exactly [] — a "not null" check alone
	// would also pass if the field were dropped from the document entirely.
	for _, key := range []string{"violations", "pending", "unshaped", "revisit"} {
		got, ok := raw[key]
		if !ok || string(got) != "[]" {
			t.Errorf("%s = %q, want a present, empty []", key, got)
		}
	}
}
