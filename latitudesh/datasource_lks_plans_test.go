package latitudesh

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// specs.memory.total is an int-or-string union in the SDK because the API is
// not consistent about it, so both branches have to render.
func TestLksPlanMemoryTotal(t *testing.T) {
	str := "32 GB"

	cases := []struct {
		name  string
		total *components.LksPlansTotal
		want  string
		null  bool
	}{
		{name: "nil", total: nil, null: true},
		{name: "integer", total: &components.LksPlansTotal{Integer: ptrInt64(32768)}, want: "32768"},
		{name: "string", total: &components.LksPlansTotal{Str: &str}, want: "32 GB"},
		{name: "empty union", total: &components.LksPlansTotal{}, null: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := lksPlanMemoryTotal(tc.total)
			if tc.null {
				if !got.IsNull() {
					t.Fatalf("memory_total = %s, want null", got)
				}
				return
			}
			if got.ValueString() != tc.want {
				t.Fatalf("memory_total = %q, want %q", got.ValueString(), tc.want)
			}
		})
	}
}

func ptrInt64(v int64) *int64 { return &v }

// A plan with no attributes envelope maps to nulls rather than panicking.
func TestLksPlanItemValue_NilAttributes(t *testing.T) {
	id := "lks-plan-1"

	item, diags := lksPlanItemValue(context.Background(), &components.LksPlansData{ID: &id})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags.Errors())
	}

	if item.ID.ValueString() != id {
		t.Errorf("ID = %q, want %q", item.ID.ValueString(), id)
	}
	if !item.InStockCount.IsNull() {
		t.Errorf("in_stock_count = %s, want null for a nil envelope", item.InStockCount)
	}
	if !item.Slug.IsNull() || !item.Name.IsNull() || !item.Specs.IsNull() || !item.Regions.IsNull() {
		t.Errorf("expected every attribute null for a nil envelope, got %+v", item)
	}
}

func TestLksPlans_Read(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/plans/lks" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"data":[
			{"id":"plan_1","type":"lks_plans","attributes":{
				"name":"c2 Medium","slug":"c2-medium-x86",
				"specs":{
					"cpu":{"type":"Intel Xeon","clock":2.4,"cores":8,"count":1},
					"memory":{"total":32768},
					"drives":[{"count":2,"size":"960GB","type":"NVME"}],
					"nics":[{"count":2,"type":"10Gbps"}],
					"gpu":{}
				},
				"regions":[{
					"name":"North America",
					"stock_level":"high",
					"locations":{"available":["ASH","DAL"],"in_stock":["ASH"],"in_stock_count":{"ASH":7}}
				}]
			}},
			{"id":"plan_2","type":"lks_plans","attributes":{
				"name":"g3 GPU","slug":"g3-gpu-x86",
				"specs":{"memory":{"total":"1 TB"},"gpu":{"count":4,"type":"H100","vram_per_gpu":80,"interconnect":"NVLink"}},
				"regions":[]
			}}
		]}`)
	}))
	defer server.Close()

	const ds = "data.latitudesh_lks_plans.test"

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: `
provider "latitudesh" {
  auth_token = "mock-token"
}

data "latitudesh_lks_plans" "test" {}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(ds, "id", "all"),
					resource.TestCheckResourceAttr(ds, "plans.#", "2"),
					resource.TestCheckResourceAttr(ds, "plans.0.slug", "c2-medium-x86"),
					resource.TestCheckResourceAttr(ds, "plans.0.specs.cpu.cores", "8"),
					resource.TestCheckResourceAttr(ds, "plans.0.specs.cpu.clock", "2.4"),
					// The integer branch of the memory union renders as digits.
					resource.TestCheckResourceAttr(ds, "plans.0.specs.memory_total", "32768"),
					resource.TestCheckResourceAttr(ds, "plans.0.specs.drives.0.size", "960GB"),
					resource.TestCheckResourceAttr(ds, "plans.0.specs.nics.0.type", "10Gbps"),
					// "gpu":{} is how the API says "no GPU" — it must not
					// surface as an object of nulls.
					resource.TestCheckNoResourceAttr(ds, "plans.0.specs.gpu.type"),
					resource.TestCheckResourceAttr(ds, "plans.0.regions.0.stock_level", "high"),
					resource.TestCheckResourceAttr(ds, "plans.0.regions.0.available_sites.#", "2"),
					resource.TestCheckResourceAttr(ds, "plans.0.regions.0.in_stock_sites.#", "1"),
					resource.TestCheckResourceAttr(ds, "plans.0.regions.0.in_stock_count.ASH", "7"),
					// The flattened form is the one callers actually reach for:
					// it answers "can this plan build at this site" without a
					// nested loop over regions.
					resource.TestCheckResourceAttr(ds, "plans.0.in_stock_count.ASH", "7"),
					resource.TestCheckNoResourceAttr(ds, "plans.0.in_stock_count.DAL"),
					// A plan with no regions at all still gets a known, empty map
					// rather than null, so lookup() never trips over it.
					resource.TestCheckResourceAttr(ds, "plans.1.in_stock_count.%", "0"),
					// And the string branch of the union.
					resource.TestCheckResourceAttr(ds, "plans.1.specs.memory_total", "1 TB"),
					resource.TestCheckResourceAttr(ds, "plans.1.specs.gpu.type", "H100"),
					resource.TestCheckResourceAttr(ds, "plans.1.specs.gpu.vram_per_gpu", "80"),
					resource.TestCheckResourceAttr(ds, "plans.1.regions.#", "0"),
					// Unfiltered, a per-site count would be meaningless.
					resource.TestCheckNoResourceAttr(ds, "stock.%"),
				),
			},
		},
	})
}

// TestAccLksPlans_Basic reads the real LKS plan catalog. Skipped unless TF_ACC
// and LATITUDESH_AUTH_TOKEN are set. This is what confirms the slugs a node
// pool can be built from, and which sites actually have stock for them.
func TestAccLksPlans_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTokenCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `data "latitudesh_lks_plans" "test" {}`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_lks_plans.test", "id", "all"),
					resource.TestCheckResourceAttrSet("data.latitudesh_lks_plans.test", "plans.#"),
					resource.TestCheckResourceAttrSet("data.latitudesh_lks_plans.test", "plans.0.slug"),
				),
			},
		},
	})
}

// With `site` set the data source answers the question directly: `stock` is
// slug -> node count at that site, containing only plans that can actually
// build there. No loop, no filtering in configuration.
func TestLksPlans_ReadFilteredBySite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"data":[
			{"id":"plan_1","type":"lks_plans","attributes":{"slug":"c2-medium-x86","regions":[
				{"locations":{"available":["LAX2","CHI"],"in_stock":["LAX2"],"in_stock_count":{"LAX2":7}}}
			]}},
			{"id":"plan_2","type":"lks_plans","attributes":{"slug":"g3-gpu-x86","regions":[
				{"locations":{"available":["LAX2"],"in_stock":[],"in_stock_count":{}}}
			]}},
			{"id":"plan_3","type":"lks_plans","attributes":{"slug":"c3-large-x86","regions":[
				{"locations":{"available":["CHI"],"in_stock":["CHI"],"in_stock_count":{"CHI":3}}}
			]}}
		]}`)
	}))
	defer server.Close()

	const ds = "data.latitudesh_lks_plans.at_site"

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: `
provider "latitudesh" {
  auth_token = "mock-token"
}

data "latitudesh_lks_plans" "at_site" {
  site = "LAX2"
}
`,
				Check: resource.ComposeTestCheckFunc(
					// The synthetic id reflects the filter, as in
					// latitudesh_marketplace_apps.
					resource.TestCheckResourceAttr(ds, "id", "LAX2"),
					// Only c2 qualifies: g3 is available at LAX2 but has no
					// stock, and c3 is a different site entirely.
					resource.TestCheckResourceAttr(ds, "plans.#", "1"),
					resource.TestCheckResourceAttr(ds, "plans.0.slug", "c2-medium-x86"),
					resource.TestCheckResourceAttr(ds, "stock.%", "1"),
					resource.TestCheckResourceAttr(ds, "stock.c2-medium-x86", "7"),
					resource.TestCheckNoResourceAttr(ds, "stock.g3-gpu-x86"),
					resource.TestCheckNoResourceAttr(ds, "stock.c3-large-x86"),
				),
			},
		},
	})
}

// A site nobody has capacity at is an empty answer, not an error: the caller
// decides whether that is fatal.
func TestLksPlans_ReadFilteredBySiteWithNoStock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"data":[
			{"id":"plan_1","type":"lks_plans","attributes":{"slug":"c2-medium-x86","regions":[
				{"locations":{"available":["LAX2"],"in_stock":["LAX2"],"in_stock_count":{"LAX2":7}}}
			]}}
		]}`)
	}))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: `
provider "latitudesh" {
  auth_token = "mock-token"
}

data "latitudesh_lks_plans" "empty" {
  site = "NOWHERE"
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_lks_plans.empty", "plans.#", "0"),
					resource.TestCheckResourceAttr("data.latitudesh_lks_plans.empty", "stock.%", "0"),
				),
			},
		},
	})
}
