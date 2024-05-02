---
page_title: "latitudesh_tag Resource - terraform-provider-latitudesh"
subcategory: ""
description: |-
  
---

# latitudesh_tag (Resource)



## Example usage

```terraform
resource "latitudesh_tag" "tag" {
  name          = "Tag Name"
  description   = "Tag Description"
  color         = "#ff0000"
}
```

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `color` (String) The tag color
- `description` (String) The tag description
- `name` (String) The tag name

### Read-Only

- `id` (String) The ID of this resource.

## Import
Tag can be imported using the TagID along with the projectID that contains the Tag, e.g.,

```sh
$ terraform import latitudesh_tag.tag projectID:TagID