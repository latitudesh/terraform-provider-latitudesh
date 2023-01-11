package latitudesh

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	api "github.com/latitudesh/latitudesh-go"
)

func resourceProject() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceProjectCreate,
		ReadContext:   resourceProjectRead,
		UpdateContext: resourceProjectUpdate,
		DeleteContext: resourceProjectDelete,
		Schema: map[string]*schema.Schema{
			"name": {
				Type:        schema.TypeString,
				Description: "The name of the project",
				Required:    true,
			},
			"description": {
				Type:        schema.TypeString,
				Description: "The description of the project",
				Required:    true,
			},
			"environment": {
				Type:        schema.TypeString,
				Description: "The name of the project",
				Required:    true,
			},
			"created": {
				Type:        schema.TypeString,
				Description: "The timestamp for when the project was created",
				Computed:    true,
			},
			"updated": {
				Type:        schema.TypeString,
				Description: "The timestamp for the last time the project was updated",
				Computed:    true,
			},
		},
	}
}

func resourceProjectCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	c := m.(*api.Client)

	createRequest := &api.ProjectCreateRequest{
		Data: api.ProjectCreateData{
			Type: "projects",
			Attributes: api.ProjectCreateAttributes{
				Name:        d.Get("name").(string),
				Description: d.Get("description").(string),
				Environment: d.Get("environment").(string),
			},
		},
	}

	project, _, err := c.Projects.Create(createRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(project.ID)

	resourceProjectRead(ctx, d, m)

	return diags
}

func resourceProjectRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	projectID := d.Id()

	project, _, err := c.Projects.Get(projectID, nil)
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("name", &project.Name); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("description", &project.Description); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("environment", &project.Environment); err != nil {
		return diag.FromErr(err)
	}

	return diags
}

func resourceProjectUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	projectID := d.Id()

	updateRequest := &api.ProjectUpdateRequest{
		Data: api.ProjectUpdateData{
			Type: "projects",
			ID:   projectID,
			Attributes: api.ProjectCreateAttributes{
				Name:        d.Get("name").(string),
				Description: d.Get("description").(string),
				Environment: d.Get("environment").(string),
			},
		},
	}

	_, _, err := c.Projects.Update(projectID, updateRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	d.Set("updated", time.Now().Format(time.RFC850))

	return resourceProjectRead(ctx, d, m)
}

func resourceProjectDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	projectID := d.Id()

	_, err := c.Projects.Delete(projectID)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")

	return diags
}
