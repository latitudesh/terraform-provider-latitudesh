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

// Regression tests for the project Read path.
//
// readProject used to call GET /projects with no pagination and search the
// first page (20 projects by default) for the ID. A team with more projects
// than that got `id = null` for any project past the first page, and a project
// deleted outside Terraform stayed in state with `id = null` instead of being
// removed. Read now uses GET /projects/{id}.

const testReadProjectID = "proj_TARGET"

func projectJSON(id, name string) string {
	return fmt.Sprintf(`{"id":%q,"type":"projects","attributes":{`+
		`"name":%q,"slug":%q,"description":"desc","environment":"Production",`+
		`"provisioning_type":"on_demand","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-02T00:00:00Z"}}`,
		id, name, strings.ToLower(name))
}

// projectsAPI serves GET /projects (a full first page of other projects, so the
// target is never on it) and GET /projects/{id}, which returns the target when
// exists is true and 404 otherwise.
func projectsAPI(t *testing.T, exists bool) *latitudeshgosdk.Latitudesh {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects":
			entries := make([]string, 0, 20)
			for i := 0; i < 20; i++ {
				entries = append(entries, projectJSON(fmt.Sprintf("proj_FILL%d", i), fmt.Sprintf("filler%d", i)))
			}
			_, _ = w.Write([]byte(`{"data":[` + strings.Join(entries, ",") + `],"meta":{}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+testReadProjectID && exists:
			_, _ = w.Write([]byte(`{"data":` + projectJSON(testReadProjectID, "target") + `,"meta":{}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404","title":"Not Found"}]}`))
		}
	}))
	t.Cleanup(s.Close)
	return latitudeshgosdk.New(
		latitudeshgosdk.WithSecurity("test"),
		latitudeshgosdk.WithServerURL(s.URL),
	)
}

func runProjectRead(t *testing.T, r *ProjectResource) *resource.ReadResponse {
	t.Helper()
	ctx := context.Background()

	schemaResp := &resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, schemaResp)
	sch := schemaResp.Schema
	objType := sch.Type().TerraformType(ctx).(tftypes.Object)

	attrs := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, typ := range objType.AttributeTypes {
		attrs[name] = tftypes.NewValue(typ, nil)
	}
	attrs["id"] = tftypes.NewValue(tftypes.String, testReadProjectID)
	attrs["name"] = tftypes.NewValue(tftypes.String, "target")
	stateVal := tftypes.NewValue(objType, attrs)

	req := resource.ReadRequest{State: tfsdk.State{Raw: stateVal, Schema: sch}}
	resp := &resource.ReadResponse{State: tfsdk.State{Raw: stateVal, Schema: sch}}
	r.Read(ctx, req, resp)
	return resp
}

func TestProjectRead_NotOnFirstPage(t *testing.T) {
	resp := runProjectRead(t, &ProjectResource{client: projectsAPI(t, true)})

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics.Errors())
	}
	if resp.State.Raw.IsNull() {
		t.Fatal("project exists but Read removed it from state")
	}

	var out ProjectResourceModel
	if diags := resp.State.Get(context.Background(), &out); diags.HasError() {
		t.Fatalf("reading state: %v", diags.Errors())
	}
	if got := out.ID.ValueString(); got != testReadProjectID {
		t.Fatalf("expected id %q, got %q (null: %v)", testReadProjectID, got, out.ID.IsNull())
	}
	if got := out.Slug.ValueString(); got != "target" {
		t.Fatalf("expected slug %q, got %q", "target", got)
	}
	if got := out.CreatedAt.ValueString(); got != "2026-01-01T00:00:00Z" {
		t.Fatalf("expected created_at to be refreshed, got %q", got)
	}
}

func TestProjectRead_DeletedOutOfBand(t *testing.T) {
	resp := runProjectRead(t, &ProjectResource{client: projectsAPI(t, false)})

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics.Errors())
	}
	if !resp.State.Raw.IsNull() {
		t.Fatalf("expected a deleted project to be removed from state, got %s", resp.State.Raw)
	}
}
