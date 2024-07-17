package latitudesh

import (
	"context"
	"errors"
	"net/http"
	"strings"
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
			"provisioning_type": {
				Type:        schema.TypeString,
				Description: "The provisioning type of the project",
				Required:    true,
			},
			"description": {
				Type:        schema.TypeString,
				Description: "The description of the project",
				Optional:    true,
			},
			"environment": {
				Type:        schema.TypeString,
				Description: "The name of the project",
				Required:    true,
			},
			"tags": {
				Type: schema.TypeList,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Description: "List of project tags",
				Optional:    true,
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
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
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
				Name:             d.Get("name").(string),
				ProvisioningType: d.Get("provisioning_type").(string),
				Description:      d.Get("description").(string),
				Environment:      d.Get("environment").(string),
			},
		},
	}

	project, _, err := c.Projects.Create(createRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(project.ID)

	if d.Get("tags") != nil {
		resourceProjectUpdate(ctx, d, m)
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
	if err := d.Set("created", &project.CreatedAt); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("updated", &project.UpdatedAt); err != nil {
		return diag.FromErr(err)
	}

	if d.Get("tags") != nil {
		resourceProjectUpdate(ctx, d, m)
	}

	return diags
}

func resourceProjectRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	projectID := d.Id()

	project, resp, err := c.Projects.Get(projectID, nil)
	if err != nil {
		if resp.StatusCode == http.StatusNotFound {
			d.SetId("")
			return diags
		}

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
	if err := d.Set("created", &project.CreatedAt); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("updated", &project.UpdatedAt); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("tags", tagIDs(project.Tags)); err != nil {
		return diag.FromErr(err)
	}

	return diags
}

func resourceProjectUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)
	var diags diag.Diagnostics

	projectID := d.Id()
	tags := parseTags(d)

	updateRequest := &api.ProjectUpdateRequest{
		Data: api.ProjectUpdateData{
			Type: "projects",
			ID:   projectID,
			Attributes: api.ProjectUpdateAttributes{
				Name:        d.Get("name").(string),
				Description: d.Get("description").(string),
				Environment: d.Get("environment").(string),
				Tags:        tags,
			},
		},
	}

	project, _, err := c.Projects.Update(projectID, updateRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	d.Set("updated", time.Now().Format(time.RFC850))

	if err := d.Set("name", &project.Name); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("description", &project.Description); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("environment", &project.Environment); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("created", &project.CreatedAt); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("updated", &project.UpdatedAt); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("tags", tagIDs(project.Tags)); err != nil {
		return diag.FromErr(err)
	}

	return diags
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

func NestedResourceRestAPIImport(ctx context.Context, d *schema.ResourceData, m interface{}) ([]*schema.ResourceData, error) {
	var err error
	nestedResourceID := d.Id()

	//extract projectID and nestedResourceID
	splitIDs := strings.Split(nestedResourceID, ":")

	if len(splitIDs) == 2 {
		// Set the projectID and requested nestedResourceID
		d.Set("project", splitIDs[0])
		d.SetId(splitIDs[1])
	} else {
		err = errors.New("projectID and nestedResourceID not passed correctly. Please pass as projectID:nestedResourceID")
	}

	return []*schema.ResourceData{d}, err
}
