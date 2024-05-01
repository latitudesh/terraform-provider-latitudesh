package latitudesh

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	api "github.com/latitudesh/latitudesh-go"
)

func dataSourceRole() *schema.Resource {
	return &schema.Resource{
		ReadContext: dataSourceRegionsRead,
		Schema: map[string]*schema.Schema{
			"id": {
				Type:        schema.TypeString,
				Description: "The ID of the Role.",
				Computed:    true,
			},
			"name": {
				Type:        schema.TypeString,
				Description: "The name of the Role",
				Computed:    true,
			},
		},
	}
}

func dataSourceRoleRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	c := m.(*api.Client)
	name := d.Get("name").(string)

	roles, _, err := c.Roles.List(new(api.GetOptions).Filter("name", name))
	if err != nil {
		return diag.FromErr(err)
	}
	if len(roles) != 1 {
		return diag.Errorf("No roles found for name %s", name)
	}

	r := roles[0]
	if r.Name != name {
		return diag.Errorf("Incorrect role returned: expected %s received %s", name, r.Name)
	}

	d.SetId(r.ID)
	regionMap := map[string]interface{}{
		"id":   r.ID,
		"name": r.Name,
	}
	for key, v := range regionMap {
		err = d.Set(key, v)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	return diags
}
