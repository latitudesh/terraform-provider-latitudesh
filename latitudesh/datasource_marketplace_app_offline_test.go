package latitudesh

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func TestMarketplaceAppNotFound(t *testing.T) {
	status404 := "404"
	status422 := "422"

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "ErrorObject with 404 status (typed 404 the GET endpoint returns)",
			err:  &components.ErrorObject{Errors: []components.Errors{{Status: &status404}}},
			want: true,
		},
		{
			name: "ErrorObject without a 404 status",
			err:  &components.ErrorObject{Errors: []components.Errors{{Status: &status422}}},
			want: false,
		},
		{
			name: "APIError with 404 status code",
			err:  components.NewAPIError("not found", http.StatusNotFound, "", nil),
			want: true,
		},
		{
			name: "APIError with a non-404 status code",
			err:  components.NewAPIError("boom", http.StatusInternalServerError, "", nil),
			want: false,
		},
		{
			name: "wrapped ErrorObject 404 is still detected",
			err:  fmt.Errorf("get failed: %w", &components.ErrorObject{Errors: []components.Errors{{Status: &status404}}}),
			want: true,
		},
		{
			name: "unrelated error",
			err:  fmt.Errorf("connection reset"),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := marketplaceAppNotFound(tc.err); got != tc.want {
				t.Errorf("marketplaceAppNotFound() = %v, want %v", got, tc.want)
			}
		})
	}
}

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
