#!/usr/bin/env bash
#
# scaffold-rm.sh — the ONLY delete capability the scaffolding agent is given.
#
# The agent writes files with Write/MultiEdit, which cannot delete. When it
# writes a Go file to the wrong path (e.g. latitudesh/public_network_mapping.go,
# missing the resource_ prefix) it has no way to undo the mistake: the failed
# 2026-09-07 PublicNetworks run left a stray out-of-scope file that the diff-scope
# gate then hard-rejected, after the agent burned turns fighting a sandbox with no
# rm/mv/git rm. This script closes that trap with a single, tightly-scoped delete.
#
# It is exposed to the agent as `Bash(scripts/scaffold-rm.sh:*)`. That allowlist
# entry is a loose string prefix and cannot confine a path on its own — a bare
# `Bash(rm:*)` (or even `Bash(rm latitudesh/:*)`) would admit `rm latitudesh/../go.mod`,
# `rm latitudesh/*`, or chained commands. Real path confinement has to be a
# validation, so this script IS that validation: it accepts exactly one argument,
# requires it to match an anchored allowlist, and refuses everything else. The
# delete scope is deliberately the SAME shape the diff-scope gate accepts, so the
# agent can only remove a file it would have been allowed to create.
#
# Usage:
#   scripts/scaffold-rm.sh <latitudesh/NAME.go>
#
# Exit codes: 0 removed · 1 refused (out of scope / missing) · 2 usage error.
#
set -euo pipefail

if [ "$#" -ne 1 ]; then
	echo "usage: scaffold-rm.sh <latitudesh/NAME.go>" >&2
	exit 2
fi
p=$1

# Reject nested paths before the regex: in a case pattern '*' spans '/', so this
# catches latitudesh/sub/x.go (and thus any '../' escape) explicitly first.
case "$p" in
latitudesh/*/*)
	echo "refused: nested path '$p' — only flat latitudesh/*.go files may be removed" >&2
	exit 1
	;;
esac

# Anchored allowlist: 'latitudesh/' + one or more [A-Za-z0-9_] + '.go'. The
# character class admits no '.', '/', or whitespace, so '..', absolute paths,
# subdirectories, globs (* ? [ ]), and command-chaining metacharacters are all
# structurally impossible — the string is either a single flat Go file or it is
# rejected.
if ! printf '%s' "$p" | grep -Eq '^latitudesh/[A-Za-z0-9_]+\.go$'; then
	echo "refused: '$p' is not a flat latitudesh/*.go file" >&2
	exit 1
fi

# Must be a real regular file (not a symlink, dir, or already-gone path). The
# stray file is untracked during the agent run — created by Write, never added —
# so plain rm is correct here; git rm would fail on an untracked path. The '--'
# stops the validated name from ever being read as an option.
if [ ! -f "$p" ] || [ -L "$p" ]; then
	echo "refused: '$p' is not an existing regular file" >&2
	exit 1
fi

rm -f -- "$p"
echo "removed $p"
