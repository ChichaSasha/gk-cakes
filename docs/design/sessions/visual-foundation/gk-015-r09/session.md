# GK-015 r09 — transparent hero cake on the page surface

2026-11-02. Creator feedback on r08: “I don't like this cake on a table is not in png, as idea is
it's background is the same as website background … same cake on the table, but with … no
background color at all, so background is website itself”. r09 is a candidate for Creator review
and is **not approved**. r08 (independently reviewed, PASS for Creator review) and r07 are
preserved byte-for-byte.

## Execution check and scope

Continuing GK-015 visual author in the same conversation; host model `llmplatform/claude-opus-5-5`
(role default gpt-5.6-sol/high — mismatch; user-selected model takes precedence). Inputs: r08
sources and review, r04 hero provenance, unchanged GK-019 r6.5 handoff (r08 input manifest).
Writes limited to this r09 folder. No shared contracts, owner data, Trello, Git or deployment.

## Change

- **Hero image:** the same Creator-requested r04 image (`hero-reference.webp`, SHA-256
  `97f9c769…`, matches r04 higgsfield-evidence.json) with its grey studio background removed
  on-device by Apple Vision’s foreground subject mask (macOS 27.2). The mask is the PNG’s alpha
  channel; **all 495,763 visible pixels keep the exact source RGB** (0 mismatches), then a
  lossless crop to the subject + 24px. No generation, retouching or paid service. Evidence:
  `hero-cutout-evidence.json`, `tools/extract.swift`, `tools/verifypx.swift`,
  `tools/extract-report.json`. Output `poc/assets/hero-cake-stand.png` (936×1031), plus the alpha-only pocket cleanup below. The old
  backdrop image is removed from this revision’s assets.
- **Layout:** the cutout stands in the hero’s right column directly on the page surface (no
  panel, no fill), with a soft CSS ground shadow under the stand. The mask also kept a thin
  natural contact shadow at the base. Works on Satin silver, Porcelain and Brushed.
- **Carried r08 review items:** R2 phone text links 9px → 11px; R4 wrapped tertiary link no
  longer indented at narrow widths; R5 the default Satin surface is painted before the script
  runs (no warm flash). Storage namespace moved to r09.

Unchanged from r08: Satin silver + Serif proposal, design-option switch, typography, buttons,
cards, all behaviour. Still open from r08 review: R1 (Sans is sentence case, so the r07/reference
uppercase look is only in r07), R3 (Back/Forward restores the URL’s older option), R6 (phone
preview bar three rows; accordion headings 12px → 11px).

## Review round 1 fixes (review-r09.md, REVISE → fixed)

- F1/F2: `.hero` no longer clips vertically (horizontal clip kept) and grows with the art, so the
  stand is never cut on tall tablets and its shadow has no hard edge on phones.
- F4: one enclosed backdrop pocket between a calla tip and the cake (327 px) made transparent by
  an alpha-only edit (`tools/island.swift`); all 495,436 visible pixels still match the source.
- F3: the see-through check now samples every transparent PNG grid point beyond drop-shadow reach
  (≥129 points per run, 19 runs, max diff 2) and a new check proves no clipping and no step under
  the stand at 320–1440 including 768×1024, 820×1180, 1024×1366 (24 runs, max step 1.6). A scratch
  copy without the fix fails it.
- F6: unindented wrapped link extended to 560px. F5 (edge halo on dark colours only) and F7
  (875 KB lossless PNG; optimise for production) are recorded as limitations.

## Preview

`python3 -m http.server 8773 --bind 127.0.0.1 --directory docs/design/sessions/visual-foundation/gk-015-r09/poc`
then http://127.0.0.1:8773/index.html. Renders: index/home-story/shop/cake/custom × desktop and
mobile; compare-porcelain (desktop/mobile), compare-brushed, compare-sans; fallback-mobile.

## Verification

`verify.cjs`: full r08 suite plus — cutout file matches the evidence hash and the old backdrop
file is absent; hero, column and stage have no background; **the page shows through the
cutout**: the same five padding pixels are compared with the cake shown and hidden (max channel
difference ≤2) on all three surfaces at 320/768/1440 and the default desktop; cutout stays in the
right column; 11px phone text links; unindented wrapped link at 320px; Satin painted with
JavaScript disabled. Result in verification.json (24 check groups, 16 pages × 4 widths, 38
contrast pairs). Local POC evidence only.

## Approval and questions

Approval: **none yet**. Questions: (1) Does the cake now sit on the page the way you intended?
(2) Ground shadow under the stand — keep, softer, or none? (3) Earlier open choices still
stand: surface, headings, one or two chrome seals, anything too sparse.
