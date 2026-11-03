Creator handoff — GK-003 — manual-handover-r2 — 2026-10-05

Final expected result and manual handover. Sasha explicitly requested recording these answers, independent subagents and main GK-003 Done. The original chef answer is retained; later user clarifications supersede the conflicting portions. This is decision acceptance, not software completion or legal approval.

Sources: Elina's original BO-GK-003 answer https://trello.com/c/5FbrZxfc#comment-6abac3d56b4b97c36cd21b0c plus Sasha's actual 2026-10-05 answers in the GK-003 task and the linked GK-001 fixed-deadline/both-cake-type clarifications. New replies are not falsely attributed to the chef.

Погоджений результат:

• Індивідуальний запит: дата, порції/розмір, смак, дизайн і деталі, кольори за потреби; референси можна замінити достатнім описом. Перед оплатою шеф погоджує остаточну ціну, дату, розмір, смак, дизайн і спосіб отримання.
• Усі поточні оплати через сайт: клієнт оформлює сам або шеф після домовленості в Instagram створює шаблон і дає посилання для погодження та оплати. Строк — 24 години. Неоплачений шаблон ще не прийняте замовлення; після строку оплата вимикається. Попередні 48 годин пропозиції та Revolut як поточний спосіб замінені; старі платежі/докази зберігаються.
• Пропозиція не бронює дату. Утримання під час оплати — 15 хвилин. Показуємо фактично вільні місця, стандарт 7 тортів за днем видачі. Якщо повна оплата успішна, замовлення прийняте навіть при заповненій даті. Окреме правило підготовки +48 годин із GK-001 не змінюється.
• Завжди повне ім'я клієнта і дані іншого платника, якщо платить не клієнт. Покупка без кабінету дозволена. Старе замовлення прив'язує шеф лише до перевіреної особи з доказом належності замовлення; сам Instagram/референс не доказ. Повторення потребує актуальних ціни, складу/алергенів, дизайну, дати й доступності.
• Обидва типи тортів: 100% фактично сплаченої загальної суми більше ніж за 120 годин до ПЕРВІСНОЇ видачі; рівно на межі й пізніше — рівно 50%, включно з неявкою/після видачі. Шеф особисто скасовує — 100%. Зміна дати/часу не змінює збережений початковий дедлайн. Старі 3/4-денні та 0% правила — історія.
• Доставка GK входить у розрахунок, але шеф задає її ціну окремо, оплата окрема, повертає лише шеф окремо через адмінпанель. Якщо GK не виконав доставку зі своєї вини — повернення доставки 100%, мінус уже повернене. Сторонній кур'єр, оплачений клієнтом, не є платежем GK.
• Скасоване не відкривається знову: нове замовлення. Розпочатий/готовий торт дозволено скасувати за єдиним правилом; шеф може виставити його за 50% вартості з тими самими датами. Необговорені випадки — особисто через шефа (Instagram/WhatsApp/email), без автоматичної довільної відмови в поверненні. Законні права збережені.
• Повертаємо всю належну суму в межах кожного платежу однією операцією; доставку окремо шеф. Помилки й залишок фіксуються. Очікуваний/невідомий результат не запускаємо повторно як нове повернення. Частку пенні округлюємо на користь клієнта.
• Побажання щодо фото — публікація за умовами з опцією відмови. GK-005 перевіряє права/формулювання до публікації; це не дозвіл на сторонні референси. Юриста залучають перед фінальним запуском, перевірку Terms проводять перед їх фінальною публікацією.

State/action matrix: unpaid/expired = nonaccepted template, no refund without actual collection; paid-confirmed/in-production/ready = unified 100/50 by fixed deadline; completed/no-show = 50% with chef handling; cancelled = never reopen and preserve refund debt. Current manual agreements use chef-created website templates; historic offline evidence remains auditable. Unexpected transfer plus pending online payment requires reconciliation, not an arbitrary paid switch.

Exact examples (GBP):
• £100 cake + £20 separately paid GK delivery: ordinary 50% target £60 = £50 cake + £10 chef delivery; 100% target £120 = £100 + £20.
• Cake £120 minus £20 discount: paid £100, 50% cake allocation £50.
• Historical target £70, £20 succeeded, £50 pending: £50 owed, £0 extra dispatch. Verified failure leaves £50 retryable as one operation; success leaves £0. Old £70 eligibility is history, not current ordinary 50% policy.
• One historical collection paid £120: full-return target £120, £20 succeeded, £50 pending: £100 owed. Wait for that collection's pending result: failure leaves £100; success leaves £50. Never resend £120.
• Cake refund £50 succeeds but delivery refund £10 fails: still owe £10; chef retries only after verified failure. Do not call the whole order fully refunded.
• 50% of 10001p rounds to 5001p. Technical multi-item allocation: ceil aggregate entitlement, allocate floors plus largest remainders, stable tie order; component totals must equal target and never exceed paid amounts. No separately added VAT in fixtures under current GK-005 evidence.

Developer interpretations for GK-019: retain sending-to-customer anchor, payment expires sent_at+24 elapsed hours; at equality disable new initiation and also prohibit repeat payment when paid. Expiry does not erase an in-flight payment. Exact minimal payer/provider fields and UI wording are technical design, not invented chef quotations.

Manual handover: load docs/creator/GK-003/handover.md, acceptance-cases.md, decisions.json, source-comments.json and handover.json at the recorded hashes; read latest GK-001/GK-005 and owner-data-contract. These files are local/unpushed: obtain the exact packet before another checkout uses it. GK-019 reconciles changed contracts and dependent tickets; no automatic Ready transitions. Retain historical source and payment facts. No application implementation, service signup, public Terms/photos, release or deployment is authorized.

Acceptance evidence: AC1 state/action matrix; AC2 16 exact money cases; AC3 agreement/verified-claim/payment safeguards plus 16 workflow cases; AC4 source hashes, independent review and publication readback. Three document checks pass: check_owner_intake.py, check_planning_kit.py, check_role_packets.py. Review and final hashes are in the companion verification receipt. Future lawyer/provider/UI execution remains downstream.
