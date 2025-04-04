package latitudesh

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	latitude "github.com/latitudesh/latitudesh-go"
)

func resourceFirewall() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceFirewallCreate,
		ReadContext:   resourceFirewallRead,
		UpdateContext: resourceFirewallUpdate,
		DeleteContext: resourceFirewallDelete,
		Schema: map[string]*schema.Schema{
			"name": {
				Type:        schema.TypeString,
				Description: "The name of the firewall",
				Required:    true,
			},
			"project": {
				Type:        schema.TypeString,
				Description: "The id or slug of the project",
				Required:    true,
				ForceNew:    true,
			},
			"rules": {
				Type:        schema.TypeSet,
				Description: "The firewall rules",
				Optional:    true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"from": {
							Type:        schema.TypeString,
							Description: "The source of the rule",
							Required:    true,
						},
						"to": {
							Type:        schema.TypeString,
							Description: "The destination of the rule",
							Required:    true,
						},
						"port": {
							Type:        schema.TypeString,
							Description: "The port or port range for the rule",
							Required:    true,
						},
						"protocol": {
							Type:        schema.TypeString,
							Description: "The protocol for the rule (e.g., tcp, udp, icmp)",
							Required:    true,
						},
						"default": {
							Type:        schema.TypeBool,
							Description: "Whether this is a default rule",
							Computed:    true,
						},
					},
				},
			},
		},
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},
	}
}

func resourceFirewallCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	c := m.(*latitude.Client)

	rules := parseFirewallRules(d)
	createRequest := &latitude.FirewallCreateRequest{
		Data: latitude.FirewallCreateData{
			Type: "firewalls",
			Attributes: latitude.FirewallCreateAttributes{
				Name:    d.Get("name").(string),
				Project: d.Get("project").(string),
				Rules:   rules,
			},
		},
	}

	firewall, _, err := c.Firewalls.Create(createRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(firewall.ID)

	if err := d.Set("name", firewall.Name); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("rules", flattenFirewallRules(firewall.Rules)); err != nil {
		return diag.FromErr(err)
	}

	return diags
}

func resourceFirewallRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*latitude.Client)
	var diags diag.Diagnostics

	firewallID := d.Id()

	firewall, resp, err := c.Firewalls.Get(firewallID, nil)
	if err != nil {
		if resp.StatusCode == http.StatusNotFound {
			d.SetId("")
			return diags
		}

		return diag.FromErr(err)
	}

	if err := d.Set("name", firewall.Name); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("project", firewall.Project.ID); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("rules", flattenFirewallRules(firewall.Rules)); err != nil {
		return diag.FromErr(err)
	}

	return diags
}

func resourceFirewallUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*latitude.Client)
	var diags diag.Diagnostics

	firewallID := d.Id()

	updateRequest := &latitude.FirewallUpdateRequest{
		Data: latitude.FirewallUpdateData{
			ID:   firewallID,
			Type: "firewalls",
			Attributes: latitude.FirewallUpdateAttributes{
				Name:  d.Get("name").(string),
				Rules: parseFirewallRules(d),
			},
		},
	}

	firewall, _, err := c.Firewalls.Update(firewallID, updateRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("name", firewall.Name); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("rules", flattenFirewallRules(firewall.Rules)); err != nil {
		return diag.FromErr(err)
	}

	return diags
}

func resourceFirewallDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*latitude.Client)
	var diags diag.Diagnostics

	firewallID := d.Id()

	_, err := c.Firewalls.Delete(firewallID)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")

	return diags
}

func parseFirewallRules(d *schema.ResourceData) []latitude.FirewallRule {
	ruleSet := d.Get("rules").(*schema.Set)
	rules := make([]latitude.FirewallRule, 0, ruleSet.Len())

	for _, ruleRaw := range ruleSet.List() {
		rule := ruleRaw.(map[string]interface{})
		rules = append(rules, latitude.FirewallRule{
			From:     rule["from"].(string),
			To:       rule["to"].(string),
			Port:     rule["port"].(string),
			Protocol: rule["protocol"].(string),
		})
	}
	return rules
}

func flattenFirewallRules(rules []latitude.FirewallRule) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(rules))
	for _, rule := range rules {
		result = append(result, map[string]interface{}{
			"from":     rule.From,
			"to":       rule.To,
			"port":     rule.Port,
			"protocol": rule.Protocol,
			"default":  rule.Default,
		})
	}
	return result
}
