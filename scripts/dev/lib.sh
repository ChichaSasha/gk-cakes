# shellcheck shell=bash
# Shared helpers for scripts/dev (sourced, not run). Bash 3.2 compatible (macOS).
# Nothing here prints tokens, passwords or API keys.

GK_REPO="${GK_REPO:-ChichaSasha/gk-cakes}"
# The personal GitHub CLI profile (the project's account), never a work
# profile: an inherited token or config directory would override it (GK-006 AC2).
if [ -n "${GH_TOKEN:-}${GITHUB_TOKEN:-}${GH_ENTERPRISE_TOKEN:-}" ]; then
	echo "• ignoring an inherited GH_TOKEN/GITHUB_TOKEN: using the personal gh profile" >&2
fi
unset GH_TOKEN GITHUB_TOKEN GH_ENTERPRISE_TOKEN GH_HOST
export GH_CONFIG_DIR="$HOME/.config/gh"
# Scratch root for review worktrees and QA output (absolute, outside every clone).
GK_SCRATCH="${GK_SCRATCH:-${TMPDIR:-/tmp}}"
GK_SCRATCH="${GK_SCRATCH%/}"

die() { echo "error: $*" >&2; exit 1; }

# Prints a script's leading comment block (its usage) and exits 2.
usage_of() { awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$1" >&2; exit 2; }
note() { echo "• $*" >&2; }

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }

# The main checkout this worktree belongs to (the parent of the shared .git).
main_clone() {
	local common
	common="$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null)" || die "run inside a gk-cakes checkout"
	dirname "$common"
}

# The scratch root, absolute and symlink-resolved; it must exist and must not
# be inside any Git working tree (a review worktree there could be committed).
scratch_root() {
	[ -d "$GK_SCRATCH" ] || die "GK_SCRATCH ($GK_SCRATCH) is not a directory"
	local root
	root="$(cd "$GK_SCRATCH" && pwd -P)"
	if git -C "$root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
		die "GK_SCRATCH ($root) is inside a Git working tree; use a folder outside every clone"
	fi
	echo "$root"
}

# A plain label usable as a folder name.
check_label() {
	case "$1" in
		""|*/*|.*|*[!A-Za-z0-9._-]*) die "'$1' must be a plain label (letters, digits, . _ -)" ;;
	esac
}

# Fails unless option $1 has a value ($2 is the remaining argument count).
need_value() { [ "$2" -ge 2 ] || die "$1 needs a value"; }

# Redacts credential-bearing query values and bearer tokens from text on stdin.
redact() {
	perl -pe 's/((?:key|token|apikey|access_token|password)=)[^&" ]+/$1REDACTED/gi; s/(Bearer )[A-Za-z0-9._~+\/-]+=*/$1REDACTED/g'
}

# The Node version pinned in ui/.nvmrc, from nvm when installed.
use_node() {
	local top want nvm_node have
	top="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
	want="$(tr -d 'v \n' < "$top/ui/.nvmrc" 2>/dev/null || true)"
	[ -n "$want" ] || die "ui/.nvmrc is missing"
	nvm_node="$HOME/.nvm/versions/node/v$want/bin"
	[ -d "$nvm_node" ] && export PATH="$nvm_node:$PATH"
	need node
	have="$(node -v | tr -d v)"
	[ "$have" = "$want" ] || die "Node $want is required (ui/.nvmrc), found $have: nvm install $want"
}
