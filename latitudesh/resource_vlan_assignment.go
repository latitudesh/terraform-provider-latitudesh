package latitudesh

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	api "github.com/latitudesh/latitudesh-go"
)

func resourceVlanAssignment() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceVlanAssignmentCreate,
		ReadContext:   resourceVlanAssignmentRead,
		DeleteContext: resourceVlanAssignmentDelete,
		Schema: map[string]*schema.Schema{
			"virtual_network_id": {
				Type:        schema.TypeInt,
				Description: "The virtual network ID",
				Required:    true,
				ForceNew:    true,
			},
			"vid": {
				Type:        schema.TypeInt,
				Description: "The vlan ID of the virtual network",
				Computed:    true,
			},
			"description": {
				Type:        schema.TypeString,
				Description: "The Virtual Network description",
				Computed:    true,
			},
			"status": {
				Type:        schema.TypeString,
				Description: "The assignment status",
				Computed:    true,
			},
			"server_id": {
				Type:        schema.TypeInt,
				Description: "The assignment server ID",
				Required:    true,
				ForceNew:    true,
			},
			"server_hostname": {
				Type:        schema.TypeString,
				Description: "The assignment server hostname",
				Computed:    true,
			},
			"server_label": {
				Type:        schema.TypeString,
				Description: "The assignment server label",
				Computed:    true,
			},
		},
		Importer: &schema.ResourceImporter{
			StateContext: NestedResourceRestAPIImport,
		},
	}
}

func resourceVlanAssignmentCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	c := m.(*api.Client)

	assignRequest := &api.VlanAssignRequest{
		Data: api.VlanAssignData{
			Type: "virtual_network_assignment",
			Attributes: api.VlanAssignAttributes{
				ServerID:         d.Get("server_id").(string),
				VirtualNetworkID: d.Get("virtual_network_id").(string),
			},
		},
	}

	vlanAssignment, _, err := c.VlanAssignments.Assign(assignRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(vlanAssignment.ID)

	resourceVlanAssignmentRead(ctx, d, m)

	return diags
}

func resourceVlanAssignmentRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	vlanAssignmentID := d.Id()

	vlanAssignment, _, err := c.VlanAssignments.Get(vlanAssignmentID)
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("virtual_network_id", &vlanAssignment.VirtualNetworkID); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("vid", &vlanAssignment.Vid); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("status", &vlanAssignment.Status); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("server_id", &vlanAssignment.ServerID); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("server_hostname", &vlanAssignment.ServerHostname); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("server_label", &vlanAssignment.ServerLabel); err != nil {
		return diag.FromErr(err)
	}

	return diags
}

func resourceVlanAssignmentDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	VlanAssignmentID := d.Id()

	_, err := c.VlanAssignments.Delete(VlanAssignmentID)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")

	return diags
}
