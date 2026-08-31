package latitudesh

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
)

// eventDataSample is a representative /events entry: every nested object
// populated, plus the always-empty "properties" object the API sends.
const eventDataSample = `{
  "id": "evt_123",
  "type": "events",
  "attributes": {
    "action": "server.power_on",
    "created_at": "2026-07-01T12:00:00Z",
    "author": {"id": "user_1", "name": "Ada Lovelace", "email": "ada@example.com"},
    "project": {"id": "proj_1", "name": "Prod", "slug": "prod"},
    "team": {"id": "team_1", "name": "Ops"},
    "target": {"id": "srv_1", "name": "web-01"},
    "properties": {}
  }
}`

func TestEventsValueMapsAllFields(t *testing.T) {
	ctx := context.Background()

	var e components.EventData
	if err := json.Unmarshal([]byte(eventDataSample), &e); err != nil {
		t.Fatalf("unmarshaling event: %s", err)
	}

	list, diags := eventsValue(ctx, []components.EventData{e})
	if diags.HasError() {
		t.Fatalf("eventsValue diagnostics: %v", diags)
	}

	var got []EventModel
	if d := list.ElementsAs(ctx, &got, false); d.HasError() {
		t.Fatalf("ElementsAs: %v", d)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}

	ev := got[0]
	if ev.ID.ValueString() != "evt_123" {
		t.Errorf("id = %q, want evt_123", ev.ID.ValueString())
	}
	if ev.Type.ValueString() != "events" {
		t.Errorf("type = %q, want events", ev.Type.ValueString())
	}
	if ev.Action.ValueString() != "server.power_on" {
		t.Errorf("action = %q, want server.power_on", ev.Action.ValueString())
	}
	if ev.CreatedAt.ValueString() != "2026-07-01T12:00:00Z" {
		t.Errorf("created_at = %q, want 2026-07-01T12:00:00Z", ev.CreatedAt.ValueString())
	}

	var author EventAuthorModel
	if d := ev.Author.As(ctx, &author, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatalf("author As: %v", d)
	}
	if author.Name.ValueString() != "Ada Lovelace" {
		t.Errorf("author name = %q, want Ada Lovelace", author.Name.ValueString())
	}
	if author.Email.ValueString() != "ada@example.com" {
		t.Errorf("author email = %q, want ada@example.com", author.Email.ValueString())
	}

	var project EventProjectModel
	if d := ev.Project.As(ctx, &project, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatalf("project As: %v", d)
	}
	if project.Slug.ValueString() != "prod" {
		t.Errorf("project slug = %q, want prod", project.Slug.ValueString())
	}

	var team EventTeamModel
	if d := ev.Team.As(ctx, &team, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatalf("team As: %v", d)
	}
	if team.Name.ValueString() != "Ops" {
		t.Errorf("team name = %q, want Ops", team.Name.ValueString())
	}

	var target EventTargetModel
	if d := ev.Target.As(ctx, &target, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatalf("target As: %v", d)
	}
	if target.Name.ValueString() != "web-01" {
		t.Errorf("target name = %q, want web-01", target.Name.ValueString())
	}
}

// TestEventsValueNilAttributesAndEmptyNeverNull guards two shapes at once: an
// event whose "attributes" the API omitted entirely (every nested field falls
// back to null, not a zero-value struct), and the "no matching events" case,
// which must still be a known empty list so `[for e in data.events : ...]`
// never fails on a null iteratee.
func TestEventsValueNilAttributesAndEmptyNeverNull(t *testing.T) {
	ctx := context.Background()

	id := "evt_no_attrs"
	list, diags := eventsValue(ctx, []components.EventData{{ID: &id}})
	if diags.HasError() {
		t.Fatalf("eventsValue diagnostics: %v", diags)
	}
	var got []EventModel
	if d := list.ElementsAs(ctx, &got, false); d.HasError() {
		t.Fatalf("ElementsAs: %v", d)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if !got[0].Action.IsNull() {
		t.Error("action should be null when attributes is nil")
	}
	if !got[0].Author.IsNull() {
		t.Error("author should be null when attributes is nil")
	}

	empty, diags := eventsValue(ctx, nil)
	if diags.HasError() {
		t.Fatalf("eventsValue(nil) diagnostics: %v", diags)
	}
	if empty.IsNull() {
		t.Fatal("eventsValue(nil) returned a null list; want an empty known list")
	}
	if n := len(empty.Elements()); n != 0 {
		t.Fatalf("eventsValue(nil) length = %d, want 0", n)
	}
}

// TestEventsQueryIDStableAndSensitive pins the two properties the computed
// "id" attribute depends on: it must be deterministic for a given set of
// filters (so it doesn't churn state across reads), and it must change when a
// filter changes (so two differently-filtered data sources don't collide).
func TestEventsQueryIDStableAndSensitive(t *testing.T) {
	author := "user_1"
	req1 := operations.GetEventsRequest{FilterAuthor: &author}
	req2 := operations.GetEventsRequest{FilterAuthor: &author}
	if eventsQueryID(req1) != eventsQueryID(req2) {
		t.Error("eventsQueryID is not deterministic for identical requests")
	}

	otherAuthor := "user_2"
	req3 := operations.GetEventsRequest{FilterAuthor: &otherAuthor}
	if eventsQueryID(req1) == eventsQueryID(req3) {
		t.Error("eventsQueryID did not change when filter_author changed")
	}

	empty := operations.GetEventsRequest{}
	if eventsQueryID(req1) == eventsQueryID(empty) {
		t.Error("eventsQueryID did not change between a filtered and an unfiltered request")
	}
}
