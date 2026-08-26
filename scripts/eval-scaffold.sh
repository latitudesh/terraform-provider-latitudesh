#!/usr/bin/env bash
#
# eval-scaffold.sh — score the scaffolding agent on a known-good target.
#
# Everything here is offline except the single `claude -p` call. The harness
# ablates an existing resource at HEAD inside a throwaway git worktree (so its SDK
# group reappears as pending), runs the agent with the same prompt, tool allowlist
# and turn cap the workflow will use, and scores the result with
# scripts/scaffold-validate.sh — the identical gate that decides PR creation in
# CI. A green eval means the prompt + gate produce mergeable-shaped output.
#
# Manual and local only: a full run costs roughly US$8–12 and needs the `claude`
# CLI authenticated. It operates on HEAD, so commit your work first.
#
# Convention: any PR that changes the prompt or the validation gate must report a
# fresh eval result (case, PASS/FAIL, cost, turns) in its description.
#
# Usage:
#   scripts/eval-scaffold.sh --case elastic_ip
#   scripts/eval-scaffold.sh --case elastic_ip --dry-run      # ablate + render prompt, no agent
#   scripts/eval-scaffold.sh --case elastic_ip --keep         # leave the worktree for inspection
#   scripts/eval-scaffold.sh --case elastic_ip --model claude-opus-4-8 --max-turns 60
#
# Model policy (DX-23): claude-sonnet-5 by default, claude-opus-4-8 escalation for
# structurally ambiguous deltas.
set -euo pipefail

CASE=""
MODEL="claude-sonnet-5"
MAX_TURNS="50"
DRY_RUN="false"
KEEP="false"

while [ $# -gt 0 ]; do
	case "$1" in
	--case | --model | --max-turns)
		# Explicit check: under set -e a bare `shift 2` with no value would kill
		# the script silently, with no message at all.
		if [ $# -lt 2 ]; then
			echo "missing value for $1" >&2
			exit 2
		fi
		case "$1" in
		--case) CASE="$2" ;;
		--model) MODEL="$2" ;;
		--max-turns) MAX_TURNS="$2" ;;
		esac
		shift 2
		;;
	--dry-run) DRY_RUN="true"; shift ;;
	--keep) KEEP="true"; shift ;;
	*) echo "unknown argument: $1" >&2; exit 2 ;;
	esac
done

if [ -z "$CASE" ]; then
	echo "usage: $0 --case <name> [--dry-run] [--keep] [--model id] [--max-turns n]" >&2
	exit 2
fi

ROOT=$(git rev-parse --show-toplevel)
cd "$ROOT"

CASE_DIR="eval/cases/$CASE"
[ -d "$CASE_DIR" ] || { echo "no such eval case: $CASE_DIR" >&2; exit 2; }
[ -f "$CASE_DIR/ablate.patch" ] || { echo "missing $CASE_DIR/ablate.patch" >&2; exit 2; }
[ -f "$CASE_DIR/meta.env" ] || { echo "missing $CASE_DIR/meta.env" >&2; exit 2; }

# shellcheck disable=SC1090
. "$CASE_DIR/meta.env"
[ -n "${GROUP:-}" ] || { echo "$CASE_DIR/meta.env must set GROUP" >&2; exit 2; }
# A case may pin KINDS to a subset of what the report derives — the elastic_ip
# fixture restores only the resource that ever existed, so scoring the report's
# full resource+datasource list would fail every run by construction.
META_KINDS="${KINDS:-}"

command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }
if [ "$DRY_RUN" != "true" ]; then
	command -v claude >/dev/null || { echo "the claude CLI is required (or use --dry-run)" >&2; exit 2; }
fi

PROMPT_TEMPLATE=".github/prompts/scaffold-resource.md"
[ -f "$PROMPT_TEMPLATE" ] || { echo "missing $PROMPT_TEMPLATE" >&2; exit 2; }

# Harness artifacts (rendered prompt, agent transcript) live OUTSIDE the
# worktree on purpose: the validation gate scans untracked files too, so
# anything the harness dropped inside the tree would fail the diff-scope check
# on every single run and no eval could ever pass.
RUN_DIR=$(mktemp -d)
WORKTREE="$RUN_DIR/eval-$CASE"
ART_DIR="$RUN_DIR/artifacts"
mkdir -p "$ART_DIR"
cleanup() {
	if [ "$KEEP" = "true" ]; then
		echo "worktree kept at: $WORKTREE"
		return
	fi
	git worktree remove --force "$WORKTREE" >/dev/null 2>&1 || true
	git worktree prune >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "== preparing an ablated worktree at HEAD =="
git worktree add -q "$WORKTREE" HEAD
cd "$WORKTREE"

# The worktree must carry the JSON report support this harness depends on. If it
# predates that, the checkout is too old — bail with a clear message rather than
# failing obscurely later.
if ! go run ./cmd/sdkcoverage report -format json >/dev/null 2>&1; then
	echo "this checkout's sdkcoverage has no -format json; run the eval from a branch that includes the detector JSON work" >&2
	exit 2
fi

echo "== ablating $CASE ($GROUP) =="
git apply "$ROOT/$CASE_DIR/ablate.patch"
git add -A
git commit -q -m "eval: ablate $CASE"

# Confirm the ablation actually put the group back in the pending queue; if not,
# the fixture is stale against the current SDK/provider and scoring would be
# meaningless.
report=$(go run ./cmd/sdkcoverage report -format json)
group_json=$(echo "$report" | jq -c --arg g "$GROUP" '.pending[] | select(.group==$g)')
if [ -z "$group_json" ]; then
	echo "ablation did not surface $GROUP in the pending queue — the fixture is stale" >&2
	exit 1
fi

TF_NAME=$(echo "$group_json" | jq -r '.suggested_type_name')
KINDS=$(echo "$group_json" | jq -r '.generates | join(", ")')
if [ -n "$META_KINDS" ]; then
	KINDS="$META_KINDS"
fi
METHODS=$(echo "$group_json" | jq -r '.methods | join(", ")')
NOTES=$(echo "$group_json" | jq -r 'if (.notes // "") == "" then "none" else (.notes | gsub("\\s+";" ")) end')
SDK_VERSION=$(echo "$report" | jq -r '.sdk_version')

echo "  group=$GROUP  type=$TF_NAME  kinds=[$KINDS]"

# Render the prompt by substituting placeholders. Values go through the
# environment, and the splice uses index/substr rather than gsub: a gsub
# replacement string treats '&' and '\' specially, so a note containing either
# would silently corrupt the rendered prompt.
render_prompt() {
	GROUP="$GROUP" KINDS="$KINDS" TF_NAME="$TF_NAME" METHODS="$METHODS" \
		NOTES="$NOTES" SDK_VERSION="$SDK_VERSION" \
		awk '
		function subst(line, token, value,    out, i) {
			out = ""
			while ((i = index(line, token)) > 0) {
				out = out substr(line, 1, i - 1) value
				line = substr(line, i + length(token))
			}
			return out line
		}
		{
			line = $0
			line = subst(line, "{{GROUP}}", ENVIRON["GROUP"])
			line = subst(line, "{{KINDS}}", ENVIRON["KINDS"])
			line = subst(line, "{{TF_NAME}}", ENVIRON["TF_NAME"])
			line = subst(line, "{{METHODS}}", ENVIRON["METHODS"])
			line = subst(line, "{{NOTES}}", ENVIRON["NOTES"])
			line = subst(line, "{{SDK_VERSION}}", ENVIRON["SDK_VERSION"])
			print line
		}' "$PROMPT_TEMPLATE"
}
render_prompt > "$ART_DIR/prompt.txt"

if [ "$DRY_RUN" = "true" ]; then
	echo
	echo "== rendered prompt (dry run — agent NOT invoked) =="
	cat "$ART_DIR/prompt.txt"
	echo
	echo "worktree: $WORKTREE"
	echo "artifacts: $ART_DIR"
	exit 0
fi

# The tool allowlist mirrors what the scaffold job will grant: read/edit the tree,
# and only the go/gofmt/validation commands — no gh, no git writes, no arbitrary
# shell. The exact flag spellings track the installed claude CLI; adjust here if
# your version differs.
ALLOWED_TOOLS="Read,Glob,Grep,Edit,Write,MultiEdit,Bash(go build:*),Bash(go vet:*),Bash(go test:*),Bash(go doc:*),Bash(gofmt:*),Bash(go generate:*),Bash(go run ./cmd/sdkcoverage:*),Bash(scripts/scaffold-validate.sh:*)"

echo
echo "== running the agent (model=$MODEL, max-turns=$MAX_TURNS) =="
set +e
claude -p "$(cat "$ART_DIR/prompt.txt")" \
	--model "$MODEL" \
	--max-turns "$MAX_TURNS" \
	--allowedTools "$ALLOWED_TOOLS" \
	--output-format json >"$ART_DIR/claude-out.json" 2>"$ART_DIR/claude-err.txt"
agent_status=$?
set -e

if [ "$agent_status" -ne 0 ]; then
	echo "the agent exited non-zero ($agent_status); see $ART_DIR/claude-err.txt" >&2
fi

cost=$(jq -r '.total_cost_usd // .cost_usd // "unknown"' "$ART_DIR/claude-out.json" 2>/dev/null || echo unknown)
turns=$(jq -r '.num_turns // "unknown"' "$ART_DIR/claude-out.json" 2>/dev/null || echo unknown)

echo
echo "== scoring with the shared validation gate =="
set +e
bash scripts/scaffold-validate.sh --group "$GROUP" --type-name "$TF_NAME" --kinds "$KINDS" --base HEAD
score=$?
set -e

echo
echo "======================= eval result ======================="
echo "  case:      $CASE ($GROUP -> $TF_NAME)"
echo "  model:     $MODEL"
echo "  cost:      \$$cost"
echo "  turns:     $turns"
echo "  agent:     exit $agent_status"
if [ "$score" -eq 0 ]; then
	echo "  gate:      PASS"
else
	echo "  gate:      FAIL (exit $score)"
fi
echo "  artifacts: $ART_DIR"
echo "==========================================================="

# A crashed agent is a failed eval even when the tree it left behind happens to
# validate — otherwise a prompt change that makes the agent die at the finish
# line would still score green.
if [ "$agent_status" -ne 0 ]; then
	echo "RESULT: FAIL — the agent process exited $agent_status (gate outcome above is informational)"
	exit 1
fi
exit "$score"
