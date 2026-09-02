package latitudesh

import (
	"context"
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func TestMarketplaceAppItemValue(t *testing.T) {
	ctx := context.Background()

	id := "mkapp_mock_1"
	name := "WordPress"
	slug := "wordpress"
	category := "cms"
	version := "6.5.0"
	strategy := components.DeploymentStrategyUserData
	vcpus := int64(2)

	item, diags := marketplaceAppItemValue(ctx, &components.MarketplaceAppData{
		ID: &id,
		Attributes: &components.MarketplaceAppDataAttributes{
			Name:               &name,
			Slug:               &slug,
			Category:           &category,
			Version:            &version,
			DeploymentStrategy: &strategy,
			CompatiblePlans:    []string{"c2-small-x86", "c2-medium-x86"},
			SystemRequirements: &components.SystemRequirements{Vcpus: &vcpus},
		},
	})
	if diags.HasError() {
		t.Fatalf("marketplaceAppItemValue diagnostics: %v", diags)
	}

	if item.ID.ValueString() != id {
		t.Errorf("ID = %q, want %q", item.ID.ValueString(), id)
	}
	if item.Slug.ValueString() != slug {
		t.Errorf("Slug = %q, want %q", item.Slug.ValueString(), slug)
	}
	if item.Category.ValueString() != category {
		t.Errorf("Category = %q, want %q", item.Category.ValueString(), category)
	}
	if item.DeploymentStrategy.ValueString() != string(strategy) {
		t.Errorf("DeploymentStrategy = %q, want %q", item.DeploymentStrategy.ValueString(), string(strategy))
	}
	if l := len(item.CompatiblePlans.Elements()); l != 2 {
		t.Errorf("CompatiblePlans length = %d, want 2", l)
	}
	if item.SystemRequirements.IsNull() {
		t.Error("SystemRequirements is null; want a known object")
	}

	// An attribute absent from the API must land as null, not "".
	if !item.DefaultOperatingSystem.IsNull() {
		t.Errorf("DefaultOperatingSystem = %q, want null", item.DefaultOperatingSystem.ValueString())
	}
}

func TestMarketplaceAppItemValueNilAttributes(t *testing.T) {
	ctx := context.Background()

	id := "mkapp_mock_2"
	item, diags := marketplaceAppItemValue(ctx, &components.MarketplaceAppData{ID: &id})
	if diags.HasError() {
		t.Fatalf("marketplaceAppItemValue diagnostics: %v", diags)
	}

	if item.ID.ValueString() != id {
		t.Errorf("ID = %q, want %q", item.ID.ValueString(), id)
	}
	if !item.Name.IsNull() {
		t.Error("Name is not null; want null when attributes are absent")
	}
	if !item.SystemRequirements.IsNull() {
		t.Error("SystemRequirements is not null; want null when attributes are absent")
	}
	if !item.CompatiblePlans.IsNull() {
		t.Error("CompatiblePlans is not null; want null when attributes are absent")
	}
}
