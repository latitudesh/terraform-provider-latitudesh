package latitudesh

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

// Regression tests for resources deleted outside of Terraform.
//
// Read must drop a resource whose GET answers 404 so Terraform plans a fresh
// create. Before the fix, latitudesh_server, latitudesh_ssh_key and
// latitudesh_user_data failed the refresh with an error (blocking every plan
// until the entry was removed by hand), and latitudesh_firewall kept the entry
// in state with id = null.

func notFoundAPI(t *testing.T) *latitudeshgosdk.Latitudesh {
	t.Helper()
	return statusAPI(t, http.StatusNotFound)
}

// statusAPI answers every request with the given error status.
func statusAPI(t *testing.T, status int) *latitudeshgosdk.Latitudesh {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, `{"errors":[{"status":"%d"}]}`, status)
	}))
	t.Cleanup(s.Close)
	return latitudeshgosdk.New(
		latitudeshgosdk.WithSecurity("test"),
		latitudeshgosdk.WithServerURL(s.URL),
	)
}

// readWithOnlyID drives r.Read() with a prior state where every attribute is
// null except id, which is all Read needs to look the resource up.
func readWithOnlyID(t *testing.T, r resource.Resource, id string) *resource.ReadResponse {
	t.Helper()
	ctx := context.Background()

	schemaResp := &resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", schemaResp.Diagnostics.Errors())
	}
	sch := schemaResp.Schema
	objType := sch.Type().TerraformType(ctx).(tftypes.Object)

	attrs := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, typ := range objType.AttributeTypes {
		attrs[name] = tftypes.NewValue(typ, nil)
	}
	attrs["id"] = tftypes.NewValue(tftypes.String, id)
	stateVal := tftypes.NewValue(objType, attrs)

	req := resource.ReadRequest{State: tfsdk.State{Raw: stateVal, Schema: sch}}
	resp := &resource.ReadResponse{State: tfsdk.State{Raw: stateVal, Schema: sch}}
	r.Read(ctx, req, resp)
	return resp
}

var readNotFoundCases = []struct {
	name string
	new  func(*latitudeshgosdk.Latitudesh) resource.Resource
	id   string
}{
	{"server", func(c *latitudeshgosdk.Latitudesh) resource.Resource { return &ServerResource{client: c} }, "sv_gone"},
	{"ssh_key", func(c *latitudeshgosdk.Latitudesh) resource.Resource { return &SSHKeyResource{client: c} }, "ssh_gone"},
	{"user_data", func(c *latitudeshgosdk.Latitudesh) resource.Resource { return &UserDataResource{client: c} }, "ud_gone"},
	{"firewall", func(c *latitudeshgosdk.Latitudesh) resource.Resource { return &FirewallResource{client: c} }, "fw_gone"},
}

func TestRead_DeletedOutOfBand_RemovesFromState(t *testing.T) {
	for _, tc := range readNotFoundCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := readWithOnlyID(t, tc.new(notFoundAPI(t)), tc.id)

			if resp.Diagnostics.HasError() {
				t.Fatalf("expected a 404 to remove the resource from state, got diagnostics: %v", resp.Diagnostics.Errors())
			}
			if !resp.State.Raw.IsNull() {
				t.Fatalf("expected the resource to be removed from state, got %s", resp.State.Raw)
			}
		})
	}
}

// Any other failed GET must stay an error and keep the prior state, so a
// transient API failure never makes Terraform plan to recreate a live object.
func TestRead_OtherErrors_KeepState(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError} {
		for _, tc := range readNotFoundCases {
			t.Run(fmt.Sprintf("%s/%d", tc.name, status), func(t *testing.T) {
				resp := readWithOnlyID(t, tc.new(statusAPI(t, status)), tc.id)

				if !resp.Diagnostics.HasError() {
					t.Fatalf("expected an error for HTTP %d", status)
				}
				if resp.State.Raw.IsNull() {
					t.Fatalf("HTTP %d removed the resource from state", status)
				}
			})
		}
	}
}

// Import of an unknown SSH key ID keeps the explicit error added for #116
// instead of Terraform's generic "non-existent remote object".
func TestSSHKeyImportState_NotFound(t *testing.T) {
	ctx := context.Background()
	r := &SSHKeyResource{client: notFoundAPI(t)}

	schemaResp := &resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, schemaResp)
	sch := schemaResp.Schema

	resp := &resource.ImportStateResponse{
		State: tfsdk.State{Raw: tftypes.NewValue(sch.Type().TerraformType(ctx), nil), Schema: sch},
	}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "ssh_invalid"}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error importing an unknown SSH key")
	}
	if got := resp.Diagnostics.Errors()[0].Summary(); !strings.EqualFold(got, "SSH key not found") {
		t.Fatalf("expected summary %q, got %q", "SSH key not found", got)
	}
}
