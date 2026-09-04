package latitudesh

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// TestPlanVMNicsValue exercises the specs.nics -> types.List mapping against
// the real vm-large payload (defined in vm_plan_discovery_test.go), which has
// nics[].count as a JSON number, the shape PD-6519 broke on before SDK v1.19.0.
func TestPlanVMNicsValue(t *testing.T) {
	ctx := context.Background()
	var plans components.VirtualMachinePlans
	if err := json.Unmarshal([]byte(vmPlansLivePayload), &plans); err != nil {
		t.Fatalf("unmarshaling payload: %s", err)
	}

	specs := plans.Data[0].Attributes.Specs
	list, diags := planVMNicsValue(ctx, specs.Nics)
	if diags.HasError() {
		t.Fatalf("planVMNicsValue diagnostics: %v", diags)
	}
	if list.IsNull() {
		t.Fatal("nics list is null; want a known list")
	}

	var got []PlanVMNicModel
	if d := list.ElementsAs(ctx, &got, false); d.HasError() {
		t.Fatalf("ElementsAs: %v", d)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 nic group, got %d", len(got))
	}
	if got[0].Count.ValueInt64() != 1 {
		t.Errorf("nic count = %d, want 1", got[0].Count.ValueInt64())
	}
	if got[0].Type.ValueString() != "10 Gbps" {
		t.Errorf("nic type = %q, want %q", got[0].Type.ValueString(), "10 Gbps")
	}
}

// TestPlanVMDiskValue exercises the specs.disk -> types.Object mapping,
// including the nested size sub-object.
func TestPlanVMDiskValue(t *testing.T) {
	ctx := context.Background()
	var plans components.VirtualMachinePlans
	if err := json.Unmarshal([]byte(vmPlansLivePayload), &plans); err != nil {
		t.Fatalf("unmarshaling payload: %s", err)
	}

	disk := plans.Data[0].Attributes.Specs.Disk
	obj, diags := planVMDiskValue(ctx, disk)
	if diags.HasError() {
		t.Fatalf("planVMDiskValue diagnostics: %v", diags)
	}
	if obj.IsNull() {
		t.Fatal("disk object is null; want a known object")
	}

	var got PlanVMDiskModel
	if d := obj.As(ctx, &got, basicObjectAsOptions); d.HasError() {
		t.Fatalf("As: %v", d)
	}

	var size PlanVMDiskSizeModel
	if d := got.Size.As(ctx, &size, basicObjectAsOptions); d.HasError() {
		t.Fatalf("size As: %v", d)
	}
	if size.Amount.ValueInt64() != 640 {
		t.Errorf("disk size amount = %d, want 640", size.Amount.ValueInt64())
	}
	if size.Unit.ValueString() != "gib" {
		t.Errorf("disk size unit = %q, want gib", size.Unit.ValueString())
	}
}

// TestPlanVMRegionsValue exercises the regions -> types.List mapping,
// including per-currency pricing (a map) and the nested locations object, for
// a plan with two currencies and one region.
func TestPlanVMRegionsValue(t *testing.T) {
	ctx := context.Background()
	var plans components.VirtualMachinePlans
	if err := json.Unmarshal([]byte(vmPlansLivePayload), &plans); err != nil {
		t.Fatalf("unmarshaling payload: %s", err)
	}

	regions := plans.Data[0].Attributes.Regions
	list, diags := planVMRegionsValue(ctx, regions)
	if diags.HasError() {
		t.Fatalf("planVMRegionsValue diagnostics: %v", diags)
	}

	var got []PlanVMRegionModel
	if d := list.ElementsAs(ctx, &got, false); d.HasError() {
		t.Fatalf("ElementsAs: %v", d)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 region, got %d", len(got))
	}
	if got[0].Name.ValueString() != "United States" {
		t.Errorf("region name = %q, want %q", got[0].Name.ValueString(), "United States")
	}

	var pricing map[string]PlanVMPricingModel
	if d := got[0].Pricing.ElementsAs(ctx, &pricing, false); d.HasError() {
		t.Fatalf("pricing ElementsAs: %v", d)
	}
	usd, ok := pricing["USD"]
	if !ok {
		t.Fatal("expected a USD pricing entry")
	}
	if usd.Hour.ValueFloat64() != 0.65 {
		t.Errorf("USD hour price = %v, want 0.65", usd.Hour.ValueFloat64())
	}
	if _, ok := pricing["BRL"]; !ok {
		t.Fatal("expected a BRL pricing entry")
	}
}

// TestPlanVMRegionsValueNeverNull guards that an empty regions slice still
// maps to a known (empty) list, not null, so downstream `for` expressions over
// `regions` never error even for a plan the API returns with no regions.
func TestPlanVMRegionsValueNeverNull(t *testing.T) {
	ctx := context.Background()
	list, diags := planVMRegionsValue(ctx, nil)
	if diags.HasError() {
		t.Fatalf("planVMRegionsValue(nil) diagnostics: %v", diags)
	}
	if list.IsNull() {
		t.Fatal("planVMRegionsValue(nil) returned a null list; want an empty known list")
	}
	if n := len(list.Elements()); n != 0 {
		t.Fatalf("planVMRegionsValue(nil) length = %d, want 0", n)
	}
}
