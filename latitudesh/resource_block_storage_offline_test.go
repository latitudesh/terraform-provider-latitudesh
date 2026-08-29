package latitudesh

import (
	"context"
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// TestBlockStorageInitiatorsValueNeverNull guards the "always a list, never
// null" guarantee so a customer's `for`/`length` expression over `initiators`
// never errors on a null iteratee, even for a volume the API returns with no
// initiators.
func TestBlockStorageInitiatorsValueNeverNull(t *testing.T) {
	ctx := context.Background()
	list, diags := blockStorageInitiatorsValue(ctx, nil)
	if diags.HasError() {
		t.Fatalf("blockStorageInitiatorsValue(nil) diagnostics: %v", diags)
	}
	if list.IsNull() {
		t.Fatal("blockStorageInitiatorsValue(nil) returned a null list; want an empty known list")
	}
	if n := len(list.Elements()); n != 0 {
		t.Fatalf("blockStorageInitiatorsValue(nil) length = %d, want 0", n)
	}
}

func TestBlockStorageInitiatorsValueMapsNqn(t *testing.T) {
	ctx := context.Background()
	nqn := "nqn.2024-01.com.example:server01"

	list, diags := blockStorageInitiatorsValue(ctx, []components.Initiators{
		{Nqn: &nqn},
		{},
	})
	if diags.HasError() {
		t.Fatalf("blockStorageInitiatorsValue diagnostics: %v", diags)
	}

	var got []BlockStorageInitiatorModel
	if d := list.ElementsAs(ctx, &got, false); d.HasError() {
		t.Fatalf("ElementsAs: %v", d)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 initiators, got %d", len(got))
	}
	if got[0].Nqn.ValueString() != nqn {
		t.Errorf("initiator 0 nqn = %q, want %q", got[0].Nqn.ValueString(), nqn)
	}
	if !got[1].Nqn.IsNull() {
		t.Errorf("initiator 1 nqn = %q, want null", got[1].Nqn.ValueString())
	}
}
