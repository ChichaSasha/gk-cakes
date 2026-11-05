# GK-017 r02b — checkout and order problem states

**r02b — Creator-approved 2026-11-05 (answers as suggested).** The Creator approved r02 "as suggested" with the
answers to questions 6–21 recorded under "Creator decisions (2026-11-05)"; r02b applies them. The fresh integration
review (AC3) still follows.

2026-11-05. Card [GK-017](https://trello.com/c/1YGOcOHR), In Progress. r02 covers the **problem states** the
Creator asked for on 2026-11-04, plus the two r02 items (visible start–end times as a radio list; no delivery
time self-change on GK delivery orders). It reuses the Creator-approved r01 checkout (look, copy style,
accessibility patterns) on the GK-015 r12 foundation.

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
(Glean, Slack, Jira, Grafana, Snoocode GitHub tools) were used in this round. Review round 2 copy fixes landed in
dc2bd09 (neutral preparation wording per K05, past tense once returned, delivery refund "requested" wording).
r02b: the same author subagent and model applied the Creator's answers from head dc2bd09; no corporate tools used.

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
| 2 | Refund terms updated (POLICY_CHANGED) | A short **"What changed:"** line (published with the policy revision; example text) plus the full current terms marked "Updated" (Q11); total unchanged; tick cleared. | Same as 1 |
| 3 | Time taken / date now full (SLOT_UNAVAILABLE; day capacity reached) | Time: fresh radio list; the lost time is shown struck through "No longer available" **only because it was the customer's selected time** — other gone times are dropped (Q15; the checkout picker does the same); **no time pre-selected**; choosing one fills the new refund deadline; tick cleared because the deadline follows the time. Date: "Thu 22 October is now fully booked", choose another date or contact the chef; Continue disabled ("Choose another date first"). | Choose another date → calendar dialog; Continue → time and tick errors, then payment |
| 4 | Cake no longer available (ITEM_UNAVAILABLE) | Ready-made Chocolate swirl marked "Sold · No longer available"; total withheld until a new quote; never swapped. After removing: "Chocolate swirl removed · new total", re-tick. | Remove and update the total → variant "after removing"; Back to your bag → bag |
| 5 | Other refusals | LEAD_TIME (checkout and order-page wording), CODE_USED_UP, PAYMENT_IN_PROGRESS, using the built wording. | Choose a later date; Start a new order; Review the new total; View your order |
| 6 | Still checking payment (polling / stopped / payment page still open) | Title "Still checking payment; do not pay again"; place stays held past 15 minutes while the result is awaited; "This page updates by itself" ticker (visible, not announced each second); after 5 minutes "We're still waiting… Refresh to check"; "What happens next" list ("…you can try again while your place is still held"). Payment page still open: "Your place… is held while you pay", go back to the same payment, "This link disappears once the payment page closes" (the polling variant has no link). | Refresh status → polling; Back to the secure payment page → (demo) polling variant; Contact us |
| 7 | Result unclear — a person is checking | "We're checking your payment": a team member checks; don't pay again; place and code stay with the order; if paid → confirmed, or everything returned if the date can't be met; if not paid → nothing charged. No pay button. | Contact us (with order reference) |
| 8 | Payment received — confirming (`confirmation_pending`) | Spinner, "usually a few seconds", don't pay again. | — (page updates) |
| 9 | Time ran out — place released (`cancelled`, `payment_not_completed`) | "Your place was released": "The payment wasn't completed, so this order was cancelled and your place released. No payment was taken." (no reference to the 15-minute timer, which never proves failure). New order re-checks price and times. Paid elsewhere? Contact us. | Start a new order with the same cakes → checkout with the cake re-added (new quote, new hold) |
| 10 | Paid after the preparation time (`paid_awaiting_agreement`; waiting / chef can make your date / chef offers another date / you agreed / you accepted another date) | "Payment received"; date and time labelled **requested (not confirmed yet)**; the chef's answer appears on this page; never moved without agreement; if not agreed, everything back (£90); don't pay again. The customer answers **on the order page** (Q6): "Yes, confirm this date" or, when the chef proposes Sat 24 October 13:00–14:00, "Accept Sat 24 October, 13:00–14:00" — "This is only an offer: nothing changes unless you accept it"; "We keep a record of your answer and when you gave it" (agreement evidence). Confirmed screen: "You agreed to … on Thu 15 October, 18:20". | Yes / Accept → confirmed with standard terms; No, refund everything · £90 → refund "full refund · date not agreed"; Contact the chef |
| 11 | Paid after the order was cancelled (requested / pending / returned / needs a check) | "Payment received after this order was cancelled": payment kept safe and all of it being returned; 3-step tracker (see 13); wording follows the API refund state, "returned" only when the API says so; cake not made. Always a full refund for now (Q7). | Start a new order; Contact us |
| 12 | Cancellation preview (before / deadline passed while open / after / after with GK delivery) | Inline "Cancel your order?" panel: amount back, which side of the deadline, time left (as of a server time). Boundary: alert "The full-refund deadline has just passed… now £45… confirm again", button relabelled with the new amount. After: 50% "rounded in your favour" with the labelled rounding example; "If we have to cancel, you always get 100% back". Delivery: split cakes £45 (to your payment method) + delivery £60 (sent by the chef). | Cancel order · refund £X → state 13 with the **same amount**: before → "Requested · £90", after and deadline-passed → "Requested · £45", with GK delivery → "Requested · £105" (then partly returned); Keep my order → order page |
| 13 | Refund progress (requested £90 / requested £45 / requested £105 with delivery / partly returned / pending / returned / needs a check / we cancelled / full refund · date not agreed / full refund · we couldn't deliver / full refund · delivery was paid) | Why this amount (one sentence), then the built refund line verbatim. Tracker for the card refund: "Refund requested" → "Waiting to be sent…" / "Sent to our payment provider · waiting for them" / "Needs a check by our team" → "Return to your payment method"; past tense only once a step is finished ("Returned to your payment method", all steps ticked). The chef's delivery refund is always its own line ("Delivery · sent separately by the chef · Waiting for the chef"). "Returned so far" in the summary; chef cancellation: apology and 100%. | Contact us |
| 14 | We can't deliver after payment (Q5; choose / choose with delivery already paid / collection chosen / own courier chosen / both results with the £120 delivery refund) | Apology; cake still paid and booked; choose **Collect it · free**, **Send your own courier** (responsibility passes at handover) or **Full refund · everything paid**; "There's no deadline: the chef keeps your order open until you decide" (Q8); collection/courier reveal **every opening time that day** (11:00–17:00, example hourly starts; Q10); deadline unchanged; if delivery was paid it is returned in full whatever they choose. Nothing changes until Confirm. Summary shows "GK delivery · Not paid". Results: "You'll collect your cake" or "Your courier will collect your cake" (you book and pay the courier; responsibility passes at handover), new time, unchanged deadline, and in the delivery-paid case "Your delivery payment of £120 is being returned in full" with a "Delivery refund · requested" summary line. | Confirm my choice → the matching result, or refund "full refund · we couldn't deliver" (£90) / "delivery was paid" (£210: cakes £90 + delivery £120 on its own line); inline errors if nothing/no time chosen |
| 15 | Delivery quote not paid (waiting / delivery payment being checked / pay-by time passed) | Quote (address, date, time, £120) with **"Please pay by 11:00 (London time) on Tue 20 October (48 hours before your delivery)"** (Q9); cake already paid and confirmed; the same three Q5 alternatives; summary "GK delivery · quoted, not paid · £120". Checking variant: "Still checking delivery payment; do not pay again". Pay-by passed: "Your delivery wasn't paid in time… we can't deliver", the same choice with no deadline (Q8/Q9). The r01 checkout delivery note now adds "Delivery must be paid at least 48 hours before your delivery time". | Pay for delivery · £120 → checking variant; Confirm my choice |
| 16 | Free order £0 (checkout / confirm / confirming / refused: order changed / place released / time passed) | Checkout: GIFT100 example code, "To pay now £0", "Continue · confirm your free order", no payment page, "nothing to refund if it's cancelled". Order page: "Confirm your order" (place held 15 min, as live — Q13), confirming spinner, refusal wording by reason from the built UI, one variant per reason (order_changed → Try again; holds_released and window_closed → Start a new order); no review-only text in customer copy. | Confirm order; Try again; Start a new order |
| 17 | r02 item — visible start–end times (times shown / GK delivery requestable / loading / no times / couldn't load) | Radio cards "12:30–13:30 · Available", London time in the legend; delivery shows "Requestable · subject to chef confirmation"; loading skeleton with announced status; "No times… we haven't changed your date" with another date / contact chef; failure with Try again only. Also live in `checkout.html`. | Choose another date; Try again; mode cards switch variants |
| 18 | r02 item — time changes (GK delivery order / collection order) | GK delivery: no time-change control; "Need a different delivery time? Please contact the chef: a GK delivery time can only change with the chef's agreement." Collection (and own courier): unchanged rule, same day until 6 hours before (05:00 for an 11:00 slot), radio list, "doesn't change your refund deadline". | Preview cancellation → state 12; Move to the selected time (preview only) |
| 19 | Order timeline (confirmed · being made / GK delivery · being made / cancel while being made / not collected / collected · ask the chef / collected · refund agreed with the chef / delivery failed · our side (choose, collection chosen, redelivery chosen) / delivery failed · nobody available) | Two lanes side by side, **Money** and **Your cake**, each with its own ticked, current (ringed) and upcoming steps, so money and fulfilment move independently. Being made: paid £90, full refund until the deadline; cake confirmed → being made (neutral "being prepared since…" wording) → ready (collection address about 48 h before) → collected. GK delivery: cakes and delivery paid separately, "Delivery confirmed by the chef", out for delivery, delivered; no time change (contact the chef). Cancel while being made: "Your cake already being made doesn't reduce your refund" — £90 before the deadline, 50% (£45) after, never less. Not collected: cake lane ends "Not collected"; "Refund of £45 requested · automatically" — requested when the chef recorded it (Q19). Collected: "Something wrong with your cake? Contact the chef… agree any refund"; afterwards "Refund of £30 requested: the amount you agreed with the chef" (example; Q20 financial case), while "Collected" stays in the cake lane. **Delivery failed · our side** (Q17): apology, choose "Deliver it again" (chef confirms a new time, nothing more to pay) or "Collect it today · delivery refunded" (any opening time still ahead today; £120 = 100% of the delivery back); results "We'll deliver your cake again" or "You'll collect your cake today" with "Delivery refund of £120 requested". **Delivery failed · nobody available** (Q18): treated like a no-show — "50% of the cake price back: £45, requested automatically"; "The delivery payment of £120 isn't refunded, because the delivery was attempted"; money lane "Delivery payment kept". | Change your time / Preview cancellation / Keep my order; Cancel order · refund £90 → refund requested £90; Confirm my choice (inline errors if nothing or no time chosen) → the matching result; Contact the chef |

Accessibility patterns reused from r01: one `h1`, numbered `h2` sections, visible 3px focus ring, problem banners
and status headings receive focus on arrival, polite live regions only for state changes, radio cards built on
native radios inside `fieldset`/`legend` (arrow keys, forced-colours outline), inline errors linked with
`aria-describedby`, ≥44px targets, reduced-motion stops spinners and shimmer.

The r01 order page (`order.html`) also no longer says "Collection details follow separately" on GK delivery
orders: it says "The chef delivers at the time confirmed with you". The r01 checkout summary no longer says
"We'll email you", and its GK delivery promise says the delivery quote "appears on your order page" instead of
being emailed (Q21; order emails come with GK-031).

## Creator decisions (2026-11-05)

Approved "as suggested". Answers and where r02b applies them:

| Q | Answer | Applied |
| --- | --- | --- |
| 6 | Paid late: the customer agrees or declines on the order page; the buttons record explicit agreement as evidence. The chef may propose a different date, only as an offer the customer must accept. | State 10: new "Chef offers another date" and "You accepted another date" variants; "We keep a record of your answer and when you gave it"; agreed time shown on confirmation; "What happens next" says the answer appears on this page |
| 7 | Paid after cancellation: always a full refund for now (reinstatement needs its own consent rules later). | State 11 unchanged in copy; recorded in its API note |
| 8 | No time limit on the Q5 choice; the case stays open with the chef. | States 14 and 15 (pay-by passed): "There's no deadline: the chef keeps your order open until you decide" |
| 9 | Unpaid delivery quote: deadline 48 h before handover, then the same choice. | State 15: pay-by line and new "Pay-by time passed" variant; r01 checkout delivery note |
| 10 | After a failed delivery: any opening time that day. | State 14 shows all of Thursday's opening times; state 19 (our side) shows the times still ahead today |
| 11 | Terms changed at Continue: a short "what changed" line plus the full terms. | State 2 |
| 12 | Keep "Your bank may take a few days". | States 11, 13 (no change) |
| 13 | The 15-minute hold applies to £0 confirmation (as live). | State 16 (no change) |
| 14 | Collapse unaffected sections to "Kept" after a refusal. | States 1–4 (no change) |
| 15 | Strike through a lost time only if it was the customer's selected time; otherwise drop it. | State 3 (already so); the checkout picker now keeps a lost selected time struck through |
| 17 | GK-caused delivery failure: redelivery or collection, or 100% of the delivery back (K05). | State 19: "Delivery failed · our side" with choose and both results (replaces the deferred note) |
| 18 | Customer-caused failure: treated like a no-show (50% of the cake; delivery payment kept). | State 19: "Delivery failed · nobody available" |
| 19 | The no-show 50% refund is requested automatically when the chef records the no-show. | State 19: "Not collected" copy and money lane |
| 20 | Refunds after handover: the amount the chef agrees (financial case). | State 19: "Collected · refund agreed with the chef", £30 example instead of £45 |
| 21 | Remove "We'll email you" until order emails exist (GK-031). | r01 checkout summary and GK delivery promise |

Question 16 ("anything to change before approving") was answered by the approval itself.

## Review round 1 changes (kept for the record)

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

`screens/` holds 67 full-page renders, each at desktop (1440) and mobile (390) — 134 JPEGs (quality 78, to keep
the repository small): `00-states-index` … `66-delivery-unpaid-pay-by-passed`, listed at the end of `shoot.sh`.
All were re-shot for r02b (including those affected by the dc2bd09 copy fixes). They use
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
- r02b harness (temporary), all passing: our-side delivery failure requires a choice, shows the four later-today
  times, and leads to the collection (with the £120 refund) or redelivery result; nobody-available shows £45 and
  the kept delivery; accepting the chef's other date confirms Sat 24 October; Q5 shows all six day times and "no
  deadline"; the pay-by time shows; "What changed" shows; no "We'll email you" on the checkout; the post-handover
  refund shows the agreed £30. All 84 state/variant links render at 320 and 1440 px without horizontal overflow.
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
- The checkout problem states use a condensed checkout (unaffected sections collapsed to "Kept"), kept by the
  Creator (Q14).
- r01's typed-details-lost-on-date-change limitation remains in the prototype; the live shop keeps details.
- r02b reading of Q17: two choices (redelivery, or collection with the delivery refunded in full); question 24
  confirms whether a third option is needed.
- Order-page wording follows the built UI (`order.ts`, `messages.ts`); r02 adds the "What happens next" lists,
  trackers and Q5 screens, which need new API states (delivery infeasible, delivery quote) not built yet.
- "Your bank may take a few days to show it" is new wording, kept by the Creator (Q12).
- The cancellation boundary is shown as a separate variant ("Deadline passed while open") rather than a live
  timer; the prototype has no clock-driven refresh.
- Deferred: over-capacity chef cancellation is covered only by the generic "We had to cancel your order" refund
  variant. (The r02 deferrals for delivery failure, no-show refunds and post-handover amounts were resolved by
  Q17–Q20 and are now designed.)

## Open questions for the Creator

Questions 6–21 were answered on 2026-11-05 (above). New questions raised while applying them:

22. **Chef offers another date after a late payment:** the new date keeps the original full-refund deadline (K05:
    moves never change it), as shown. Is that right, or should the deadline follow the new date?
23. **After a no-show or a missed delivery** (nobody available): may the customer still collect the cake later that
    day, and if so, is the automatic 50% refund cancelled?
24. **Delivery failed on our side:** r02b offers redelivery, or collection with 100% of the delivery back. Should
    there also be an own-courier option, or a refund of the cake?
25. **Redelivery:** same day only, or another day too (a date change the chef and customer agree)? r02b says only
    "the chef confirms a new delivery time with you".
