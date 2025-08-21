package latitudesh

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/internal/provider"
)

// Ensure latitudeshProvider satisfies various provider interfaces
var _ provider.Provider = &latitudeshProvider{}

// latitudeshProvider defines the provider implementation.
type latitudeshProvider struct {
	version string
}

// latitudeshProviderModel describes the provider data model.
type latitudeshProviderModel struct {
	AuthToken types.String `tfsdk:"auth_token"`
	Project   types.String `tfsdk:"project"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &latitudeshProvider{
			version: version,
		}
	}
}

func (p *latitudeshProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "latitudesh"
	resp.Version = p.version
}

func (p *latitudeshProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"auth_token": schema.StringAttribute{
				MarkdownDescription: "Latitude.sh API authentication token. Can also be set via the LATITUDESH_AUTH_TOKEN environment variable.",
				Optional:            true,
				Sensitive:           true,
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "The project ID to use for all resources. If not set, project must be defined in the resource.",
				Optional:            true,
			},
		},
	}
}

func getAuthToken(data latitudeshProviderModel) (string, bool) {
	authToken := data.AuthToken.ValueString()

	if authToken != "" {
		return authToken, true
	}

	authToken = os.Getenv("LATITUDESH_AUTH_TOKEN")
	if authToken != "" {
		return authToken, true
	}

	return "", false
}

func (p *latitudeshProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data latitudeshProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	authToken, found := getAuthToken(data)

	if !found {
		resp.Diagnostics.AddError(
			"Missing Auth Token",
			"Either 'auth_token' must be set in the provider configuration or LATITUDESH_AUTH_TOKEN environment variable must be set.",
		)
		return
	}

	sdkClient := latitudeshgosdk.New(
		latitudeshgosdk.WithSecurity(authToken),
	)
	project := data.Project.ValueString()

	providerContext := &iprovider.ProviderContext{
		Client:  sdkClient,
		Project: project,
	}

	resp.ResourceData = providerContext
	resp.DataSourceData = providerContext
}

func (p *latitudeshProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewProjectResource,
		NewServerResource,
		NewSSHKeyResource,
		NewUserDataResource,
		NewVirtualNetworkResource,
		NewVlanAssignmentResource,
		NewTagResource,
		NewMemberResource,
		NewFirewallResource,
		NewFirewallAssignmentResource,
	}
}

func (p *latitudeshProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewPlanDataSource,
		NewRegionDataSource,
		NewRoleDataSource,
	}
}
