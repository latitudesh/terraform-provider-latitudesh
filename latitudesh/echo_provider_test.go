package latitudesh

// echoTestProvider mirrors HashiCorp's test-only echo provider: it accepts a
// value (typically an ephemeral one) in its provider configuration and copies
// it into resource state, where the test harness can assert it. The upstream
// echoprovider package cannot be used here: it ships with the
// terraform-plugin-testing harness, and that harness and the sdk/v2 one used
// by this package both register a -sweep flag, so they cannot share a test
// binary — and the sdk/v2 harness additionally requires an `id` attribute the
// upstream echo resource lacks.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	providerschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

func echoTestProviderFactory() func() (tfprotov6.ProviderServer, error) {
	return providerserver.NewProtocol6WithError(&echoTestProvider{})
}

type echoTestProvider struct{}

var _ provider.Provider = &echoTestProvider{}

func (p *echoTestProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "echo"
}

func (p *echoTestProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = providerschema.Schema{
		Attributes: map[string]providerschema.Attribute{
			"data": providerschema.DynamicAttribute{Optional: true},
		},
	}
}

func (p *echoTestProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config struct {
		Data types.Dynamic `tfsdk:"data"`
	}
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.ResourceData = config.Data
}

func (p *echoTestProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		func() resource.Resource { return &echoTestResource{} },
	}
}

func (p *echoTestProvider) DataSources(context.Context) []func() datasource.DataSource {
	return nil
}

type echoTestResource struct {
	data types.Dynamic
}

type echoTestResourceModel struct {
	ID   types.String  `tfsdk:"id"`
	Data types.Dynamic `tfsdk:"data"`
}

func (r *echoTestResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName
}

func (r *echoTestResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		Attributes: map[string]resourceschema.Attribute{
			"id":   resourceschema.StringAttribute{Computed: true},
			"data": resourceschema.DynamicAttribute{Computed: true},
		},
	}
}

func (r *echoTestResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	if data, ok := req.ProviderData.(types.Dynamic); ok {
		r.data = data
	}
}

func (r *echoTestResource) Create(ctx context.Context, _ resource.CreateRequest, resp *resource.CreateResponse) {
	model := echoTestResourceModel{
		ID:   types.StringValue("echo"),
		Data: r.data,
	}
	if model.Data.IsUnderlyingValueNull() || model.Data.UnderlyingValue() == nil {
		model.Data = types.DynamicNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *echoTestResource) Read(_ context.Context, _ resource.ReadRequest, _ *resource.ReadResponse) {
}

func (r *echoTestResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var model echoTestResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *echoTestResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}
