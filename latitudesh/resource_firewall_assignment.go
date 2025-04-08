package latitudesh

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	latitude "github.com/latitudesh/latitudesh-go"
)

func resourceFirewallAssignment() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceFirewallAssignmentCreate,
		ReadContext:   resourceFirewallAssignmentRead,
		DeleteContext: resourceFirewallAssignmentDelete,
		Schema: map[string]*schema.Schema{
			"firewall_id": {
				Type:        schema.TypeString,
				Description: "The ID of the firewall",
				Required:    true,
				ForceNew:    true,
			},
			"server_id": {
				Type:        schema.TypeString,
				Description: "The ID of the server to assign the firewall to",
				Required:    true,
				ForceNew:    true,
			},
		},
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},
	}
}

func resourceFirewallAssignmentCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	c := m.(*latitude.Client)

	firewallID := d.Get("firewall_id").(string)
	serverID := d.Get("server_id").(string)

	createRequest := &latitude.FirewallAssignmentCreateRequest{
		Data: latitude.FirewallAssignmentCreateData{
			Type: "firewall_server",
			Attributes: latitude.FirewallAssignmentCreateAttributes{
				Server: serverID,
			},
		},
	}

	assignment, _, err := c.Firewalls.CreateAssignment(firewallID, createRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(assignment.ID)

	return diags
}

func resourceFirewallAssignmentRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*latitude.Client)
	var diags diag.Diagnostics

	firewallID := d.Get("firewall_id").(string)
	assignmentID := d.Id()

	assignments, resp, err := c.Firewalls.ListAssignments(firewallID, nil)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			d.SetId("")
			return diags
		}
		return diag.FromErr(err)
	}

	var found bool
	for _, assignment := range assignments {
		if assignment.ID == assignmentID {
			found = true
			if err := d.Set("server_id", assignment.Server.ID); err != nil {
				return diag.FromErr(err)
			}
			break
		}
	}

	if !found {
		d.SetId("")
	}

	return diags
}

func resourceFirewallAssignmentDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*latitude.Client)
	var diags diag.Diagnostics

	firewallID := d.Get("firewall_id").(string)
	assignmentID := d.Id()

	_, err := c.Firewalls.DeleteAssignment(firewallID, assignmentID)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")

	return diags
}
