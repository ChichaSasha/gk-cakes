# GK-017 r02 — checkout and order problem states

2026-11-05. Card [GK-017](https://trello.com/c/1YGOcOHR), In Progress. r02 covers the **problem states** the
Creator asked for on 2026-11-04, plus the two r02 items (visible start–end times as a radio list; no delivery
time self-change on GK delivery orders). It reuses the Creator-approved r01 checkout (look, copy style,
accessibility patterns) on the GK-015 r12 foundation. **Not approved** — needs exact-version Creator approval,
then the fresh integration review (AC3).

## Execution check

Role: GK-017 design author (Owner: Agent, Kind: Design), bounded r02 sketch. Card policy requests
`gpt-5.6-sol` / high in a fresh scoped worker. Host reports `llmplatform/claude-opus-5-5` (system environment),
running as a **fresh subagent** with only the master's brief and the files below — **model mismatch; the
current user-selected model takes precedence** (status: host-reported, not the requested model). This worker is
the author, so it is not an independent review. Writes: this folder only, worktree branch
`sasha-chycha/gk-017-r02-design` from `origin/main` 7df96d0. `api/`, `ui/`, r01 files and other worktrees untouched.

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

## States (AC2) — what each shows

Checkout re-shown after a refusal at Continue: the problem banner comes first (focused, `role="alert"`), the
affected section is open, and untouched sections collapse to a "Kept" line, so the customer can see that their
details survived. Nothing is charged; nothing is replaced for them.

| # | State (variants) | What the customer sees | Buttons → destination |
| --- | --- | --- | --- |
| 1 | Price changed / total refreshed (PRICE_CHANGED, QUOTE_EXPIRED) | "The price has changed" or "We've refreshed your total"; old price struck through beside the new one; refund terms show the new amounts with an "Updated" badge; tick-box cleared with "Your earlier tick was cleared because the total changed". | Continue → inline tick error until ticked; then "Opening secure payment…" → state 8 (demo) |
| 2 | Refund terms updated (POLICY_CHANGED) | Full current terms marked "Updated"; total unchanged; tick cleared. | Same as 1 |
| 3 | Time taken / date now full (SLOT_UNAVAILABLE; day capacity reached) | Time: fresh radio list, the lost time shown struck through "No longer available", **no time pre-selected**; choosing one fills the new refund deadline; tick cleared because the deadline follows the time. Date: "Thu 22 October is now fully booked", choose another date or contact the chef; Continue disabled ("Choose another date first"). | Choose another date → calendar dialog; Continue → time and tick errors, then payment |
| 4 | Cake no longer available (ITEM_UNAVAILABLE) | Ready-made Chocolate swirl marked "Sold · No longer available"; total withheld until a new quote; never swapped. After removing: "Chocolate swirl removed · new total", re-tick. | Remove and update the total → variant "after removing"; Back to your bag → bag |
| 5 | Other refusals | LEAD_TIME (checkout and order-page wording), CODE_USED_UP, PAYMENT_IN_PROGRESS, using the built wording. | Choose a later date; Start a new order; Review the new total; View your order |
| 6 | Still checking payment (polling / stopped / payment page still open) | Title "Still checking payment; do not pay again"; place stays held past 15 minutes; "This page updates by itself" ticker (visible, not announced each second); after 5 minutes "We're still waiting… Refresh to check"; "What happens next" list. If the provider page is still open: "Back to the secure payment page — same payment, not charged twice". | Refresh status; Back to the secure payment page; Contact us |
| 7 | Result unclear — a person is checking | "We're checking your payment": a team member checks; don't pay again; place and code stay with the order; if paid → confirmed, or everything returned if the date can't be met; if not paid → nothing charged. No pay button. | Contact us (with order reference) |
| 8 | Payment received — confirming (`confirmation_pending`) | Spinner, "usually a few seconds", don't pay again. | — (page updates) |
| 9 | Time ran out — place released (`cancelled`, `payment_not_completed`) | "Your place was released… No payment was taken." New order re-checks price and times. Paid elsewhere? Contact us. | Start a new order with the same cakes → checkout with the cake re-added (new quote, new hold) |
| 10 | Paid after the preparation time (`paid_awaiting_agreement`; waiting / chef offers a date / agreed) | "Payment received"; date and time labelled **requested (not confirmed yet)**; chef will contact; never moved without agreement; if not agreed, everything back (£90); don't pay again. Offer variant: "The chef can make your cake for Thu 22 October… Do you agree?" | Contact the chef; Yes, confirm this date → confirmed with standard terms; No, refund everything → refund requested (full) |
| 11 | Paid after the order was cancelled (requested / pending / returned / needs a check) | "Payment received after this order was cancelled": payment kept safe and all of it being returned; 3-step tracker; wording follows the API refund state, "returned" only when the API says so; cake not made. | Start a new order; Contact us |
| 12 | Cancellation preview (before / deadline passed while open / after / after with GK delivery) | Inline "Cancel your order?" panel: amount back, which side of the deadline, time left (as of a server time). Boundary: alert "The full-refund deadline has just passed… now £45… confirm again", button relabelled with the new amount. After: 50% "rounded in your favour" with the labelled rounding example; "If we have to cancel, you always get 100% back". Delivery: split cakes £45 (to your payment method) + delivery £60 (sent by the chef). | Cancel order · refund £X → state 13; Keep my order → order page |
| 13 | Refund progress (requested / pending / partly returned / returned / needs a check / we cancelled / full refund after date not agreed) | Built refund lines verbatim with a tracker; partly returned lists cakes returned ✓ and delivery pending (chef); "Returned so far" in the summary; chef cancellation: apology and 100%. | Contact us |
| 14 | We can't deliver after payment (Q5; delivery not paid / delivery already paid / collection chosen) | Apology; cake still paid and booked; choose **Collect it · free**, **Send your own courier** (responsibility passes at handover) or **Full refund · everything paid**; collection/courier reveal the radio list of times that day; deadline unchanged; if delivery was paid it is returned in full whatever they choose. Nothing changes until Confirm. | Confirm my choice → "You'll collect your cake" or refund requested (full); inline errors if nothing/no time chosen |
| 15 | Delivery quote not paid (waiting / delivery payment being checked) | Quote (address, date, time, £120); cake already paid and confirmed; "We can deliver only once the delivery is paid"; the same three Q5 alternatives. Checking variant: "Still checking delivery payment; do not pay again". | Pay for delivery · £120 → checking variant; Confirm my choice |
| 16 | Free order £0 (checkout / confirm / confirming / refused-try again / refused-start again) | Checkout: GIFT100 example code, "To pay now £0", "Continue · confirm your free order", no payment page, "nothing to refund if it's cancelled". Order page: "Confirm your order" (place held 15 min), confirming spinner, refusal wording by reason from the built UI. | Confirm order; Try again; Start a new order |
| 17 | r02 item — visible start–end times (times shown / GK delivery requestable / loading / no times / couldn't load) | Radio cards "12:30–13:30 · Available", London time in the legend; delivery shows "Requestable · subject to chef confirmation"; loading skeleton with announced status; "No times… we haven't changed your date" with another date / contact chef; failure with Try again only. Also live in `checkout.html`. | Choose another date; Try again; mode cards switch variants |
| 18 | r02 item — time changes (GK delivery order / collection order) | GK delivery: no time-change control; "Need a different delivery time? Please contact the chef: a GK delivery time can only change with the chef's agreement." Collection (and own courier): unchanged rule, same day until 6 hours before (05:00 for an 11:00 slot), radio list, "doesn't change your refund deadline". | Preview cancellation → state 12; Move to the selected time (preview only) |

Accessibility patterns reused from r01: one `h1`, numbered `h2` sections, visible 3px focus ring, problem banners
and status headings receive focus on arrival, polite live regions only for state changes, radio cards built on
native radios inside `fieldset`/`legend` (arrow keys, forced-colours outline), inline errors linked with
`aria-describedby`, ≥44px targets, reduced-motion stops spinners and shimmer.

## Preview

```sh
python3 -m http.server 8792 --bind 127.0.0.1 --directory docs/design/sessions/checkout-recovery/gk-017-r02/poc
```

Open <http://127.0.0.1:8792/states.html> (index) or e.g.
<http://127.0.0.1:8792/recovery.html?state=cancel&v=boundary>. Real checkout with the radio list:
<http://127.0.0.1:8792/checkout.html?seed=pink>. Screenshots: with the server running, `sh shoot.sh` from this
folder (headless Chrome; `poc/shot.html` frames each page at exactly 1440 or 390 px, because headless Chrome's
window is at least 500 px wide, and reports the full height).

`screens/` holds 47 full-page renders, each at desktop (1440) and mobile (390) — 94 JPEGs (quality 78, to keep
the repository small): `00-states-index` … `46-order-page-delivery`, listed at the end of `shoot.sh`. They use
screenshot mode (`?shot=1`): no focus move, no polling ticker and no site footer; everything else is as on screen.

## Checks done

- Scripted browser checks (temporary harness, not committed), all passing: checkout radio list keeps the time
  across collect/courier with announcement, summary and refund deadline follow the radio, list is busy while
  loading, delivery times are requestable, validation without a stray time error; price state focuses the banner,
  requires re-tick, then continues; slot state disables the lost time, pre-selects nothing, requires a time and
  updates the deadline; Q5 requires a choice and shows/hides times; delivery order has no time change, collection
  keeps it.
- Every state and variant renders at 320, 768 and 1440 px with no horizontal overflow (one 8px overflow from a
  long API note in the review switcher at 320 px was fixed with `overflow-wrap`).
- New colours (WCAG contrast): review text 13.1:1, review API note 10.7:1, re-tick note 8.4:1, muted text on the
  unavailable time 6.4:1, "Kept" 6.5:1, warning border 5.4:1 (non-text); r01 focus ring unchanged.
- `python3 scripts/check_planning_kit.py` (it does not scan `docs/design/sessions`, run for CI parity).
- No independent review yet: T3's fresh review is still required after the Creator's feedback.

## Deviations and notes

- Model mismatch (above). A read-only Glean memory-schema lookup was made by mistake during the execution check;
  no data from it was used.
- The checkout problem states use a condensed checkout (unaffected sections collapsed to "Kept"); proposed for the
  live shop, not yet decided (question 14).
- r01's typed-details-lost-on-date-change limitation remains in the prototype; the live shop keeps details.
- Order-page wording follows the built UI (`order.ts`, `messages.ts`); r02 adds the "What happens next" lists,
  trackers and Q5 screens, which need new API states (delivery infeasible, delivery quote) not built yet.
- "Your bank may take a few days to show it" is new wording (question 12).

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
