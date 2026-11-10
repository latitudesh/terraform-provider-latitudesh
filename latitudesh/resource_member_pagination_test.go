package latitudesh

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

// Regression tests for the team member lookup. Read and ImportState find a
// member by walking GET /team/members. They used to read only the first page,
// which the API caps at 20 members by default, so in a larger team the member
// was not found and its id was set to null.

const (
	memberTargetID    = "user_TARGET"
	memberTargetEmail = "target@example.com"
)

// teamMembersMock serves a team of size members. When targetIndex >= 0, the
// targetIndex-th member is the target.
type teamMembersMock struct {
	size        int
	targetIndex int
	requests    int
}

func (m *teamMembersMock) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/team/members" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		m.requests++

		number, size := 1, 20
		if n, err := strconv.Atoi(r.URL.Query().Get("page[number]")); err == nil && n > 0 {
			number = n
		}
		if s, err := strconv.Atoi(r.URL.Query().Get("page[size]")); err == nil && s > 0 {
			size = s
		}

		entries := []string{}
		for i := (number - 1) * size; i < number*size && i < m.size; i++ {
			id, email, first := fmt.Sprintf("user_%d", i), fmt.Sprintf("member%d@example.com", i), fmt.Sprintf("Member%d", i)
			if i == m.targetIndex {
				id, email, first = memberTargetID, memberTargetEmail, "Target"
			}
			entries = append(entries, fmt.Sprintf(`{"id":%q,"type":"memberships","attributes":{`+
				`"first_name":%q,"last_name":"User","email":%q,"mfa_enabled":false,"role":{"id":"role_1","name":"collaborator"}}}`,
				id, first, email))
		}

		w.Header().Set("Content-Type", "application/vnd.api+json")
		_, _ = w.Write([]byte(`{"data":[` + strings.Join(entries, ",") + `],"meta":{}}`))
	}))
	t.Cleanup(s.Close)
	return s
}

func newTestMemberResource(serverURL string) *MemberResource {
	return &MemberResource{
		client: latitudeshgosdk.New(
			latitudeshgosdk.WithSecurity("test"),
			latitudeshgosdk.WithServerURL(serverURL),
		),
	}
}

func memberState(t *testing.T, r *MemberResource, id, email string) tfsdk.State {
	t.Helper()
	ctx := context.Background()

	schemaResp := &resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, schemaResp)
	sch := schemaResp.Schema
	objType := sch.Type().TerraformType(ctx).(tftypes.Object)

	values := map[string]tftypes.Value{}
	for name, typ := range objType.AttributeTypes {
		values[name] = tftypes.NewValue(typ, nil)
	}
	values["id"] = tftypes.NewValue(tftypes.String, id)
	values["email"] = tftypes.NewValue(tftypes.String, email)
	values["role"] = tftypes.NewValue(tftypes.String, "collaborator")

	return tfsdk.State{Raw: tftypes.NewValue(objType, values), Schema: sch}
}

func runMemberRead(t *testing.T, r *MemberResource, id, email string) *resource.ReadResponse {
	t.Helper()
	state := memberState(t, r, id, email)
	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	return resp
}

func stateString(t *testing.T, s tfsdk.State, attr string) types.String {
	t.Helper()
	var v types.String
	if diags := s.GetAttribute(context.Background(), path.Root(attr), &v); diags.HasError() {
		t.Fatalf("reading %s from state: %v", attr, diags.Errors())
	}
	return v
}

func TestMemberRead_BeyondFirstPage(t *testing.T) {
	m := &teamMembersMock{size: 130, targetIndex: 120}
	resp := runMemberRead(t, newTestMemberResource(m.server(t).URL), memberTargetID, memberTargetEmail)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics.Errors())
	}
	if resp.State.Raw.IsNull() {
		t.Fatal("member exists past the first page but Read removed it from state")
	}
	if got := stateString(t, resp.State, "id"); got.ValueString() != memberTargetID {
		t.Fatalf("id = %s, want %s", got, memberTargetID)
	}
	if got := stateString(t, resp.State, "first_name"); got.ValueString() != "Target" {
		t.Fatalf("first_name = %s, want Target", got)
	}
	if m.requests != 2 {
		t.Fatalf("expected 2 page requests, got %d", m.requests)
	}
}

func TestMemberRead_EmailFallbackBeyondFirstPage(t *testing.T) {
	m := &teamMembersMock{size: 130, targetIndex: 120}
	resp := runMemberRead(t, newTestMemberResource(m.server(t).URL), "user_STALE", memberTargetEmail)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics.Errors())
	}
	if resp.State.Raw.IsNull() {
		t.Fatal("member with the same email exists past the first page but Read removed it from state")
	}
	if got := stateString(t, resp.State, "id"); got.ValueString() != memberTargetID {
		t.Fatalf("id = %s, want %s (matched by email)", got, memberTargetID)
	}
}

func TestMemberRead_DeletedOutOfBand(t *testing.T) {
	m := &teamMembersMock{size: 130, targetIndex: -1}
	resp := runMemberRead(t, newTestMemberResource(m.server(t).URL), memberTargetID, memberTargetEmail)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics.Errors())
	}
	if !resp.State.Raw.IsNull() {
		t.Fatal("member is on no page but Read kept it in state")
	}
}

func TestMemberImport_BeyondFirstPage(t *testing.T) {
	m := &teamMembersMock{size: 130, targetIndex: 120}
	r := newTestMemberResource(m.server(t).URL)

	empty := memberState(t, r, "", "")
	empty.Raw = tftypes.NewValue(empty.Schema.Type().TerraformType(context.Background()), nil)
	resp := &resource.ImportStateResponse{State: empty}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: memberTargetID}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("ImportState returned diagnostics: %v", resp.Diagnostics.Errors())
	}
	if got := stateString(t, resp.State, "email"); got.ValueString() != memberTargetEmail {
		t.Fatalf("email = %s, want %s", got, memberTargetEmail)
	}
}
