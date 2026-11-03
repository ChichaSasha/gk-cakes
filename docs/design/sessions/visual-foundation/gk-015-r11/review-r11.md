# GK-015 r11 "calm hover" — independent review

- Reviewer: independent agent (host model `llmplatform/claude-opus-5-5`); did not author r10 or r11.
- Date: 2026-11-02. Browser: Google Chrome 157.0.8103.27 via Playwright (headless), macOS.
- Under review: `gk-015-r11/` (served on 127.0.0.1:8775), compared with `gk-015-r10/` (127.0.0.1:8774).
- Only this file was created. No other files edited, no Git/Trello actions, and `verify.cjs` was **not** run inside the r11 folder.

## Verdict: **PASS for Creator review**

r11 changes only cake-picture hover, as the Creator asked. The sparkle sources are gone: the specular sweep, the cake zoom/rotation and the cursor-following spotlight on tiles and on the cake-page photo. What remains is a small 3px lift, a slightly deeper shadow and the "View cake" pill. I found no blockers or majors. Two minor hover-polish issues and some evidence nits are listed below; none of them should stop the Creator from reviewing r11.

**This review is not visual approval.** Only the Creator can say whether this is "a bit" of motion and approve r11. Until then GK-015 stays as the author left it: candidate, not approved.

## 1. Integrity

| Check | Result (verified myself) |
|---|---|
| r11 `output-manifest.json` SHA-256 | `8d698f70a5f8c7a271574da2cd4062b36691b7c768519bba6fab1db2e5e83fe7` — matches the expected value |
| r11 manifest contents | 65 files listed, all hashes match. No unlisted files apart from the declared exclusions (`output-manifest.json`, `review-r11.md`) |
| r10 manifest (`931a5b38…`) | 61 files, all hashes match, no unlisted files. **r10 is unchanged.** |
| r07–r09 (author says preserved) | Every listed hash matches. r07 uses an older manifest format that lists only 31 files, so its other files are not covered |
| Served bytes vs files on disk | `styles.css` and `app.js` served on 8774 and 8775 are byte-identical to the r10 and r11 files on disk |
| Review files | r10 has **no review file** (limitation, see §6). r08 and r09 have one |

## 2. Scope: r11 vs r10

What the diff contains (`diff -r`, read line by line):

- **`poc/styles.css`**: only one block appended at the end, lines 555–571, marked "r11". Nothing else in the stylesheet changed. The block:
  - turns off the sweep: `.product-photo::before{content:none}` (:560)
  - keeps each tile on its own compositor layer: `will-change:transform; translate3d(0,0,0)`, with a 0.6s transition (:561)
  - stops the cake from transforming (:562)
  - neutralises hover by default (:563), then gives a 3px lift and the `--sh-2` shadow only under `(hover:hover) and (pointer:fine)` (:564)
  - gives keyboard focus the same lift (:565)
  - starts the pill 6px low instead of 14px (:566)
  - lifts the hero card 2px instead of 4px (:567)
  - removes the lifts and transitions under reduced motion (:568–571)
- **`poc/app.js`**: four lines changed.
  - The sessionStorage prefix changes from `gk-poc-r10-` to `gk-poc-r11-` (3 places). This keeps demo state separate per revision.
  - The policies page now says "revision r11".
  - In the pointer handler (:209), the tile `--mx/--my` branch is deleted. The magnetic-button branch is byte-identical.
- **No other POC file changed**: the HTML files and every file under `assets/` are identical.
- `verify.cjs`: port and storage prefix updated, the text-link check now polls, and a new r11 group was added (:125–140). New evidence files: `motion-evidence.json`, `tools/measure-hover.cjs`, `hover-compare-r1x-mid.png`. Renders were regenerated.

**Resting pages are unchanged (my own render comparison).** I compared 10 routes at 1440×950 and 390×844, scrolled the full height, with time-based animations frozen at t=0.

| Pages | Result |
|---|---|
| cake, custom, about, faq, bag, checkout | Pixel-identical (max channel difference ≤1) |
| policies | Differs only in the "revision r11" text |
| shop, home, favourites (pages with tiles) | Only edge-level differences: the "02" index label and the cake outlines. This comes from the tile now sitting on its own layer (sub-pixel placement). Sharpness is unchanged: gradient energy is within ±0.7% over 38 fully revealed tile samples at DPR 1 and 2 |

**Features outside hover are unchanged** (checked myself):

| Feature | How I checked | Result |
|---|---|---|
| Magnetic primary button | Code is identical; `--tx` responds | Same |
| Dropdowns | CSS untouched; suite group passes | Same |
| Cake morph between pages (view transitions) | Clicked a hovered Shop tile through to the cake page | Same transition groups in both revisions (`root`, `site-header`, `cake-silver` plus exit snapshots); mid-morph frames look equivalent |
| Glass header | Compared header pixels with and without blur | Same difference in both revisions; the r10 backdrop-root guard still passes |
| Scroll reveals | Not touched by the diff | Same |
| Reduced transparency / increased contrast | Not touched by the diff; suite passes | Same |
| Layout | Hovered a tile at 4 widths on shop and home | No layout change, no horizontal overflow; CLS is the same in both revisions |

## 3. Does r11 answer the feedback?

Yes: there is still a little motion, it is clearly less, and the sparkle is gone.

**How I measured.** I paused the CSS transitions that a hover starts and stepped their `currentTime`, so the frames do not depend on machine load. Background animations were frozen. In the table:

- **Cake edge shift**: how far the cake's corners move, computed from the actual transform matrices.
- **Change after removing the lift**: mean change left once the lift is undone (catches zoom, rotation and shimmer).
- **Bright patches**: share of pixels more than 25 levels brighter than at rest, after removing the lift. This is the glint/sparkle signature.

| Tile | Cake edge shift, r10 → r11 | Change after removing the lift, peak (settled), r10 → r11 | Bright patches, peak (settled), r10 → r11 |
|---|---|---|---|
| Shop 02 | 15.7px → **3.0px** | 18.8 (13.2) → **1.9 (0.15)** | 16.3% (5.3%) → **0.5% (0.01%)** |
| Shop 01 | 15.7px → **3.0px** | 11.7 (8.7) → **1.4 (0.16)** | 11.3% (3.4%) → **0.1% (0%)** |
| Home 01 (large) | 21.1px → **3.0px** | 9.4 (8.3) → **0.95 (0.03)** | 7.1% (3.3%) → **0.04% (0%)** |
| Home 04 (wide) | 21.1px → **3.0px** | 19.0 (19.0) → **1.2 (0.03)** | 10.4% (6.4%) → **0.3% (0%)** |

- **What a hover now animates.** r10: tile transform and shadow (0.7s), the `::before` sweep (1.2s), cake scale and rotation (0.8s), and the pill. r11: tile transform and shadow (0.6s) and the pill only.
- **Visual check.** In my frame strips, the white diagonal sweep band is clearly visible in r10 at about 200ms; r11 shows no band and the cake keeps its size. The author's `hover-compare-r10-mid.png` and `hover-compare-r11-mid.png` agree.
- **Every cake picture on 9 pages.** I hovered the centre, then moved the cursor across each picture:

  | Picture | r10 | r11 |
  |---|---|---|
  | Shop, home and favourites tiles | 6px lift + zoom + rotation; follows the cursor | 3px lift only; does not follow the cursor (≤0.15 mean noise) |
  | Floating "Pink piping" hero card | 4px lift | 2px lift |
  | Cake-page photo | Followed the cursor (mean 2.2–3.0, peak 15 levels) | No reaction |
  | Hero stand, story photos, custom-page reference, bag rows | No reaction | No reaction |

- **What is still visible on hover:**
  - the 3px lift and slightly deeper shadow
  - the glass "View cake" pill fading in and rising 6px
  - on the hero card, the existing arrow-orb turn (unchanged)

  The pill is now the most noticeable remaining element, so it is worth asking the Creator about directly (see §7).
- **Brief softening while moving (observation, not a defect).** During the 0.6s lift the tile layer passes through fractional pixel positions and is resampled. Fine piping looks slightly softer for about the first 200ms at DPR 1, then is sharp again at rest and when settled. Any transform transition does this, and it is much less than r10's zoom.

## 4. Accessibility and robustness (checked myself)

- **Keyboard.** Tab reaches the tile link; `:focus-visible` gives the 3px lift and shows the pill. The 3px blue outline (`rgb(33,65,200)`, offset 3px) is clearly visible around the card (screenshot taken). With reduced motion there is no lift and the outline stays.
- **Touch.** Emulated taps at 1024×1366, 820×1180 and 390×844, with navigation blocked so the result can be inspected:
  - r11: no lift, no zoom, resting shadow.
  - r10: 6px lift plus zoom.
  - Caveat: the pill still appears on tablets (Finding 1).
- **Reduced motion.** No lift, no transition, no cake transform, and no `::before`; the hero card does not move.
  - r10 bug now gone: r10's sweep kept its 1.2s transition even with reduced motion on, because `*{transition:none}` (styles.css:5) does not reach pseudo-elements. r11 fixes this by removing the sweep.
- **Layout.** Hover changes no layout at 1440, 1024, 820 or 390 (shop and home). No overflow. CLS is the same as r10.
- **`will-change` cost is sane.** Exactly one extra compositor layer per tile:

  | Page | Layers r10 → r11 |
  |---|---|
  | shop | 42 → 50 |
  | home | 42 → 46 |
  | favourites | 21 → 24 |

  That is about 1.1 MP more CSS area on Shop (about 4.3M device pixels at DPR 2).
- **Blur still works.**
  - "View cake" pill: computed `blur(22px) saturate(1.7)`. With its fill cleared, the blur vs no-blur difference is 20.4 in r11 vs 18.2 in r10, so the blur is active and not isolated by the tile layer.
  - Header island: blur difference identical in both revisions.
- **Checked and rejected (transparency).** I first suspected the always-on `will-change` made some tiles slightly softer at rest at DPR 1 (−5%). That reading came from my own script freezing the scroll reveal mid-animation. With tiles fully revealed, repeated runs give identical sharpness in r10, r11, and r11 with `will-change` disabled. There is no rest-state regression.

## 5. Evidence review

- **Suite rerun from a copy.** I copied `gk-015-r11` to a temp folder and ran `node verify.cjs` against 8775: **PASS, 27 check groups; 16 pages × 4 widths; 63 contrast pairs.** Its `r11HoverMotion` result was 0.69% bright / residual 2.35. The author recorded 0.50% / 1.91 in `verification.json` and 0.2% / 1.46 in `motion-evidence.json`. The spread is real-time sampling noise, well inside the thresholds (<2%, <4). The real r11 folder was not touched and still verifies against its manifest.
- **Do the new assertions actually detect the problem?** I ported the r11 group (verify.cjs:125–140) and ran it against r10:
  - These all **fail on r10 and pass on r11**: resting properties, hover state, no spotlight on tiles, the aligned-frame glint check (r10: 29.8% / 25.96), no spotlight on the cake page, hero card 2px, touch (r10 −6px) and reduced motion (r10 −6px).
  - The magnetic-button check passes on both, which is correct for a "nothing else broke" check.
  - Playwright's `hover` in the touch context does set `:hover`, so the touch check is meaningful.
- **Text-link timing change (verify.cjs:85) — not weakened.**
  - Same final assertion: the computed `background-size` must match `^100% 1px`. At rest it is `0% 1px`, so the check cannot pass before the underline has drawn in.
  - The transition is 0.45s, and I measured full width at 535–544ms (including polling). The old fixed 600ms sample had only about 60ms headroom, so a failure under load is plausible.
  - The only thing loosened is the implicit upper time limit (now about 3s). Optional fix: Finding 5.
- **`motion-evidence.json` holds up.** My deterministic measurements and my rerun of the author's own metric give the same direction and similar magnitudes.
- **Claims in `session.md` the suite does not assert:**
  - "keyboard focus gets the same lift": I verified it myself (Finding 4).
  - "Lift for mouse/trackpad only": true for the lift, but the pill still reacts on touch (Finding 1).
  - All other claims in the change table are confirmed above.

## Findings

| # | Severity | Finding |
|---|---|---|
| 1 | minor | "View cake" pill still sticky on touch tablets |
| 2 | minor | Moving from the cake to the heart drops the lift and hides the pill |
| 3 | nit | Stale "spotlight" wording in a code comment and a suite label |
| 4 | nit | Keyboard-focus lift is claimed but not asserted by the suite |
| 5 | nit | Text-link check no longer pins the 0.45s timing |
| 6 | nit | `measure-hover.cjs` output names don't match the committed evidence |
| 7 | minor, out of scope (inherited from r10) | Dark "Choose date →" pill peeks between header and filter bar on Shop |

**1. minor — "View cake" pill still sticky on touch tablets.**
- *What happens:* on touch screens wider than 760px, the tile no longer lifts, but after a tap `:hover` stays on. The pill becomes visible and rises 6px (styles.css:279, not inside the hover/pointer media query).
- *Why it matters:* `session.md` lists "Hover also applied on touch (sticky after tap)" as fixed. The impact is small, because a tap normally navigates; it shows on back-navigation or long-press. The same applies to `.hero-pick:hover` (:567, only on touch screens ≥1051px) and the inherited `.save:hover` scale (:287).
- *Evidence:* emulated tap at 1024×1366 and 820×1180 — `:hover` true, tile `matrix(1,0,0,1,0,0)`, pill opacity `1`, translate `-50%`.
- *Fix:* move the `:hover` pill reveal (and `.hero-pick:hover`) inside `@media (hover:hover) and (pointer:fine)`. Keep the `:focus-visible` reveal unconditional.

**2. minor — moving from the cake to the heart drops the lift and hides the pill.**
- *What happens:* the heart (`.save`) is a sibling of the tile link but sits over the photo. Moving onto it ends `.product>a:hover`, so the tile drops 3px and the pill fades out. The heart also does not move with the tile.
- *Context:* inherited from r10, where the drop was 6px plus un-zooming and the sweep reversing. r11 halves it, but it is still a small jump inside the exact interaction the Creator commented on.
- *Evidence:* on cake: tile `-3`, pill `1` → on heart: tile `0`, pill `0`.
- *Fix:* drive the lift from the card, e.g. `.product:has(> a:hover, > .save:hover) .product-photo` inside the pointer media query, and move `.save` with the tile (`translate: 0 -3px`, same easing).

**3. nit — stale "spotlight" wording.**
- *Where:* the comment at `poc/app.js:209` still says "r10: pointer spotlight on cake tiles". The inherited suite group's label (verify.cjs:94/100, copied into `verification.json`) still says "pearl tiles: … pointer spotlight", although that assertion only checks for a static radial gradient.
- *Fix:* reword both to "static soft highlight; magnetic pull on primary buttons".

**4. nit — keyboard-focus lift not asserted.**
- *What:* `session.md` claims "keyboard focus gets the same lift", but the r11 group does not test it. I verified it manually: tile `-3` on focus, `none` with reduced motion.
- *Fix:* add a Tab-to-tile assertion for both cases.

**5. nit — text-link timing.**
- *What:* the polling change keeps the same final assertion but no longer pins the timing.
- *Fix (optional):* also assert `transitionDuration === '0.45s'`.

**6. nit — evidence tool output names.**
- *What:* `tools/measure-hover.cjs` writes `tools/r1x-{rest,mid,hover}.png` and `tools/motion-measure.json`. The committed evidence is `hover-compare-r1x-mid.png` and `motion-evidence.json`, which was hand-assembled with extra `method`, `limits` and `measured_at` fields. Rerunning the tool in place would add 7 unlisted files to the revision folder and break its manifest coverage.
- *Fix:* write to the committed names, or to a temp folder, and note the renaming step.

**7. minor, out of scope (inherited from r10, not hover-related) — dark pill peeks between header and filter bar.**
- *What happens:* on Shop, when scrolled, the dark "Choose date →" pill (`.date-prompt .text-link`) shows through the gap between the floating header island and the sticky glass filter toolbar.
- *Evidence:* `elementsFromPoint(1300,82)` at scrollY=380. It is visible top-right in both `gk-015-r10/shop-hover-desktop.png` and `gk-015-r11/shop-hover-desktop.png`.
- *Recommendation:* raise it with the Creator or fix it in a later revision; r11 correctly did not touch it.

## 6. Verified myself vs taken from the author

**Verified myself:**
- manifests and hashes (r07–r11)
- the full diff
- served bytes
- the rest-state render comparison
- deterministic hover frames and metrics (4 tiles, both revisions)
- the picture-by-picture hover check (9 pages)
- keyboard, touch, reduced motion, layout/overflow/CLS
- compositor layer counts
- pill and header blur
- the cake morph
- suite rerun from a copy
- whether the r11 assertions detect r10
- text-link timing
- the heart interaction

**Taken from the author without independent re-derivation:**
- the inherited r07–r10 functional, contrast and font checks (I confirmed they pass in my rerun but did not re-audit them)
- the hero-cutout provenance
- the claim that r10 is the Creator-reviewed baseline

## 7. Limits

- r10 has no review file in its folder, so r10's own quality rests on its author evidence and the Creator's feedback ("the rest is fine"), not on an independent review.
- One engine only: headless Chrome 157 on macOS. No Safari or Firefox. DPR 1 and 2 were emulated; touch was emulated, not a real device.
- Motion was measured with background animations frozen. Real perception on the Creator's display, refresh rate and pointer may differ. "A bit of motion" is a subjective judgement that only the Creator can make.
- My scripts and frame captures are in a temporary folder (`/var/folders/…/T/opencode/r11rev/`) and are not part of the revision.

## Question to put to the Creator (unchanged from `session.md`, plus one)

1. Is the 3px lift right, or a touch more or less?
2. Should the "View cake" pill that appears on hover stay as it is, or be quieter?

If the answer to both is "right", the Creator can approve **r11** specifically.
