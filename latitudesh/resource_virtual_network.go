package latitudesh

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	api "github.com/latitudesh/latitudesh-go"
)

func resourceVirtualNetwork() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceVirtualNetworkCreate,
		ReadContext:   resourceVirtualNetworkRead,
		UpdateContext: resourceVirtualNetworkUpdate,
		DeleteContext: resourceVirtualNetworkDelete,
		Schema: map[string]*schema.Schema{
			"project": {
				Type:        schema.TypeString,
				Description: "The project id or slug",
				Required:    true,
			},
			"site": {
				Type:        schema.TypeString,
				Description: "The site id or slug",
				Required:    true,
			},
			"description": {
				Type:        schema.TypeString,
				Description: "The virtual network description",
				Required:    true,
			},
			"vid": {
				Type:        schema.TypeInt,
				Description: "The vlan ID of the virtual network",
				Computed:    true,
			},
			"assignments_count": {
				Type:        schema.TypeInt,
				Description: "Amount of devices assigned to the virtual network",
				Computed:    true,
			},
		},
		Importer: &schema.ResourceImporter{
			StateContext: NestedResourceRestAPIImport,
		},
	}
}

func resourceVirtualNetworkCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	c := m.(*api.Client)

	createRequest := &api.VirtualNetworkCreateRequest{
		Data: api.VirtualNetworkCreateData{
			Type: "virtual_network",
			Attributes: api.VirtualNetworkCreateAttributes{
				Description: d.Get("description").(string),
				Site:        d.Get("site").(string),
				Project:     d.Get("project").(string),
			},
		},
	}

	virtualNetwork, _, err := c.VirtualNetworks.Create(createRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(virtualNetwork.ID)

	resourceVirtualNetworkRead(ctx, d, m)

	return diags
}

func resourceVirtualNetworkRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	virtualNetworkID := d.Id()

	virtualNetwork, _, err := c.VirtualNetworks.Get(virtualNetworkID, nil)
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("vid", &virtualNetwork.Vid); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("description", &virtualNetwork.Description); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("site", &virtualNetwork.SiteSlug); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("assignments_count", &virtualNetwork.AssignmentsCount); err != nil {
		return diag.FromErr(err)
	}

	return diags
}

func resourceVirtualNetworkUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	virtualNetworkID := d.Id()

	updateRequest := &api.VirtualNetworkUpdateRequest{
		Data: api.VirtualNetworkUpdateData{
			Type: "virtual_networks",
			ID:   virtualNetworkID,
			Attributes: api.VirtualNetworkUpdateAttributes{
				Description: d.Get("description").(string),
			},
		},
	}

	_, _, err := c.VirtualNetworks.Update(virtualNetworkID, updateRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	return resourceVirtualNetworkRead(ctx, d, m)
}

func resourceVirtualNetworkDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	virtualNetworkID := d.Id()

	_, err := c.VirtualNetworks.Delete(virtualNetworkID)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")

	return diags
}
