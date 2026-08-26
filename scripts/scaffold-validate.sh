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
#
#   --group       the SDK service group that was scaffolded (e.g. PublicNetworks)
#   --type-name   the Terraform type name that was added (e.g. latitudesh_public_network)
#   --kinds       comma-separated kinds that were REQUESTED (resource, datasource,
#                 action). Required: coverage alone cannot express this — one
#                 implemented kind marks the whole group covered, so a datasource
#                 the shape asked for would otherwise go silently undelivered,
#                 and the group never returns to the pending queue to flag it.
#   --base        git ref the change is measured against (default: HEAD)
#
set -euo pipefail

GROUP=""
TYPE_NAME=""
KINDS=""
BASE="HEAD"

while [ $# -gt 0 ]; do
	case "$1" in
	--group | --type-name | --kinds | --base)
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

if [ -z "$GROUP" ] || [ -z "$TYPE_NAME" ] || [ -z "$KINDS" ]; then
	echo "usage: $0 --group <SDKGroup> --type-name <latitudesh_x> --kinds <resource,datasource> [--base <ref>]" >&2
	exit 2
fi

# Normalize the kinds list ("resource, datasource" and "resource,datasource"
# both arrive here — the report joins with a comma and a space).
kinds=()
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
	sdk-coverage.yaml)
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
# explicitly unset so no acceptance test tries to reach the live API.
step "6/9 unit tests"
env -u TF_ACC go test ./latitudesh ./internal/... || fail "unit tests failed"
echo "ok: unit tests pass"

# ------------------------------------------------------- 7. coverage reconciles --
# The gate itself: after scaffolding, the SDK/manifest/provider must agree, and
# the target group must have moved out of the pending queue (i.e. it is now
# covered). A group still pending means the resource was written but never
# registered or never recorded in sdk-coverage.yaml.
step "7/9 coverage reconciles"
go run ./cmd/sdkcoverage check || fail "sdkcoverage check found contradictions"
report=$(go run ./cmd/sdkcoverage report -format json)
if ! echo "$report" | jq -e --arg g "$GROUP" '.pending | map(.group) | index($g) | not' >/dev/null; then
	fail "group $GROUP is still pending — the resource is not registered or not recorded in sdk-coverage.yaml"
fi
echo "ok: coverage reconciles and $GROUP is covered"

# ------------------------------------------------------------ 8. docs reproduce --
# docs/ is generated from templates/ plus the schema. A hand-edit to docs/ is
# silently lost on the next generate, so the committed docs must be exactly what
# generation produces.
step "8/9 docs reproduce"
# Capture output and replay it on failure: tfplugindocs fails loudly for real
# reasons (a missing example file, a bad template) and swallowing that turns an
# actionable error into a bare "go generate failed".
if ! generate_out=$(go generate ./... 2>&1); then
	printf '%s\n' "$generate_out" >&2
	fail "go generate failed"
fi
if ! git diff --quiet -- docs/; then
	git --no-pager diff --stat -- docs/ >&2
	fail "docs/ is out of sync with templates/ — edit the template, not the rendered doc, then regenerate"
fi
echo "ok: docs regenerate with no drift"

# -------------------------------------------------- 9. deliverables per kind --
# Coverage (step 7) only proves SOME implementation claimed the group. This step
# holds the scaffold to what was actually requested: every asked-for kind ships
# its Go file and doc template, an example exists, and the change carries both
# test categories the prompt demands — a passing package-wide `go test` says
# nothing about whether the NEW type has any test at all.
step "9/9 deliverables per kind"
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
	;;
esac
case " ${kinds[*]} " in
*" action "*)
	[ -f "examples/actions/${TYPE_NAME}/action.tf" ] || fail "missing examples/actions/${TYPE_NAME}/action.tf"
	;;
esac

# Test deliverables: the change must bring its own tests — at least one changed
# test file in the provider package, with at least one TestAcc (compiles now,
# recorded by a human later) and at least one offline test (runs on every PR).
test_files=()
for p in "${paths[@]}"; do
	case "$p" in
	latitudesh/*_test.go) [ -f "$p" ] && test_files+=("$p") ;;
	esac
done
if [ "${#test_files[@]}" -eq 0 ]; then
	fail "the change adds or modifies no latitudesh/*_test.go — a scaffold without tests is not reviewable"
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
[ "$has_acc" = "true" ] || fail "no TestAcc function in the changed test files — the acceptance path would be unrecordable"
[ "$has_offline" = "true" ] || fail "no offline (non-TestAcc) test in the changed test files — nothing about the new type runs on ordinary PRs"

echo "ok: every requested kind (${kinds[*]}) shipped with example, templates, and both test categories"

echo
echo "PASS: $TYPE_NAME ($GROUP) — scaffolding validates"
