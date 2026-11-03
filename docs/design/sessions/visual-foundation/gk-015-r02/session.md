# GK-015 visual foundation — r02, 2026-10-05

Status: two proposed directions shown to Sasha; choice and exact-version approval pending.
This folder alone is owned by this session. Prior v01 is preserved and unapproved.
No shared contract, canonical owner data, ADR, application code or Trello was changed.

## Execution check / scope readback

Role: interactive feature designer. Routing default: gpt-5.6-sol/high; current user-selected host model takes precedence. Current model/effort not exposed by host metadata; status **unknown**, no model switch claimed. History: current user-started context; author, not independent. Inputs sufficient for reversible visual drafts; consequential integration acceptance pending GK-019 and independent review. Available host models/efforts verified from tool schema; independent reviewer will request gpt-6-astra/xhigh with fork_turns=none. Do not infer its effective identity from prose.

Outcome: show two distinct chrome palettes, develop editable C-GALLERY/C-DETAIL mobile/desktop sketches, iterate with Creator, and pin approved revision plus independent integration evidence. No deployment, production implementation or publishing. Preserve pre-existing staged assets and untracked planning work. Input commit bb96a89681bcf2fa1c663a02eacbe039e20e6363 alone does not identify those dirty files; see input-manifest.json.

Live reads 2026-10-05: GK-015 Backlog, no comments; GK-002 Done with published r4 comment; GK-019 Backlog. Local files and live evidence stored in this folder. Existing GK-015 description still says authentic assets pending; that statement is superseded by GK-002 closure evidence. Publication is not authorized: prepare amendments only.

## Sources / current business rules

GK-002 interview-r4 and published closure select photo_assets/logo/3000px-03.png (not composite.png), approved processed photos, ownership/use and exact three representative examples. They are development configurations, not live stock or historical flavour claims. Source photos untouched. The transparent cake photographs reveal the proposed chrome/background treatment behind them.

- Pink piping: Vanilla Raspberry, S 6 inches, 7–10 servings, 1.5–1.7 kg, £90, made-to-order.
- Chocolate swirl: Snickers, S 6 inches, 7–10 servings, 1.5–1.7 kg, £90, ready-stock example, prominent PEANUTS.
- Raspberry ribbons: Chocolate Raspberry, M 7 inches, 12–14 servings, 2.2–2.7 kg, £105, unavailable example.
- Exact compositions, common allergens and full cross-contact notice from GK-002/GK-005. Swiss meringue buttercream coating. Final product/supplier/label verification remains GK-037.
- Decoration shown included; extras/changes require chef price agreement. £90 whole GBP display; fractional amounts must preserve pennies. No invented discounts/stock urgency.
- BO-GK-001.lead_time: 192 elapsed hours after successful full payment; no ready-stock shorter lead invented. Demo date 2026-10-22 is scripted, not a policy/capacity calculator.
- BO-GK-001.pickup_location: only St Katharine Docks public. Collection free, customer courier possible; GK delivery individually agreed from £100, separately paid under current GK-003. No exact address shown.
- Current GK-003: website full payment; 24-hour template payment window; request/quote does not book; uncertain payment cannot be retried as a new payment. Custom action references @gk.cake.london; no send claimed.
- GK-005 2026-10-05 interview supplements older JSON, including whole GBP/pence display, current food content and separate prelaunch verification. Older source conflict notices remain history.

## Artifacts and interaction map

[Editable HTML/CSS/JS](sketch.html): `?theme=warm` (A) or `?theme=dark` (B). Same core journey/content. No dependencies, data persistence, network calls or application stack.

- C-GALLERY: early native date picker above product collection; labelled blank/error/loading/unavailable/available simulation. Exact demo available date: 22 October 2026. Any other date illustrates unavailable; no real policy inferred. Changing date invalidates prior demo result. Date advisory; nothing reserved.
- C-DETAIL: gallery links switch all exact product fields/images/allergens. Buy disabled until demo date checked; unavailable cake remains disabled. Buy reveals simulation of review → website payment; no real order. Size fixed to each approved example; other sizes go to chef custom request, not fabricated priced variants.
- C-CUSTOM fragment: custom action previews next form’s purpose; generic entry has no cake reference; detail entry references the selected cake. Both preserve the chosen date when present. Nothing saved/sent. Full constructor, handoff failure/copy fallback remain GK-016 and existing complete behaviour mapping; this fragment does not approve them.
- State board: default/focus/loading/disabled, date unavailable and recovery, failed date check, changed price requiring agreement, uncertain payment warning (GK-017 downstream). Annotated states, not backend tests.

Proposal A: #f8f2e9 paper, #fffdf8 panel, #282323 ink, #685951 muted, #842c48 accent, #075a9c focus. Georgia headings; Arial body.
Proposal B: #191d21 paper, #242a30 panel, #f7f3ee ink, #c3c8ce muted, #ecd2b0 accent with #191d21 label, #86d8ff focus. Arial hero, Georgia section headings, Arial body.
Chrome CSS gradient: #777c82 → #eceef1 → #a4a9b0 → #fff → #7d838b → #d8dce1 → #fafbfc → #898f98. Decorative only. Approved orange logo unchanged on chrome plate; no logo recolouring approval inferred.
Type/spacing/control dimensions editable in source. Body 16px/1.5; controls ≥48px; visible focus 3px +4px offset; no animation. Browser rendering and contrast evidence saved separately.

## Current references, read 2026-10-05

- [Crumbs & Doilies cake collection](https://www.crumbsanddoilies.co.uk/collections/cakes): separates bespoke discovery and shows product prices in the collection. Lesson: visible prices and distinct custom route. Text/page structure inspected; no measured usability claim or visual screenshot audit.
- [Lola’s](https://www.lolas.co.uk/): current ordering/catalogue reference opened. Do not import their delivery promises or availability rules. No copied brand assets.

## Remaining integration dependencies

GK-019 owns the accepted successor to contracts r5, canonical owner data and ADRs. Existing r5 warns that strict last-place/one-winner assumptions are displaced by current owner rules. This session does not resolve concurrency, hold/admission, month endpoints, ready-stock mapping or snapshot interfaces. Reconcile calendar hints and all buy/custom destinations against the exact accepted GK-019 successor before final acceptance. Dates/stock in mockup remain explicit simulations.

Affected GK-016/017/018 UI design sessions remain gated by their own approved revisions; this foundation is not blanket screen approval. GK-022 catalogue implementation must consume exact product/price/allergen evidence. GK-037 verification and Mini 5-inch versus recipe 4-inch reconciliation remain their respective downstream concerns, not repeated owner questions.

## Feedback and acceptance

Shown A and B desktop previews; focused question: choose A, B or specified blend. Author recommends A. No user answer or approval yet. Initial mobile renders showed a browser capture duplication artifact; clean exports were captured in new browser contexts and inspected. No approval claimed for colour, typography, chrome or screen hierarchy.

AC1/2: draft source, clean previews, focused browser controls and contrast checks completed; verification.json pins the checked source hash. Desktop 1440×1160 and mobile 390×844, both palettes: no horizontal overflow or JS errors. Text pairs minimum 6.02:1; focus/background minimum 6.39:1; control borders minimum 3.80:1. These checks are scoped, not full accessibility certification. AC3: Creator approval and final independent GK-019 integration review pending. AC4: exact revision and proposed Trello amendment prepared locally; publishing and live acceptance evidence pending authorization.

Reviewer dispatch: accepted tool invocation gpt-6-astra/xhigh, fork_turns=none, /root/gk015_review; effective identity not exposed, **requested-only**. Separate context; no inherited author conversation. Preliminary review pending; final review requires GK-019 successor.

[Proposed exact Trello comment](trello-amendments-proposed.md) is prepared for review, not published. [Verification](verification.json). Local preview: http://127.0.0.1:8766/docs/design/sessions/visual-foundation/gk-015-r02/sketch.html?theme=warm ; switch palettes using top links. Server is temporary, localhost only.

Preliminary review fixes applied: inverse-toolbar focus now uses paper colour against ink background; generic custom entry no longer silently references Pink piping; corrected catalogue handoff ticket to GK-022. Focus and both custom routes verified in all four browser configurations. Exact checked source SHA-256: 3b82ab5bb58b4040971a753d36c9bde14e50929680361fa053ec478ef61368e0.

## Concurrent input refresh

GK-019 changed shared contracts to r6 draft and owner-data mapping to gk019-owner-r2 during this session. Preserve original input-manifest.json; observed successor hashes are in input-drift-gk019.json. Read r6 K01/K02/K04 and its question queue for draft compatibility only. r6 explicitly awaits independent review and is not accepted integration evidence. Preliminary review is bounded to original inputs plus approved owner evidence. Q1 shorter-lead ready/resale treatment and Q2 payment-success/192h boundary affect final date/buy behaviour; they remain with GK-019 and are not re-asked here. Generic/reference custom paths, immutable content and advisory availability remain compatible at the inspected draft level. Refresh exact accepted revision and re-review affected visual states before integration acceptance.

## Preview exports

| Proposed direction | Desktop | Mobile | Detail | States |
| --- | --- | --- | --- | --- |
| A: Pearl & raspberry | [screen](warm-desktop.png), [gallery](warm-gallery-desktop.png) | [screen](warm-mobile.png), [gallery](warm-gallery-mobile.png) | [desktop](warm-detail-desktop.png), [mobile](warm-detail-mobile.png) | [desktop](warm-states-desktop.png), [mobile](warm-states-mobile.png) |
| B: Graphite & champagne | [screen](dark-desktop.png), [gallery](dark-gallery-desktop.png) | [screen](dark-mobile.png), [gallery](dark-gallery-mobile.png) | [desktop](dark-detail-desktop.png), [mobile](dark-detail-mobile.png) | [desktop](dark-states-desktop.png), [mobile](dark-states-mobile.png) |

[Approved source-asset fingerprint check](asset-verification.json): logo and all three selected cake photos match GK-002 r4 inventory, 4/4. [Independent preliminary review](review-preliminary.md) is separate from Creator approval and final GK-019 integration acceptance.

## Preliminary review completed — coordinator addendum

Fresh independent report review-preliminary.md re-reviewed sketch 3b82ab5bb58b4040971a753d36c9bde14e50929680361fa053ec478ef61368e0 and resolved all three findings. Verdict: suitable for continued Creator comparison/iteration; final acceptance blocked on Creator and accepted GK-019 successor. Earlier pending-review entries are chronological history. This addendum follows the session hash pinned inside that report; it changes only completion status, not design or checked source. Current package hashes are in output-manifest.json. Creator question remains unanswered; no final revision approved.

## Subsequent asset blocker

The approved photo_assets/logo/3000px-03.png is now missing; all three selected cake images still match approved hashes. See asset-blocker.json. Existing PNG previews retain the logo as historical renders, but the editable sketch currently has a broken logo reference. Prior asset verification/output manifest predates this disappearance; do not claim current asset readiness. No restoration or substitution with composite.png was performed. Creator clarification is pending.
