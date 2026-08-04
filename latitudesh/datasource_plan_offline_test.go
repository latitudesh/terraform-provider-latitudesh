package latitudesh

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// planSpecsGpuEmptyObject is a real GET /plans specs block (c3-large-x86,
// captured 2026-07-31). The plan has no GPU, yet the API returns "gpu":{}
// rather than null — the shape that made has_gpu report true for every
// non-GPU plan (GitHub #205).
const planSpecsGpuEmptyObject = `{"cpu":{"type":"AMD 7443P","clock":2.85,"cores":24,"count":1},
  "memory":{"total":256},
  "drives":[{"count":2,"size":"1.9 TB","type":"NVME"}],
  "gpu":{}}`

// planSpecsMultiGroupDrives is a real specs block (rs4-metal-xlarge) with two
// disk groups, exercising the multi-group drive mapping and the float64->int64
// count conversion.
const planSpecsMultiGroupDrives = `{"drives":[{"count":2,"size":"480GB","type":"NVME"},{"count":4,"size":"8TB","type":"NVME"}]}`

func TestPlanHasGPU(t *testing.T) {
	gpuType := "H100"
	gpuCount := 8.0
	cases := []struct {
		name string
		gpu  *components.Gpu
		want bool
	}{
		{"nil gpu", nil, false},
		{"empty object (gpu:{})", &components.Gpu{}, false},
		{"real gpu with type", &components.Gpu{Type: &gpuType}, true},
		{"real gpu with count", &components.Gpu{Count: &gpuCount}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := planHasGPU(tc.gpu); got != tc.want {
				t.Errorf("planHasGPU() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestPlanHasGPUEmptyObjectPayload is the offline regression for the #205
// has_gpu false positive: the real "gpu":{} payload must unmarshal to a
// non-nil *Gpu (documenting why the old pointer check was wrong) yet resolve
// to has_gpu=false.
func TestPlanHasGPUEmptyObjectPayload(t *testing.T) {
	var specs components.Specs
	if err := json.Unmarshal([]byte(planSpecsGpuEmptyObject), &specs); err != nil {
		t.Fatalf("unmarshaling specs with gpu:{}: %s", err)
	}
	if specs.Gpu == nil {
		t.Fatal("expected non-nil *Gpu for gpu:{} (the whole point of the bug); got nil")
	}
	if planHasGPU(specs.Gpu) {
		t.Error("planHasGPU = true for a plan with gpu:{}; want false")
	}
}

func TestPlanDrivesValueMultiGroup(t *testing.T) {
	ctx := context.Background()
	var specs components.Specs
	if err := json.Unmarshal([]byte(planSpecsMultiGroupDrives), &specs); err != nil {
		t.Fatalf("unmarshaling multi-group drives: %s", err)
	}

	list, diags := planDrivesValue(ctx, specs.Drives)
	if diags.HasError() {
		t.Fatalf("planDrivesValue diagnostics: %v", diags)
	}
	if list.IsNull() {
		t.Fatal("drives list is null; want a known list")
	}

	var got []PlanDriveModel
	if d := list.ElementsAs(ctx, &got, false); d.HasError() {
		t.Fatalf("ElementsAs: %v", d)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 drive groups, got %d", len(got))
	}
	if got[0].Count.ValueInt64() != 2 {
		t.Errorf("group 0 count = %d, want 2", got[0].Count.ValueInt64())
	}
	if got[0].Type.ValueString() != "NVME" {
		t.Errorf("group 0 type = %q, want NVME", got[0].Type.ValueString())
	}
	if got[1].Count.ValueInt64() != 4 {
		t.Errorf("group 1 count = %d, want 4", got[1].Count.ValueInt64())
	}
}

// TestPlanDrivesValueNeverNull guards the "always a list, never null"
// guarantee so the customer's sum([for d in ... : d.count]) never errors on a
// null iteratee, even for a plan the API returns with no drives group.
func TestPlanDrivesValueNeverNull(t *testing.T) {
	ctx := context.Background()
	list, diags := planDrivesValue(ctx, nil)
	if diags.HasError() {
		t.Fatalf("planDrivesValue(nil) diagnostics: %v", diags)
	}
	if list.IsNull() {
		t.Fatal("planDrivesValue(nil) returned a null list; want an empty known list")
	}
	if n := len(list.Elements()); n != 0 {
		t.Fatalf("planDrivesValue(nil) length = %d, want 0", n)
	}
}
