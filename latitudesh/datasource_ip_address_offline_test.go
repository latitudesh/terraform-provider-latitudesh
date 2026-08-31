package latitudesh

import (
	"context"
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func TestIPAddressAssignmentValue(t *testing.T) {
	ctx := context.Background()

	t.Run("nil pointer is null", func(t *testing.T) {
		obj, diags := ipAddressAssignmentValue(ctx, nil)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if !obj.IsNull() {
			t.Error("expected a null object for a nil *Assignment")
		}
	})

	t.Run("empty object (unassigned) is known with null fields, not null", func(t *testing.T) {
		// The API returns an empty {} rather than omitting assignment entirely
		// when the IP is not assigned to an active server.
		obj, diags := ipAddressAssignmentValue(ctx, &components.Assignment{})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if obj.IsNull() {
			t.Fatal("expected a known object for an empty (non-nil) *Assignment; got null")
		}

		var model IPAddressAssignmentModel
		if d := obj.As(ctx, &model, basicObjectAsOptions); d.HasError() {
			t.Fatalf("As: %v", d)
		}
		if !model.ServerID.IsNull() || !model.Hostname.IsNull() || !model.AssignedAt.IsNull() {
			t.Errorf("expected all fields null for an empty assignment, got %+v", model)
		}
	})

	t.Run("populated assignment maps every field", func(t *testing.T) {
		serverID := "sv_1"
		hostname := "web-01"
		assignedAt := "2026-01-01T00:00:00Z"
		obj, diags := ipAddressAssignmentValue(ctx, &components.Assignment{
			ServerID:   &serverID,
			Hostname:   &hostname,
			AssignedAt: &assignedAt,
		})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}

		var model IPAddressAssignmentModel
		if d := obj.As(ctx, &model, basicObjectAsOptions); d.HasError() {
			t.Fatalf("As: %v", d)
		}
		if model.ServerID.ValueString() != serverID {
			t.Errorf("server_id = %q, want %q", model.ServerID.ValueString(), serverID)
		}
		if model.Hostname.ValueString() != hostname {
			t.Errorf("hostname = %q, want %q", model.Hostname.ValueString(), hostname)
		}
		if model.AssignedAt.ValueString() != assignedAt {
			t.Errorf("assigned_at = %q, want %q", model.AssignedAt.ValueString(), assignedAt)
		}
	})
}

func TestIPAddressElasticValue(t *testing.T) {
	ctx := context.Background()

	obj, diags := ipAddressElasticValue(ctx, nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !obj.IsNull() {
		t.Error("expected a null object for a nil *Elastic")
	}

	obj, diags = ipAddressElasticValue(ctx, &components.Elastic{})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if obj.IsNull() {
		t.Fatal("expected a known object for an empty (non-nil) *Elastic; got null")
	}
}

func TestIPAddressRegionValue(t *testing.T) {
	ctx := context.Background()

	obj, diags := ipAddressRegionValue(ctx, nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !obj.IsNull() {
		t.Error("expected a null object for a nil *IPAddressDataRegion")
	}

	id, name, slug := "reg_1", "Ashburn", "ASH"
	obj, diags = ipAddressRegionValue(ctx, &components.IPAddressDataRegion{
		ID:   &id,
		Name: &name,
		Location: &components.IPAddressDataLocation{
			Slug: &slug,
		},
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	var model IPAddressRegionModel
	if d := obj.As(ctx, &model, basicObjectAsOptions); d.HasError() {
		t.Fatalf("As: %v", d)
	}
	if model.Name.ValueString() != name {
		t.Errorf("region name = %q, want %q", model.Name.ValueString(), name)
	}
	var loc IPAddressLocationModel
	if d := model.Location.As(ctx, &loc, basicObjectAsOptions); d.HasError() {
		t.Fatalf("location As: %v", d)
	}
	if loc.Slug.ValueString() != slug {
		t.Errorf("location slug = %q, want %q", loc.Slug.ValueString(), slug)
	}
}

// TestMapIPAddressToModel exercises the top-level mapping end to end,
// including the pointer-family/type enums that get stringified rather than
// passed through as-is.
func TestMapIPAddressToModel(t *testing.T) {
	ctx := context.Background()

	id := "ip_mgmt1"
	address := "203.0.113.10"
	family := components.IPAddressDataFamilyIPv4
	typ := components.IPAddressDataAttributesTypePublic
	serverID := "sv_1"

	ip := &components.IPAddressData{
		ID: &id,
		Attributes: &components.IPAddressDataAttributes{
			Address:    &address,
			Family:     &family,
			Type:       &typ,
			Management: ipBoolPtr(true),
			Assignment: &components.Assignment{ServerID: &serverID},
			Elastic:    &components.Elastic{},
		},
	}

	ds := &IPAddressDataSource{}
	var data IPAddressDataSourceModel
	diags := ds.mapIPAddressToModel(ctx, ip, &data)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	if data.ID.ValueString() != id {
		t.Errorf("id = %q, want %q", data.ID.ValueString(), id)
	}
	if data.Address.ValueString() != address {
		t.Errorf("address = %q, want %q", data.Address.ValueString(), address)
	}
	if data.Family.ValueString() != "IPv4" {
		t.Errorf("family = %q, want %q", data.Family.ValueString(), "IPv4")
	}
	if data.Type.ValueString() != "Public" {
		t.Errorf("type = %q, want %q", data.Type.ValueString(), "Public")
	}
	if !data.Management.ValueBool() {
		t.Error("management = false, want true")
	}
	if data.CreatedAt.IsNull() == false && data.CreatedAt.ValueString() != "" {
		t.Errorf("created_at should be null when Attributes.CreatedAt is nil, got %q", data.CreatedAt.ValueString())
	}

	var assignment IPAddressAssignmentModel
	if d := data.Assignment.As(ctx, &assignment, basicObjectAsOptions); d.HasError() {
		t.Fatalf("assignment As: %v", d)
	}
	if assignment.ServerID.ValueString() != serverID {
		t.Errorf("assignment.server_id = %q, want %q", assignment.ServerID.ValueString(), serverID)
	}
}
