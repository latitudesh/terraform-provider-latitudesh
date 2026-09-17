---
prompt-version: 7
---

# Scaffold Terraform support for a latitudesh-go-sdk service group

You are producing a **draft PR** that adds Terraform support for one
`latitudesh-go-sdk` service group. A human reviews it, fills the gaps you flag,
exercises it against the live API, and merges. You never reach the network.

The validation gate at the end is **necessary, not sufficient**: it proves the
scaffold is well-formed, not that it is correct. Reviewers rely on your handoff
notes (last section) to know what still needs checking. A silent gap is worse
than a flagged one. Rules marked **(recurring)** each closed a class of human
fix commits on earlier scaffolds.

## The target

- SDK group: `{{GROUP}}`
- Kinds to generate: {{KINDS}}
- Terraform type name: `{{TF_NAME}}` — use exactly this (the gate keys on it).
  If it looks mis-singularized (`latitudesh_releas`, `_databas`), still use it
  and put the correct spelling under **Open questions**.
- SDK methods on the group: {{METHODS}}
- Pinned SDK version: `{{SDK_VERSION}}`
- Manifest notes: {{NOTES}}

Notes of `none` mean nobody has looked at this group yet — not that it is clean.
When the notes record fields as deliberately unmapped (legacy, lazy-loaded,
envelope-only, team decision), that is a standing decision: do not expose those
fields, and carry the reason forward into your own notes.

## 0. Verify the premise before writing anything

```
go run ./cmd/sdkcoverage report -format json | jq '.pending[] | select(.group=="{{GROUP}}")'
```

Empty output means `{{GROUP}}` is already covered — **stop: write no files and
say so in your final message**. Do not add a second implementation, and do not
"improve" the existing one; that is a different task with a different review.

## 1. Decide the shape before the code

Answer all four first. Create `/tmp/scaffold-handoff.md` with the **Type shape**
line before you write any repo file; overwrite it with the full block once your
first draft builds (see Finish).

- **Type, or verb on another type?** Restore, reinstall, rotate, mount, map: a
  create whose subject is an existing resource is an argument or action on that
  resource, not a standalone type (`VirtualMachineRestores` is `backup_id` on
  `latitudesh_virtual_machine`). If the **whole** group is such a verb, scaffold
  nothing: open your final message with
  `DELIBERATE STOP: {{GROUP}} is a verb on <type>`, follow with the handoff block
  naming the recommended home for every method, and stop. Only the transcript
  survives this path — the gate fails on "no changes", the tracking issue gets a
  one-line link to the run, the handoff file is not read — and the group is
  re-picked every run until the reviewer records `ceiling` + `rationale` under
  `{{GROUP}}` in `sdk-coverage.yaml`; say so. This is the one gate failure that
  is the intended outcome. If only some methods are verbs, scaffold the type and
  list them as unmapped with the home you recommend.
- **Plural data source?** Only when {{KINDS}} includes `datasource`, the group
  has `List` / `ListFor<Parent>`, and practitioners consume the collection
  (backups, apps, catalog entries) or the singular has no unique selector: add
  `{{TF_NAME}}s` — this exact name, the one sanctioned addition — returning a
  list with client-side filters, and register both in `implemented_by`.
  Exemplar: the plural row in the conventions table. **(recurring)**
- **Which methods stay unmapped?** You scaffold **{{KINDS}} only**; a group often
  carries a second lifecycle that is its own resource (`ElasticIps` → BGP
  sessions, now `latitudesh_elastic_ip_bgp`; `Firewalls` →
  `latitudesh_firewall_assignment`). Name every method you did not map, with the
  home you recommend, in `notes:` under `{{GROUP}}` in `sdk-coverage.yaml` — a
  covered group never returns to the pending queue, so an unmentioned method is
  one nobody looks at again.
- **Which fields?** Curate for provisioning, not catalog browsing: identifiers
  (id/slug/name), filterable classifiers, capacity and compatibility, deploy
  inputs. Leave out presentation-only metadata — marketing descriptions,
  logo/image URLs, external links, catalog timestamps such as `created_at`,
  post-deploy instructions for humans. Record every deliberate omission in the
  group's `notes:` ("not exposed by team decision — <why>") so the field-drift
  agent treats it as binding instead of mapping the field back later.

## Turn budget

You have a hard cap of {{MAX_TURNS}} turns; a clean pass fits well under it.

- Chain independent `go doc` lookups in one Bash call (`go doc X && go doc Y`).
  Never pipe into `grep`, `head` or `sed` — only `jq` is allowlisted after a
  pipe, and a refused segment costs the whole call.
- Run the gate after your first complete draft and after fixing what it
  reported. If a third run still fails, stop iterating: record what is red in
  the handoff file and end — CI re-runs the gate.
- You cannot see a turn counter. Keep `/tmp/scaffold-handoff.md` current: a
  turn-capped session emits no final message, and when the gate passes that file
  is spliced into the PR body. When the gate fails — including the deliberate
  stops above — only the transcript survives, so anything a human must read on
  that path goes in your final message.

## Confirm every SDK symbol — never guess

The SDK is code-generated; names and shapes vary between groups.

- `go doc github.com/latitudesh/latitudesh-go-sdk.{{GROUP}}` lists the methods
  with their request and response types. Those names are not derivable
  (`ElasticIps.CreateElasticIP` takes `components.CreateElasticIP`;
  `VirtualMachines.Get` returns `operations.ShowVirtualMachineResponse`), so
  take them from the signatures, then
  `go doc github.com/latitudesh/latitudesh-go-sdk/models/components <Type>` (or
  `.../models/operations <Type>`) for each, and for every attributes/data type
  they reference. Always pass a type name: the bare package prints a ~700-line
  index with no field comments, `-all` prints ~7000.
- **Terraform attribute names are the SDK's json tags verbatim** (`ssh_keys`,
  `operating_system`, `server_id`). Never translate by hand.
- **Read the field doc comments, not just the types.** They carry semantics the
  signature cannot: `CreateElasticIPAttributes.ServerID` is "Required in routed
  mode and rejected in bgp mode"; `ElasticIPData.ID` "may be null during initial
  provisioning". Honor them; put what you cannot honor offline in the handoff.

### Error shapes are declared per operation — read them (recurring)

`go doc -src github.com/latitudesh/latitudesh-go-sdk {{GROUP}}.<Method>`, with
`<Method>` exactly as listed above (`GetElasticIP`, not `Get`), prints the whole
generated function (~230 lines, unfilterable). A `case httpRes.StatusCode == 404:`
that unmarshals `components.ErrorObject` means that operation returns a
**typed** `*components.ErrorObject` (JSON:API `errors[].status == "404"`); one
with only the `>= 400 && < 500` branch returns the generic `*components.APIError`
with a `StatusCode`. Shapes differ within a group, but only the operations your
code branches on need inspecting: the single-item Get (pollers read it too) and
the Delete — look both up in one call and list the rest as uninspected under
**API error codes**. Write one `<short>NotFound(err error) bool` that `errors.As`
both shapes and accepts **only** 404 — copy `publicNetworkNotFound` in
`latitudesh/resource_public_network.go`. Never match `err.Error()` against
`"404"` or `"not_found"`: a 403 or a transient 5xx then reads as "gone".

## House conventions — one exemplar per obligation

Read the file for the thing you need; do not read all of them. Do not invent
patterns that are not already here, with two mandated exceptions absent from
the repo today: `IsUnitTest: true` on mock-backed tests (see Tests — the mock
exemplar predates it) and `datasourcevalidator` from
`terraform-plugin-framework-validators` (the data-source twin of
`resourcevalidator.ExactlyOneOf`, used in `resource_firewall_assignment.go`).

Two legacy patterns in these files are bugs, not conventions: (a) matching
`err.Error()` against `"404"`/`"not_found"` in the `Delete` of most older
resources (`resource_virtual_machine.go`, `resource_server.go`,
`resource_user_data.go`, …) — write yours like `publicNetworkNotFound`;
(b) `ImportStateVerifyIgnore: … "project"` / `"plan"` in
`resource_virtual_machine_site_test.go` — it papers over the slug/ID selector
mismatch; keep the configured selector instead.

| What you need | Read |
|---|---|
| Resource skeleton, CRUD, waiters | `latitudesh/resource_virtual_machine.go` |
| Not-found helper across both SDK error shapes | `publicNetworkNotFound` in `latitudesh/resource_public_network.go` |
| Create → persist ID → reconciling GET; slug→ID before POST; `project` defaulting (`effectiveProject`) | `Create` and `ModifyPlan` in `latitudesh/resource_public_network.go` |
| Preserving configured inputs on read and import | `readPublicNetworkInto` / `ImportState` in `latitudesh/resource_public_network.go` |
| Unordered collections as `Set` | `sessionsToState` in `latitudesh/resource_elastic_ip_bgp.go` |
| Data source skeleton, lookup by filter — its selectors predate the blank-selector rule: add `stringvalidator.LengthAtLeast(1)` to each one you copy | `latitudesh/datasource_ssh_key.go` |
| Plural (list) data source, client-side filters, stable list sort | `latitudesh/datasource_virtual_machine_backups.go` |
| Minimal shapes when the group is small | `latitudesh/resource_user_data.go`, `latitudesh/datasource_tag.go` |
| SDK client + provider defaults in `Configure` | `internal/provider/config.go` (`ConfigureFromProviderData`) |
| Import, composite ID | `latitudesh/resource_vlan_assignment.go`, `latitudesh/resource_firewall_assignment.go` |
| Delete poller that accepts a terminal status | `waitForBackupDeleted` in `latitudesh/resource_virtual_machine_backup.go` |
| Offline unit test | `latitudesh/datasource_plan_offline_test.go`, `latitudesh/vm_plan_discovery_test.go` |
| Mock-server test — copy the `httptest` wiring only (rename `Test…`, add `IsUnitTest: true`, drop its `ImportStateVerifyIgnore`) | `latitudesh/resource_virtual_machine_site_test.go` |
| Acceptance test against the live API | `latitudesh/resource_virtual_network_test.go` |
| Doc template (`tffile` embed + `## Import`, no pasted HCL) | `templates/resources/virtual_machine_backup.md.tmpl`; data source: `templates/data-sources/virtual_machine_backups.md.tmpl` |
| Registration | `latitudesh/provider.go` |

Structure taken from those files, non-negotiable:

- Compile-time assertions per interface (`var _ resource.ResourceWithImportState = &XResource{}`).
- `Configure` goes through `ConfigureFromProviderData`; never read `ProviderData` by hand.
- One private `read<Name>Into(ctx, data, diags)` shared by Create, Read and Update.
  A 404 nulls the ID and the caller decides whether to `RemoveResource`.
- **Two classes of attribute on the read path (recurring — P1 in two PRs).**
  *Computed-only* (id, timestamps, server-assigned; also an unconfigured
  Optional+Computed with no `Default`): nil from the API → `types.XNull()`, never
  stale. *Configured or defaulted* (Required; Optional; Optional+Computed with a
  `Default`): overwrite **only when the API returns a value** — null over a
  planned value is "Provider produced inconsistent result after apply", and on
  refresh permanent drift. Plain Optional is in this second class; if the API
  can return a value the practitioner did not write (a server default, a
  normalised form), declare it Optional+Computed or the first apply fails the
  same way. Put the split in a comment above the mapping, as
  `resource_object_storage.go` does.
- **Selectors that accept a slug or an ID (`project`, `site`) keep what the
  practitioner wrote.** Fill from the API only when null or unknown (import). If
  the POST wants an ID and the config may carry a slug, resolve slug→ID first.
  Never hide the mismatch with `ImportStateVerifyIgnore`.
- **Create ends with the read.** After the POST, `SetAttribute` the ID into state,
  then run `read<Name>Into` and keep its result: a sparse create envelope must
  never leave nulls in state, and a failed reconcile must leave a tainted
  resource, not an orphan.
- **Collections without an API-defined order are a `SetAttribute`, or a
  `ListAttribute` sorted by a stable key before `types.ListValueFrom`
  (recurring).** `allowed_ips`, `ssh_key_ids`, `platforms`, nested product rows:
  a positional list in API order is a perpetual diff.
- Diagnostics reuse the existing vocabulary: `"Client Error"`, `"API Error"`,
  `"Missing project"`, `"Timeout waiting for …"`.
- Async lifecycles poll with `select { case <-ctx.Done(): …; case <-time.After(…): }`,
  never `time.Sleep`, behind `timeouts.Attributes(ctx, …)`. **A delete poller is
  done on a 404 _or_ the API's post-delete terminal status** — a soft-deleting
  API otherwise times out every destroy. Take candidates from the SDK status
  enum (`go doc …/models/components <Type>Status`) and flag which one Get returns
  after Delete for live confirmation; the only evidenced case is `Archived` for
  VM backups. No enum has `Deleted`; `Deleting` means keep polling; `Failed`
  during delete is an error to surface, never "done". A readiness poller stops
  at the configured deadline and never extends it. A data source read during
  `plan` caps retries with `operations.WithRetries(…)` (see `metricsRetryConfig`
  in `datasource_managed_database_metrics.go`) so a persistent 5xx fails in
  seconds, not the provider-wide five minutes.

**Import is expected.** Nearly every resource implements
`resource.ResourceWithImportState` with an `## Import` section in its template;
skipping it needs a stated reason. Import must leave every Required and
RequiresReplace input populated (`site`, `size`, `project`) — a null one makes
the first plan propose **replacement (recurring — P1)**. When the API cannot
supply a value, `AddWarning` naming the attribute and the
`lifecycle { ignore_changes }` workaround. If the API has no single-item read,
say so in the handoff — never silently omit import.

**Validate every constraint you document (recurring — the most frequent review
finding).** A `MarkdownDescription` that states a rule the schema does not
enforce is a bug. From `terraform-plugin-framework-validators` (already a
dependency): `stringvalidator.OneOf` / `OneOfCaseInsensitive` for enumerations,
`LengthAtLeast(1)` on every selector (a blank selector must not scan the
catalog), `ConflictsWith` / `ExactlyOneOf` / `AtLeastOneOf` for documented
exclusivity, `RegexMatches` for formats, `int64validator.OneOf` / `Between` for
size and period sets, `datasourcevalidator.Conflicting` for cross-attribute
rules on a data source. The ban is on **editing** `internal/validators` and
`internal/planmodifiers` — reuse what is there — not on validating. Only a rule
none of the above can express becomes a handoff item.

## Tests — three tiers, and you owe two of them

| Tier | Runs when | Name |
|---|---|---|
| Offline unit test (mapping, helpers, pollers) | always, `go test ./latitudesh` | `Test…` |
| Offline acceptance against an `httptest` mock | always — `IsUnitTest: true` | `Test…` (not `TestAcc`) |
| Acceptance against the live API | `TF_ACC=1` + `LATITUDESH_AUTH_TOKEN` | `TestAcc…` |

File names mirror the source file: `resource_<short>_test.go` /
`datasource_<short>_test.go`; extra files take a suffix (`_offline_test.go`,
`_mock_test.go`, `_lifecycle_test.go` offline; `_acc_test.go` live). Ship **at
least one offline test and at least one `TestAcc`** — the gate counts both by
function name in your `*<short>*_test.go` files.

**Mock-backed tests are offline tests (recurring).** `resource.Test` with
`IsUnitTest: true`, `ProtoV6ProviderFactories:
testAccProtoV6ProviderFactoriesWithMock(server)`, named `Test…`, **no
`PreCheck`**, and `auth_token = "mock-token"` in the HCL provider block. The gate
runs `env -u TF_ACC -u LATITUDESH_AUTH_TOKEN go test ./latitudesh`, so under it
`testAccTokenCheck` is `t.Fatal`, `Configure` needs the HCL token, and a test
without `IsUnitTest` skips — it only compiles, which is how a never-wired
recorder, an untested poller and a paging-blind mock all passed. With
`IsUnitTest` the callbacks do **not** self-skip: nothing in a mock test may
build a client other than the mock factory. `IsUnitTest` also makes the
framework launch a real `terraform` from `PATH` (present on the gate's runner);
if `go test` fails with `failed to find or install Terraform CLI`, that is the
environment — say so under **Tests actually exercised**, never rename the test
to `TestAcc…` to make it skip.

A mock must earn its keep:

- When the list request has `PageNumber`/`PageSize`, the SDK's `Next()` stops
  only on an empty page (or a page shorter than a `PageSize` you passed), so the
  mock must honour `page[number]`/`page[size]` and return an **empty page** after
  the last one, or it loops forever; put the matching item on page 2 at least
  once. A request without page fields (`VirtualMachineBackups.List`) has no
  `Next()` — one call is the whole collection; do not fake paging for it.
- Exercise every poller state you coded — ready, failed with reason, transient
  404 and 5xx, terminal status on delete, timeout — by calling the poller
  directly, as `resource_virtual_machine_backup_lifecycle_test.go` does.
- Cover not-found for each selector, each documented conflict, and — for a
  resource — import of a Required+RequiresReplace input.
- Assert with `TestCheckResourceAttr`, not `TestCheckResourceAttrSet` — `"0"` is
  "set".

The live `TestAcc…` is compiled here and run by a human, so it must be runnable
as written: `PreCheck: func() { testAccTokenCheck(t) }` (not `testAccPreCheck`);
`ProtoV6ProviderFactories: testAccProtoV6ProviderFactories()`, or
`…WithVCR(rec)` whenever you create a recorder (an unpassed recorder records
nothing); a `testAccCheck<Name>Destroy` on `newSDKClientFromEnv()` that accepts
**only** not-found — any other error is a failure, not proof of deletion;
`testAccSharedServers(t, n)` if you need a server. Anything that could reach the
API belongs only inside that `TestAcc…`'s callbacks, which self-skip without
`TF_ACC` — never in a test function body, never in a mock test's callbacks:
both run on every contributor's `go test` and fail the gate.

Do not set `TF_ACC`, run the live tier, or record cassettes
(`latitudesh/fixtures/<TestName>.yaml`, recorded by a human with
`LATITUDE_TEST_RECORDER=record`).

## Deliverables

1. `latitudesh/resource_<short>.go` and/or `latitudesh/datasource_<short>.go`, per {{KINDS}}
   — plus `latitudesh/datasource_<short>s.go` when section 1 called for a plural. The
   plural is a full deliverable: its own `templates/data-sources/<short>s.md.tmpl`,
   `examples/{{TF_NAME}}s.tf` and `latitudesh/datasource_<short>s_test.go`, like the
   exemplar. The gate's per-kind checks look only at `{{TF_NAME}}`: a plural without a
   template gets a silent default doc, a `tffile` pointing at a missing example fails
   `go generate`, and only `sdkcoverage check` (registration ↔ `implemented_by`) holds
   it to account.
2. `latitudesh/resource_<short>_test.go` (plus `_offline_test.go` / `_lifecycle_test.go` if cleaner) covering both required tiers.
3. Registration in `latitudesh/provider.go` — `Resources()`, `DataSources()`, and/or `Actions()`.
4. `sdk-coverage.yaml`: `implemented_by: [{{TF_NAME}}]` (and the plural, if any) under `{{GROUP}}`,
   plus `notes:` naming every unmapped method and every deliberately omitted field.
5. `sdk-fields.lock.yaml`: **do not touch it** — the pipeline runs
   `go run ./cmd/sdkcoverage fields -write -group {{GROUP}}` after the gate passes. For a
   checklist of the group's field rows, the read-only
   `go run ./cmd/sdkcoverage fields -group {{GROUP}}` works once deliverables 3–4 are in
   place. Never run a bare `fields -write`: it re-locks every other group.
6. `templates/resources/<short>.md.tmpl` and/or `templates/data-sources/<short>.md.tmpl`.
   Pull the example in with `{{ tffile (printf "examples/%s.tf" .Name) }}` rather than
   pasting HCL, so the two cannot drift. Add an `## Import` section if you implemented
   import; an ID prefix shown there is only ever one an SDK doc comment states verbatim
   (`bkt_` from `ObjectStorageData.ID`, "Object storage ID with bkt_ prefix") — otherwise
   a `<BACKUP_ID>`-style placeholder, like the existing templates. Never a guess.
7. `examples/{{TF_NAME}}.tf` — runnable, minimal, **free of placeholders (recurring)**:
   reference a resource created in the same file (`id = latitudesh_x.example.id`),
   never `"proj_..."` or `"pnet_..."`. The gate runs `terraform validate` on the file
   **alone**: no `var.*`, no `locals`, nothing declared elsewhere — helper resources
   live in the same file. Grep `examples/` for `resource "<helper_type>" "<label>"`
   before choosing each helper's label: CI validates `examples/` as one module, so a
   collision surfaces only there. Pick the site from this group's own doc comments or
   manifest notes — never copy `SAO2` from a sibling — and say why in the handoff.
8. `docs/` — produced only by the gate's generate step, never hand-written.

## Hard rules

- Touch only: `latitudesh/{resource,datasource,action}_*.go`, `latitudesh/*_test.go`,
  `latitudesh/provider.go`, `sdk-coverage.yaml`, `templates/**/*.tmpl`, `examples/**/*.tf`,
  `docs/**/*.md` — the gate's exact patterns — plus `/tmp/scaffold-handoff.md`, the one
  write allowed outside the repository. No other file types in those directories (no
  README, `.tfvars`, JSON) and nothing nested under `latitudesh/` — a new subdirectory
  fails the gate's first check and `scripts/scaffold-rm.sh` cannot remove it; mock
  payloads live inline in the `_test.go`. Not `sdk-fields.lock.yaml` (the workflow
  seeds it), not `go.mod`, not `cmd/`, not `internal/`, not `.github/`.
- If you `Write` a Go file to the wrong path (`latitudesh/foo_mapping.go` instead of
  `latitudesh/resource_foo_mapping.go`), delete it with
  `scripts/scaffold-rm.sh latitudesh/<name>.go` — the **only** way to remove a file;
  `Write`/`Edit` cannot, and a leftover fails the gate. Never leave a
  "delete before merge" stub.
- Never add a `go:generate` directive.
- Never reach the network, provision infrastructure, set `TF_ACC`, run the live
  `TestAcc…` tier, or record cassettes. Mock-backed `IsUnitTest` cases are offline
  tests: the gate runs them under plain `go test`, and so do you.
- Never write a token, key, or secret anywhere.
- Never hand-edit `docs/` — edit the template and regenerate.

## Finish: handoff file, then gate

**As soon as your first complete draft builds, write the full handoff block
(below) to `/tmp/scaffold-handoff.md` with the Write tool, and update it whenever
you learn something new.** It lives outside the repo tree on purpose — never
write it inside the repository.

Then run the gate and fix what it reports:

```
scripts/scaffold-validate.sh --group {{GROUP}} --type-name {{TF_NAME}} --kinds "{{KINDS}}"
```

Treat each failure as a task to fix, not to work around. `PASS` means well-formed
(the gate prints its nine steps), not correct. End your final message with the
same block, filled in, **as plain markdown — no surrounding code fence — under
7,000 bytes**: the PR body keeps everything from the `## Handoff` line to the end
of your message and truncates at 8,000 bytes (same cap for the file), so a long
early section silently drops the later ones and a reproduced closing fence
swallows the rest of the PR body. One dense line per heading; detail belongs in
code comments. Write "none" only where genuinely nothing applies — the reviewer
reads this before the diff.

```markdown
## Handoff — not verified offline

**Type shape:** type vs verb-on-another-type; whether a plural data source was added, or why not
**Unmapped SDK methods:** each with the home you recommend
**State consistency:** the Computed-only vs configured/defaulted split; selectors that accept slug or ID and how the configured value is preserved; any plan modifier you would have added under `internal/planmodifiers`
**Collection ordering:** every list/set attribute and why its order is safe
**Async lifecycle:** which operations are async, which statuses you assumed, what ends a delete; whether plan-time reads cap their retries
**API error codes:** which operations return a typed `ErrorObject`, which the generic `APIError`; which you inspected, which you did not
**Required vs optional:** attributes where the spec and the real API may disagree
**Validation:** every documented constraint and the validator enforcing it; what nothing in `terraform-plugin-framework-validators` or `internal/validators` could express — a missing validator is never why a documented rule goes unenforced
**Pagination:** whether list calls paginate, and whether you follow `Next`
**Import:** the ID format you assumed, which inputs the read fills, or why import is not implemented
**Tests actually exercised:** what the offline mock tests execute (pages, poller states, not-found, conflicts, import) vs what only compiles; which routes and response shapes the mock merely assumes — what only the live `TestAcc` can confirm
**Example choices:** the site and any ID prefix you used, and where each came from
**Open questions for the reviewer:** …
```
