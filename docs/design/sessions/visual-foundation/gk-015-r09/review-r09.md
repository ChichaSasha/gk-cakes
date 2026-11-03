# GK-015 r09: independent review

Date: 2026-11-02. Reviewer: separate fresh-context reviewer, the same one that reviewed r08. The host reports `llmplatform/claude-opus-5-5`, the same host model the author reports. Independence therefore comes from a fresh context and no authorship, not from a different model. This review covers revision **GK-015-r09**, `output-manifest.json` SHA-256 `cbad004ff269b2d8a1f0c8d5bc71134cd2277bb65fd1d57b4ccd53e30f2d0358` (58 files). It does not cover later bytes.

This is a technical and design-quality review. **It is not visual approval.** Only the Creator can approve a revision.

## Verdict: REVISE (bounded; no blockers)

The asset meets the Creator's request:

- it is the same cake and stand, and the pixels are unchanged (I recompiled and reran the pixel check myself);
- the grey studio backdrop is gone;
- the page surface shows around the cake and through the gaps on Satin silver, Porcelain and Brushed.

On desktop (1024–1920 wide, ordinary heights) the composition is calm and well grounded. The code change is tightly contained.

Two layout defects come from r08's `.hero{overflow:hidden}` meeting the new free-standing art:

- **F1:** On tall tablet-portrait screens the bottom of the stand is cut off by a straight line.
- **F2:** On every phone width the CSS drop-shadow under the stand is clipped. The result is a straight edge that reads as the bottom of a faint box under the cake, which is the "image with its own background" impression the Creator asked to remove.

Both are small CSS fixes. The suite passes, but it does not look at either place (F3).

## Findings

### F1 — major — The stand is cropped on tall tablet-portrait screens

Evidence:

- **The CSS:**
  - At 761–1050px wide, `.hero` is a fixed `height:540px` with `overflow:hidden`. Both are r08 rules and unchanged.
  - r09 sets `.hero-art{height:clamp(420px,56vh,560px)}` at ≤1050px (`styles.css`, r09 block).
  - Once the viewport is taller than ~964px, the art (560px) is taller than the hero (540px). The art's bottom, where the stand sits, is clipped.
- **Measured** clearance between the hero's bottom and the stand's bottom (Chrome; image box and object-fit maths):

  | Viewport | Clearance |
  |---|---|
  | 768×1024 | −13px |
  | 800×1280 | −12px |
  | 820×1180 | −12px |
  | 834×1194 | −12px |
  | 912×1368 | −11px |
  | 1024×1366 | −8px |
  | 1050×1000 | −7px |

  Negative means the stand is cut. At 1024×768 the clearance is +65px, and at 1051×1000 it is +52px (fine).
- **Pixel step** across the hero's bottom edge at the stand's centre, 768×1024: −190 (Satin), −197 (Porcelain) and −153 (Brushed) grey levels. That is the dark base cut by a straight line. My screenshot at 768×1024 shows the lower part of the base missing.
- **Coverage:** `verify.cjs` tests 768px only with a 900px-tall viewport (art 504px, so no crop). No author render shows a tablet portrait screen.

Fix:

- Keep the art inside the hero at ≤1050px, e.g. `height:min(470px,56vh)`. That keeps it below 540px and leaves room for the shadow (see F2).
- Alternatively, replace the fixed `height:540px` with a `min-height` so the hero grows.
- Add a check that the stand's bottom sits above the hero's bottom at 768×1024 and 1024×1366.

### F2 — major — The drop-shadow is clipped into a straight edge under the stand on phones and some tablets

Evidence:

- **The CSS:** `.hero-cutout{filter:drop-shadow(0 26px 24px #1c1b1a1a)}` paints about 60px below the stand base. `.hero` still clips at its own bottom.
  - At ≤760px the art is 380px tall and runs to the hero's bottom, so the base ends only **7–9px** above the clip line. Measured at 320×568, 360×780, 375×667, 390×844, 414×896, 430×932, 600×900 and 760×900.
  - Clearance is 15–17px at 761×900, 768×900 and 900×900, and only 42–43px at 1280×720 and 1366×768.
- **Measured step** across the clip line under the stand (average grey level, just above vs just below):

  | Viewport | Satin | Porcelain | Brushed |
  |---|---|---|---|
  | 320 | −16 | −17 | −13 |
  | 390 | −15 | −15 | −12 |
  | 900×900 | −13 | −13 | −10 |
  | 1440×950 | −3 | −3 | −2 (not visible) |

  With the filter disabled the step is 0 at every width, which confirms the drop-shadow as the cause. Outside the stand's width the step is 0.
- **What it looks like:** in my 390 and 900 screenshots a slightly darker band under the stand ends in a hard horizontal line. With the hero's bottom it reads as a faint rectangle, right where the Creator wants the cake to sit on the page with no background of its own.
- **Coverage:** the author's renders don't show it. `index-mobile.png` is viewport-only and stops above the hero's bottom; `home-story-mobile.png` starts below it.

Fix — any one of:

- give the shadow room, e.g. ≥64px bottom padding inside `.hero-art`, or a smaller stage;
- drop `overflow:hidden` from `.hero` now that the full-bleed panel is gone, and re-check that nothing overflows horizontally;
- remove the CSS drop-shadow and rely on the kept contact shadow plus the `::after` ellipse, which already ground the stand well on desktop.

Then add a check that the step across the hero's bottom under the stand is ≤2 at 320, 390, 768×900 and 1366×768.

### F3 — minor — The "see-through" check is valid but narrow, and blind to F1, F2 and interior gaps

Evidence:

- **How it works:** `heroShowsPage()` compares 5 points 6px inside the `.hero-cutout` box (top-left, top-right, middle-left, middle-right, bottom-left), shown vs `visibility:hidden`, and allows a difference of ≤2.
- **What it does catch:** this does discriminate against the main risk. If the PNG or the image box had an opaque backdrop, the corner points would differ. `session.md` describes it accurately ("the same five padding pixels").
- **Its blind spots:**
  - **Letterbox points:** `object-fit:contain` with bottom positioning leaves empty space at the top of the box at 320px (71.6px) and 768×900 (115.7px). There the two top points sample the page, not the PNG.
  - **Missed regions:** no point lies under the stand (F2), and none lies in the gaps between leaves, spikes and cake (F4).
  - **Tablet heights:** tablet widths are tested only at a 900px height (F1).
- **Recording:** `verification.json` records only "pass", not the measured differences.

Fix:

- sample a band under the stand and across the hero's bottom;
- compute alpha=0 points from the PNG, including interior gaps, and compare them with the filter disabled;
- add tall tablet viewports;
- record the differences.

### F4 — nit — A small island of backdrop grey remains between the calla tip and the cake

Evidence:

- **Where:** in `hero-cake-stand.png`, the block x 714–726, y 556–572 is opaque backdrop grey (RGB ≈200, alpha 246–255). Decoded with my own PNG reader.
- **Size on screen:** about 8×10 CSS px at 1440 and about 4×5 on phones. It is slightly darker than Satin (≈227–231) and Porcelain (≈237).
- **Visibility:** on harsh colours it is clearly a leftover piece of background (magenta composite). At 1× on the site it is barely noticeable (4× zoom of the live 1440 page).
- **Claim mismatch:** `hero-cutout-evidence.json` says "Holes between leaves and cake are transparent". That is true everywhere else I checked: upper-left leaf and cake, spike and right leaf, and under the turntable plate.

Fix: optionally set alpha to 0 for that island. An alpha-only edit keeps the "RGB unchanged" claim. Then rerun `verifypx.swift`. Otherwise, disclose it.

### F5 — nit — Edges keep backdrop colour

This is a consequence of "pixels unchanged". Partial-alpha edge pixels keep the source's grey (mean RGB 180,179,178 where alpha <64).

- **On the three site surfaces:** a faint grey hairline, invisible at 1×.
- **On black, blue or magenta:** a light halo along the leaves, spikes and rim (my composites).

This matters only if a dark surface is ever added. If so, decontaminate the edges and say that the partial-alpha RGB changed. No action is needed for r09.

### F6 — nit — R4 is only fixed up to 420px wide

On the pending and failed order pages, "Continue exploring" still wraps under the disabled button with a **20px indent** at 421, 440, 480 and 520px. It sits on the same row from 560px. r08 is identical at these widths, and ≤420px is now correct (indent 0).

Fix: make the action row `display:flex;flex-wrap:wrap;gap:12px 20px` and drop the margin, or raise the breakpoint to about 560px.

### F7 — nit — The hero is about 10× heavier

`hero-cake-stand.png` is 875,240 bytes. It replaces the 87,274-byte `hero-reference.webp` and is the image most likely to set Largest Contentful Paint. That is fine for a local POC. For production, consider lossless WebP with alpha, which keeps the exact pixels, plus responsive sizes.

## Assessment against the five questions

1. **Does r09 meet the request?** In substance yes; in layout not yet. It is the same cake, flowers and stand (checked by pixels, and visually against the source). The backdrop is removed, there is no panel or fill (`.hero`, `.hero-art`, `.hero-stage` have background `rgba(0,0,0,0)`/`none` on all surfaces), and the page shows around the cake and through its gaps. I found nothing missing from the subject: both spikes, all four leaves, the calla, the green stem, and the full stand with rim and base are present. Where it falls short:
   - **Tablet portrait:** the stand is cropped (F1).
   - **Phones:** a box edge appears under the stand (F2).
   - **The cutout:** one small grey island (F4).
2. **Non-generative, with unchanged subject pixels?** Yes.
   - **Pixel check:** I compiled `tools/verifypx.swift` and ran it on `gk-015-r08/poc/assets/hero-reference.webp` and the PNG at offset 563 95. Result: 495,763 visible pixels checked, **0 mismatches**, 469,253 transparent.
   - **Sensitivity:** shifting the offset by one pixel gives 371,926 (564 95), 379,616 (563 96) and 415,425 (562 94) mismatches, so the check is meaningful.
   - **Independent decode:** my own decoder confirms 418,208 opaque, 77,555 partial and 469,253 transparent pixels. The file contains only `sRGB` and an `eXIf` chunk holding just the colour space and dimensions.
   - **Hashes:**
     - the source `97f9c769…` matches r04's `higgsfield-evidence.json`;
     - the output `98fd9f94…` and all three tool hashes match `hero-cutout-evidence.json`.
   - **Code:** I read `extract.swift`. It applies the Vision foreground mask as straight alpha on the original RGBA, then crops to the alpha>8 bounds plus 24px. Nothing is generated or retouched.
   - **Caveats:**
     - "Unchanged" means equal to the decoded lossy WebP. The source itself is the r04 GPT Image 2 extraction; only the cutout step is non-generative.
     - I did not rerun `extract.swift`, so I have not shown that the Vision mask reproduces. I verified the output against the source instead.
3. **Calm and rich, without regressions?**
   - **Desktop:** at 1024–1920 the cutout is larger and calmer than r08's panel. It is grounded by the kept contact shadow and the soft ellipse. It overlaps the copy by 0 px² at all 25 sizes I measured, and nothing overflows horizontally.
   - **Phones:** the cake sits well below the buttons (327×362 at 390). At 320 there is a 72px empty band above it, which is acceptable.
   - **Regressions:** F1 and F2.
4. **Carried fixes and unintended changes:**
   - **R2 holds:** "Shop all cakes →" and "Choose date →" are 11px at 390. The other 10px items listed under "Pre-existing" below are byte-identical in r08.
   - **R4 holds only at ≤420px** (F6).
   - **R5 holds:**
     - with JavaScript disabled, the background is `rgb(227,230,231)` (suite);
     - with `app.js` held back 700ms, `<html>` has no `data-surface`, the Satin background and the Satin sheen, then becomes `satin`.
     - The trade-off: a tab whose stored choice is Porcelain now shows Satin until the script runs. That is acceptable because the script is local and deferred.
   - **The `app.js` diff against r08 is exactly:**
     - the storage namespace, `r08` → `r09` in 6 places;
     - the hero markup (`span.hero-stage > img.hero-cutout` with `width`/`height`, same alt text);
     - the policy text (revision number plus one sentence about the cutout).
   - **`styles.css`:** the first 322 lines are byte-identical to all of r08, plus a 17-line appended block.
   - **Everything else:** all HTML pages and other assets are byte-identical. The WebP was removed and the PNG added. The r08 `.hero-reference` rules remain as harmless dead CSS.
   - **Still open from r08:** R1, R3 and R6 are unchanged, as `session.md` says.
5. **Does the suite test what `session.md` claims?** Literally, yes. I reran it from a temporary copy (r07 and r08 copied alongside, because it byte-compares r07's brushed texture): **PASS**, 24 check groups, 16 pages × 4 widths, 38 contrast pairs. `checks`, `contrast` and `sources` are identical to the committed `verification.json`. The see-through check is narrower than "the page shows through the cutout" suggests (F3).

## Integrity

- **r09 manifest:** MATCH (`cbad004f…`); 58/58 listed files match; no unlisted files apart from the excluded ones.
- **r08 manifest:** MATCH (`c02d5baa…`); 55/55.
- **r07 manifest:** MATCH (`902b527b…`).
- **Nothing outside r09 changed** after r08's manifest. The one later file is my own `review-r08.md` addendum (20:33), written before r09's files (20:49). Its current SHA-256 is `bf88b7e9c7bd013aaef9acd17734b581b0b74ebc796b89c32b5fc28d9ecd1f01`.
- **Source hashes:** `verification.json` pins `app.js` `46b772cc…` and `styles.css` `b7dd688e…`, matching the files on disk.
- **My writes:** this file only. All my scripts, renders and the suite rerun stayed under the system temp folder. The r09 server on 8773 was already running, and I left it running.

## What I verified myself vs took from the author

**Verified myself:**

- manifests and hashes;
- the `verifypx` run and its sensitivity;
- an independent PNG decode and alpha statistics;
- composites on magenta, black, white, blue, Satin and Porcelain, plus 2.5–3× zooms of the spike tips, leaf tips, leaf/cake gaps, calla, rim and base;
- the live page at 25 viewport sizes across all three surfaces: geometry, overlap and overflow;
- the hero-bottom pixel step with and without the filter and the ellipse;
- R2 font sizes across 12 routes at 390 (r08 vs r09);
- R4 at 10 widths in two order states (r08 vs r09);
- R5 with the script held back;
- the r08→r09 code diffs;
- the suite rerun.

**Taken from the author:**

- that the mask came from Apple Vision on macOS 27.2 (I did not rerun the extraction);
- the `extract-report.json` statistics beyond the counts I recomputed.

## Pre-existing (unchanged from r08; not counted in this verdict)

At 390px, some `main` text is still 10px:

- the "Create your own →" primary button on index, shop, cake and about;
- breadcrumbs;
- checkout and custom step labels;
- the shop catalogue note;
- the bag "Remove" button.

I did not flag these in the r08 review. They are worth a look in a later pass.

## Limits

- **Browser:** Chrome via Playwright only (device pixel ratio 1 for the measurements). No Safari, Firefox, real-device or screen-reader testing.
- **Measurements:** pixel steps are averaged across colour channels from screenshots on one display.
- **Not reproduced:** I did not rerun the Vision extraction.
- **Judgement:** severity and composition calls are reviewer opinion, not Creator decisions.

**Creator approval of r09: none.**

---

## Re-review — 2026-11-02 (focused, after REVISE)

This covers revision `output-manifest.json` SHA-256 `adeb9d67b004966bb9f14a36e8fd26b88b297c384f096307403f18fe15c50fbe` (60 files). The original review above is unchanged. This is still not visual approval.

### Updated verdict: PASS for Creator review (bounded, technical)

Both major findings are fixed, and I re-measured them myself:

- **F1:** the stand is no longer cut on any viewport.
- **F2:** the hard shadow edge under the stand is gone on all three surfaces.

Also resolved:

- **F3:** the rebuilt suite checks catch a regression; I proved this with two mutants.
- **F4:** the pocket is transparent, using an alpha-only edit with the RGB proven unchanged.
- **F6:** the R4 fix now holds up to 560px.

F5 and F7 are now disclosed as limitations, which I accept. Two nits remain (N1, N2).

### Integrity

- **r09:** MATCH (`adeb9d67…`); 60/60 listed files match; no unlisted files apart from the excluded ones.
- **r08:** MATCH (`c02d5baa…`, 55/55), unchanged; `review-r08.md` is still `bf88b7e9…`.
- **r07:** MATCH.
- **Nothing outside r09 changed** since this review was first written.
- **Changes against the previous r09 bytes:**
  - `styles.css`: 5 appended lines, with the first 339 lines byte-identical;
  - the PNG, plus 2 new tool files;
  - `hero-cutout-evidence.json`, `session.md`, `verify.cjs`, `verification.json` and the renders.
- **Unchanged:** `app.js`, all HTML pages and all other assets.

### Disposition of the original findings (each checked myself)

| # | Status | Evidence |
|---|---|---|
| F1 (major) | **Resolved** | `.hero-inner{height:auto}` and `.hero{overflow-y:visible}`. Hero bottom minus stand bottom is now positive at all 25 viewports × 3 surfaces. It is 11–13px on tall tablets (768×1024, 800×1280, 820×1180, 834×1194, 912×1368, 1024×1366, 1050×1000), where the hero now grows to fit the art, and 35–65px elsewhere. In crops at 768×1024 and 1024×1366 the stand and base are whole. |
| F2 (major) | **Resolved** | Step across the hero's bottom under the stand: −2.0 to +1.3 at all 75 measurements (it was 13–17). The largest jump between neighbouring pixels in the 95px under the base is 2–5 levels, 4–10px below the base, inside the soft contact-shadow fade; there is no straight edge. Phones also gain a 28px margin, giving 35–37px clearance. In crops at 390, 768×1024 and 1024×1366 the shadow fades out naturally, clear of the next section's text. On tall tablets the shadow's ~62px reach ends 1–3px past that text's top, at near-zero intensity, with no visible effect. |
| F3 (minor) | **Resolved** | `heroShowsPage` now maps the PNG alpha map into the image's on-screen rectangle (no samples in empty space beside it). It uses only transparent grid points beyond drop-shadow reach and records points and max difference. `heroNotClipped` checks `overflow-y`, that the stage stays within the hero, and the 9-point step under the stand, at 8 viewports including three tall tablets. **Mutant A** (my temporary copy without the fix block) fails with "hero clips vertically", confirming the author's claim. **Mutant B** (fix kept, but `.hero-inner{overflow:hidden}` reintroduces clipping) fails with "hard edge under the stand … (6.6)". So the pixel step catches clipping on its own, not only through the `overflow` assertion. By construction, interior gaps are too small to clear the shadow exclusion and are not sampled; the pocket is covered by alpha evidence instead (F4). |
| F4 (nit) | **Resolved** | My rerun of `verifypx` at 563 95: 495,436 visible pixels, **0 mismatches**, 469,580 transparent. A 1px shift (564 95) gives 371,718 mismatches. My own decode of old vs new PNG: RGB identical at every pixel; alpha changed at exactly 327 pixels, all to 0, within x708–722, y552–580. The cleared pixels were all neutral grey (RGB 185–212), i.e. backdrop. Hashes match the updated evidence (PNG `137f419f…`, `island.swift` `bd8c8535…`, `island-report.json` `123c45d7…`). On magenta the pocket now shows through; on Satin, Porcelain and Brushed (live 1440, 4× zoom) the grey wedge is gone. |
| F5 (nit) | **Disclosed** | The edge-halo limitation is now stated in the evidence notes and `session.md`. Acceptable as is. |
| F6 (nit) | **Resolved** | Pending and failed order pages: the link sits on its own line with indent 0 at every width from 320 to 560px, and on the same row from 561px. |
| F7 (nit) | **Disclosed** | The 875 KB lossless PNG and the need for a production derivative are now disclosed. Acceptable for the POC. |

Regression checks:

- **No horizontal overflow:** none in 528 page loads (16 routes × 11 widths from 320 to 1440 × 3 surfaces). The page doesn't overflow even with no clip at all, so `overflow-x:clip` is only a safeguard.
- **Layout:** the subject never overlaps the hero buttons, and the desktop composition is unchanged (1440×950 clearance is still 53px).
- **R2 and R5:** unchanged code, so they still hold.

Suite rerun from a temporary copy, with r07 and r08 alongside: **PASS**, 24 check groups, 16 pages × 4 widths, 38 contrast pairs.

- `sources`, `checks`, `contrast` and all 19 `heroSeeThrough` runs (min 129 points, max diff 2) are identical to the committed `verification.json`.
- The 24 `heroClipChecks` match except one rounding-level difference (Porcelain 820×1180: 1.4 vs 1.3). All steps are ≤1.6.
- The `session.md` figures (≥129 points, 19 runs, max diff 2; 24 runs, max step 1.6) are accurate.

### Remaining findings

#### N1 — nit — Without `overflow:clip` support, the clip returns

In a browser without `overflow:clip` (Safari before 16, iOS before 16):

- `overflow-x:clip` is dropped, so r08's `overflow:hidden` x-axis stays;
- `overflow-y:visible` then computes to `auto`;
- a scroll container clips what it paints.

Emulated in Chrome with those computed values, the hard step under the stand returns: −15.0 at 768×1024, −5.7 at 390, −3.3 at 1440. The stand itself is not cut.

Fix: declare `.hero{overflow:visible}`, which I confirmed gives no horizontal overflow, or put `overflow:visible` before `overflow-x:clip` so older browsers fall back safely.

#### N2 — nit — The cleaned pocket keeps a thin rim

A 1–2px rim of near-opaque grey remains along the pocket's leaf edge (e.g. x≈706, y570–585, RGB ≈196–200, alpha 191–239). The cleared area also has a binary, slightly stair-stepped edge.

- **On magenta:** visible as a thin pale outline.
- **On the three site surfaces:** invisible at 1× and in a 4× zoom of the live page.

The author partly discloses this ("the leaf edge bordering the cleaned pocket keeps its feathered grey"). No action is needed unless a dark surface is added.

**Still open from r08, as `session.md` says:** R1, R3 and R6, unchanged. The pre-existing 10px text noted above is also unchanged.

### What I verified myself vs took from the author

**Verified myself:**

- all manifests and hashes;
- the `verifypx` rerun and the shifted offset;
- an independent old/new PNG decode and diff;
- harsh-colour and live zooms of the pocket;
- 25 viewports × 3 surfaces: clearance, step, neighbouring-pixel profile, overlap and overflow;
- the 528-load overflow sweep;
- a no-clip control and the old-Safari emulation;
- R4 at 14 widths in two order states;
- reading the rebuilt checks;
- the suite rerun, and both mutants (temporary copies on temporary ports 8795 and 8796, since stopped).

**Taken from the author:** how `island.swift` was invoked. I verified its output, not the run.

### Limits

- **Browser:** Chrome via Playwright only. The old-Safari behaviour is an emulation of the computed style, not a real Safari 15.
- **Not reproduced:** I did not rerun the Vision extraction.
- **Judgement:** severity and composition calls are reviewer opinion.

**Creator approval of r09: none.**
