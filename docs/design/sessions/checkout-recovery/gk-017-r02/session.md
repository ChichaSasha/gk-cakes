# GK-017 r02 — checkout and order problem states

2026-11-05. Card [GK-017](https://trello.com/c/1YGOcOHR), In Progress. r02 covers the **problem states** the
Creator asked for on 2026-11-04, plus the two r02 items (visible start–end times as a radio list; no delivery
time self-change on GK delivery orders). It reuses the Creator-approved r01 checkout (look, copy style,
accessibility patterns) on the GK-015 r12 foundation. **Not approved** — needs exact-version Creator approval,
then the fresh integration review (AC3).

**Review round 1** (independent design reviewer, PR #28, REVISE on a4c3a81) is addressed in this revision; see
"Review round 1 changes" below.

## Execution check

Role: GK-017 design author (Owner: Agent, Kind: Design), bounded r02 sketch. Card policy requests
`gpt-5.6-sol` / high in a fresh scoped worker. Host reports `llmplatform/claude-opus-5-5` (system environment),
running as a **fresh subagent** with only the master's brief and the files below — **model mismatch; the
current user-selected model takes precedence** (status: host-reported, not the requested model). This worker is
the author, so it is not an independent review. Writes: this folder only, worktree branch
`sasha-chycha/gk-017-r02-design` from `origin/main` 7df96d0. `api/`, `ui/`, r01 files and other worktrees untouched.
Round 1 fixes: the same author subagent and model, working from the reviewer's findings; no corporate tools
(Glean, Slack, Jira, Grafana, Snoocode GitHub tools) were used in this round.

Inputs (SHA-256, first 12 hex):

| Input | Hash |
| --- | --- |
| `AGENTS.md` | 73502bdeedc5 |
| `docs/design/contracts.md` (K03–K06) | b34b0926f371 |
| `gk-017-r01/session.md` (approved r01 decisions) | e4617c32417b |
| `gk-017-r01/poc/app.js` · `styles.css` (copied as the base) | 07834eae2fb1 · 6a54c554e5c7 |
| GK-017 card text (local export `gk017-card.md`) | 9a6abf0e35c9 |
| GK-017 comments (local export `gk017-comments.md`, Creator answers 2026-11-04) | 5174147cbd03 |
| `ui/src/lib/checkout/order.ts` (built order states and refund lines) | aa4bf76d3bb8 |
| `ui/src/lib/checkout/messages.ts` (built API refusal wording) | c34c8781fe9b |
| `ui/src/routes/orders/[id]/+page.svelte` · `ui/src/routes/checkout/+page.svelte` | d395417cc73b · 9c6facfb2a31 |
| `ui/README.md` | 10cc95c6f38a |

Readback: Q5 = when GK can't deliver after payment, or the delivery quote isn't paid, the customer chooses
collection, own courier or a full refund of everything paid. Contract points carried into copy: "Still checking
payment; do not pay again"; 15-minute hold that never turns an in-flight payment into a failure; release only
after proof of no payment; Q2 `paid_awaiting_agreement` (date requested, not promised; silence is not agreement;
full return, never 50%); cancellation 100% before / 50% at or after original handover − 120 h, rounded in the
customer's favour, refreshed and reconfirmed at the boundary; chef cancellation 100%; refund requested ≠ returned;
delivery paid as a separate collection, delivery refund sent by the chef.

## How the prototype is organised

- `poc/` is a copy of the r01 prototype with these changes: `r02.js` (all states), `recovery.html`
  (`?state=…&v=…`), `states.html` (index), the shared radio time list in `checkout.html`, no "Preview time change"
  on GK delivery orders in `order.html`, the checkout's design-review box now also opens the r02 states, and an
  r02 section at the end of `styles.css`. Demo storage keys use `gk-poc-gk017r02-`.
- Every state page starts with a **design-review switcher** (striped yellow, dashed border, mono type, labelled
  "Prototype only · design review · not part of the shop"): state picker, previous/next, variants, and the API
  state it simulates. It is not part of the shop design.
- **Money**: every figure is an example server response held in one fixture (`FX` in `r02.js`). The page never
  adds, halves or rounds money. The only computed-looking figure, £85.85 → £42.93, is labelled "Example · how
  rounding works". Fixture: Pink piping £90, Thu 22 October 2026 11:00–12:00, full-refund deadline 11:00 on Sat
  17 October; changed price £95 (50% £47.50); GK delivery £120 (cakes + delivery £210, 50% = £45 + £60).
- No analytics, network calls, fonts or scripts from elsewhere.
- `shot.html` (screenshot frame) and `shoot.sh` sit next to this file, outside `poc/`.

## States (AC2) — what each shows

Checkout re-shown after a refusal at Continue: the problem banner comes first (focused, `role="alert"`), the
affected section is open, and untouched sections collapse to a "Kept" line, so the customer can see that their
details survived. Nothing is charged; nothing is replaced for them.

| # | State (variants) | What the customer sees | Buttons → destination |
| --- | --- | --- | --- |
| 1 | Price changed / total refreshed (PRICE_CHANGED, QUOTE_EXPIRED) | "The price has changed" or "We've refreshed your total"; old price struck through beside the new one; refund terms show the new amounts with an "Updated" badge; tick-box cleared with "Your earlier tick was cleared because the total changed" (refreshed variant: "…because your total was refreshed"). | Continue → inline tick error until ticked; then "Opening secure payment…" → state 8 (demo) |
| 2 | Refund terms updated (POLICY_CHANGED) | Full current terms marked "Updated"; total unchanged; tick cleared. | Same as 1 |
| 3 | Time taken / date now full (SLOT_UNAVAILABLE; day capacity reached) | Time: fresh radio list, the lost time shown struck through "No longer available", **no time pre-selected**; choosing one fills the new refund deadline; tick cleared because the deadline follows the time. Date: "Thu 22 October is now fully booked", choose another date or contact the chef; Continue disabled ("Choose another date first"). | Choose another date → calendar dialog; Continue → time and tick errors, then payment |
| 4 | Cake no longer available (ITEM_UNAVAILABLE) | Ready-made Chocolate swirl marked "Sold · No longer available"; total withheld until a new quote; never swapped. After removing: "Chocolate swirl removed · new total", re-tick. | Remove and update the total → variant "after removing"; Back to your bag → bag |
| 5 | Other refusals | LEAD_TIME (checkout and order-page wording), CODE_USED_UP, PAYMENT_IN_PROGRESS, using the built wording. | Choose a later date; Start a new order; Review the new total; View your order |
| 6 | Still checking payment (polling / stopped / payment page still open) | Title "Still checking payment; do not pay again"; place stays held past 15 minutes while the result is awaited; "This page updates by itself" ticker (visible, not announced each second); after 5 minutes "We're still waiting… Refresh to check"; "What happens next" list ("…you can try again while your place is still held"). Payment page still open: "Your place… is held while you pay", go back to the same payment, "This link disappears once the payment page closes" (the polling variant has no link). | Refresh status → polling; Back to the secure payment page → (demo) polling variant; Contact us |
| 7 | Result unclear — a person is checking | "We're checking your payment": a team member checks; don't pay again; place and code stay with the order; if paid → confirmed, or everything returned if the date can't be met; if not paid → nothing charged. No pay button. | Contact us (with order reference) |
| 8 | Payment received — confirming (`confirmation_pending`) | Spinner, "usually a few seconds", don't pay again. | — (page updates) |
| 9 | Time ran out — place released (`cancelled`, `payment_not_completed`) | "Your place was released": "The payment wasn't completed, so this order was cancelled and your place released. No payment was taken." (no reference to the 15-minute timer, which never proves failure). New order re-checks price and times. Paid elsewhere? Contact us. | Start a new order with the same cakes → checkout with the cake re-added (new quote, new hold) |
| 10 | Paid after the preparation time (`paid_awaiting_agreement`; waiting / chef offers a date / agreed) | "Payment received"; date and time labelled **requested (not confirmed yet)**; chef will contact; never moved without agreement; if not agreed, everything back (£90); don't pay again. Offer variant: "The chef can make your cake for Thu 22 October… Do you agree?" | Contact the chef; Yes, confirm this date → confirmed with standard terms; No, refund everything → refund requested (full) |
| 11 | Paid after the order was cancelled (requested / pending / returned / needs a check) | "Payment received after this order was cancelled": payment kept safe and all of it being returned; 3-step tracker (see 13); wording follows the API refund state, "returned" only when the API says so; cake not made. | Start a new order; Contact us |
| 12 | Cancellation preview (before / deadline passed while open / after / after with GK delivery) | Inline "Cancel your order?" panel: amount back, which side of the deadline, time left (as of a server time). Boundary: alert "The full-refund deadline has just passed… now £45… confirm again", button relabelled with the new amount. After: 50% "rounded in your favour" with the labelled rounding example; "If we have to cancel, you always get 100% back". Delivery: split cakes £45 (to your payment method) + delivery £60 (sent by the chef). | Cancel order · refund £X → state 13 with the **same amount**: before → "Requested · £90", after and deadline-passed → "Requested · £45", with GK delivery → "Requested · £105" (then partly returned); Keep my order → order page |
| 13 | Refund progress (requested £90 / requested £45 / requested £105 with delivery / partly returned / pending / returned / needs a check / we cancelled / full refund · date not agreed / full refund · we couldn't deliver / full refund · delivery was paid) | Why this amount (one sentence), then the built refund line verbatim. Tracker for the card refund: "Refund requested" → "Waiting to be sent…" / "Sent to our payment provider · waiting for them" / "Needs a check by our team" → "Return to your payment method"; past tense only once a step is finished ("Returned to your payment method", all steps ticked). The chef's delivery refund is always its own line ("Delivery · sent separately by the chef · Waiting for the chef"). "Returned so far" in the summary; chef cancellation: apology and 100%. | Contact us |
| 14 | We can't deliver after payment (Q5; choose / choose with delivery already paid / collection chosen / own courier chosen / both results with the £120 delivery refund) | Apology; cake still paid and booked; choose **Collect it · free**, **Send your own courier** (responsibility passes at handover) or **Full refund · everything paid**; collection/courier reveal the radio list of times that day; deadline unchanged; if delivery was paid it is returned in full whatever they choose. Nothing changes until Confirm. Summary shows "GK delivery · Not paid". Results: "You'll collect your cake" or "Your courier will collect your cake" (you book and pay the courier; responsibility passes at handover), new time, unchanged deadline, and in the delivery-paid case "Your delivery payment of £120 is being returned in full" with a "Delivery refund · requested" summary line. | Confirm my choice → the matching result, or refund "full refund · we couldn't deliver" (£90) / "delivery was paid" (£210: cakes £90 + delivery £120 on its own line); inline errors if nothing/no time chosen |
| 15 | Delivery quote not paid (waiting / delivery payment being checked) | Quote (address, date, time, £120); cake already paid and confirmed; "We can deliver only once the delivery is paid"; the same three Q5 alternatives; summary "GK delivery · quoted, not paid · £120". Checking variant: "Still checking delivery payment; do not pay again". | Pay for delivery · £120 → checking variant; Confirm my choice |
| 16 | Free order £0 (checkout / confirm / confirming / refused: order changed / place released / time passed) | Checkout: GIFT100 example code, "To pay now £0", "Continue · confirm your free order", no payment page, "nothing to refund if it's cancelled". Order page: "Confirm your order" (place held 15 min), confirming spinner, refusal wording by reason from the built UI, one variant per reason (order_changed → Try again; holds_released and window_closed → Start a new order); no review-only text in customer copy. | Confirm order; Try again; Start a new order |
| 17 | r02 item — visible start–end times (times shown / GK delivery requestable / loading / no times / couldn't load) | Radio cards "12:30–13:30 · Available", London time in the legend; delivery shows "Requestable · subject to chef confirmation"; loading skeleton with announced status; "No times… we haven't changed your date" with another date / contact chef; failure with Try again only. Also live in `checkout.html`. | Choose another date; Try again; mode cards switch variants |
| 18 | r02 item — time changes (GK delivery order / collection order) | GK delivery: no time-change control; "Need a different delivery time? Please contact the chef: a GK delivery time can only change with the chef's agreement." Collection (and own courier): unchanged rule, same day until 6 hours before (05:00 for an 11:00 slot), radio list, "doesn't change your refund deadline". | Preview cancellation → state 12; Move to the selected time (preview only) |
| 19 | Order timeline (confirmed · being made / GK delivery · being made / cancel while being made / not collected / collected · ask the chef / collected · refund via the chef / delivery failed on the day) | Two lanes side by side, **Money** and **Your cake**, each with its own ticked, current (ringed) and upcoming steps, so money and fulfilment move independently. Being made: paid £90, full refund until the deadline; cake confirmed → being made → ready (collection address about 48 h before) → collected. GK delivery: cakes and delivery paid separately, "Delivery confirmed by the chef", out for delivery, delivered; no time change (contact the chef). Cancel while being made: "Your cake already being made doesn't reduce your refund" — £90 before the deadline, 50% (£45) after, never less. Not collected: cake lane ends "Not collected" (problem marker), money lane "Refund of £45 requested · 50% for a cake that wasn't collected". Collected: "Something wrong with your cake? Contact the chef"; after the chef arranges it, "Refund of £45 requested · arranged with the chef", while "Collected" stays in the cake lane. Delivery failed: out for delivery ✓ → "Delivery couldn't be completed", chef will contact today, paid delivery returned in full if the problem was ours. | Change your time / Preview cancellation / Keep my order; Cancel order · refund £90 → refund requested £90; Contact the chef |

Accessibility patterns reused from r01: one `h1`, numbered `h2` sections, visible 3px focus ring, problem banners
and status headings receive focus on arrival, polite live regions only for state changes, radio cards built on
native radios inside `fieldset`/`legend` (arrow keys, forced-colours outline), inline errors linked with
`aria-describedby`, ≥44px targets, reduced-motion stops spinners and shimmer.

The r01 order page (`order.html`) also no longer says "Collection details follow separately" on GK delivery
orders: it says "The chef delivers at the time confirmed with you".

## Review round 1 changes

- M1: cancellation and refund amounts now match end to end (£90 / £45 / £105; Q5 refunds £90 / £210).
- M2: new "Order timeline" state (row 19); r01 delivery wording fixed; undesignable parts deferred below.
- Minors 1–8 and nits: tracker wording, tense and needs-a-check step; chef delivery refund on its own line;
  payment-page-open copy and link; refreshed-total tick note; free-order refusals split by reason; released-place
  copy without the timer; Q5 result screens (own courier, £120 delivery refund, "Not paid" / "quoted, not paid");
  time-card border and radio ring `#767b7d` (4.3:1 on white); "while your place is still held"; Back to the
  payment page → polling variant; unused `data-reconfirm` removed; `shot.html` moved next to `shoot.sh`;
  question 21 added about r01's "We'll email you".

## Preview

```sh
python3 -m http.server 8792 --bind 127.0.0.1 --directory docs/design/sessions/checkout-recovery/gk-017-r02/poc
```

Open <http://127.0.0.1:8792/states.html> (index) or e.g.
<http://127.0.0.1:8792/recovery.html?state=cancel&v=boundary>. Real checkout with the radio list:
<http://127.0.0.1:8792/checkout.html?seed=pink>. Screenshots: serve this folder instead
(`python3 -m http.server 8792 --bind 127.0.0.1 --directory docs/design/sessions/checkout-recovery/gk-017-r02`) and
run `sh shoot.sh` (headless Chrome; `shot.html` frames each `poc/` page at exactly 1440 or 390 px, because
headless Chrome's window is at least 500 px wide, and reports the full height).

`screens/` holds 61 full-page renders, each at desktop (1440) and mobile (390) — 122 JPEGs (quality 78, to keep
the repository small): `00-states-index` … `60-timeline-delivery-failed`, listed at the end of `shoot.sh`. All
were re-shot in round 1. They use
screenshot mode (`?shot=1`): no focus move, no polling ticker and no site footer; everything else is as on screen.

## Checks done

- Scripted browser checks (temporary harness, not committed), all passing: checkout radio list keeps the time
  across collect/courier with announcement, summary and refund deadline follow the radio, list is busy while
  loading, delivery times are requestable, validation without a stray time error; price state focuses the banner,
  requires re-tick, then continues; slot state disables the lost time, pre-selects nothing, requires a time and
  updates the deadline; Q5 requires a choice and shows/hides times; delivery order has no time change, collection
  keeps it.
- Round 1 harness (also temporary), all passing: each cancel variant's button amount equals the refund page amount
  (£90, £45, £45, £105); Q5 refund with paid delivery shows £210; own-courier result mentions the £120 delivery
  refund; unpaid-quote refund £90; "Back to the secure payment page" opens the polling variant; cancelling while
  being made offers £90 (never £0); r01 delivery order page no longer says "Collection details follow separately";
  no `data-reconfirm`; no review-only text in the free-order refusal.
- Round 1: all 78 state/variant links render at 320, 768 and 1440 px with no horizontal overflow.
- Every state and variant renders at 320, 768 and 1440 px with no horizontal overflow (one 8px overflow from a
  long API note in the review switcher at 320 px was fixed with `overflow-wrap`).
- New colours (WCAG contrast): review text 13.1:1, review API note 10.7:1, re-tick note 8.4:1, muted text on the
  unavailable time 6.4:1, "Kept" 6.5:1, warning border 5.4:1 (non-text); round 1: unselected time-card border,
radio ring and timeline step ring `#767b7d` 4.3:1 on white (3.9:1 on the page background); r01 focus ring unchanged.
- `python3 scripts/check_planning_kit.py` (it does not scan `docs/design/sessions`, run for CI parity).
- No independent review yet: T3's fresh review is still required after the Creator's feedback.

## Deviations and notes

- Model mismatch (above). A read-only Glean memory-schema lookup was made by mistake during the first run's
  execution check, which broke the project's no-corporate-tools rule; no data from it was used. None were used in
  round 1.
- The checkout problem states use a condensed checkout (unaffected sections collapsed to "Kept"); proposed for the
  live shop, not yet decided (question 14).
- r01's typed-details-lost-on-date-change limitation remains in the prototype; the live shop keeps details.
- Order-page wording follows the built UI (`order.ts`, `messages.ts`); r02 adds the "What happens next" lists,
  trackers and Q5 screens, which need new API states (delivery infeasible, delivery quote) not built yet.
- "Your bank may take a few days to show it" is new wording (question 12).
- The cancellation boundary is shown as a separate variant ("Deadline passed while open") rather than a live
  timer; the prototype has no clock-driven refresh.
- **Deferred (not designed, Creator decision needed):**
  - What the customer can choose after a GK delivery fails on the day (redelivery, later collection, cake refund),
    and how a customer-caused failure (nobody at the address) differs — shown only as "the chef will contact you
    today"; questions 17 and 18.
  - Whether the 50% refund for a cake that wasn't collected is requested automatically when the chef records it
    (as drawn) or only when the customer asks — question 19.
  - How much a post-handover refund can be (the 50% rule or an amount the chef agrees) — the screen shows an
    example £45 "arranged with the chef"; question 20.
  - Over-capacity chef cancellation is covered only by the generic "We had to cancel your order" refund variant.

## Open questions for the Creator

6. **Paid late, agreeing the date:** should the customer accept or decline on the order page (buttons as in
   "Chef offers a date"), or only by message with the chef recording it? May the chef offer a different date there?
7. **Paid after cancellation:** always a full refund, or may the chef offer to reinstate the order if the customer
   explicitly agrees?
8. **Q5 choice:** is there a time limit for choosing collection / own courier / refund? If the customer doesn't
   answer, the chef keeps the case open (the contract forbids inventing a timeout). Is that right?
9. **Unpaid delivery quote:** should it have a payment deadline, and what happens if it passes?
10. **After delivery fails:** may the customer pick any collection time that day (as shown), or only the original
    time?
11. **Terms changed at Continue:** show a short "what changed" summary, or only the full current terms (as now)?
12. Is "Your bank may take a few days to show it" acceptable after a refund is returned, or should it be left out?
13. **Free orders:** confirm the same 15-minute hold applies while the customer confirms a £0 order.
14. Collapse unaffected checkout sections to "Kept" lines after a refusal (as shown), or keep the full checkout?
15. A lost time is shown struck through as "No longer available". Keep it visible like this, or simply drop it?
16. Anything to change before approving this exact r02 revision?
17. **GK delivery fails on the day:** what may the customer choose — redelivery, collect later, or a refund of the
    cake as well as the delivery? (Deferred; the screen says only that the chef will contact them.)
18. **Customer-caused delivery failure** (nobody at the address): is the delivery payment kept, and is the cake
    treated like a cake that wasn't collected (50%)?
19. **Not collected:** is the 50% refund requested automatically once the chef records the no-show (as drawn), or
    only when the customer asks the chef?
20. **After handover:** is a refund the customer asks the chef for always 50%, or whatever amount the chef agrees?
21. r01's summary says "Your order is confirmed only when we receive the payment. **We'll email you.**" Order
    emails don't exist yet (the built UI doesn't promise one). Remove "We'll email you" until emails exist, or keep
    it as the target design?
