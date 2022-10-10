package latitudesh

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	api "github.com/latitudesh/latitudesh-go"
)

func Provider() *schema.Provider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"auth_token": {
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("LATITUDESH_AUTH_TOKEN", nil),
			},
		},
		ResourcesMap: map[string]*schema.Resource{
			"latitudesh_project": resourceProject(),
			"latitudesh_server":  resourceServer(),
			"latitudesh_ssh_key": resourceSSHKey(),
		},
		DataSourcesMap: map[string]*schema.Resource{
			"latitudesh_plan":   dataSourcePlan(),
			"latitudesh_region": dataSourceRegion(),
		},
		ConfigureContextFunc: providerConfigure,
	}
}

func providerConfigure(ctx context.Context, d *schema.ResourceData) (interface{}, diag.Diagnostics) {
	authToken := d.Get("auth_token").(string)

	var diags diag.Diagnostics

	if authToken != "" {
		c := api.NewClientWithAuth("latitudesh", authToken, nil)

		return c, diags
	}

	return nil, diags
}
