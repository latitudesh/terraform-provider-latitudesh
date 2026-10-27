package latitudesh

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

// An interrupted apply cancels the provider's context. When that lands while a
// pool's Create is waiting for its cluster, the pool itself is already up, so
// Create must still end clean: the pool in state, the cluster's readiness
// reported as a warning, and no error. An error would taint the pool, and the
// next apply would rebuild healthy bare metal. This drives Create directly
// because the cancellation has to land at one exact point — the first cluster
// poll after the pool exists.
func TestLksNodePoolCreate_CancelledDuringClusterWaitDoesNotTaint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mock := &mockLksAPI{readyNeedsNodePool: true, clusterNeverReady: true}
	mock.id, mock.exists, mock.name, mock.version = "lks_mock_1", true, "tf-test-cluster", "1.31.0"
	mock.onClusterGetWithPool = cancel
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	prevReady, prevPool := lksReadyPollInterval, lksNodePoolReadyPollInterval
	lksReadyPollInterval = 5 * time.Millisecond
	lksNodePoolReadyPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		lksReadyPollInterval = prevReady
		lksNodePoolReadyPollInterval = prevPool
	})

	r := &LksNodePoolResource{
		client: latitudeshgosdk.New(
			latitudeshgosdk.WithSecurity("test"),
			latitudeshgosdk.WithServerURL(server.URL),
		),
	}

	schemaResp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, schemaResp)
	sch := schemaResp.Schema
	objType := sch.Type().TerraformType(context.Background()).(tftypes.Object)

	// What a plan for a minimal pool looks like: the configured attributes
	// known, every other Computed one unknown, everything else null.
	vals := map[string]tftypes.Value{}
	for name, typ := range objType.AttributeTypes {
		if a, ok := sch.Attributes[name]; ok && a.IsComputed() {
			vals[name] = tftypes.NewValue(typ, tftypes.UnknownValue)
			continue
		}
		vals[name] = tftypes.NewValue(typ, nil)
	}
	vals["cluster_id"] = tftypes.NewValue(tftypes.String, "lks_mock_1")
	vals["plan"] = tftypes.NewValue(tftypes.String, "c2-medium-x86")
	vals["node_count"] = tftypes.NewValue(tftypes.Number, 1)

	req := resource.CreateRequest{Plan: tfsdk.Plan{Raw: tftypes.NewValue(objType, vals), Schema: sch}}
	resp := &resource.CreateResponse{State: tfsdk.State{Raw: tftypes.NewValue(objType, nil), Schema: sch}}
	r.Create(ctx, req, resp)

	if ctx.Err() == nil {
		t.Fatal("the context was never cancelled, so the cluster wait was not reached")
	}
	if resp.Diagnostics.HasError() {
		t.Fatalf("a cancelled cluster wait must not fail the pool's create (it would taint the pool): %v", resp.Diagnostics.Errors())
	}
	if len(resp.Diagnostics.Warnings()) == 0 {
		t.Fatal("expected a warning that the cluster's readiness could not be confirmed")
	}

	var id, status types.String
	resp.Diagnostics.Append(resp.State.GetAttribute(context.Background(), path.Root("id"), &id)...)
	resp.Diagnostics.Append(resp.State.GetAttribute(context.Background(), path.Root("status"), &status)...)
	if id.ValueString() != "np_mock_1" || status.ValueString() != "ready" {
		t.Fatalf("the pool must be recorded as created and ready, got id=%s status=%s", id, status)
	}
}
