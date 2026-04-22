# latitudesh_elastic_ip (Resource)

Manages a Latitude.sh Elastic IP — a static public IPv4 address that is
assigned to a server and can be moved between servers within the same project.

Provisioning is asynchronous: on create the IP transitions `configuring → active`;
on move, `active → moving → active`; on destroy, `active → releasing → gone`.
The provider waits for each transition with a configurable timeout.

## Example Usage

```hcl
provider "latitudesh" {
  project = "proj_abc123"
}

resource "latitudesh_elastic_ip" "front" {
  server_id = "sv_XYZ123"
}

output "address" {
  value = latitudesh_elastic_ip.front.address
}
```

To move the Elastic IP to a different server, change `server_id`.

## Schema

### Required

- `server_id` (String) — The server ID the Elastic IP is assigned to. Changing this value performs an asynchronous move.

### Optional

- `project` (String) — The project (ID or slug) that owns the Elastic IP. If unset, the provider-level `project` default is used. Changing this forces a new resource.
- `timeouts` (Block) — Timeout overrides for create, update (move), and delete. Each defaults to 10 minutes.

### Read-Only

- `id` (String) — Elastic IP identifier.
- `address` (String) — The assigned IP address.
- `status` (String) — Current status: `configuring`, `active`, `moving`, `releasing`, or `error`.

## Error Handling

The provider surfaces well-known API errors as named diagnostics:

| Error code | Meaning |
|---|---|
| `SITE_NOT_SUPPORTED` | The server's site does not support Elastic IPs. |
| `ELASTIC_IP_LIMIT_REACHED` | The team has reached its Elastic IP quota. |
| `ELASTIC_IP_NOT_ACTIVE` | Move attempted while the Elastic IP is not `active`. |

## Import

Import is not yet supported for this resource. See PD-5929 follow-ups.
