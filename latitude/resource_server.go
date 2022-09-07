package latitude

import (
	"context"
	"time"

	api "github.com/capturealpha/latitude-api-client"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourceServer() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceServerCreate,
		ReadContext:   resourceServerRead,
		UpdateContext: resourceServerUpdate,
		DeleteContext: resourceServerDelete,
		Schema: map[string]*schema.Schema{
			"project_id": {
				Type:        schema.TypeString,
				Description: "The id of the project",
				Required:    true,
			},
			"site": {
				Type:        schema.TypeString,
				Description: "The server site",
				Required:    true,
			},
			"plan": {
				Type:        schema.TypeString,
				Description: "The server plan",
				Required:    true,
			},
			"operating_system": {
				Type:        schema.TypeString,
				Description: "The server OS",
				Required:    true,
			},
			"hostname": {
				Type:        schema.TypeString,
				Description: "The server hostname",
				Required:    true,
			},
			"created": {
				Type:        schema.TypeString,
				Description: "The timestamp for when the project was created",
				Computed:    true,
			},
			"updated": {
				Type:        schema.TypeString,
				Description: "The timestamp for the last time the project was updated",
				Computed:    true,
			},
		},
	}
}

func resourceServerCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	c := m.(*api.Client)

	createRequest := &api.ServerCreateRequest{
		Data: api.ServerCreateData{
			Type: "servers",
			Attributes: api.ServerCreateAttributes{
				Project:         d.Get("project_id").(string),
				Site:            d.Get("site").(string),
				Plan:            d.Get("plan").(string),
				OperatingSystem: d.Get("operating_system").(string),
				Hostname:        d.Get("hostname").(string),
			},
		},
	}

	server, _, err := c.Servers.Create(createRequest)
	if err != nil {
		diag.FromErr(err)
	}

	d.SetId(server.ID)

	resourceServerRead(ctx, d, m)

	return diags
}

func resourceServerRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	serverID := d.Id()

	server, _, err := c.Servers.Get(serverID, nil)
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("hostname", &server.Hostname); err != nil {
		return diag.FromErr(err)
	}

	return diags
}

func resourceServerUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	serverID := d.Id()

	updateRequest := &api.ServerUpdateRequest{
		Data: api.ServerUpdateData{
			Type: "servers",
			Attributes: api.ServerCreateAttributes{
				Hostname: d.Get("hostname").(string),
			},
		},
	}

	_, _, err := c.Servers.Update(serverID, updateRequest)
	if err != nil {
		diag.FromErr(err)
	}

	d.Set("updated", time.Now().Format(time.RFC850))

	return resourceProjectRead(ctx, d, m)
}

func resourceServerDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	serverID := d.Id()

	_, err := c.Servers.Delete(serverID, true)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")

	return diags
}
