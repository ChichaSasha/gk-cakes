# GK-015 Visual foundation — r11 "calm hover"

- Date: 2026-11-02 · Status: **candidate for Creator review — not approved**. GK-015 stays in Trello Backlog.
- Built on: r10 (its manifest verifies; r07–r10 preserved untouched). Only the cake-picture hover changed.
- Preview: `python3 -m http.server 8775 --bind 127.0.0.1 --directory docs/design/sessions/visual-foundation/gk-015-r11/poc` → http://127.0.0.1:8775/shop.html (r10 for comparison: 8774)
- Model: host `llmplatform/claude-opus-5-5` (role default gpt-5.6-sol/high — mismatch; user-selected model takes precedence). Continuing GK-015 author; not an independent reviewer of this work.

## Creator feedback on r10 (verbatim)

> Everything Looks fine apart from motion on pictures. It's distractive, update it to be a bit montioned but not this much and withour sparkles. (I mean the motion as effect when my cursor is on cake picture. The rest if fine! )

## What changed (hover on cake pictures only)

| r10 effect | r11 |
|---|---|
| Tile lifts 6px, deeper shadow | Lifts **3px**, softly deeper shadow, 0.6s gentle ease |
| Cake grows 4.5% and rotates −1.2° | **Removed** — the cake keeps its size and angle |
| Diagonal white "specular sweep" across the tile | **Removed** (the main sparkle) |
| White spotlight following the cursor (tiles and cake-page photo) | **Removed** — static soft highlight (app.js tile branch deleted) |
| Moving fine detail re-rasterised every frame (shimmer) | Tile **pre-composited** (`will-change: transform`), so piping/pearl detail is not redrawn while moving |
| “View cake” pill rises 14px | Kept, rises **6px** |
| Floating hero “Pink piping” card lifts 4px | Lifts **2px** |
| Hover also applied on touch (sticky after tap) | Lift and “View cake” pill for mouse/trackpad only; keyboard focus gets the same lift and pill |
| Moving from the cake onto its heart button dropped the lift (flicker) | Lift and pill hold anywhere on the tile, including the heart |
| Reduced motion | No lift, no transition |

Unchanged on purpose (Creator: “the rest is fine”): layout, colours, typography, buttons and their magnetic pull, dropdowns, ticker, rotating seal, scroll reveals, cake morph between pages, hero cutout, all behaviour. Code: last block of `poc/styles.css` (“r11”) and one removed branch of the pointer handler in `poc/app.js`.

## Evidence

Measured on Shop tile 2 in Chrome (`motion-evidence.json`, `tools/measure-hover.cjs`; frames `hover-compare-r10-mid.png` vs `hover-compare-r11-mid.png`):

| Measure | r10 | r11 |
|---|---|---|
| Tile area brightened >25 levels during hover, after undoing the lift (glint) | 27.8% | 0.2% |
| Change left after undoing the lift (zoom/rotation/shimmer) | 25.0 | 1.46 |
| Background change when the cursor moves (spotlight) | 7.2 | 0 |

`node verify.cjs` (POC on 8775): **PASS — 27 check groups; 16 pages × 4 widths; 63 contrast pairs.** All r07–r10 checks carried, plus the r11 group: 3px composited lift, no zoom/rotation, no sweep, no spotlight on tiles or the cake-page photo, the aligned-frame no-glint check on every run (<2% brightened, residual <4), pill still appears, lift and pill hold on the heart, keyboard focus gets the same lift, magnetic button unchanged, hero card 2px, no lift or pill on touch, none with reduced motion. One inherited r10 timing check (“text link underline draws in”) sampled a 0.45s transition at 0.6s and failed once under load; it now waits for the transition to finish — same assertion, not weakened. Local POC evidence; one browser.

## Review

Independent fresh-context review: `review-r11.md` (PASS for Creator review). Its two hover-related minors (pill after a tap on touch tablets; lift dropping on the heart) and the nits (stale spotlight wording, untested keyboard claim, measurement script writing into the folder) were fixed and re-reviewed — see the addendum. Not changed, outside this request: on Shop the dark “Choose date” button can show through the gap under the sticky filter bar while scrolling (already in r10).

## Question for the Creator

Is this amount of hover motion right — or a touch more / less? If it’s right, please confirm approval of **r11** as the GK-015 visual foundation.

## Creator approval — 2026-11-03

Asked whether “Looks great, let's keep it” referred to the r11 hover, and following this note’s
request to confirm r11 as the visual foundation, the Creator replied: **“Yes, let's keep r11”**.
Recorded as Creator approval of exact revision **GK-015-r11** (this folder’s output-manifest.json)
as the GK-015 visual foundation, including the calm hover. Approval was given after the
independent review in review-r11.md; no design file changed with this note.

Still open and not covered by this approval: the Shop sticky-filter gap noted above; the canonical
logo record (composite.png is the Creator’s POC choice); a fresh integration review before
affected UI implementation tickets become Ready; publishing the GK-015 Trello update and status
(not yet authorized). The design-option switch, if present, is review tooling only.

## Superseded — 2026-11-03

The Creator recorded **GK-015-r12** as the approved visual foundation instead of r11 (see
gk-015-r12/session.md). r11 files are unchanged; its approval is historical.
