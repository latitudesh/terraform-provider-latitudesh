package latitudesh

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func boolPtr(b bool) *bool { return &b }

// TestMapAPIKeyToModel_PreservesTokenOnRead guards the special case that makes
// this resource usable at all: List (the only read path available, since the
// API has no single-item GET) never returns the full token, so a Read/Update
// refresh must not null out a token captured at Create.
func TestMapAPIKeyToModel_PreservesTokenOnRead(t *testing.T) {
	ctx := context.Background()
	data := &APIKeyResourceModel{
		Token: types.StringValue("existing-token-value"),
	}
	diags := &diag.Diagnostics{}

	apiKey := &components.APIKey{
		ID: strPtr("api_key_1"),
		Attributes: &components.Attributes{
			Name: strPtr("ci-key"),
			// Token intentionally nil: this is what List actually returns.
		},
	}

	mapAPIKeyToModel(ctx, apiKey, data, diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	if data.Token.ValueString() != "existing-token-value" {
		t.Errorf("Token = %q, want preserved value %q", data.Token.ValueString(), "existing-token-value")
	}
}

// TestMapAPIKeyToModel_SetsTokenOnCreate guards the other half: when the API
// does return a token (the Create response), it must be captured into state.
func TestMapAPIKeyToModel_SetsTokenOnCreate(t *testing.T) {
	ctx := context.Background()
	data := &APIKeyResourceModel{
		Token: types.StringNull(),
	}
	diags := &diag.Diagnostics{}

	apiKey := &components.APIKey{
		ID: strPtr("api_key_1"),
		Attributes: &components.Attributes{
			Name:  strPtr("ci-key"),
			Token: strPtr("full-token-value"),
		},
	}

	mapAPIKeyToModel(ctx, apiKey, data, diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	if data.Token.ValueString() != "full-token-value" {
		t.Errorf("Token = %q, want %q", data.Token.ValueString(), "full-token-value")
	}
}

func TestMapAPIKeyToModel_NilAttributesNulled(t *testing.T) {
	ctx := context.Background()
	data := &APIKeyResourceModel{}
	diags := &diag.Diagnostics{}

	apiKey := &components.APIKey{
		ID: strPtr("api_key_1"),
		Attributes: &components.Attributes{
			Name: strPtr("ci-key"),
			// ReadOnly, User, CreatedAt, UpdatedAt, LastUsedAt all nil.
		},
	}

	mapAPIKeyToModel(ctx, apiKey, data, diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	if !data.ReadOnly.IsNull() {
		t.Errorf("ReadOnly = %v, want null", data.ReadOnly)
	}
	if !data.UserID.IsNull() || !data.UserEmail.IsNull() {
		t.Errorf("UserID/UserEmail = %v/%v, want null/null", data.UserID, data.UserEmail)
	}
	if !data.CreatedAt.IsNull() || !data.UpdatedAt.IsNull() || !data.LastUsedAt.IsNull() {
		t.Errorf("timestamps = %v/%v/%v, want all null", data.CreatedAt, data.UpdatedAt, data.LastUsedAt)
	}
	// allowed_ips is Computed and must always resolve to a known (possibly
	// empty) list, never null, so `for` expressions over it never error.
	if data.AllowedIps.IsNull() {
		t.Error("AllowedIps is null, want a known (possibly empty) list")
	}
}

func TestMapAPIKeyToModel_FullMapping(t *testing.T) {
	ctx := context.Background()
	data := &APIKeyResourceModel{}
	diags := &diag.Diagnostics{}

	created := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 7, 2, 8, 30, 0, 0, time.UTC)
	lastUsed := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)

	apiKey := &components.APIKey{
		ID: strPtr("api_key_42"),
		Attributes: &components.Attributes{
			Name:           strPtr("ci-key"),
			ReadOnly:       boolPtr(true),
			AllowedIps:     []string{"203.0.113.10", "198.51.100.0/24"},
			APIVersion:     strPtr("v1"),
			TokenLastSlice: strPtr("aBcDe"),
			User: &components.APIKeyUser{
				ID:    strPtr("user_1"),
				Email: strPtr("dev@example.com"),
			},
			CreatedAt:  &created,
			UpdatedAt:  &updated,
			LastUsedAt: &lastUsed,
		},
	}

	mapAPIKeyToModel(ctx, apiKey, data, diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	if data.ID.ValueString() != "api_key_42" {
		t.Errorf("ID = %q, want api_key_42", data.ID.ValueString())
	}
	if !data.ReadOnly.ValueBool() {
		t.Error("ReadOnly = false, want true")
	}
	if data.APIVersion.ValueString() != "v1" {
		t.Errorf("APIVersion = %q, want v1", data.APIVersion.ValueString())
	}
	if data.TokenLastSlice.ValueString() != "aBcDe" {
		t.Errorf("TokenLastSlice = %q, want aBcDe", data.TokenLastSlice.ValueString())
	}
	if data.UserID.ValueString() != "user_1" || data.UserEmail.ValueString() != "dev@example.com" {
		t.Errorf("User = %q/%q, want user_1/dev@example.com", data.UserID.ValueString(), data.UserEmail.ValueString())
	}
	if data.CreatedAt.ValueString() != created.Format(time.RFC3339) {
		t.Errorf("CreatedAt = %q, want %q", data.CreatedAt.ValueString(), created.Format(time.RFC3339))
	}

	var ips []string
	if d := data.AllowedIps.ElementsAs(ctx, &ips, false); d.HasError() {
		t.Fatalf("ElementsAs: %v", d)
	}
	if len(ips) != 2 || ips[0] != "203.0.113.10" || ips[1] != "198.51.100.0/24" {
		t.Errorf("AllowedIps = %v, want [203.0.113.10 198.51.100.0/24]", ips)
	}
}
