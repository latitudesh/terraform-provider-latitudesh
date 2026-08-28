package latitudesh

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// TestMapAPIKeyToModel_TokenPreservedWhenOmitted guards the core nuance of
// this resource: components.Attributes.Token is documented as "only returned
// on create or rotate", so a nil Token in a List/Update response means "not
// included in this response", not "the key has no token". mapAPIKeyToModel
// must leave a pre-existing state token untouched in that case rather than
// nulling it out.
func TestMapAPIKeyToModel_TokenPreservedWhenOmitted(t *testing.T) {
	r := &APIKeyResource{}

	data := &APIKeyResourceModel{
		Token: types.StringValue("lsh_existing_token"),
	}

	name := "my-key"
	readOnly := false
	key := &components.APIKey{
		ID: strPtr("key_123"),
		Attributes: &components.Attributes{
			Name:     &name,
			ReadOnly: &readOnly,
			// Token intentionally nil, mirroring a List/UpdateAPIKey response.
		},
	}

	r.mapAPIKeyToModel(key, data)

	if data.Token.ValueString() != "lsh_existing_token" {
		t.Errorf("Token = %q, want preserved value %q", data.Token.ValueString(), "lsh_existing_token")
	}
	if data.Name.ValueString() != name {
		t.Errorf("Name = %q, want %q", data.Name.ValueString(), name)
	}
}

// TestMapAPIKeyToModel_TokenSetOnCreate is the complementary case: when the
// API does include a Token (the create/rotate response), it must be mapped
// into state.
func TestMapAPIKeyToModel_TokenSetOnCreate(t *testing.T) {
	r := &APIKeyResource{}

	data := &APIKeyResourceModel{
		Token: types.StringNull(),
	}

	token := "lsh_brand_new_token"
	key := &components.APIKey{
		ID: strPtr("key_123"),
		Attributes: &components.Attributes{
			Token: &token,
		},
	}

	r.mapAPIKeyToModel(key, data)

	if data.Token.ValueString() != token {
		t.Errorf("Token = %q, want %q", data.Token.ValueString(), token)
	}
}

// TestMapAPIKeyToModel_AllowedIpsNilBecomesNull guards the nil-pointer
// mapping convention used throughout this provider: a nil AllowedIps from
// the API maps to a known-null list rather than being left stale.
func TestMapAPIKeyToModel_AllowedIpsNilBecomesNull(t *testing.T) {
	r := &APIKeyResource{}
	data := &APIKeyResourceModel{
		AllowedIps: types.ListNull(types.StringType),
	}

	key := &components.APIKey{
		ID:         strPtr("key_123"),
		Attributes: &components.Attributes{},
	}

	r.mapAPIKeyToModel(key, data)

	if !data.AllowedIps.IsNull() {
		t.Errorf("AllowedIps = %v, want null", data.AllowedIps)
	}
}
