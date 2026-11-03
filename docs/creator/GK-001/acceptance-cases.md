# GK-001 worked acceptance cases — manual-handover-r2

Status: developer-prepared examples for human review, **not owner acceptance or
application test results**. No application implementation, live ticket change or
policy activation is represented by this document. Read with [the intake](README.md),
[structured decisions](decisions.json), [source evidence](source-comments.json),
[the historical ticket criteria](../../design/tickets/GK-001.md) and
[SQL/admin mapping](../../design/owner-data-contract.md).

## Evidence and execution check

Role: bounded GK-001 acceptance-case author using the `gk-feature` worker workflow.
Requested model/effort: `gpt-6-astra` / `high`; master's accepted explicit dispatch
reported selection, but effective model/effort are not exposed. Status:
**requested-only**, not confirmed. History: fresh worker via `fork_turns=none`.
Independence: author, not independent reviewer. Inputs are sufficient for these worked acceptance cases, including the user’s
subsequent explicit original-deadline and both-cake-types answers. Application
implementation and operational activation remain outside this assignment. Git HEAD at read:
`bb96a89681bcf2fa1c663a02eacbe039e20e6363`; pre-existing staged/untracked work remains.

Current new source is [Sasha's exact chat reply](submission-2026-10-05.uk.txt),
SHA-256 `a51fd7c0287d7c55163b3ac4d55c6f938152cb6f31b96762c2327ad1909b8923`,
received in this task on 2026-10-05. It is developer/user input, not a new direct
chef statement or a verified Trello comment. The source explicitly confirms exact
50%, inclusion of no-show and post-handover requests, chef cancellation at 100%,
entitlement calculated only at placement and unchanged by date/time moves, a
192-hour minimum, personal date clarification with the chef/admin, and preservation
of all seven accepted cakes when a limit is reduced to five. It also requests
execution of the manual handover steps. The master owns source ingestion,
cross-file reconciliation, independent review and any authorized Trello action.

Two subsequent direct user replies resolve the initially ambiguous refund rule:

- `chat:GK-001:2026-10-05:refund-deadline`: “Save original deadline: 100% more
  than 120 hours before original handover; exactly 50% afterwards. Moving date/time
  never changes that deadline.”
- `chat:GK-001:2026-10-05:refund-scope`: “Yes—use the new policy for both cake
  types; retain the old wording only as history.”

These exact reply texts were relayed by the master from the user's responses,
not supplied by a simulated chef/reviewer. The identifiers are local chat source
identifiers, not Trello comment IDs. The final handover manifest/source evidence
records their hashes and integrated decision revisions. Both ready-stock and
made-to-order cases below use these answers; older contradictory refund wording
is history only.

These r2 author-time fingerprints identify inputs before the master's parallel
reconciliation, rather than claiming they are the final output manifest:

| Input | SHA-256 |
| --- | --- |
| `docs/creator/README.md` | `72c408d56d3f61e1d9ffe64761998ef81f4343b2c644156a0d5f2c75fb498698` |
| `docs/creator/GK-001/README.md` | `ca6f1c8e077d526aaaa3243e32728b0b7e6288517fcdf99e3591703c00c029ea` |
| `docs/creator/GK-001/handover.md` | `a3642937976539b6ea62575e4d658ccc9de22cdb5a04875c189201e5087690d3` |
| `docs/creator/GK-001/source-comments.json` | `19abdd86d87fc26986a4c868b1be099bb7f4443b67f97a8b0dbf08b4e835a54d` |
| `docs/creator/GK-001/decisions.json` | `baa52584641835bb96c53cefeedf834256bf6b1f7b0a59745e032a83b7015902` |
| `docs/design/owner-data-contract.md` | `6b2884521f785588a3427e68333809c935c5cbd9a27f83c98661a77f7f8730ab` |
| `docs/design/tickets/GK-001.md` | `f577d353e145d5f3948552705f10a5f01b7e4e71c809e9eb6ea9a958404bb061` |

### Current adjacent GK-003 payment and delivery evidence

Read the exact originals in [GK-003 source evidence](../GK-003/source-comments.json)
and `payment_methods`, `quote_validity`, `refund_allocation`, `refund_execution`
in [its current decisions](../GK-003/decisions.json). These direct Sasha chat
answers supersede the older off-site/residual-balance/payment wording wherever
it affected these examples; they are not invented chef or Trello statements.

| Source / author-time input | SHA-256 |
| --- | --- |
| `chat:GK-003:2026-10-05:website-payment-24h` — original text | `037d57fa3fc3254b357b6d33fb0e5baf0361990e7284984948ec3189618ba614` |
| `chat:GK-003:2026-10-05:delivery-clarification` — original text | `2d74d41e99e304e1111de3199009c5225e80b0e5aa34fe0296c77726ec933c04` |
| `docs/creator/GK-003/source-comments.json` at refresh | `72aeedd3f42eebada3e0185580706658b027d9a282d6c85a87b37213c2621d4c` |
| `docs/creator/GK-003/decisions.json` at refresh | `59ea5cd42df17f799b71015403535a28911b7fd52fb54550004c3a560728558a` |

Current decision revisions read: payment_methods `ee6d206bcef6c234535608c8d564ed18206e16d0b78061b91410901144c55242`;
quote_validity `491d273160444ce26e2be95b20e9f0c7da07084d3d3287c7db22a26e58110939`;
refund_allocation `08842d29879ae5615c8301d34d852332edb2a7c41176d6ff268c027b01b42894`;
refund_execution `d15cebd8e1853abde4645e313f3a33e1dc763486bf5430ebdbf2f9212d21d6b2`.
GK-003 reconciliation remains in progress in its own task; these are exact read
revisions, not a claim of GK-003 completion or an immutable final manifest.

### Historical r1 source and method provenance

The following evidence and original author-time fingerprints are retained as
**historical r1 provenance**, not the current reconciliation or review result.
The complete earlier cases are preserved by the master in handover-history.json.

The r1 evidence was the user-supplied `Pasted text.txt`, read from the attachment
`61096423-2925-402b-8a0c-db5131648fac`, original UTF-8 SHA-256
`137833f269f89cac0a2e64ce6781b9cbdb23f8c605370c10818e61c99532e2ba`.
Its six answer keys are `refund_scope`, `days_months`, `count`, `slot_grid`,
`placement_clock`, `delivery_details`; they correspond to
`BO-GK-001.question.<key>`. Its exact bytes are preserved in
[the Ukrainian submission](submission-2026-10-04.uk.txt). The master subsequently
reported a matching live comment on the [main card](https://trello.com/c/eI8UpD6l):
action `6ac2d95174a456d0b2b9ba6a`, timestamp `2026-10-04T22:55:13.910Z`, author
`sashachicha13`, whole-comment SHA-256
`612835cca5d1566d859bf32a69f2d1ce50e660390ebadc775b991103f499ce69`.
That comment includes code fences, so its hash differs from the attachment;
answer lines are 3, 6, 9, 12, 15 and 18. This live observation comes from the
master, not a network read by this worker. Sasha's posted relay does not independently
verify direct chef authorship or approval of these examples. The master owns durable
intake and reconciliation.

Author-time input fingerprints below identify the revisions read **before** the
master's parallel intake update. They are historical baselines, not a claim that
these files remain unchanged after reconciliation.

| Input | SHA-256 |
| --- | --- |
| `docs/creator/README.md` | `dd117914b04e63bfa36c763925684587754a77ab6ac6015430e5a7f0f1e15cd1` |
| `docs/creator/GK-001/README.md` | `2b59b30cefa96ddd5565616fb61807ecf231e2522e4071d4bcb63171dd0d87e7` |
| `docs/creator/GK-001/source-comments.json` | `e33b2a7b27ad0431910a87369e7c60bc998a1527d857276a6bc8d6997f372ae4` |
| `docs/creator/GK-001/decisions.json` | `2be14abb9748759da009768b72d6f09d9fcc67d368e397a94ebf233c50b13b7d` |
| `docs/creator/GK-001/follow-up.json` | `d6caf317bfb5dfa9d82b3467166cb27e867555e4177f9bebe726a9af8ccfa549` |
| `docs/design/owner-data-contract.md` | `32cf60a5c58701042eaebf4d890bef50a5823f8b39051f12230b0d78df83e38c` |
| `docs/creator/GK-003/decisions.json` | `3888702d777eff36e4c5d02a0cf4d2f0a0641db634240467a6c4b0f36a97dbe7` |
| `docs/creator/GK-005/decisions.json` | `3d4bdf96e9824addf5d528ad95135582831d4651779fcb12483581b46db94942` |

Original BO-GK-001 comment: `6abab66bbf2b0e811db912b7`, content SHA-256
`cdb7805b1e1d6a3d1bff62a1ea5a615c776dcdb1b58a967707902ad971d67855`.
Original decision IDs below refer to the fingerprinted decision file, which stores
each decision's revision hash. New answer keys identify the subsequent evidence;
old conflicting values are not reactivated by citing their source IDs.

All dates, order identifiers, prices and destinations below are **derived test
fixtures**, unless explicitly described as a source decision. Arithmetic uses UTC
instants and integer seconds or GBP pence. Proposed observable behavior is labelled
where the source does not fully decide it. A case marked blocked remains a useful
review question, not a passing acceptance result.

## AC1 — payment clock, effective start and time boundaries

Source decisions: `BO-GK-001.production_start`, `ready_preparation`, `lead_time`,
`booking_horizon`, `wedding_recommendation`; new answers `placement_clock` and
`days_months`.

The new answer anchors 48 elapsed hours to **full successful payment**. Draft
creation, payment initiation and quote sending are not that event. Manual
confirmation starts work earlier; for ready cakes, confirmation and preparation
start are the same event. The automatic rule takes effect at its scheduled instant
even when processing is late. The following notation is derived technical wording:
`D = paid_at + 172800 seconds`; an earlier valid manual confirmation gives the
effective start `M`, otherwise `D`. This is not pickup time minus 48 hours. Current GK-003 payment/quote validity is
**24 hours from sending**, including chef-created website templates for Instagram
arrangements. It does not change this separate production/preparation +48-hour
clock. All current payments use the website; the earlier off-site/residual-balance
exception survives only as historical evidence.

| Case | Given / action | Expected result and remaining limit |
| --- | --- | --- |
| T01 — payment anchor | Draft created 5 Nov 2026 09:00 UTC; payment initiated 10:00; full payment succeeds 11:00. | `paid_at = 11:00`; automatic due instant is 7 Nov 11:00 UTC. Neither earlier event starts the 48-hour clock. Partial/failed/pending payment is not full successful payment. |
| T02 — early manual start | T01 order; chef confirms at 5 Nov 15:00 UTC, four hours after payment. | Effective start is 5 Nov 15:00 UTC. For a ready cake this same confirmation starts preparation; no second mandatory preparation-start button is inferred. Scheduled processing at 7 Nov 11:00 must not replace the earlier start. |
| T03 — just before / exactly due | Uncancelled, unrefused T01 order without manual confirmation, evaluated at 7 Nov 10:59:59 and 11:00:00 UTC. | At the first instant 172799 seconds have elapsed; at equality 172800 have elapsed and the automatic start is effective. It must not wait until a later whole second or the scheduler's arrival. |
| T04 — late processing | T01 scheduler does not process until 7 Nov 11:07 UTC. | Effective automatic start remains 11:00; processing is recorded at 11:07. The interval is 420 seconds. Do not report the late recording as the policy start or claim it proves the physical cake preparation began at 11:00. Physical/observed preparation evidence stays separate. |
| T05 — cancellation at start equality | Use T01 payment at 5 Nov 11:00 UTC and original handover 14 Nov 13:00 UTC. Customer cancellation and automatic start meet at D = 7 Nov 11:00; repeat with scheduler processing seven minutes late. | Start is effective at D in both runs. Original handover is 612000 seconds (170 hours) away: >120 hours, so customer refund target is 100% (£200 on the money fixture). Processing seven minutes later does not change that request’s effective evaluation or money outcome. Start alone never erases entitlement; GK-019 serializes state changes so an already cancelled order does not later start. Repeat with manual start exactly at the refund deadline: customer cancellation then yields exactly 50%, independent of manual-start ordering. |
| T06 — chef refuses after payment | Chef personally refuses/cancels the fully paid £200 fixture within the 48-hour window. | R2 specifies 100% for chef cancellation: refund target is £200, including delivery. The cancelled order must not later silently start. Record refund requested/pending/completed separately; refusal alone is not proof money returned. GK-019 owns the transition and provider workflow. |
| T07 — urgent handover | Customer tries the ordinary ordering path for handover less than 192 elapsed hours after full payment, including less than 48 hours. | R2 selects 192 hours as the standard minimum, so the ordinary booking is unavailable. A Special Request/personal date clarification with chef/admin remains communication requiring an individual decision; it is not automatic acceptance or a numeric override. Chef-arranged orders still use a website template/payment link; no current partly paid off-site exception is inferred. |

### Both UK clock changes

These two calculations were checked with Python's installed
`zoneinfo.ZoneInfo('Europe/London')`, adding `timedelta(hours=48)` **in UTC**.
The handover in each fixture is later than the start event: 9 April and 5 November
respectively. The automatic event is not a pickup appointment and does not inherit
the Sunday collection hours.

| Case | Full successful payment | Add exactly 172800 seconds | Customer/chef display |
| --- | --- | --- | --- |
| T08 — spring forward | Friday 27 March 2026 12:00 GMT = `2026-03-27T12:00:00Z` | `2026-03-29T12:00:00Z` | Sunday 29 March **13:00 BST** (`+01:00`). Using Sunday 12:00 local would give only 47 elapsed hours. |
| T09 — autumn back | Friday 23 October 2026 12:00 BST = `2026-10-23T11:00:00Z` | `2026-10-25T11:00:00Z` | Sunday 25 October **11:00 GMT** (`+00:00`). Using Sunday 12:00 local would give 49 elapsed hours. |

In the checked zone data, spring jumps from 29 March 00:59:59 GMT to 02:00:00 BST;
autumn changes from 25 October 01:59:59 BST to 01:00:00 GMT. The repeated autumn
01:00 hour is not an ambiguous stored deadline because the persisted UTC instant
and display offset identify it. These calculations demonstrate the submitted
elapsed-time rule. Independent review can verify the derived arithmetic; a fresh
human approval of every calculated timestamp is not an additional completion gate.

### Lead-time equality and personally clarified date boundaries

| Case | Fixture | Expected result and remaining limit |
| --- | --- | --- |
| T10 — selected minimum | Full payment at 6 Nov 2026 13:00 UTC; compare handover at 14 Nov 12:59:59, 13:00:00 and 13:00:01 UTC. | Elapsed intervals are 691199, 691200 and 691201 seconds. R2 chooses 192 hours (=691200 seconds): first fails the minimum; equality and one second later meet this condition, subject to the actual handover calendar and other admission rules. A 216- or 240-hour interval also meets this minimum; neither becomes a replacement minimum. These instants isolate arithmetic, not a new slot grid. Original standard lead wording covers any order; no separate automatic ready-stock exemption is supplied. |
| T11 — uncertain month endpoints | Ordinary request at a two-month boundary; wedding request seven versus eight months ahead, including a month-end date. | R2 instructs personal clarification with the chef, who has admin access. Route uncertain dates to that decision and preserve its agreed outcome; do not automatically confirm a disputed endpoint or invent 30-day months/an eight-month maximum. Existing ordinary two-month and wedding 7–8-month wording and approximate two-month wedding recommendation remain. Manual clarification is the supplied operating outcome; exact automatic month arithmetic remains downstream work. |
| T12 — production exceptions | Automatic due instant falls outside collection hours; administrator later configures a closed day. | Earlier answer supplies no separate production hours, holidays or closed days, while allowing later admin configuration. Do not invent recurring production clock hours or silently apply future exceptions to accepted orders. Date uncertainty goes to chef/admin; configuration effect/versioning needs deliberate technical reconciliation. |
| T13 — minimum across UK clock changes | Payment 21 March 2026 12:00 GMT; add 192 elapsed hours. Repeat from 17 October 2026 12:00 BST. | Spring result: 29 March 13:00 BST (`12:00Z`); autumn result: 25 October 11:00 GMT (`11:00Z`). These are earliest elapsed-time instants only: spring Sunday 13:00 lies outside Sunday 11:00–12:00 collection, so meeting minimum alone does not enable that handover. Select a genuinely offered later interval or personally clarify with chef. |

## AC2 — cake counts, oversubscription and pickup changes

Source decisions: `BO-GK-001.daily_limit`, `capacity_exceptions`, `pickup_hours`,
`pickup_slot`, `move_date`, `move_time`; new answers `count` and `slot_grid`.

| Case | Given / action | Expected result and remaining limit |
| --- | --- | --- |
| C01 — two cakes in one order | Five ordinary cakes already count toward Saturday's standard limit; customer buys one order containing two cakes for Saturday handover. | Consumption is `5 + 2 = 7` cakes, not six orders. Count against **handover day**, even if production is on Friday. No design/complexity multiplier. |
| C02 — chef's separate ten-cake order | Seven ordinary cakes are already booked for Saturday. A customer contacts the chef; chef agrees and enters a separately calculated ten-cake order in admin. | Standard counter stays at seven; the manually agreed order contributes ten cakes outside that counter. Operational handover workload is **17 cakes** and must remain visible. Excluded from standard counting does not mean excluded from the schedule, money records or fulfillment obligations. This example does not establish a ten-cake exception ceiling. |
| C03 — two payments for the last place | Standard counter is six; two customers simultaneously complete a one-cake order for the same handover day. | New answer explicitly allows **both** to place their orders. Result is eight ordinary cakes against the standard seven, with chef discretion to cancel an order later. Do not replace this with an invented one-winner admission rule or label one order a chef-entered exception without evidence. Duplicate processing must still not create duplicate orders. Exact race window/admission mechanics and customer wording need GK-019 reconciliation with older no-overbooking contracts. |
| C04 — chef cancels after C03 | Chef personally cancels one accepted, fully paid £200 order that cannot be fulfilled. | R2 expressly requires 100% refund: £200, including delivery, regardless of the customer 50% branch. Preserve the other accepted order. Choice, notification and refund execution need the GK-019 workflow; a refund request alone is not a completed refund. |
| C05 — reduce below accepted commitments | Seven ordinary cakes already accepted for Saturday; standard limit changes to five. | R2: all seven remain accepted, two above the new limit. Only the chef personally can cancel an accepted order; there is no automatic cancellation, redistribution or conversion to an unaccepted state. Show both the new five-cake standard and seven accepted commitments. If chef cancels an order, apply the 100% branch in C04. The answer does not define an automatic policy for later new bookings after the reduction. |
| C06 — multiple orders share a slot | Two ordinary orders choose the same available handover interval and standard daily admission otherwise permits them. | No separate per-slot order-count limit exists. Neither one order per slot nor zero/unlimited daily capacity follows from this. Actual operational ability still matters. |

### Saturday slot list

Source window: Saturday 12:30–15:00; each slot lasts 60 minutes. The new answer
explicitly lists `12:30–13:30`, `13:00–14:00`, `13:30–14:30`, then “etc”, and says
the slot's end must fit within the handover hours. This confirms overlap.

| Case | Candidate interval | Result |
| --- | --- | --- |
| S01 | 12:30–13:30 | Explicit source example; 60 minutes within the window. |
| S02 | 13:00–14:00 | Explicit source example; overlap is allowed. |
| S03 | 13:30–14:30 | Explicit source example; 60 minutes within the window. |
| S04 | 14:00–15:00 | **Derived candidate** from continuing the examples with a 30-minute start step. Ends exactly at closing. Include in the complete Saturday example list for independent technical review; the general 30-minute step is inferred, not separately stated policy. |
| S05 | 14:30–15:30 | Excluded: ends after 15:00. |
| S06 | 14:30–15:00 | Excluded: only 30 minutes; do not truncate the 60-minute duration. |

No separate short “last 30 minutes” slot is necessary in the proposed list.
Do not apply an inferred 30-minute start step to all weekdays as an approved
setting without recording that inference and obtaining the needed decision.

### Date moves and same-day time moves are different actions

| Case | Given / action | Expected result and remaining limit |
| --- | --- | --- |
| M01 — date change | Customer asks to move Saturday handover to Sunday. | Customer contacts the chef; only the chef changes the date after personal agreement if feasible. The site does not automatically move the date. Previous one-day/shelf-life wording remains a constraint to reconcile; “this is accounted for” does not define a new numeric override. |
| M02 — full next day / after Sunday | Target next day has no standard availability, or current handover is Sunday. | Contact chef for an individual decision. Do not silently skip to next Thursday, grant a date override, or declare a pickup slot full using an invented per-slot cap. Keep the existing appointment until a replacement is actually agreed. |
| M03 — repeated date move | Customer already had a date change and requests another. | No automatic repeated date move; a further change requires communication and agreement with the chef. The answer does not establish an absolute ban on all second moves. |
| M04 — six-hour equality | Saturday 14 November 2026 current slot 13:00–14:00 GMT; same-day 14:00–15:00 is available. Customer changes time at 07:00 GMT. | Six elapsed hours remain until the **current** slot starts; self-service is allowed by the existing `move_time` rule. Daily cake count is unchanged. The target is the derived S04 example, whose arithmetic and fit are independently reviewable. |
| M05 — six hours minus one second | Same setup; customer acts at 07:00:01 GMT. | 21599 seconds remain, less than 21600; self-service is unavailable. Contact chef for an exception. Do not compare against the later target slot to reopen self-service. |
| M06 — unavailable same-day target | Six-hour condition holds, but requested time is unavailable because of the actual schedule/operational feasibility. | Do not confirm a failed move or release the existing appointment. Offer contact with chef. No hidden per-slot numeric cap is introduced. |

## Refund interaction — original deadline, exact amounts and both cake types

The 2026-10-04 `refund_scope` answer introduced full order refund more than five
days before handover and a 50%/maximum-50% branch including **product and delivery**.
R2 and the user's two subsequent explicit replies resolve this to **100% more than
120 elapsed hours before original handover**, and **exactly 50% at equality or
later**, including no-show and time **after handover**. Chef cancellation is
**100%**. The same new policy applies to both **made-to-order and ready-stock**;
older contradictory 3/4-day, zero-after-start and separate-delivery rules are
retained only as history, not alternative active defaults.

At placement, save the original handover and its refund deadline
`F = original_handover_at − 432000 seconds`. For customer cancellation before F,
refund 100%; at or after F, refund exactly 50%. A date or same-day time move in
either direction never changes F. The right saved at placement is this original
rule/deadline, not a percentage determined once from the initial booking lead time.
Effective production/preparation start does not close the stated refund right.
The timestamp H below is the agreed original handover instant; using a slot-start
fixture is a technical example, not a new change to the offered slot grid.

Use a fully paid, undiscounted illustrative order: a **10000-pence (£100)** cake
amount and a separately quoted/paid **10000-pence (£100)** GK Delivery amount,
giving **20000 pence (£200)** across two collections. The cake amount was used in
`BO-GK-003.refund_example_delivery`; delivery uses the supplied £100 starting
amount solely as a hypothetical individual quote. This combination is not a real
quote or owner-approved fixture; no amount establishes a flat tariff. There are
no earlier refunds. Apply the same entitled percentage to the actually paid cake
(after any discount) and actually paid GK-chef delivery:

- At 50%: cake refund `10000 * 50 / 100 = 5000` pence plus delivery refund
  `10000 * 50 / 100 = 5000` pence; aggregate **10000 pence (£100)**.
- At 100%: cake refund 10000 pence plus delivery refund 10000 pence; aggregate
  **20000 pence (£200)**.

The chef enters GK delivery pricing/payment through admin and alone refunds that
separately paid collection via admin. The amounts in R01–R10 are **aggregate
entitlement targets**, not a direction to refund the whole total from each
collection or a claim that both refunds complete together. Refunding £100 from
each collection in the 50% branch would wrongly return £200; refunding £50 cake
plus all £100 delivery would wrongly return £150. Customer-arranged courier fees
are paid to that courier, are not GK collections and never enter these GK refund
totals. Preserve actual historical payment provenance without offering the old
Revolut/off-site payment choice as current behavior.

Use original `H = 2026-11-14T13:00:00Z` and save
`F = 2026-11-09T13:00:00Z` at placement. The user explicitly selected 120 elapsed
hours and preservation of this original deadline. Run every customer-refund case
for both cake types. All refund targets below assume no earlier refunds; pending
or succeeded refunds still count against the aggregate paid amount.

| Case | Fixture / calculation | Expected result |
| --- | --- | --- |
| R01 — just beyond 120 hours | Customer cancels `2026-11-09T12:59:59Z`; `H − cancel = 432001` seconds. | Cancellation is one second before F: 100% = 20000 pence (£200), including delivery. |
| R02 — exactly 120 hours | Customer cancels `2026-11-09T13:00:00Z`; difference 432000 seconds. | Cancellation is exactly at F: strict “more than” is false, so exactly 50% = 10000 pence (£100), including delivery. Repeat one second later: still £100. |
| R03 — after work starts / closer to handover | Customer cancels `2026-11-13T13:00:00Z`; original handover is 86400 seconds away; preparation already started. | After F: exactly £100. Preparation/start does not make this zero. Conversely, if preparation starts early but cancellation is before F, the target remains £200. |
| R04 — date moved later or earlier | At 10 Nov 10:00 UTC, original Saturday H is 356400 seconds away; agreed Sunday 15 Nov 11:00 is 435600 seconds away. Repeat moving an appointment earlier. | Save the new operational appointment but retain F = 9 Nov 13:00 UTC. Cancellation at 10 Nov 10:00 is after F and returns £100 even though the new date is >120 hours away. Moving earlier also never advances F: any cancellation still before original F remains £200. Actual move feasibility and chef agreement are fixture assumptions. |
| R05 — after handover / no-show | Repeat a customer request after original handover/customer delivery, after delivery was performed, and after a missed original handover. | All are after F and expressly covered: exactly £100 including delivery. Do not return zero due to these states or apply the historical component exclusions. If an exceptional earlier handover were personally agreed, use the preserved F rather than introducing a new state override. Legal/nonconformity handling remains separate GK-003/005 work. |
| R06 — same-day time move | Original Saturday handover 13:00 changes to 14:00. Customer cancels 9 Nov 13:30 UTC; repeat an earlier time move. | Original F remains 9 Nov 13:00 UTC: refund £100. Recomputing from 14:00 would wrongly give £200. Moving earlier likewise never changes F or downgrades a before-F cancellation. |
| R07 — cancellation confirmation | Customer starts cancellation before F, at F, or after F with no earlier refunds. | Display the exact amount and confirm the customer's intention before submission: £200 before F; £100 at/after F. Do not say “maximum 50%”. Viewing the explanation does not cancel the order or refund money. |
| R08 — chef cancellation | Chef personally cancels the paid £200 fixture, including capacity-related cancellation or refusal before automatic start. Repeat before/at/after F. | Always 100%: target £200 including delivery. Do not apply the customer's 50% branch. Pending/successful previous refunds count against total paid; no duplicate allowance or false completed-refund claim. |
| R09 — original deadline saved at placement | Standard fixture placed 6 Nov 13:00 UTC for H, 691200 seconds (192 hours) ahead, then customer cancels on 13 Nov 13:00. | Placement saves F. Although initial lead was >120 hours, it does not lock in 100% forever. Cancellation after F returns £100. |
| R10 — refund deadline across UK clock changes | Spring original handover 2 April 2026 13:00 BST (`12:00Z`); autumn original handover 29 October 2026 13:00 GMT (`13:00Z`). | Subtract exactly 432000 seconds in UTC. Spring F = 28 March 12:00 GMT; autumn F = 24 October 14:00 BST (`13:00Z`). At each F return 50%; one second before return 100%. Local-clock subtraction must not silently turn five days into 119 or 121 elapsed hours. |
| R11 — separate delivery refund still outstanding | After-F cancellation of the two-collection fixture; cake refund £50 succeeds, while chef/admin delivery refund £50 fails or its provider outcome is unknown. | Aggregate obligation remains £100. Record £50 completed and the delivery obligation still outstanding/unknown; do not mark the whole refund complete or repeat the succeeded cake refund. Chef resolves the separate delivery refund using actual provider evidence. No all-or-nothing provider guarantee or intentional splitting within either collection is inferred. |

Downstream implementation must define multi-item rounding and reconcile previous
pending/succeeded refunds across actual collections. Use actually paid amounts
after discount, not an unpaid nominal price or a second independent allowance.
Partial/pending payment is not a currently accepted paid order; handling real
historical evidence does not restore an off-site balance-payment workflow.
Aggregate no-over-refund and pending-outcome safeguards still govern. Exact legal
wording/review is separate GK-003/GK-005 work, not a new GK-001 closure gate.
The former five-day and cross-cake source gaps are resolved by the actual replies
quoted above; the initial alternatives are preserved in history only.

## AC3 — individually agreed delivery and handover examples

Source decisions: `BO-GK-001.fulfillment_modes`, `gk_delivery`,
`delivery_geography`, `customer_courier`, `pickup_location`, `address_release`;
new answer `delivery_details`.

Delivery windows now use the same recurring hours as collection: Thursday
11:00–17:00, Friday 10:00–17:00, Saturday 12:30–15:00, Sunday 11:00–12:00,
Europe/London. Chef sets the price individually through email, Instagram or
WhatsApp agreement. No area is an automatically approved zone. Source starting
price remains 10000 pence (£100); a quote is not a flat tariff.

All locations below are **fictional labels**, with no customer address or actual
route verification. A “confirmed” result is conditional on the test's explicit
chef agreement, not evidence that a real service was quoted or performed.

| Case | Given / action | Expected result and remaining limit |
| --- | --- | --- |
| D01 — London request | “Fixture L: London destination”; Saturday 13:00–14:00 requested. Assume chef agrees feasibility and a hypothetical 10000-pence quote at the supplied £100 starting amount by email. | Record this individual quote and agreed window; customer sees £100 delivery plus the example £100 cake amount, combined amount `10000 + 10000 = 20000` pence (£200), with separately priced/paid GK delivery entered by chef in admin. London label alone is not approval; this is a derived fixture, not a real agreed quote. Current quote/payment window is 24 hours; no new expiry is inferred from journey distance. |
| D02 — outside London, pending | “Fixture O: destination outside London”; Friday 14:00–15:00 requested, no chef decision yet. | Keep request pending separate feasibility/price agreement. Do not auto-approve, auto-reject solely because it is outside London, or calculate a zone price. |
| D03 — outside London, individually accepted | For Fixture O, assume chef expressly agrees feasibility, Friday 14:00–15:00, and a hypothetical 10000-pence quote at the supplied £100 starting amount. | Proposed quote records that individual offer. With the example 10000-pence cake, combined website-paid amount across cake and separate GK-delivery collections is `10000 + 10000 = 20000` pence (£200). The same illustrative minimum is used only to avoid inventing another tariff; it does not assert that any actual outside-London journey can be delivered for £100. |
| D04 — delivery infeasible | Chef declines Fixture O because the requested route/window cannot be fulfilled. | No delivery commitment or accepted paid delivery quote is created from the request alone. Customer can discuss another arrangement. No automatic geographic rejection rule is inferred. |
| D05 — outside handover hours | Customer requests Saturday 15:30–16:30. | This is outside the supplied Saturday hours; do not promise it through the standard offer. An explicit exception workflow would need agreement rather than an invented extension. |
| D06 — responsibility | Customer-arranged courier takes cake; contrast GK driver collecting cake for its own delivery. | Customer/courier assumes transport/condition responsibility at the former handover under the owner wording. GK retains responsibility until customer handover in GK Delivery. A driver leaving the kitchen is not the final customer handover. A customer-paid courier fee is outside GK collections and GK refunds; do not invent a third-party courier refund. |
| D07 — all orders paid through website | Compare customer website checkout with an Instagram arrangement entered by chef as a website template and sent to the customer for confirmation/payment. | Both current paths require website payment; the template alone is not a paid order or date reservation. Current payment window is 24 hours from sending; payment is disabled after expiry and cannot be repeated after successful payment. Exact server equality/recovery mechanics use GK-003’s recorded technical interpretation. The former off-site residual-balance/Revolut exception is historical only. Separately quoted GK delivery is entered via chef/admin and separately paid; it does not authorize an unpaid cake reservation or change production +48 hours. |
| D08 — public address and draft couriers | Public page and a demonstration of courier instructions. | Public pickup location stays **St Katharine Docks**. Private address is absent here. Requested fictional courier content may appear only as conspicuous demo placeholders, editable later; do not present invented services as real recommendations or operational instructions. Actual courier content and sender/channel/early-address-release mechanics are downstream content and GK-019 design work. |

The answer's phrase about dependence on distance does not supply a quote validity
duration. Do not reuse production's 48-hour deadline as a delivery-quote expiry.
Current GK-003 supplies the 24-hour payment/quote window; it replaces the earlier
48-hour quote-validity wording, while production remains +48 hours from payment.
Expiry mechanics, address-dispatch mechanics and final courier content are
downstream work, not extra GK-001 completion gates. The supplied individual-assessment rule and
these inside/outside-London fixtures cover geographic behavior without inventing
zones or requiring private customer addresses. They are not real route or price
verification; the master assesses them against the actual AC3 handover requirement.

## AC4 — developer checks and human acceptance

Developer checks prepare the exact policy and demonstrate outcomes; they do not
approve business choices. A subagent may simulate a customer or chef walkthrough,
report confusing copy and check arithmetic. It cannot act as Sasha, Elina or a real
customer or supply their approval. The user's explicit request to mark GK-001 Done
already authorizes closure once its actual criteria are met; do not ask for the same
final permission again. The master must attach the completed artifact/review revision
to that authorization and record the real approving actor. Subagent arithmetic and
walkthrough review do not impersonate human acceptance. Outstanding source-policy
choices still need actual answers, but derived calculations do not create another
mandatory round of human approval.

Completed in this authoring context: read the listed local inputs and new source;
checked the 48-hour, 192-hour and 120-hour refund DST calculations and transition displays with local
Python `datetime`/`zoneinfo`; asserted the refund-boundary/date-move seconds,
192-hour and six-hour equality, integer money/cake-count arithmetic, links and
document whitespace. These are author checks, not independent review. No application
tests, real payment/provider checks, real route checks, live Trello reads or owner
walkthrough occurred in this worker.
The master owns whole-packet document checks and separate review evidence.

| Criterion | Coverage provided | Remaining requirement / downstream boundary |
| --- | --- | --- |
| AC1 | T01–T13 and R01–R11: source-driven start, both DST transitions, equality, scheduler delay, 192-hour minimum, immutable 120-hour refund deadline, both cake types and personal date clarification. | Policy outcomes supplied and arithmetic checked. Independent review of the exact revision remains required. Technical workflow/rounding/chef-exception design belongs to GK-019; personal month/date clarification is supplied behavior, not an invented calendar default. |
| AC2 | C01–C06, S01–S06, M01–M06: count, manual exceptions, both-last-place outcome, full-target chef contact and 7→5 preservation. | All named outcomes are explicit; independently review fixtures and reconcile mechanics in GK-019. Accepted orders remain until individual chef cancellation; no additional policy-reduction approval is missing. |
| AC3 | D01–D08: quote totals, pending/declined/accepted outcomes, windows, responsibility and payment scope. | Assess the concrete inside/outside-London cases against supplied manual-quote policy. Final courier content, quote-expiry and address-dispatch mechanics remain downstream GK-003/GK-019 work, not invented GK-001 blockers; legal review remains GK-003/GK-005. |
| AC4 | Fingerprinted source baseline and explicit case status. | Record independent review of the reconciled revision and the user's existing completion authorization. Master may close when actual criteria are met; unanswered policy cannot be replaced by that authorization. |

This document is a reviewable handover component. The named GK-001 acceptance
outcomes are now supplied; downstream implementation details remain explicit.
The master records independent review and the authorized handover/status action;
this worker does not claim that publication or acceptance occurred.
