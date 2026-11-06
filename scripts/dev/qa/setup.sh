#!/usr/bin/env bash
# Installs the browser-QA dependency once, outside the repository:
#   scripts/dev/qa/setup.sh        -> ~/.cache/gk-qa/node_modules/puppeteer-core
# (GK_QA_DIR overrides the folder). Uses the installed Google Chrome; no
# browser download. Pinned version, so every run drives Chrome the same way.
set -euo pipefail
# shellcheck source=scripts/dev/lib.sh
. "$(dirname "$0")/../lib.sh"
use_node
dir="${GK_QA_DIR:-$HOME/.cache/gk-qa}"
mkdir -p "$dir"
[ -f "$dir/package.json" ] || echo '{"name":"gk-qa","private":true}' > "$dir/package.json"
(cd "$dir" && npm install --no-audit --no-fund --save-exact puppeteer-core@25.16.0 >/dev/null)
echo "$dir/node_modules/puppeteer-core"
