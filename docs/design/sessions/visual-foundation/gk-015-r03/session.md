# GK-015 — selected reference POC r03

2026-10-06. Creator selected the supplied two-screen reference and requested: “Use this design choice as our target and build POC until it's ready!” This approves the visual target, not the exact finished POC revision. Earlier A/B proposals remain historical and are not the selected direction.

## Outcome and boundaries

Build a responsive, local, multi-page customer storefront matching the supplied reference: plain white shop, unframed PNG cake cutouts, dark slim type, chrome hero turntable and restrained silver decorative details. Primary home/shop plus cake detail, custom request, bag, checkout/state preview, account/order example, favourites, about, FAQ, contact and policy preview. All transactions, availability and account states are labelled simulations. No production implementation, real customer data, provider calls, analytics, messaging, deployment, shared-contract edits or Trello publication.

Files owned: this r03 folder only. Shared checkout has extensive existing staged/untracked work; preserved. The work-only commit/push policy does not apply to this personal design artifact. No Git staging, reset or publishing needed to present this local POC.

## Execution/context check

Author is continuing GK-015 context with current user-selected host settings; effective model/effort not exposed, status unknown. gk-feature/gk-plan execution guidance and work-implementation/ponytail engineering methods applied to disposable POC code. Host supports explicit separate reviewer selection; independent review will use requested gpt-6-astra/xhigh with fresh fork_turns=none. No consequential architecture decision is accepted here.

Fresh inputs fingerprinted in input-manifest.json. Current GK-019 handoff r4/contracts r6.3 reviewed with Q1/Q2/Q3 accepted; Q4 payment qualification and Q5 restitution remain unresolved. POC explicitly demonstrates pending/unknown (no repeat payment), known late success awaiting agreement, definitive failure, expired new initiation and recovery unavailable. It does not implement real state machines or select payment methods. No provider readiness inferred. Owner structured conditions/source records and approved GK-002 examples refreshed. These are sufficient for this bounded local POC, not production checkout.

## Observable acceptance criteria

1. Home and shop are distinct navigable page URLs and visually follow the supplied reference on desktop/mobile; real owner cake photographs, white grid without frames/props, chrome hero/detail accents.
2. Shop filtering/sorting, no-result recovery, product navigation, favourites and unavailable-product recovery work. Only three approved examples have prices/flavours/servings. Other gallery images are “By request”, with descriptive proposed display names and no invented product facts.
3. Date picker shows blank validation, checking, scripted available and unavailable outcomes. Date is advisory; cannot buy unavailable cake; no booking created. Approved examples, allergens (including prominent PEANUTS), compositions and exact cross-contact notice remain visible before simulated purchase.
4. Bag and checkout support the local browse-to-order demonstration with exact GBP arithmetic, free collection/customer courier, separate quoted GK delivery, checked allergen review, and distinct payment result screens. Unknown and paid-awaiting-agreement prevent a replacement checkout. No credentials/card data collected.
5. Generic custom flow has no implicit cake reference; product custom flow retains the selected image. Required date/servings/flavour/description, optional multiple local references, editable summary and copy-failure fallback work. No server save/upload/message; external app opening is deliberately simulated.
6. Mobile menu, keyboard/focus, text/control contrast, normal/disabled/loading/error states and reduced motion are checked. No broken internal links, missing POC assets, JavaScript errors or horizontal overflow in tested viewports. Independent review findings resolved before calling POC ready for Creator review.

## Visual/content evidence

[Selected Creator reference](creator-reference.png). [Asset source/derivative manifest](asset-manifest.json). Eight owner-approved processed photos; sips proportional downsize only, no retouch or invented cake. SVG viewboxes fit each PNG's alpha bounding box without modifying its pixels. Shop cutouts retain existing physical cake boards in the supplied photographs; no new stand or props are added there. The CSS chrome turntable appears only in the homepage hero.

The earlier GK-002 selected 3000px-03.png remains missing. No restore/substitution of canonical branding. The POC uses a code-native GK wordmark and chrome emblem matching the newly selected reference; these are explicitly POC artwork, not an amendment to GK-002 or final production logo approval. User reference selects composition/visual treatment, not the generated screenshot's invented cake prices/claims/contact details.

Maintained source: [poc/app.js](poc/app.js), [poc/styles.css](poc/styles.css), small separate HTML page entrypoints. No dependency install or application framework. Native forms/dialogs, sessionStorage for non-personal demo selections only; custom text/files stay in current page memory. No tracking, upload or network call by application code.

## Running and reviewing

From repo root: `python3 -m http.server 8767 --bind 127.0.0.1 --directory docs/design/sessions/visual-foundation/gk-015-r03/poc`.

Open http://127.0.0.1:8767/index.html or http://127.0.0.1:8767/shop.html . The isolated POC asset copies make this folder self-contained. Use 22 or 24 October 2026 for available date fixtures, 23 October for unavailable. No live time/capacity calculator. Clear scenario/session data on policies.html before testing a new pending/late scenario.

Checks/reviewer/preview fingerprints will be recorded in final evidence. Final exact-version Creator approval, Q4/Q5 affected production behavior and final UI integration acceptance remain separate gates. No Trello changes published.

## Final candidate and reconciliation — 2026-10-06

Exact candidate: **GK-015-r03**. `output-manifest.json` SHA-256
`4064e85685a890a841f41145b18ff12bbbe69b84a66159d9b4975d89ac703817` pins every local POC file. Review and automated checks pin app.js and styles.css directly. All eight rendered home/shop/cake/custom mobile/desktop PNGs were refreshed after the final fixes.

Final browser checks: PASS, 15 check groups, 16 route variants at 320/390/768/1440px, seven contrast pairs. See `verification.json` and editable `verify.cjs`. Covered browse/filter/sort/empty results, date validation/loading/unavailable, exact bag arithmetic, unavailable buy exclusion, custom generic/reference/edit/clipboard recovery, seven checkout outcomes, unknown/late replacement guard, delivery agreement guard, cancellation/time-change examples, focus retention, disabled styling and internal route/asset loads. This is local POC verification, not a full accessibility audit or production/provider test. `node --check poc/app.js` and `python3 scripts/check_planning_kit.py` passed. No full GK-019 checker pass is claimed: its separately reported missing-logo link remains.

Independent reviewer `/root/gk015_r03_review`, fresh context, requested Astra/xhigh with effective identity unavailable, checked source, reference, eight renders and live browser interactions. Review findings addressed: selected Email now controls handoff button/success/failure text; favourites toggles preserve focus and item removal restores a meaningful surviving focus target; disabled primary buttons visibly differ; dialog named and duplicate decorative SVG IDs eliminated. Exact verdict and scope are in `review-r03.md`.

### Updated shared input, superseding the earlier r4 readback

GK-019 changed during execution. Final reconciliation consumed handoff r5, contracts r6.4, owner-r6 and schema r1.4; fresh hashes are in `input-manifest-final.json`. Q1/Q2/Q3 are accepted. Q4 optional customer currency is now accepted with GBP shop/accounting, while method/Onelink bundle/settlement qualification remains gated. Q5 verified alternative restitution is now accepted for definitively unavailable provider refunds; unknown return blocks every route. These Q4/Q5 choices are no longer unresolved owner questions. The POC neither selects a payment-method set nor simulates provider conversion/manual payout, so its displayed customer rules remain compatible. Current GK-019 independent review still identifies r6.3; acceptance of the latest shared packet must be rechecked before final integration acceptance. No shared source was changed here.

### Acceptance status

- Ticket AC1: responsive visual screens and required control/outcome states rendered and supplied for Creator review.
- Ticket AC2: editable HTML/CSS/JS with explicit colour/type/spacing, click paths and browser evidence supplied.
- Ticket AC3: requested screenshot/white-chrome direction applied; exact candidate Creator approval **pending**. Independent local review recorded separately; latest shared-contract acceptance pending as above.
- Ticket AC4: local exact-version evidence prepared; proposed Trello amendment in `trello-amendment.md` **unpublished**. GK-015 is not Done.

Remaining dependencies: Creator approval of this exact manifest; replacement/restoration or explicit approval of final canonical logo (current SVG is POC artwork); GK-019 accepted final shared packet and recheck of affected behavior; authorization to publish the exact Trello amendment. None prevents trying this local POC. No release/deployment authorized or performed.

Approval record: selected reference approved by the user's attached-image instruction; final candidate r03 approval has **not** been received. Do not infer it from silence, reference selection, test success or independent review.

## Subsequent accepted GK-019 handoff — 2026-10-06

The earlier “latest shared review pending” status is superseded. The GK-019 context delivered independent conditional PASS for contracts r6.4/owner-r6/schema r1.4 at exact95-file manifest `f15556358b4cac4217d96d9537c27981e32915a8dd87cdcfe2bfd7e818e54c23`. Current review.md was read and its manifest hash verified. The handoff document title still says review pending, but the newer exact-manifest review resolves that status. `reconciliation-evidence.json` records this read without rewriting the frozen input evidence from the original POC review.

No displayed POC behavior changes: GBP catalogue/accounting retained; no method set, FX or manual payout is implemented. Q5 verified alternative restitution requires definitive provider inability, shared reservation, no unknown earlier result, verified recipient/amount and successful proof. Current Q4 Onelink bundle, settlement compatibility and implementation provider qualification remain conditional. No shared sources or visual files were edited. The POC output remains `4064e85685a890a841f41145b18ff12bbbe69b84a66159d9b4975d89ac703817`.

GK-019 now reports default document/structured/coverage checks passing after its truthful missing-logo README reconciliation. This supersedes the earlier observed missing-link checker failure, but does not restore the asset or prove application/provider readiness. Exact Creator approval and the missing-approved-logo gate remain open; Trello amendment is updated locally and unpublished. Independent POC reviewer requested to append a bounded reconciliation addendum.
