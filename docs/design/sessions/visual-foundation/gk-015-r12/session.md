# GK-015 r12 — Shop filter-bar gap fix + earlier zoom hover (trial)

2026-11-03. Built on **r11 (Creator-approved, untouched)**. Creator request: “Fix the Shop filter-bar
gap and let's try the old effect again”. r12 is a trial candidate, **not approved**.

Changes (CSS/JS appended at the end of poc/styles.css and poc/app.js; storage prefix and revision label r12):

1. Shop filter bar: while it is stuck (desktop/tablet), one frosted layer sits behind the header island
   and the bar and fades out softly below it, so cards and the dark “Choose date” pill no longer show
   through the gap. Not shown when the bar is not stuck; the bar is not sticky on mobile.
2. Cake-picture hover: the earlier effect — the cake zooms 4.5% over 0.6s with a deeper soft shadow; the
   tile does not lift; no sweep or spotlight; holds over the heart; same on keyboard focus; mouse/trackpad
   only; off with reduced motion. The cake image is pre-composited to avoid shimmer. r11’s “View cake”
   pill is unchanged.

Preview: `python3 -m http.server 8776 --bind 127.0.0.1 --directory docs/design/sessions/visual-foundation/gk-015-r12/poc`
→ http://127.0.0.1:8776/shop.html (approved r11 for comparison: 8775).

Checks run (targeted, local): strip on only when stuck at 1440/1024, gap visually clear at scroll 380;
zoom 1.045 with deeper shadow, tile static, holds on heart; reduced motion none; mobile not sticky; no
JS errors or horizontal overflow on Shop. The full r11 suite was not rerun — its hover assertions expect
r11’s 3px lift and would need updating if this effect is kept. No independent review yet.

## Update — Creator: “cake images effect as not as previousely animated one with sparcle like effect”

The trial zoom (item 2 above) is replaced by the **r10 hover exactly**: tile lifts 6px with deeper shadow,
cake grows 4.5% and turns −1.2°, diagonal shine sweep across the tile, soft highlight following the cursor
(r10 app.js branch restored), “View cake” pill rises. r12 is now r10’s styles.css/app.js plus only the
frosted filter-bar layer (and the r12 storage prefix/label). Note: this brings back the r10 behaviours r11
had fixed (pill can stick after a tap on touch tablets; moving onto the heart drops the lift).
Checked: lift −6px, cake scale≈1.045 with rotation, sweep moves, highlight follows the cursor, filter-bar
layer still on when stuck, no JS errors or overflow on Shop. Full suite not rerun.

## Update — r10 shine hover kept, both glitches fixed

- Hover effects (lift, zoom/tilt, shine sweep, cursor highlight, “View cake” pill, heart and hero-card hover)
  now apply only for mouse/trackpad, so nothing sticks after a tap on touch tablets. The leftover r07 5px
  link-hover lift is neutralised on touch.
- The hover is driven by the whole card, so moving onto the heart keeps the lift, zoom, shine and pill; the
  heart rises 6px with the tile (same 0.7s easing).
- Keyboard focus keeps r10’s lift and pill (heart rises too). Reduced motion: no lift, zoom, tilt or shine;
  the pill still appears.
Checked: on cake / on heart / away / keyboard at 1440; tap at 1024×1366 and 820×1180 (nothing stuck);
reduced motion; filter-bar layer still on when stuck; no JS errors or overflow. Full suite not rerun.

## Creator approval — 2026-11-03

After “Looks good, thank you”, the Creator instructed: **“record r12 as the approved version instead”**.
Recorded as Creator approval of exact revision **GK-015-r12** (this folder’s output-manifest.json) as the
GK-015 visual foundation, superseding the r11 approval. Includes the r10 shine hover with both glitch
fixes and the frosted Shop filter-bar layer.

Evidence limits: targeted local browser checks only (listed above); the full suite was not rerun and r12
has no independent review. Still open: canonical logo record (composite.png is the Creator’s POC choice);
a fresh integration review before affected UI implementation tickets become Ready; the GK-015 Trello
update and status (not yet authorized).
