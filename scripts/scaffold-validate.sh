#!/usr/bin/env bash
#
# scaffold-validate.sh — the correctness gate for generated provider scaffolding.
#
# This one script is the real check that a scaffolded resource/data source is
# sound. It is shared literally by three callers so they cannot drift apart:
#
#   - the eval harness (scripts/eval-scaffold.sh), scoring a local agent run;
#   - the scaffolding agent itself, which must run it before finishing;
#   - the sdk-watch workflow, which gates PR creation on it.
#
# It runs entirely offline (no TF_ACC, no API token, no network beyond the module
# cache) and is deliberately strict: it decides whether generated code is even
# allowed to become a draft PR, so a soft pass here is worse than a hard fail.
#
# Usage:
#   scripts/scaffold-validate.sh --group <SDKGroup> --type-name <latitudesh_x> --kinds <resource,datasource> [--base <ref>]
#   scripts/scaffold-validate.sh --mode drift --group <SDKGroup>[,<SDKGroup>...] [--base <ref>]
#
#   --group       the SDK service group that was scaffolded (e.g. PublicNetworks).
#                 In drift mode this may be a comma-separated list: a drift PR
#                 that bumps the SDK must resolve EVERY breaking-drifted group in
#                 one change, or the gate (which checks the whole surface) could
#                 never go green for any of them.
#   --type-name   the Terraform type name that was added (e.g. latitudesh_public_network)
#   --kinds       comma-separated kinds that were REQUESTED (resource, datasource,
#                 action). Required: coverage alone cannot express this — one
#                 implemented kind marks the whole group covered, so a datasource
#                 the shape asked for would otherwise go silently undelivered,
#                 and the group never returns to the pending queue to flag it.
#   --mode        scaffold (default) gates a NEW type; drift gates a change that
#                 reconciles an EXISTING covered group with SDK field drift. In
#                 drift mode --type-name/--kinds do not apply: success is "zero
#                 remaining drift for the group, lock synced, own tests present",
#                 not "a new type was delivered".
#   --base        git ref the change is measured against (default: HEAD)
#
set -euo pipefail

GROUP=""
TYPE_NAME=""
KINDS=""
MODE="scaffold"
BASE="HEAD"

while [ $# -gt 0 ]; do
	case "$1" in
	--group | --type-name | --kinds | --mode | --base)
		# Explicit check: under set -e a bare `shift 2` with no value would kill
		# the script silently, with no message and a misleading exit code.
		if [ $# -lt 2 ]; then
			echo "missing value for $1" >&2
			exit 2
		fi
		case "$1" in
		--group) GROUP="$2" ;;
		--type-name) TYPE_NAME="$2" ;;
		--kinds) KINDS="$2" ;;
		--mode) MODE="$2" ;;
		--base) BASE="$2" ;;
		esac
		shift 2
		;;
	*)
		echo "unknown argument: $1" >&2
		exit 2
		;;
	esac
done

case "$MODE" in
scaffold)
	if [ -z "$GROUP" ] || [ -z "$TYPE_NAME" ] || [ -z "$KINDS" ]; then
		echo "usage: $0 --group <SDKGroup> --type-name <latitudesh_x> --kinds <resource,datasource> [--base <ref>]" >&2
		exit 2
	fi
	;;
drift)
	if [ -z "$GROUP" ]; then
		echo "usage: $0 --mode drift --group <SDKGroup> [--base <ref>]" >&2
		exit 2
	fi
	if [ -n "$TYPE_NAME" ] || [ -n "$KINDS" ]; then
		echo "--type-name/--kinds do not apply in drift mode" >&2
		exit 2
	fi
	;;
*)
	echo "unknown mode $MODE (want scaffold or drift)" >&2
	exit 2
	;;
esac

# Normalize the kinds list ("resource, datasource" and "resource,datasource"
# both arrive here — the report joins with a comma and a space).
kinds=()
if [ "$MODE" = "scaffold" ]; then
	for kind in $(printf '%s' "$KINDS" | tr ',' ' '); do
		case "$kind" in
		resource | datasource | action) kinds+=("$kind") ;;
		*)
			echo "unknown kind $kind (want resource, datasource, or action)" >&2
			exit 2
			;;
		esac
	done
	if [ "${#kinds[@]}" -eq 0 ]; then
		echo "--kinds lists no kinds" >&2
		exit 2
	fi
fi

ROOT=$(git rev-parse --show-toplevel)
cd "$ROOT"

# short is the type name without the provider prefix, used to locate the doc
# template and the example file (templates/<kind>/<short>.md.tmpl,
# examples/<type-name>.tf).
short="${TYPE_NAME#latitudesh_}"

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

step() { echo; echo "== $* =="; }

# changedPaths lists every path that differs from BASE, including untracked new
# files (a scaffolded resource is a brand-new file, so a tracked-only diff would
# miss it) and deletions (removing an out-of-scope file — a CI config, a test
# helper — is just as much an out-of-scope change as editing one, so filtering
# deletions out would let it pass unseen). Renames surface as an add + a delete.
changedPaths() {
	{
		git diff --name-only "$BASE" -- .
		git ls-files --others --exclude-standard
	} | sort -u
}

# --------------------------------------------------------------- 1. diff scope --
# The agent runs with a write-capable token downstream, so the first thing to
# prove is that it only touched paths a resource is allowed to touch. Anything
# outside the allowlist — CI config, tooling, go.mod, internal packages — is a
# hard stop, whatever else passes.
step "1/9 diff scope"
# Read into an array with a portable loop rather than mapfile: this gate also
# runs on macOS, whose default bash is 3.2 and has no mapfile.
paths=()
while IFS= read -r p; do
	[ -n "$p" ] && paths+=("$p")
done < <(changedPaths)
if [ "${#paths[@]}" -eq 0 ]; then
	fail "no changes detected against $BASE — nothing was scaffolded"
fi
for p in "${paths[@]}"; do
	case "$p" in
	# In a case pattern '*' spans '/', so latitudesh/resource_*.go alone would
	# also admit latitudesh/resource_x/anything.go — a nested package that go
	# build compiles but go test ./latitudesh never runs. Reject depth first.
	latitudesh/*/*)
		fail "unexpected nested path under latitudesh/: $p (provider Go files live flat in latitudesh/)"
		;;
	latitudesh/resource_*.go | latitudesh/datasource_*.go | latitudesh/action_*.go | latitudesh/*_test.go)
		: ;;
	latitudesh/provider.go)
		: ;;
	sdk-coverage.yaml | sdk-fields.lock.yaml)
		: ;;
	# The first pattern of each pair already admits the subdirectories ('*'
	# spans '/'), but the explicit variants spell the intent out — reviewers
	# keep misreading the bare glob as slash-bounded.
	templates/*.tmpl | templates/resources/*.tmpl | templates/data-sources/*.tmpl | templates/actions/*.tmpl)
		: ;;
	examples/*.tf | examples/actions/*.tf)
		: ;;
	docs/*.md | docs/resources/*.md | docs/data-sources/*.md | docs/actions/*.md)
		: ;;
	*)
		fail "out-of-scope change: $p (scaffolding may only touch resource/datasource/action Go files, tests, provider.go, sdk-coverage.yaml, templates/, examples/, docs/)"
		;;
	esac
done
echo "ok: ${#paths[@]} changed path(s), all in scope"

# ------------------------------------------------ 2. forbidden content in diff --
# Two things must never appear in scaffolded code: a go:generate directive (it
# would run arbitrary code in a privileged context the next time anyone
# generates), and anything resembling key material (a token or private key
# committed by mistake). Grep the changed files' contents, not just the diff, so
# a brand-new untracked file is covered too.
step "2/9 forbidden content"
for p in "${paths[@]}"; do
	[ -f "$p" ] || continue
	if grep -nE '^[[:space:]]*//[[:space:]]*go:generate' "$p" >/dev/null 2>&1; then
		fail "$p introduces a go:generate directive — not allowed in scaffolded code"
	fi
	if grep -nE '(-----BEGIN[[:space:]]+[A-Z ]*PRIVATE KEY-----|ANTHROPIC_API_KEY|sk-ant-|LATITUDESH_AUTH_TOKEN[[:space:]]*=)' "$p" >/dev/null 2>&1; then
		fail "$p contains what looks like key material — remove it"
	fi
done
echo "ok: no go:generate directives, no key material"

# ----------------------------------------------------------------- 3. gofmt --
step "3/9 gofmt"
unformatted=$(gofmt -l latitudesh internal cmd main.go 2>/dev/null || true)
if [ -n "$unformatted" ]; then
	fail "gofmt would rewrite:"$'\n'"$unformatted"
fi
echo "ok: gofmt clean"

# ------------------------------------------------------------------- 4. vet --
step "4/9 go vet"
go vet ./... || fail "go vet reported problems"
echo "ok: go vet clean"

# ----------------------------------------------------------------- 5. build --
step "5/9 go build"
go build ./... || fail "go build failed"
echo "ok: builds"

# ------------------------------------------------------------------ 6. tests --
# The offline unit tests, which include TestProviderSDKCoverage. TF_ACC is
# explicitly unset so no acceptance test tries to reach the live API, and so is
# LATITUDESH_AUTH_TOKEN: a stray token in the caller's shell would let a test
# that eagerly touches the shared fixture reach the real API from what is
# supposed to be the offline tier — the gate must behave identically on a dev
# machine and in CI.
step "6/9 unit tests"
env -u TF_ACC -u LATITUDESH_AUTH_TOKEN go test ./latitudesh ./internal/... || fail "unit tests failed"
echo "ok: unit tests pass"

# ------------------------------------------------------- 7. coverage reconciles --
# The gate itself: after scaffolding, the SDK/manifest/provider must agree, and
# the target group must have moved out of the pending queue (i.e. it is now
# covered). A group still pending means the resource was written but never
# registered or never recorded in sdk-coverage.yaml.
step "7/9 coverage reconciles"
# check covers both ledgers: group-level contradictions AND breaking field
# drift against sdk-fields.lock.yaml.
go run ./cmd/sdkcoverage check || fail "sdkcoverage check found contradictions"
if [ "$MODE" = "scaffold" ]; then
	report=$(go run ./cmd/sdkcoverage report -format json)
	if ! echo "$report" | jq -e --arg g "$GROUP" '.pending | map(.group) | index($g) | not' >/dev/null; then
		fail "group $GROUP is still pending — the resource is not registered or not recorded in sdk-coverage.yaml"
	fi
	echo "ok: coverage reconciles and $GROUP is covered"
else
	# The lock must still exist and actually load: deleting it would make every
	# drift check downstream an inert no-op that reads as success.
	[ -f sdk-fields.lock.yaml ] || fail "sdk-fields.lock.yaml is gone — a drift change regenerates the lock, never deletes it"

	# Drift mode's success criterion: the target groups no longer drift AT ALL —
	# every row was either mapped or accepted into the lock. A leftover
	# informational row would pass `check` (breaking-only) while leaving the
	# tracking issue reporting the very drift this PR claims to close.
	drift=$(go run ./cmd/sdkcoverage drift -format json)
	if [ "$(echo "$drift" | jq -r '.lock_missing')" = "true" ]; then
		fail "the drift report cannot see the lock — sdk-fields.lock.yaml is missing or unreadable"
	fi
	groups_json=$(printf '%s' "$GROUP" | jq -R 'split(",") | map(gsub("^\\s+|\\s+$"; ""))')
	remaining=$(echo "$drift" | jq --argjson gs "$groups_json" '[.drift[] | select(.group as $g | $gs | index($g))] | length')
	if [ "$remaining" -ne 0 ]; then
		echo "$drift" | jq -r --argjson gs "$groups_json" '.drift[] | select(.group as $g | $gs | index($g)) | "  - [\(.kind)] \(.group) \(.model) \(.field): \(.detail)"' >&2
		fail "target group(s) still have $remaining drift row(s) — map each one or accept it via 'go run ./cmd/sdkcoverage fields -write -group $GROUP'"
	fi
	echo "ok: coverage reconciles and $GROUP has zero remaining drift"
fi

# ------------------------------------------------------------ 8. docs reproduce --
# docs/ is generated from templates/ plus the schema. A hand-edit to docs/ is
# silently lost on the next generate, so the docs in the change must be exactly
# what generation produces.
step "8/9 docs reproduce"

# docsState fingerprints the full content of docs/, tracked or not — git diff
# alone cannot express "generation changed nothing", because it compares
# against the index, not against the tree generation just ran on.
docsState() {
	find docs -type f | LC_ALL=C sort | xargs git hash-object | git hash-object --stdin
}

docs_before=$(docsState)
# Capture output and replay it on failure: tfplugindocs fails loudly for real
# reasons (a missing example file, a bad template) and swallowing that turns an
# actionable error into a bare "go generate failed".
if ! generate_out=$(go generate ./... 2>&1); then
	printf '%s\n' "$generate_out" >&2
	fail "go generate failed"
fi
if [ "$MODE" = "drift" ]; then
	# A drift change edits an EXISTING resource's schema, which legitimately
	# rewrites tracked docs — so comparing docs/ against the committed state
	# (the scaffold branch below) could never pass here, no matter how correct
	# the change. What must hold instead is IDEMPOTENCE: the gate's own
	# generate run changes nothing further. Stale docs (schema changed, docs
	# never regenerated) and hand-edited docs both fail — and both are fixed by
	# running `go generate ./...` and re-running this gate.
	if [ "$docs_before" != "$(docsState)" ]; then
		git --no-pager diff --stat -- docs/ >&2
		fail "docs/ was not regenerated after the mapping change — run 'go generate ./...' and keep the updated docs in the change"
	fi
else
	if ! git diff --quiet -- docs/; then
		git --no-pager diff --stat -- docs/ >&2
		fail "docs/ is out of sync with templates/ — edit the template, not the rendered doc, then regenerate"
	fi
fi
echo "ok: docs regenerate with no drift"

# -------------------------------------------------- 9. deliverables per kind --
# Drift mode first: its deliverables are different in kind, not in degree. The
# lock must be part of the change (regenerating it IS the act of acceptance the
# whole pipeline hinges on), and a mapping change must bring its own offline
# test — but there is no new type, so nothing below about registration,
# templates, or examples applies.
if [ "$MODE" = "drift" ]; then
	step "9/9 drift deliverables"

	lock_changed=false
	code_changed=false
	test_files=()
	for p in "${paths[@]}"; do
		case "$p" in
		sdk-fields.lock.yaml) lock_changed=true ;;
		latitudesh/*_test.go) [ -f "$p" ] && test_files+=("$p") ;;
		latitudesh/*.go) code_changed=true ;;
		esac
	done

	if [ "$lock_changed" != "true" ]; then
		fail "sdk-fields.lock.yaml did not change — a drift PR must accept the drift it closes (go run ./cmd/sdkcoverage fields -write -group $GROUP)"
	fi

	# The lock diff must be scoped to the target groups: a full regenerate here
	# would silently accept every OTHER group's drift inside a PR reviewed for
	# these. Reconstruct the only acceptable lock — BASE's lock with exactly the
	# target groups resynced — and require byte equality.
	base_lock=$(mktemp)
	expected_lock=$(mktemp)
	if ! git show "$BASE:sdk-fields.lock.yaml" > "$base_lock" 2>/dev/null; then
		rm -f "$base_lock" "$expected_lock"
		fail "drift mode needs sdk-fields.lock.yaml to exist at $BASE — seeding the lock is a full 'fields -write', not a drift PR"
	fi
	if ! go run ./cmd/sdkcoverage fields -lock "$base_lock" -group "$GROUP" > "$expected_lock"; then
		rm -f "$base_lock" "$expected_lock"
		fail "could not compute the expected lock for $GROUP"
	fi
	if ! cmp -s "$expected_lock" sdk-fields.lock.yaml; then
		diff "$expected_lock" sdk-fields.lock.yaml | head -20 >&2
		rm -f "$base_lock" "$expected_lock"
		fail "sdk-fields.lock.yaml changed outside the target group(s) $GROUP — regenerate it with 'go run ./cmd/sdkcoverage fields -write -group $GROUP' only"
	fi
	rm -f "$base_lock" "$expected_lock"

	if [ "$code_changed" = "true" ]; then
		if [ "${#test_files[@]}" -eq 0 ]; then
			fail "the mapping changed but no latitudesh/*_test.go did — a schema or mapping change must bring its own test"
		fi
		has_offline=false
		for f in "${test_files[@]}"; do
			total=$(grep -cE '^func Test' "$f" || true)
			acc=$(grep -cE '^func TestAcc' "$f" || true)
			if [ "${total:-0}" -gt "${acc:-0}" ]; then
				has_offline=true
			fi
		done
		[ "$has_offline" = "true" ] || fail "no offline (non-TestAcc) test in the changed test files — nothing about the mapping change runs on ordinary PRs"
	fi

	echo "ok: lock accepted the drift$([ "$code_changed" = "true" ] && echo ", mapping change ships its own offline test")"
	echo
	echo "PASS: $GROUP — field drift reconciles"
	exit 0
fi

# Coverage (step 7) only proves SOME implementation claimed the group. This step
# holds the scaffold to what was actually requested: every asked-for kind is
# actually registered and ships its Go file and doc template, an example exists,
# and the change carries the new type's own tests in both categories — a passing
# package-wide `go test` says nothing about whether the NEW type has any test.
step "9/9 deliverables per kind"

# Registration first, from the provider's own runtime metadata. A file that
# exists but was never added to provider.go passes every other check: the build
# compiles it, and coverage (step 7) cannot see the gap because the shared type
# name is already claimed by a sibling kind (ssh_key ships as both kinds).
shipped=$(go run ./cmd/sdkcoverage shipped) || fail "could not introspect the provider's registered types"
for kind in "${kinds[@]}"; do
	case "$kind" in
	resource) field="resources" registered_in="Resources()" ;;
	datasource) field="datasources" registered_in="DataSources()" ;;
	action) field="actions" registered_in="Actions()" ;;
	esac
	echo "$shipped" | jq -e --arg k "$field" --arg t "$TYPE_NAME" '.[$k] | index($t) != null' >/dev/null ||
		fail "requested kind $kind: $TYPE_NAME is not registered in provider.go ($registered_in)"
done

for kind in "${kinds[@]}"; do
	case "$kind" in
	resource)
		[ -f "latitudesh/resource_${short}.go" ] || fail "requested kind resource: missing latitudesh/resource_${short}.go"
		[ -f "templates/resources/${short}.md.tmpl" ] || fail "requested kind resource: missing templates/resources/${short}.md.tmpl"
		;;
	datasource)
		[ -f "latitudesh/datasource_${short}.go" ] || fail "requested kind datasource: missing latitudesh/datasource_${short}.go"
		[ -f "templates/data-sources/${short}.md.tmpl" ] || fail "requested kind datasource: missing templates/data-sources/${short}.md.tmpl"
		;;
	action)
		[ -f "latitudesh/action_${short}.go" ] || fail "requested kind action: missing latitudesh/action_${short}.go"
		[ -f "templates/actions/${short}.md.tmpl" ] || fail "requested kind action: missing templates/actions/${short}.md.tmpl"
		;;
	esac
done

# Resources and data sources keep a flat example; actions follow tfplugindocs'
# examples/actions/<type>/action.tf layout (see latitudesh_server_reinstall).
case " ${kinds[*]} " in
*" resource "* | *" datasource "*)
	[ -f "examples/${TYPE_NAME}.tf" ] || fail "missing examples/${TYPE_NAME}.tf"
	# The example must be SELF-CONTAINED: the PR's manual-validation snippet
	# copies it alone into a scratch directory, so any reference satisfied only
	# by a sibling file — a var.*, another example's resource, a local, a
	# module — turns the advertised `terraform plan` into an undeclared-
	# reference error instead of exercising the provider. Validate it exactly
	# the way the snippet consumes it: alone, against this very build, through
	# a throwaway dev_overrides. No init, no credentials, no network.
	command -v terraform >/dev/null || fail "the terraform CLI is required: the gate validates examples/${TYPE_NAME}.tf standalone"
	tfscratch=$(mktemp -d)
	go build -o "$tfscratch/terraform-provider-latitudesh" . ||
		{ rm -rf "$tfscratch"; fail "could not build the provider binary for standalone example validation"; }
	cat >"$tfscratch/dev.tfrc" <<-TFRC
		provider_installation {
		  dev_overrides {
		    "latitudesh/latitudesh" = "$tfscratch"
		  }
		  direct {}
		}
	TFRC
	mkdir "$tfscratch/example"
	cp "examples/${TYPE_NAME}.tf" "$tfscratch/example/main.tf"
	cat >"$tfscratch/example/provider.tf" <<-'TFPROVIDER'
		terraform {
		  required_providers {
		    latitudesh = { source = "latitudesh/latitudesh" }
		  }
		}
	TFPROVIDER
	if ! (cd "$tfscratch/example" && TF_CLI_CONFIG_FILE="$tfscratch/dev.tfrc" terraform validate -no-color); then
		rm -rf "$tfscratch"
		fail "examples/${TYPE_NAME}.tf does not validate standalone — it must be self-contained: no var.*, no references to resources declared elsewhere; declare helper resources in the same file"
	fi
	rm -rf "$tfscratch"
	;;
esac
case " ${kinds[*]} " in
*" action "*)
	[ -f "examples/actions/${TYPE_NAME}/action.tf" ] || fail "missing examples/actions/${TYPE_NAME}/action.tf"
	;;
esac

# Test deliverables: the change must bring the NEW TYPE's own tests — at least
# one changed test file whose name carries the type (house convention:
# <kind>_<short>[_suffix]_test.go), with at least one TestAcc (compiles now,
# recorded by a human later) and at least one offline test (runs on every PR).
# Scoping to the target matters: counting categories across every changed test
# file would let an edit to some unrelated, already-tested file satisfy the
# check while the new type ships with no tests at all.
test_files=()
for p in "${paths[@]}"; do
	case "$p" in
	latitudesh/*"${short}"*_test.go) [ -f "$p" ] && test_files+=("$p") ;;
	esac
done
if [ "${#test_files[@]}" -eq 0 ]; then
	fail "no changed test file targets ${short} — the new type must bring its own tests (latitudesh/<kind>_${short}*_test.go), not lean on unrelated ones"
fi
has_acc=false
has_offline=false
for f in "${test_files[@]}"; do
	# Counted rather than piped: under pipefail, `grep | grep -q` can report a
	# false miss when the reader exits early and the writer dies on SIGPIPE.
	total=$(grep -cE '^func Test' "$f" || true)
	acc=$(grep -cE '^func TestAcc' "$f" || true)
	if [ "${acc:-0}" -gt 0 ]; then
		has_acc=true
	fi
	if [ "${total:-0}" -gt "${acc:-0}" ]; then
		has_offline=true
	fi
done
[ "$has_acc" = "true" ] || fail "no TestAcc function in the target's changed test files — the acceptance path would be unrecordable"
[ "$has_offline" = "true" ] || fail "no offline (non-TestAcc) test in the target's changed test files — nothing about the new type runs on ordinary PRs"

echo "ok: every requested kind (${kinds[*]}) registered and shipped with example, templates, and the target's own tests in both categories"

echo
echo "PASS: $TYPE_NAME ($GROUP) — scaffolding validates"
