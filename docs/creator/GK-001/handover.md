# Creator handoff — GK-001 — manual-handover-r2

Date: 2026-10-05 Europe/London.
Status: **Policy decisions complete; final reviewed revision for authorized delivery.**
Actual review, Trello publication and Done transition are recorded in [handover.json](handover.json).
The user explicitly authorized GK-001 Done, supplied the three missing decision
groups, resolved two precise refund readings, and instructed execution of the manual
handover. No additional approval is required for that scoped transition after checks.
This is a decision/document handover, not application delivery or legal approval.

## Exact inputs and attribution

- Original chef: [Elina's BO-GK-001 answer](https://trello.com/c/rVHbBffB#comment-6abab66bbf2b0e811db912b7), action `6abab66bbf2b0e811db912b7`, 2026-09-28.
- [Sasha's main-card answer](https://trello.com/c/eI8UpD6l#comment-6ac2d95174a456d0b2b9ba6a), action `6ac2d95174a456d0b2b9ba6a`, 2026-10-04T22:55:13.910Z; [exact pasted Ukrainian](submission-2026-10-04.uk.txt).
- [Latest Ukrainian reply](submission-2026-10-05.uk.txt), local source `chat:GK-001:2026-10-05:clarification-1`; exact wording/typos/transport markers preserved. Source send timestamp is unavailable; ingestion time is separately recorded.
- Explicit subsequent user replies, local source IDs `chat:GK-001:2026-10-05:refund-deadline` and `chat:GK-001:2026-10-05:refund-scope`, verbatim in [source-comments.json](source-comments.json). These are chat references, not invented Trello action IDs/permalinks.
- Sasha is the developer/user supplying these decisions, not Elina. The current instructions authorize reconciliation and closure; no direct chef authorship or blanket approval of other tickets is fabricated.

The main/owner GK-001, GK-003 and GK-005 cards, comments and attachment lists were
refreshed using authenticated CLI. Original chef fingerprints are unchanged; no new
Trello substantive replies were present at that read. CLI has no pagination controls:
only exposed current comments are claimed, not deleted/edit history. All six attachment
lists were empty. Exact current sources, authors, dates and hashes are in the source
store and manifest. The private address stays protected and is not needed for this review.

## Final result assets and loading path

- [decisions.json](decisions.json): current typed business values, stable IDs, conditions, provenance and retained prior revisions.
- [source-comments.json](source-comments.json): original Ukrainian/English evidence, source kind, attribution, timestamps, hashes, source coverage and every numeric occurrence.
- [acceptance-cases.md](acceptance-cases.md): 50 source-linked walkthroughs, including exact equality, both DST changes, capacity and delivery outcomes; derived fixtures are labelled.
- [owner-data-contract.md](../../design/owner-data-contract.md): typed relational SQL/admin proposals, versioning and effects on existing orders.
- [handover-history.json](handover-history.json): exact r1 handover/cases/manifest and its published partial-handover receipt; historical, never current status.
- [handover.json](handover.json): r2 input/output fingerprints, source read, review/checks, actual publication and status-transition evidence.

Future workers load Creator README → this packet → current source/decision JSON →
owner data contract → shared contracts/feature documents. AGENTS and the execution
check require that path. Record the exact decision/source/file revision used. Do not
revive old defaults or interpret a local/unpushed path as available in another checkout.

## Current policy readback

| Stable decision ID | English interpretation | Status / downstream semantics |
| --- | --- | --- |
| `BO-GK-001.work_week` | Standard production/work and collection days are Thursday through Sunday. Later schedule_admin clarification supplies no separate current production hours/holidays and requests administrator configuration. | confirmed; Supplied meaning captured. |
| `BO-GK-001.daily_limit` | Standard seven cakes per handover day remains. The limit may be lowered from seven to five with seven cakes accepted: all accepted cakes remain accepted; only the chef personally may cancel their orders. Five is the supplied change example, not a replacement initial limit. | confirmed; Supplied meaning captured. |
| `BO-GK-001.capacity_exceptions` | Customers contact the chef for individually assessed extra orders; the chef enters separately priced orders through admin outside the standard count. Ten cakes for one day is the supplied example, not a new limit. | confirmed; No upper bound supplied; do not invent one. |
| `BO-GK-001.lead_time` | Minimum lead time is 192 elapsed hours (exactly 8×24 hours); the former range no longer selects the minimum. Date uncertainties are clarified personally with the chef through their admin access. No separate shorter ready-stock minimum is supplied; do not invent one. | confirmed; Supplied meaning captured. |
| `BO-GK-001.wedding_recommendation` | Approximately two months or earlier for wedding/tiered orders remains a recommendation; do not turn it into a mandatory minimum or arbitrary hours. Original approximate/advisory qualifier is retained. | confirmed; Calendar-month arithmetic remains undefined. |
| `BO-GK-001.urgent_requests` | The new answer says an order with handover earlier than 48 hours cannot be placed because minimum lead time applies. Earlier general urgent-request wording must not authorize that online booking; retain human enquiry as communication, not guaranteed acceptance. | confirmed; No blanket exception to the 192-hour ordinary minimum is supplied for chef-entered offline orders; seek explicit chef agreement for uncertain dates. |
| `BO-GK-001.booking_horizon` | Retain ordinary two-month and wedding 7–8-month horizons as supplied ranges. Uncertain dates/endpoints are resolved personally with the chef, who has admin access. This is an explicit manual resolution workflow; no automatic month-end arithmetic or default wedding maximum is invented. | confirmed; No automatic month conversion or endpoint rule supplied; require a chef-confirmed concrete date for uncertain cases. |
| `BO-GK-001.production_start` | The 48-hour clock starts at successful full payment. Earlier manual confirmation starts work; automatic start takes effect at its scheduled deadline even when processing is late. Retain actual processing time separately. | confirmed; Supplied meaning captured. |
| `BO-GK-001.ready_preparation` | For ready cakes, confirmation and preparation start are the same event. The automatic 48-hour clock uses successful full payment and scheduled-time effect; keep original actual-start evidence requirement. | confirmed; Supplied meaning captured. |
| `BO-GK-001.ready_cancellation` | Both made-to-order and ready-made cakes use the new whole-order policy, including delivery. At placement save the original handover deadline minus 120 elapsed hours: customer cancellation strictly before that deadline returns 100%; at equality or later exactly 50%, including no-show and after handover. Date/time moves never change it. Chef cancellation returns 100%. This expressly replaces older 3/4-day, component and incompatible state rules; legal verification remains separate. | confirmed; Supplied meaning captured. |
| `BO-GK-001.pickup_location` | Publish only St Katharine Docks. Exact full address is supplied but intentionally excluded from repository evidence; authorized staff must retrieve it from the private source. | confirmed; Supplied meaning captured. |
| `BO-GK-001.address_release` | Send full address and detailed instructions approximately 2 days (48 elapsed hours under the later exact-hour clarification) before collection, earlier if necessary. Approximation remains; no exact automated dispatch time was specified. | confirmed; Dispatch channel/actor and authorized early-send workflow. |
| `BO-GK-001.pickup_hours` | Recurring collection windows: Thursday 11:00–17:00; Friday 10:00–17:00; Saturday 12:30–15:00; Sunday 11:00–12:00. Later clarification allows overlapping 60-minute intervals wholly within these windows; Europe/London comes from existing project context. | confirmed; Whether the illustrated Saturday 30-minute start step applies to other weekdays. |
| `BO-GK-001.pickup_slot` | Retain 60-minute slots and no separate slot order cap; overlapping Saturday intervals are explicitly supplied and their ends must lie within opening hours. A 30-minute start step and final 14:00–15:00 slot are exact continuation of the shown pattern, labelled derived rather than an additional quoted interval. | confirmed; Whether the illustrated 30-minute step applies to all weekdays. |
| `BO-GK-001.move_date` | Date changes require direct agreement with the chef, who changes the date when still feasible. Repeats, a full next day and Sunday cases require chef communication. The answer does not approve a longer shelf life; retain the original one-day condition as unresolved for exceptional moves. | confirmed; Exact permitted shelf-life exceptions under chef discretion; no automatic skip to a later day. |
| `BO-GK-001.move_time` | Customer can self-reschedule to an available slot on the same day no later than 6 hours before the current slot starts; less than 6 hours disables self-service; contact directly for exceptions/no suitable slot. | confirmed; Supplied meaning captured. |
| `BO-GK-001.refund_follows_move` | At placement save the refund deadline from the original handover instant minus 120 elapsed hours. Customer refund is 100% strictly before that deadline, exactly 50% at or after it; moving date or time in either direction never recalculates or restores entitlement. | confirmed; Supplied meaning captured. |
| `BO-GK-001.fulfillment_modes` | Three fulfillment modes; Collection is free. | confirmed; Supplied meaning captured. |
| `BO-GK-001.customer_courier` | Customer arranges/pays their courier and takes transport responsibility at handover; any outstanding historical balance must be settled. Current orders, including chef-created Instagram templates, use website payment under the later explicit GK-003 clarification. The older off-site residual-balance exception is historical, not current intake. | confirmed; Supplied meaning captured. |
| `BO-GK-001.gk_delivery` | Retain original from £100 price, individually agreed delivery and responsibility until customer handover. Delivery windows match pickup hours; chef sets the quote through email, Instagram or WhatsApp. The distance phrase supplies no numeric tariff. Current website/template payment window is supplied separately by BO-GK-003.quote_validity (24 hours); it is not the production clock. | confirmed; Supplied meaning captured. |
| `BO-GK-001.delivery_geography` | No fixed delivery zones or automated zone tariffs. Assess each address, distance and travel time. Distant/outside-London addresses require Special Request with full address and separate feasibility/price confirmation; no automatic approval. | confirmed; Supplied meaning captured. |
| `BO-GK-001.schedule_admin` | No separate production hours or holiday/closed-day exceptions currently; an administrator must be able to configure these in the admin panel. Existing Thu–Sun working/collection week remains. | confirmed; Activation/versioning and permissions within the administrator role need technical design. |
| `BO-GK-001.capacity_concurrency` | Both customers competing for the last place may order; lowering the limit never automatically invalidates accepted orders. Capacity-driven cancellation requires the chef personally and the supplied 100% refund. This does not withdraw the separately stated customer cancellation/refund rights. | confirmed; Supplied meaning captured. |
| `BO-GK-001.courier_content` | User requests fictional placeholder courier/instruction content for preparing the site, editable later by the commissioning customer. No real courier recommendation or permission for end-customers to edit policy is established. Keep placeholder/demo content visibly distinct from approved operational instructions. | confirmed; Who may edit this content in the admin UI; real operational content before public use. |

## Український підсумок

Для замовних і готових тортів застосовуємо нове правило до повної вартості
замовлення, включно з доставкою. Під час оформлення зберігаємо строк: початковий
час видачі мінус 120 годин. Якщо клієнт скасовує раніше цього строку — повертаємо
100%; рівно в цей момент або пізніше — рівно 50%, включно з неявкою та зверненням
після видачі. Перенесення дати чи часу в будь-який бік не змінює збережений строк.
Коли скасовує шеф — повертаємо 100%. Попередні правила 3/4 днів та окремого
повернення доставки для цієї політики залишаються лише історією джерел.

Мінімум від оформлення/успішної повної оплати до видачі — 192 точні години.
Неточності дат, зокрема межі місяців, узгоджуються особисто з шефом, який має
доступ до адмінпанелі; система не вигадує автоматичні межі чи винятки.
Весільна рекомендація приблизно за два місяці або раніше залишається рекомендацією.

Стандартний ліміт — 7 тортів на день видачі. Якщо вже прийнято 7, а ліміт змінено
на 5, усі 7 залишаються прийнятими. Автоматично їх не скасовуємо; лише шеф може
особисто скасувати конкретне замовлення з поверненням 100%. Два торти в одному
замовленні займають два місця. Погоджені додаткові замовлення шеф вводить окремо;
приклад 10 тортів не є новою максимальною межею.

Зберігаються попередні підтверджені правила: 48 годин від повної успішної оплати
до автоматичного початку з можливістю раннього ручного підтвердження; дія від
запланованого моменту навіть при затримці обробки; години видачі та інтервали
60 хвилин; особисте погодження зміни дати; самостійна зміна часу того самого дня
не пізніше ніж за 6 годин. Доставка GK індивідуально погоджується від £100 у години
видачі. Публічно вказуємо лише St Katharine Docks. Точна адреса залишається приватною.

## Acceptance coverage

| Original criterion | Result/evidence |
| --- | --- |
| AC1 — approved start/cutoff/equality and both DST transitions | Full payment +48 elapsed hours, earlier manual start, scheduled effect on late processing. 192-hour booking minimum. Both cake types use original handover−120h deadline; >120h100%, equality/later50%, chef100%. T01–T12 and R01–R11 exercise independent clocks, exact equality, spring/autumn and moves. The old pickup-minus-X proposal is replaced, not assigned a guessed X. |
| AC2 — simultaneous last place, full target move, reduction | Both last-place customers may order; full/unavailable target requires chef agreement and retains old appointment until agreement; 7→5 preserves all 7 until personal chef cancellation. C01–C06, M01–M06 and S01–S06. |
| AC3 — fees, eligibility, handover windows, London boundaries | Free collection, GK quote from £100, no automatic zones; London/outside/distant addresses manually assessed; quote window matches pickup hours. D01–D08 provide labelled fictional addresses/quotes and responsibility/payment distinctions without inventing geographic thresholds. |
| AC4 — exact revision, appropriate acceptance/review, Trello | User answers and prior explicit Done instruction establish scoped business acceptance. Fresh independent review and document checks apply to the fingerprinted r2 outputs; exact posting/Done readback are in handover.json. No application/PR/CI evidence is fabricated for this Decision ticket. |

## Still undecided / downstream handoff

Current overlapping payment rules are in [GK-003 handover](../GK-003/handover.md):
website/template payments within 24 hours; separate GK-delivery payment and chef-admin
refund. These replace the earlier off-site residual-balance exception for new intake;
retain real historic money evidence.

No unanswered GK-001 completion question remains. The user supplied a manual
chef/admin resolution path for uncertain dates, not numeric month defaults.
Other-day slot-start steps, detailed urgent/off-site payment handling, operational
courier copy, address-dispatch mechanics, staff delegation, money rounding/provider
execution and legal/privacy review remain in their owning design/policy tickets.
GK-003 is being reconciled separately; its original evidence is not rewritten here.
No unrelated ticket is Done or Ready merely because this policy handover completes.

Proposed affected-ticket updates, separate from published snapshots:

- GK-019: consume this exact revision; reconcile state machine, admission/holds,
  refund accounting/payment sources and typed policy snapshots with all other prerequisites.
- GK-017/018: show exact cancellation amount, saved deadline, post-handover/no-show
  path, personal chef cancellation and over-capacity preservation; no misleading maximum50%.
- GK-023/025/028: count cakes by handover date, preserve concurrent accepted orders,
  192-hour minimum and personal date agreement; keep finite ready stock distinct.
- GK-026/030/033: original-deadline100/50, chef100, both types; prior succeeded/pending
  refunds prevent duplicate payout. Keep money recovery evidence and history.
- GK-027/031: chef-entered extras/manual orders, personal date agreement, immutable
  original refund deadline and accepted-order preservation across edits.
- GK-003/005: historical 3/4-day/component rules must not reactivate; complete their
  separate accounting, legal and privacy requirements. No Terms publication here.

## Manual handover steps for the next worker — executed r2

1. Loaded original/current sources, decisions, cases, SQL/admin mapping and manifest;
   preserved dirty working tree and previous reviewed r1 packet. Protected address
   was not copied or required for public examples.
2. Refreshed main/owner cards/comments/attachments; ingested actual chat answers with
   exact wording, source attribution and hashes. Source history and prior revisions remain.
3. Resolved all three groups and the two precise refund readings; updated cases,
   current contracts/mappings and cross-policy notices. Actual checks/reviewer findings
   are recorded in the manifest, not inferred from this checklist.
4. Publish this exact reviewed revision on main GK-001 and use the existing Done
   authorization after checks. Record actual comment and Done-list readback in the
   manifest. Do not archive or change owner/other-card status. A planned action is
   not evidence of execution; the manifest distinguishes them.
5. Hand the policy to GK-019 and named consumers; no dependent readiness changes.
   Other prerequisites must be checked by the receiving worker before execution.

Replaces only explicitly clarified GK-001 portions and the expressly replaced old
refund rules for both cake types. Prior source evidence, r1 publication and historical
ticket bodies remain intact. No runtime seeds, migrations, application code,
commit/push, service registration, release, deployment or public Terms changes.
