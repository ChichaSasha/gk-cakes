#!/bin/sh
# GK-018 r04-claims screenshots (proposal, not approved). Serve the chef-operations folder first:
#   python3 -m http.server 4196 --bind 127.0.0.1 --directory docs/design/sessions/chef-operations
# shot.html frames poc/<page> at an exact width (headless Chrome's window is at least 500 px wide).
# Usage: sh shoot.sh [state-filter] — desktop is laid out at 1440 px and saved at 1120 px; writes screenshots/<id>-<slug>-desktop.jpg and -mobile.jpg for states marked shot=1 in poc/states.js.
set -eu
cd "$(dirname "$0")"
CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
B=http://127.0.0.1:4196/gk-018-r04-claims
F=${1:-}
run() { perl -e "alarm 60; exec @ARGV" "$CHROME" "$@"; }  # a stuck headless run is stopped after 60 s
mkdir -p screenshots
shot() { name=$1; src=$2; for dev in desktop:1440 mobile:390; do d=${dev%%:*}; w=${dev#*:}
  enc=$(python3 -c "import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1],safe=''))" "poc/$src")
  h=$(run --headless=new --disable-gpu --hide-scrollbars --window-size=$w,900 --virtual-time-budget=5000 --dump-dom "$B/shot.html?w=$w&src=$enc" 2>/dev/null | sed -n 's/.*<title>H=\([0-9]*\)<.*/\1/p')
  [ -n "$h" ] || { echo "no height for $name $d"; exit 1; }
  [ "$h" -le 6000 ] || h=6000
  run --headless=new --disable-gpu --hide-scrollbars --window-size=$w,$h --virtual-time-budget=5000 --screenshot="screenshots/$name-$d.png" "$B/shot.html?w=$w&h=$h&src=$enc" 2>/dev/null
  [ "$d" = desktop ] && sips --resampleWidth 1120 "screenshots/$name-$d.png" >/dev/null  # desktop laid out at 1440, saved at 1120 (as r03b2)
  sips -s format jpeg -s formatOptions 60 "screenshots/$name-$d.png" --out "screenshots/$name-$d.jpg" >/dev/null && rm "screenshots/$name-$d.png"
  echo "$name-$d ${w}x$h"; done; }
node -e 'const S=require("./poc/states.js");console.log("S00 index.html Index");for(const [id,u,t,s] of S)if(s)console.log(id,u,t)' | while read -r id src title; do
  slug=$(printf '%s' "$title" | tr 'A-Z' 'a-z' | sed 's/[^a-z0-9]\{1,\}/-/g; s/^-//; s/-$//' | cut -c1-48 | sed 's/-$//')
  case "$id $slug" in *"$F"*) shot "$id-$slug" "$src";; esac; done
