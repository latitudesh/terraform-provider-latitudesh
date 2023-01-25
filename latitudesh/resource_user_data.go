package latitudesh

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	api "github.com/latitudesh/latitudesh-go"
)

func resourceUserData() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceUserDataCreate,
		ReadContext:   resourceUserDataRead,
		UpdateContext: resourceUserDataUpdate,
		DeleteContext: resourceUserDataDelete,
		Schema: map[string]*schema.Schema{
			"project": {
				Type:        schema.TypeString,
				Description: "The id or slug of the project",
				Required:    true,
			},
			"description": {
				Type:        schema.TypeString,
				Description: "The User Data description",
				Required:    true,
			},
			"content": {
				Type:        schema.TypeString,
				Description: "Base64 encoded content of the User Data				",
				Required:    true,
			},
			"created": {
				Type:        schema.TypeString,
				Description: "The timestamp for when the User Data was created",
				Computed:    true,
			},
			"updated": {
				Type:        schema.TypeString,
				Description: "The timestamp for the last time the User Data was updated",
				Computed:    true,
			},
		},
	}
}

func resourceUserDataCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	c := m.(*api.Client)

	createRequest := &api.UserDataCreateRequest{
		Data: api.UserDataCreateData{
			Type: "user_data",
			Attributes: api.UserDataCreateAttributes{
				Description: d.Get("description").(string),
				Content:     d.Get("content").(string),
			},
		},
	}

	userData, _, err := c.UserData.Create(d.Get("project").(string), createRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(userData.ID)

	resourceUserDataRead(ctx, d, m)

	return diags
}

func resourceUserDataRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	userDataID := d.Id()

	userData, _, err := c.UserData.Get(userDataID, d.Get("project").(string), nil)
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("description", &userData.Description); err != nil {
		return diag.FromErr(err)
	}

	return diags
}

func resourceUserDataUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	userDataID := d.Id()

	updateRequest := &api.UserDataUpdateRequest{
		Data: api.UserDataUpdateData{
			Type: "user_data",
			ID:   userDataID,
			Attributes: api.UserDataUpdateAttributes{
				Description: d.Get("description").(string),
				Content:     d.Get("content").(string),
			},
		},
	}

	_, _, err := c.UserData.Update(userDataID, d.Get("project").(string), updateRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	d.Set("updated", time.Now().Format(time.RFC850))

	return resourceUserDataRead(ctx, d, m)
}

func resourceUserDataDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	userDataID := d.Id()

	_, err := c.UserData.Delete(userDataID, d.Get("project").(string))
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")

	return diags
}
