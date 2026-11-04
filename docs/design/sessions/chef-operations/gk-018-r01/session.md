# GK-018 r01b — daily chef operations sketch (Today + order detail)

Status: r01 was committed on 2026-11-03; its session note said it had been shown to the
Creator with feedback pending (not independently verified). r01b (2026-11-04) corrects r01
after an independent design review. **Not approved.** Example data only; the prototype
saves, sends, charges and refunds nothing and makes no network calls.

- Card: GK-018 https://trello.com/c/jjP7btz9 (moved Backlog → In Progress 2026-11-03 on the user's "Yes, take GK-018").
- Base: origin/main 784a717. Visual tokens/fonts reused from approved GK-015 r12 (the reviewer confirmed the tokens match and the assets are byte-identical).
- Model check: host model llmplatform/claude-opus-5-5 (user-selected) vs card policy gpt-5.6-sol/high; the user's selection takes precedence. Author session: continued; review: a fresh independent reviewer with no author discussion.
- Inputs (SHA-256, first 16 hex): card description 7bf6663486190891; gk019/amendments/GK-018.md
  efe1067824f0eae6; contracts.md (r6.5) b34b0926f371683a; owner-data-contract.md
  e71792978a198263; GK-001 decisions.json 6d3112d834930504; GK-003 decisions.json
  41d7792baa5be64e; GK-005 decisions.json d6593cfc91948c32; screens.md d2a9c9db693ab865.
- Owner decisions applied (GK-001 IDs): `lead_time` (192 h: every example payment is at least 8 days
  before its handover; online booking closes for nearer days), `pickup_hours` (Thursday 11:00–17:00,
  Saturday 12:30–15:00, Sunday 11:00–12:00), `production_start` (automatic start at 48 h even if
  processing is late; actual start kept separately), `ready_cancellation` (deadline saved at payment as
  the original handover in Europe/London minus 120 elapsed hours; customer 100% strictly before, 50%
  from then on including no-show; chef cancellation 100% of everything paid including delivery),
  `move_time` (customer self-move up to 6 h before), `address_release` (collection details about 48 h
  before). K00 roles: chef (all), fulfilment (start, ready, hand over only), technical support (read
  only, no money or content authority).
- Bounded scope: A-ORDERS/A-FULFILLMENT Today view and order detail, desktop + mobile. Later revisions:
  calendar & capacity editing, manual order entry, ingredients/batches/stock, groceries/cost, custom
  message compose/preview, money dashboard. AC1 needs manual order entry and capacity adjustment, so r02
  is required before approval.
- Run: `python3 -m http.server 8780 --bind 127.0.0.1` in `poc/`, open `index.html`. The review bar
  switches role (Chef / Fulfilment / Technical support) and, on an order, the state (normal, changed
  while viewing, refund unknown, email bounced).

## Buttons and destinations

| Where | Button | Who | In the preview | In the live workspace |
| --- | --- | --- | --- | --- |
| Today | Previous / Next day | all | “later” notice | Another day’s view |
| Today | Adjust capacity | chef | “later” notice | Calendar & capacity (r02) |
| Today, Needs you | View order / View refund / Check email | all | Opens the order (or “later” for orders not in the preview) | Order detail |
| Today, Needs you | Decide / Send quote | chef (others see View) | “later” notice | Late-payment decision / delivery quote (r02) |
| Today, handovers | Start making / Mark ready | chef, fulfilment | Marks done (preview) | Records the time; batch selection with Start (r02) |
| Today, handovers / order | Hand over → Confirm handover / Not yet | chef, fulfilment | Two-step confirm | Completes the order |
| Order | Move time / Change date… | chef | “later” notice | Same-day move; date change with agreement |
| Order | Write a message | chef | “later” notice | Compose with a fixed preview (r02) |
| Order, bounced email | Update email address / Resend | chef (others: note only) | “later” notice | Fix and resend |
| Order | Cancel and refund… → Cancel order and refund | chef | Preview text only | Cancel; cake refund now, delivery refund as the next step |
| Order | Report a problem | all | “later” notice | Flags the order to the chef |
| Order, changed while viewing | Show latest | all | Shows the moved time | Reloads the latest version |

## Independent review (2026-11-04) and r01b changes

Fresh reviewer verdict on r01: **revise** (10 major, 11 minor, 6 nits). Fixed in r01b:

1. Chef-cancel refund on a delivery order now shows the full amount owed (cake + delivery), with the
   delivery refund as the chef's separate next step (K05; GK-001 `ready_cancellation`).
2. Example data follows the owner rules: Sunday 11:00–12:00; every payment at least 192 h before its
   handover; unknown/late payments and the quote request are for dates more than 8 days away.
3. “Changed while viewing” now comes from another device at 10:44 (before the preview clock) and “Show
   latest” shows the moved time.
4. “Refund unknown” shows the order as Cancelled, with a refund step and no fulfilment actions.
5. Orders past their automatic start show “In production (automatic) · not started by you” on both pages.
6. One capability check everywhere; roles Chef (Elina), Fulfilment (kitchen helper), Technical support
   (Sasha, read only); technical support cannot resend or change email addresses.
7. Capacity text: “2 standard places unused. Online booking for this day is closed (less than 8 days away).”
8. The customer's full-refund deadline is a saved instant per order (no fixed UTC offset; the 25 Oct
   order shows Tue 20 Oct, 12:00 after the clocks change).
9. Hand over needs a confirm step (order, name, how).
10. This record now lists inputs, decision IDs, buttons and Creator questions.

Minor fixes: “Refund result not confirmed” wording (K05 unknown ≠ pending); “50% from then on, including
delivery”; the chef-cancel reasons no longer include “Agreed with the customer”; collection/delivery
details emails added for today's orders; the bounced banner distinguishes a typo from a mailbox that
received earlier emails; “updated” times in London time with the date; 320 px overflow; the brand link
has an accessible name (wordmark kept as the approved r12 “GK Cake London”; see question 8); the start toast no longer promises one-tap ingredient use;
the unused 2.7 MB `composite.png` was removed. Not changed: mobile action order, nav contrast and the
email “sending” state is defined but not shown in an example (nits).

## r02 scope (prioritised by the review)

1. Cancel and refund in full: target vs returned vs reserved/unknown vs still owed; refund the whole
   remaining allocation once; separate delivery refund to-do; blocking while an earlier refund is
   unresolved; customer-requested vs chef cancellation; no-show / after handover (50%); resale at 50%.
2. Manual order entry (AC1): payment link with a 24 h window, 192 h minimum and agreed date, extra place
   outside the standard count, late or unknown payment.
3. Capacity and calendar (AC1/AC2): lowering the limit below bookings, holidays, Saturday 15:00 end,
   affected bookings.
4. Delivery: quote, requested time, paid / FREEDELIVERY, dispatch separate from handover, failure.
5. Ingredients: chef start with batch selection, negative-stock warning with a reason, corrections,
   expiry, discrepancies (AC2).
6. Messages: compose, fixed preview (15 min, bound to recipient and content), recipient changed after
   preview, unknown result and resend.
7. Technical-support recovery queue (audited, read only for money).
8. Finding orders: list and search (production vs pickup, name or number).
9. Groceries and cost; money dashboard.

## Questions for the Creator

1. Can a chef-entered extra or Instagram order have less than 192 hours' lead time, or only a specific
   ready-made cake?
2. Should Today show unused standard places at all once online booking has closed?
3. When a customer asks the chef to cancel, which rule applies: the customer's 100%/50% or the chef's
   100%? Can the chef grant a goodwill amount in between?
4. Who may record a handover: Elina only, or a fulfilment helper? (r01b requires a confirm step.)
5. What may Sasha (technical support) do with a bounced email: fix the address, resend, or only flag it?
6. Delivery quote not paid, or GK can't deliver after the cake is paid: collection, own courier, or a
   full refund? (GK-017 question 5.)
7. Should “Needs you” alert when collection details haven't been sent about 48 hours before?
8. Workspace name: the approved GK-015 r12 wordmark says “GK Cake London”, while emails are sent as
   “GK Cakes” (GK-012). Which should the chef workspace show?

Review round 2 on 9c4195d: **pass** (all majors and minors fixed). Its remaining minor and nits were
fixed afterwards: the hidden “Hand over” button (CSS `[hidden]`) and focus after “Not yet”; a
cancellation email in the refund-unknown state; the extra-cake text for non-chef roles; “including
delivery” only on delivery orders; the wordmark question (8).

## Checks (r01b)

Headless Chrome, 0 script errors: Today at 1440 px (Chef, Technical support) and 390 px; order GK-1038
(chef, cancel preview text checked: owed £255 in total, cake £145 now, delivery £110 next), GK-1036 with
“refund unknown”, GK-1044 as technical support. At 320 px (DevTools protocol), Today and order detail
have no horizontal overflow (scrollWidth 320); this needed `min-width:0` on grid items and wrapping
pills/review groups beyond the reviewer's one-line fix. `node --check admin.js` passes.
