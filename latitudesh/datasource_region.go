package latitude

import (
	"context"

	api "github.com/maxihost/latitudesh-go"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func dataSourceRegion() *schema.Resource {
	return &schema.Resource{
		ReadContext: dataSourceRegionsRead,
		Schema: map[string]*schema.Schema{
			"id": {
				Type:        schema.TypeString,
				Description: "The ID of this Region.",
				Computed:    true,
			},
			"name": {
				Type:        schema.TypeString,
				Description: "The name of the Region",
				Computed:    true,
			},
			"slug": {
				Type:        schema.TypeString,
				Description: "The slug of this Region to match.",
				Required:    true,
			},
			"facility": {
				Type:        schema.TypeString,
				Description: "The facility of this Region.",
				Computed:    true,
			},
			"country_name": {
				Type:        schema.TypeString,
				Description: "The country name of this Region.",
				Computed:    true,
			},
			"country_slug": {
				Type:        schema.TypeString,
				Description: "The country slug of this Region.",
				Computed:    true,
			},
		},
	}
}

func dataSourceRegionsRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	c := m.(*api.Client)
	slug := d.Get("slug").(string)

	regions, _, err := c.Regions.List(new(api.GetOptions).Filter("slug", slug))
	if err != nil {
		return diag.FromErr(err)
	}
	if len(regions) != 1 {
		return diag.Errorf("No regions found for region %s", slug)
	}

	r := regions[0]
	if r.Slug != slug {
		return diag.Errorf("Incorrect region returned: expected %s received %s", slug, r.Slug)
	}

	// Check availability
	/* available := false
	for _, a := range p.Availibility {
		for _, s := range a.Sites {
			if s.InStock {
				available = true
			}
		}
	}
	if !available {
		diag.Errorf("No available stock found for plan %s", name)
	} */

	d.SetId(r.ID)
	regionMap := map[string]interface{}{
		"id":           r.ID,
		"name":         r.Name,
		"slug":         r.Slug,
		"facility":     r.Facility,
		"country_name": r.CountryName,
		"country_slug": r.CountrySlug,
	}
	for key, v := range regionMap {
		err = d.Set(key, v)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	return diags
}
