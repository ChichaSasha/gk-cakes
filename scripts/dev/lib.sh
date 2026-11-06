# shellcheck shell=bash
# Shared helpers for scripts/dev (sourced, not run). Bash 3.2 compatible (macOS).
# Nothing here prints tokens, passwords or API keys.

GK_REPO="${GK_REPO:-ChichaSasha/gk-cakes}"
# Personal GitHub CLI profile (the project's account), never a work profile.
export GH_CONFIG_DIR="${GH_CONFIG_DIR:-$HOME/.config/gh}"
# Scratch root for review worktrees and QA output (absolute, outside every clone).
GK_SCRATCH="${GK_SCRATCH:-${TMPDIR:-/tmp}}"
GK_SCRATCH="${GK_SCRATCH%/}"

die() { echo "error: $*" >&2; exit 1; }
note() { echo "• $*" >&2; }

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }

# The main checkout this worktree belongs to (the parent of the shared .git).
main_clone() {
	local common
	common="$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null)" || die "run inside a gk-cakes checkout"
	dirname "$common"
}

# Absolute, symlink-resolved form of a directory path that may not exist yet.
abs_path() {
	local dir base
	dir="$(dirname "$1")"; base="$(basename "$1")"
	mkdir -p "$dir"
	echo "$(cd "$dir" && pwd -P)/$base"
}

# Redacts credential-bearing query values and bearer tokens from text on stdin.
redact() {
	perl -pe 's/((?:key|token|apikey|access_token|password)=)[^&" ]+/$1REDACTED/gi; s/(Bearer )[A-Za-z0-9._~+\/-]+=*/$1REDACTED/g'
}

# Node 26 from nvm when present (the repository's pinned runtime).
use_node() {
	local nvm_node="$HOME/.nvm/versions/node/v26.13.0/bin"
	[ -d "$nvm_node" ] && export PATH="$nvm_node:$PATH"
	need node
}
