package latitudesh

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/retry"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

// Ensure latitudeshProvider satisfies various provider interfaces
var _ provider.Provider = &latitudeshProvider{}

// latitudeshProvider defines the provider implementation.
type latitudeshProvider struct {
	version    string
	httpClient *http.Client // optional: used in tests for VCR recording/playback
}

// userAgentTransport overrides the User-Agent header on every request so the
// API can distinguish Terraform traffic from direct Go SDK usage. The SDK's
// own User-Agent (set before the transport runs) is preserved as a suffix.
type userAgentTransport struct {
	base      http.RoundTripper
	userAgent string
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if sdkUA := req.Header.Get("User-Agent"); sdkUA != "" {
		req.Header.Set("User-Agent", t.userAgent+" "+sdkUA)
	} else {
		req.Header.Set("User-Agent", t.userAgent)
	}
	return t.base.RoundTrip(req)
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

	userAgent := fmt.Sprintf("terraform-provider-latitudesh/%s", p.version)
	if req.TerraformVersion != "" {
		userAgent += fmt.Sprintf(" Terraform/%s", req.TerraformVersion)
	}

	baseClient := p.httpClient
	if baseClient == nil {
		// match the SDK's default client behavior
		baseClient = &http.Client{Timeout: 60 * time.Second}
	}
	baseTransport := baseClient.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	httpClient := &http.Client{
		Transport:     &userAgentTransport{base: baseTransport, userAgent: userAgent},
		Timeout:       baseClient.Timeout,
		CheckRedirect: baseClient.CheckRedirect,
		Jar:           baseClient.Jar,
	}

	sdkOpts := []latitudeshgosdk.SDKOption{
		latitudeshgosdk.WithSecurity(authToken),
		latitudeshgosdk.WithClient(httpClient),
		latitudeshgosdk.WithRetryConfig(retry.Config{
			Strategy: "backoff",
			Backoff: &retry.BackoffStrategy{
				InitialInterval: 500,
				MaxInterval:     60000,
				Exponent:        1.5,
				MaxElapsedTime:  300000,
			},
			RetryConnectionErrors: false,
		}),
	}
	sdkClient := latitudeshgosdk.New(sdkOpts...)
	project := data.Project.ValueString()

	providerContext := &iprovider.ProviderContext{
		Client:            sdkClient,
		Project:           project,
		UserDataHashCache: &sync.Map{},
	}

	resp.ResourceData = providerContext
	resp.DataSourceData = providerContext
}

func (p *latitudeshProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewProjectResource,
		NewServerResource,
		NewVirtualMachineResource,
		NewElasticIPResource,
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
		NewSSHKeyDataSource,
		NewTagDataSource,
	}
}
