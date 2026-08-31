package latitudesh

// Regression coverage for the marketplace_app attribute (SDK drift: new
// VirtualMachinePayloadAttributes.marketplace_app / VirtualMachineAttributesAttributes.marketplace_app
// fields). Both tests drive Create() directly against a mock API so no live
// credentials or VCR fixtures are needed.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

const testVMMktID = "vm_mkt_1"

// vmMarketplaceMock stands up a mock API for POST/GET /virtual_machines. It
// records the marketplace_app value received in the create payload, and
// echoes marketplaceAppInRead (as a {slug,name,version} object, mirroring the
// SDK's VirtualMachineAttributesMarketplaceApp) back from both endpoints when
// set. The VM is always reported "Running" with a primary IPv4 so
// waitForVMReady resolves on the first poll.
type vmMarketplaceMock struct {
	createdMarketplaceApp *string
	marketplaceAppInRead  *string
}

func (m *vmMarketplaceMock) envelope() map[string]any {
	attrs := map[string]any{
		"name":             testVMName,
		"status":           vmStatusRunning,
		"primary_ipv4":     "203.0.113.55",
		"created_at":       "2026-07-14T12:00:00Z",
		"billing":          "monthly",
		"site":             "DAL",
		"operating_system": map[string]any{"slug": "ubuntu_24_04_x64_lts"},
		"plan":             map[string]any{"id": "plan_mock", "name": testVMPlan},
		"project":          map[string]any{"id": "proj_mock", "slug": "test-project"},
		"credentials":      map[string]any{"username": "ubuntu"},
		"specs":            map[string]any{"vcpu": 1, "ram": "4GB", "storage": "20GB"},
	}
	if m.marketplaceAppInRead != nil {
		attrs["marketplace_app"] = map[string]any{
			"slug":    *m.marketplaceAppInRead,
			"name":    "OpenClaw",
			"version": "1.0",
		}
	}
	return map[string]any{
		"data": map[string]any{
			"id":         testVMMktID,
			"type":       "virtual_machines",
			"attributes": attrs,
		},
	}
}

func (m *vmMarketplaceMock) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/virtual_machines":
			var payload struct {
				Data struct {
					Attributes map[string]any `json:"attributes"`
				} `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if v, ok := payload.Data.Attributes["marketplace_app"].(string); ok {
				m.createdMarketplaceApp = &v
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(m.envelope())
		case r.Method == http.MethodGet && r.URL.Path == "/virtual_machines/"+testVMMktID:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(m.envelope())
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func newTestVMMarketplaceResource(serverURL string) *VirtualMachineResource {
	return &VirtualMachineResource{
		client: latitudeshgosdk.New(
			latitudeshgosdk.WithSecurity("test"),
			latitudeshgosdk.WithServerURL(serverURL),
		),
	}
}

// runVMMarketplaceCreate drives r.Create() with a plan carrying the given
// marketplace_app value and returns the response.
func runVMMarketplaceCreate(t *testing.T, r *VirtualMachineResource, marketplaceApp tftypes.Value) *resource.CreateResponse {
	t.Helper()
	ctx := context.Background()

	schemaResp := &resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, schemaResp)
	sch := schemaResp.Schema
	objType := sch.Type().TerraformType(ctx).(tftypes.Object)

	planVal := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":               tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"name":             tftypes.NewValue(tftypes.String, testVMName),
		"site":             tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"plan":             tftypes.NewValue(tftypes.String, testVMPlan),
		"project":          tftypes.NewValue(tftypes.String, "test-project"),
		"operating_system": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"marketplace_app":  marketplaceApp,
		"billing":          tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"ssh_keys":         tftypes.NewValue(objType.AttributeTypes["ssh_keys"], nil),
		"status":           tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"primary_ipv4":     tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"created_at":       tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"ssh_user":         tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"vcpu":             tftypes.NewValue(tftypes.Number, tftypes.UnknownValue),
		"ram":              tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"storage":          tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"timeouts":         tftypes.NewValue(objType.AttributeTypes["timeouts"], nil),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Raw: planVal, Schema: sch}}
	resp := &resource.CreateResponse{
		State: tfsdk.State{Raw: tftypes.NewValue(objType, nil), Schema: sch},
	}
	r.Create(ctx, req, resp)
	return resp
}

// TestVirtualMachineCreate_MarketplaceAppSent: a configured marketplace_app
// must reach the create payload verbatim.
func TestVirtualMachineCreate_MarketplaceAppSent(t *testing.T) {
	m := &vmMarketplaceMock{}
	server := m.server(t)

	resp := runVMMarketplaceCreate(t, newTestVMMarketplaceResource(server.URL),
		tftypes.NewValue(tftypes.String, "openclaw"))

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics.Errors())
	}
	if m.createdMarketplaceApp == nil {
		t.Fatalf("create payload did not include marketplace_app")
	}
	if *m.createdMarketplaceApp != "openclaw" {
		t.Fatalf("create payload marketplace_app = %q, want %q", *m.createdMarketplaceApp, "openclaw")
	}
}

// TestVirtualMachineCreate_MarketplaceAppOmittedWhenUnset: when marketplace_app
// is not configured, it must not be sent in the create payload, and the
// resulting state must be null (not the zero value) so a later plan does not
// show spurious drift.
func TestVirtualMachineCreate_MarketplaceAppOmittedWhenUnset(t *testing.T) {
	m := &vmMarketplaceMock{}
	server := m.server(t)

	resp := runVMMarketplaceCreate(t, newTestVMMarketplaceResource(server.URL),
		tftypes.NewValue(tftypes.String, nil))

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics.Errors())
	}
	if m.createdMarketplaceApp != nil {
		t.Fatalf("create payload included marketplace_app %q, want it omitted", *m.createdMarketplaceApp)
	}

	var out VirtualMachineResourceModel
	if diags := resp.State.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("reading state: %v", diags.Errors())
	}
	if !out.MarketplaceApp.IsNull() {
		t.Fatalf("expected marketplace_app to stay null when the API omits it, got %#v", out.MarketplaceApp)
	}
}

// TestVirtualMachineCreate_MarketplaceAppComputedFromAPI: when the API reports
// a marketplace_app snapshot on read-back (e.g. re-read shortly after create),
// its slug must populate the attribute even though the config did not set it.
func TestVirtualMachineCreate_MarketplaceAppComputedFromAPI(t *testing.T) {
	echoed := "openclaw"
	m := &vmMarketplaceMock{marketplaceAppInRead: &echoed}
	server := m.server(t)

	resp := runVMMarketplaceCreate(t, newTestVMMarketplaceResource(server.URL),
		tftypes.NewValue(tftypes.String, nil))

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics.Errors())
	}

	var out VirtualMachineResourceModel
	if diags := resp.State.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("reading state: %v", diags.Errors())
	}
	if got := out.MarketplaceApp.ValueString(); got != echoed {
		t.Fatalf("expected marketplace_app %q read back from the API, got %q", echoed, got)
	}
}

// TestVirtualMachineSchema_MarketplaceAppRequiresReplace: the app is installed
// at provision time and the update API takes no marketplace_app, so changing it
// must force a new resource instead of being silently written to state by the
// no-op Update path.
func TestVirtualMachineSchema_MarketplaceAppRequiresReplace(t *testing.T) {
	ctx := context.Background()

	schemaResp := &resource.SchemaResponse{}
	(&VirtualMachineResource{}).Schema(ctx, resource.SchemaRequest{}, schemaResp)

	attr, ok := schemaResp.Schema.Attributes["marketplace_app"].(schema.StringAttribute)
	if !ok {
		t.Fatalf("marketplace_app is not a StringAttribute: %#v", schemaResp.Schema.Attributes["marketplace_app"])
	}

	// Simulate an update (non-null state and plan) that changes marketplace_app
	// on an existing VM.
	objType := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	attrValues := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, attrType := range objType.AttributeTypes {
		attrValues[name] = tftypes.NewValue(attrType, nil)
	}
	raw := tftypes.NewValue(objType, attrValues)

	req := planmodifier.StringRequest{
		Path:        path.Root("marketplace_app"),
		ConfigValue: types.StringValue("wordpress"),
		PlanValue:   types.StringValue("wordpress"),
		StateValue:  types.StringValue("openclaw"),
		State:       tfsdk.State{Raw: raw, Schema: schemaResp.Schema},
		Plan:        tfsdk.Plan{Raw: raw, Schema: schemaResp.Schema},
	}

	var replaced bool
	for _, m := range attr.PlanModifiers {
		resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
		m.PlanModifyString(ctx, req, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("plan modifier returned diagnostics: %v", resp.Diagnostics.Errors())
		}
		if resp.RequiresReplace {
			replaced = true
		}
	}
	if !replaced {
		t.Fatal("changing marketplace_app must force a new resource")
	}
}
