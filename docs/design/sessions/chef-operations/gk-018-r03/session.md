# GK-018 r03 — daily chef operations sketch (small revision of r02b)

Status: **Not approved.** r03 is drafted from r02b (e26a37c). It applies the two minor items the r02b
independent review left for r03, and it makes the chef screens match the payment, order and refund states
that are now live in the backend. It hasn't had a fresh independent review, and it needs exact-version
Creator approval. Example data only. The prototype saves, sends, charges and refunds nothing, and it makes
no network calls. `../gk-018-r01/` and `../gk-018-r02/` are unchanged.

- Card: GK-018 https://trello.com/c/jjP7btz9. Branch `sasha-chycha/gk-018-chef-ops-design`, base head
  e26a37c (r02b, draft PR #15).
- Model note: claude-opus-5-5, host-reported (`llmplatform/claude-opus-5-5`). The card's policy is
  gpt-5.6-sol/high; the user's host selection is what ran. A scoped design worker wrote r03 from a written
  brief plus the files below. It isn't an independent review.
- Visual tokens, fonts and images are unchanged from r02b (GK-015 r12). The workspace shows “GK Cake London”
  (Creator answer C8); emails keep the sender name “GK Cakes”.

## Inputs (SHA-256, first 16 hex)

| Input | Hash |
| --- | --- |
| Card description (`gk018-card.md`) | 9b2535fd34d044d2 |
| Card comments (`gk018-comments.md`: the r01b comment and Creator answers C1–C8 of 2026-11-04) | 353566447ddd245d |
| r02b Trello comment (`gk018-r02-trello.md`: the two items for r03) | 8078717367c427fe |
| AGENTS.md | 73502bdeedc5697d |
| docs/design/contracts.md (r6.5; K04, K05) | b34b0926f371683a |
| docs/design/owner-data-contract.md | e71792978a198263 |
| docs/creator/GK-001/decisions.json / GK-003/decisions.json | 6d3112d834930504 / 41d7792baa5be64e |
| gk-018-r02/session.md (r02b, e26a37c) | 6bce40bdca4af5a7 |
| gk-018-r02/poc core.js / new-order.js / order.js / support.js | da3c460b5f4c1412 / 9f6779c39b168cc9 / 4463840af9b00b73 / 35db831271096e8d |
| api/README.md at origin/main 82c5b0d (read with `git show`; main wasn't checked out) | 4fb440cbd48d5ff8 |
| the same file, “Orders and checkout holds” up to “Not yet verified” (Payments; Full return) | 5a56140ec361fd1c |

These sections are byte-identical at bed0a67 (the commit the brief named) and at 82c5b0d (GK-034 restore,
merged since then). contracts.md, owner-data-contract.md and the GK-001/GK-003 decisions are identical on
main.

## Changes from r02b

### 1. Payment deadlines in K04 wording (review item 1)

r02b had one field, “Customer must pay by”. It didn't separate starting a payment from finishing it, and
it didn't say what happens to a late finish. r03 shows two deadlines on New order, on the created-link
preview and on the order:

- **Can start paying until**: the initiation deadline. It's the earlier of the 24-hour link end (K04 “quote
  sent+24h”) and the last moment the applicable lead time still holds. The backend refuses to start a
  payment after that (`QUOTE_EXPIRED`, `LEAD_TIME`).
- **Paid in full by**: the completion deadline. For ordinary notice this is handover − 192 h. For a chef's
  short-notice order it's the chef's chosen latest confirmation (K04 `lead_time_exception`, never after
  handover; this sketch proposes at most handover − 2 h, as in r02b).
- **A payment started in time that completes after “Paid in full by” is never accepted silently.** The
  order becomes **“Paid late · your decision”** (`paid_awaiting_agreement`). Elina then either makes the
  cake (she can make it and the customer agrees) or returns everything paid, never the 50% charge. Nothing
  is produced until she decides.

On a short-notice order the field Elina sets is now **“Paid in full by”**. “Can start paying until” is
worked out from it: the earlier of the link end and that time. The r02b rule that the time must be within
24 hours is dropped, because completing a payment may take longer than starting it (K04 Q2); starting is
still capped at 24 hours. The GK-1053 banner, the GK-1078 order (a link in progress) and the GK-1039
paid-late card all use these words. GK-1039 now says the customer started before the start deadline
(Wed 21 Oct, 13:00) and the payment completed after the 192-hour point.

### 2. Orders 192–216 hours away are ordinary notice (review item 2)

r02b flagged a link as short notice whenever a payment at the *end* of a 24-hour link would leave under 192
hours (that is, anything under 216 h). r03 treats any handover **192 hours or more away** as ordinary:
there's no short-notice tick or reason and no design-note conflict. “Can start paying until” is simply
192 hours before the handover, which comes sooner than the usual 24 hours. The page says so. If less than an
hour is left to start paying, it warns (for example `new-order.html?date=2026-10-30`, 10:00: 192 h away,
15 minutes to start). **Under 192 hours** it stays Elina's deliberate choice (tick and reason). It's still
flagged as conflicting with GK-001 `lead_time`/`urgent_requests` and K04 until the owner intake and K04 are
amended. The note now adds that the live backend refuses to start such a payment (`LEAD_TIME`).

### 3. The live backend states on the chef screens (api/README.md: Payments, Full return)

| Backend (live) | Chef wording | Where |
| --- | --- | --- |
| collection `created` | Waiting for payment · page being prepared | order pill, Money |
| `pending` | Payment in progress. After 15 minutes: “Still checking payment; do not pay again” (what the customer sees) | GK-1078 |
| `unknown` | Payment result unknown | GK-1042 |
| `unknown` with `mismatch_reason` | **Payment doesn't match the order** · held (never confirmed, place held, no second payment) | GK-1083 |
| `succeeded` | Paid | everywhere |
| `failed` / `cancelled` | Payment failed · may try again / Payment not completed | GK-1080 |
| order `awaiting_payment` | Waiting for payment (+ the collection state) | list, pill |
| order `confirmation_pending` | **Paid · confirming the order** (nothing to do yet; no promise until it shows Paid) | GK-1079 |
| order `paid_awaiting_agreement` | Paid late · your decision | GK-1039 |
| `cancelled`, `cancel_reason` `payment_not_completed` | **Not paid · released**: nothing taken, nothing owed, not reopened | GK-1080 |
| `cancelled`, `free_not_confirmed` | **Free order not confirmed · released** | GK-1081 |
| refund `requested` / `pending` / `partly_returned` / `returned` / `needs_attention` | Refund owed · nothing sent yet / Refund in progress · not confirmed / Partly refunded / Refunded / Refund needs attention | refund chip on the order, queue |

The day counts follow the hold review: an order whose payment is created, pending or unknown, and an order
being confirmed, keep their hold (“customer paying now”). Released orders don't. The r02b `TENTATIVE`
constant is gone; the example on Fri 30 Oct is now the real order GK-1078.

**Needs attention** replaces the r02b “Recovery queue” (same page, `support.html`; technical support's
home; renamed in the navigation). It's grouped as follows:

1. **Money · needs a manager decision**: the four cases the worker raises, plus paid late.
   - **Payment arrived after the order was released** (refused paid confirmation, GK-1082). The order stays
     cancelled and is never reopened. The full return started automatically (`late_capture_terminal`,
     state “in progress”). Elina tells the customer and offers a new order. The card also says that money
     arriving for an order that *isn't* cancelled is never refunded automatically and waits for a manager.
   - **Payment doesn't match the order** (GK-1083: £95.00 reported for £105). Held, not confirmed.
   - **Refund changed after it was confirmed** (refund contradiction, GK-1084). The refund was confirmed,
     then reported failed. Nothing is resent and nothing becomes owed automatically; the payment is blocked.
   - **Refund made outside GK Cakes** (quarantined dashboard refund, GK-1050: £20). The order is unchanged.
     The money counts as possibly returned, and it blocks refunds from the workspace for that payment.
   - **Paid late · your decision** (GK-1039).
2. **Still checking with the provider**: payment in progress over 15 minutes (GK-1078), payment unknown,
   refund not confirmed, email result unknown.
3. **Emails that didn't arrive**: the bounced-email flow (Creator answer 5), unchanged.
4. **Elina's open to-dos**: collection details and the delivery refund, unchanged.

The decision buttons appear for Elina only, marked “(manager tool, later)”: Reconcile…, Review… and Match
or explain…. Sasha sees the cases and “Check again with the provider”, which moves no money; a note says
money decisions stay with Elina. Fulfilment sees a role note. On Today, Elina's and Sasha's “Needs you”
gets one summary line, “4 money cases need a manager”, linking to the queue; fulfilment doesn't see it. The
order list adds “Needs a manager”, “Waiting for payment or confirming” and “Paid late” filters, and the Money
column uses the new states. The order pages show each case as a banner, progress steps, a Money row and a
refund chip.

## Deviations and interpretations

- **Only the state names and the late-capture full return are live.** Customer and chef cancellation goals,
  the chef's paid-late resolution and all manager tools are GK-026 or later (README “Not yet”). The sketch
  still shows those flows as designed in r02b, with the live names.
- **The order-level refund state's precedence is the sketch's reading**: needs attention, then in progress,
  then refunded, then partly refunded, then owed with nothing sent. The README defines the states but not
  their precedence. For a contradiction the confirmed amount stays shown, with “the provider now reports a
  failure; a manager decides”. Nothing becomes owed again (README: “nothing becomes available”).
- A quarantined outside refund on an *active* order has no refund goal, so the backend's order view shows no
  `refund` block. The sketch shows the case as a banner and a Money row instead.
- **The released (payment not completed) GK-1082 shows “Cancelled · full return”**, not “Not paid ·
  released”, because money did arrive. The backend keeps `cancel_reason = payment_not_completed`.
- In the 192–216 h band the start window can be very short. r03 warns but doesn't offer to lengthen it (that
  would be short notice under 192 h); see question 1.
- GK-1081 (a free giveaway order) is example data for `free_not_confirmed`. No owner decision says the chef
  creates free orders.
- **These are GK-018's first committed screenshots.** r01 and r02 recorded visual checks without
  committing images.

## Open questions (for Elina and Sasha)

New in r03:

1. **192–216 h orders get a short start window.** For example, at 194 h the customer must *start* paying
   within 2 hours. Is that acceptable as ordinary notice, or should Elina be able to give the customer up to
   24 hours to start, which would make the order short notice (under 192 h) and need the K04 amendment?
2. **Money for an order that isn't cancelled** (for example, a second payment after the order was already
   paid). The backend refunds nothing automatically. Who decides, and what are the options: return it, or
   keep it against a change the customer agreed? (GK-026)
3. **Manager decisions = Elina?** The sketch gives Reconcile/Review/Match to Elina only and lets Sasha check
   again. Should Sasha be able to *match* an outside refund to a known one (no money moves), or only flag it?
4. **Should the customer's link email state both times** (“start paying by … ; must be completed by …”), or
   only the start time?
5. **Released unpaid orders in the order list**: shown under “Cancelled” as “Not paid · released”. Hide them
   by default?

Still open from r02b (questions 1–9 of r02b's session.md, unchanged): the floor under short notice (Q1),
when a customer's cancellation request counts (Q2), the short-notice refund window (Q3), who sends
collection details (Q4), helpers choosing batches (Q5), Sasha and addresses/provider checks (Q6),
chef-recorded customer agreement (Q7), the short-notice set point (Q8, now the “Paid in full by” limit of
handover − 2 h), and the resale base after a discount (Q9).

## How to view

```sh
cd docs/design/sessions/chef-operations/gk-018-r03/poc
python3 -m http.server 8780 --bind 127.0.0.1
```

Open http://127.0.0.1:8780/index.html. The dark bar switches the role and demo states. New in r03:
`support.html` (Needs attention), `new-order.html?date=2026-10-30` (192 h, ordinary),
`new-order.html?date=2026-10-24` (short notice), `order.html?id=GK-1078`, `GK-1079`, `GK-1080`, `GK-1081`,
`GK-1082`, `GK-1083`, `GK-1084`, `GK-1050`, `GK-1039`, and `orders.html?period=all&filter=attention`.

## Screenshots (`screenshots/`, headless Chrome, JPEG)

Desktop shots were taken with `--headless=new --screenshot` at 1440 px and resampled to 1120 px. Mobile shots
are 390 px, taken through DevTools device emulation (cut at 4200 px tall). With `--window-size=390`,
headless Chrome laid the page out wider than 390 px and cropped it, so those images were discarded.

- New order: `new-order-ordinary-192-216h-desktop`, `new-order-ordinary-desktop`,
  `new-order-short-notice-desktop`, `new-order-short-notice-mobile`.
- Queue and lists: `needs-attention-desktop` (Elina), `needs-attention-mobile` (Sasha), `today-desktop`,
  `orders-payment-states-desktop`.
- Orders: `order-GK-1078-payment-in-progress-desktop`, `order-GK-1082-refused-full-return-desktop`,
  `order-GK-1083-mismatch-desktop`, `order-GK-1084-contradiction-desktop`,
  `order-GK-1050-outside-refund-desktop`, `order-GK-1039-paid-late-desktop`, `order-GK-1080-released-mobile`.

## Checks (r03)

- `node --check` passes on every `poc/*.js`.
- Headless Chrome over the DevTools protocol, cache disabled. 224 cases: 32 pages/states × chef at 1440, 390
  and 320 px, plus fulfilment and technical support at 1440 and 390 px. Results: 0 script exceptions,
  0 console errors or warnings, and no horizontal overflow. Every page rendered, and fulfilment never sees
  “£”.
- Scripted:
  - **New order**: Sat 31 Oct 12:30 gives start until Fri 23 Oct 10:45 (24 h) and paid in full by Fri 23 Oct
    13:30 (192 h). Fri 30 Oct 10:00 is ordinary: start until and paid in full by Thu 22 Oct 11:00 (192
    elapsed hours across the clock change), with a “15 minutes” warning and no short-notice tick.
  - **Short notice**: Sat 24 Oct needs the tick and reason. With them, the link reads start until Fri 23 Oct
    10:45, paid in full by Fri 23 Oct 12:30. “Paid in full by” Sat 24 Oct 11:00 is refused (no later than
    10:30). Thu 22 Oct 11:00 gives “Too close to take payment”.
  - GK-1039 “Return £90” still works. Sasha's queue has “Check again” and no manager buttons; Elina's has 3.
  - Today: Elina's “Needs you” includes “4 money cases need a manager”; fulfilment's doesn't.
  - Calendar Fri 30 Oct lists GK-1078 as “waiting for payment · in progress (held)”.

## Not done in r03

Everything r02b listed as not done (groceries and cost, the money dashboard, the verified transfer, move
and change date, “Report a problem”, account claims, recipe and catalog editing), plus the manager tools
themselves. Nothing was published to Trello and no PR was edited.
