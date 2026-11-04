# GK-018 r02 — daily chef operations sketch (second sketch)

Status: **Not approved.** Drafted on 2026-11-04 from the reviewed r01b and the "GK-018 Creator answers /
2026-11-04". No independent review or Creator feedback yet. Example data only; the prototype saves, sends,
charges and refunds nothing and makes no network calls. r01 (`../gk-018-r01/`) is left unchanged.

- Card: GK-018 https://trello.com/c/jjP7btz9. Branch `sasha-chycha/gk-018-chef-ops-design`, base head
  7ed84cd (r01b, independently reviewed: round 2 pass).
- Model note: claude-opus-5-5, host-reported (`llmplatform/claude-opus-5-5`), against the card's policy
  gpt-5.6-sol/high; the user's host selection is what ran. r02 was drafted by a scoped design-sketch worker
  from a written brief plus the files below, not by r01's author conversation. That isn't an independent
  review: r02 still needs a fresh reviewer.
- Visual tokens, fonts and images come from r01b (GK-015 r12). The workspace shows the approved wordmark
  “GK Cake London”; emails show the sender “GK Cakes”.

## Inputs (SHA-256, first 16 hex)

| Input | Hash |
| --- | --- |
| Card description (`gk018-card.md`, read 2026-11-04) | 9b2535fd34d044d2 |
| Card comments (`gk018-comments.md`: r01b comment + Creator answers) | 353566447ddd245d |
| “GK-018 Creator answers / 2026-11-04” block only | 8d985d87c3c46db9 |
| AGENTS.md | 73502bdeedc5697d |
| skills/gk-feature/SKILL.md | b5a68cb7c520cf1f |
| docs/creator/README.md | 67b15691fdd19f05 |
| docs/creator/GK-001/decisions.json | 6d3112d834930504 |
| docs/creator/GK-003/decisions.json | 41d7792baa5be64e |
| docs/creator/GK-007/interview-2026-10-05.md | a171c851183eff1a |
| docs/design/contracts.md (r6.5; K00, K03–K08) | b34b0926f371683a |
| docs/design/owner-data-contract.md | e71792978a198263 |
| gk-018-r01/session.md (r01b) | a8ff7c3805d04d71 |
| gk-018-r01/poc/admin.js / admin.css (r01b base) | ecae4efa5cb07fa3 / da37d350c053bb25 |
| api/README.md at origin/main e0d8bc4 (Catalog; Schedule, capacity and finished cakes; Discount codes and quotes) | 3bcc9cbbae5391d3 |
| api/README.md at local `sasha-chycha/gk-025-orders` ae22b01 (adds “Orders and checkout holds”) | 17d6643736308bee |

The card description hash differs from r01's (7bf6663486190891): the copy read today includes the GK-019
r6.5 amendment text. “Orders and checkout holds” exists only on the unmerged GK-025 branch, so it's mirrored
as behaviour still to land.

## What r02 adds to r01b

The preview clock is still Thu 22 Oct 2026, 10:45 London time. Today and the order detail keep the r01b
behaviour: the role bar, the stale/moved/refund-unknown/bounced states on GK-1036, the confirmed handover. The cancel panel now asks who wanted the
cancellation (item 1).

1. **Cancel and refund** (order detail). The panel asks who wanted the cancellation. *The customer asked*:
   it compares the saved full-refund deadline with the moment Elina records the request, applies 100% or
   50% of everything paid (delivery included), and offers an optional goodwill amount up to 100%, which
   needs a reason. *I'm cancelling*: 100% of everything paid. The split is one target allocated across the
   cake and delivery payments (floor, then largest remainder). The cake is refunded on cancel; “Refund
   delivery” is the chef's separate next step. A started or ready cake can then be offered for resale at
   base × 50%, halves up, keeping the original handover. Cancelled orders show a **refund ledger**: owed in
   total, refunded (confirmed), sent but not confirmed, still owed, plus per-payment shares. A payment with
   an unresolved refund blocks another one. A failed refund re-opens the whole remaining amount (“Refund
   delivery £55 again”). If the provider definitively can't return it, a verified transfer is noted for a
   later revision. Examples: GK-1029 (four states), GK-1031, GK-1036 “Refund unknown”.
2. **Manual order entry** (`new-order.html`). Source (Instagram, email, in person). Customer full name,
   notice email and an optional payer. A cake from the menu, a custom cake (agreed design, price and the
   recipe its allergens come from) or a finished ready/resale cake. Date, mode and a time from the
   published starts. Day places: a standard place when the limit allows (admission counts accepted cakes
   only), otherwise an extra outside the 7 that still counts in the workload. One discount code. The six
   agreed terms (BO-GK-003.agreement) are ticked. The result is a 24-hour payment link; the date isn't held
   until payment (15-minute hold while paying). The lead-time check shows hours if paid now and if paid at
   the end of the link. Under 192 hours, a warning asks for a deliberate choice: a tick and a reason. It also
   says when the customer's full-refund deadline would already have passed (they'd get 50%), and that under
   48 hours the automatic start comes too late. A **design note** says this needs a K04 amendment. GK-1053
   is a saved example (Instagram, 91 hours, Elina's reason).
3. **Capacity and calendar** (`calendar.html`). A four-week grid shows standard used against the limit,
   extras and a status: open online, full online, **closed online**, closed date, not a working day. The
   selected day shows the limit (override or schedule default), accepted cakes, customers paying now,
   extras, workload, what customers see, and any paid-late case. Elina can save a day limit; lowering it
   below the accepted cakes keeps them all (Thu 29 Oct: 7 accepted at limit 5). She can also choose “Use
   the schedule default (7)” or “Close this date…”. The **schedule version** editor covers standard places,
   a concrete bookable-until date, opening hours, and explicit 60-minute starts added and removed one by
   one. Validation says when a start doesn't fit the hours or touches 01:00–02:00. Closed dates can be added
   or reopened. Before “Publish version 4”, a preview lists the changes and the affected bookings (GK-1068
   on Sun 15 Nov), which keep their time. **Finished cakes** lists ready and resale lots with release and
   withdraw actions and an add form (resale price 50% half up, original handover, required use-by date).
   Demo states: “Day changed elsewhere” and “Schedule changed elsewhere”.
4. **Delivery quote and dispatch** (GK-1047 has five states; GK-1038 has three). The address is shown in
   full to the chef and fulfilment, and only as the postcode district to technical support. A requested
   time is “subject to your confirmation”. Elina enters a quote (from £100, validated), the distance and how
   she measured it, and the confirmed time, then sees a fixed preview of the quote email (a 24-hour payment
   window, with the three alternatives stated). After sending, the quote shows as not paid. “Out for
   delivery” is only possible once delivery is paid; then comes “Delivered · hand over”. **Not paid in 24
   hours** or **GK can't deliver after payment** leads to the customer's choice: collection, their own
   courier, or a full refund of everything paid. With collection or courier after a paid delivery, the
   delivery payment is refunded in full as the separate step.
5. **Ingredients** (`stock.html`, light touch). Warning tiles cover below zero, expired but still on the
   shelf, and last usable day today. Batches can be filtered and show a use-by date (“blocked from …
   00:00”), state and reason. History is shown for three batches. Corrections, disposal and date fixes are
   separate entries that need a reason. Receiving a batch requires a use-by date. **Chef start with batch
   selection** works from Today and the order: nothing is pre-selected and expired batches are disabled.
   Going below zero is allowed with a reason. A unit mismatch (milk in ml against a recipe in g) asks for
   the amount used without converting it. A helper's start leaves “Batches to choose” for Elina.
6. **Message compose and preview** (`messages.html`). The recipient is fixed from the order, the purpose is
   service only, and the counters cover 200 and 10,000 characters. A fixed preview lasts 15 minutes; any
   edit invalidates it. Demo states: preview expired; recipient changed after preview (send blocked, new
   preview needed); send result unknown (resend needs a “may get it twice” acknowledgement). An email trail
   covers all orders, with a problems filter.
7. **Recovery queue** (`support.html`, technical support's home). The bounced email follows the agreed
   steps (flag → Elina fixes → Sasha or Elina resends), with four demo states. Unknown payment, refund and
   email results have an audited “Check again with the provider” that moves no money. Elina's open to-dos
   (collection details not sent, delivery refund owed) are read only for Sasha. A “Recorded today” list
   shows the audit trail.
8. **Order list and search** (`orders.html`). Search covers name, “GK-” number and email, with results
   announced. Orders can be listed by handover or production date (started, or the automatic start).
   Filters cover period, mode and status (needs action, paid · not started, in production, ready,
   cancelled, payment unknown or late). The list collapses to cards on mobile. Fulfilment doesn't see money.
9. **Today additions.** Any working day (previous/next). “N standard places unused · **closed online**”.
   Workload covers standard places plus extras. The “Needs you” list now has 8 items, including the
   computed 48-hour collection-details alert (GK-1048) and the stock warnings. Extra, Instagram and short-notice
   tags appear on handovers. GK deliveries go out for delivery before handover. The chef's “Start making”
   opens batch selection.

## Buttons and destinations

C = chef (Elina), F = fulfilment (kitchen helper), S = technical support (Sasha). “—” = not shown; others see
a role note naming who can act.

| Where | Button | C | F | S | In the preview | In the live workspace |
| --- | --- | --- | --- | --- | --- | --- |
| Today | New order | ✓ | — | — | Opens New order | Manual order entry |
| Today | Previous / Next day, Today | ✓ | ✓ | ✓ | Another working day | Same |
| Today | Adjust capacity | ✓ | — | — | Calendar for that day | Day limit editor |
| Today, Needs you | Send details / Decide / Send quote / Refund delivery / Fix address | ✓ | View | View | Opens the order (Fix address → queue) | Same |
| Today, Needs you | View order / View refund / View ingredients | ✓ | ✓ | ✓ | Opens the page | Same |
| Today / order | Start making | ✓ (batch panel) | ✓ (time only) | — | Batch panel or “started” | Records start; chef batches move stock |
| Today / order | Mark ready · Out for delivery | ✓ | ✓ | — | Marks done | Records time; dispatch needs paid delivery |
| Today / order | Hand over → Confirm handover / Not yet | ✓ | ✓ | — | Two-step confirm naming who records it | Completes the order |
| Order | Move time / Change date… | ✓ | — | — | “later” notice | Same-day move; agreed date change |
| Order | Write a message | ✓ | — | — | Messages, compose for this order | Compose with fixed preview |
| Order | Preview collection details → Send | ✓ | — | — | Preview, then “Sent (preview)” | Authorised details notice |
| Order | Cancel and refund… → Cancel order and refund £x | ✓ | — | — | Live amounts; nothing saved; resale offer | Cancel, cake refund, delivery step |
| Order, refund | Refund cake / Refund delivery £x (again) | ✓ | — | — | “not sent” note | Whole remaining allocation once |
| Order, refund | Arrange a verified transfer… | ✓ | — | — | “later” notice | Q5 alternative payout |
| Order, delivery | Preview quote email → Send quote | ✓ | — | — | Validated preview | Quote with 24 h payment |
| Order, delivery | We can't deliver this… / Delivery problem… | ✓ | — | — | Links to the outcome state | Delivery exception |
| Order, delivery | Record the customer's choice | ✓ | — | — | “Sent (preview)” | Collection / courier / full refund |
| Order, paid late | Confirm the order… / Return £x | ✓ | — | — | “Sent (preview)” | Honour with agreement or full return |
| Order, bounced | Save address | ✓ | — | — | Note; nothing resent | Fix address |
| Order, bounced | Flag to Elina | — | — | ✓ | Opens queue | Flag |
| Order | Report a problem | ✓ | ✓ | ✓ | “later” notice | Flags the order to the chef |
| Orders | Search, Dates by, When, How, status chips | ✓ | ✓ | ✓ | Filters example orders | Server search |
| New order | Create payment link → Copy / Email it | ✓ | — | — | Validation, then preview link | Order awaiting payment + link |
| Calendar | Day cells | ✓ | ✓ | ✓ | Select day | Same |
| Calendar | Save limit / Use the schedule default (7) / Close this date… | ✓ | — | — | Consequences text; nothing saved | `POST /admin/days/{date}/limit`; draft closed date |
| Calendar | Prepare version 4… → Add / × / Preview changes → Publish | ✓ | — | — | Validated draft, affected bookings | `POST /admin/schedule/versions` |
| Calendar | Release / Withdraw / Add a finished cake | ✓ | — | — | Marks done; computed resale price | `POST /admin/lots…` |
| Ingredients | Choose batches | ✓ | — | — | Batch panel | Records helper start's batches |
| Ingredients | Correct… / Dispose… / Fix date… / Add batch | ✓ | — | — | Preview text with reason | Appended stock entries |
| Messages | Preview → Send this message | ✓ | — | — | Fixed 15-minute preview | K08 custom message |
| Messages | Resend (unknown result, after acknowledgement) | ✓ | — | — | “Resent (preview)” | Linked possible-duplicate resend |
| Recovery queue | Flag to Elina | — | — | ✓ | “Flagged (preview)” | Flag on Elina's list |
| Recovery queue | Save address | ✓ | — | — | “Saved (preview)” | Fix address |
| Recovery queue | Resend (only after the fix) → Resend / Not yet | ✓ | — | ✓ | Confirm step | Resend to fixed address, audited |
| Recovery queue | Check again with the provider | ✓ | — | ✓ | “still unknown” | Reconcile; no money moved |

## Owner rules and Creator answers applied

GK-001: `daily_limit` (7 standard cakes counted per cake on the handover date; 7 → 5 keeps 7; only the chef
cancels), `capacity_exceptions` (chef extras outside the count, no upper bound invented), `lead_time` (192 h,
equality allowed; online closed nearer), `pickup_hours`/`pickup_slot` (Thu 11:00–17:00, Fri 10:00–17:00, Sat
12:30–15:00 with starts 12:30–14:00, Sun 11:00–12:00; 60-minute appointments inside the hours),
`production_start` (automatic at payment + 48 h; moves no stock), `move_time` (6 h), `address_release`
(about 48 h), `schedule_admin` (closed dates and versions configured by the chef), `gk_delivery` (from £100,
chef-quoted, paid separately). GK-003: `cancellation_early`/`cancellation_late` (100% strictly before
handover − 120 h, saved at placement and never moved; 50% from then on), `business_cancellation` (100%
including delivery), `refund_allocation`/`refund_execution` (one target; separate chef delivery refund; whole
remainder; unresolved blocks), `refund_rounding` (half up), `resale` (50%, original handover),
`delivery_failure` (100% delivery), `quote_validity`/`manual_order_link` (24 h), `checkout_hold` (15 min),
`agreement`, `cancelled_order` (no reopening). GK-007: one code; percentage on cake only; chef-chosen batches;
below zero with a reason; mandatory use-by date with a London-midnight block; no restock on cancel; units are
never converted without measurements. Contracts K03–K08, K00 roles (fulfilment: prepare/ready/dispatch/
handover).

“GK-018 Creator answers / 2026-11-04”:

1. **Under 192 hours for chef-entered or Instagram orders, any cake, when the chef chooses.** The New order
   page shows a warning that needs a tick and a reason. GK-1053 is a saved example with its “Short notice ·
   your choice” banner. **Needs a contract amendment**: K04 only allows a shorter lead for a specific
   ready-made/resale cake (`lead_time_exception`); the design note says so on screen.
2. **“closed online”.** Today, the calendar and New order show unused standard places with that label once
   every time on the day is under 192 hours away.
3. **Customer-asked cancellation.** The customer's 100%/50% rule applies, and goodwill can be added up to
   100% with a reason. The reason is kept on the order and visible to Elina and technical support. This fits
   K05's bounded chef refund goal.
4. **Handover by Elina or a fulfilment helper.** Both roles see “Hand over”, and the confirm step names who
   records it.
5. **Bounced email.** Sasha flags it → Elina fixes the address → Sasha (or Elina) resends. Sasha's Resend is
   disabled until the fix. The order page and the recovery queue show the same three steps. Contract note:
   K00 gives technical support no routine money or content authority, and a resend to a chef-fixed address is
   neither, but K08 should record this explicitly.
6. **Delivery unpaid, or impossible after payment.** The customer chooses collection, their own courier, or
   a full refund of everything paid. It's recorded as the customer's choice, not the 50% charge. After a paid
   delivery that GK can't make, the delivery money is refunded in full (`delivery_failure`).
7. **48-hour alert.** “Needs you” lists any active order whose collection or delivery details haven't gone
   out once handover is within about 48 hours. The alert is computed (GK-1048); later orders show “due about …”.
8. **“GK Cake London” in the workspace; “GK Cakes” in emails.** Shown in the brand, page titles and
   accessible names. Every email preview uses From `GK Cakes <orders@gkcake.com>`.

## Open questions (for Elina and Sasha)

1. **Is there any floor under the chef's short notice?** GK-001 `urgent_requests` says online handover under
   48 hours isn't orderable. Answer 1 lifts the 192-hour minimum for chef orders. Can Elina go below 48
   hours, or to the same day?
2. **When does a customer's cancellation request count?** The customer may message on Instagram before their
   full-refund deadline while Elina records it after. K00 uses the time the system receives the command, so
   recording later could turn 100% into 50%. Should Elina be able to enter when the customer asked (with
   evidence)?
3. **Short-notice refund window.** A chef order taken less than 120 hours before handover has passed its
   full-refund deadline before payment, so the customer only ever has 50%. Is that intended for chef orders,
   or should they get a different deadline?
4. **Who sends collection details?** Are they sent automatically about 48 hours before, or only when Elina
   presses Send? (`address_release` leaves the actor open.) The alert works either way.
5. **Can a fulfilment helper choose ingredient batches?** K07 and GK-007 say “chef-selected”. r02 lets a
   helper record the start time and leaves the batch choice to Elina (“Batches to choose”).
6. **Technical support and addresses / provider checks.** r02 hides delivery addresses from Sasha except the
   postcode district, and lets Sasha run an audited “Check again with the provider” that moves no money. Is
   that the right boundary?

## Not done in r02

Groceries and cost; the money dashboard; the verified-transfer flow (K05 Q5); move time and change date;
“Report a problem”; account claims; recipe and catalog editing; delivery “requestable” time agreement beyond
the quote; post-cancellation stock review screen (described in text only); independent review and Creator
approval. Nothing is pushed, published or posted to Trello, GitHub or Slack.

## How to view

```sh
cd docs/design/sessions/chef-operations/gk-018-r02/poc
python3 -m http.server 8780 --bind 127.0.0.1
```

Open http://127.0.0.1:8780/index.html. The dark bar switches the role (Chef / Fulfilment / Technical support,
kept for the tab) and, where a page has them, demo states. Good starting links: `order.html?id=GK-1036`
(cancel with goodwill), `order.html?id=GK-1029` (refund ledger), `order.html?id=GK-1047` (delivery),
`order.html?id=GK-1039` (paid late), `new-order.html`, `calendar.html?date=2026-10-29`, `stock.html`,
`messages.html`, `support.html?role=support`, `orders.html`.

## Checks (r02)

- `node --check` passes on every `poc/*.js`.
- Headless Chrome (DevTools protocol), 56 page/state/role/width cases: 0 script errors, 0 console errors and
  no horizontal overflow at 1440, 390 and 320 px.
- 28 scripted interactions:
  - GK-1036 customer-asked cancel: £40.50; with £10 goodwill, £50.50 (blocked until a reason is given);
    chef cancel £81; resale £45.
  - GK-1038 at 50%: £127.50, split cake £72.50 / delivery £55; with £20 goodwill, £83.87 / £63.63.
  - GK-1029 ledger: owed £115, refunded £60, still owed £55.
  - Delivery quote below £100 refused.
  - Batch panel blocks until every batch is chosen and a below-zero reason is given; the expired batch is
    disabled.
  - New order lists its validation errors, then creates the link.
  - Schedule start 14:30 refused for Saturday; publish preview lists GK-1068.
  - Editing a message after its preview blocks Send.
  - Queue resend has a confirm step.
- Visual check of screenshots: Today, order, calendar, ingredients, messages and queue at desktop width;
  Today, New order and calendar at 390 px.
