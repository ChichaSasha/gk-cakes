# Creator handoff — GK-003 — manual-handover-r2 — 2026-10-05

Status: **Final decision packet prepared for independent review and publication.**
[handover.json](handover.json) records the actual review/publication state and receipts;
this document alone does not assert those later actions have completed. User explicitly requested Done and
provided the clarifications below. No additional approval is requested.

Parent: [GK-003](https://trello.com/c/ftGtoGNB). Original chef answer:
[Elina, 2026-09-28](https://trello.com/c/5FbrZxfc#comment-6abac3d56b4b97c36cd21b0c),
re-read unchanged for this intake. Current user answers and exact words:
[initial follow-up](submission-2026-10-05.uk.txt),
[delivery clarification](clarification-2026-10-05.uk.txt),
[website/24-hour clarification](clarification-payment-2026-10-05.uk.txt).
The user's actual GK-001 clarification, deadline and scope replies are copied with
original hashes into [source-comments.json](source-comments.json), with explicit
`imported_from` attribution. No new direct chef comment or approval was fabricated.

[decisions.json](decisions.json) retains stable IDs and all previous revisions.
[acceptance-cases.md](acceptance-cases.md) supplies exact calculations and technical
cases. [handover.json](handover.json) pins input/output hashes, checks, review and
actual publication receipts. Live status is Trello's responsibility, not this file.

## Final expected result — український підсумок

1. **Індивідуальне замовлення.** Потрібні дата, розмір/порції, смак, опис і дизайн,
   важливі деталі, кольори за потреби. Можна надати кілька референсів або достатній
   опис без готового зображення. Шеф вручну погоджує остаточну ціну, дату, порції,
   смак, дизайн та спосіб отримання. Декор/складність оцінюється індивідуально.
2. **Оплата тільки через сайт.** Клієнт оформлює сам або домовляється з шефом через
   Instagram; тоді шеф створює шаблон на сайті й надсилає посилання для підтвердження
   та оплати. Строк оплати — **24 години**. Прострочений шаблон оплатити не можна.
   Прийняте замовлення завжди оплачене; неоплачений шаблон — ще не прийняте замовлення.
   Старий 48-годинний строк пропозиції та Revolut-шлях більше не є поточними правилами.
3. **Місця.** Пропозиція не бронює дату. Під час оплати допускається утримання місця
   на **15 хвилин**. Показуються фактично вільні місця; стандарт — **7 тортів** за
   датою видачі. Якщо повна оплата пройшла, замовлення прийняте навіть при заповненій
   даті. Початок підготовки через 48 годин від оплати — окреме правило GK-001.
4. **Клієнт і платник.** Завжди повне ім'я клієнта; якщо платить інша людина,
   потрібні також її дані. Не вимагаємо вигаданого Instagram. Покупка без кабінету
   дозволена. Старе замовлення прив'язує шеф через адмінпанель після перевірки
   особи та належності замовлення. Повторне замовлення підтверджує актуальні умови.
5. **Єдине повернення для обох типів тортів.** Більше ніж за **120 годин до первісної
   видачі** — **100%** фактично сплаченої загальної суми; рівно на межі та пізніше —
   **рівно 50%**, включно з неявкою та часом після видачі. При особистому скасуванні
   шефом — **100%**. Дедлайн зберігається при оформленні; зміна дати/часу його не
   пересуває. Це замінює старі 3/4-денні, 0% та несумісні окремі правила доставки.
6. **Доставка GK.** Входить у загальний розрахунок, якщо її виконує GK Cake chef,
   але її ціну шеф вносить окремо, оплата окрема і повернення робить окремо лише
   шеф через адмінпанель. Якщо доставку не виконано з вини GK — повне повернення
   сплаченої доставки, з урахуванням уже поверненого. Кошти стороннього кур'єра,
   якого оплачує клієнт, не є платежем GK.
7. **Скасування й винятки.** Скасоване замовлення не відкриваємо; потрібне нове.
   Розпочатий/готовий торт можна скасувати за єдиним правилом вище. Його можна
   повторно виставити за 50% вартості з тими самими датами видачі. Це можливість,
   не автоматична публікація чи продовження строку. Необговорені випадки — через
   особисте спілкування з шефом в Instagram, WhatsApp або email. Законні права збережені.
8. **Повернення коштів.** Уся належна сума в межах одного платежу повертається однією
   операцією, без навмисного дроблення. Окремо оплачена доставка повертається
   окремою операцією шефа. Помилка фіксується; уже успішні суми не стираються,
   а невідомий/очікуваний результат не запускається повторно як нове повернення.
   Частку пенні округлюємо на користь клієнта.
9. **Фото й юридична перевірка.** Висловлене побажання — публікація за умовами з
   опцією відмови клієнта. Це не перевірена юридична згода чи дозвіл на сторонні
   зображення; GK-005 має перевірити права й формулювання. Юриста буде найнято
   до фінального запуску; перевірка перед публікацією фінальних Terms зберігається.

## O1 — state/action, authority and amount matrix

For customer cancellation let D = original handover instant −120 elapsed hours,
saved at order placement. Rate R=100% if cancellation request is strictly before D;
R=50% at D or after it. This covers made-to-order and ready-made cakes, regardless
of production state. Date/time moves never recalculate D. Use actual amounts paid
after discount. GK delivery has a separate collection and chef-only refund action.

| State / trigger | Customer / chef action | Refund / accounting result |
| --- | --- | --- |
| Unpaid template / payment attempt | Confirm and pay while link valid; no accepted order until successful full payment | No successful collection means no refund. Pending is not proof of no collection. Chef may issue a new reviewed template after expiry. |
| Paid confirmed, not started | Customer cancellation; exceptional handling through chef | R of paid total; whole remaining cake allocation, separate chef delivery allocation. |
| In production | Customer may cancel; chef may choose resale | Same R and original D; old zero-refund and unconditional state-only 50% rules replaced by explicit unified policy. |
| Ready, before handover | Customer may cancel; optional resale at 50% stated cost, original handover dates | Same R; do not extend shelf life or duplicate stock automatically. |
| Completed / handed over | Personal chef contact for cancellation/refund request; retain fulfilled history | Exactly 50% under unified policy; no blanket ordinary-refund exclusion. Mandatory rights preserved. |
| Expired unpaid template | Payment function disabled; chef can issue newly agreed template | Not an accepted unpaid order. No reopening cancelled/expired payment attempt; actual late success must be reconciled, never discarded. |
| Cancelled order | Cannot reopen; a new order requires new agreement/payment | Existing refund target stays due; duplicate cancel cannot create another payout. |
| No-show | Chef contact for rescheduling or refund | 50% including separately paid GK delivery; replaces initial no-show exclusions. |
| GK fails to perform delivery | Chef records service failure and returns delivery charge | 100% delivery obligation, net of actual prior delivery refunds; cake entitlement determined separately. Never add a second duplicate delivery refund. |
| Chef personally cancels | Chef records cancellation and reason | 100% actual paid total, with separate delivery operation; no old breach exclusion silently restored. |
| Customer breach alleged / otherwise unaddressed | Personal chef decision, no automated broad refund-denial code | Current 100/50 policy and statutory rights remain; do not invent a penalty or override. |
| Chef/Instagram-arranged current order | Chef enters website template; customer confirms/pays on site | Same paid commitment, 24-hour payment window and refund policy; no current offline-paid shortcut. |
| Historical Revolut/manual record or unexpected transfer | Preserve real evidence; chef reconciles order, reference, amount, date and success | Historical source remains distinguishable; no fake provider capture, duplicate recording or deleted liability. |

Cancelled original order and optional resale are separate records. The original
customer's refund does not depend on finding a second buyer. Exact resale display
price basis and safe catalog/stock mapping are GK-019 technical design, with the
supplied 50% price and unchanged handover dates as constraints.

## O2 — agreement, account and payment safeguards

Current path: request → manually reviewed/current agreement or storefront order →
customer confirms terms → website payment → verified successful full payment →
accepted order. Chef-created templates are now explicitly selected, superseding the
earlier tentative link proposal. The 24-hour payment window replaces old 48-hour
quote validity; it does not change GK-001's separate production/preparation clock.

Technical interpretation for deterministic implementation (not extra chef quotations):

- Retain the original “from sending to customer” anchor: store the immutable
  template/payment-link sent event and expires_at=sent_at+24 elapsed hours. At
  server_now >= expires_at reject a new payment initiation, including equality.
  Already paid links also cannot collect twice. Resending the same link does not
  silently reset expiry; a new agreement/template receives a new window.
- The source typo “Якщо е оплачено” is preserved exactly. Combined with the stated
  24-hour window, the operational interpretation is no payment after expiry and
  no repeat payment once paid. This is a developer readback, not a silently edited quote.
- Record payment initiation/hold and actual provider outcome independently. A
  local 15-minute timer cannot prove that a provider operation failed. Reconcile
  a payment already underway when the timer/link expires; do not take a second
  payment or erase a real success. Genuine valid full payment is accepted even
  when capacity filled; mismatched/excess events are retained for chef resolution.
- Current accepted cake orders have no unpaid-balance exception. Separately quoted
  GK delivery is its own paid charge. New payment/collection uses the website;
  retain older offline balances and receipts as historical facts, not a current option.
- Always collect customer full name; collect distinct payer details when different.
  Exact supplementary payer fields were not supplied. GK-019 should minimise to
  payer identification and fields required by the payment provider/fulfillment;
  this is a technical proposal, not a new compulsory phone/email/document list.
  Use relevant contact details for the chosen delivery/communication channel;
  Instagram is not a universal prerequisite.
- Guest checkout stays allowed. Chef links an old order only to a verified account
  with order-ownership evidence. Store actor/order/account/safe evidence reference;
  insufficient evidence leaves it unlinked/pending verification without disclosure.
  A matching Instagram, email text or payment reference alone is not identity proof.
  Different payer identity does not automatically transfer access to their account.
- If an unexpected transfer is claimed while online payment is pending, record safe
  evidence and reconcile both actual outcomes; no arbitrary paid switch. If both
  really succeeded, keep both receipts and expose excess payment for chef resolution.
  Historic Revolut evidence uses actual transaction success, unique reference,
  amount, currency, date and order; bank documents stay in protected storage.
- Reorders revalidate current price, composition/allergens, design, date, availability
  and all new terms. Old history or reorder does not by itself publish any photo.

## O3 — accounting derivation and exact cases

[acceptance-cases.md](acceptance-cases.md) distinguishes current examples from the
original chef's historical £70 cake/delivery example. That old arithmetic is retained
as evidence, not an active calculation under the new policy.

For the applicable whole-order obligation T, actual successful refunds S and pending
or unknown amounts P: remaining=T−S; unreserved=max(0,T−S−P), bounded by real
collections. Do not deliberately dispatch a fragment while another operation for
the same collection is unresolved. Wait/reconcile, then attempt the whole remaining
allocation against that collection. Separate cake/delivery collections can have
separate outcomes; the overall refund is complete only when every obligation is met.

Customer-favour rounding technical derivation: round the aggregate target upward to
whole pence, then allocate that exact total across eligible paid components using
floor amounts plus largest fractional remainders; break equal remainders by stable
item/payment order. Cap at actual paid amounts, preserve the allocation snapshot.
This supplies a deterministic multi-item method without claiming the chef chose
that algorithm. A later delivery-only service-failure target never adds an already
returned amount again. Tax status comes from GK-005: no separately added VAT under
the owner-attested current status; no invented tax rate or tax deduction in fixtures.

## Acceptance and remaining work

| Original criterion | Evidence |
| --- | --- |
| AC1 | O1 covers all seven named states, chef/manual website path, historical offline facts, cancellation authority and exact unified 100/50 money rule. Actual user GK-001/GK-003 replies supersede old conflicts. |
| AC2 | O3 and M01–M16 show full targets, succeeded/pending/failed amounts, remaining obligation, separate delivery operation, rounding, discount and current tax treatment. Whole-obligation attempts do not erase prior partial facts. |
| AC3 | O2 retains verified identity plus order proof and chef confirmation; reviewed template/storefront terms precede verified full website payment. Unexpected offline/online overlap cannot create duplicate accounting. |
| AC4 | Exact sources/outputs and current user closure authorization are recorded. Independent review, document-check results and actual Trello readback are tracked in handover.json; pending steps must complete before Done. Software tests/PRs/releases are outside this Decision intake. |

No unresolved original refund-policy choice remains after the latest replies.
Technical interpretations above are explicit and subject to GK-019 integration,
not fabricated business quotations. Still outstanding downstream: implementation
and provider tests, privacy/rights/public Terms review in GK-005, exact minimal payer
fields and catalog resale workflow in GK-019/022/023/027. Hiring a lawyer is future
work; this handover does not claim legal compliance or authorize public Terms/launch.

## Manual handover to the next worker

1. Load this handover, current decisions/source JSON and acceptance cases at manifest
   hashes. Read current GK-001 and GK-005 intake and owner-data-contract. Local files
   are unpushed: transfer and verify this exact packet before using another checkout.
   The Trello handover is a self-contained human summary, not a claim of remote file access.
2. Check actual newer main/owner comments and task replies. Preserve authorship and
   hashes; do not resurrect old 48-hour quote, Revolut, 3/4-day or no-show exclusions.
3. GK-019 integrates the 24-hour website/template path, 15-minute hold, actual cake
   capacity, fixed original refund deadline, separate chef delivery refund and proof
   safeguards. Re-review affected K02/K04/K05 contracts, money ADR and dependent cards.
4. Use exact M/A cases for later meaningful implementation checks. Provider timing,
   race behavior, per-collection refund reconciliation and UI wording need execution
   evidence in their implementation tickets; document arithmetic is not that evidence.
5. After this packet's fresh review and checks, publish it on main GK-003 and use the
   user's existing Done instruction. Verify the live Done list and read the moved card
   back. Do not change the BO card or dependent statuses without scoped instructions.
6. GK-016/017/018/021/025/026/027/029/030 and affected catalog/capacity work consume this
   revision. A Done predecessor alone does not make another card Ready.

Replaces only expressly superseded policy/workflow portions. Original chef input,
previous decision versions and historic ticket/review receipts remain. No application
code, new service, public photo/Terms publication, Git commit/push, release or deployment.
