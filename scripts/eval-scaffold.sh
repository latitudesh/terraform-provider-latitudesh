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
# The cap is runaway protection, not a target: measured runs gate-greened by
# ~50-80 turns and the agent then polishes until killed (it cannot see a turn
# counter), so 200 makes truncation rare while the handoff file (prompt v5)
# makes it harmless. Cost scales with turns — the workflow warns above $6.
# Keep in sync with SCAFFOLD_MAX_TURNS in .github/workflows/sdk-watch.yml.
MAX_TURNS="200"
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

# The eval scores HEAD, so everything it needs must be COMMITTED: the worktree
# carries no uncommitted files. Check the two load-bearing pieces up front and
# say exactly what is missing rather than failing obscurely later.
if ! go run ./cmd/sdkcoverage report -format json >/dev/null 2>&1; then
	echo "HEAD's sdkcoverage has no -format json — the eval runs against HEAD, so run it from a checkout whose HEAD includes the detector JSON work (main after PR #214) with the scaffolding files committed" >&2
	exit 2
fi
if [ ! -f "$PROMPT_TEMPLATE" ]; then
	echo "HEAD does not contain $PROMPT_TEMPLATE — the eval runs against HEAD, so uncommitted scaffolding files are invisible here; commit or merge them first" >&2
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

# Render through the shared script — the same one the scaffold job uses in CI —
# via $ROOT so a renderer fix under review is what actually gets exercised (the
# worktree only carries HEAD). The template itself is read from the worktree.
GROUP="$GROUP" KINDS="$KINDS" TF_NAME="$TF_NAME" METHODS="$METHODS" \
	NOTES="$NOTES" SDK_VERSION="$SDK_VERSION" MAX_TURNS="$MAX_TURNS" \
	"$ROOT/scripts/render-scaffold-prompt.sh" "$PROMPT_TEMPLATE" > "$ART_DIR/prompt.txt"

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
# and only the go/gofmt/jq/validation commands — no gh, no git writes, no arbitrary
# shell. jq is needed by the prompt's step 0 (premise check pipes the JSON report
# through it). The exact flag spellings track the installed claude CLI; adjust here
# if your version differs.
ALLOWED_TOOLS="Read,Glob,Grep,Edit,Write,MultiEdit,Bash(go build:*),Bash(go vet:*),Bash(go test:*),Bash(go doc:*),Bash(gofmt:*),Bash(go generate:*),Bash(go run ./cmd/sdkcoverage:*),Bash(jq:*),Bash(scripts/scaffold-validate.sh:*)"

echo
echo "== running the agent (model=$MODEL, max-turns=$MAX_TURNS) =="
# A stale handoff from a previous run must never score as this run's — the
# prompt has the agent maintain this exact path from its first draft onwards.
rm -f /tmp/scaffold-handoff.md
set +e
# The prompt goes through stdin, never as an argument: it starts with the
# template's `---` frontmatter, and the CLI's option parser rejects a leading
# dash as an unknown flag before print mode ever sees it.
claude -p \
	--model "$MODEL" \
	--max-turns "$MAX_TURNS" \
	--allowedTools "$ALLOWED_TOOLS" \
	--output-format json \
	<"$ART_DIR/prompt.txt" >"$ART_DIR/claude-out.json" 2>"$ART_DIR/claude-err.txt"
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

if [ -f /tmp/scaffold-handoff.md ]; then
	handoff_state="file present ($(wc -c < /tmp/scaffold-handoff.md | tr -d ' ') bytes)"
	cp /tmp/scaffold-handoff.md "$ART_DIR/scaffold-handoff.md"
else
	handoff_state="MISSING — a turn-capped session then ships no handoff at all"
fi

echo
echo "======================= eval result ======================="
echo "  case:      $CASE ($GROUP -> $TF_NAME)"
echo "  model:     $MODEL"
echo "  cost:      \$$cost"
echo "  turns:     $turns"
echo "  agent:     exit $agent_status"
echo "  handoff:   $handoff_state"
if [ "$score" -eq 0 ]; then
	echo "  gate:      PASS"
else
	echo "  gate:      FAIL (exit $score)"
fi
echo "  artifacts: $ART_DIR"
echo "==========================================================="

# The gate is the arbiter here exactly as in the scaffold job: production opens
# the PR whenever the gate passes, whatever the agent's exit status, so scoring
# a stricter bar would block prompt iterations production happily ships. A
# non-zero agent exit still prints loudly — it usually means the turn cap fired,
# which costs the reviewer handoff and flags prompt inefficiency — but it does
# not flip a green gate red. An agent that died early leaves a tree the gate
# fails on its own.
if [ "$agent_status" -ne 0 ] && [ "$score" -eq 0 ]; then
	echo "RESULT: PASS with a caveat — the agent exited $agent_status (turn cap?), so no handoff was emitted; review prompt efficiency before shipping this prompt version"
fi
exit "$score"
