package latitudesh

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// trafficRegionsPayload is a representative /traffic regions array: one
// region with two daily data points in reverse-date order, so the mapping
// and the date sort are both exercised.
const trafficRegionsPayload = `[
  {
    "region_slug": "SAO",
    "total_inbound_gb": 10,
    "total_outbound_gb": 20,
    "total_inbound_95th_percentile_mbps": 12.5,
    "total_outbound_95th_percentile_mbps": 25.5,
    "data": [
      {"date": "2024-04-02", "inbound_gb": 6, "outbound_gb": 12, "avg_outbound_speed_mbps": 5.5, "avg_inbound_speed_mbps": 2.5, "outbound_speed_mbps": 6, "inbound_speed_mbps": 3},
      {"date": "2024-04-01", "inbound_gb": 4, "outbound_gb": 8, "avg_outbound_speed_mbps": 4.5, "avg_inbound_speed_mbps": 1.5, "outbound_speed_mbps": 5, "inbound_speed_mbps": 2}
    ]
  }
]`

func TestTrafficRegionsValueMapping(t *testing.T) {
	ctx := context.Background()

	var regions []components.TrafficRegions
	if err := json.Unmarshal([]byte(trafficRegionsPayload), &regions); err != nil {
		t.Fatalf("unmarshaling regions payload: %s", err)
	}

	list, diags := trafficRegionsValue(ctx, regions)
	if diags.HasError() {
		t.Fatalf("trafficRegionsValue diagnostics: %v", diags)
	}
	if list.IsNull() {
		t.Fatal("regions list is null; want a known list")
	}

	var got []TrafficRegionModel
	if d := list.ElementsAs(ctx, &got, false); d.HasError() {
		t.Fatalf("ElementsAs: %v", d)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 region, got %d", len(got))
	}
	if got[0].RegionSlug.ValueString() != "SAO" {
		t.Errorf("region slug = %q, want SAO", got[0].RegionSlug.ValueString())
	}

	var data []TrafficRegionDataModel
	if d := got[0].Data.ElementsAs(ctx, &data, false); d.HasError() {
		t.Fatalf("ElementsAs data: %v", d)
	}
	if len(data) != 2 {
		t.Fatalf("expected 2 data points, got %d", len(data))
	}
	if data[0].Date.ValueString() != "2024-04-01" {
		t.Errorf("data[0].date = %q, want 2024-04-01 (sorted ascending)", data[0].Date.ValueString())
	}
	if data[1].Date.ValueString() != "2024-04-02" {
		t.Errorf("data[1].date = %q, want 2024-04-02", data[1].Date.ValueString())
	}
}

func TestTrafficRegionsValueEmptyNeverNull(t *testing.T) {
	ctx := context.Background()
	list, diags := trafficRegionsValue(ctx, nil)
	if diags.HasError() {
		t.Fatalf("trafficRegionsValue(nil) diagnostics: %v", diags)
	}
	if list.IsNull() {
		t.Fatal("trafficRegionsValue(nil) returned a null list; want an empty known list")
	}
	if n := len(list.Elements()); n != 0 {
		t.Fatalf("trafficRegionsValue(nil) length = %d, want 0", n)
	}
}

// quotaPerProjectPayload covers both feature-flag shapes side by side: one
// region reporting quota_in_tb, the other quota_in_mbps, so the "handle both"
// requirement from the manifest notes is exercised directly.
const quotaPerProjectPayload = `[
  {
    "project_id": "proj_2",
    "project_slug": "proj-b",
    "price": 100,
    "billing_method": "bandwidth",
    "quota_per_region": [
      {"region_id": "reg_2", "region_slug": "NYC", "price": 5, "quota_in_mbps": {"granted": 1000, "additional": 0, "total": 1000}}
    ]
  },
  {
    "project_id": "proj_1",
    "project_slug": "proj-a",
    "price": 200,
    "billing_method": "volume",
    "quota_per_region": [
      {"region_id": "reg_1", "region_slug": "SAO", "price": 10, "quota_in_tb": {"granted": 10, "additional": 2, "total": 12}}
    ]
  }
]`

func TestQuotaPerProjectValueMapping(t *testing.T) {
	ctx := context.Background()

	var items []components.QuotaPerProject
	if err := json.Unmarshal([]byte(quotaPerProjectPayload), &items); err != nil {
		t.Fatalf("unmarshaling quota_per_project payload: %s", err)
	}

	list, diags := quotaPerProjectValue(ctx, items)
	if diags.HasError() {
		t.Fatalf("quotaPerProjectValue diagnostics: %v", diags)
	}

	var got []QuotaPerProjectModel
	if d := list.ElementsAs(ctx, &got, false); d.HasError() {
		t.Fatalf("ElementsAs: %v", d)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(got))
	}

	// Sorted by project_id ascending regardless of input order.
	if got[0].ProjectID.ValueString() != "proj_1" {
		t.Errorf("project 0 id = %q, want proj_1", got[0].ProjectID.ValueString())
	}
	if got[1].ProjectID.ValueString() != "proj_2" {
		t.Errorf("project 1 id = %q, want proj_2", got[1].ProjectID.ValueString())
	}

	var regionsA []QuotaPerRegionModel
	if d := got[0].QuotaPerRegion.ElementsAs(ctx, &regionsA, false); d.HasError() {
		t.Fatalf("ElementsAs regions: %v", d)
	}
	if len(regionsA) != 1 {
		t.Fatalf("expected 1 region for proj_1, got %d", len(regionsA))
	}
	if regionsA[0].QuotaInTb.IsNull() {
		t.Error("proj_1 region quota_in_tb should be known (volume billing)")
	}
	if !regionsA[0].QuotaInMbps.IsNull() {
		t.Error("proj_1 region quota_in_mbps should be null (volume billing)")
	}
	var tb QuotaAmountModel
	if d := regionsA[0].QuotaInTb.As(ctx, &tb, basicObjectAsOptions); d.HasError() {
		t.Fatalf("quota_in_tb As: %v", d)
	}
	if tb.Total.ValueInt64() != 12 {
		t.Errorf("quota_in_tb.total = %d, want 12", tb.Total.ValueInt64())
	}

	var regionsB []QuotaPerRegionModel
	if d := got[1].QuotaPerRegion.ElementsAs(ctx, &regionsB, false); d.HasError() {
		t.Fatalf("ElementsAs regions: %v", d)
	}
	if len(regionsB) != 1 {
		t.Fatalf("expected 1 region for proj_2, got %d", len(regionsB))
	}
	if regionsB[0].QuotaInMbps.IsNull() {
		t.Error("proj_2 region quota_in_mbps should be known (bandwidth billing)")
	}
	if !regionsB[0].QuotaInTb.IsNull() {
		t.Error("proj_2 region quota_in_tb should be null (bandwidth billing)")
	}
}

func TestQuotaPerProjectValueEmptyNeverNull(t *testing.T) {
	ctx := context.Background()
	list, diags := quotaPerProjectValue(ctx, nil)
	if diags.HasError() {
		t.Fatalf("quotaPerProjectValue(nil) diagnostics: %v", diags)
	}
	if list.IsNull() {
		t.Fatal("quotaPerProjectValue(nil) returned a null list; want an empty known list")
	}
	if n := len(list.Elements()); n != 0 {
		t.Fatalf("quotaPerProjectValue(nil) length = %d, want 0", n)
	}
}

func TestQuotaInTbAndMbpsValueNil(t *testing.T) {
	ctx := context.Background()

	tb, diags := quotaInTbValue(ctx, nil)
	if diags.HasError() {
		t.Fatalf("quotaInTbValue(nil) diagnostics: %v", diags)
	}
	if !tb.IsNull() {
		t.Error("quotaInTbValue(nil) is not null")
	}

	mbps, diags := quotaInMbpsValue(ctx, nil)
	if diags.HasError() {
		t.Fatalf("quotaInMbpsValue(nil) diagnostics: %v", diags)
	}
	if !mbps.IsNull() {
		t.Error("quotaInMbpsValue(nil) is not null")
	}
}
