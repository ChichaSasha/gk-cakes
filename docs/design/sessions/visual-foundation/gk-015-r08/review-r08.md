# GK-015 r08: independent review

Date: 2026-11-02. Reviewer: separate fresh-context reviewer. The host reports `llmplatform/claude-opus-5-5`, the same host model the author reports, so independence comes from a fresh context and no authorship, not from a different model. This review covers revision **GK-015-r08**, `output-manifest.json` SHA-256
`d66ca2b7c0327963deadb180cdebc0ac4f11c0b8c46921bcf11a2330c875c2c7`. It does not cover later bytes.

This is a technical and design-quality review. **It is not visual approval.** Only the Creator can approve a revision.

## Verdict: REVISE (bounded; no blockers)

r08 is technically sound:

- the manifests verify;
- the JS change is small and contained;
- behaviour and the "simulated / nothing sent" wording are unchanged from r07;
- the author's suite reproduces;
- the font provenance is genuine.

It gives a credible *calm and minimal* answer, but it is not ready to show yet. The default proposal moves away from the accepted silver reference direction and from two explicit Creator requests. The session note presents this as a refinement, and none of the comparison switches shows the accepted treatment (F1). One Porcelain control-contrast gap (F2) and several inaccurate evidence claims (F3) also need small fixes first. All fixes are local to r08 and small.

## Findings

### F1 — major — The default moves away from the accepted direction, and the framing understates it

**Evidence**

- **The accepted direction.** The Creator chose the supplied reference as "our target" (`gk-015-r03/session.md:3`). This is silver/brushed metal with thin uppercase sans headings.
- **Explicit Creator requests r08 departs from:**
  - "match the supplied reference background" (`gk-015-r04/session.md:3`);
  - "more realistic metal", with the brushed texture authorised (`gk-015-r07/session.md:3`).
- **The rule.** "For later sessions, start from the accepted direction. Do not make the Creator repeatedly reselect fonts/colours" (`skills/gk-feature/references/design-sessions.md:15-16`). The same file, line 94, says global style changes invalidate dependent approvals.
- **What r08 defaults to:**
  - a warm Porcelain `#efede9` canvas (`poc/styles.css:117`);
  - Instrument Serif headings in sentence case (`poc/styles.css:141`);
  - promise icons removed (`poc/styles.css:175`).
- **How it is framed.** `session.md:4` calls this "a direction refinement of the selected reference, not a restart". Questions 1–2 (`session.md:73-74`) ask the Creator to choose the surface and the heading typeface again.
- **The comparison switches do not show r07.**
  - The option labelled "Brushed metal (r07)" (`poc/app.js:106`) shows r08's layout, hero panel and serif headings on the r07 texture (`compare-brushed-desktop.png`).
  - "Sans" gives sentence-case Manrope 300, not the r06/r07 uppercase treatment (`compare-sans-desktop.png` vs `gk-015-r07/index-desktop.png`).
  - So no combination of switches shows the accepted direction.

The change *is* listed in the session table (`session.md:22-24`), so this is a framing problem, not concealment.

**Fix**

1. In `session.md` and the message to the Creator, say plainly that Porcelain + Serif would replace the r03-selected reference direction and the r04/r07 metal-background requests. Note that it is a global style change for dependent sessions.
2. Ask it as one explicit decision ("change direction, or refine within silver?"), not as two re-selections.
3. Either rename the option to "Brushed metal texture", or make it a true r07 baseline (uppercase Manrope headings and the r07 hero treatment).
4. Consider offering "Satin silver + uppercase Manrope" as the calm variant that stays within the accepted direction.
5. Link r07, served on its own port, side by side in the Creator message.

### F2 — minor — Porcelain control borders fall below 3:1 on the darker part of the canvas, and this is untested

**Evidence**

- The control border colour `--edge` is `#8b847d` (`poc/styles.css:117`).
- I sampled the rendered Porcelain canvas with page content hidden. The darkest pixels are:
  - `#e6e3de` at the bottom-left of 1440×900;
  - `#e6e1dd` at 390×844.
- Border contrast against those, and against `--tint #e6e3de`, is **2.88:1 and 2.84:1** (below the 3:1 non-text minimum).
- `verification.json` pairs the border colour only with `#efede9` (3.15) and `#fbfaf8` (3.54). For muted text it uses `#e4e0da` as the cautious minimum, so the two checks are inconsistent.
- Affected controls over that region include inputs, selects, serving chips and the date trigger in the left column of custom/shop/checkout.

**Fix**

- Change Porcelain `--edge` to about `#7f7871`. That gives 3.40 on `#e6e3de`, 3.72 on the canvas and 4.17 on the field background. `#7d766f` would also work.
- Add border-vs-measured-minimum pairs for all three surfaces to `verify.cjs:38`.

### F3 — minor — Some session-note claims are inaccurate or not backed by evidence

**Evidence**

- **(a) "Manrope for UI at r06 sizes" (`session.md:47`).** I measured computed styles, r07 → r08, at 1440px:

  | Element | r07 | r08 |
  | --- | --- | --- |
  | Buttons | 12px | 11px, with .18em letter-spacing (`styles.css:149`) |
  | Shop filter labels | 12px, sentence case | 10px, uppercase (`styles.css:214`) |
  | Product names | 14px | 12px (`styles.css:191`) |
  | Accordion summaries | 12px | 11px |

  Mobile text links and the shop "Choose date" button stay at 9px. Form labels and controls *do* keep the r06 sizes (14px; 13–14px).
- **(b) "Brushed keeps r07 contrast values" (`session.md:45-46`).** The Brushed surface does not override `--ink`. Body text and headings therefore change from `#171919` to `#1c1b1a` over the texture: 4.72:1 against the r07 measured minimum `#858688`. That passes, but it is not in the tested pairs (`verify.cjs:38`).
- **(c) "33 contrast pairs across all three surfaces" (`session.md:65`).** These are calculated colour-value pairs, not rendered samples. Brushed gets four pairs, none of them for ink. Porcelain's border colour is never paired with the canvas minimum (see F2).
- **(d) "Same approved image" for the hero (`session.md:23`).** The hero is a Creator-requested, reference-derived extraction. The POC itself says it is "not a product photograph or a newly approved cake specification" (`poc/app.js:144`).

**Fix**

- Correct the wording in all four places.
- Add the Brushed ink pair and measured-minimum pairs to `verify.cjs`, or state that the pairs are calculated rather than measured.

### F4 — minor — The hero photo panel is the weakest point against "rich / calm"

**Evidence**

- In `index-desktop.png`, the photo backdrop samples `#8e8e8e`–`#a5a5a5`: neutral, fairly dark grey. It covers about 48% of the first screen, right next to a warm `#efede9` canvas.
- The result reads as a grey block placed beside the page rather than as calm editorial photography:
  - the colour temperatures clash;
  - the luminance step is large;
  - the panel's left edge (x≈666 at 1280px; ≈749 at 1440px) does not line up with the content grid.
- In Satin (`compare-satin-desktop.png`, canvas ≈`#e3e5e7`), the same panel looks natural.
- In Brushed, it is a flat grey rectangle on streaked metal.
- At 768px, it becomes a 369×540 strip beside two stacked buttons of unequal width (`poc/styles.css:161-169`; my 768px capture).

**Fix** — any of:

- pair the panel with the cooler Satin surface;
- frame the photo inside the grid with generous margins;
- tone the panel area so it belongs to the canvas.

Show the Creator the 768px state as well.

### F5 — minor — Functional text is very small, tracked uppercase

**Evidence**

| Element | Size | Source |
| --- | --- | --- |
| Buttons, including the primary hero CTA | 11px, .18em letter-spacing | `styles.css:149` |
| Mobile hero buttons | 10px | `styles.css:287` |
| Navigation | 11px | `styles.css:136` |
| Promise statements | 10.5px, muted | `styles.css:174` |
| Filter labels | 10px | `styles.css:214` |
| Footer headings | 10px | `styles.css:205` |
| Mobile text links ("Shop all cakes →", shop "Choose date →") | 9px | computed |

On desktop this looks refined. On phones it strains legibility, and the primary CTA is the smallest text on the hero. With the icons gone, the promise row is four lines of faint 10.5px uppercase. "Explore our ingredients" sounds like a link but is not one; the wording predates r08.

**Fix**

- Set a 12px minimum for functional text and 13px for primary CTAs.
- Reduce letter-spacing to about .08–.12em.
- Keep very small uppercase for eyebrows only.

### F6 — minor — Product cards put the price above the name

**Evidence**

- The name is 12px Manrope caps. The price is 24px Instrument Serif (20px on mobile) (`poc/styles.css:191-192`).
- In `shop-desktop.png`, "By request" in the display serif is the loudest text on the gallery-inspiration cards (5 of 8 in the grid; 2 of 4 on Home). It outweighs the names and the real prices of the orderable examples.

**Fix**

- Make the name the first thing read.
- Quieten the price (15–16px Manrope).
- Style "By request" differently from real prices, for example muted and small.

### F7 — minor — The preview bar crowds phone screens

**Evidence**

- The bar is 79px tall at 390px wide and 94px at 320px, because the notice wraps.
- At 390px, the "HEADINGS" label sits on line 2 (y=28) while its options wrap onto line 3 (y=49), visible in `index-mobile.png`.
- It uses space above the fold on every page.

**Fix**

- Keep each label together with its options (one non-wrapping unit per group).
- Or, at ≤760px, fold the switch into a single "Design options" disclosure, keeping the honest POC notice first.

### F8 — nit — Side effects of the design switch

**Evidence**

- Switching reloads the page. On `custom.html`, the typed description was empty after switching (verified), and local files are lost too. This matches the in-page-only policy but surprises a reviewer comparing options mid-form.
- "Clear this tab's demo data" (`poc/app.js:189`) does not clear `gk-poc-r08-design`. The policy text lists that key as stored data (`poc/app.js:144`).

**Fix**

- Note the reload next to the switch, or apply the option without a reload.
- Make the clear button also clear the design key, or say that it is kept.

### F9 — nit — Leftover cool r07 greys on the warm Porcelain surface

**Evidence** — on the Porcelain calendar:

- disabled days are `#777` at 0.7 opacity;
- the selected time slot is `#171919`;
- other controls use ink `#1c1b1a`.

These are small temperature and token mismatches carried over from the r07 base layer.

**Fix** — map the remaining base-layer greys to r08 tokens.

### F10 — nit — Manrope provenance is not in the r08 folder

**Evidence** — `font-evidence.json` covers Instrument Serif only. Manrope provenance lives in `gk-015-r06/font-evidence.json`. Its hashes `3ae11c49…` and `58172e0c…` match the r08 files.

**Fix** — reference or copy the r06 record so r08 stands on its own.

### F11 — nit, already in r07 — The order-status buttons have no gap

**Evidence** — on the pending result page, the disabled "Waiting for the payment result" button and "Continue exploring →" touch:

- r08: x 340–642 and 642–813;
- r07: same pattern.

This detracts from the polished look.

**Fix** — add a gap to the action row.

## Assessment against the brief

1. **Rich / calm / modern / minimal.**
   - *Calm and minimal: yes.* Generous ~120px section rhythm, quiet promise row, facts separated by space instead of dividers, a clear primary → secondary → tertiary order on cake detail, soft contact shadows on the cutouts. The serif headings add an editorial voice.
   - *"Rich" is only partly met.* Weak spots: the hero panel (F4), very small functional text (F5), product-card hierarchy (F6), and chrome now appearing only in the seals and the logo. That last point makes Porcelain closer to a generic beige-luxury look than to the distinctive chrome identity the Creator picked.
   - *Mobile* is clean apart from F5 and F7.
2. **Earlier requests.**
   - *Preserved:*
     - no decorative horizontal section borders (suite check plus renders; the remaining hairlines are content separators that already existed in r07);
     - frameless, transparent inspiration aside on every surface (suite check plus `custom-desktop.png`);
     - Manrope for all UI (computed `fontFamily`);
     - styled shop dropdowns (`appearance: base-select`; open picker captured with opaque menu and checkmarked dark selected row);
     - unavailable calendar dates are disabled and struck through;
     - both chrome seals render `assets/composite.png` (unchanged hash).
   - *Changed:* the r07 brushed surface and the reference treatment. The change is listed, but framed as a refinement (F1).
3. **Behaviour versus r07.**
   - Only `app.js` and `styles.css` differ; all HTML and existing assets are byte-identical to r07.
   - The JS diff covers only:
     - the design-option state and links (lines 106–107, 124, 126);
     - the session-storage prefix r07 → r08;
     - hero heading line breaks plus `<em>` (same words);
     - one label wrapper;
     - policy text naming r08 and the switch.
   - No product data changed.
   - Option links keep existing parameters (`cake.html?id=chocolate&surface=brushed`); invalid values are ignored; the choice persists.
   - No regression found.
4. **Accessibility.**
   - Text contrast passes on all three surfaces.
   - Focus is visible everywhere I checked, including the preview-bar option links on Porcelain and Brushed: 3px `#003354` outline at 4px offset.
   - Reduced motion removes the new transitions.
   - No horizontal overflow at 320px on 15 routes × 3 surfaces.
   - Gap: Porcelain control borders (F2).
5. **Honesty and scope.**
   - No new product facts, prices, availability or claims.
   - The switch is described as review-only in the policy text, but the "(r07)" label is misleading (F1).
   - No files outside r08 were modified after r08 work started (19:28).
   - Instrument Serif files match upstream Google Fonts byte-for-byte, and the OFL text is included.
6. **Evidence quality.** The suite tests most r08 claims directly. The unbacked or inaccurate ones are listed in F2 and F3.

## What I verified myself vs took from the author

**Verified myself:**

- **Manifests.** The r08 manifest hash matches. All 54 entries match, with no unlisted files apart from the excluded `review-r08.md`. The r07 manifest (`902b527b…`) still matches all 31 entries. The r07 files pinned in r08's `input-manifest.json` also match:
  - `verify.cjs`
  - `session.md`
  - `review-r07.md`
- **Shared contracts.** The seven GK-019 handoff contract hashes and the handoff file match.
- **Untouched files.** No modifications outside r08 after 19:28, judged by mtime.
- **Code.** Full r07 → r08 diff of `app.js` and of the CSS (the first 111 lines are identical to r07).
- **Served bytes.** The server at :8772 serves the pinned `app.js`, `styles.css`, `index.html` and font bytes.
- **Fonts.** Instrument Serif upstream hashes fetched live and matched.
- **My own Playwright runs:**
  - computed type sizes, r07 vs r08;
  - 320px overflow, 15 routes × 3 surfaces;
  - preview-bar layout at 320/390/768/1440;
  - hero geometry at 768–1280;
  - rendered-canvas pixel sampling;
  - focus outlines;
  - Porcelain/Brushed calendar states;
  - the date → slot → bag → checkout → "result unknown" flow and its wording;
  - the styled select opened on Porcelain/Brushed;
  - mobile menu;
  - switch side effects;
  - clear-data behaviour;
  - inner pages on Satin/Brushed.
- **Suite reproduction.** I ran `verify.cjs` from a **temporary copy** of the r08 folder (`$TMPDIR/opencode/rerun/`) against the live r08 server: **PASS — 24 groups, 16 pages × 4 widths, 33 pairs**. Its sources, checks and contrast arrays are identical to the committed `verification.json`.
- **Renders.** All r08 renders inspected visually.

**Taken from the author:** checkout outcomes other than "result unknown", and the deeper calendar-keyboard and file-handling paths. These rely on the reproduced suite run, not on my own manual stepping.

## Limits

- I did **not** run `verify.cjs` inside r08, so r08's PNGs and `verification.json` are untouched.
- I started a temporary read-only `http.server` on :8781 for r07 comparison and stopped it afterwards.
- Testing used current desktop Chrome via Playwright only. There was no real-device, Safari/Firefox or screen-reader testing, and no full WCAG audit.
- Design judgements (F4–F7) are reviewer opinion, offered for the Creator's consideration. They are not requirements.
- Approval status is unchanged: **none for r08**.

---

## Re-review — 2026-11-02 (focused)

This addendum covers revision **GK-015-r08**, `output-manifest.json` SHA-256
`c02d5baa1120ec12fa1bac5b2719c0dfbd3554760ee8978e624ed3b58f97c1ad`. That manifest lists 55 files; `review-r08.md` and the manifest itself are excluded. `verification.json` pins `poc/app.js` `0bbdc750…` and `poc/styles.css` `cc43cc6c…`. The original findings above are unchanged. Same reviewer and the same limits apply. This is still **not** visual approval.

### Updated verdict: PASS for Creator review (bounded, technical)

The major finding (F1) and F2/F3 are resolved. F6, F8, F9 and F10 are resolved. F4 is resolved for the proposal. F5, F7 and F11 are partly resolved.

The in-place switch introduces no regression in focus, `aria-current`, history, DOM identity or form state. Name-led cards hold at 320px.

What remains is two minor items and four nits (R1–R6). None should stop the Creator from reviewing this revision; fix them in the next iteration, or before presenting if cheap.

### Integrity

- **r08 manifest:** the hash matches. All 55 entries match, and there are no unlisted files apart from the excluded `review-r08.md`.
- **r07 manifest:** `902b527b…` still matches all 31 entries.
- **Files outside r08:** none modified since the first review.
- **This report:** my original text was unaltered when I started this addendum (278 lines, F1–F11 intact).
- **Code change since the first review:** only `poc/app.js` and `poc/styles.css` changed.
  - `app.js`: option order and label, grouped markup, a delegated in-place click handler, policy text, and the `design` key added to Clear.
  - `styles.css`: an appended block of 18 lines. The base 111 lines are still byte-identical to r07.
- **Served bytes:** the server at :8772 serves the pinned `app.js` and `styles.css`.
- **Suite:** I reran `verify.cjs` from a temporary copy of the new folder, against the live server, without touching r08: **PASS — 24 groups, 16 pages × 4 widths, 38 pairs**. Sources, checks and contrast arrays are identical to the committed `verification.json`.

### Disposition of the original findings (each checked myself)

| # | Status | Evidence |
| --- | --- | --- |
| F1 | **Resolved** (residual R1) | Default is `satin` (`app.js:106`; computed dataset `{surface:'satin',type:'serif'}`). Option renamed "Brushed (r07 surface)". `session.md:25-36` has an explicit accept/reject departures table. `session.md:49-50` points to the preserved r07 revision for the exact r07 look. Policy text updated (`app.js:146`). |
| F2 | **Resolved** | Computed Porcelain `--edge` is `#7f7871`. Against measured canvas minima: 3.36 on `#e5e2dc` (author), 3.40 on `#e6e3de` (my earlier sample). Against field and canvas: 4.17 and 3.72. Pairs added to `verify.cjs:38`. |
| F3 | **Resolved** | `session.md:40-43` and `:56` checked against computed values: buttons 12px/.16em; filter labels 11px uppercase; nav and promise 11px; mobile hero 11px; names 25/20px serif; Brushed ink 4.72:1 tested. Contrast is now described as computed pairs with measured minima (`:75-77`). Hero is described as reference-derived POC imagery (`:20`). Trivial omission in R6. |
| F4 | **Resolved for the proposal** | On Satin the cool hero backdrop sits naturally (`index-desktop.png`). Porcelain keeps the panel, as disclosed. |
| F5 | **Partly resolved** (R2) | Desktop buttons are now 12px, mobile hero 11px, filter labels and promise 11px. Mobile text links remain 9px. |
| F6 | **Resolved** | Name in 25px Instrument Serif (20px mobile); price in 14/13px Manrope. "By request" is now as quiet as real prices. At 320px all 8 shop names and 4 Home names fit on one line with no clipping (`scrollWidth ≤ clientWidth`). |
| F7 | **Resolved for label wrapping** (R5) | `.design-group` keeps each label on its options' row at 320/390/768/1440: the label sits about 5px lower because of font size, but on the same line. |
| F8 | **Resolved** (R3) | See "In-place switch" below. Clear removes `gk-poc-r08-design`, and the next load returns to Satin. |
| F9 | **Resolved** | Porcelain disabled days are `#57524d` at 0.55 opacity with line-through; weekdays use muted. Satin uses `#45494c`. Brushed keeps its r07 rule. |
| F10 | **Resolved** | `font-evidence.json` now has a `manrope` block. The record hash `da1eb7e7…` matches `gk-015-r06/font-evidence.json`, and the file hashes match. |
| F11 | **Resolved at desktop** (R4) | 20px gap on the pending and failed order pages at 1440px. |

### In-place switch — regression checks

These are my own Playwright runs, using both mouse and keyboard.

- **Updates correctly.** It updates `data-surface`/`data-type`, sessionStorage, and the URL through `replaceState`. `history.length` stays the same (2 → 2), so Back is not polluted.
- **`aria-current`** moves to exactly one option per group.
- **Option links rebuild correctly.** Their hrefs are rebuilt from the new URL, and other parameters are kept (`custom.html?ref=midnight&surface=brushed`).
- **Focus.**
  - Focus lands on the chosen option.
  - After keyboard Enter it shows the 3px focus ring; after a mouse click it shows none.
  - Shift+Tab and Tab continue in the expected order (Serif ↔ Sans → Home).
- **Nothing else re-renders.**
  - `main`, `.header` and `.demo-bar` are the same DOM nodes before and after.
  - Scroll is preserved when the bar is in view (6 → 6). The bar is not sticky, so it can only be clicked near the top.
  - Mobile menu stays open (`aria-expanded` still true).
- **Custom request form keeps:** description, servings radio, flavour select, colours, an attached local file preview, and the open inline calendar.
- **Edge cases.**
  - Cmd/Ctrl-click falls through to a normal new tab.
  - Reload keeps the choice, because it is now in the URL.
  - No page errors.

### Remaining findings

**R1 — minor — The "Sans" option does not restore the departed heading treatment.**

- **Evidence:** The departures table (`session.md:30`) pairs "light uppercase sans headings" with "Sans remains selectable". But Sans renders sentence-case Manrope 300 (computed `text-transform: none`; `compare-sans-desktop.png`). So the Creator still cannot see the reference/r07 uppercase heading treatment inside r08.
- **Fix:** Either give Sans the r07 uppercase tracked treatment, or reword the table: "Sans: Manrope sentence case; the uppercase treatment is visible in r07".

**R2 — minor — 9px tracked uppercase text links remain on phones (rest of F5).**

- **Evidence:** "Shop all cakes →" on Home and "Choose date →" on Shop compute to 9px at 390px. The source is r07 base rules (`.date-prompt .text-link{font-size:9px}` and the equivalent `.text-link` mobile rule). "Choose date" is the date-first entry point on Shop.
- **Fix:** At ≤760px, set these to ≥11px, or ≥12px to match the buttons.

**R3 — nit — On Back/Forward, the URL's design parameter overrides the per-tab choice.**

- **Evidence:** This follows from the URL taking priority over sessionStorage on load. I reproduced it:
  1. On Home, choose Porcelain → URL becomes `index.html?surface=porcelain`.
  2. Navigate to Shop and choose Brushed → `shop.html?surface=brushed`.
  3. Press Back → Home shows **Porcelain**, and storage is overwritten to Porcelain.
  4. Press Forward → Brushed again.

  This is consistent "URL wins" behaviour, and earlier, link-based revisions behaved similarly. It can still surprise a reviewer comparing options.
- **Fix:** Either document it next to the switch, or let a parameter apply only on an explicit shared-link entry (for example, drop design parameters from the URL after applying them and offer a separate "copy link").

**R4 — nit — The F11 margin indents a wrapped link.**

- **Evidence:** `button+.text-link{margin-inline-start:20px}`. At 320px on the pending and failed order pages, the link wraps under the full-width button and starts at x=40 while the button starts at x=20.
- **Fix:** Use `display:flex; flex-wrap:wrap; gap` on the action container instead of an inline margin.

**R5 — nit — The pre-script canvas is still Porcelain.**

- **Evidence:** The base `:root` tokens are Porcelain, and the HTML loads `app.js` with `defer`. With `app.js` delayed by 1.5s, the first paint is the warm Porcelain gradient and `data-surface` is unset (my `fouc` capture). On a fast local load this is not noticeable.
- **Fix:** Make Satin the base token set (and adjust the `:root:not(...)` edge rule), or put `data-surface="satin"` on `<html>`.

**R6 — nit — Small documentation and bar residue.**

- The phone preview bar is still three rows (76px at 390px, 89px at 320px). Acceptable for a review-only control.
- `session.md:40-43` omits the change to accordion summaries (12px → 11px).

### What I verified myself vs took from the author

**Verified myself:**

- manifest and source hashes;
- full `app.js` and CSS diffs against the previous r08 bytes;
- the suite rerun from a temporary copy;
- in-place switch behaviour: mouse, keyboard, history, DOM identity, form/file/calendar/menu state, and Clear;
- computed type sizes;
- calendar colours on all three surfaces;
- name-led cards at 320/390;
- 320px overflow on 16 routes × 3 surfaces × 2 heading options (0 cases);
- the preview-bar label alignment;
- order-action spacing at 1440/320;
- contrast arithmetic;
- the Manrope evidence hash;
- the refreshed Satin renders.

**Taken from the author:** the author's measured Porcelain minimum `#e5e2dc`. It is consistent with, and slightly darker than, my own earlier sample. Deeper behaviour paths remain covered by the reproduced suite only.

### Limits

As in the original review: Chrome via Playwright only, and no real-device, Safari/Firefox or screen-reader testing. R1–R6 and the design notes are reviewer opinion and judgement, not Creator decisions. **Creator approval of r08: none.**
