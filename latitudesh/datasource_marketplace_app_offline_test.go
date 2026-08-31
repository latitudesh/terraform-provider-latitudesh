package latitudesh

import (
	"context"
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func TestMarketplaceAppSystemRequirementsValueNil(t *testing.T) {
	ctx := context.Background()

	obj, diags := marketplaceAppSystemRequirementsValue(ctx, nil)
	if diags.HasError() {
		t.Fatalf("marketplaceAppSystemRequirementsValue(nil) diagnostics: %v", diags)
	}
	if !obj.IsNull() {
		t.Fatal("marketplaceAppSystemRequirementsValue(nil) is not null; want null object")
	}
}

func TestMarketplaceAppSystemRequirementsValue(t *testing.T) {
	ctx := context.Background()

	vcpus := int64(4)
	memory := int64(8)
	storage := int64(80)
	gpu := true

	obj, diags := marketplaceAppSystemRequirementsValue(ctx, &components.SystemRequirements{
		Vcpus:       &vcpus,
		MemoryInGb:  &memory,
		StorageInGb: &storage,
		Gpu:         &gpu,
	})
	if diags.HasError() {
		t.Fatalf("marketplaceAppSystemRequirementsValue diagnostics: %v", diags)
	}
	if obj.IsNull() {
		t.Fatal("marketplaceAppSystemRequirementsValue is null; want a known object")
	}

	var got MarketplaceAppSystemRequirementsModel
	if d := obj.As(ctx, &got, basicObjectAsOptions); d.HasError() {
		t.Fatalf("As: %v", d)
	}
	if got.Vcpus.ValueInt64() != vcpus {
		t.Errorf("Vcpus = %d, want %d", got.Vcpus.ValueInt64(), vcpus)
	}
	if got.MemoryInGb.ValueInt64() != memory {
		t.Errorf("MemoryInGb = %d, want %d", got.MemoryInGb.ValueInt64(), memory)
	}
	if got.StorageInGb.ValueInt64() != storage {
		t.Errorf("StorageInGb = %d, want %d", got.StorageInGb.ValueInt64(), storage)
	}
	if !got.Gpu.ValueBool() {
		t.Error("Gpu = false, want true")
	}
}
