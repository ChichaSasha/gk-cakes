# GK-015 Visual foundation — r10 "liquid chrome atelier"

- Date: 2026-11-02 · Status: **candidate for Creator review — not approved**. GK-015 stays in Trello Backlog.
- Built on: r09 (Creator-approved hero cutout, unchanged bytes) and the r07 base stylesheet. r07, r08 and r09 are preserved untouched.
- Preview: `python3 -m http.server 8774 --bind 127.0.0.1 --directory docs/design/sessions/visual-foundation/gk-015-r10/poc` → http://127.0.0.1:8774/index.html
- Model: host `llmplatform/claude-opus-5-5` (role default differs; user-selected model takes precedence).

## Creator feedback on r08 (verbatim)

> All good with image, but not with design, as it looks very cheap and not modern if we compare it with other 'best shop designs web' websites. This needs to be reworked along with smothing coloring and working on structuring of the objects and effects.
>
> Satin silver is fine, but probably a bit ligher version would work better.
> Dropdowns and buttons are too simple and not for design lovers.
>
> The final web version should look more modern, more design pleasuristic and just have cutting edge solutions in design, so it doesn't look as was created 5 years ago. This site should angage creative people and make them think it's a pleasure to buy something from this place!

## Response, point by point

| Feedback | r10 |
|---|---|
| Image is good | r09 transparent cake-stand cutout kept byte-identical (sha256 `137f419f…`), standing directly on the page; r09 shadow values kept. |
| Smoother colouring, lighter satin | Lighter pearlescent satin canvas (`#f8f9fa→#f1f2f5`) with soft blush, ice and lilac light; slow 40s drift; fine grain. Solid pearl tiles for content. |
| Structure of objects | Floating glass header island; editorial asymmetric collection grid (7/5 then 5/7) with numbered tiles and cakes grounded on their shadows; dark graphite bespoke tile; oversized chrome footer wordmark; mono ticker. |
| Effects | Glass (backdrop blur) only on the floating control layer — header, menus, chips, picker, product card, seal — per current Apple "Liquid Glass" guidance; pointer spotlight on tiles; hover lift + "View cake" pill; chrome text sheen; rotating text-ring seal; scroll reveals; cake morphs between shop tile and cake page (cross-document view transitions). |
| Buttons too simple | Ink-gradient pill with a chrome orb arrow that turns on hover, specular sweep and gentle magnetic pull; glass secondary pill; quiet ghost tertiary; drawn underline text links. |
| Dropdowns too simple | Customizable selects: glass toolbar, captions inside 64px fields, chrome chevron orb that flips, frosted animated menu, ink selected option with check; active filters marked. Native fallback keeps a chevron with room for long values. |

## Deliberate departures (Creator to confirm)

1. **Typeface:** Geist (display/UI) + Geist Mono (captions, prices) replace Manrope (accepted in r06); Instrument Serif is now italic accent words only. See `font-evidence.json`.
2. **One direction:** the r08 surface/heading switch is removed (Satin chosen, lightened).
3. **Floating "Pink piping" card** shows on desktop only — on tablets/phones it would cover the stand; the collection directly below shows the same cake.
4. **Hero fact chips** hidden on phones (the ticker directly below repeats them).
5. r09 promise strip replaced by the ticker.

## Accessibility and fallbacks

Reduced motion (no looping or reveal motion), reduced transparency (solid island and menus), increased contrast (darker muted text), print (nothing left hidden), no-JS (canvas painted). 63 contrast pairs checked; control edges ≥3:1; 3px focus ring. Browser support: view transitions Chrome 126+/Safari 18.2+ (others navigate normally); customizable select Chrome 135+ (others get the styled native select); scroll reveals are progressive.

## Verification

`node verify.cjs` (POC on 8774): **PASS — 26 check groups; 16 pages × 4 widths; 63 contrast pairs.** Carries every r07–r09 behaviour check plus r10 checks (glass, backdrop-root guard, controls, morph names, motion preferences, fonts requested). Results in `verification.json`; renders `*.png` in this folder.

## Questions for the Creator

1. Does the lighter pearlescent satin with glass feel premium and current?
2. Geist instead of Manrope — keep?
3. Motion level (ticker, rotating seal, cake morph, reveals) — right, or calmer?
4. The dark graphite "Bespoke" tile — keep as the one dark moment?
5. Collection layout (asymmetric numbered tiles) — does it feel like a design-led shop?
