#!/bin/sh
# GK-016 r02 screenshots. Serve this folder first: python3 -m http.server 8794 --bind 127.0.0.1 --directory .
# shot.html (next to this script) frames poc/<page> at an exact width; headless Chrome's window is at least 500 px wide.
# Usage: sh shoot.sh [name-filter]  — writes screens/<name>-desktop.jpg and screens/<name>-mobile.jpg (JPEG 62 to keep the repository small).
set -eu
cd "$(dirname "$0")"
CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
B=http://127.0.0.1:8794
F=${1:-}
run() { perl -e "alarm 60; exec @ARGV" "$CHROME" "$@"; }  # a stuck headless run is stopped after 60 s
mkdir -p screens
shot() { name=$1; src=$2; for dev in desktop:1440 mobile:390; do d=${dev%%:*}; w=${dev#*:}
  enc=$(python3 -c "import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1],safe=''))" "poc/$src")
  h=$(run --headless=new --disable-gpu --hide-scrollbars --window-size=$w,900 --virtual-time-budget=4000 --dump-dom "$B/shot.html?w=$w&src=$enc" 2>/dev/null | sed -n 's/.*<title>H=\([0-9]*\)<.*/\1/p')
  [ -n "$h" ] || { echo "no height for $name $d"; exit 1; }
  run --headless=new --disable-gpu --hide-scrollbars --window-size=$w,$h --virtual-time-budget=4000 --screenshot="screens/$name-$d.png" "$B/shot.html?w=$w&h=$h&src=$enc" 2>/dev/null
  sips -s format jpeg -s formatOptions 62 "screens/$name-$d.png" --out "screens/$name-$d.jpg" >/dev/null && rm "screens/$name-$d.png"
  echo "$name-$d ${w}x$h"; done; }
while read -r name src; do [ -n "$name" ] || continue; case "$name" in *"$F"*) shot "$name" "$src";; esac; done <<'LIST'
00-states-index states.html?shot=1
01-signin signin.html?state=signin&shot=1
02-signin-errors signin.html?state=signin-errors&shot=1
03-sent signin.html?state=sent&shot=1
04-callback signin.html?state=callback&shot=1
05-created signin.html?state=created&shot=1
06-created-google signin.html?state=created-google&shot=1
07-link-failed signin.html?state=link-failed&shot=1
08-google-cancelled signin.html?state=google-cancelled&shot=1
09-google-error signin.html?state=google-error&shot=1
10-unavailable signin.html?state=unavailable&shot=1
11-session-ended signin.html?state=session-ended&shot=1
12-signed-out signin.html?state=signed-out&shot=1
13-home account.html?state=home&shot=1
14-empty account.html?state=empty&shot=1
15-kept account.html?state=kept&shot=1
16-chef-added account.html?state=chef-added&shot=1
17-load-error account.html?state=load-error&shot=1
18-disabled account.html?state=disabled&shot=1
19-signed-out-account account.html?state=signed-out-account&shot=1
20-order-checking order.html?state=order-checking&shot=1
21-order-agreement order.html?state=order-agreement&shot=1
22-order-confirmed order.html?state=order-confirmed&shot=1
23-order-refund order.html?state=order-refund&shot=1
24-order-refunded order.html?state=order-refunded&shot=1
25-order-completed order.html?state=order-completed&shot=1
26-order-unknown order.html?state=order-unknown&shot=1
27-order-older order.html?state=order-older&shot=1
28-link-guest claim.html?state=link-guest&shot=1
29-link-offer claim.html?state=link-offer&shot=1
30-link-asked claim.html?state=link-asked&shot=1
31-link-added claim.html?state=link-added&shot=1
32-link-yours claim.html?state=link-yours&shot=1
33-link-dead claim.html?state=link-dead&shot=1
34-old-form claim.html?state=old-form&shot=1
35-old-errors claim.html?state=old-errors&shot=1
36-old-pending claim.html?state=old-pending&shot=1
37-old-approved claim.html?state=old-approved&shot=1
38-old-declined claim.html?state=old-declined&shot=1
39-profile profile.html?state=profile&shot=1
40-profile-saved profile.html?state=profile-saved&shot=1
41-profile-errors profile.html?state=profile-errors&shot=1
42-profile-conflict profile.html?state=profile-conflict&shot=1
43-email-change-sent profile.html?state=email-change-sent&shot=1
44-privacy privacy.html?state=privacy&shot=1
45-warning-email privacy.html?state=warning-email&shot=1
46-delete-review privacy.html?state=delete-review&shot=1
47-delete-blocked privacy.html?state=delete-blocked&shot=1
48-delete-warn privacy.html?state=delete-warn&shot=1
49-deleted privacy.html?state=deleted&shot=1
50-signout-all privacy.html?state=signout-all&shot=1
51-signout-all-done privacy.html?state=signout-all-done&shot=1
52-signout-all-partial privacy.html?state=signout-all-partial&shot=1
53-reorder-from-account reorder.html?order=GK-1009&shot=1
54-favourites-signed-in favourites.html?demo=signed-in&shot=1
LIST
