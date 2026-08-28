---
prompt-version: 2
---

# Map field drift on covered latitudesh-go-sdk service group(s)

You are producing a **draft PR** that reconciles already-covered SDK service
group(s) with what their Terraform types actually map, after the SDK's models
moved. A human reviews it, exercises it against the live API, and merges. You
never reach the network.

`{{GROUP}}` may name several comma-separated groups: when drift is breaking,
every breaking group rides in one PR, because the gate checks the whole surface
and could never go green one group at a time.

This is NOT scaffolding: the resource exists, users hold state written by it,
and your edits change what future plans and applies do to that state. The
validation gate proves your change is well-formed, not that it is correct —
reviewers rely on your handoff (last section) for everything you could not
verify offline.

## 0. Verify the premise before writing anything

```
go run ./cmd/sdkcoverage drift -format text
```

No row naming any of `{{GROUP}}` means the drift is already reconciled — **stop
and say so**. Someone else's PR got there first; do not "improve" the existing
mapping, that is a different task with a different review.

## The target

- SDK group(s): `{{GROUP}}`
- Terraform types that own the mapping: {{IMPLEMENTED_BY}}
- Pinned SDK version: `{{SDK_VERSION}}`
- The drift, verbatim from the detector:

```json
{{DRIFT_JSON}}
```

Every row is yours to close, one way or the other: map it, or deliberately omit
it and say why. `breaking: true` rows are failing `TestProviderSDKFieldDrift`
on this branch right now — the gate stays red until you resolve them.

## Turn budget

Hard cap of {{MAX_TURNS}} turns; a clean pass fits well under it. Keep
`/tmp/drift-handoff.md` current from your first edit onwards (see Finish) — a
turn-capped session emits no final message, and that file is the only handoff
that survives.

## How to treat each drift kind

**`field_added`** — expose it. The Terraform attribute name is the SDK's json
tag verbatim (the detector's `suggested_attribute` already says it). New
attributes are **Optional + Computed** — never Required — unless the field is a
required member of a *create request body*, in which case flag the
config-breaking consequence in the handoff instead of deciding it yourself.
Read the SDK field's doc comment before mapping: it carries semantics the type
cannot (nullable-during-provisioning, mode-dependent requirements). A field the
provider deliberately should NOT expose (server-side seeding artifacts,
lazy-loaded extras) is omitted by syncing the lock and recording why — see
Finish.

**`enum_value_added` / `enum_value_removed`** — find every site that validates
or coerces the enum's values, not just the schema. The canonical trap:
`resource_firewall.go` coerces unknown protocols in a `switch` — adding a value
to a validator without touching the coercion fixes nothing. Name the exact
sites you changed (file and function) in the handoff.

**`field_type_changed` / `field_required_changed`** — adjust the mapping to the
new shape. If the old type can still appear in existing state files, say what a
plan against old state does after your change; if you cannot tell offline, that
is a handoff item, not a guess.

**`field_removed` / `method_removed` / `method_signature_changed`** — remove or
rewrite the dead mapping. If an attribute loses its API backing entirely, do
NOT silently delete a user-visible attribute: keep it with a deprecation note in
the schema description and flag the removal decision for the reviewer.

**`deprecated` / `doc_changed` / `default_changed`** — read the new doc text in
the SDK source and decide whether the mapping or the resource documentation
must change. Often the whole fix is syncing the lock; the handoff says what you
read and why nothing else moved.

**`group_unlocked` / `model_added` / `model_removed` / `method_added`** —
usually no code change: sync the lock (Finish) and note anything a reviewer
should pick up later. A new *method* on a covered group may deserve its own
resource — name it in the handoff and in `notes:` under `{{GROUP}}` in
`sdk-coverage.yaml`; do not build it here.

## Hard rules

- **Never add or change plan modifiers** (`RequiresReplace`, defaults on
  existing attributes) and never flip an existing attribute's Required/Optional/
  Computed — those are API-behavior judgments no syntax reveals. If one seems
  needed, write it in the handoff for the reviewer.
- Touch only: `latitudesh/{resource,datasource,action}_*.go`,
  `latitudesh/*_test.go`, `latitudesh/provider.go`, `sdk-coverage.yaml`,
  `sdk-fields.lock.yaml` (via the sync command only), `templates/**`,
  `examples/**`, `docs/**` — plus `/tmp/drift-handoff.md`, the one write allowed
  outside the repository. Not `go.mod`, not `cmd/`, not `internal/`, not
  `.github/`.
- Sync the lock **only** with
  `go run ./cmd/sdkcoverage fields -write -group {{GROUP}}` — never hand-edit
  it, and never a full `fields -write`: that would silently accept every other
  group's drift in a PR reviewed for this one.
- Follow the house conventions of the files you edit — nil-check every SDK
  pointer to `types.XNull()`, reuse the existing diagnostics vocabulary, keep
  attribute docs in the schema `Description` fields.
- Every schema change needs test coverage: extend the type's existing
  `*_test.go` (offline tier at minimum — plain `go test ./latitudesh` must stay
  green, no TF_ACC, no token).
- Never add a `go:generate` directive, never reach the network, never write a
  token or key anywhere, never hand-edit `docs/` — schema and template changes
  reach `docs/` only through `go generate ./...`, which you run yourself (see
  Finish).

## Finish: lock, handoff, gate

1. Sync the target lock entries (and ONLY those — the command takes the exact
   comma-separated list): `go run ./cmd/sdkcoverage fields -write -group {{GROUP}}`.
   The lock's git diff is the record of what this PR accepted — every deliberate
   omission must also be explained in `notes:` under its group in
   `sdk-coverage.yaml`. The gate verifies the lock changed nowhere else.
2. Regenerate the docs after any schema or template change:
   `go generate ./...`. Changed schema descriptions rewrite tracked files under
   `docs/` — those regenerated files belong in your change, and the gate fails
   if generation still produces something you did not keep.
3. **Write the handoff block below to `/tmp/drift-handoff.md` as soon as your
   first edit builds, and keep it current.**
4. Run the gate and fix what it reports:

```
scripts/scaffold-validate.sh --mode drift --group {{GROUP}}
```

`PASS` means well-formed: build, vet, offline tests, docs regenerated, zero
remaining drift for `{{GROUP}}`, coverage still reconciles. It does not mean
correct. End your final message with the same block, filled in — write "none"
only where genuinely nothing applies.

```markdown
## Handoff — not verified offline

**Drift rows closed by mapping:** attribute ↔ SDK field, per row
**Drift rows closed by deliberate omission:** which, and the why recorded in notes
**Breaking rows:** what the old mapping did, what the new one does, what happens to existing state
**Enum/coercion sites touched:** file and function, per enum
**Plan-modifier questions for the reviewer:** RequiresReplace / defaults you did NOT add
**State-compatibility assumptions:** …
**Open questions for the reviewer:** …
```
