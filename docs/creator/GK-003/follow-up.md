GK-003 acceptance follow-up — 2026-10-04 — main-acceptance-r1

Result: owner input is captured, but AC1–AC4 are not yet fully satisfied. Keep this main ticket open. Sasha coordinates developer work and the Ukrainian owner questions below.

Source: BO-GK-003, Elina's 2026-09-28 comment [source comment](https://trello.com/c/5FbrZxfc#comment-6abac3d56b4b97c36cd21b0c). Linked cancellation conflict: BO-GK-001 [source comment](https://trello.com/c/rVHbBffB#comment-6abab66bbf2b0e811db912b7); BO-GK-005 confirmation [source comment](https://trello.com/c/PSsIz3tI#comment-6abacbecde4959cdb8da9e21).

Already captured: quote valid 48 hours from sending without reserving the date; booking after 100% successful payment; guest checkout; historical-order proof; reconfirm current details on reorder; Revolut invoices for selected large/corporate orders; separate cake/delivery refund allocation. Owner example: £100 cake + £20 delivery, 50% cake refund + unperformed delivery = £70; after £20 successfully refunded, £50 remains. Pending is not completed.

Уточнення для власниці (можна відповідати за номерами):

1. refund_matrix — Чи може клієнт скасувати вже розпочатий/готовий торт без повернення його ціни? Що дозволено для неоплаченого, простроченого та вже скасованого замовлення; чи можна відкрити його знову, чи потрібне нове? Які винятки може робити адміністратор, зокрема після видачі, хто це погоджує та які потрібні докази? Що вважаємо порушенням умов клієнтом? При неявці чи повертаємо невиконану доставку; якщо GK скасовує після виконаної доставки, чи 100% включають її? Будь ласка, разом із GK-001 уточніть окреме правило готових тортів 3/4 днів, точну межу й наслідки перенесення. Це потрібно для повної таблиці дій і сум у кожному стані; законні права не виключаємо.

2. payment_capacity — Чи допускається коротке технічне утримання місця лише під час оплати, хоча пропозиція сама не бронює дату? Якщо так, на який строк і що бачить клієнт? Що показувати про доступність: стандартний ліміт чи фактично вільні місця? Що вважаємо моментом надсилання пропозиції і чи можна оплатити рівно через 48 годин? Якщо оплата пройшла, а місця вже немає, що робимо, коли клієнт не погоджується на іншу дату, і хто це узгоджує? Спільне питання GK-001: 7 тортів чи 7 замовлень на день. Це потрібно, щоб оплата не створювала нездійсненної обіцянки.

3. proof_payment — Які докази достатні для прив'язки старого замовлення до перевіреного клієнта, хто перевіряє і що робити, якщо доказів недостатньо? Instagram сам по собі не підтверджує особу. Який безпечний доказ Revolut-оплати/повернення бачить перевіряльник, хто має доступ і хто може підтвердити його? Якщо клієнт уже платить онлайн, а також повідомив про переказ, які дії оператора погоджуємо до з'ясування результату обох оплат? Чи всі чотири контакти обов'язкові для кожного типу замовлення; як замовити без Instagram? Чи історія/повторення замовлення дає окремий дозвіл показувати його фото публічно, чи потрібна окрема згода? Це потрібно для прав доступу й повернень без подвійного обліку. Банківські та особисті документи сюди не додавайте.

4. refund_examples — Потрібно погодити залишок після невдалого повернення, округлення частки пенні й розподіл між кількома позиціями. Нижче — пропозиція Саші для перевірки, а не вже погоджене правило. Хто забезпечить фахову перевірку пропорційності утримання 50%, умов 4 днів і 100% передоплати, включно з великими/весільними замовленнями, перед фінальними Terms? Узгоджуємо це разом із GK-005.

Developer examples for owner review (derived from the supplied amounts; NOT new approved policy):
- Owner's £70 obligation: £20 succeeded and £50 pending leaves £50 still owed, but £0 available for an additional refund while that £50 is pending. If it fails, £50 remains owed and can be retried after failure is verified.
- Where a full return of £120 is owed: after £20 succeeded, the total remaining obligation is £100. With £50 pending, only £50 is available for an additional payout. Pending success leaves £50 owed; pending failure leaves £100 owed. The full-return target stays £120 in either case. These cases do not create a right to a full refund where policy does not provide one.

Developer completion work:
- AC1: produce the versioned customer/admin action and refund matrix for paid-confirmed, in-production, ready, completed, expired, cancelled and unpaid states, including offline orders, exception authority, no-show and delivery precedence. Preserve ready-cake conflict until explicitly resolved.
- AC2: review the examples above, then add approved fractional-penny/component-allocation cases with GK-005. Preserve full-return target separately from succeeded/pending amounts; no second payout for the same pending obligation.
- AC3: specify verified-identity + order-proof claim/rejection; required/conditional contact validation; reviewed request → quote → order → successful payment path, expired quotes, capacity races and online-pending/offline-recording arbitration. Owner chooses business outcomes; Sasha supplies technical safeguards. Reorders require current price, availability and content confirmation.
- AC4: record exact approved policy/example revision and appropriate review here. No application implementation is required to close this Decision ticket.

Durable local packet: docs/creator/GK-003/{README.md,source-comments.json,decisions.json,follow-up.md,follow-up.json}; question suffixes map to BO-GK-003.question.<suffix>. Original wording and source decision IDs remain in decisions.json. SQL/admin consequences: docs/design/owner-data-contract.md. These local files are unpushed; share the exact packet before future work. This gap record is not owner approval or a replacement for the published description.
