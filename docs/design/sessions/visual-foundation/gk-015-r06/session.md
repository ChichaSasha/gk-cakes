# GK-015 r06 — typography, filters and unframed inspiration

2026-10-06. Creator accepted r05 as the basis (“Yes, but also…”), then requested researched elegant/simple fonts, removal of the inspiration panel's different background/frame, and modern shop dropdowns. This exact new r06 revision remains pending Creator approval. Preserve r05, including its approval/review history.

## Execution and boundaries

Continued bounded visual author; requested/effective model and effort remain unknown because the host exposes no author identity evidence. No model switch claimed. Separate qualified reviewer continues its independent Astra/xhigh requested-only assignment. Applicable AGENTS, gk-feature design-session and gk-plan execution/context guidance reused and refreshed; native/CSS-first simplicity guidance applied. All pinned owner/contract inputs remain unchanged from r05 (input-manifest.json); source/asset fingerprints identify dirty working-tree inputs. The existing extensive staged/untracked work is preserved. Only this r06 visual packet changed. No shared contracts, canonical owner data, ADRs, business behavior, deployment, Git publication or Trello write.

## Design choice and research

Read 2026-10-06. Compared [Manrope](https://fonts.google.com/specimen/Manrope) and [Inter](https://rsms.me/inter/). Inter is a strong UI alternative, with text/display optical sizes. Chose **Manrope** for this proposal: its slightly rounded, clean forms feel softer alongside polished silver while remaining simple. This is an aesthetic recommendation, not an objective claim that one font is universally best.

One locally hosted variable family keeps the UI coherent: weight300 headings, weight500 labels at14px, 15px desktop/16px mobile form controls, relaxed line-height and less mechanical letter spacing. Navigation/short captions remain smaller. The source logo and reference header wordmark are unchanged. The downloaded font is unmodified; its SIL Open Font License and exact source/content hashes are included in font-evidence.json and poc/assets/fonts/. The POC makes no runtime request to a font CDN.

Compared exposing every filter as chips, a scripted custom combobox, and a styled native select. Four independent catalogue settings suit compact controls without turning all flavour/colour choices into a long chip bar. Chose separate rounded controls with visible labels, generous hit areas and a dark selected-row checkmark. [MDN customizable select guidance](https://developer.mozilla.org/en-US/docs/Learn_web_development/Extensions/Forms/Customizable_select) documents native `base-select`, picker styling and progressive fallback. [GOV.UK select guidance](https://design-system.service.gov.uk/components/select/) cautions about selects for long/question-led forms; the existing servings/contact choices remain visible radios. Shop filters keep native select semantics, keyboard and change events. No hand-written ARIA widget or new JS dependency.

Supported browsers get styled menus, rounded options, hover, selection and focus. Others receive the same rounded closed controls with a chevron and the system option picker. A forced fallback stylesheet branch is tested in current Chrome; this is not certification of a real older browser. Desktop has four controls across; small screens use two columns, and long selected flavour names wrap in enhanced controls. Contrast uses dark#363838 boundaries; menus are opaque so cakes do not obscure option labels.

## Inspiration surface

Only the custom-page inspiration aside, both blank-start and cake-referenced variants, loses its frame and surface fill. It is transparent over the exact continuous silver page gradient, with comfortable spacing and the same authentic cake image. Bag and checkout summaries retain their existing presentation. No cake or logo pixels were modified.

## Preserved behavior and integration

All r05 interactions remain: disabled unavailable purchase dates, chef-subject custom date requests, confirmed six serving ranges plus Other, local file previews/removal, icon channel cards and honest simulated handoff. Approved product facts, GBP prices/allergens and authentic processed photos unchanged. Existing GK-019 r6.5/owner-r7/schema1.5 integration remains; actual cart/server availability, owned-order move picker and payment/provider qualification are separate gates. No current policy was inferred from typography changes.

## Editable preview and evidence

Source: poc/app.js, poc/styles.css, local assets and13HTML page entrypoints. Serve `python3 -m http.server 8770 --bind 127.0.0.1 --directory docs/design/sessions/visual-foundation/gk-015-r06/poc`. Open http://127.0.0.1:8770/shop.html and custom.html?ref=midnight . Previous r05 remains at8769.

Exact source/asset version: output-manifest.json. Automated evidence: verification.json and verify.cjs. Screens include desktop/mobile home/shop/cake/custom, opened filter menus, long selected names and a forced native fallback. Independent verdict: review-r06.md. Review does not substitute for Creator approval. Trello amendment is a local draft only; no Done or production readiness claimed.

Final candidate output-manifest SHA256 `a770fd7043020fe0a22cb35481c066e683d3659298e29bd76851d00948ba27a9`. Final automated result: **PASS —21groups,16route variants×4widths,13contrast pairs**, exact source hashes in verification.json. Syntax/planning-kit pass. Independent review caught a missing fallback chevron and a follow-on appearance cascade conflict; both corrected and actual computed enhanced/fallback appearances tested. The new boundary contrast was strengthened to#363838. Final desktop/mobile/open-menu images refreshed. r05 preserved byte-for-byte.

Independent final review: **bounded PASS**, no unresolved findings. Report SHA256 `5d89075d4267d9c50caeded3b2d8e6b6fb6974a8276744a8480125b3999d74b0`. Separate source/render/hash review; browser assertions authored and executed in the author context. Real cross-browser/production gates and exact Creator approval remain separate.
