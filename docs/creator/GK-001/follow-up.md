GK-001 acceptance follow-up — 2026-10-04 — main-acceptance-r1

Result: owner input is captured, but AC1–AC4 are not yet fully satisfied. Keep this main ticket open. Sasha is the developer; Elina is the business owner. The questions below are for Sasha to coordinate; no blanket owner approval is inferred.

Source: BO-GK-001, Elina's 2026-09-28 comment [source comment](https://trello.com/c/rVHbBffB#comment-6abab66bbf2b0e811db912b7). Cross-check BO-GK-003 [source comment](https://trello.com/c/5FbrZxfc#comment-6abac3d56b4b97c36cd21b0c) and BO-GK-005 [source comment](https://trello.com/c/PSsIz3tI#comment-6abacbecde4959cdb8da9e21).

Already captured: Thu–Sun; up to 7 cakes/day; 8–10 days; 48-hour start rule; exact collection windows; 60-minute slots without a separate slot order cap; same-day time moves at least 6 hours before the existing slot; individual GK Delivery quotes from £100, with no fixed zones. These confirmed portions remain valid while the following gaps are resolved.

Уточнення для власниці (можна відповідати за номерами):

1. refund_scope — Для готових тортів у BO-GK-001 вказано 3 дні, у BO-GK-003/005 — 4 дні. Це окреме правило чи правило 4 днів його замінює? Після початку підготовки готового торта чи можна скасувати і що повертається? «50% сплаченої суми» стосується торта чи також доставки? Як повертається невиконана доставка? Чи перенесення дати або часу може відновити право на повернення після початку роботи? Це визначає суму й пріоритет правил.

2. days_months — «8–10 днів», «3/4 дні», «приблизно 2 дні» — календарні/робочі дні чи точні години; від якої події та до якого моменту їх рахувати? Яка найраніша доступна дата в діапазоні 8–10 і чи він поширюється на вже готові торти? Як рахувати «наступні два місяці» та 7–8 місяців для весільних тортів, включно з останньою датою? Чи 2 місяці для весільних залишаються рекомендацією? Чи є окремі години виробництва, свята/закриті дні; якщо ні — так і зазначте. Це потрібно для календаря без вигаданих меж.

3. count — Ліміт 7 — тортів чи замовлень: скільки місць займає одне замовлення з двома тортами? Це день виготовлення чи видачі, якщо вони різні? Хто може дозволити перевищення; чи є верхня межа, чи кожен випадок вирішується окремо? Що робити, якщо два клієнти одночасно оплачують останнє місце, і що робити з уже прийнятими замовленнями при зменшенні ліміту? Це визначає обіцянки клієнтам і повноваження адміністратора.

4. slot_grid — У суботу 12:30–15:00, слот 60 хвилин: які точні інтервали показувати й що робити з останніми 30 хвилинами? Чи можуть інтервали перекриватися, і чи кінець слота має вкладатися у години видачі? Хто може змінювати дату, чи дозволені повторні перенесення, що робити, коли наступний день зайнятий, та після неділі? Як рахувати максимум один день з огляду на строк зберігання? Це потрібно для доступних варіантів і випадку перенесення на заповнений день.

5. placement_clock — Від якої події рахувати 48 годин після «оформлення»: повної успішної оплати чи іншої? Для готового торта підтвердження і початок підготовки — окремі дії? Що робити з терміновою видачею раніше 48 годин, відмовою після оплати та скасуванням рівно у момент автоматичного початку? Якщо система запізнилася, правило діє від запланованої чи фактичної події? Саша підготує зрозумілі приклади переходів літнього/зимового часу після вибору точки відліку.

6. delivery_details — Які часові вікна обіцяємо для GK Delivery, хто встановлює ціну і як довго вона діє? Наведіть без приватних адрес приклади доставки в Лондоні та поза ним: що можна підтвердити, що відхиляємо або погоджуємо окремо? Які рекомендації кур'єрів та інструкції ще потрібно додати? Хто й яким каналом надсилає повну адресу приблизно за 2 дні або раніше? Чи згаданий «залишок до оплати» є винятком із 100% оплати до бронювання? Це потрібно для ціни, вікна передачі й умов оплати.

Developer completion work:
- AC1: replace obsolete proposed X-hours-before-pickup/weighted-capacity assumptions with the actual owner rule only after resolving its anchor and scope. Record manual start, automatic equality, late scheduler and ready-preparation outcomes. Prepare both Europe/London DST walkthroughs; do not convert calendar days/months into hours.
- AC2: obtain intended outcomes for simultaneous last-place payments, full-target move and reducing capacity below existing commitments; preserve the no-separate-pickup-slot-cap decision. Demonstrate consequences before owner signoff.
- AC3: record accepted quote fee/window, address-based eligibility and courier/customer handover responsibility. No zone tariff engine implied.
- AC4: obtain acceptance of the exact resulting policy and examples, then record revision/review evidence here. This is a Decision ticket; implementing dependent features is not required to complete it.

Durable local packet: docs/creator/GK-001/{README.md,source-comments.json,decisions.json,follow-up.md,follow-up.json}; all question suffixes above map to BO-GK-001.question.<suffix>. Source decision IDs and wording remain in decisions.json. SQL/admin consequences: docs/design/owner-data-contract.md. Files are local and unpushed: Sasha must share this exact packet with future workers. Published ticket descriptions remain historical snapshots; this comment is the current gap record, not a new owner decision.
