package latitudesh

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	api "github.com/latitudesh/latitudesh-go"
)

func resourceSSHKey() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceSSHKeyCreate,
		ReadContext:   resourceSSHKeyRead,
		UpdateContext: resourceSSHKeyUpdate,
		DeleteContext: resourceSSHKeyDelete,
		Schema: map[string]*schema.Schema{
			"project": {
				Type:        schema.TypeString,
				Description: "The id or slug of the project",
				Required:    true,
				ForceNew:    true,
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
				ForceNew:    true,
			},
			"created": {
				Type:        schema.TypeString,
				Description: "The timestamp for when the SSH key was created",
				Computed:    true,
			},
			"updated": {
				Type:        schema.TypeString,
				Description: "The timestamp for the last time the SSH key was updated",
				Computed:    true,
			},
		},
		Importer: &schema.ResourceImporter{
			StateContext: NestedResourceRestAPIImport,
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

	key, _, err := c.SSHKeys.Create(d.Get("project").(string), createRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(key.ID)

	resourceSSHKeyRead(ctx, d, m)

	return diags
}

func resourceSSHKeyRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	keyID := d.Id()

	key, _, err := c.SSHKeys.Get(keyID, d.Get("project").(string), nil)
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("name", &key.Name); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("public_key", &key.PublicKey); err != nil {
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
			ID:   keyID,
			Attributes: api.SSHKeyUpdateAttributes{
				Name: d.Get("name").(string),
			},
		},
	}

	_, _, err := c.SSHKeys.Update(keyID, d.Get("project").(string), updateRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	d.Set("updated", time.Now().Format(time.RFC850))

	return resourceSSHKeyRead(ctx, d, m)
}

func resourceSSHKeyDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	keyID := d.Id()

	_, err := c.SSHKeys.Delete(keyID, d.Get("project").(string))
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")

	return diags
}
