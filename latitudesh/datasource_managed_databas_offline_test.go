package latitudesh

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
)

func TestManagedDatabasMetricsValueMapping(t *testing.T) {
	ctx := context.Background()

	current := 42.5
	ts := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	metrics := map[string]operations.Metrics{
		"cpuUsage": {
			Unit:    "percent",
			Current: &current,
			Points: []operations.Points{
				{Timestamp: ts, Value: 12.3},
				{Timestamp: ts.Add(30 * time.Minute), Value: 15.1},
			},
		},
		"deadlocks": {
			Unit:   "count",
			Points: []operations.Points{},
		},
	}

	got, diags := managedDatabasMetricsValue(ctx, metrics)
	if diags.HasError() {
		t.Fatalf("managedDatabasMetricsValue diagnostics: %v", diags)
	}
	if got.IsNull() {
		t.Fatal("metrics map is null; want a known map")
	}

	var models map[string]ManagedDatabasMetricModel
	if d := got.ElementsAs(ctx, &models, false); d.HasError() {
		t.Fatalf("ElementsAs: %v", d)
	}

	cpu, ok := models["cpuUsage"]
	if !ok {
		t.Fatal("cpuUsage metric missing from mapped result")
	}
	if cpu.Unit.ValueString() != "percent" {
		t.Errorf("cpuUsage unit = %q, want percent", cpu.Unit.ValueString())
	}
	if cpu.Current.ValueFloat64() != 42.5 {
		t.Errorf("cpuUsage current = %v, want 42.5", cpu.Current.ValueFloat64())
	}

	var points []ManagedDatabasMetricPointModel
	if d := cpu.Points.ElementsAs(ctx, &points, false); d.HasError() {
		t.Fatalf("ElementsAs points: %v", d)
	}
	if len(points) != 2 {
		t.Fatalf("cpuUsage points length = %d, want 2", len(points))
	}
	if points[0].Value.ValueFloat64() != 12.3 {
		t.Errorf("point 0 value = %v, want 12.3", points[0].Value.ValueFloat64())
	}
	if want := "2026-09-01T12:00:00Z"; points[0].Timestamp.ValueString() != want {
		t.Errorf("point 0 timestamp = %q, want %q", points[0].Timestamp.ValueString(), want)
	}

	deadlocks, ok := models["deadlocks"]
	if !ok {
		t.Fatal("deadlocks metric missing from mapped result")
	}
	if !deadlocks.Current.IsNull() {
		t.Errorf("deadlocks current = %v, want null (no current sample)", deadlocks.Current.ValueFloat64())
	}
	if n := len(deadlocks.Points.Elements()); n != 0 {
		t.Errorf("deadlocks points length = %d, want 0", n)
	}
}

// TestManagedDatabasMetricsValueEmptyNeverNull guards the "always a map, never
// null" guarantee (mirrors TestBillingProductsValueEmptyNeverNull) so a config
// that iterates `metrics` never fails on a null iteratee for a database with
// no metrics reported yet.
func TestManagedDatabasMetricsValueEmptyNeverNull(t *testing.T) {
	ctx := context.Background()
	got, diags := managedDatabasMetricsValue(ctx, nil)
	if diags.HasError() {
		t.Fatalf("managedDatabasMetricsValue(nil) diagnostics: %v", diags)
	}
	if got.IsNull() {
		t.Fatal("managedDatabasMetricsValue(nil) returned a null map; want an empty known map")
	}
	if n := len(got.Elements()); n != 0 {
		t.Fatalf("managedDatabasMetricsValue(nil) length = %d, want 0", n)
	}
}

func TestManagedDatabasMetricsNotFound(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "APIError 404",
			err:  components.NewAPIError("not found", http.StatusNotFound, "", nil),
			want: true,
		},
		{
			name: "APIError 500",
			err:  components.NewAPIError("boom", http.StatusInternalServerError, "", nil),
			want: false,
		},
		{
			name: "ErrorObject 404",
			err:  &components.ErrorObject{Errors: []components.Errors{{Status: strPtr("404")}}},
			want: true,
		},
		{
			name: "ErrorObject 422",
			err:  &components.ErrorObject{Errors: []components.Errors{{Status: strPtr("422")}}},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := managedDatabasMetricsNotFound(tc.err); got != tc.want {
				t.Errorf("managedDatabasMetricsNotFound(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
