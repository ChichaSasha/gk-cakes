# GK-001 owner evidence — intake 2026-10-04

Q2 now accepted in GK-019:24h initiation; unknown payment waits without replacement; late paid needs chef feasibility and explicit customer agreement, otherwise full return. See current decisions/source and GK-019 q2-acceptance.json.

Current canonical reconciliation: **gk019-owner-r4**, from GK-019. Read current
`decisions.json` and `source-comments.json` plus the
[shared contracts](../../design/contracts.md) and
[revision manifest](../../design/gk019/output-manifest.json). Original intake/handover
review receipts below identify historical revisions; they do not review this integration.
Exact prior JSON bytes are retained in `docs/design/gk019/prior/` and each old decision
remains in previous_decision_revisions. Source statements and old publication hashes
are preserved. GK-019 Q1 is now accepted: chef-approved shorter lead for a specific ready-made/resale cake, ordinary192h and original resale handover retained. Exact source/decision revision in the current JSON and GK-019 q1-acceptance.json. Q3–Q5 remain separate.


## Current manual handover — 2026-10-05, r2

Read [the current handover](handover.md), [50 worked cases](acceptance-cases.md),
[current decisions](decisions.json), [original sources](source-comments.json) and
[revision/review/publication receipt](handover.json) first. They combine original
chef evidence, Sasha's main-card answers, [latest Ukrainian reply](submission-2026-10-05.uk.txt)
and two explicit refund clarifications. Chat source IDs are local references, not
Trello actions; source send timestamps are unknown where not exposed.

The three completion groups are resolved: original immutable120-hour refund deadline,
100%/exact50% for both cake types and chef100%; 192-hour minimum with chef handling
of date uncertainties; lowering7→5 preserves7accepted. Prior policies and the posted
partial handover are retained in [handover-history.json](handover-history.json).
Use the receipt for actual live status/publication evidence. Older initial-read tables
below remain source history and must not override current decisions.

## Later main-ticket follow-up

See the [2026-10-04 acceptance check and questions](follow-up.md) and
[read/review/publication receipt](follow-up.json). The [main-ticket comment](https://trello.com/c/eI8UpD6l#comment-6ac2cafe5ceafaf69b2795c1) is posted and verified.
The follow-up is agent-authored, not owner approval. Read it alongside the original evidence and decisions; ingest
answers by source identity/revision before resolving a question. The original intake
sections below describe the earlier read-only ingestion. Live status remains on Trello.


Read with [structured decisions](decisions.json), [source comments and provenance](source-comments.json), and the [owner data contract](../../design/owner-data-contract.md).

Owner card [BO-GK-001](https://trello.com/c/rVHbBffB/2-bo-gk-001o010-%D0%BF%D0%BE%D0%B3%D0%BE%D0%B4%D0%B8%D1%82%D0%B8-%D0%BF%D1%80%D0%B0%D0%B2%D0%B8%D0%BB%D0%B0-%D0%B2%D0%B8%D0%B3%D0%BE%D1%82%D0%BE%D0%B2%D0%BB%D0%B5%D0%BD%D0%BD%D1%8F-%D0%B2%D0%B8%D0%B4%D0%B0%D1%87%D1%96-%D1%82%D0%B0-%D0%B4%D0%BE%D1%81%D1%82%D0%B0%D0%B2%D0%BA%D0%B8) (`6ab8580af5fcdd5860d21234`), board `6ab846cadeac31eff3e42507`. Parent [GK-001](https://trello.com/c/eI8UpD6l/16-gk-001p0o010w00-set-production-pickup-and-london-delivery-policy) (`6ab13b4fa34d3a2205f61353`). Identities matched the role mapping and live reads.

Author reported by Trello: **Elina Hryhorian**, `@elinahryhorian`, member `6aba9a6ee5e52c67f1fbb09b`. [Original comment](https://trello.com/c/rVHbBffB#comment-6abab66bbf2b0e811db912b7) / action `6abab66bbf2b0e811db912b7`, source timestamp `2026-09-28T18:48:11.509Z`, retrieval `2026-10-04T20:59:12.600056+00:00`. Content SHA-256 `cdb7805b1e1d6a3d1bff62a1ea5a615c776dcdb1b58a967707902ad971d67855` (original UTF-8). Attribution comes from the author’s business answer and explicit owner self-attribution in BO-GK-005; membership/list placement does not establish approval. Sasha is the developer.

One current comment returned per owner card; no comments on corresponding parents; all six attachment lists empty. CLI has no pagination controls. Authenticated browser showed the same comment and original creation event without an older-comment control; observed comment permalinks are saved. Current exposed content is covered; deleted comments and prior edited revisions are not exposed. Preserve newly observed revisions by `(comment ID, content fingerprint)` on rerun. Descriptions are instruction/example snapshots, not owner decisions.

**Protected exclusion:** source line 63 contains the exact private pickup address. Its original remains on the private Trello card; the repository preserves a safe reference, full-comment fingerprint and redaction record. No address is copied to SQL fixtures or public content. Protected temporary retrieval files are not a durable dependency.

## Український readback — чернетка для Саші, не надіслано

Зафіксовано роботу й видачу з четверга до неділі, стандартний ліміт до 7 тортів на день без автоматичної ваги складності, ручні винятки, 8–10 днів та окремі горизонти бронювання. Зберігаємо 48 годин після оформлення, години видачі, 60 хвилин, межу 6 годин і перенесення повернення на нову дату/час. GK Delivery — індивідуальний запит від £100, без зон. Правила 3/4 днів і значення «оформлення» ще потребують уточнення. Повну адресу тут не публікуємо.

## English interpretation and decision coverage

`confirmed` means the cited explicit business statement; it does not imply blanket acceptance, resolved semantics or legal compliance. Each entry below resolves to `/decisions/<key>` in decisions.json. Original wording controls.

| Decision key | Status | Source lines | Faithful interpretation |
| --- | --- | --- | --- |
| `work_week` | confirmed | 9–14 | Standard production/work and collection days are Thursday through Sunday. Production clock hours and holiday exceptions were not supplied. |
| `daily_limit` | confirmed | 18–20 | Up to 7 cakes per day, same limit for all orders; design/complexity does not automatically change slot consumption. BO-GK-003 says 7 orders: multi-cake orders require clarification. |
| `capacity_exceptions` | confirmed | 21–21 | These orders may exceed the standard daily limit only after manual confirmation. |
| `lead_time` | confirmed | 25–25 | Standard lead time for any order is 8–10 days before collection. This is a range, not an approved minimum of 8 or 10. |
| `wedding_recommendation` | confirmed | 26–26 | Wedding and tiered cakes: recommended booking approximately 2 months ahead or earlier. A recommendation, not a hard minimum. |
| `urgent_requests` | confirmed | 27–30 | Earlier than 8–10 days, even very urgent or dates absent from the standard calendar: allow a Special Request, individually assessed for load and feasibility; manual acceptance is possible. |
| `booking_horizon` | confirmed | 31–31 | Ordinary maximum advance booking is the next two months; wedding orders can be placed 7–8 months ahead. Do not collapse the wedding range to 8. |
| `production_start` | confirmed | 40–43 | Manual acceptance or refusal is possible within 48 hours after order placement; earlier manual confirmation starts work then. Without manual action, enter «в роботі» after 48 hours. |
| `ready_preparation` | confirmed | 47–49 | Ready cakes have their own preparation start: manual any time after confirmation, otherwise 48 hours after placement; record actual date/time. |
| `ready_cancellation` | conflict | 53–56 | Ready cake: more than 3 days before collection returns 50% paid, retaining a nonrefundable 50% deposit; within the last 3 days no refund, also when paid in full. Conflicts in scope/basis with BO-GK-003 and BO-GK-005. |
| `pickup_location` | confirmed | 62–63 | Publish only St Katharine Docks. Exact full address is supplied but intentionally excluded from repository evidence; authorized staff must retrieve it from the private source. |
| `address_release` | confirmed | 64–64 | Send full address and detailed instructions approximately 2 days before collection, earlier if necessary; no exact automated dispatch time was specified. |
| `pickup_hours` | confirmed | 68–71 | Recurring collection windows: Thursday 11:00–17:00; Friday 10:00–17:00; Saturday 12:30–15:00; Sunday 11:00–12:00. Europe/London comes from existing project context, not this comment. |
| `pickup_slot` | confirmed | 73–75 | One slot lasts 60 minutes. No separate order-count limit per time slot; daily limit and actual ability govern admission. None is not zero capacity or unlimited daily production. |
| `move_date` | confirmed | 79–85 | Move only to the next available day, no more than one day because of shelf life, with space on that next day. Contact directly for an individual decision if unavailable. |
| `move_time` | confirmed | 89–93 | Customer can self-reschedule to an available slot on the same day no later than 6 hours before the current slot starts; less than 6 hours disables self-service; contact directly for exceptions/no suitable slot. |
| `refund_follows_move` | confirmed | 94–94 | After rescheduling, the refund rule remains tied to the new collection date/time. This does not say production start is reset. |
| `fulfillment_modes` | confirmed | 98–102, 104–104, 112–112 | Three fulfillment modes; Collection is free. |
| `customer_courier` | confirmed | 106–110 | Customer arranges/pays courier; recommendations and booking instructions supplied separately. Full address/handover instructions given separately before delivery. Transport/condition responsibility transfers at courier handover. Any balance must be paid before handover. |
| `gk_delivery` | confirmed | 114–118 | GK Cake London or its driver delivers; price starts from £100 and depends on address, individually quoted. Website needs separate GK Delivery option collecting address and requesting a quote; GK is responsible until customer handover. |
| `delivery_geography` | confirmed | 122–128 | No fixed delivery zones or automated zone tariffs. Assess each address, distance and travel time. Distant/outside-London addresses require Special Request with full address and separate feasibility/price confirmation; no automatic approval. |

## Acceptance coverage and downstream impact

- AC1 partial: work/start/preparation, time values supplied; placement anchor, ready-cake conflict, equality/DST walkthrough approval and precise lead/horizon semantics missing. Old X-before-pickup criterion needs proposed revision, not an invented X.
- AC2 partial: count limit/manual exceptions/no slot cap stated; multi-cake counting, concurrent last-place, no-cap pickup and reduction-below-commitments outcomes still require technical reconciliation and owner walkthrough.
- AC3 partial: manual delivery geography/starting price/handover responsibility supplied; automatic zones rejected, windows and concrete London/outside address examples absent.
- AC4 pending: evidence now captured locally; no technical tests, complete parent acceptance, Trello update or completion.

Every decision maps to a proposed typed SQL/admin consequence in [owner-data-contract.md](../../design/owner-data-contract.md). These assets are planning inputs, not migrations, seeds or permission to change settings. Record source/decision SHA-256 and contract revision in each downstream packet; use exact confirmed values in fixtures and keep conflicts blocked. Proposed ticket updates are in that contract; published snapshots are unchanged.

## Уточнення для власниці — не надіслано

### BO-GK-001.question.refund_scope

У BO-GK-001 для готових тортів вказано 3 дні, а в BO-GK-003/005 — 4 дні. Це окреме правило для готових тортів чи його треба замінити? Яке правило діє після початку підготовки, чи повертаємо невиконану доставку окремо та чи може перенесення знову відкрити право на повернення?

Source: [comment](https://trello.com/c/rVHbBffB#comment-6abab66bbf2b0e811db912b7), `BO-GK-001.ready_cancellation`, `BO-GK-001.ready_preparation`, `BO-GK-001.refund_follows_move`. Why it matters: Controls eligibility, allocation and policy version; preserve both until explicitly resolved.

### BO-GK-001.question.days_months

«8–10 днів», «3/4 дні» та «приблизно 2 дні» — календарні чи робочі дні, або точна кількість годин? Яка найраніша дата у правилі 8–10? Як рахувати наступні два місяці та 7–8 місяців для весілля, включно з граничною датою?

Source: [comment](https://trello.com/c/rVHbBffB#comment-6abab66bbf2b0e811db912b7), `BO-GK-001.lead_time`, `BO-GK-001.wedding_recommendation`, `BO-GK-001.booking_horizon`, `BO-GK-001.address_release`. Why it matters: Defines calendar validation without silently converting days/months to hours.

### BO-GK-001.question.count

Ліміт 7 — це кількість тортів чи замовлень? Якщо в одному замовленні два торти, скільки місць воно займає? До якого дня належить ліміт, якщо виготовлення й видача у різні дні? Хто підтверджує додаткові місця та як змінюємо ліміт із уже прийнятими замовленнями?

Source: [comment](https://trello.com/c/rVHbBffB#comment-6abab66bbf2b0e811db912b7), `BO-GK-001.daily_limit`, `BO-GK-001.capacity_exceptions`, `BO-GK-001.pickup_slot`. Why it matters: Needed for relational capacity and audited exceptions; no weighted default.

### BO-GK-001.question.slot_grid

У суботу видача 12:30–15:00, слот — 60 хвилин. Які саме інтервали показувати: що робимо з останніми 30 хвилинами? Чи можна змінювати дату самостійно, кілька разів, або пропускати зайнятий наступний день? Що робити в неділю?

Source: [comment](https://trello.com/c/rVHbBffB#comment-6abab66bbf2b0e811db912b7), `BO-GK-001.pickup_hours`, `BO-GK-001.pickup_slot`, `BO-GK-001.move_date`, `BO-GK-001.move_time`. Why it matters: Slot construction and rescheduling must not silently invent an interval or extend shelf life.

### BO-GK-001.question.placement_clock

Від якої події рахуються 48 годин після «оформлення»: успішної повної оплати чи іншої події? Для готового торта підтвердження і початок підготовки — різні дії? Що робимо з терміновою видачею раніше 48 годин, відмовою та одночасним скасуванням у момент автоматичного початку?

Source: [comment](https://trello.com/c/rVHbBffB#comment-6abab66bbf2b0e811db912b7), `BO-GK-001.production_start`, `BO-GK-001.ready_preparation`. Why it matters: Maps order/payment/preparation states and equality races. Developer must prepare both London DST examples.

### BO-GK-001.question.delivery_details

Які часові вікна погоджуємо для GK Delivery, як довго діє її ціна та які інструкції/рекомендовані кур’єри надсилаються окремо? Хто й яким каналом надсилає адресу раніше приблизно двох днів за потреби? Чи згадка залишку до оплати означає виняток із 100% оплати до бронювання?

Source: [comment](https://trello.com/c/rVHbBffB#comment-6abab66bbf2b0e811db912b7), `BO-GK-001.gk_delivery`, `BO-GK-001.delivery_geography`, `BO-GK-001.customer_courier`, `BO-GK-001.address_release`. Why it matters: Defines quoted delivery commitments, address release and payment exceptions.

## Original Ukrainian evidence

Line numbers below are stable within the fingerprinted revision and match JSON references. Markdown, typos, qualifiers and English text embedded by the owner are retained. Only the disclosed private-address exclusion is redacted.

```text
001 # BO-GK-001
002
003 ## Правила виготовлення, видачі та доставки
004
005 ### 1. Виготовлення
006
007 #### Робочі дні
008
009 Стандартні дні роботи та видачі замовлень:
010
011 - четвер
012 - п’ятниця
013 - субота
014 - неділя
015
016 #### Денний ліміт
017
018 - Стандартний ліміт — **до 7 тортів на день**.
019 - Ліміт однаковий для всіх замовлень.
020 - Складність або дизайн торта не змінюють кількість доступних слотів автоматично.
021 - Великі, весільні, ярусні або нестандартні замовлення можуть бути прийняті понад стандартний денний ліміт лише після ручного підтвердження.
022
023 #### Термін замовлення
024
025 - Стандартний термін для **будь-якого замовлення — 8–10 днів до дати видачі**.
026 - Для весільних та ярусних тортів рекомендовано бронювати дату приблизно **за 2 місяці або раніше**.
027 - Якщо клієнту потрібен торт раніше, ніж за 8–10 днів, він може надіслати **Special Request** із бажаною датою.
028 - Такі замовлення розглядаються індивідуально залежно від завантаження та можливості виконати замовлення.
029 - Навіть дуже термінове замовлення може бути прийняте вручну, якщо є така можливість.
030 - Календар показує стандартно доступні дати, але клієнт може окремо звернутися щодо дати, якої немає у стандартному календарі.
031 - Максимум передзамовлння на наступни два місяці. Якщо це весільний торт - можемо розмістити за 7-8 місяців до.
032   ‌
033
034 ‌
035
036 ‌
037
038 **Початок роботи над замовленням**
039
040 - Після оформлення замовлення є **48 годин для ручного підтвердження або відмови**.
041 - Якщо замовлення підтверджено вручну раніше, саме момент підтвердження вважається початком роботи.
042 - Якщо протягом 48 годин не було ручної дії, замовлення автоматично переходить у статус **«в роботі»** через 48 годин після оформлення.
043 - Після підтвердження замовлення воно вважається прийнятим до виконання та переходить у роботу відповідно до встановлених правил.
044
045 ### 2. Готові торти
046
047 - Підготовка готового торта може бути розпочата вручну в будь-який момент після підтвердження замовлення.
048 - Якщо ручного запуску не було, система автоматично запускає підготовку через **48 годин після оформлення замовлення**.
049 - Система повинна фіксувати дату та час фактичного початку підготовки.
050
051 #### Скасування готового торта
052
053 - Якщо клієнт скасовує замовлення **більш ніж за 3 дні до видачі**, повертається **50% сплаченої суми**.
054 - Інші 50% є неповоротним депозитом.
055 - Якщо скасування відбувається **протягом останніх 3 днів до видачі**, повернення коштів не здійснюється.
056 - Якщо замовлення було оплачено повністю, при скасуванні більш ніж за 3 дні повертається 50% суми; у межах останніх 3 днів — повернення немає.
057
058 ### 3. Самовивіз
059
060 #### Адреса
061
062 - Публічно на сайті показується лише: **St Katharine Docks**.
063 - Повна адреса: [REDACTED: private pickup address; authorized retrieval from this Trello comment required]
064 - Повна адреса та детальні інструкції надсилаються клієнту приблизно за **2 дні до видачі** або раніше, якщо необхідно.
065
066 #### Години видачі
067
068 - **Четвер:** 11:00–17:00
069 - **П’ятниця:** 10:00–17:00
070 - **Субота:** 12:30–15:00
071 - **Неділя:** 11:00–12:00
072
073 Тривалість одного слоту — **60 хвилин**.
074
075 Окремого ліміту кількості замовлень на один часовий слот немає. Кількість замовлень визначається загальним денним лімітом та фактичною можливістю прийняти замовлення.
076
077 #### Перенесення дати
078
079 - Дату видачі можна перенести лише на **наступний доступний день**:
080   - четвер → п’ятниця
081   - п’ятниця → субота
082   - субота → неділя
083 - Перенесення більше ніж на один день не передбачається через термін придатності торта.
084 - Перенесення можливе лише за наявності вільного місця на наступний день.
085 - Якщо стандартної доступності немає, клієнт може звернутися напряму для індивідуального рішення.
086
087 #### Перенесення часу
088
089 - Клієнт може самостійно перенести час видачі в межах того самого дня.
090 - Перенесення можливе не пізніше ніж за **6 годин до початку поточного слоту**.
091 - Якщо до початку поточного слоту залишається менше 6 годин, самостійне перенесення через сайт вже недоступне.
092 - Новий слот має бути доступним у межах того самого дня.
093 - Якщо потрібного слоту немає або виникли особливі обставини, клієнт може звернутися напряму.
094 - Правило повернення коштів залишається прив’язаним до **нової дати та часу видачі** після перенесення.
095
096 ### 4. Доставка
097
098 Клієнту доступні три способи отримання:
099
100 #### 4.1. Collection
101
102 - Безкоштовний самовивіз.
103
104 #### 4.2. Customer-arranged courier
105
106 - Клієнт самостійно організовує та оплачує кур’єра.
107 - Рекомендовані кур’єрські сервіси та детальні інструкції щодо їх замовлення надаються клієнту окремо.
108 - Після передачі торта кур’єру відповідальність за транспортування та стан торта під час доставки переходить до клієнта/кур’єра.
109 - Повна адреса та інструкції для передачі надаються клієнту окремо перед доставкою.
110 - Якщо залишилася сума до сплати, вона має бути повністю сплачена до передачі торта кур’єру.
111
112 #### 4.3. GK Delivery
113
114 - Персональна доставка GK Cake London або її водієм.
115 - Вартість починається **від £100** і залежить від адреси.
116 - Точна вартість визначається індивідуально після отримання адреси.
117 - На сайті має бути окрема опція **GK Delivery**, через яку клієнт залишає адресу та надсилає запит на розрахунок вартості.
118 - У цьому випадку GK Cake London відповідає за торт під час транспортування **до моменту його передачі клієнту**.
119
120 ### 5. Географія доставки
121
122 - Фіксованих зон доставки та автоматичних тарифів за зонами немає.
123 - GK Delivery розглядається індивідуально залежно від адреси.
124 - Стандартно доставка розглядається в межах Лондона, але можливість залежить від відстані та часу в дорозі.
125 - Адреси за межами Лондона або дуже віддалені адреси не підтверджуються автоматично.
126 - Клієнт може залишити **Special Request** із повною адресою, після чого GK Cake London окремо розгляне можливість доставки та повідомить вартість.
127 - Доставка за межі стандартної географії можлива лише після окремого підтвердження.
128 - Конкретні delivery zones та автоматичний розрахунок за зонами не використовуються.
129
130 ### Статус
131
132 **Дата відповіді:** 28.09.2026
133 **Статус:** готово до фінальної перевірки.
```

GK-019 slot-selection clarification (2026-10-06), canonical gk019-owner-r7: show available pickup/delivery times directly without repeated Check. Original hours, no slot count cap and chef delivery agreements remain; exact source and previous decision hashes preserved.
