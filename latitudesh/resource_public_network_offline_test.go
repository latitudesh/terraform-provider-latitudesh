package latitudesh

import (
	"encoding/json"
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

const publicNetworkAttrsPayload = `{
  "ipv4": "203.0.113.0/27",
  "ipv6": "2001:db8:1234::/64",
  "size": 27,
  "activated": true,
  "capacity": 30,
  "ips_used": 2,
  "ips_free": 28,
  "created_at": "2026-01-02T15:04:05Z",
  "project": {"id": "proj_123", "name": "Test", "slug": "test"},
  "region": {"id": "reg_1", "name": "SAO2", "location": {"id": "loc_1", "name": "Sao Paulo", "slug": "sao2"}}
}`

func TestMapPublicNetworkAttributes(t *testing.T) {
	var attrs components.PublicNetworkDataAttributes
	if err := json.Unmarshal([]byte(publicNetworkAttrsPayload), &attrs); err != nil {
		t.Fatalf("unmarshaling public network attributes: %s", err)
	}

	got := mapPublicNetworkAttributes(&attrs)

	if got.Ipv4.ValueString() != "203.0.113.0/27" {
		t.Errorf("Ipv4 = %q, want 203.0.113.0/27", got.Ipv4.ValueString())
	}
	if got.Ipv6.ValueString() != "2001:db8:1234::/64" {
		t.Errorf("Ipv6 = %q, want 2001:db8:1234::/64", got.Ipv6.ValueString())
	}
	if got.Size.ValueInt64() != 27 {
		t.Errorf("Size = %d, want 27", got.Size.ValueInt64())
	}
	if !got.Activated.ValueBool() {
		t.Error("Activated = false, want true")
	}
	if got.Capacity.ValueInt64() != 30 {
		t.Errorf("Capacity = %d, want 30", got.Capacity.ValueInt64())
	}
	if got.IpsUsed.ValueInt64() != 2 {
		t.Errorf("IpsUsed = %d, want 2", got.IpsUsed.ValueInt64())
	}
	if got.IpsFree.ValueInt64() != 28 {
		t.Errorf("IpsFree = %d, want 28", got.IpsFree.ValueInt64())
	}
	if got.CreatedAt.ValueString() != "2026-01-02T15:04:05Z" {
		t.Errorf("CreatedAt = %q, want 2026-01-02T15:04:05Z", got.CreatedAt.ValueString())
	}
	if got.Project.ValueString() != "proj_123" {
		t.Errorf("Project = %q, want proj_123", got.Project.ValueString())
	}
	if got.RegionSlug.ValueString() != "sao2" {
		t.Errorf("RegionSlug = %q, want sao2", got.RegionSlug.ValueString())
	}
}

// TestMapPublicNetworkAttributesNilSafety guards the nil-check contract:
// a nil attributes pointer (e.g. Create returning a bare envelope) must
// produce all-null values rather than panicking.
func TestMapPublicNetworkAttributesNilSafety(t *testing.T) {
	got := mapPublicNetworkAttributes(nil)

	if !got.Ipv4.IsNull() || !got.Ipv6.IsNull() || !got.Size.IsNull() || !got.Activated.IsNull() ||
		!got.Capacity.IsNull() || !got.IpsUsed.IsNull() || !got.IpsFree.IsNull() ||
		!got.CreatedAt.IsNull() || !got.Project.IsNull() || !got.RegionSlug.IsNull() {
		t.Error("mapPublicNetworkAttributes(nil) produced a non-null field; want all-null")
	}
}

// TestMapPublicNetworkAttributesEmptyObject guards against a populated
// attributes struct whose nested pointers (project/region) are nil — e.g. a
// list response that omits the relationship expansion.
func TestMapPublicNetworkAttributesEmptyObject(t *testing.T) {
	got := mapPublicNetworkAttributes(&components.PublicNetworkDataAttributes{})

	if !got.Project.IsNull() {
		t.Errorf("Project = %q, want null when attrs.Project is nil", got.Project.ValueString())
	}
	if !got.RegionSlug.IsNull() {
		t.Errorf("RegionSlug = %q, want null when attrs.Region is nil", got.RegionSlug.ValueString())
	}
}
