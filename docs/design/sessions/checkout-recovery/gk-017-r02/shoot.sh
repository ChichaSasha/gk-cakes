#!/bin/sh
# GK-017 r02 screenshots. Needs, from this folder: python3 -m http.server 8792 --bind 127.0.0.1 --directory .
# shot.html (next to this script) frames poc/<page> at an exact width; headless Chrome's window is at least 500 px wide.
# Usage: sh shoot.sh  — writes screens/<name>-desktop.jpg and screens/<name>-mobile.jpg (JPEG 78 to keep the repository small).
set -eu
cd "$(dirname "$0")"
CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
B=http://127.0.0.1:8792
run() { perl -e "alarm 60; exec @ARGV" "$CHROME" "$@"; }  # a stuck headless run is stopped after 60 s
mkdir -p screens
shot() { name=$1; src=$2; for dev in desktop:1440 mobile:390; do d=${dev%%:*}; w=${dev#*:}
  enc=$(python3 -c "import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1],safe=''))" "poc/$src")
  h=$(run --headless=new --disable-gpu --hide-scrollbars --window-size=$w,900 --virtual-time-budget=4000 --dump-dom "$B/shot.html?w=$w&src=$enc" 2>/dev/null | sed -n 's/.*<title>H=\([0-9]*\)<.*/\1/p')
  [ -n "$h" ] || { echo "no height for $name $d"; exit 1; }
  run --headless=new --disable-gpu --hide-scrollbars --window-size=$w,$h --virtual-time-budget=4000 --screenshot="screens/$name-$d.png" "$B/shot.html?w=$w&h=$h&src=$enc" 2>/dev/null
  sips -s format jpeg -s formatOptions 78 "screens/$name-$d.png" --out "screens/$name-$d.jpg" >/dev/null && rm "screens/$name-$d.png"
  echo "$name-$d ${w}x$h"; done; }
while read -r name src; do [ -n "$name" ] && shot "$name" "$src"; done <<'LIST'
00-states-index states.html?shot=1
01-price-changed recovery.html?state=price-changed&shot=1
02-quote-refreshed recovery.html?state=price-changed&v=expired&shot=1
03-terms-changed recovery.html?state=terms-changed&shot=1
04-slot-taken recovery.html?state=slot-taken&shot=1
05-date-full recovery.html?state=slot-taken&v=date&shot=1
06-item-gone recovery.html?state=item-gone&shot=1
07-item-removed recovery.html?state=item-gone&v=removed&shot=1
08-other-refusals recovery.html?state=other-refusals&shot=1
09-still-checking recovery.html?state=still-checking&shot=1
10-still-checking-stopped recovery.html?state=still-checking&v=stopped&shot=1
11-still-checking-page-open recovery.html?state=still-checking&v=open&shot=1
12-unknown-result recovery.html?state=unknown-result&shot=1
13-confirming recovery.html?state=confirming&shot=1
14-hold-released recovery.html?state=hold-released&shot=1
15-late-payment recovery.html?state=late-payment&shot=1
16-late-payment-offer recovery.html?state=late-payment&v=offer&shot=1
17-late-on-cancelled-requested recovery.html?state=late-on-cancelled&shot=1
18-late-on-cancelled-pending recovery.html?state=late-on-cancelled&v=pending&shot=1
19-late-on-cancelled-returned recovery.html?state=late-on-cancelled&v=returned&shot=1
20-cancel-before recovery.html?state=cancel&shot=1
21-cancel-boundary recovery.html?state=cancel&v=boundary&shot=1
22-cancel-after recovery.html?state=cancel&v=after&shot=1
23-cancel-after-delivery recovery.html?state=cancel&v=delivery&shot=1
24-refund-requested recovery.html?state=refund&shot=1
25-refund-pending recovery.html?state=refund&v=pending&shot=1
26-refund-partly-returned recovery.html?state=refund&v=partly&shot=1
27-refund-returned recovery.html?state=refund&v=returned&shot=1
28-refund-needs-attention recovery.html?state=refund&v=attention&shot=1
29-refund-chef-cancelled recovery.html?state=refund&v=chef&shot=1
30-delivery-impossible recovery.html?state=delivery-impossible&shot=1
31-delivery-impossible-delivery-paid recovery.html?state=delivery-impossible&v=delivery-paid&shot=1
32-delivery-unpaid recovery.html?state=delivery-unpaid&shot=1
33-delivery-payment-checking recovery.html?state=delivery-unpaid&v=checking&shot=1
34-free-checkout recovery.html?state=free&v=checkout&shot=1
35-free-confirm recovery.html?state=free&v=confirm&shot=1
36-free-refused-try-again recovery.html?state=free&v=changed&shot=1
37-free-refused-time-passed recovery.html?state=free&v=closed&shot=1
38-times-collection recovery.html?state=time-list&shot=1
39-times-delivery-requestable recovery.html?state=time-list&v=delivery&shot=1
40-times-loading recovery.html?state=time-list&v=loading&shot=1
41-times-none recovery.html?state=time-list&v=none&shot=1
42-times-failed recovery.html?state=time-list&v=failed&shot=1
43-order-delivery-no-time-change recovery.html?state=order-time-change&shot=1
44-order-collection-time-change recovery.html?state=order-time-change&v=collection&shot=1
45-checkout-radio-times checkout.html?seed=pink&shot=1
46-order-page-delivery order.html?example=true&mode=delivery&shot=1
47-free-refused-place-released recovery.html?state=free&v=released&shot=1
48-refund-requested-half recovery.html?state=refund&v=requested-half&shot=1
49-refund-requested-with-delivery recovery.html?state=refund&v=requested-delivery&shot=1
50-refund-q5-full recovery.html?state=refund&v=q5&shot=1
51-refund-q5-delivery-paid recovery.html?state=refund&v=q5-paid&shot=1
52-delivery-own-courier-chosen recovery.html?state=delivery-impossible&v=courier&shot=1
53-delivery-collection-chosen-delivery-refund recovery.html?state=delivery-impossible&v=collection-paid&shot=1
54-timeline-being-made recovery.html?state=timeline&shot=1
55-timeline-delivery-being-made recovery.html?state=timeline&v=delivery&shot=1
56-timeline-cancel-while-being-made recovery.html?state=timeline&v=cancel-production&shot=1
57-timeline-no-show recovery.html?state=timeline&v=no-show&shot=1
58-timeline-completed recovery.html?state=timeline&v=completed&shot=1
59-timeline-completed-refund-via-chef recovery.html?state=timeline&v=completed-refund&shot=1
60-timeline-delivery-failed recovery.html?state=timeline&v=delivery-failed&shot=1
LIST
