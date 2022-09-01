package latitude

import (
	"context"
	"time"

	api "github.com/capturealpha/latitude-api-client"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourceSSHKey() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceSSHKeyCreate,
		ReadContext:   resourceSSHKeyRead,
		UpdateContext: resourceSSHKeyUpdate,
		DeleteContext: resourceSSHKeyDelete,
		Schema: map[string]*schema.Schema{
			"project_id": {
				Type:        schema.TypeString,
				Description: "The id of the project",
				Required:    true,
			},
			"name": {
				Type:        schema.TypeString,
				Description: "The SSH key name",
				Required:    true,
			},
			"public_key": {
				Type:        schema.TypeString,
				Description: "The SSH public key",
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

func resourceSSHKeyCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	c := m.(*api.Client)

	createRequest := &api.SSHKeyCreateRequest{
		Data: api.SSHKeyCreateData{
			Type: "ssh_keys",
			Attributes: api.SSHKeyCreateAttributes{
				Name:      d.Get("name").(string),
				PublicKey: d.Get("public_key").(string),
			},
		},
	}

	key, _, err := c.SSHKeys.Create(d.Get("project_id").(string), createRequest)
	if err != nil {
		diag.FromErr(err)
	}

	d.SetId(key.Data.ID)

	resourceSSHKeyRead(ctx, d, m)

	return diags
}

func resourceSSHKeyRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	keyID := d.Id()

	key, _, err := c.SSHKeys.Get(keyID, d.Get("project_id").(string), nil)
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("name", &key.Data.Attributes.Name); err != nil {
		return diag.FromErr(err)
	}

	return diags
}

func resourceSSHKeyUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	keyID := d.Id()

	updateRequest := &api.SSHKeyUpdateRequest{
		Data: api.SSHKeyUpdateData{
			Type: "ssh_keys",
			Attributes: api.SSHKeyUpdateAttributes{
				Name: d.Get("name").(string),
			},
		},
	}

	_, _, err := c.SSHKeys.Update(keyID, d.Get("project_id").(string), updateRequest)
	if err != nil {
		diag.FromErr(err)
	}

	d.Set("updated", time.Now().Format(time.RFC850))

	return resourceProjectRead(ctx, d, m)
}

func resourceSSHKeyDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	keyID := d.Id()

	_, err := c.SSHKeys.Delete(keyID, d.Get("project_id").(string))
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")

	return diags
}
