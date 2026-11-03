# GK-015 r08 — calm, rich minimalism (chrome-led)

2026-11-02. Creator request: “it should be very rich-looking/calm/modern (like minimalism
aesthetics)”. r08 is a candidate for Creator review and is **not approved**. r07 is preserved
byte-for-byte (its output manifest still verifies) and remains the exact last-reviewed look.

## Execution check and scope

Role: GK-015 visual author, continuing the current conversation (previous turn published
GK-019). Role default gpt-5.6-sol/high; host reports `llmplatform/claude-opus-5-5`, effort not
exposed — **mismatch with role default; current user-selected model takes precedence**. Not an
independent reviewer of its own work; a separate fresh-context reviewer wrote review-r08.md.
Inputs: r07 sources; all seven GK-019 r6.5 handoff hashes match (input-manifest.json); GK-019
Done on Trello; GK-015 live card read (Backlog, no comments). Writes limited to this r08 folder.
No shared contracts, owner data, ADRs, Trello, Git, deployment or production code.

## What r08 keeps, and where it deliberately departs

Kept from the accepted direction: silver/chrome identity (now a calm **Satin silver** canvas),
the Creator-selected hero image (reference-derived POC imagery, not a product photograph),
unframed owner cutouts, composite.png chrome seals, header wordmark, Manrope as the UI family,
no decorative section borders (r05), frameless inspiration aside (r06), styled native shop
dropdowns (r06), and every r04–r07 behaviour.

Deliberate departures, each for the Creator to accept or reject:

| Accepted before | r08 proposal | Why |
| --- | --- | --- |
| r07 full-page brushed-metal photo (Creator request) | Smooth **Satin silver** sheen; Brushed remains selectable | Streaks/flares competed with cakes; calm needs a quiet field while staying chrome-led |
| Reference/r06 light uppercase sans headings | **Instrument Serif** sentence-case headings, italic accent; Sans remains selectable | Adds richness and an editorial voice |
| Masked hero photo (soft visible box) | Crisp **full-bleed editorial panel** (cover, no mask) | Its grey studio backdrop cannot blend invisibly into any light canvas |
| Uppercase tracked product names under prices | **Name-led cards**: serif name, Manrope price | Clearer hierarchy and a richer catalogue |
| 7px radius, three equal boxed actions | 2px architectural buttons; date secondary, Add to bag primary, custom version as text link | One obvious next action |
| Icons + rules in the promise strip; fact dividers | Text-only statements; spacing instead of lines | Minimal, consistent with r05 |

Option also offered: **Porcelain** (warm `#efede9`) as a softer, warmer alternative surface.

## Typography (corrected)

Instrument Serif 400/italic for headings and product names. Manrope unchanged as a family but
some functional sizes changed from r06/r07: buttons 12px with .16em tracking (r07 12px/1px),
filter labels 11px uppercase (r06 12px sentence case), nav 11px, promise statements 11px; hero
buttons 11px on mobile. Product names moved from 14px Manrope caps to 25px serif (20px mobile).

## Design options (preview bar, review-only)

**Surface** Satin silver (proposal) · Porcelain · Brushed (r07 surface); **Headings** Serif
(proposal) · Sans. Switching happens in place (typed text and local files are kept) and is stored
per tab; “Clear this tab’s demo data” also resets it. Brushed shows the r07 texture inside the r08
layout — for the exact r07 look, open the preserved r07 revision. After the Creator’s choice the
switch is removed and one direction continues.

Tokens — Satin: ink `#1c1b1a`, muted `#45494c`, sheen `#d4d8da→#f1f2f3`, panel `#eef0f1`, tint
`#d7dbdd`, field `#f7f8f8`, control edge `#6e7377`, focus `#003354`, error `#7a1f14`. Porcelain:
muted `#57524d`, canvas `#efede9`, tint `#e6e3de`, edge `#7f7871`. Brushed: r07 texture and r07
muted/edge/focus/error values; ink becomes `#1c1b1a` (4.72:1 on the texture minimum, tested).
Source: the r08 blocks at the end of `poc/styles.css`.

## Preview

`python3 -m http.server 8772 --bind 127.0.0.1 --directory docs/design/sessions/visual-foundation/gk-015-r08/poc`
then http://127.0.0.1:8772/index.html (also shop.html, cake.html?id=pink, custom.html).
Renders (viewport captures): index, home-story, shop, cake, custom × desktop/mobile (Satin);
compare-porcelain desktop/mobile, compare-brushed, compare-sans; fallback-mobile.

## Verification

`verify.cjs` keeps the full r07 suite and adds r08 checks (see verification.json): defaults,
both Instrument Serif styles loaded locally, Manrope on UI, unmasked edge-to-edge hero panel,
in-place switching with preserved form input, persistence, invalid parameters ignored, demo-data
reset, three surfaces × 3 widths × 8 routes without overflow, per-surface muted colour, frameless
aside, removed dividers, tertiary action, label alignment, name-led cards, 12px buttons,
order-action spacing, grouped option labels at 320/390, keyboard focus on option links.

Contrast: 38 **computed** colour pairs. Canvas pairs use the darkest **measured** rendered
pixels (Porcelain `#e5e2dc`, Satin `#d4d8da`, Brushed texture `#858688`); other pairs use token
values, not per-element rendered sampling. Local POC evidence only — not a full accessibility
audit, real cross-browser test or production check.

## Review

Independent fresh-context review: review-r08.md (initial verdict REVISE, findings F1–F11).
Dispositions are recorded in that file’s re-review addendum.

## Approval status and questions for the Creator

Approval: **none yet**. Questions:

1. Surface: Satin silver (proposal), Porcelain, or keep Brushed metal?
2. Headings: Serif (proposal) or Sans?
3. Hero: is the full-bleed photo panel right for the cake-on-stand image?
4. The chrome seal appears in both the bespoke band and the footer on Home/Shop — keep both or one?
5. Anything too sparse, or where you want more warmth or colour?

Remaining gates unchanged: exact-revision Creator approval, canonical logo record (composite.png
is the Creator’s POC choice), server availability/owned-order picker/payment qualification in
implementation, unpublished Trello amendment. GK-015 is not Done.
