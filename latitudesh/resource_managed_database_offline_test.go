package latitudesh

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func int64Ptr(i int64) *int64 { return &i }

func TestManagedDatabaseSchema_SensitiveCredentials(t *testing.T) {
	r := &ManagedDatabaseResource{}
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}
	creds, ok := resp.Schema.Attributes["credentials"].(schema.SingleNestedAttribute)
	if !ok {
		t.Fatalf("credentials is %T, want SingleNestedAttribute", resp.Schema.Attributes["credentials"])
	}
	for name, wantSensitive := range map[string]bool{"password": true, "url": true, "host": false, "username": false, "database": false} {
		a, ok := creds.Attributes[name].(schema.StringAttribute)
		if !ok {
			t.Fatalf("credentials.%s is %T", name, creds.Attributes[name])
		}
		if a.Sensitive != wantSensitive {
			t.Errorf("credentials.%s Sensitive = %v, want %v", name, a.Sensitive, wantSensitive)
		}
	}
}

func mdbTestModel() ManagedDatabaseResourceModel {
	return ManagedDatabaseResourceModel{
		ID:             types.StringValue("mdb_1"),
		Project:        types.StringValue("my-slug"),
		Name:           types.StringValue("api"),
		Region:         types.StringValue("ASH"),
		Plan:           types.StringValue("db.psql.small"),
		Engine:         types.StringValue("postgres"),
		Billing:        types.StringValue("monthly"),
		TrustedSources: types.SetValueMust(types.StringType, []attr.Value{types.StringValue("10.0.0.0/24")}),
		Parameters:     types.MapValueMust(types.StringType, map[string]attr.Value{"work_mem": types.StringValue("64MB")}),
		Status:         types.StringValue("running"),
		Credentials: types.ObjectValueMust(mdbCredentialsAttrTypes, map[string]attr.Value{
			"host":     types.StringValue("db.prior"),
			"port":     types.Int64Value(5432),
			"database": types.StringValue("app"),
			"username": types.StringValue("admin"),
			"password": types.StringValue("prior-secret"),
			"url":      types.StringValue("postgres://admin:prior-secret@db.prior:5432/app"),
		}),
	}
}

func mdbTestAttrs() *components.ManagedDatabaseAttributesAttributes {
	return &components.ManagedDatabaseAttributesAttributes{
		Name:           strPtr("api-normalised"),
		Engine:         strPtr("postgres"),
		Status:         strPtr("running"),
		TrustedSources: []string{"10.0.0.0/24", "10.1.0.0/24"},
		Parameters:     map[string]string{"work_mem": "65536kB", "max_connections": "100"},
		Plan:           &components.ManagedDatabaseAttributesPlan{Slug: strPtr("db.psql.small"), Billing: strPtr("yearly")},
		Region:         &components.ManagedDatabaseAttributesRegion{Site: &components.ManagedDatabaseAttributesSite{Slug: strPtr("ASH")}},
		Project:        &components.ProjectInclude{ID: strPtr("proj_1")},
		Credentials: &components.ManagedDatabaseAttributesCredentials{
			Host:     strPtr("db.api"),
			Port:     int64Ptr(5433),
			Database: strPtr("app"),
			Username: strPtr("admin"),
			Password: strPtr("api-secret"),
			URL:      strPtr("postgres://admin:api-secret@db.api:5433/app"),
		},
	}
}

func credAttr(t *testing.T, obj types.Object, name string) attr.Value {
	t.Helper()
	if obj.IsNull() || obj.IsUnknown() {
		t.Fatalf("credentials is null/unknown, want an object")
	}
	return obj.Attributes()[name]
}

func TestMdbApplyAttributes_Refresh(t *testing.T) {
	ctx := context.Background()

	t.Run("selectors keep configured values, API wins for trusted_sources and tracked parameters", func(t *testing.T) {
		var diags diag.Diagnostics
		data := mdbTestModel()
		mdbApplyAttributes(ctx, &data, nil, mdbTestAttrs(), nil, &diags)
		if diags.HasError() {
			t.Fatal(diags)
		}
		if got := data.Name.ValueString(); got != "api" {
			t.Errorf("name = %q, want the configured %q", got, "api")
		}
		if got := data.Project.ValueString(); got != "my-slug" {
			t.Errorf("project = %q, want the configured slug", got)
		}
		if got := data.Billing.ValueString(); got != "monthly" {
			t.Errorf("billing = %q, want the configured %q (selector)", got, "monthly")
		}
		if got := len(data.TrustedSources.Elements()); got != 2 {
			t.Errorf("trusted_sources has %d elements, want the API's 2", got)
		}
		params := data.Parameters.Elements()
		if len(params) != 1 {
			t.Errorf("parameters = %v, want only the tracked key", params)
		}
		if got := params["work_mem"].(types.String).ValueString(); got != "65536kB" {
			t.Errorf("parameters.work_mem = %q, want the API's value", got)
		}
		if got := credAttr(t, data.Credentials, "password").(types.String).ValueString(); got != "api-secret" {
			t.Errorf("password = %q, want the API's value", got)
		}
	})

	t.Run("API omissions keep prior values", func(t *testing.T) {
		var diags diag.Diagnostics
		data := mdbTestModel()
		attrs := mdbTestAttrs()
		attrs.TrustedSources = nil
		attrs.Parameters = map[string]string{"max_connections": "100"}
		attrs.Credentials = nil
		mdbApplyAttributes(ctx, &data, nil, attrs, nil, &diags)
		if got := len(data.TrustedSources.Elements()); got != 1 {
			t.Errorf("trusted_sources has %d elements, want the prior 1", got)
		}
		if got := data.Parameters.Elements()["work_mem"].(types.String).ValueString(); got != "64MB" {
			t.Errorf("parameters.work_mem = %q, want the prior value", got)
		}
		if got := credAttr(t, data.Credentials, "password").(types.String).ValueString(); got != "prior-secret" {
			t.Errorf("password = %q, want the prior value", got)
		}
	})

	t.Run("credentials without a password keep the prior password", func(t *testing.T) {
		var diags diag.Diagnostics
		data := mdbTestModel()
		attrs := mdbTestAttrs()
		attrs.Credentials.Password = nil
		attrs.Credentials.URL = nil
		mdbApplyAttributes(ctx, &data, nil, attrs, nil, &diags)
		if got := credAttr(t, data.Credentials, "password").(types.String).ValueString(); got != "prior-secret" {
			t.Errorf("password = %q, want the prior value", got)
		}
		if got := credAttr(t, data.Credentials, "host").(types.String).ValueString(); got != "db.api" {
			t.Errorf("host = %q, want the API's value", got)
		}
	})

	t.Run("import fills every selector from the API", func(t *testing.T) {
		var diags diag.Diagnostics
		data := ManagedDatabaseResourceModel{
			ID:             types.StringValue("mdb_1"),
			Project:        types.StringNull(),
			Name:           types.StringNull(),
			Region:         types.StringNull(),
			Plan:           types.StringNull(),
			Engine:         types.StringNull(),
			Billing:        types.StringNull(),
			TrustedSources: types.SetNull(types.StringType),
			Parameters:     types.MapNull(types.StringType),
			Credentials:    types.ObjectNull(mdbCredentialsAttrTypes),
		}
		mdbApplyAttributes(ctx, &data, nil, mdbTestAttrs(), nil, &diags)
		for name, got := range map[string]types.String{"project": data.Project, "name": data.Name, "region": data.Region, "plan": data.Plan, "engine": data.Engine, "billing": data.Billing} {
			if got.IsNull() || got.IsUnknown() {
				t.Errorf("%s not filled on import", name)
			}
		}
		if got := data.Project.ValueString(); got != "proj_1" {
			t.Errorf("project = %q, want the API's ID", got)
		}
		if !data.Parameters.IsNull() {
			t.Errorf("parameters = %v, want null on import", data.Parameters)
		}
	})

	t.Run("nil billing with unset billing gives null, not unknown", func(t *testing.T) {
		var diags diag.Diagnostics
		data := mdbTestModel()
		data.Billing = types.StringUnknown()
		attrs := mdbTestAttrs()
		attrs.Plan.Billing = nil
		mdbApplyAttributes(ctx, &data, nil, attrs, nil, &diags)
		if !data.Billing.IsNull() {
			t.Errorf("billing = %v, want null", data.Billing)
		}
	})
}

func TestMdbApplyAttributes_AfterApply(t *testing.T) {
	ctx := context.Background()

	t.Run("known planned values win over a normalised API", func(t *testing.T) {
		var diags diag.Diagnostics
		planned := mdbTestModel()
		planned.Status = types.StringUnknown()
		data := planned
		attrs := mdbTestAttrs() // normalised parameter value, extra CIDR, yearly billing
		mdbApplyAttributes(ctx, &data, &planned, attrs, nil, &diags)
		if got := data.Parameters.Elements()["work_mem"].(types.String).ValueString(); got != "64MB" {
			t.Errorf("parameters.work_mem = %q, want the planned value", got)
		}
		if got := len(data.TrustedSources.Elements()); got != 1 {
			t.Errorf("trusted_sources has %d elements, want the planned 1", got)
		}
		if got := data.Billing.ValueString(); got != "monthly" {
			t.Errorf("billing = %q, want the planned value", got)
		}
		if got := data.Name.ValueString(); got != "api" {
			t.Errorf("name = %q, want the planned value", got)
		}
		if got := data.Status.ValueString(); got != "running" {
			t.Errorf("status = %q, want the API's value (unknown in plan)", got)
		}
	})

	t.Run("planned null trusted_sources stays null", func(t *testing.T) {
		var diags diag.Diagnostics
		planned := mdbTestModel()
		planned.TrustedSources = types.SetNull(types.StringType)
		data := planned
		mdbApplyAttributes(ctx, &data, &planned, mdbTestAttrs(), nil, &diags)
		if !data.TrustedSources.IsNull() {
			t.Errorf("trusted_sources = %v, want the planned null", data.TrustedSources)
		}
	})

	t.Run("after create: unknowns come from the GET, credentials fall back to the POST", func(t *testing.T) {
		var diags diag.Diagnostics
		planned := mdbTestModel()
		planned.Status = types.StringUnknown()
		planned.Billing = types.StringUnknown()
		planned.TrustedSources = types.SetUnknown(types.StringType)
		planned.Credentials = types.ObjectUnknown(mdbCredentialsAttrTypes)
		// data was already changed by Create's early State.Set.
		data := planned
		data.Status = types.StringValue("provisioning")
		data.Credentials = types.ObjectNull(mdbCredentialsAttrTypes)
		data.Billing = types.StringNull()
		data.TrustedSources = types.SetNull(types.StringType)

		attrs := mdbTestAttrs()
		attrs.Credentials.Password = nil
		attrs.Credentials.URL = nil
		post := &components.ManagedDatabaseAttributesAttributes{
			Credentials: &components.ManagedDatabaseAttributesCredentials{Password: strPtr("post-secret")},
		}
		mdbApplyAttributes(ctx, &data, &planned, attrs, post, &diags)
		if got := data.Status.ValueString(); got != "running" {
			t.Errorf("status = %q, want the GET's value", got)
		}
		if got := data.Billing.ValueString(); got != "yearly" {
			t.Errorf("billing = %q, want the GET's value", got)
		}
		if got := len(data.TrustedSources.Elements()); got != 2 {
			t.Errorf("trusted_sources has %d elements, want the GET's 2", got)
		}
		if got := credAttr(t, data.Credentials, "host").(types.String).ValueString(); got != "db.api" {
			t.Errorf("host = %q, want the GET's value", got)
		}
		if got := credAttr(t, data.Credentials, "password").(types.String).ValueString(); got != "post-secret" {
			t.Errorf("password = %q, want the POST's value", got)
		}
		if !credAttr(t, data.Credentials, "url").IsNull() {
			t.Errorf("url = %v, want null (in neither response)", credAttr(t, data.Credentials, "url"))
		}
	})

	t.Run("after update: known planned credentials are kept", func(t *testing.T) {
		var diags diag.Diagnostics
		planned := mdbTestModel()
		planned.Status = types.StringUnknown()
		data := planned
		mdbApplyAttributes(ctx, &data, &planned, mdbTestAttrs(), nil, &diags)
		if got := credAttr(t, data.Credentials, "password").(types.String).ValueString(); got != "prior-secret" {
			t.Errorf("password = %q, want the planned (prior) value", got)
		}
	})

	t.Run("no credentials anywhere gives a typed null object", func(t *testing.T) {
		var diags diag.Diagnostics
		planned := mdbTestModel()
		planned.Credentials = types.ObjectUnknown(mdbCredentialsAttrTypes)
		data := planned
		data.Credentials = types.ObjectUnknown(mdbCredentialsAttrTypes)
		attrs := mdbTestAttrs()
		attrs.Credentials = nil
		mdbApplyAttributes(ctx, &data, &planned, attrs, nil, &diags)
		if !data.Credentials.IsNull() {
			t.Errorf("credentials = %v, want null", data.Credentials)
		}
		if diags.HasError() {
			t.Fatal(diags)
		}
	})
}

func TestMdbParametersPayload(t *testing.T) {
	m := types.MapValueMust(types.StringType, map[string]attr.Value{"work_mem": types.StringValue("64MB")})
	got := mdbParametersPayload(m)
	if got["work_mem"] != "64MB" || len(got) != 1 {
		t.Errorf("payload = %v", got)
	}
	if mdbParametersPayload(types.MapNull(types.StringType)) != nil {
		t.Error("null map should give nil")
	}
	if mdbParametersPayload(types.MapValueMust(types.StringType, map[string]attr.Value{})) != nil {
		t.Error("empty map should give nil, so it is left out of the PATCH")
	}
}

func TestTrustedSourcesValidators(t *testing.T) {
	ctx := context.Background()
	elementValidator := setvalidator.ValueStringsAre(canonicalCIDR{})

	set := func(vals ...attr.Value) types.Set {
		return types.SetValueMust(types.StringType, vals)
	}
	cases := []struct {
		name      string
		value     types.Set
		wantError string
	}{
		{"null", types.SetNull(types.StringType), ""},
		{"unknown set", types.SetUnknown(types.StringType), ""},
		{"unknown element", set(types.StringUnknown()), ""},
		{"empty", set(), "cannot be cleared"},
		{"canonical v4", set(types.StringValue("203.0.113.10/32")), ""},
		{"canonical v6", set(types.StringValue("2001:db8::/32")), ""},
		{"bare IP", set(types.StringValue("203.0.113.10")), "is not a CIDR block"},
		{"host bits set", set(types.StringValue("10.0.0.5/24")), "10.0.0.0/24"},
		{"non-canonical v6", set(types.StringValue("2001:DB8:0::/32")), "2001:db8::/32"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validator.SetRequest{Path: path.Root("trusted_sources"), ConfigValue: tc.value}
			resp := &validator.SetResponse{}
			trustedSourcesNotEmpty{}.ValidateSet(ctx, req, resp)
			elementValidator.ValidateSet(ctx, req, resp)
			if tc.wantError == "" {
				if resp.Diagnostics.HasError() {
					t.Fatalf("unexpected error: %v", resp.Diagnostics)
				}
				return
			}
			if !resp.Diagnostics.HasError() {
				t.Fatalf("expected an error containing %q", tc.wantError)
			}
			var all strings.Builder
			for _, d := range resp.Diagnostics.Errors() {
				all.WriteString(d.Summary() + " " + d.Detail())
			}
			if !strings.Contains(all.String(), tc.wantError) {
				t.Errorf("errors %q don't contain %q", all.String(), tc.wantError)
			}
		})
	}

	// ISC-14: the "cannot be cleared" phrase is in the summary, so ExpectError
	// isn't broken by Terraform wrapping the detail.
	resp := &validator.SetResponse{}
	trustedSourcesNotEmpty{}.ValidateSet(ctx, validator.SetRequest{Path: path.Root("trusted_sources"), ConfigValue: set()}, resp)
	if !strings.Contains(resp.Diagnostics.Errors()[0].Summary(), "cannot be cleared") {
		t.Errorf("summary %q should contain the matched phrase", resp.Diagnostics.Errors()[0].Summary())
	}
}

func TestManagedDatabaseErrorDetail(t *testing.T) {
	body := `{"data":{"id":"mdb_1","attributes":{"credentials":{"password":"s3cr3t"}}}}`
	got := managedDatabaseErrorDetail(components.NewAPIError("unknown content-type received: application/json", http.StatusOK, body, nil))
	if strings.Contains(got, "s3cr3t") {
		t.Errorf("detail leaks the body: %q", got)
	}
	if !strings.Contains(got, "HTTP 200") || !strings.Contains(got, "application/json") {
		t.Errorf("detail %q should keep the status and the SDK message", got)
	}
	if got := managedDatabaseErrorDetail(components.NewAPIError("API error occurred", 422, `{"errors":[{"detail":"bad cidr"}]}`, nil)); !strings.Contains(got, "bad cidr") {
		t.Errorf("4xx detail %q should keep the API error message", got)
	}
	if got := managedDatabaseErrorDetail(errors.New("dial tcp: timeout")); got != "dial tcp: timeout" {
		t.Errorf("plain error = %q", got)
	}
}

func TestManagedDatabaseRetryableDuringPoll(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"404", components.NewAPIError("", http.StatusNotFound, "", nil), true},
		{"429", components.NewAPIError("", http.StatusTooManyRequests, "", nil), true},
		{"500", components.NewAPIError("", http.StatusInternalServerError, "", nil), true},
		{"503", components.NewAPIError("", http.StatusServiceUnavailable, "", nil), true},
		{"403", components.NewAPIError("", http.StatusForbidden, "", nil), false},
		{"409", components.NewAPIError("", http.StatusConflict, "", nil), false},
		{"422", components.NewAPIError("", http.StatusUnprocessableEntity, "", nil), false},
		{"plain", errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := managedDatabaseRetryableDuringPoll(tc.err); got != tc.want {
				t.Errorf("retryable = %v, want %v", got, tc.want)
			}
		})
	}
	if !mdbRateLimited(components.NewAPIError("", http.StatusTooManyRequests, "", nil)) {
		t.Error("429 should be rate-limited")
	}
	if mdbRateLimited(components.NewAPIError("", http.StatusServiceUnavailable, "", nil)) {
		t.Error("503 should not be rate-limited")
	}
}

func TestMdbUpdateErrorTextNeverClaimsTaint(t *testing.T) {
	errs := []error{nil, components.NewAPIError("x", 409, "", nil), components.NewAPIError("x", 403, "", nil), components.NewAPIError("x", 422, "", nil)}
	stages := []mdbUpdateStage{mdbUpdatePreWait, mdbUpdatePatchRejected, mdbUpdatePatchFailed, mdbUpdatePostWait}
	kinds := []mdbWaitKind{mdbWaitReady, mdbWaitTimeout, mdbWaitFailedStatus, mdbWaitGone, mdbWaitError}
	for _, stage := range stages {
		for _, kind := range kinds {
			for _, err := range errs {
				summary, detail := mdbUpdateErrorText("mdb_1", stage, kind, "failed", err)
				text := strings.ToLower(summary + " " + detail)
				for _, banned := range []string{"untaint", "is tainted", "marked tainted"} {
					if strings.Contains(text, banned) {
						t.Errorf("stage %v kind %v: text contains %q: %s", stage, kind, banned, text)
					}
				}
			}
		}
	}
	if _, detail := mdbUpdateErrorText("mdb_1", mdbUpdatePatchFailed, mdbWaitError, "", components.NewAPIError("x", 403, "", nil)); !strings.Contains(detail, "permissions") {
		t.Errorf("403 detail %q should point at the token's permissions", detail)
	}
}

func TestMdbCreatePatchErrorClassification(t *testing.T) {
	cases := []struct {
		status  int
		summary string
	}{
		{400, "trusted_sources rejected by the API"},
		{422, "trusted_sources rejected by the API"},
		{409, "Unable to apply trusted_sources"},
		{403, "Unable to apply trusted_sources"},
		{503, "Unable to apply trusted_sources"},
	}
	for _, tc := range cases {
		var diags diag.Diagnostics
		mdbAddCreatePatchError("mdb_1", components.NewAPIError("x", tc.status, "", nil), &diags)
		if got := diags.Errors()[0].Summary(); got != tc.summary {
			t.Errorf("%d: summary %q, want %q", tc.status, got, tc.summary)
		}
		if tc.status == 403 && !strings.Contains(diags.Errors()[0].Detail(), "permissions") {
			t.Errorf("403 detail should mention permissions")
		}
	}
}

func TestMdbModifyPlan(t *testing.T) {
	base := func() ManagedDatabaseResourceModel {
		m := mdbTestModel()
		m.Project = types.StringValue("proj_1")
		return m
	}
	unset := func(m ManagedDatabaseResourceModel) ManagedDatabaseResourceModel {
		m.Project = types.StringNull()
		m.Billing = types.StringNull()
		return m
	}
	summaries := func(d diag.Diagnostics) (errs, warns []string) {
		for _, e := range d.Errors() {
			errs = append(errs, e.Summary())
		}
		for _, w := range d.Warnings() {
			warns = append(warns, w.Summary())
		}
		return
	}

	t.Run("create fills project from the provider default", func(t *testing.T) {
		var d diag.Diagnostics
		cfg, plan := unset(base()), base()
		plan.Project = types.StringUnknown()
		mdbModifyPlan(&cfg, &plan, nil, "proj_default", &d)
		if d.HasError() || plan.Project.ValueString() != "proj_default" {
			t.Fatalf("project = %v, diags %v", plan.Project, d)
		}
	})
	t.Run("missing project", func(t *testing.T) {
		var d diag.Diagnostics
		cfg, plan := unset(base()), base()
		mdbModifyPlan(&cfg, &plan, nil, "", &d)
		if errs, _ := summaries(d); len(errs) != 1 || errs[0] != "Missing project" {
			t.Fatalf("errors = %v", errs)
		}
	})
	t.Run("imported with no project anywhere keeps state", func(t *testing.T) {
		var d diag.Diagnostics
		cfg, plan, state := unset(base()), base(), base()
		mdbModifyPlan(&cfg, &plan, &state, "", &d)
		if d.HasError() || plan.Project.ValueString() != "proj_1" {
			t.Fatalf("project = %v, diags %v", plan.Project, d)
		}
	})
	t.Run("default project changed is an error naming the state value", func(t *testing.T) {
		var d diag.Diagnostics
		cfg, plan, state := unset(base()), base(), base()
		mdbModifyPlan(&cfg, &plan, &state, "proj_2", &d)
		if errs, _ := summaries(d); len(errs) != 1 || errs[0] != "Default project changed" {
			t.Fatalf("errors = %v", errs)
		}
		if !strings.Contains(d.Errors()[0].Detail(), `"proj_1"`) {
			t.Errorf("detail %q should quote the state value", d.Errors()[0].Detail())
		}
	})
	t.Run("billing change is an error", func(t *testing.T) {
		var d diag.Diagnostics
		cfg, plan, state := base(), base(), base()
		plan.Billing = types.StringValue("yearly")
		mdbModifyPlan(&cfg, &plan, &state, "", &d)
		if errs, _ := summaries(d); len(errs) != 1 || errs[0] != "Billing can't be changed" {
			t.Fatalf("errors = %v", errs)
		}
	})
	t.Run("billing change with a replacement is allowed", func(t *testing.T) {
		var d diag.Diagnostics
		cfg, plan, state := base(), base(), base()
		plan.Billing = types.StringValue("yearly")
		plan.Plan = types.StringValue("db.psql.medium")
		mdbModifyPlan(&cfg, &plan, &state, "", &d)
		if d.HasError() || d.WarningsCount() != 0 {
			t.Fatalf("diags = %v", d)
		}
	})
	t.Run("billing adoption warns", func(t *testing.T) {
		var d diag.Diagnostics
		cfg, plan, state := base(), base(), base()
		state.Billing = types.StringNull()
		mdbModifyPlan(&cfg, &plan, &state, "", &d)
		if _, warns := summaries(d); d.HasError() || len(warns) != 1 || warns[0] != "billing only recorded in state" {
			t.Fatalf("diags = %v", d)
		}
	})
	t.Run("removed parameter keys warn", func(t *testing.T) {
		var d diag.Diagnostics
		cfg, plan, state := base(), base(), base()
		state.Parameters = types.MapValueMust(types.StringType, map[string]attr.Value{"work_mem": types.StringValue("64MB"), "max_connections": types.StringValue("100")})
		mdbModifyPlan(&cfg, &plan, &state, "", &d)
		if _, warns := summaries(d); len(warns) != 1 || !strings.Contains(d.Warnings()[0].Detail(), "max_connections") {
			t.Fatalf("diags = %v", d)
		}
	})
	t.Run("no checks on create", func(t *testing.T) {
		var d diag.Diagnostics
		cfg, plan := base(), base()
		plan.Billing = types.StringValue("yearly")
		mdbModifyPlan(&cfg, &plan, nil, "", &d)
		if d.HasError() || d.WarningsCount() != 0 {
			t.Fatalf("diags = %v", d)
		}
	})
	if got := mdbRemovedParameterKeys(types.MapNull(types.StringType), types.MapNull(types.StringType)); got != nil {
		t.Errorf("removed = %v, want nil", got)
	}
}

func TestMdbUpdatePatch(t *testing.T) {
	ctx := context.Background()
	params := func(kv ...string) types.Map {
		m := map[string]attr.Value{}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i]] = types.StringValue(kv[i+1])
		}
		return types.MapValueMust(types.StringType, m)
	}
	cases := []struct {
		name       string
		state      types.Map
		plan       types.Map
		wantSend   bool
		wantParams map[string]any
	}{
		{"removing a key sends nothing", params("a", "1", "b", "2"), params("a", "1"), false, nil},
		{"changing a value sends the full map", params("a", "1", "b", "2"), params("a", "1", "b", "3"), true, map[string]any{"a": "1", "b": "3"}},
		{"adding a key sends the full map", params("a", "1"), params("a", "1", "c", "4"), true, map[string]any{"a": "1", "c": "4"}},
		{"unchanged sends nothing", params("a", "1"), params("a", "1"), false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var diags diag.Diagnostics
			state, plan := mdbTestModel(), mdbTestModel()
			state.Parameters, plan.Parameters = tc.state, tc.plan
			patch, send := mdbUpdatePatch(ctx, &plan, &state, &diags)
			if send != tc.wantSend {
				t.Fatalf("send = %v, want %v", send, tc.wantSend)
			}
			if tc.wantParams != nil && len(patch.Parameters) != len(tc.wantParams) {
				t.Errorf("parameters = %v, want %v", patch.Parameters, tc.wantParams)
			}
			for k, v := range tc.wantParams {
				if patch.Parameters[k] != v {
					t.Errorf("parameters[%s] = %v, want %v", k, patch.Parameters[k], v)
				}
			}
		})
	}
}

// A 404 reaches the "gone" texts with no status; they must not print one.
func TestMdbGoneTextsWithoutStatus(t *testing.T) {
	_, detail := mdbUpdateErrorText("mdb_1", mdbUpdatePreWait, mdbWaitGone, "", nil)
	if strings.Contains(detail, `status ""`) {
		t.Errorf("update pre-wait text prints an empty status: %q", detail)
	}
	var diags diag.Diagnostics
	mdbAddCreateWaitError("mdb_1", mdbWaitResult{Kind: mdbWaitGone}, &diags)
	if d := diags.Errors()[0].Detail(); strings.Contains(d, `status ""`) {
		t.Errorf("create text prints an empty status: %q", d)
	}
	var withStatus diag.Diagnostics
	mdbAddCreateWaitError("mdb_1", mdbWaitResult{Kind: mdbWaitGone, LastStatus: "deleting"}, &withStatus)
	if d := withStatus.Errors()[0].Detail(); !strings.Contains(d, `"deleting"`) {
		t.Errorf("create text should name a known status: %q", d)
	}
}

// A 404 after a polled status must still read as "not found", not as that
// status.
func TestMdbGoneTextsAfterAStatus(t *testing.T) {
	var diags diag.Diagnostics
	mdbAddCreateWaitError("mdb_1", mdbWaitResult{Kind: mdbWaitGone, LastStatus: "updating", NotFound: true}, &diags)
	if d := diags.Errors()[0].Detail(); strings.Contains(d, "updating") || !strings.Contains(d, "no longer finds") {
		t.Errorf("create text blames the earlier status: %q", d)
	}
	var upd diag.Diagnostics
	mdbAddUpdateWaitError(&upd, "mdb_1", mdbUpdatePreWait, mdbWaitResult{Kind: mdbWaitGone, LastStatus: "updating", NotFound: true})
	if d := upd.Errors()[0].Detail(); strings.Contains(d, "updating") || !strings.Contains(d, "no longer finds") {
		t.Errorf("update text blames the earlier status: %q", d)
	}
}
