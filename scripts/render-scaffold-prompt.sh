#!/usr/bin/env bash
#
# render-scaffold-prompt.sh — substitute the {{...}} placeholders in the scaffold
# prompt template and print the result. Shared by the eval harness and the
# sdk-watch scaffold job so the agent always receives an identically rendered
# prompt; if the two ever rendered differently, the eval would no longer measure
# what production runs.
#
# Inputs come through the environment (GROUP, KINDS, TF_NAME, METHODS, NOTES,
# SDK_VERSION, and for the drift template DRIFT_JSON, IMPLEMENTED_BY), never
# argv: values are free text from the coverage report and argv would invite
# quoting bugs. The splice uses index/substr rather than gsub: a gsub
# replacement string treats '&' and '\' specially, so a note containing either
# would silently corrupt the rendered prompt.
#
# Usage:
#   GROUP=... KINDS=... TF_NAME=... METHODS=... NOTES=... SDK_VERSION=... \
#     scripts/render-scaffold-prompt.sh [template]
set -euo pipefail

TEMPLATE="${1:-.github/prompts/scaffold-resource.md}"
[ -f "$TEMPLATE" ] || { echo "render-scaffold-prompt.sh: no such template: $TEMPLATE" >&2; exit 2; }

# Require exactly the variables the template references, so the one renderer
# serves both prompt templates without dummy values for the other's tokens —
# and a template gaining a token cannot silently render it empty.
for v in GROUP KINDS TF_NAME METHODS NOTES SDK_VERSION MAX_TURNS DRIFT_JSON IMPLEMENTED_BY; do
	if grep -q "{{$v}}" "$TEMPLATE" && [ -z "${!v:-}" ]; then
		echo "render-scaffold-prompt.sh: $v must be set (and non-empty) in the environment — $TEMPLATE references {{$v}}" >&2
		exit 2
	fi
done

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
		line = subst(line, "{{MAX_TURNS}}", ENVIRON["MAX_TURNS"])
		line = subst(line, "{{DRIFT_JSON}}", ENVIRON["DRIFT_JSON"])
		line = subst(line, "{{IMPLEMENTED_BY}}", ENVIRON["IMPLEMENTED_BY"])
		print line
	}' "$TEMPLATE"
