---
prompt-version: 2
---

# Scaffold Terraform support for a latitudesh-go-sdk service group

You are producing a **draft PR** that adds Terraform support for one
`latitudesh-go-sdk` service group. A human reviews it, fills the gaps you flag,
exercises it against the live API, and merges. You never reach the network.

The validation gate at the end is **necessary, not sufficient**: it proves the
scaffold is well-formed, not that it is correct. Reviewers rely on your handoff
notes (last section) to know what still needs checking. A silent gap is worse
than a flagged one.

## 0. Verify the premise before writing anything

```
go run ./cmd/sdkcoverage report -format json | jq '.pending[] | select(.group=="{{GROUP}}")'
```

Empty output means `{{GROUP}}` is already covered — **stop and say so**. Do not add
a second implementation, and do not "improve" the existing one; that is a
different task with a different review.

## The target

- SDK group: `{{GROUP}}`
- Kinds to generate: {{KINDS}}
- Terraform type name: `{{TF_NAME}}` — use exactly this, do not invent your own
- SDK methods on the group: {{METHODS}}
- Pinned SDK version: `{{SDK_VERSION}}`
- Manifest notes: {{NOTES}}

Notes of `none` mean nobody has looked at this group yet — not that it is clean.
Derive the shape from the SDK and record what you find.

## Confirm every SDK symbol — never guess

The SDK is code-generated; names and shapes vary between groups and are not
guessable.

- `go doc github.com/latitudesh/latitudesh-go-sdk.{{GROUP}}` for the methods.
- `go doc github.com/latitudesh/latitudesh-go-sdk/models/operations` and
  `.../models/components` for request and response shapes.

Two rules carry most of the value here:

1. **Terraform attribute names are the SDK's json tags verbatim** — those are the
   API's snake_case names (`ssh_keys`, `operating_system`, `server_id`). Copy
   them; never translate by hand.
2. **Read the field doc comments, not just the types.** They carry API semantics
   the signature cannot. `CreateElasticIPAttributes.ServerID` is documented
   "Required in routed mode and rejected in bgp mode"; `ElasticIPData.ID` is "may
   be null during initial provisioning". Honor them, and put anything you cannot
   honor offline into the handoff.

## Methods the requested kinds do not cover

Compare {{METHODS}} against what you actually wired up. A group often carries a
second lifecycle that is its own Terraform resource — `ElasticIps` carries four
BGP-session methods; `Firewalls` carries assignments, which ship separately as
`latitudesh_firewall_assignment`.

You are scaffolding **{{KINDS}} only**. Every method you did not map must be named
in `notes:` under `{{GROUP}}` in `sdk-coverage.yaml`, and repeated in the handoff.
A covered group never returns to the pending queue, so an unmentioned method is
one nobody will look at again.

## House conventions — one exemplar per obligation

Read the file for the thing you need. Do not read all of them, and do not invent
patterns that are not already here.

| What you need | Read |
|---|---|
| Resource skeleton, CRUD, waiters, typed errors | `latitudesh/resource_virtual_machine.go` |
| Data source skeleton, lookup by filter | `latitudesh/datasource_ssh_key.go` |
| Minimal shapes when the group is small | `latitudesh/resource_user_data.go`, `latitudesh/datasource_tag.go` |
| SDK client + provider defaults in `Configure` | `internal/provider/config.go` (`ConfigureFromProviderData`) |
| `project` defaulting (resource attr > provider block) | `ModifyPlan` in `latitudesh/resource_user_data.go` |
| Import, simple ID | `ImportState` in `latitudesh/resource_virtual_machine.go` |
| Import, composite ID | `latitudesh/resource_vlan_assignment.go`, `latitudesh/resource_firewall_assignment.go` |
| Offline unit test | `latitudesh/datasource_plan_offline_test.go`, `latitudesh/vm_plan_discovery_test.go` |
| Acceptance test against a mock server (no credentials) | `latitudesh/resource_virtual_machine_site_test.go` |
| Acceptance test against the live API | `latitudesh/resource_virtual_network_test.go` |
| Doc template | `templates/resources/virtual_machine.md.tmpl` |
| Registration | `latitudesh/provider.go` |

Structure taken from those files, non-negotiable:

- Compile-time assertions per interface (`var _ resource.ResourceWithImportState = &XResource{}`).
- `Configure` goes through `ConfigureFromProviderData`; never read `ProviderData` by hand.
- One private `read<Name>Into(ctx, data, diags)` shared by Create, Read and Update.
  A 404 nulls the ID and the caller decides whether to `RemoveResource`.
- Every SDK pointer is nil-checked and mapped to `types.XNull()` — never left stale.
- Diagnostics reuse the existing vocabulary: `"Client Error"`, `"API Error"`,
  `"Missing project"`, `"Timeout waiting for …"`.
- Async lifecycles poll with `select { case <-ctx.Done(): …; case <-time.After(…): }`,
  never `time.Sleep`, behind `timeouts.Attributes(ctx, …)`.

**Import is expected.** Eleven of twelve resources implement
`resource.ResourceWithImportState` and carry an `## Import` section in their doc
template. If the API has no single-item read and import is impossible, say so in
the handoff — do not silently omit it.

**Reuse `internal/planmodifiers` and `internal/validators`; do not hand-roll a
duplicate.** They are also off-limits to edit. If this group needs a validator or
plan modifier that is not there, use plain schema constraints or
`terraform-plugin-framework-validators`, and record the gap in the handoff.

## Tests — three tiers, and you owe two of them

| Tier | Runs when | Name |
|---|---|---|
| Offline unit test | always, `go test ./latitudesh` | `Test…` (no `Acc`) |
| Acceptance against an `httptest` mock | `TF_ACC=1`, no credentials | `TestAcc…` |
| Acceptance against the live API | `TF_ACC=1` + `LATITUDESH_AUTH_TOKEN` | `TestAcc…` |

File names mirror the source file: `resource_<short>_test.go` /
`datasource_<short>_test.go`. Extra files take a topic or kind suffix
(`_offline_test.go`, `_mock_test.go`, `_acc_test.go`) — that suffix is how the
offline tests are kept separate in this package.

Ship **at least one offline test and at least one `TestAcc`**. The acceptance test
only has to compile; a human runs it. Use the existing helpers:
`PreCheck: func() { testAccTokenCheck(t) }` (not `testAccPreCheck`),
`ProtoV6ProviderFactories: testAccProtoV6ProviderFactories()`, a
`testAccCheck<Name>Destroy` built on `newSDKClientFromEnv()`, and
`testAccSharedServers(t, n)` if you need a server.

Do not run acceptance tests and do not record cassettes. Cassettes live in
`latitudesh/fixtures/<TestName>.yaml` and a human records them with
`LATITUDE_TEST_RECORDER=record`; most newer resources have none and run live.

## Deliverables

1. `latitudesh/resource_<short>.go` and/or `latitudesh/datasource_<short>.go`, per {{KINDS}}.
2. `latitudesh/resource_<short>_test.go` (plus `_offline_test.go` if cleaner) covering both required tiers.
3. Registration in `latitudesh/provider.go` — `Resources()`, `DataSources()`, and/or `Actions()`.
4. `sdk-coverage.yaml`: `implemented_by: [{{TF_NAME}}]` under `{{GROUP}}`, plus `notes:` naming every unmapped method.
5. `templates/resources/<short>.md.tmpl` and/or `templates/data-sources/<short>.md.tmpl`.
   Pull the example in with `{{ tffile (printf "examples/%s.tf" .Name) }}` rather than
   pasting HCL, so the example and the doc cannot drift. Add an `## Import` section
   if you implemented import.
6. `examples/{{TF_NAME}}.tf` — runnable and minimal.
7. `docs/` — produced only by the gate's generate step, never hand-written.

## Hard rules

- Touch only: `latitudesh/{resource,datasource,action}_*.go`, `latitudesh/*_test.go`,
  `latitudesh/provider.go`, `sdk-coverage.yaml`, `templates/**`, `examples/**`, `docs/**`.
  Not `go.mod`, not `cmd/`, not `internal/`, not `.github/`.
- Never add a `go:generate` directive.
- Never reach the network, provision infrastructure, run acceptance tests, or record cassettes.
- Never write a token, key, or secret anywhere.
- Never hand-edit `docs/` — edit the template and regenerate.

## Finish: gate, then handoff

Run the gate and fix what it reports:

```
scripts/scaffold-validate.sh --group {{GROUP}} --type-name {{TF_NAME}} --kinds "{{KINDS}}"
```

Treat each failure as a task to fix, not to work around. `PASS` means the scaffold
is well-formed: gofmt, vet, build, offline tests, coverage reconcile, docs
regenerate clean, and every requested kind registered with its file, template,
example and tests.

`PASS` does **not** mean correct. End your run with this block, filled in. Write
"none" only where genuinely nothing applies — the reviewer reads this before the
diff.

```markdown
## Handoff — not verified offline

**Unmapped SDK methods:** …
**Async lifecycle:** which operations are async, which statuses you assumed, what you polled for
**API error codes:** which you mapped, which you guessed
**Required vs optional:** attributes where the spec and the real API may disagree
**Pagination:** whether list calls paginate, and whether you follow `Next`
**Import:** the ID format you assumed, or why import is not implemented
**Missing validator / plan modifier:** what you would have added under `internal/`
**Open questions for the reviewer:** …
```
