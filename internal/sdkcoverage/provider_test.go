package sdkcoverage

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// The fakes below implement just enough of the plugin-framework interfaces for
// introspection: Metadata is the only method the shipped-types walk calls.

type fakeResource struct{ suffix string }

func (r fakeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.suffix
}
func (fakeResource) Schema(context.Context, resource.SchemaRequest, *resource.SchemaResponse) {}
func (fakeResource) Create(context.Context, resource.CreateRequest, *resource.CreateResponse) {}
func (fakeResource) Read(context.Context, resource.ReadRequest, *resource.ReadResponse)       {}
func (fakeResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {}
func (fakeResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {}

type fakeDataSource struct{ suffix string }

func (d fakeDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.suffix
}
func (fakeDataSource) Schema(context.Context, datasource.SchemaRequest, *datasource.SchemaResponse) {
}
func (fakeDataSource) Read(context.Context, datasource.ReadRequest, *datasource.ReadResponse) {}

type fakeAction struct{ suffix string }

func (a fakeAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + a.suffix
}
func (fakeAction) Schema(context.Context, action.SchemaRequest, *action.SchemaResponse) {}
func (fakeAction) Invoke(context.Context, action.InvokeRequest, *action.InvokeResponse) {}

// fakeProvider registers ssh_key as BOTH a resource and a data source — the
// shape that motivates the by-kind split, since the merged view collapses them.
type fakeProvider struct{}

func (fakeProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "fake"
}
func (fakeProvider) Schema(context.Context, provider.SchemaRequest, *provider.SchemaResponse) {}
func (fakeProvider) Configure(context.Context, provider.ConfigureRequest, *provider.ConfigureResponse) {
}
func (fakeProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		func() resource.Resource { return fakeResource{"ssh_key"} },
		func() resource.Resource { return fakeResource{"server"} },
	}
}
func (fakeProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		func() datasource.DataSource { return fakeDataSource{"ssh_key"} },
	}
}
func (fakeProvider) Actions(context.Context) []func() action.Action {
	return []func() action.Action{
		func() action.Action { return fakeAction{"server_reinstall"} },
	}
}

// fakeBareProvider registers no data sources and does not implement
// ProviderWithActions at all.
type fakeBareProvider struct{}

func (fakeBareProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "fake"
}
func (fakeBareProvider) Schema(context.Context, provider.SchemaRequest, *provider.SchemaResponse) {}
func (fakeBareProvider) Configure(context.Context, provider.ConfigureRequest, *provider.ConfigureResponse) {
}
func (fakeBareProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		func() resource.Resource { return fakeResource{"server"} },
	}
}
func (fakeBareProvider) DataSources(context.Context) []func() datasource.DataSource {
	return nil
}

func TestShippedByKindSplitsRegistrationsByKind(t *testing.T) {
	got := ShippedByKind(context.Background(), fakeProvider{}, "fake")

	want := ShippedTypes{
		Resources:   []string{"fake_server", "fake_ssh_key"},
		DataSources: []string{"fake_ssh_key"},
		Actions:     []string{"fake_server_reinstall"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ShippedByKind = %+v, want %+v", got, want)
	}
}

// A kind nothing registers must come back as an empty non-nil slice, so the
// JSON the gate consumes renders [] rather than null.
func TestShippedByKindEmptyKindsAreNotNil(t *testing.T) {
	got := ShippedByKind(context.Background(), fakeBareProvider{}, "fake")

	if got.DataSources == nil || len(got.DataSources) != 0 {
		t.Errorf("DataSources = %#v, want empty non-nil slice", got.DataSources)
	}
	if got.Actions == nil || len(got.Actions) != 0 {
		t.Errorf("Actions = %#v, want empty non-nil slice", got.Actions)
	}
	if want := []string{"fake_server"}; !reflect.DeepEqual(got.Resources, want) {
		t.Errorf("Resources = %v, want %v", got.Resources, want)
	}
}

// The merged view stays what reconciliation expects: one fact per type name,
// however many kinds serve it.
func TestShippedTypeNamesMergesAndDedupes(t *testing.T) {
	got := ShippedTypeNames(context.Background(), fakeProvider{}, "fake")

	want := []string{"fake_server", "fake_server_reinstall", "fake_ssh_key"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ShippedTypeNames = %v, want %v", got, want)
	}
}
