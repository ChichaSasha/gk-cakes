# GK-018 r03b — daily chef operations sketch (small revision of r02b, corrected after review)

Status: **Not approved.** r03 (0e1e954) was drafted from r02b (e26a37c). It applies the two minor items the
r02b independent review left for r03, and it makes the chef screens match the payment, order and refund
states that are now live in the backend. An independent design reviewer returned **revise** on 0e1e954
(3 major, 5 minor, 3 nits). r03b, in the same folder, fixes all of them; see “r03b changes”. r03b still
needs a fresh review and exact-version Creator approval. Round 2 of that review (on ac8456d) left one major
issue, fixed in r03b2; see “r03b2 change”. Example data only. The prototype saves, sends, charges and refunds nothing, and it makes
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
still capped at 24 hours. When both deadlines are the same moment (192–216 h, below), one line says
**“Must start and finish paying by …”** (r03b). The GK-1053 banner, the GK-1078 order (a link in progress) and the GK-1039
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
| `failed` / `cancelled` | Payment failed · may try again / Payment not completed: **only within the 15-minute hold; afterwards the order is released** (below) | GK-1080, GK-1086 |
| order `awaiting_payment` | Waiting for payment (+ the collection state) | list, pill |
| order `confirmation_pending` | **Paid · confirming the order** (nothing to do yet; no promise until it shows Paid) | GK-1079 |
| order `paid_awaiting_agreement` | Paid late · your decision | GK-1039 |
| `cancelled`, `cancel_reason` `payment_not_completed` | **Not paid · released**: nothing taken, nothing owed, not reopened | GK-1080 |
| `cancelled`, `free_not_confirmed` | **Free order not confirmed · released** | GK-1081 |
| refund `requested` / `pending` / `partly_returned` / `returned` / `needs_attention` | Refund owed · nothing sent yet / Refund in progress · not confirmed / Partly refunded / Refunded / Refund needs attention | refund chip on the order, queue |

The day counts follow the hold review: an order whose payment is created, pending or unknown, and an order
being confirmed, keep their hold (“customer paying now”). Released orders don't. The r02b `TENTATIVE`
constant is gone; the example on Fri 30 Oct is now the real order GK-1078.

**Paid late counts in the day (r03b M1).** The backend commits the day place, sells ready cakes and consumes
the code for `paid_awaiting_agreement` exactly as for `confirmed`. Today, the calendar and the honour form
therefore count GK-1039 as a standard place, labelled “paid late · decision pending”: it isn't started, and
“Make it” takes no extra place (“this order already counted”). A full return releases the place; GK-026
defines the release. Thu 29 Oct now shows 8 of 5 (7 accepted at the lowered limit, plus GK-1039).

**Needs attention** replaces the r02b “Recovery queue”. It's the same page (`support.html`), technical
support's home, renamed in the navigation. Groups:

1. **Money · needs a manager decision.** Exactly the cases Today counts (`moneyCases()`; 6 in the example),
   all raised by the payment worker as job attention. The group note says the one automatic step: the full
   return of money that arrived for an order already released (cancelled). Nothing else is sent, refunded
   or confirmed automatically.
   - **Payment arrived after the order was released** (refused paid confirmation, GK-1082). The order stays
     cancelled and is never reopened. The full return started automatically (`late_capture_terminal`, in
     progress). Money arriving for an order that *isn't* cancelled is never refunded automatically.
   - **Payment doesn't match the order** (GK-1083: £95.00 reported for £105). Held, never confirmed.
   - **Refund changed after it was confirmed** (contradiction, GK-1084). Nothing is resent and nothing
     becomes owed automatically; that payment is blocked.
   - **Refund made outside GK Cakes** (quarantined dashboard refund, GK-1050: £20). The order is unchanged;
     the refund blocks workspace refunds for that payment.
   - **Refund failed · won't be resent automatically** (definitive failure, GK-1087; r03b). It stays owed. A
     manager either reserves the remainder again for one new attempt, or arranges a K05 Q5 verified
     transfer with evidence, never both.
   - **Refund needs an action at the provider** (`requires_action`, GK-1085; r03b). Counts as in progress;
     no other refund for that payment.
2. **Your decisions** (Elina's decisions for others): paid late (GK-1039), a chef-owned case, not a manager
   one.
3. **Still checking with the provider**: payment in progress over 15 minutes (GK-1078), payment unknown,
   refund not confirmed, email result unknown. The note no longer says they “resolve by themselves” (r03b
   M3):
   - a refund still unknown after its 23-hour retry window goes to a manager (the GK-1031 card shows when);
   - unknown payments and payment pages moved to reconciliation go to manager tooling;
   - a delayed payment that fails after the customer finished the payment page stays “in progress”, with
     the place held, until a manager rule exists.
4. **Technical (Sasha)** (r03b): jobs in attention for setup reasons, with no money decision.
   - The payment provider refused the worker's key (401): fix the key, redeploy, requeue.
   - A payment page never created (GK-1086, r03b2). The journal went down before the first provider call
     was admitted. The never-dispatched cleanup closed the created collection at 15 minutes (`cancelled`,
     `never_dispatched`), and the hold review released the order (`cancelled`, `payment_not_completed`).
     Nothing was charged; Elina creates a new order and link. `expired_before_dispatch` (payment failed, job
     in attention) only happens when the hold review itself was delayed and a late first call has under
     31 minutes of page lifetime left.
   - The recovery journal was unavailable: jobs waited and continued by themselves; requeue only jobs that
     reached attention.
5. **Emails that didn't arrive**: the bounced-email flow (Creator answer 5), unchanged.
6. **Your / Elina's open to-dos**: collection details and the delivery refund, unchanged.

Manager decision buttons are Elina's only and marked “(manager tool, later)”. Technical buttons (Requeue…)
are marked “(tool later)”. Sasha sees the money cases and “Check again with the provider”, which moves no
money. After a check, focus moves to a status line (“Checked with the provider (recorded): no change”).
Fulfilment sees a role note.

On Today, Elina's and Sasha's “Needs you” has one summary line, “6 money cases need a manager”, the same
number as the queue's Money group; fulfilment doesn't see it. Labels depend on the role: Elina sees
“Paid late · your decision” and “Paid late · decide”; fulfilment and Sasha see “Paid late · Elina decides”.
The order list adds “Needs a manager”, “Waiting for payment or confirming” and “Paid late” filters, and the
Money column uses the new states. The order pages show each case as a banner, progress steps, a Money row
and a refund chip.

## r03b2 change (round 2 of the review, on ac8456d)

- **Major — GK-1086 contradicted the hold review.** r03b showed GK-1086 still waiting for payment with a
  failed collection (`expired_before_dispatch`). The backend's never-dispatched cleanup closes a `created`
  collection that had no admitted call once it's 15 minutes old. The hold review then releases the order.
  GK-1086 now matches that:
  - order: `cancelled`, `payment_not_completed`, “Not paid · released”, released 08:20;
  - collection: `cancelled`, `never_dispatched`;
  - action: “Create a new order and link”.

  The order page no longer has an `expired_before_dispatch` waiting state. The Technical card says the
  cleanup normally closes such payments at 15 minutes, and `expired_before_dispatch` only happens when the
  hold review itself was delayed. “Payment failed · may try again” now says “within the 15-minute hold”,
  and the state table's row is narrowed the same way.
- **Nits:** GK-1087's “Refund needs attention” is labelled as a reading. Today's workload reads “8 cakes
  to make, incl. 1 pending decision”. The audit list records the GK-1086 closure at 08:20.

## r03b changes (independent review of 0e1e954, verdict revise)

- **M1 — paid late is counted.** `paid_awaiting_agreement` is in the committed set: it counts in Today's
  standard places, the calendar and the workload. It's labelled “paid late · decision pending (counted)”
  and isn't started. The honour form says “this order already counted”, and Return says the full return
  releases the place (GK-026). Files: core.js, today.js, calendar.js, order.js.
- **M2 — “Make it” captures K04 Q2.** The form needs:
  - a separate feasibility tick, recorded as Elina at the current time;
  - how the customer agreed (required);
  - when they agreed (required, not in the future);
  - the evidence (required);
  - “The customer agreed to exactly Thu 29 Oct, 12:00–13:00”.

  The button stays disabled, with a “Still needed: …” line, until everything is complete. A line says the
  system rechecks the original date and time, the cake's safety and availability, and recovery (K11) before
  confirming, and that no date is moved automatically. Confirm and Return £90 are marked “Preview only. The
  live action comes with GK-026.”
- **M3 — Needs attention.** The “Still checking” note is corrected (above). New rows cover a definitively
  failed refund, `requires_action`, a payment page never created (GK-1086, corrected in r03b2) and a 401
  key refusal. There's a new
  “Technical (Sasha)” group (key refused, journal down, requeue). The Money note now names the automatic
  late-capture full return.
- **Minor 1:** 192–216 h shows one line, “Must start and finish paying by …”, with a warning that slower
  methods (for example a bank payment) end up paid late. This applies on New order, the link preview and
  GK-1078.
- **Minor 2:** Today's count and the queue's Money group come from the same list (6).
- **Minor 3:** role-dependent wording, “Paid late · Elina decides”, for fulfilment and technical support:
  pill, Needs you, queue, order list and progress.
- **Minor 4:** the finished-cake short-notice note says K04 describes `lead_time_exception` but it isn't
  built, and that the backend refuses starts under 192 h (proposed only). GK-1053 is tagged “proposed ·
  needs K04 amendment” on Today, the order list and its banner.
- **Minor 5:** an r03b buttons table (below).
- **Nits:**
  - The core.js comment on GK-1078 now says “about 213 hours”.
  - “Check again” moves focus to a status message.
  - Open question 4 has suggested customer wording.
  - The technical example times are consistent with the 10:30 checks (key refused 10:35; journal down
    08:10–09:50; GK-1086 started 08:05, closed and released 08:20).

## Buttons and destinations (r03b additions and changes)

C = chef (Elina), F = fulfilment, S = technical support (Sasha). The r02b table in `../gk-018-r02/session.md`
still applies to everything not listed here. “Later” means the live action isn't built yet.

| Where | Button | C | F | S | In the preview | In the live workspace |
| --- | --- | --- | --- | --- | --- | --- |
| New order | Create payment link | ✓ | — | — | Shows “Must start and finish paying by …” or the two deadlines | Order awaiting payment + link (initiation refused after the start deadline: `QUOTE_EXPIRED`/`LEAD_TIME`) |
| Order, paid late | Make it → Confirm the order for Thu 29 Oct, 12:00 | ✓ | — | — | Disabled until feasibility, channel, time, evidence and exact date/time are given; then “not confirmed” | **Later (GK-026)**: honour with recheck of date, cake and K11 |
| Order, paid late | Return the full £90 → Return £90 | ✓ | — | — | “Sent ✓ (preview)” | **Later (GK-026)**: full-return goal for everything paid; place released |
| Needs attention | Check again with the provider | ✓ | — | ✓ | Disables itself; focus to “Checked … no change” | Audited reconcile request; moves no money |
| Needs attention, money | Reconcile… / Review… / Match or explain… / Follow up… | ✓ | — | — | “Designed in a later revision” | **Later (manager tooling)** |
| Needs attention, money | Reserve the remainder again… / Arrange a verified transfer… | ✓ | — | — | “Designed in a later revision” | **Later**: one new provider attempt, or K05 Q5 transfer |
| Needs attention, GK-1082 | Write to the customer / View refund | ✓ | — | — | Opens Messages / the order | Same |
| Needs attention, technical | Requeue 2 jobs… / Requeue jobs in attention… | ✓ | — | ✓ | “Designed in a later revision” | **Later**; today `business.job_requeue` by the operator |
| Needs attention | Decide (paid late) | ✓ | — | — | Opens GK-1039 | Same |
| Today, Needs you | Needs attention (money summary) | ✓ | — | ✓ | Opens the queue | Same |

## Deviations and interpretations

- **Only the state names and the late-capture full return are live.** Customer and chef cancellation goals,
  the chef's paid-late resolution, the full-return release of a paid-late place and all manager and
  technical tools are GK-026 or later (README “Not yet”). The sketch shows those flows as designed, with the
  live names.
- **The order-level refund state's precedence is the sketch's reading**: needs attention, then in progress,
  then refunded, then partly refunded, then owed with nothing sent. The README defines the states but not
  their precedence. A definitively failed refund on a case in attention reads “Refund needs attention”;
  `requires_action` reads “in progress” and also appears as a money case. For a contradiction the
  confirmed amount stays shown with “the provider now reports a failure; a manager decides”. Nothing
  becomes owed again (README: “nothing becomes available”).
- A quarantined outside refund on an *active* order has no refund goal, so the backend's order view shows no
  `refund` block. The sketch shows the case as a banner and a Money row instead.
- **The released (payment not completed) GK-1082 shows “Cancelled · full return”**, not “Not paid ·
  released”, because money did arrive. The backend keeps `cancel_reason = payment_not_completed`.
- Paid late is counted in the day's workload as well as the standard places. It can't be started until
  Elina decides, so Today reads “8 cakes to make, incl. 1 pending decision”.
- “Refund needs attention” on GK-1087 (a definitively failed refund in attention) is the sketch's reading:
  the README doesn't say which order-level refund state such a case reports (see the precedence above).
- The technical-job examples (key refused, journal outage, the never-dispatched GK-1086) are illustrative
  example data. The README names the behaviour, not these times.
- In the 192–216 h band the start window can be very short. r03b warns, but doesn't offer to lengthen it
  (that would be short notice under 192 h); see question 1.
- GK-1081 (a free giveaway order) is example data for `free_not_confirmed`. No owner decision says the chef
  creates free orders.
- **These are GK-018's first committed screenshots.** r01 and r02 recorded visual checks without
  committing images.

## Open questions (for Elina and Sasha)

New in r03:

1. **192–216 h orders get a short window.** For example, at 194 h the customer must start *and finish*
   paying within 2 hours. Is that acceptable as ordinary notice, or should Elina be able to give the
   customer up to 24 hours to start, which would make the order short notice (under 192 h) and need the K04
   amendment?
2. **Money for an order that isn't cancelled** (for example, a second payment after the order was already
   paid). The backend refunds nothing automatically. Who decides, and what are the options: return it, or
   keep it against a change the customer agreed? (GK-026)
3. **Manager decisions = Elina?** The sketch gives Reconcile/Review/Match to Elina only and lets Sasha check
   again and requeue technical jobs. Should Sasha be able to *match* an outside refund to a known one (no
   money moves), or only flag it?
4. **Should the customer's link email state the completion time?** Suggested wording: “To keep your date,
   payment needs to complete by X. If it finishes later, we'll contact you to agree the date or return
   everything.” For a different start time, add “You can start paying until Y.”
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

Open http://127.0.0.1:8780/index.html. The dark bar switches the role and demo states. New in r03/r03b:

- `support.html` (Needs attention), `index.html?date=2026-10-29` and `calendar.html?date=2026-10-29` (paid
  late counted).
- `new-order.html?date=2026-10-30` (192 h, one deadline) and `new-order.html?date=2026-10-24` (short notice).
- Orders: `order.html?id=GK-1039` (Make it form), `GK-1078`, `GK-1079`, `GK-1080`, `GK-1081`, `GK-1082`,
  `GK-1083`, `GK-1084`, `GK-1085`, `GK-1086`, `GK-1087`, `GK-1050`.
- `orders.html?period=all&filter=attention`.

## Screenshots (`screenshots/`, headless Chrome, JPEG)

Most desktop shots were taken with `--headless=new --screenshot` at 1440 px and resampled to 1120 px. The
GK-1039 form (filled in) and the mobile shots were taken through DevTools device emulation; mobile shots
are 390 px, cut at 4200 px tall. With `--window-size=390`, headless Chrome laid the page out wider than
390 px and cropped it, so those images were discarded.

All 18 were re-shot for r03b2 (the preview bar reads “r03b2” after the round-2 fix):

- New order: `new-order-ordinary-192-216h-desktop`, `new-order-ordinary-desktop`,
  `new-order-short-notice-desktop`, `new-order-short-notice-mobile`.
- Queue, days and lists: `needs-attention-desktop` (Elina), `needs-attention-mobile` (Sasha),
  `today-desktop` (Thu 29 Oct, paid late counted), `calendar-paid-late-counted-desktop`,
  `orders-payment-states-desktop`.
- Orders:
  - `order-GK-1039-paid-late-make-it-desktop` (form filled in; replaces `order-GK-1039-paid-late-desktop`);
  - `order-GK-1078-payment-in-progress-desktop`, `order-GK-1082-refused-full-return-desktop`;
  - `order-GK-1083-mismatch-desktop`, `order-GK-1084-contradiction-desktop`;
  - `order-GK-1050-outside-refund-desktop`, `order-GK-1087-refund-failed-desktop`;
  - `order-GK-1086-never-dispatched-desktop`, `order-GK-1080-released-mobile`.

## Checks (r03b)

- `node --check` passes on every `poc/*.js`.
- Headless Chrome over the DevTools protocol, cache disabled, on its own port. 245 cases: 35 pages/states ×
  chef at 1440, 390 and 320 px, plus fulfilment and technical support at 1440 and 390 px. Results:
  0 script exceptions, 0 console errors or warnings, and no horizontal overflow. Every page rendered, and
  fulfilment never sees “£”.
- Scripted (r03b):
  - **M1:** Today Thu 29 Oct shows “8 of 5 standard places … Includes 1 paid late · decision pending
    (GK-1039): counted in the day”. The calendar shows “Paid late · decision pending (counted)”, and the
    honour form says “8 standard places used … this order already counted”.
  - **M2:** Confirm starts disabled, listing all five missing items. Complete enables it. A future time
    (11:30) gives “a time that isn't in the future”. Unticking “exactly” disables it again. Confirming
    shows “Preview only: not confirmed” and takes focus.
  - **Roles:** for fulfilment and Sasha, the GK-1039 pill and their Needs-you title read “Paid late · Elina
    decides”.
  - **M3 / counts:** Today shows “6 money cases need a manager”. The queue groups are Money 6, Your
    decisions 1, Still checking 4, Technical 3, Emails 1, To-dos 2. After “Check again”, focus is on the
    status line.
  - **192–216 h:** Fri 30 Oct 10:00 reads “Must start and finish paying by Thu 22 Oct, 11:00” with the
    slower-methods warning, and GK-1078 reads “must start and finish paying by Thu 22 Oct, 15:00”.
  - **GK-1086** (r03b2) is “Not paid · released”: the payment page was never created, the cleanup closed
    it after 15 minutes, nothing was charged, with the next step “create a new order and link” and a button
    for it. Its Technical card says the cleanup normally closes such payments at 15 minutes. GK-1087 shows
    “Refund needs attention” (a reading).
- The r03 scripted cases still pass:
  - Sat 31 Oct shows the two deadlines (start Fri 23 Oct 10:45, paid in full 13:30).
  - Short notice needs the tick and reason; “Paid in full by” after handover − 2 h is refused.
  - Thu 22 Oct 11:00 is “Too close to take payment”.
  - Return £90 works.

## Not done in r03b

Everything r02b listed as not done (groceries and cost, the money dashboard, the verified transfer, move
and change date, “Report a problem”, account claims, recipe and catalog editing), plus the manager tools
themselves. Nothing was published to Trello and no PR was edited.
