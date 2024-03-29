package latitudesh

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	api "github.com/latitudesh/latitudesh-go"
)

func dataSourcePlan() *schema.Resource {
	return &schema.Resource{
		ReadContext: dataSourcePlansRead,
		Schema: map[string]*schema.Schema{
			"id": {
				Type:        schema.TypeString,
				Description: "The ID of this Plan.",
				Computed:    true,
			},
			"name": {
				Type:        schema.TypeString,
				Description: "The name of the Plan to match",
				Required:    true,
			},
			"slug": {
				Type:        schema.TypeString,
				Description: "The slug of this Plan.",
				Computed:    true,
			},
			"in_stock": {
				Type: schema.TypeList,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Description: "List of the sites where this Plan is in stock.",
				Computed:    true,
			},
		},
	}
}

func dataSourcePlansRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	c := m.(*api.Client)
	name := d.Get("name").(string)

	plans, _, err := c.Plans.List(new(api.GetOptions).Filter("name", name))
	if err != nil {
		return diag.FromErr(err)
	}
	if len(plans) != 1 {
		return diag.Errorf("No plans found for plan %s", name)
	}

	p := plans[0]
	if p.Name != name {
		return diag.Errorf("Incorrect plan returned: expected %s received %s", name, p.Name)
	}

	// Check availability
	if len(p.InStock) == 0 {
		return diag.Errorf("No available stock found for plan %s", name)
	}

	d.SetId(p.ID)

	planMap := map[string]interface{}{
		"id":       p.ID,
		"name":     p.Name,
		"slug":     p.Slug,
		"in_stock": p.InStock,
	}
	for key, v := range planMap {
		err = d.Set(key, v)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	return diags
}
