package latitudesh

import (
	"context"
	"errors"
	"net/http"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	api "github.com/latitudesh/latitudesh-go"
)

func resourceMember() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceMemberCreate,
		ReadContext:   resourceMemberRead,
		DeleteContext: resourceMemberDelete,
		Schema: map[string]*schema.Schema{
			"first_name": {
				Type:        schema.TypeString,
				Description: "The member name",
				Optional:    true,
				ForceNew:    true,
			},
			"last_name": {
				Type:        schema.TypeString,
				Description: "The member name",
				Optional:    true,
				ForceNew:    true,
			},
			"email": {
				Type:        schema.TypeString,
				Description: "The member description",
				Required:    true,
				ForceNew:    true,
			},
			"mfa_enabled": {
				Type:        schema.TypeString,
				Description: "The member color",
				Computed:    true,
				ForceNew:    true,
			},
			"role": {
				Type:        schema.TypeString,
				Description: "The member color",
				Required:    true,
				ForceNew:    true,
			},
		},
		Importer: &schema.ResourceImporter{
			StateContext: NestedResourceRestAPIImport,
		},
	}
}

func resourceMemberCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	c := m.(*api.Client)

	createRequest := &api.MemberCreateRequest{
		Data: api.MemberCreateData{
			Type: "Member",
			Attributes: api.MemberCreateAttributes{
				FirstName: d.Get("first_name").(string),
				LastName:  d.Get("last_name").(string),
				Email:     d.Get("email").(string),
				Role:      api.AvailableRole(d.Get("role").(string)),
			},
		},
	}

	Member, _, err := c.Members.Create(createRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(Member.ID)

	resourceMemberRead(ctx, d, m)

	return diags
}

func resourceMemberRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	memberID := d.Id()

	member, resp, err := GetMember(c, memberID)
	if err != nil {
		if resp.StatusCode == http.StatusNotFound {
			d.SetId("")
			return diags
		}

		return diag.FromErr(err)
	}

	if err := d.Set("first_name", &member.FirstName); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("last_name", &member.LastName); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("email", &member.Email); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("role", &member.RoleName); err != nil {
		return diag.FromErr(err)
	}

	return diags
}

func resourceMemberDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	MemberID := d.Id()

	_, err := c.Members.Delete(MemberID)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")

	return diags
}

func GetMember(c *api.Client, memberID string) (*api.Member, *api.Response, error) {
	members, resp, err := c.Members.List(nil)
	if err != nil {
		return nil, resp, err
	}

	for _, member := range members {
		if member.ID == memberID {
			return &member, resp, nil
		}
	}

	resp.Status = "404 Not Found"
	resp.StatusCode = http.StatusNotFound

	notFoundErr := errors.New("ERROR\nStatus: 404\nSpecified Record Not Found")

	return nil, resp, notFoundErr
}
