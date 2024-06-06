package latitudesh

import (
	"context"
	"errors"
	"net/http"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	api "github.com/latitudesh/latitudesh-go"
)

func resourceTag() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceTagCreate,
		ReadContext:   resourceTagRead,
		UpdateContext: resourceTagUpdate,
		DeleteContext: resourceTagDelete,
		Schema: map[string]*schema.Schema{
			"name": {
				Type:        schema.TypeString,
				Description: "The tag name",
				Required:    true,
			},
			"description": {
				Type:        schema.TypeString,
				Description: "The tag description",
				Required:    true,
			},
			"color": {
				Type:        schema.TypeString,
				Description: "The tag color",
				Required:    true,
			},
		},
		Importer: &schema.ResourceImporter{
			StateContext: NestedResourceRestAPIImport,
		},
	}
}

func resourceTagCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)
	var diags diag.Diagnostics

	createRequest := &api.TagCreateRequest{
		Data: api.TagCreateData{
			Type: "Tag",
			Attributes: api.TagCreateAttributes{
				Name:        d.Get("name").(string),
				Description: d.Get("description").(string),
				Color:       d.Get("color").(string),
			},
		},
	}

	Tag, _, err := c.Tags.Create(createRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(Tag.ID)

	if err := d.Set("name", &Tag.Name); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("description", &Tag.Description); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("color", &Tag.Color); err != nil {
		return diag.FromErr(err)
	}

	return diags
}

func resourceTagRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)
	var diags diag.Diagnostics

	TagID := d.Id()

	Tag, resp, err := GetTag(c, TagID)

	if err != nil {
		if resp.StatusCode == http.StatusNotFound {
			d.SetId("")
			return diags
		}

		return diag.FromErr(err)
	}

	if err := d.Set("name", &Tag.Name); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("description", &Tag.Description); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("color", &Tag.Color); err != nil {
		return diag.FromErr(err)
	}

	return diags
}

func resourceTagUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)
	var diags diag.Diagnostics

	TagID := d.Id()

	updateRequest := &api.TagUpdateRequest{
		Data: api.TagUpdateData{
			Type: "Tags",
			ID:   TagID,
			Attributes: api.TagUpdateAttributes{
				Name:        d.Get("name").(string),
				Description: d.Get("description").(string),
				Color:       d.Get("color").(string),
			},
		},
	}

	Tag, _, err := c.Tags.Update(TagID, updateRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("name", &Tag.Name); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("description", &Tag.Description); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("color", &Tag.Color); err != nil {
		return diag.FromErr(err)
	}

	return diags
}

func resourceTagDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)
	var diags diag.Diagnostics

	TagID := d.Id()

	_, err := c.Tags.Delete(TagID)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")

	return diags
}

func tagIDs(tags []api.EmbedTag) []string {
	ids := []string{}
	for _, tag := range tags {
		ids = append(ids, tag.ID)
	}
	return ids
}

func parseTags(d *schema.ResourceData) []string {
	tags := d.Get("tags").([]interface{})

	if len(tags) == 0 {
		return []string{""}
	}

	tags_slice := make([]string, len(tags))
	for i, ssh_key := range tags {
		tags_slice[i] = ssh_key.(string)
	}

	return tags_slice
}

func GetTag(c *api.Client, tagID string) (*api.Tag, *api.Response, error) {
	tags, resp, err := c.Tags.List(nil)
	if err != nil {
		return nil, resp, err
	}

	for _, tag := range tags {
		if tag.ID == tagID {
			return &tag, resp, nil
		}
	}

	resp.Status = "404 Not Found"
	resp.StatusCode = http.StatusNotFound

	notFoundErr := errors.New("ERROR\nStatus: 404\nSpecified Record Not Found")

	return nil, resp, notFoundErr
}
