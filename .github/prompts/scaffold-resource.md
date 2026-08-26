---
prompt-version: 1
---

# Scaffold Terraform support for a latitudesh-go-sdk service group

You are adding first-class Terraform support for a `latitudesh-go-sdk` service
group this provider does not expose yet. Produce a complete, compiling,
reviewable **draft**. A human reviews it, records the acceptance-test cassettes,
and merges it — you do not run against the live API or record anything.

## The target

- SDK group: `{{GROUP}}`
- Kinds to generate: {{KINDS}}
- Terraform type name: `{{TF_NAME}}`
- SDK methods on the group: {{METHODS}}
- Pinned SDK version: `{{SDK_VERSION}}`
- Manifest notes (read these carefully — they often flag a spec/API gap): {{NOTES}}

## Study these first, and mirror them exactly

This provider has strong house conventions. Read these before writing anything
and follow their structure, error handling, context/diagnostics usage, import
support, and test layout. Do not invent new patterns:

- `latitudesh/resource_virtual_machine.go` — the canonical resource pattern (schema, Create/Read/Update/Delete, import).
- `latitudesh/datasource_ssh_key.go` — the canonical data source pattern.
- `latitudesh/resource_user_data.go` and `latitudesh/datasource_tag.go` — smaller, simpler shapes when the group is minimal.
- `internal/planmodifiers` and `internal/validators` — reuse these; do not hand-roll a plan modifier or validator that already exists here.
- `latitudesh/provider.go` — where resources, data sources, and actions are registered.
- The `*_test.go` file next to whichever resource you modeled on — for the offline vs. acceptance test split.
- A matching doc template under `templates/resources/` or `templates/data-sources/`, and an `examples/latitudesh_*.tf` — the doc template and example shape.

## Confirm every SDK symbol with `go doc` — never guess

The SDK is code-generated: method names, request/response types, and field names
are not guessable and vary between groups. Before calling anything, verify it:

- `go doc github.com/latitudesh/latitudesh-go-sdk.<GroupType>` for the methods.
- `go doc github.com/latitudesh/latitudesh-go-sdk/models/operations` and `.../models/components` for request and response shapes.

Terraform attribute names must match the SDK model's json tags — those are the
API's snake_case names (`ssh_keys`, `operating_system`). Copy them; do not
translate by hand.

## Deliverables — all of them

1. **Go file(s)** under `latitudesh/`: `resource_<name>.go` and/or `datasource_<name>.go`, per the kinds above, following the studied files.
2. **Test file** `latitudesh/<name>_test.go` with both:
   - offline unit test(s) that pass without `TF_ACC` (schema/model assertions, no network), and
   - at least one `TestAcc…` acceptance test that **compiles** but is meant to be recorded later by a human. It must guard on `TF_ACC` and skip when unset. Do not run it and do not record a cassette.
3. **Registration** in `latitudesh/provider.go` — add the constructor to `Resources()`, `DataSources()`, and/or `Actions()`.
4. **Manifest**: add `implemented_by: [{{TF_NAME}}]` under `{{GROUP}}` in `sdk-coverage.yaml`, matching the existing entries' style and grouping.
5. **Doc template**: `templates/resources/<name>.md.tmpl` and/or `templates/data-sources/<name>.md.tmpl`, mirroring an existing one.
6. **Example**: `examples/{{TF_NAME}}.tf`, runnable and minimal.
7. **Rendered docs** under `docs/` — produced only by the validation script's generate step, never hand-written.

## Hard rules

- Touch **only** these paths: `latitudesh/{resource,datasource,action}_*.go`, `latitudesh/*_test.go`, `latitudesh/provider.go`, `sdk-coverage.yaml`, `templates/**`, `examples/**`, `docs/**`. Nothing else — not `go.mod`, not `cmd/`, not `internal/`, not `.github/`.
- Never add a `go:generate` directive.
- Never reach the network, provision infrastructure, run acceptance tests, or record cassettes.
- Never write a token, key, or secret anywhere.
- Do not hand-edit anything under `docs/` — edit the template and let generation produce the doc.

## Finish only when the gate passes

When you think you are done, run the validation gate and fix whatever it reports.
You are **not** finished until it prints `PASS`:

```
scripts/scaffold-validate.sh --group {{GROUP}} --type-name {{TF_NAME}} --kinds "{{KINDS}}"
```

It runs gofmt, `go vet`, `go build`, the offline tests (including the coverage
reconcile), regenerates the docs, and then holds you to the full deliverable list:
every requested kind must be registered in `provider.go` AND ship its Go file and
doc template, the example must exist, and a test file named after the new type
must contain both a `TestAcc` function and an offline test. Treat each failure as
a task to fix, not to work around.
