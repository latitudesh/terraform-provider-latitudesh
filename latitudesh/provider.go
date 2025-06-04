package latitudesh

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

const (
	userAgentForProvider = "Latitude-Terraform-Provider"
)

var currentVersion = "2.0.0"

// Ensure latitudeshProvider satisfies various provider interfaces
var _ provider.Provider = &latitudeshProvider{}

// latitudeshProvider defines the provider implementation.
type latitudeshProvider struct {
	version string
}

// latitudeshProviderModel describes the provider data model.
type latitudeshProviderModel struct {
	AuthToken types.String `tfsdk:"auth_token"`
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
				MarkdownDescription: "Latitude.sh API authentication token",
				Optional:            true,
				Sensitive:           true,
			},
		},
	}
}

func (p *latitudeshProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data latitudeshProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Configuration values are now available.
	// Example client configuration for data sources and resources.
	authToken := data.AuthToken.ValueString()

	if authToken != "" {
		sdkClient := latitudeshgosdk.New(
			latitudeshgosdk.WithSecurity(authToken),
		)
		resp.DataSourceData = sdkClient
		resp.ResourceData = sdkClient
	} else {
		sdkClient := latitudeshgosdk.New(
			latitudeshgosdk.WithSecurity(""),
		)
		resp.DataSourceData = sdkClient
		resp.ResourceData = sdkClient
	}
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
