package latitudesh

import (
	"context"
	"net/http"
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
				ForceNew:    true,
			},
			"site": {
				Type:        schema.TypeString,
				Description: "The server site",
				Required:    true,
				ForceNew:    true,
			},
			"plan": {
				Type:        schema.TypeString,
				Description: "The server plan",
				Required:    true,
				ForceNew:    true,
			},
			"operating_system": {
				Type:        schema.TypeString,
				Description: "The server OS. Update will trigger a reinstall only if allow_reinstall is set to true.",
				Required:    true,
			},
			"hostname": {
				Type:        schema.TypeString,
				Description: "The server hostname",
				Required:    true,
			},
			"ssh_keys": {
				Type: schema.TypeList,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Description: "List of server SSH key ids. Update will trigger a reinstall only if allow_reinstall is set to true.",
				Optional:    true,
			},
			"user_data": {
				Type:        schema.TypeString,
				Description: "The id of user data to set on the server. Update will trigger a reinstall only if allow_reinstall is set to true.",
				Optional:    true,
			},
			"raid": {
				Type:        schema.TypeString,
				Description: "RAID mode for the server. Update will trigger a reinstall only if allow_reinstall is set to true.",
				Optional:    true,
			},
			"ipxe_url": {
				Type:        schema.TypeString,
				Description: "Url for the iPXE script that will be used. Update will trigger a reinstall only if allow_reinstall is set to true.",
				Optional:    true,
			},
			"primary_ipv4": {
				Type:        schema.TypeString,
				Description: "The server IP address",
				Computed:    true,
			},
			"tags": {
				Type: schema.TypeList,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Description: "List of server tags",
				Optional:    true,
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
			"allow_reinstall": {
				Type:        schema.TypeBool,
				Description: "Allow server reinstallation when operating_system, ssh_keys, user_data,raid, or ipxe_url changes.",
				Optional:    true,
				Default:     false,
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

	ssh_keys := parseSSHKeys(d)
	createRequest := &api.ServerCreateRequest{
		Data: api.ServerCreateData{
			Type: "servers",
			Attributes: api.ServerCreateAttributes{
				Project:         d.Get("project").(string),
				Site:            d.Get("site").(string),
				Plan:            d.Get("plan").(string),
				OperatingSystem: d.Get("operating_system").(string),
				Hostname:        d.Get("hostname").(string),
				SSHKeys:         ssh_keys,
				UserData:        d.Get("user_data").(string),
				Raid:            d.Get("raid").(string),
				IpxeUrl:         d.Get("ipxe_url").(string),
			},
		},
	}

	server, _, err := c.Servers.Create(createRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(server.ID)

	if d.Get("tags") != nil {
		resourceServerUpdate(ctx, d, m)
	}

	resourceServerRead(ctx, d, m)

	return diags
}

func resourceServerRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)
	var diags diag.Diagnostics

	serverID := d.Id()

	server, resp, err := c.Servers.Get(serverID, nil)
	if err != nil {
		if resp.StatusCode == http.StatusNotFound {
			d.SetId("")
			return diags
		}

		return diag.FromErr(err)
	}

	// server.Project.ID is an interface{} type so we should verify before using
	var serverProjectId string
	switch server.Project.ID.(type) {
	case string:
		serverProjectId = server.Project.ID.(string)
	case float64:
		serverProjectId = strconv.FormatFloat(server.Project.ID.(float64), 'b', 2, 64)
	}

	if err := d.Set("project", serverProjectId); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("hostname", &server.Hostname); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("primary_ipv4", &server.PrimaryIPv4); err != nil {
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

	if err := d.Set("created", &server.CreatedAt); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("tags", tagIDs(server.Tags)); err != nil {
		return diag.FromErr(err)
	}

	return diags
}

func resourceServerUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)
	var diags diag.Diagnostics

	serverID := d.Id()
	tags := parseTags(d)

	updateRequest := &api.ServerUpdateRequest{
		Data: api.ServerUpdateData{
			Type: "servers",
			ID:   serverID,
			Attributes: api.ServerUpdateAttributes{
				Hostname: d.Get("hostname").(string),
				Tags:     tags,
			},
		},
	}

	_, _, err := c.Servers.Update(serverID, updateRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	// The 'hostname' key is currently the only key that's able to update with the Latitude 'ServerUpdateRequest'.
	// For all other keys that don't set ForceNew: true, we must re-install the server to update them.
	// Resources with ForceNew: true, won't hit this code-path and will instead run delete & create.
	if d.HasChangesExcept("hostname", "tags") {
		if d.Get("allow_reinstall").(bool) {
			err := serverReinstall(c, serverID, ctx, d)
			if err != nil {
				return diag.FromErr(err)
			}
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Warning,
				Summary:  "Your Server is being reinstalled",
				Detail: "[WARN] The changes made to your server resource require a reinstallation. " +
					"Please note that this process may take some time to complete.",
			})
		}
	}

	err = d.Set("updated", time.Now().Format(time.RFC850))
	if err != nil {
		return diag.FromErr(err)
	}

	return append(diags, resourceServerRead(ctx, d, m)...)
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

func serverReinstall(c *api.Client, serverID string, ctx context.Context, d *schema.ResourceData) error {
	ssh_keys := parseSSHKeys(d)

	_, err := c.Servers.Reinstall(serverID, &api.ServerReinstallRequest{
		Data: api.ServerReinstallData{
			Type: "reinstalls",
			Attributes: api.ServerReinstallAttributes{
				OperatingSystem: d.Get("operating_system").(string),
				Hostname:        d.Get("hostname").(string),
				SSHKeys:         ssh_keys,
				UserData:        d.Get("user_data").(string),
				Raid:            d.Get("raid").(string),
				IpxeUrl:         d.Get("ipxe_url").(string),
			},
		},
	})

	if err != nil {
		return err
	}
	return nil
}

func parseSSHKeys(d *schema.ResourceData) []string {
	ssh_keys := d.Get("ssh_keys").([]interface{})
	ssh_keys_slice := make([]string, len(ssh_keys))
	for i, ssh_key := range ssh_keys {
		ssh_keys_slice[i] = ssh_key.(string)
	}
	return ssh_keys_slice
}
