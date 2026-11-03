# GK-017 r01 — checkout screen (first sketch)

2026-11-03. Card [GK-017](https://trello.com/c/1YGOcOHR) moved to In Progress on the Creator's instruction.
r01 covers the **checkout screen** only, shown early for feedback. The problem states (time taken while
paying, changed price, unknown/late payment, cancellation cutoff, pending refund) follow in r02.
Not approved.

## Execution check

Role: GK-017 design author (Owner: Agent, Kind: Design). Card policy requests gpt-5.6-sol/high in a fresh
worker; host reports `llmplatform/claude-opus-5-5`, continuing the current conversation — **mismatch;
current user-selected model takes precedence**. Not independent. Inputs: approved GK-015 r12 (visual
foundation, unchanged copy as the base), GK-019 r6.5 contracts K03/K04/K05/K06 and handoff, GK-007
discount interview, accepted payment-method inventory, live GK-017 card (description hash c6f25f1a…).
Writes: this folder only, on branch `sasha-chycha/gk-017-checkout-design` from main `4596c92`.

## What the checkout shows (AC1)

Five numbered sections plus an order summary (sticky on desktop, capped to the screen height with
Continue pinned at its bottom; below the form on tablet and mobile, with a "To pay now" line at the top):

1. **When and how** — date (change), Collect free · St Katharine Docks / Your own courier
   (responsibility passes to them at handover) / GK delivery from £100. The time list loads
   automatically, announces loading and the result, and keeps your time when it is still available.
2. **Your details** — name, email, optional "Someone else is paying" with payer name (preview: stays on
   the page, not saved).
3. **Discount code** — one per order. `DEMO10` (labelled example) takes 10% off cakes, never delivery.
   `FREEDELIVERY` works only with GK delivery, is saved as the order's single code pending the chef's
   distance check, and never covers your own courier. Applied code shows as a chip with Remove and the
   new total; it survives a date change.
4. **Before you pay** — allergens per cake (PEANUTS called out), kitchen notice, and the exact refund
   terms: "Cancel **before** [time] (London time) on [date]" → full refund of the amount actually paid;
   from then on (or if the cake isn't collected) 50%, rounded in the customer's favour; 100% if the chef
   cancels; once paid, moving date or time doesn't change the deadline; after handover contact the chef.
   Approved statutory-rights notice. Confirmation tick-box.
5. **Payment** — secure payment page shows the available options; "Ways to pay include" 6 methods, all 17
   accepted labels on demand (Google Pay not listed), with a preview note that the list is still being
   confirmed with the payment provider;
   prices and refunds in GBP; other currencies shown with fees by the payment page.

Summary promises: place on this date held 15 minutes while paying, and a still-processing payment is
waited for; order confirmed only when payment is received; unclear result is checked — don't pay twice.

GK delivery mode (Creator decision 2026-11-03: pay for the cake now, delivery afterwards): a delivery
address is required; the time is "requested, to be confirmed"; "To pay now · cakes" with "Pay for cakes ·
£X"; the hold promise applies. After payment the chef confirms address and time and sends the delivery
quote (from £100), paid separately unless FREEDELIVERY is approved. The refund deadline is set from the
requested time at payment and does not move if the chef confirms a different time; refunds cover
everything paid including delivery. If delivery is impossible, the screen only says the chef will contact
the customer (outcome not yet decided — question 5).

## Buttons and destinations

| Control | Prototype | Live shop |
| --- | --- | --- |
| Change date | Opens the availability calendar; page reloads with the new date (typed details are lost in the prototype; the live shop must keep them) | Same calendar, details kept |
| Mode cards | Switch handover option; reload available times | Same, from live availability |
| Apply / Remove (code) | Validate example codes locally; chip with Remove | Server-validated code reservation |
| Full policies | Opens policies in a new tab | Published policies |
| Show all 17 methods | Expands the list | Qualified list |
| View order (mobile) | Jumps to the summary | Same |
| Continue to secure payment (delivery: Pay for cakes) | Inline errors + summary if anything is missing; otherwise "Opening secure payment…", then the design-review result page | Hosted secure payment page; result page after the authoritative result |
| Design review box | Chooses the simulated result | Not present |
## Preview

`python3 -m http.server 8777 --bind 127.0.0.1 --directory docs/design/sessions/checkout-recovery/gk-017-r01/poc`
→ add a cake (e.g. cake.html?id=pink, date 22 October) → http://127.0.0.1:8777/checkout.html

Renders: checkout, checkout-terms (DEMO10 applied, 11:00 chosen), checkout-full and checkout-delivery,
each desktop and mobile.

## Checks and review

Targeted local browser checks: £90 → DEMO10 → £81; pink + chocolate £180 → £162; deadline
"before 11:00 (London time) on Sat 17 October" for a 22 Oct 11:00 handover; 50% rounded up; inline
errors and error summary; time kept across collect/courier; FREEDELIVERY rules; delivery mode text;
submitting state; order page keeps the discounted total and refund preview; 1440/390/320 without JS
errors or overflow. New text contrast ≥ 6.48:1, focus ring 7.9:1 (reviewer-measured).

Independent AI review on PR #6 (round 1, head fb04036): two specialist reviewers in fresh contexts
(checkout UX/accessibility; payments and refund-policy rules), both **revise**, no blockers. All findings
fixed except those deferred to r02 (round 2 on 60fc88c: both revise, one major each — Continue
out of reach on short desktop screens; FREEDELIVERY kept on a collection order after a date change — fixed
with the minor items; round 3 on d4ee7f3: payments pass, UX revise — page scroll trapped over the summary — fixed): delivery request form, already-past deadline
variant, free-order (£0) path, "last place taken" when Continue is pressed, and keeping typed details
across a date change (prototype limitation, specified for the live shop). Record: PR #6 comments.

## Creator decisions (2026-11-03)

Answers, verbatim: “pay for the cake now and pay for delivery afterwards”; “1. RIGHT”; “2. six payment
methods”; “3. Yes”.

0. GK delivery: pay for the cake now; delivery quoted by the chef and paid afterwards. Applied above.
1. Section order details → code → checks → payment: kept.
2. Six payment methods plus “show all 17”: kept.
3. Refund wording: clear; kept (delivery variant adapted for decision 0).

## Open questions for the Creator

4. Anything to add or remove before I design the problem states (r02)?
5. If the chef can’t deliver to an address after the cake is paid, what should the customer get: switch to
   collection or own courier, or a full refund if they prefer? The rules don’t cover this case yet.
