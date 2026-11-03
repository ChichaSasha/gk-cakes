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

Five numbered sections plus a sticky order summary (summary moves below on tablet/mobile, with a
“To pay now” line at the top):

1. **When and how** — date (change), Collect free · St Katharine Docks / Your own courier / GK delivery
   from £100, time list that loads automatically (London time).
2. **Your details** — name and email for order updates (preview: stays on the page, not saved).
3. **Discount code** — one per order. `DEMO10` is a labelled example (10% off cakes, rounded half-up,
   never on delivery). `FREEDELIVERY` explains it only covers approved GK delivery. Unknown code, empty
   code and second-code messages are in plain words; applied code can be removed.
4. **Before you pay** — allergens per cake (PEANUTS where present), full cross-contact notice, and the
   **exact cancellation terms** for the chosen time: full refund until handover −120h, then 50%
   (rounded in the customer’s favour), 100% if the chef cancels; date moves don’t change it. Amounts
   follow the discounted total. Confirmation tick-box.
5. **Payment** — secure payment page; 6 accepted methods shown + the other 11 on demand (all 17 accepted
   labels, Google Pay not listed); options depend on device/country; GBP prices, converted amount and
   any fee shown by the payment page.

Summary: cake, date, time, handover, cakes, discount, collection/courier/delivery line, **To pay now**
(GBP), primary **Continue to secure payment · £amount**, and three plain promises: time held 15 minutes
while paying; order confirmed only when payment is received; unclear result is checked — don’t pay
twice. GK delivery disables payment (“Payment opens after the chef confirms delivery”).

The payment-result simulator moved out of the customer flow into a dashed “Design review only” box.
Removed the earlier technical wording (“tentative hold”, “separate clock”, “reconciliation”).

## Preview

`python3 -m http.server 8777 --bind 127.0.0.1 --directory docs/design/sessions/checkout-recovery/gk-017-r01/poc`
→ add a cake (e.g. cake.html?id=pink, date 22 October) → http://127.0.0.1:8777/checkout.html

Renders: checkout, checkout-terms (DEMO10 applied, 11:00 chosen), checkout-full and checkout-delivery,
each desktop and mobile.

## Checks run (targeted, local)

£90 → DEMO10 → £81 (−£9); deadline “Sat 17 October at 11:00” for a 22 Oct 11:00 handover; 50% £40.50;
delivery disables payment with explanation; full submit reaches the order page; desktop and mobile with
no JS errors or horizontal overflow. No full suite or independent review yet.

## Questions for the Creator

1. Section order: is details → code → checks → payment right, or should payment methods sit higher?
2. Is showing 6 payment methods + “11 more” right, or show all 17?
3. Refund wording — clear enough at the moment of paying?
4. Anything to add or remove before I design the problem states (r02)?
