# GK-015 r03 — independent POC review

2026-10-06. **PASS — ready for Creator review of this local POC.** All three
actionable findings were fixed and the affected behavior was rechecked. No
remaining blocker or major finding within this scope.
This review covers readiness to show the local customer POC to the Creator. It
does not approve the finished visual design, production implementation, legal
content, payment integration, deployment or Trello publication.

## Review context

- Independent fresh reviewer, `fork_turns=none`; no inherited author discussion
  or authorship of the POC. Subsequent communication contained scope, findings,
  factual fix dispositions and check evidence.
- Requested `gpt-6-astra` / `xhigh`; accepted selection reported by the dispatching
  host, effective identity/effort unavailable: **requested-only**, not confirmed.
- Applied root `AGENTS.md`, `skills/gk-plan-review/SKILL.md`, its execution check,
  artifact/ticket and design-session contracts, project brief, SETUP and DELIVERY.
- Scope readback: preserve shared work and owner assets; inspect the selected
  reference, all customer entrypoints, actual JS/CSS and rendered previews;
  write only this report. No external changes or production services.
- Base commit `bb96a89681bcf2fa1c663a02eacbe039e20e6363`; POC and planning inputs
  are uncommitted, so exact-file fingerprints govern this review.
- Current inputs sufficient for this disposable POC: contracts r6.4, GK-019
  handoff r5, owner-data gk019-owner-r6, GK-002 interview-r4,
  GK-001/003/005 README/source-comments/decisions and GK-005 current interview.
  Initial 19 input-manifest fingerprints and nine asset-source hashes independently
  checked with no mismatch. During review, shared GK-019 inputs advanced from
  r6.3 to r6.4; refreshed contracts, handoff, question queue, Q4-currency/Q5
  acceptance records and GK-003 decisions/source before the final verdict.
- Refreshed readback: optional provider-managed currency and verified alternative
  restitution are approved design inputs. The POC has no method selector,
  currency conversion or payout execution, so these additions do not contradict
  its displayed behavior. Onelink bundle/non-GBP settlement qualification and
  latest shared-contract re-review remain separate gates. The current GK-019
  review file still reviews r6.3; this POC review does not approve the new full
  contract packet. Author model identity remains unknown as recorded in session;
  the author accepted no consequential new architecture decision in this POC.

## Findings and disposition

No blocker or major business-rule mismatch found in the bounded POC. The initial
review identified three minor but actionable interaction defects:

1. `poc/app.js`, `bindCustom`: choosing Email populated the request summary but
   successful-copy and copy-failure instructions still directed the user to
   Instagram. Correct both status/fallback and action wording for the chosen
   channel, retaining Instagram as the default. **Fixed.** Independently exercised
   Email summary/button, successful copy and selectable failure fallback in IAB;
   inspected the channel-specific real clipboard-failure catch branch.
2. `poc/app.js`, `bindShop` and list-removal rendering: saving a cake replaced
   the focused control. Independently observed AX focus falling to the WebArea;
   Tab restarted at the Pink piping link. Preserve the toggle or restore a
   meaningful surviving control after list changes. **Fixed.** Shop toggle now
   updates in place; live focus stayed on Silver sculpture and Tab advanced to
   Midnight bloom. Removing favourites focused the remaining toggle, then the
   empty-list heading. Bag uses the same inspected removal helper; author
   regression evidence covers its remaining-item and empty states.
3. `poc/styles.css`: `button.primary` overrode the earlier `button:disabled`
   foreground/background because their specificity is equal. The blank-date
   Add to bag looked like the active primary action in both detail previews.
   Give disabled primary buttons a distinct visible treatment. **Fixed.** Disabled
   rule is now last in CSS; refreshed detail mobile preview shows grey/dashed
   Add to bag. Author regression checks the computed disabled visual state.

The author additionally corrected duplicate decorative SVG IDs, named the date
dialog and added a changed-price preview. Inspected those final changes: unique
seal IDs, `aria-labelledby`, old total explicitly marked previous, no invented
replacement price, fresh quote/agreement required before payment. These are not
claimed as independently discovered findings.

## Acceptance coverage

| Session criterion | Evidence and assessment |
| --- | --- |
| AC1 — selected visual target, separate home/shop, responsive | Inspected the supplied two-screen reference and all eight home/shop/detail/custom desktop/mobile PNGs. White unframed cutout grid, dark type, centered wordmark, home hero turntable and restrained silver sections follow the target. Real cake-photo perspective and CSS chrome remain visual approximations requiring Creator judgment. |
| AC2 — discovery and honest catalogue | Source inspected filters/sort/no-results recovery, detail links, favourites and missing/unavailable recovery. Three approved priced configurations match GK-002; five other photos have by-request status without invented flavour/size/price. |
| AC3 — dates and allergens | Independently exercised blank-date disabled purchase, available-date checking and use. Scripted fixtures are openly labelled advisory. All three approved compositions, standard coating, contains declarations and exact separate cross-contact notice match owner content; Snickers PEANUTS is prominent. |
| AC4 — bag/checkout/state honesty | Independently traced Pink piping → date → £90 bag → checked allergen review → unknown-payment screen → browser Back. Back displayed the blocked checkout and retained unknown state. Source confirms separate delivery blocks confirmation, known-late success awaits agreement, failed/expired/recovery states retain selection, whole-GBP arithmetic uses pence, and no credentials/payment/provider call exists. |
| AC5 — custom request | Source distinguishes generic from explicit image reference, requires date/servings/flavour/description, accepts optional multiple files only in memory, retains fields when editing, and offers selectable copy recovery without a sent/uploaded claim. Independently exercised required-field validation, generic no-reference summary and corrected Email handoff/fallback. |
| AC6 — interaction/accessibility and review | Reviewed semantic form labels, focus CSS, dialog, menu, reduced-motion rule and simulation text. All three findings above resolved; focused live IAB and refreshed disabled visual rechecks passed. Author's wider browser verification is separately attributed below. |

Important current owner rules are retained: ordinary 192-hour paid lead; specific
chef-approved ready/resale exception; 24-hour initiation versus 15-minute tentative
hold; no replacement while unknown; paid-awaiting-agreement blocks fulfilment;
48-hour preparation anchor; six-hour same-day move boundary; immutable original
handover minus 120 hours for 100%/50% cancellation; chef 100%; separately paid GK
delivery from an individually agreed quote, no zones/tariffs. Exact public locality
and contact details match owner input; private address remains absent.

## Evidence limits and remaining gates

The author's `verification.json` records 15 passed check groups, 16 route variants
at 320/390/768/1440px and seven contrast pairs, tied to the final JS/CSS below.
Those are author-run checks, not independent browser coverage. This reviewer
independently inspected source/reference/previews, verified source fingerprints
and exercised the focused live IAB journeys described above. No provider,
PostgreSQL, production capacity race, authentication, actual
message delivery, device-level screen-reader or real payment test is claimed.

The exact finished r03 revision still needs Creator visual approval. Missing
canonical `3000px-03.png` is an existing source-asset gate; this POC explicitly
labels its wordmark/emblem as proposed artwork and does not amend GK-002. Q4
remaining qualification, current shared-contract independent review, backend/admin
design, actual integrations and final legal/allergen verification remain separate
work. Q5 is approved; do not revive its earlier unanswered status. No publication,
release or deployment approval is inferred from this review.

## Reviewed revision

All 25 POC source/asset hashes independently match [output-manifest.json](output-manifest.json).
The reference and PNG hashes below identify the visual material inspected; final
detail mobile was re-inspected after the disabled style correction.

| Artifact | SHA-256 |
| --- | --- |
| Output manifest | `4064e85685a890a841f41145b18ff12bbbe69b84a66159d9b4975d89ac703817` |
| Final input manifest | `5465af77ab62fc23554ae1d15edfa594ee6676d6d6aad42090b7b9323c757815` |
| `poc/app.js` | `a3a7e5573d4d0226d95a9892c455a897b4efc1afc57d9bade607fdfd67d820d2` |
| `poc/styles.css` | `7cca1cca880f3e1f20948d8d702655ad5c418ea96e3a83cbaa0e712353e10453` |
| Author verification | `a9ab46b021fdd0f7a5e242fe067d2f4f76247beac8cdcbe4bd7a4ba62e58e48f` |
| Selected reference | `776ac068091b1fed100db6bd5e0b3c51e26367710f8f98f72f95a3b2f941ddfb` |
| Home desktop | `a8945460f74067dfbc03eda3424cbc984ec1afd3c63ac727c72c9d7eda01444b` |
| Home mobile | `eae2912d9f301471f00528cc356799e4d66d529e1de9f1165aefea2b479c1851` |
| Shop desktop | `c3270ec23cda8c8402dad9cfc1f1e67cd80a35746cc1ec3a11205efc5516e213` |
| Shop mobile | `c8f77083359b01e50c33c2bd564577860a416dbdffd3cf399e266c85da6fb933` |
| Detail desktop | `7e852c60aa947142cbbde50c612a1aa48a6354c06d48ac75fe0d75d1fcd62218` |
| Detail mobile | `95e8881f660770842296b43c0333b933c05923bbf71c8d275d0af91d70aa045d` |
| Custom desktop | `2e3a4cf8fd9d72ff441813a51b5bc84d9d26671317010e0a6163924272037f48` |
| Custom mobile | `ed55702ea6e30ffde33c82d2b1e993817400201f5499d66d783d72bb71bc2e04` |

Owner content and decision file revisions are recorded in
[input-manifest-final.json](input-manifest-final.json); all 22 current file hashes
independently match the final manifest above. This includes the latest handoff and
GK-002 README that explicitly preserve the missing-logo gate. Checking these 22
inputs is not a full independent review of the ongoing GK-019 output packet.

## 2026-10-06 — subsequent GK-019 review reconciliation

This addendum supersedes the earlier statements that the current shared-contract
review is pending. Independently read the new GK-019 review and unchanged r5
handoff: the separate GK-019 reviewer now records **conditional PASS** for contracts
r6.4 / owner-r6 / schema r1.4 against manifest
`f15556358b4cac4217d96d9537c27981e32915a8dd87cdcfe2bfd7e818e54c23`.
Independently verified all 95 manifest file hashes, including the relevant owner
inputs, contracts, schema and handoff, with no mismatch. The frozen handoff's
“re-review pending” heading is superseded by the new review evidence; its bytes
were preserved. GK-019 review SHA-256 is
`cfa876d3f4999a93b5faa220cc6ab6e2c13ed93ee675413485dc98b118915d17`.

The POC output manifest remains
`4064e85685a890a841f41145b18ff12bbbe69b84a66159d9b4975d89ac703817`;
all 25 source/asset hashes still match. No visual or code change or new behavioral
test was needed. The frozen input manifest is historical and was not rewritten.
The existing **ready for Creator review** verdict stands. Q4 Onelink bundle,
settlement/method qualification, missing approved logo and exact-version Creator
visual approval remain separate gates; neither review establishes production
readiness, GK-019 Done or permission to publish/deploy.
