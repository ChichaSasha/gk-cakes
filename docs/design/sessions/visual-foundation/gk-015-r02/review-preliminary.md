# GK-015 r02 — independent preliminary integration review

Date: 2026-10-05. Verdict: **suitable for continued Creator comparison/iteration; final integration acceptance blocked** on exact-version Creator approval and the independently accepted GK-019 successor. No new open sketch defect remains from this bounded review after the three corrections below. This is not design approval, ticket completion, publication approval or implementation readiness.

## Execution and independence

- Reviewer: `/root/gk015_review`, separately assigned preliminary integration review.
- Requested model/effort: `gpt-6-astra` / `xhigh`; accepted spawn arguments and `fork_turns=none` confirmed by coordinator. Effective identity/effort metadata is not exposed: **requested-only**, never confirmed.
- History: fresh through `fork_turns=none`; no inherited author discussion and no authorship of this sketch. Independence: **independent**. The later messages contain factual fixes/check results for re-review, not inherited design rationale.
- Scope readback: compare the actual gallery/detail sketch and limited custom fragment with current owner evidence, source/asset provenance, accessibility, honest simulation and important states. Write only this report; no shared-contract, canonical-data, application, Trello, Git or deployment changes.
- Author gate: session records unknown effective model, continued user-started context and author role. That supports bounded reversible drafts under the execution-check policy; it is not model confirmation or independent acceptance.
- Inputs: sufficient for this preliminary visual/source assessment. Insufficient for final integration because GK-019 has no accepted successor and Creator has selected/approved no direction. No new business policy was inferred.

## Exact artifact and freshness boundary

- Checkout commit: `bb96a89681bcf2fa1c663a02eacbe039e20e6363`; substantial existing staged/untracked work was preserved. Commit alone does not identify this packet.
- Initial reviewed sketch: `770be9f342f62205485d8ba647840ab3e95ed65a26684c5e2543690775d57724`.
- Final re-reviewed sketch: **`3b82ab5bb58b4040971a753d36c9bde14e50929680361fa053ec478ef61368e0`**.
- [Original input manifest](input-manifest.json) records the 29 input file/asset fingerprints. The original local GK-015 publication snapshot and the supplied live-read [GK-015 card](gk015-card.json) have the same four design acceptance criteria; the live card's old “assets pending” description is superseded by the GK-002 closure evidence, not by an assumed screen approval.
- Initial integration comparison used contracts r5 (`d058cffd4c7da51a39c201b376db350b62717eb8cfa2e83f0ff757f4a4fb7290`) and owner-data contract (`30bd0355f991bf0d0e564a115e3179a7f4f42e7006d0fb53191223468a1556db`), together with their explicit current-owner reconciliation notices. Historical examples were not applied over newer owner decisions.
- **Concurrent change observed:** during this review GK-019 replaced both shared files. Observed contracts r6 hash: `16f5be88ee080cdd6217e311b2e678070ff519461f2e3428380f2e631ea44646`; owner-data hash: `c695019b03466b4a37015e4b48a598335d940d981db386eafc558d0f2d039d2c`. The r6 heading says technical draft / independent review pending. This report does **not** accept r6 or transfer an r5 comparison to r6. Reconcile/re-review against the exact accepted successor before final acceptance. All other original-manifest entries matched when checked at this boundary.

## Findings and dispositions

### Major M1 — generic custom entry silently chose Pink piping — resolved

Initial location: `sketch.html` hero/navigation links, `current='pink'` and custom-button handler (lines 11–12, 33, 40 in the initial source). Trigger: choose “Create your own” before selecting an inspiration, then preview the custom request. The summary referenced Pink piping even though the customer had chosen no cake. This contradicted `BO-GK-003.request_fields` (sufficient description can have no reference) and K02's generic-request option; it also made the primary bespoke action misleading.

Correction inspected in final source: generic links set `customReference=null`; only the detail's “Request a custom version” records the selected product. Intro and simulated summary use that mode and keep the selected date. Coordinator's exact-hash browser evidence covers generic and referenced routes in both themes and both viewports. The full form, immutable image revision, clipboard/Instagram fallback and real submission remain GK-016/027 work, not functionality claimed by this fragment.

### Major M2 — focus outline lost contrast on the inverted toolbar — resolved

Initial location: `sketch.html:5`, global `:focus-visible` and `.studio`. Trigger: keyboard-focus either theme link. The warm blue ring against the dark toolbar was **2.18:1**; the dark-theme cyan against its light toolbar was **1.43:1**. Body focus checks alone missed this different backdrop and did not satisfy the design's visible-focus evidence requirement.

Correction inspected in final source: `.studio a:focus-visible{outline-color:var(--paper)}` preserves the 3px ring and 4px offset. Independent calculation gives **13.93:1** for A and **15.34:1** for B against the toolbar. Coordinator browser evidence includes actual Tab focus on these links for all four configurations. This is a scoped contrast/focus correction, not an accessibility certification.

### Minor m3 — catalogue handoff named the identity ticket — resolved

Initial location: `session.md`, Remaining integration dependencies. It named GK-021 as catalogue implementation. Following that handoff would direct product/price/allergen work to the identity/authorization packet, whereas `screens.md` / `coverage.md` map catalogue work to GK-022. The final session now names **GK-022**.

## Requirements and behavior checked

| Area | Result / limit |
| --- | --- |
| GK-015 AC1 / visible foundation | Actual coloured A/B desktop and mobile exports exist, with authentic approved processed photos and separate buy/custom actions. Reviewer visually inspected warm/dark desktop hero, warm mobile hero and dark mobile detail. Labels, pricing, imagery, composition/allergens and layout are legible in those exports. This is bounded export inspection, not an exhaustive responsive audit. |
| GK-015 AC2 / editable choices and focus | Complete HTML/CSS/JS read; concrete tokens/type/spacing and action handlers are present. Chrome stays decorative; text uses solid colours. Independent focus-ratio recalculation above plus exact-hash coordinator browser/contrast evidence. |
| Current GK-002 | Correct `3000px-03.png`, exact three approved examples, £90/£90/£105, S/M diameters, servings/weights and product states. Decoration-included/chef-agreed-extras wording retained. Examples clearly distinguished from real stock and historical-photo flavour facts; processed imagery labelled. Website approval is not inferred from asset approval. |
| GK-001 / GK-003 | 192 elapsed hours from successful full payment; no invented short ready-stock lead. Only public St Katharine Docks disclosed. Free collection, customer courier and separately paid individually quoted GK delivery from £100 preserved. Date enquiry reserves nothing. Website payment path and uncertain-payment warning do not claim a completed payment. |
| GK-005 | Exact common contains declaration, Snickers PEANUTS, standard coating and separate full cross-contact notice preserved. Whole GBP values do not erase pennies from a real calculation (no monetary calculations occur here). No food-rating/legal-compliance claim or private address exposed. GK-037 retains actual product/label verification. |
| Simulation honesty | Prominent simulation label, explicit scripted available date, no booking/payment/request/message sent claim. No network, persistence, analytics or provider integration in source. Gallery product switching and separate custom modes are local demonstrations. |
| Important states | Blank date/error, checking/disabled, available/unavailable date, unavailable cake, focus, failure recovery, changed-price agreement and downstream payment uncertainty represented. Existing complete screen map is retained; this sketch does not claim every gallery fetch/empty/archive or custom-handoff state is rendered. |
| Journey boundaries | Browse → date → detail → buy ends in a truthful review/payment placeholder; inspiration/generic custom → request summary ends before real Instagram communication. Full payment/refund, fulfillment, concurrency, clock, email-outage and admin recovery contracts remain outside this visual fragment and await GK-019 plus their owned packets. |
| Dependencies | No new dependency cycle or orphan introduced by this sketch; no full project DAG review claimed. GK-016/017/018 require their own approved screen revisions, GK-022 exact catalogue evidence, GK-037 launch content checks. Current owner policy unresolved technical details are not reopened as routine owner questions. |

## Checks and acceptance still required

Read: root instructions, planning-review/execution/artifact/ticket/design-session rules, original GK-015 and supplied live card evidence, full sketch/session, project brief/delivery requirements, setup/coverage references, screen map, scoped shared contracts and owner-data mappings; GK-002 README/interview-r4/closure receipt; GK-001/GK-003 current handovers and README/decision/source records; GK-005 current interview, README/decision/source records. Source-linked original text was checked for the consequential UI facts, not just `business.value`.

[verification.json](verification.json) reports coordinator-executed checks against the final sketch hash: desktop 1440×1160 and mobile 390×844, both themes; image loads, no horizontal overflow/JS errors, date validation/loading/invalidation, available/unavailable product behavior, PEANUTS update, both custom modes, buy simulation and keyboard focus. Reviewer inspected that evidence and final handlers; these were **not separately browser-executed by the reviewer**. No application, API, database, payment, Instagram account, assistive-technology or production tests were run by this review.

The screenshot captures were refreshed while review proceeded. The four exports above were visually inspected, but this review does not assert a complete final-hash screenshot suite or full mobile/keyboard certification. Preserve/re-render exact-version exports as needed for the eventual Creator acceptance record; any later source change invalidates affected check results.

Remaining acceptance gates:

1. Creator chooses/revises a direction, sees the resulting mobile/desktop revision and explicitly approves its exact source/exports. Neither this review nor silence is approval.
2. GK-019 supplies its independently accepted contract/owner-data/ADR revision. Refresh hashes and review calendar hints, admission/stock/holds, collection/delivery timing, buy/custom destinations and snapshot boundaries against it. Current r6 draft is not that evidence.
3. Final independent integration review and any affected visual rechecks follow those changes. Capture actual approval and authorized Trello evidence; no publication was performed here.

## Supporting fingerprints

The following fingerprint table supplements the original manifest for this review. Shared-file drift is recorded above rather than silently replacing the original input record.

| Artifact | SHA-256 |
| --- | --- |
| `docs/design/sessions/visual-foundation/gk-015-r02/session.md` | `fe74285990dcd5941fde016e60e1de32ff24fae3641a5a04bfcd0a60a65ccd62` |
| `docs/design/sessions/visual-foundation/gk-015-r02/verification.json` | `9c39f03b74b8ce484bb0d2514a5eb6b5eb6384f31b5c42ab7cce1f63492cead4` |
| `docs/design/sessions/visual-foundation/gk-015-r02/gk015-card.json` | `45fb5a7f2b8955e8255e43bb012099775ea8f82c8a87863c35075e593fcfdf72` |
| `docs/design/tickets/GK-015.md` | `83e293f33645d41e3fea132cc15b7227166673c46c4bdb2a9121a1d33a818870` |
| `docs/creator/GK-001/handover.json` | `f8c09de923cd6a90e1c6b3404659e1161152910f7fee85b6c6278ae4d152557a` |
| `docs/creator/GK-003/handover.json` | `716525f65bc5cbaae13da19f51b885a9a29349604bbe73cb2fa7c9155b3a34d1` |
| `docs/creator/GK-005/interview-manifest-2026-10-05.json` | `489097fef895d0013ad0e4115b9635c2e5fd8b434b2fe5395ef0190f3a00a057` |

Relevant owner decision and original-content fingerprints are pinned below; full provenance/conditions remain in the linked source files.

| Stable decision ID | Decision revision | Source comment / content SHA-256 |
| --- | --- | --- |
| `BO-GK-001.lead_time` | `41f447d6952073826af25ba0f55c2809e3d87fe9bc6ff4bb74bec254df622986` | `chat:GK-001:2026-10-05:clarification-1` / `a51fd7c0287d7c55163b3ac4d55c6f938152cb6f31b96762c2327ad1909b8923` |
| `BO-GK-001.pickup_location` | `c52e8456fba18f262ac3c85feca6d4a7cb8c2273924815d86152141253422776` | `6abab66bbf2b0e811db912b7` / `cdb7805b1e1d6a3d1bff62a1ea5a615c776dcdb1b58a967707902ad971d67855` |
| `BO-GK-001.gk_delivery` | `3be447d37f4c07e6a926e19019263f57b98ab44882b5ef3ac797f67bca68027b` | `6ac2d95174a456d0b2b9ba6a` / `612835cca5d1566d859bf32a69f2d1ce50e660390ebadc775b991103f499ce69` |
| `BO-GK-003.request_fields` | `870636834f4d617b79f1a1ac5080e8da9f9075c777cb6511142d6a1bd91f0fcf` | `6abac3d56b4b97c36cd21b0c` / `5054a11ed6c4dfa8a5d012fdc9e999074550645199f48e9dd5fbb3299827e6cb` |
| `BO-GK-003.paid_commitment` | `e9c64782501839bda7b2eddba732ac617ab19e584ebca29fc36687b23ed019ca` | `chat:GK-003:2026-10-05:website-payment-24h` / `037d57fa3fc3254b357b6d33fb0e5baf0361990e7284984948ec3189618ba614` |
| `BO-GK-003.quote_validity` | `3cd40f5a0ec491548204c8d4a9cf27361e8478b760605dce2a24395f1fde28ab` | `chat:GK-003:2026-10-05:website-payment-24h` / `037d57fa3fc3254b357b6d33fb0e5baf0361990e7284984948ec3189618ba614` |
| `BO-GK-005.allergens` | `3dade43fe5452311508474cb3dc3aab2dbdbacadace4e6627105c5a3d1c9a587` | `chat:GK-005:2026-10-05:allergens` / `25822ed28feeb801d28a24e65f1b3fddfec072cb856d2d5a3d8a4c4d1fe0bdd0` |
| `BO-GK-005.cross_contact` | `ea64ed35e3a1b117c665c6c2d0629ee267f1bca7a0dac159e9d7f87cb1bdb35a` | `6abacbecde4959cdb8da9e21` / `c8930238a9c62512ea1192bb012169cbcb44901b3c1fb64f8060f325b3d11916` |
| `BO-GK-005.money_precision` | `00d05c48b7740f9c461555d76b0d8b7593346a77554e786cde74833ca53e2d73` | `chat:GK-005:2026-10-05:money-enquiries-photos` / `d81c3b3d31935f13798c11ee473b1d010bf1eebad028b16a96323b913ababfed` |
| `BO-GK-005.coating_decor` | `69be0d5f064eeeb7f97af17f9445e9a7efb44b27d1e6503f5c1d4693b95c7d31` | `chat:GK-005:2026-10-05:coating-whole-prices` / `bacc92157e7a351a7c157fc5cab9ae72ed1f624baca860f395537c1bb6842b33` |

GK-002 interview-r4 SHA-256: `4d153e8b597ade2c196f19609bc1674b09a2ff515a3bbaf8d33764e9113c5f29`; its explicit representative-example approval is `chat:GK-002:2026-10-05:decoration-examples`. These provenance identifiers are evidence references, not invented Trello actions.
