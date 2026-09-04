package latitudesh

import (
	"context"
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func TestOperatingSystemFeaturesValueNil(t *testing.T) {
	ctx := context.Background()

	obj, diags := operatingSystemFeaturesValue(ctx, nil)
	if diags.HasError() {
		t.Fatalf("operatingSystemFeaturesValue(nil) diagnostics: %v", diags)
	}
	if !obj.IsNull() {
		t.Fatal("operatingSystemFeaturesValue(nil) is not null; want null object")
	}
}

func TestOperatingSystemFeaturesValue(t *testing.T) {
	ctx := context.Background()

	raid := true
	sshKeys := true
	userData := false
	accelerate := false
	rescue := true
	workflow := false

	obj, diags := operatingSystemFeaturesValue(ctx, &components.Features{
		Raid:       &raid,
		SSHKeys:    &sshKeys,
		UserData:   &userData,
		Accelerate: &accelerate,
		Rescue:     &rescue,
		Workflow:   &workflow,
	})
	if diags.HasError() {
		t.Fatalf("operatingSystemFeaturesValue diagnostics: %v", diags)
	}
	if obj.IsNull() {
		t.Fatal("operatingSystemFeaturesValue is null; want a known object")
	}

	var got OperatingSystemFeaturesModel
	if d := obj.As(ctx, &got, basicObjectAsOptions); d.HasError() {
		t.Fatalf("As: %v", d)
	}
	if !got.Raid.ValueBool() {
		t.Error("Raid = false, want true")
	}
	if !got.SSHKeys.ValueBool() {
		t.Error("SSHKeys = false, want true")
	}
	if got.UserData.ValueBool() {
		t.Error("UserData = true, want false")
	}
	if !got.Rescue.ValueBool() {
		t.Error("Rescue = false, want true")
	}
}

// findOperatingSystemInPage exercises the same matching precedence used by
// findOne (id, then slug, then name) without requiring an SDK client, by
// operating directly on an in-memory page of OperatingSystemData.
func findOperatingSystemInPage(page []components.OperatingSystemData, args findOperatingSystemArgs) *components.OperatingSystemData {
	for i := range page {
		os := page[i]
		if args.ID != "" && os.ID != nil && *os.ID == args.ID {
			return &os
		}
		if os.Attributes == nil {
			continue
		}
		if args.Slug != "" && os.Attributes.Slug != nil && *os.Attributes.Slug == args.Slug {
			return &os
		}
		if args.Name != "" && os.Attributes.Name != nil && *os.Attributes.Name == args.Name {
			return &os
		}
	}
	return nil
}

func TestFindOperatingSystemInPage(t *testing.T) {
	id := "os_1"
	slug := "ubuntu_22_04_x64_lts"
	name := "Ubuntu 22.04 LTS"
	page := []components.OperatingSystemData{
		{
			ID: &id,
			Attributes: &components.OperatingSystemDataAttributes{
				Slug: &slug,
				Name: &name,
			},
		},
	}

	if got := findOperatingSystemInPage(page, findOperatingSystemArgs{ID: "os_1"}); got == nil {
		t.Error("lookup by id: expected a match, got nil")
	}
	if got := findOperatingSystemInPage(page, findOperatingSystemArgs{Slug: slug}); got == nil {
		t.Error("lookup by slug: expected a match, got nil")
	}
	if got := findOperatingSystemInPage(page, findOperatingSystemArgs{Name: name}); got == nil {
		t.Error("lookup by name: expected a match, got nil")
	}
	if got := findOperatingSystemInPage(page, findOperatingSystemArgs{ID: "does-not-exist"}); got != nil {
		t.Error("lookup by unknown id: expected nil, got a match")
	}
}
