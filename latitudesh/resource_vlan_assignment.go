package latitudesh

import (
	"context"
	"net/http"

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
				Type:        schema.TypeString,
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
				Type:        schema.TypeString,
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

	serverID := d.Get("server_id").(string)
	virtualNetworkID := d.Get("virtual_network_id").(string)

	existingAssignment, found, err := findExistingVlanAssignment(c, serverID, virtualNetworkID)
	if err != nil {
		return diag.FromErr(err)
	}

	if found {
		d.SetId(existingAssignment.ID)

		if err := d.Set("vid", &existingAssignment.Vid); err != nil {
			return diag.FromErr(err)
		}

		if err := d.Set("description", &existingAssignment.Description); err != nil {
			return diag.FromErr(err)
		}

		if err := d.Set("status", &existingAssignment.Status); err != nil {
			return diag.FromErr(err)
		}

		if err := d.Set("server_hostname", &existingAssignment.ServerHostname); err != nil {
			return diag.FromErr(err)
		}

		if err := d.Set("server_label", &existingAssignment.ServerLabel); err != nil {
			return diag.FromErr(err)
		}

		return diags
	}

	assignRequest := &api.VlanAssignRequest{
		Data: api.VlanAssignData{
			Type: "virtual_network_assignment",
			Attributes: api.VlanAssignAttributes{
				ServerID:         serverID,
				VirtualNetworkID: virtualNetworkID,
			},
		},
	}

	vlanAssignment, _, err := c.VlanAssignments.Assign(assignRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(vlanAssignment.ID)

	if err := d.Set("vid", &vlanAssignment.Vid); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("description", &vlanAssignment.Description); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("status", &vlanAssignment.Status); err != nil {
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

func findExistingVlanAssignment(c *api.Client, serverID, virtualNetworkID string) (*api.VlanAssignment, bool, error) {
	assignments, _, err := c.VlanAssignments.List(nil)
	if err != nil {
		return nil, false, err
	}

	for _, assignment := range assignments {
		if assignment.ServerID == serverID && assignment.VirtualNetworkID == virtualNetworkID {
			return &assignment, true, nil
		}
	}

	return nil, false, nil
}

func resourceVlanAssignmentRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	vlanAssignmentID := d.Id()

	vlanAssignment, resp, err := c.VlanAssignments.Get(vlanAssignmentID)
	if err != nil {
		if resp.StatusCode == http.StatusNotFound {
			d.SetId("")
			return diags
		}

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

	if err := d.Set("description", &vlanAssignment.Description); err != nil {
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
