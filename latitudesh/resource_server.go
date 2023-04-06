package latitudesh

import (
	"context"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	api "github.com/latitudesh/latitudesh-go"
)

func resourceServer() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceServerCreate,
		ReadContext:   resourceServerRead,
		UpdateContext: resourceServerUpdate,
		DeleteContext: resourceServerDelete,
		Schema: map[string]*schema.Schema{
			"project": {
				Type:        schema.TypeString,
				Description: "The id or slug of the project",
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
			"ssh_keys": {
				Type:        schema.TypeList,
				Description: "List of server SSH key ids",
				Optional:    true,
				Elem: &schema.Schema{
					Type: schema.TypeInt,
				},
			},
			"user_data": {
				Type:        schema.TypeInt,
				Description: "The id of user data to set on the server",
				Optional:    true,
			},
			"ipxe_url": {
				Type:        schema.TypeString,
				Description: "Url for the iPXE script that will be used",
				Optional:    true,
			},
			"primary_ip_v4": {
				Type:        schema.TypeString,
				Description: "The server IP address",
				Computed:    true,
			},
			"created": {
				Type:        schema.TypeString,
				Description: "The timestamp for when the server was created",
				Computed:    true,
			},
			"updated": {
				Type:        schema.TypeString,
				Description: "The timestamp for the last time the server was updated",
				Computed:    true,
			},
		},
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},
	}
}

func resourceServerCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	c := m.(*api.Client)

	// Convert ssh_keys from []interace{} to []int
	// TODO: Is there a better way to do this?
	ssh_keys := d.Get("ssh_keys").([]interface{})
	ssh_keys_slice := make([]int, len(ssh_keys))
	for i, ssh_key := range ssh_keys {
		ssh_keys_slice[i] = ssh_key.(int)
	}

	createRequest := &api.ServerCreateRequest{
		Data: api.ServerCreateData{
			Type: "servers",
			Attributes: api.ServerCreateAttributes{
				Project:         d.Get("project").(string),
				Site:            d.Get("site").(string),
				Plan:            d.Get("plan").(string),
				OperatingSystem: d.Get("operating_system").(string),
				Hostname:        d.Get("hostname").(string),
				SSHKeys:         ssh_keys_slice,
				UserData:        d.Get("user_data").(int),
				IpxeUrl:         d.Get("ipxe_url").(string),
			},
		},
	}

	server, _, err := c.Servers.Create(createRequest)
	if err != nil {
		return diag.FromErr(err)
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

	if err := d.Set("project", strconv.FormatInt(server.Project.ID, 10)); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("hostname", &server.Hostname); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("primary_ip_v4", &server.PrimaryIPv4); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("operating_system", &server.OperatingSystem.Slug); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("site", &server.Region.Site.Slug); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("plan", &server.Plan.Slug); err != nil {
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
			ID:   serverID,
			Attributes: api.ServerCreateAttributes{
				Hostname: d.Get("hostname").(string),
			},
		},
	}

	_, _, err := c.Servers.Update(serverID, updateRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	d.Set("updated", time.Now().Format(time.RFC850))

	return resourceServerRead(ctx, d, m)
}

func resourceServerDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	serverID := d.Id()

	_, err := c.Servers.Delete(serverID)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")

	return diags
}
