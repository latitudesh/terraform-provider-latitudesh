package latitudesh

import (
	"encoding/json"
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// vmPlansLivePayload is a real GET /plans/virtual_machines response captured
// on 2026-07-27. It keeps the field shapes that historically broke the SDK
// models (PD-6519): null gpu/vram_per_gpu and numeric nics[].count.
const vmPlansLivePayload = `{"data":[
  {"id":"plan_jv6m5J18NLPeE","type":"virtual_machine_plans","attributes":{"name":"vm.large","slug":"vm-large","specs":{"memory":64,"gpu":null,"vram_per_gpu":null,"vcpus":16,"vcpu":{"count":16,"clock":null,"type":null},"nics":[{"type":"10 Gbps","count":1}],"disk":{"type":"local ","size":{"amount":640,"unit":"gib"}}},"regions":[{"name":"United States","locations":{"available":["DAL","ASH"],"in_stock":["DAL","ASH"]},"stock_level":"high","pricing":{"USD":{"hour":0.65,"month":237.0,"year":1991.0},"BRL":{"hour":3.38,"month":1234.0,"year":10366.0}}}],"stock_level":"high","available_operating_systems":["ubuntu_24_04_x64_lts"]}},
  {"id":"plan_y9815XnZ0vEkd","type":"virtual_machine_plans","attributes":{"name":"vm.small","slug":"vm-small","specs":{"memory":16,"gpu":null,"vram_per_gpu":null,"vcpus":4,"vcpu":{"count":4,"clock":null,"type":null},"nics":[{"type":"10 Gbps","count":1}],"disk":{"type":"local ","size":{"amount":160,"unit":"gib"}}},"regions":[{"name":"United States","locations":{"available":["DAL","ASH"],"in_stock":["DAL","ASH"]},"stock_level":"high","pricing":{"USD":{"hour":0.19,"month":69.0,"year":580.0},"BRL":{"hour":0.99,"month":361.0,"year":3032.0}}},{"name":"Japan","locations":{"available":["TYO3"],"in_stock":["TYO3"]},"stock_level":"high","pricing":{"USD":{"hour":0.19,"month":69.0,"year":580.0},"BRL":{"hour":0.99,"month":361.0,"year":3032.0}}}],"stock_level":"high","available_operating_systems":["ubuntu_24_04_x64_lts"]}},
  {"id":"plan_MDEOaPqe0wgBL","type":"virtual_machine_plans","attributes":{"name":"vm.medium","slug":"vm-medium","specs":{"memory":32,"gpu":null,"vram_per_gpu":null,"vcpus":8,"vcpu":{"count":8,"clock":null,"type":null},"nics":[{"type":"10 Gbps","count":1}],"disk":{"type":"local ","size":{"amount":320,"unit":"gib"}}},"regions":[{"name":"Germany","locations":{"available":["FRA"],"in_stock":["FRA"]},"stock_level":"high","pricing":{"USD":{"hour":0.35,"month":128.0,"year":1075.0},"BRL":{"hour":1.82,"month":664.0,"year":5578.0}}}],"stock_level":"high","available_operating_systems":["ubuntu_24_04_x64_lts"]}}
]}`

// TestVMPlansPayloadUnmarshal is the offline regression for PD-6519: SDK
// versions before v1.19.0 failed to unmarshal this exact payload (first on
// gpu/vram_per_gpu strings, then on nics[].count typed as string).
func TestVMPlansPayloadUnmarshal(t *testing.T) {
	var plans components.VirtualMachinePlans
	if err := json.Unmarshal([]byte(vmPlansLivePayload), &plans); err != nil {
		t.Fatalf("unmarshaling real /plans/virtual_machines payload: %s", err)
	}
	if len(plans.Data) != 3 {
		t.Fatalf("expected 3 plans, got %d", len(plans.Data))
	}
}

func TestPickVMPlan(t *testing.T) {
	var plans components.VirtualMachinePlans
	if err := json.Unmarshal([]byte(vmPlansLivePayload), &plans); err != nil {
		t.Fatalf("unmarshaling payload: %s", err)
	}

	cases := []struct {
		name string
		site string
		want string
	}{
		{"smallest plan wins where several have stock", "ASH", "vm-small"},
		{"site with a single stocked plan", "FRA", "vm-medium"},
		{"site listed in another region of the same plan", "TYO3", "vm-small"},
		{"site without stock", "SAO2", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickVMPlan(plans.Data, tc.site); got != tc.want {
				t.Errorf("pickVMPlan(%q) = %q, want %q", tc.site, got, tc.want)
			}
		})
	}
}
