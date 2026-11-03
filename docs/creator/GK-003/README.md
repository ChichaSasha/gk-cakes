# GK-003 owner evidence — intake 2026-10-04

Q2 now accepted in GK-019:24h initiation; unknown payment waits without replacement; late paid needs chef feasibility and explicit customer agreement, otherwise full return. See current decisions/source and GK-019 q2-acceptance.json.

Current canonical reconciliation: **gk019-owner-r4**, from GK-019. Read current
`decisions.json` and `source-comments.json` plus the
[shared contracts](../../design/contracts.md) and
[revision manifest](../../design/gk019/output-manifest.json). Original intake/handover
review receipts below identify historical revisions; they do not review this integration.
Exact prior JSON bytes are retained in `docs/design/gk019/prior/` and each old decision
remains in previous_decision_revisions. Source statements and old publication hashes
are preserved. Current unresolved integration choices remain in GK-019.


## Current handover — 2026-10-05

Read [manual-handover-r2](handover.md), [exact cases](acceptance-cases.md) and
[revision/verification manifest](handover.json) **before the historical intake below**.
Current user clarifications and verified GK-001 source replies replace old 3/4-day,
zero-refund/no-show exclusions, 48-hour quote/payment validity and current Revolut
collection: both cake types use fixed original-handover−120h 100%/50% total refund;
chef cancellation 100%; paid GK delivery refunded separately only by chef. Current
orders use website payment within 24h, including chef-created templates. Checkout
hold 15 minutes; seven cakes; successful full payment accepted even if capacity filled.
Guest/claim verification and actual financial evidence remain required. Old examples
and statements below are source history, not current fallback defaults. New answers
are attributed to Sasha, not fabricated direct chef approval. Exact decision/source
hashes and Trello readback are in the manifest; these local files remain unpushed.


Cross-policy reconciliation: [current GK-001 handover](../GK-001/handover.md) and
the actual later user replies resolve both cake types, original-deadline timing and
100%/50% total-cost scope. Only the superseded wording remains below as history.
Original chef evidence and historical receipts are preserved; current decisions
and handover incorporate the explicit user replacements.


## Historical main-ticket follow-up — superseded by manual-handover-r2

See the [2026-10-04 acceptance check and questions](follow-up.md) and
[read/review/publication receipt](follow-up.json). The [main-ticket comment](https://trello.com/c/ftGtoGNB#comment-6ac2cb7b9d60195e8789c1ed) is posted and verified.
The follow-up is agent-authored, not owner approval. Read it alongside the original evidence and decisions; ingest
answers by source identity/revision before resolving a question. The original intake
sections below describe the earlier read-only ingestion. Live status remains on Trello.


Read with [structured decisions](decisions.json), [source comments and provenance](source-comments.json), and the [owner data contract](../../design/owner-data-contract.md).

Owner card [BO-GK-003](https://trello.com/c/5FbrZxfc/3-bo-gk-003o020-%D0%BF%D0%BE%D0%B3%D0%BE%D0%B4%D0%B8%D1%82%D0%B8-%D1%96%D0%BD%D0%B4%D0%B8%D0%B2%D1%96%D0%B4%D1%83%D0%B0%D0%BB%D1%8C%D0%BD%D1%96-%D0%B7%D0%B0%D0%BC%D0%BE%D0%B2%D0%BB%D0%B5%D0%BD%D0%BD%D1%8F-%D0%BE%D0%BF%D0%BB%D0%B0%D1%82%D1%83-%D0%B9-%D0%BF%D0%BE%D0%B2%D0%B5%D1%80%D0%BD%D0%B5%D0%BD%D0%BD%D1%8F) (`6ab8590615d32993baafa183`), board `6ab846cadeac31eff3e42507`. Parent [GK-003](https://trello.com/c/ftGtoGNB/17-gk-003p0o020w00-approve-customer-agreement-accounts-and-refund-policy) (`6ab13b539c0ea3a1a49176e9`). Identities matched the role mapping and live reads.

Author reported by Trello: **Elina Hryhorian**, `@elinahryhorian`, member `6aba9a6ee5e52c67f1fbb09b`. [Original comment](https://trello.com/c/5FbrZxfc#comment-6abac3d56b4b97c36cd21b0c) / action `6abac3d56b4b97c36cd21b0c`, source timestamp `2026-09-28T19:45:25.019Z`, retrieval `2026-10-04T20:59:15.339916+00:00`. Content SHA-256 `5054a11ed6c4dfa8a5d012fdc9e999074550645199f48e9dd5fbb3299827e6cb` (original UTF-8). Attribution comes from the author’s business answer and explicit owner self-attribution in BO-GK-005; membership/list placement does not establish approval. Sasha is the developer.

One current comment returned per owner card; no comments on corresponding parents; all six attachment lists empty. CLI has no pagination controls. Authenticated browser showed the same comment and original creation event without an older-comment control; observed comment permalinks are saved. Current exposed content is covered; deleted comments and prior edited revisions are not exposed. Preserve newly observed revisions by `(comment ID, content fingerprint)` on rerun. Descriptions are instruction/example snapshots, not owner decisions.

## Український readback — чернетка для Саші, не надіслано

Зафіксовано ручне погодження індивідуального замовлення, пропозицію на 48 годин від надсилання без бронювання, успішну повну оплату 100%, можливість покупки без кабінету та Revolut-інвойс для окремих замовлень. Повернення рахується від фактично сплаченої після знижки ціни торта, доставка окремо. Приклади £70 та £50 збережені точно. Це не фінальна юридична перевірка і не погодження всіх невизначених станів.

## English interpretation and decision coverage

`confirmed` means the cited explicit business statement; it does not imply blanket acceptance, resolved semantics or legal compliance. Each entry below resolves to `/decisions/<key>` in decisions.json. Original wording controls.

| Decision key | Status | Source lines | Faithful interpretation |
| --- | --- | --- | --- |
| `request_fields` | confirmed | 5–19 | Custom requests via Instagram require enough information to evaluate design, work and price. One or multiple references can combine elements; a sufficiently described original idea may have no reference. Colour palette is conditional. |
| `order_contacts` | confirmed | 21–26 | Additional contacts for placing a custom order; requirement scope beyond custom orders needs design review. |
| `communication` | confirmed | 30–34 | Instagram is primary for ordinary custom work. Email is possible for listed cases or customer preference. Collecting phone/email does not require communicating through them. |
| `agreement` | confirmed | 38–49 | All custom orders agreed manually; customer agrees these details and price before payment. |
| `paid_commitment` | confirmed | 51–51, 59–63, 110–112 | Only successful full payment places the order and reserves production date/space. During quote validity other customers may fill it; if full at payment it cannot be confirmed, offer another date or option. |
| `quote_validity` | confirmed | 55–57, 65–65 | Custom agreed offer valid for 48 hours from sending to customer, without date reservation; after 48 hours price/date/other terms may be revised. This is not the production 48-hour clock. |
| `daily_limit` | confirmed | 69–69 | Website displays standard available slots on each workday; daily limit 7 orders. BO-GK-001 says cakes. Public numerical availability conflicts with the older flags-only draft. |
| `capacity_exception` | confirmed | 71–77 | A full website calendar still permits a custom request. Manually assess workload, complexity, size/type, available time and production capability; approve over standard limit when feasible. |
| `pricing_factors` | confirmed | 81–96 | Base price may change for design complexity, decoration quantity, materials and added work. Listed extras may cost more; quote individually and let customer accept final price before payment. No surcharge amounts/tiers supplied. |
| `guest_purchase` | confirmed | 100–100 | Purchase without an account is allowed. |
| `reorder` | confirmed | 102–102 | Reconfirm current details on every reorder; do not clone old commitments. |
| `historical_claim` | confirmed | 104–104 | Link an old order to an account only after verifying it belongs to that customer; exact verification mechanism not supplied. |
| `payment_methods` | confirmed | 108–108, 114–116, 206–206 | Selected orders may pay by Revolut invoice; same payment/cancellation/refund rules as checkout. This is a business method choice, not provider setup or integration approval. |
| `cancellation_early` | confirmed | 120–126 | Paid, production not started, more than 4 days before collection/delivery: customer may cancel, 50% of actually paid cake price plus all unperformed delivery fee. BO-GK-005 explicitly confirms this business policy, legal review pending. |
| `cancellation_late` | confirmed | 128–132 | At 4 days or less, customer may cancel but cake is not refunded; unperformed delivery fee returned fully. Cancellation permission is separate from refund entitlement. |
| `started_or_ready` | confirmed | 134–144 | Started/ready: no cake refund, unperformed delivery fee refunded fully. The owner states money treatment, not an explicit self-cancel command permission for each state. |
| `after_handover` | confirmed | 146–150 | After customer handover ordinary cancellation/refund is not offered; nonconformity and mandatory consumer rights remain. This is owner wording, not verified law. |
| `no_show` | confirmed | 154–160 | Missed collection of ready cake: no refund; possible next-day move under stated rules. Sunday cannot move to Monday (nonworking). If missed again on agreed moved day, cake may be disposed of without refund. |
| `business_cancellation` | confirmed | 164–164 | When GK cancels an already confirmed order for reasons other than customer breach of agreed terms, refund 100% actually paid. Customer-breach policy not supplied. |
| `refund_allocation` | confirmed | 168–176 | Apply percentage only to cake amount actually paid after discount; handle delivery separately, full refund if unperformed, none if performed. Never calculate cake refund from original undiscounted price. |
| `refund_example_delivery` | example | 178–184 | Owner example: £100 cake + £20 delivery = £120 paid; eligible 50% cake and unperformed delivery give £50 + £20 = £70. £20 is illustrative, not a GK Delivery tariff. |
| `refund_example_discount` | example | 186–194 | Owner example: £120 cake less £20 discount = £100 paid; 50% refund is £50. Not a catalog price or discount setting. |
| `refund_example_remaining` | example | 196–202 | Owner example: £70 total owed less £20 already successfully returned means £50 still owed. Pending is shown separately and is not completed. This does not authorize dispatching the remaining obligation while a pending refund may still succeed. |
| `offline_evidence` | confirmed | 208–215 | Store evidence linking successful Revolut payment and any actual refund to order/date/amount. Bank/payment documents belong in protected storage, with safe references in context. |
| `legal_questions` | unresolved | 219–224 | Owner explicitly asks for qualified UK consumer-law review; these policies do not establish legal compliance. |

## Acceptance coverage and downstream impact

- AC1 partial: paid/not-started, late, started, ready, handed-over, no-show and business cancellation money rules captured; unpaid/expired/already-cancelled/customer-breach/exception permissions incomplete.
- AC2 partial: exact delivery/discount/prior-success examples captured; pending identified separately; numeric pending-then-failed example and policy reconciliation still missing.
- AC3 partial: guest purchase, revalidation, manual quote, full-payment commitment and ownership proof required; actual claim proof and checkout-hold compatibility unresolved.
- AC4 pending: owner business statements are not blanket ticket approval; legal review and complete technical evidence absent.

Every decision maps to a proposed typed SQL/admin consequence in [owner-data-contract.md](../../design/owner-data-contract.md). These assets are planning inputs, not migrations, seeds or permission to change settings. Record source/decision SHA-256 and contract revision in each downstream packet; use exact confirmed values in fixtures and keep conflicts blocked. Proposed ticket updates are in that contract; published snapshots are unchanged.

## Уточнення для власниці — не надіслано

### BO-GK-003.question.refund_matrix

Уточніть окремо: чи може клієнт скасувати вже розпочатий/готовий торт без повернення його ціни? Які правила для неоплаченого, простроченого й уже скасованого замовлення та порушення умов клієнтом? Чи при неявці повертається невиконана доставка? Якщо GK скасовує після виконаної доставки, чи 100% включають її?

Source: [comment](https://trello.com/c/5FbrZxfc#comment-6abac3d56b4b97c36cd21b0c), `BO-GK-003.cancellation_early`, `BO-GK-003.cancellation_late`, `BO-GK-003.started_or_ready`, `BO-GK-003.after_handover`, `BO-GK-003.business_cancellation`, `BO-GK-003.no_show`. Why it matters: Complete action matrix and precedence; no implied manager override or broad refund denial.

### BO-GK-003.question.payment_capacity

Пропозиція діє 48 годин від надсилання без бронювання. Чи допускаєте коротке технічне утримання місця під час checkout? Що саме показувати про кількість доступних місць? Що вважаємо моментом надсилання і чи можна оплатити рівно через 48 годин?

Source: [comment](https://trello.com/c/5FbrZxfc#comment-6abac3d56b4b97c36cd21b0c), `BO-GK-003.paid_commitment`, `BO-GK-003.quote_validity`, `BO-GK-003.daily_limit`, `BO-GK-003.capacity_exception`. Why it matters: Reconcile checkout holds, numeric availability and quote expiry without promising unpaid booking.

### BO-GK-003.question.proof_payment

Які докази достатні для прив’язки старого замовлення та підтвердження Revolut-оплати/повернення, хто це перевіряє? Чи всі чотири контакти потрібні для кожного типу замовлення, і як замовити клієнту без Instagram?

Source: [comment](https://trello.com/c/5FbrZxfc#comment-6abac3d56b4b97c36cd21b0c), `BO-GK-003.historical_claim`, `BO-GK-003.payment_methods`, `BO-GK-003.offline_evidence`, `BO-GK-003.order_contacts`. Why it matters: Verification authority and conditional required fields remain unspecified; bank originals stay private.

### BO-GK-003.question.refund_examples

Потрібно погодити приклад: частина повернення очікується, потім не вдалася. Розробник підготує суми та залишок зобов’язання. Також погодимо округлення половини пенні й розподіл між кількома позиціями; хто перевірить пропорційність утримання 50% та правила 4 днів із фахівцем?

Source: [comment](https://trello.com/c/5FbrZxfc#comment-6abac3d56b4b97c36cd21b0c), `BO-GK-003.refund_example_remaining`, `BO-GK-003.refund_allocation`, `BO-GK-003.legal_questions`. Why it matters: Pending obligation must not be mistaken for immediately safe payout; examples lack rounding and legal signoff.

## Original Ukrainian evidence

Line numbers below are stable within the fingerprinted revision and match JSON references. Markdown, typos, qualifiers and English text embedded by the owner are retained. Only the disclosed private-address exclusion is redacted.

```text
001 # BO-GK-003
002
003 ## Індивідуальний запит
004
005 Індивідуальні запити клієнт може надсилати через Instagram.
006
007 Для розрахунку індивідуального замовлення клієнт має надати:
008
009 - бажану дату видачі / доставки;
010 - кількість порцій або бажаний розмір торта;
011 - бажаний смак / начинку;
012 - повний опис того, як клієнт бачить майбутній торт;
013 - бажаний дизайн та всі важливі деталі;
014 - бажану кольорову гаму, якщо вона має значення;
015 - один або декілька референсів дизайну.
016
017 Референси можуть бути роботами GK Cake London, інших кондитерів або дизайнерів, зображеннями з Pinterest, Instagram чи інших джерел. Клієнт також може надати декілька різних референсів, якщо хоче поєднати окремі елементи або створити власну ідею на їх основі.
018
019 Клієнт може описати власну ідею без готового референсу. Важливо надати достатньо інформації про бажаний результат, щоб GK Cake London могла оцінити дизайн, обсяг роботи та вартість.
020
021 Для оформлення замовлення клієнт додатково залишає:
022
023 - повне ім’я;
024 - номер телефону;
025 - Instagram username;
026 - email.
027
028 ## Комунікація
029
030 Для звичайних індивідуальних замовлень основним каналом комунікації є Instagram.
031
032 Email може використовуватися для комунікації з компаніями, великими замовленнями, зйомками, корпоративними клієнтами або в інших випадках, коли клієнту зручніше вести офіційну комунікацію електронною поштою.
033
034 Номер телефону та email збираються як контактні дані клієнта, але це не означає, що комунікація обов’язково відбуватиметься через ці канали.
035
036 ## Погодження ціни та деталей
037
038 Усі індивідуальні замовлення погоджуються вручну.
039
040 Перед оформленням замовлення GK Cake London погоджує з клієнтом:
041
042 - остаточну ціну;
043 - дату;
044 - розмір / кількість порцій;
045 - смак / начинку;
046 - дизайн та погоджені деталі;
047 - спосіб отримання / доставки.
048
049 Після погодження клієнтом остаточної ціни та деталей клієнт може перейти до оплати.
050
051 Замовлення вважається розміщеним, а дата та місце у виробничому графіку заброньованими лише після успішної повної оплати замовлення.
052
053 ## Строк дії пропозиції та резервування дати
054
055 Індивідуальна пропозиція із погодженою ціною та деталями дійсна протягом 48 годин з моменту її надсилання клієнту.
056
057 Строк дії пропозиції не означає резервування дати.
058
059 Дата та місце у виробничому графіку бронюються лише після успішної повної оплати замовлення.
060
061 Кількість замовлень, які GK Cake London може прийняти на один день, обмежена. Тому протягом 48 годин доступність бажаної дати може змінитися, якщо вільні місця будуть заброньовані іншими клієнтами.
062
063 Якщо на момент оплати бажана дата вже заповнена, замовлення на цю дату не може бути підтверджене. Клієнту може бути запропонована інша доступна дата або інший варіант замовлення.
064
065 Після закінчення 48 годин ціна, дата та інші умови пропозиції можуть бути переглянуті.
066
067 ## Доступність слотів
068
069 На сайті відображається стандартна кількість доступних слотів на кожен робочий день. Стандартний денний ліміт становить 7 замовлень.
070
071 Якщо всі стандартні слоти на бажану дату вже заповнені, клієнт все одно може надіслати індивідуальний запит.
072
073 Такі запити розглядаються вручну залежно від поточного завантаження, складності замовлень на цю дату, розміру та типу торта, наявного часу та виробничої можливості GK Cake London.
074
075 За наявності додаткової можливості GK Cake London може погодити замовлення понад стандартний денний ліміт.
076
077 Тому відсутність доступних слотів на сайті не завжди означає, що замовлення на цю дату неможливе.
078
079 ## Формування остаточної ціни
080
081 Базова ціна торта може змінюватися залежно від складності дизайну, кількості декору, використаних матеріалів та додаткового обсягу роботи.
082
083 Додаткова вартість може застосовуватися, зокрема, за:
084
085 - металізоване покриття;
086 - велику кількість або складний piping;
087 - бантики та інші додаткові декоративні елементи;
088 - вишні та інший спеціальний декор;
089 - свіжі ягоди та фрукти;
090 - живі квіти;
091 - dome-shaped cakes, якщо така форма збільшує кількість порцій або обсяг роботи;
092 - інші складні або додаткові елементи дизайну.
093
094 Точна вартість визначається індивідуально для кожного замовлення.
095
096 Клієнт отримує остаточну ціну до оплати та самостійно вирішує, чи погоджується на запропонований варіант.
097
098 ## Покупка без кабінету
099
100 Покупка без створення облікового запису дозволена.
101
102 При повторному замовленні необхідно повторно підтвердити актуальну ціну, дату, доступність, смак / склад, дизайн та інші деталі нового замовлення.
103
104 Старе замовлення може бути пов’язане з обліковим записом лише після підтвердження, що воно належить відповідному клієнту.
105
106 ## Оплата
107
108 Основним способом оплати є онлайн-оплата через checkout.
109
110 Замовлення оплачується у розмірі 100% до резервування дати.
111
112 Замовлення вважається розміщеним і дата вважається заброньованою лише після успішної повної оплати.
113
114 Для окремих замовлень, зокрема великих або корпоративних, оплата може здійснюватися за інвойсом через Revolut.
115
116 Для таких платежів застосовуються ті самі правила оплати, скасування та повернення коштів, що й для замовлень, оплачених через checkout.
117
118 ## Скасування та повернення коштів
119
120 ### Оплачено, виробництво ще не розпочато, більше ніж 4 дні до видачі / доставки
121
122 Клієнт може скасувати замовлення.
123
124 Повернення становить 50% від фактично сплаченої ціни торта.
125
126 Якщо доставка ще не була виконана, вартість доставки повертається у повному обсязі.
127
128 ### 4 дні або менше до видачі / доставки
129
130 Клієнт може скасувати замовлення, але повернення вартості торта не здійснюється.
131
132 Якщо доставка ще не була виконана, вартість доставки повертається у повному обсязі.
133
134 ### Виробництво розпочато
135
136 Повернення вартості торта не здійснюється.
137
138 Якщо доставка ще не була виконана, вартість доставки повертається у повному обсязі.
139
140 ### Торт готовий
141
142 Повернення вартості торта не здійснюється.
143
144 Якщо доставка ще не була виконана, вартість доставки повертається у повному обсязі.
145
146 ### Торт переданий клієнту
147
148 Після передачі торта клієнту скасування замовлення та повернення коштів за звичайною процедурою скасування не передбачаються.
149
150 Це не обмежує обов’язкові права клієнта у випадках, коли товар не відповідає погодженому замовленню або застосовуються інші передбачені законом права споживача.
151
152 ## Якщо клієнт не забирає торт
153
154 Якщо клієнт не забирає готовий торт у погоджений час, повернення коштів не здійснюється.
155
156 Якщо це можливо, отримання може бути перенесене на наступний доступний робочий день відповідно до правил перенесення.
157
158 Якщо день отримання припадає на неділю, перенесення на понеділок неможливе, оскільки понеділок є неробочим днем.
159
160 Якщо клієнт не забирає торт і в погоджений перенесений день, торт може бути утилізований без повернення коштів.
161
162 ## Якщо замовлення скасовує GK Cake London
163
164 Якщо GK Cake London скасовує вже підтверджене замовлення не через порушення клієнтом погоджених умов, клієнту повертається 100% фактично сплаченої суми.
165
166 ## Розрахунок повернення коштів
167
168 Відсоток повернення застосовується лише до фактично сплаченої ціни торта.
169
170 Вартість доставки розраховується окремо.
171
172 Якщо доставка ще не була виконана, сплачена за неї сума повертається у повному обсязі.
173
174 Якщо доставка вже була виконана, вартість доставки не повертається.
175
176 Якщо на замовлення була надана знижка, відсоток повернення розраховується від фактично сплаченої після знижки ціни торта, а не від початкової ціни.
177
178 ### Приклад 1
179
180 Торт £100 + доставка £20 = £120 сплачено.
181
182 Якщо клієнт має право на повернення 50% вартості торта і доставка ще не була виконана:
183
184 £50 за торт + £20 за доставку = £70 повернення.
185
186 ### Приклад 2
187
188 Початкова ціна торта £120.
189
190 Знижка £20.
191
192 Фактично сплачено £100.
193
194 При поверненні 50% сума повернення становить £50.
195
196 ### Приклад 3
197
198 Загальна сума, яку необхідно повернути клієнту, становить £70.
199
200 Якщо £20 уже було успішно повернуто, необхідно додатково повернути £50.
201
202 Якщо частина повернення має статус pending і ще не була успішно завершена, вона відображається окремо та не вважається завершеним поверненням.
203
204 ## Оплата поза сайтом
205
206 Для окремих замовлень оплата може бути здійснена за інвойсом через Revolut.
207
208 Для підтвердження оплати GK Cake London зберігає:
209
210 - інвойс;
211 - підтвердження успішної оплати / банківську транзакцію;
212 - дату та суму платежу;
213 - інформацію про замовлення, до якого належить платіж.
214
215 У разі повернення коштів зберігається підтвердження здійсненого refund / переказу та сума повернення.
216
217 ## Юридичні питання, які ще треба перевірити
218
219 Необхідно окремо перевірити з кваліфікованим спеціалістом з UK consumer law:
220
221 - чи відповідають правила повернення 50% / 0% вимогам щодо справедливості та пропорційності cancellation charges;
222 - чи є правило «більше ніж 4 дні / 4 дні або менше» юридично коректним та достатньо обґрунтованим;
223 - чи потрібні окремі формулювання для індивідуально виготовлених, швидкопсувних, великих та весільних тортів;
224 - чи потребують додаткового формулювання умови 100% передоплати та пов’язані з ними правила повернення.
225
226 Дата відповіді: 28.09.2026
227
228 Статус: **готово до перевірки / юридичні положення щодо скасування та повернення коштів залишаються на перевірці.**
```

GK-019 Q5 (2026-10-06) adds approved verified alternative return for an owed refund when provider definitively cannot process it; unknown blocks all routes. Canonical gk019-owner-r6 retains prior revision/source hashes.
