package latitudesh

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	api "github.com/latitudesh/latitudesh-go"
)

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
