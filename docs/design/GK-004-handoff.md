# GK-004 — approved package and proposed Trello update

Final acceptance and publication completed on 2026-10-05. GK-004 was moved to
**Done** and read back successfully. The final record supersedes the historical
r3 package below; see [final publication receipt](#final-publication-receipt--2026-10-05).
Sections describing pending acceptance/publication are preserved as the prepared
proposal that Sasha subsequently accepted, not outstanding actions.

Prepared and published 2026-10-05 with Sasha's authorization. Source card:
[GK-004](https://trello.com/c/RotJkR9L). Owner contribution:
[BO-GK-004](https://trello.com/c/ixJiQtHm).

## Published comment on GK-004 (split into four parts)

The following is the historical r3 publication, not the current provider selection.
See the unpublished amendment at the end for PostHog and subsequent decisions.

Sasha completed the provider/budget interview and confirmed on 2026-10-05:
“business owner approved this package”. This records owner approval relayed by
Sasha; it does not claim a direct owner comment or independent technical review.

Approved package: Railway Pro; Supabase Pro production plus a separate Free test
organisation/project; Backblaze B2 recovery storage; UptimeRobot Solo; Umami Cloud
Free; Resend Free; Stripe UK hosted Checkout; Cloudflare Registrar/DNS for requested
gkcake.com, subject to availability. Existing Gmail remains the reply mailbox.

The rounded planning allowances total £60.52/month averaged; approved baseline
£61, hard ceiling £100. Domain is paid annually: ordinary months approximately
£59.72, first/domain-payment month £69.23, year £726.11 before separate payment
processing fees. Tax/FX are conservative estimates, not a fixed invoice guarantee.
Usage sizing and checkout quotes must remain within the approved limits.

An increase greater than £10/month cumulatively above the last approved baseline
needs owner approval; initially this means more than £71. Give advance alerts and
request an increase before needed. Without approval, stop test services first,
then affected live services, preserving orders/backups. Approval resets baseline;
the £100 ceiling remains. Reserve continuing subscription/data-retention charges.

Sasha administers accounts and responds to incidents; the owner pays and receives
emergency recovery access. Support: every day 10:00–23:00 Europe/London, phone
push, three support-hour routine recovery, clock paused overnight. A full provider
outage may take longer provided every confirmed order remains recoverable. Zero
confirmed-order loss; at most 24 hours of nonessential recent photo uploads lost.
Daily database backups alone are insufficient: proposed independent durable order
records and separate photo copies require GK-019 reconciliation and later testing.

Analytics: optional opt-in, six-month detailed history, Umami's separate dashboard;
visits/pages, sources, checkout journeys and returning estimates are not exact
people counts. Admin order/revenue charts use our database. Umami Free does not
provide the paid API for private admin charts or automated Umami-metric alerts.
UptimeRobot monitors availability and backend technical metrics instead.

AC evidence: provider-research.md decision r3 supplies the dated selection, cost
basis, responsibilities, recovery tolerances, analytics choices and documentation
compatibility matrix. ADR-0002 r2 and SETUP.md now match the approved package.
Fingerprints below identify the exact local output; files are not yet Git-published.

Remaining technical acceptance: confirm Umami EU/deletion/backup-expiry semantics;
document remaining processing regions and size runtime/recovery storage; review
feasibility of the proposed zero-order-loss approach. Detailed reconciliation is
downstream GK-019 work, not a circular prerequisite requiring GK-019 completion.
Phone delivery, account recovery and restore exercises
belong to later setup/verification tickets. Do not enable analytics before its
data-handling check or checkout before its recovery safeguards. These are explicit
gates, not new unanswered business-budget questions. Keep GK-004 In Progress until
technical acceptance and the required revision-linked Trello evidence are complete.
No registration, purchase, application implementation or release was performed.

## Короткий підсумок для власниці

Саша повідомив 5 жовтня 2026 року, що власниця бізнесу схвалила пакет.
Погоджений бюджет — **£61 на місяць**, максимальна межа — **£100**.
Наведені суми — оцінка з резервом на податки та конвертацію валют; фактичне
споживання й рахунки потрібно перевірити перед підключенням.

| Послуга | Орієнтовно на місяць |
| --- | ---: |
| Railway Pro — робота сайту, сервера й фонових завдань | £22.73 |
| Supabase Pro — база даних, облікові записи та фото | £22.73 |
| Окрема тестова база Supabase Free | £0 |
| UptimeRobot Solo — сповіщення про збої | £13.27 |
| Backblaze B2 — незалежні резервні копії | £1.00 |
| Umami Free, Resend Free, наявна пошта Gmail | £0 |
| Домен gkcake.com — середня місячна частка річної оплати | £0.79 |
| Разом за округленими сумами | **£60.52** |

Домен оплачується раз на рік: приблизно £9.51. Тому місяць його придбання або
продовження коштуватиме приблизно £69.23, звичайний місяць — £59.72. Домен ще
не придбано. Комісії Stripe за платежі оплачуються окремо; для стандартної
британської картки поточний тариф — 1.5% + £0.20 за платіж, інші картки дорожчі.

Саша керує технічними обліковими записами, власниця оплачує рахунки та має
аварійний доступ для відновлення. Сукупне збільшення місячного бюджету більш ніж
на £10 потребує нового погодження. Сповіщення й запит на збільшення надходять
завчасно; без погодження спочатку зупиняються тестові, потім за потреби робочі
сервіси. Замовлення та резервні копії зберігаються.

Саша реагує на збої щодня з 10:00 до 23:00 за лондонським часом. Звичайне
відновлення — до трьох годин у цьому проміжку: збій о 22:00 дає орієнтир до
12:00 наступного дня. Масштабний збій провайдера може тривати довше, але всі
підтверджені замовлення мають залишитися відновлюваними. Допускається втрата
до 24 годин нових фото, якщо це не заважає виконати підтверджене замовлення.

Аналітика працює лише за згодою відвідувача, з шестимісячною історією та окремою
панеллю Umami. Перед увімкненням ще потрібно технічно перевірити фактичне
видалення даних. Перевірки відновлення й телефонних сповіщень виконуються під
час подальшого налаштування. Погодження пакета не означає, що сервіси вже працюють.

## Output revision fingerprints

The following SHA-256 values identify the accompanying outputs. The owner approved the
package described in the interview, not an assertion that every technical gate
has already passed. Sasha authorized publication on 2026-10-05. Fingerprints below identify the published snapshot, not subsequent working revisions.

| Output | SHA-256 |
| --- | --- |
| docs/design/provider-research.md | `e00e98aa57f36b927c6c0492aaf723a2073efab9f3558ba9ddfd0ed1f6a7d9b0` |
| docs/design/adr/0002-providers.md | `990ed4876d7cc3ab7bfecc54361bbc8556a8a3f16e26ca18c0e33d2e1fc944a3` |
| SETUP.md | `3b1a393f705305cdeb6d2e753ed747bd0c66618f83a3cee233047ef10833991d` |

## Document verification

Passed: cost arithmetic, local link targets, trailing-whitespace check, preservation
of historical research and review of the scoped ADR/SETUP changes. No software tests apply to this documentation
change. No integration, device, account recovery or restore test is claimed.

## Publication receipt

Read back all four comment IDs after publishing; card status unchanged.
The initial oversized request returned HTTP 414 and created no comment; the
approved text was split into four parts without changing its decisions.

- [6ac2fdfdc3728e5132239edb](https://trello.com/c/RotJkR9L#comment-6ac2fdfdc3728e5132239edb) — 2026-10-05T01:31:41.330Z
- [6ac2fdfd7fd0aedd76cedabf](https://trello.com/c/RotJkR9L#comment-6ac2fdfd7fd0aedd76cedabf) — 2026-10-05T01:31:41.975Z
- [6ac2fdfe293d093f2f55b1a1](https://trello.com/c/RotJkR9L#comment-6ac2fdfe293d093f2f55b1a1) — 2026-10-05T01:31:42.557Z
- [6ac2fdfff96152b6ba3248a0](https://trello.com/c/RotJkR9L#comment-6ac2fdfff96152b6ba3248a0) — 2026-10-05T01:31:43.221Z

## Current amendment — r13 decisions, not published

Sasha subsequently selected PostHog Cloud EU instead of Umami and explicitly
accepted PostHog's standard retention policy. Start on Free within 1M analytics
events/month; its one-year retention commitment is not an exact deletion deadline.
The earlier six-month active analytics expiry and fixed 30-day analytics backup
tail are superseded. No Umami inquiry needs sending; none was sent. No new paid
analytics service or account activation has been authorized.

Visitor analytics must be ready before launch. Consent, separate dashboard and
the agreed visits/sources/checkout/returning-visitor measurements remain required.
Session replay remains out of scope. Verify EU configuration and SDK behaviour,
including opt-out/withdrawal, route events and payment-confirmation attribution,
in the later implementation/setup tickets. Documentation compatibility is not an
executed integration test.

Forecast now includes 20–30k visits/month, 300–400 launch photos and approximately
100 new photos/month. Photos only at launch; account for planned video support
before enabling it. Photo-size/page-depth assumptions remain estimates. The
GBP61 baseline and GBP100 ceiling remain unchanged; PostHog's Free quota gives
more forecast event headroom. Existing advance budget reviews still apply.

Sasha accepts ancillary US processing by the email/monitoring providers with
documented safeguards and minimal shared data. Sasha also accepted 30 days of
independent daily shop restore points and recoverable photo versions; this is
separate from PostHog retention. Expiry must preserve current recovery copies of
retained confirmed orders and essential attachments.

Sasha approved the consolidated recovery approach: independent confirmed-order
and confirmed-change records with essential attachments; pause checkout if the
safeguards are unavailable; restore DB/object records and reconcile payments and
pending emails before reopening. Decision-level self-review found this conditionally
feasible; implementation and restore evidence remain downstream requirements.

Current maintained sources: provider-research decision r13, ADR-0002 r10 and SETUP.
The hashes above continue to identify only the previously published r3 content.
No direct owner approval of this later amendment or independent review is claimed.
AC2's consolidated recovery approach is now approved. Finish final Creator
acceptance/publication evidence under AC4 and explicit acceptance of the AC1 quote
exception described below. Checkout invoices,
measured runtime/backup usage and live restore/alert/consent tests remain later
setup/implementation checks, not completed work. Trello status is unchanged.

## Final closure proposal — awaiting Creator acceptance and publication

Live GK-004 was read on 2026-10-05: still In Progress, AC1–AC4 unchanged.
No unanswered provider, analytics or recovery-policy question remains. The proposed
decision-ticket completion condition is Creator acceptance of this exact packet
and the AC1 estimate exception, followed by revision-linked Trello publication and
Done transition. This does not close or promote dependent tickets automatically.

- AC1: named package, locations, account/payment ownership and dated price basis
  are documented. Accept planning allowances in place of an exact all-in usage
  invoice: GBP61/month baseline, GBP100 ceiling, current annual-domain month about
  GBP69.23, payment fees separate. Actual supplier tax/FX, domain availability,
  runtime/egress and backup sizing must be validated before paid activation;
  approval thresholds and spending stops still apply. This is an explicit proposed
  exception permitted by AC1, not proof of a fixed invoice or permission to spend.
- AC2: Sasha accepted the consolidated database/object/order recovery approach,
  zero confirmed-order loss target, 24-hour nonessential-upload tolerance, 30-day
  operational recovery window and three support-hour routine recovery target.
  Support is daily 10:00–23:00 Europe/London; major provider outages may take longer
  while retained confirmed orders remain recoverable. Detailed protocol/tests are
  downstream gates; no operational guarantee or successful restore is asserted.
- AC3: PostHog Cloud EU Free, opt-in tracking, separate dashboard, required traffic/
  sources/checkout/returning-visitor measurements and standard provider retention
  are accepted. Analytics must be ready before launch. No replay or paid upgrade.
- AC4: exact source fingerprints below identify this locally prepared packet.
  Record final Creator acceptance and these revisions in Trello before Done.
  This is a Creator decision ticket, with self-review disclosed; no application PR,
  independent review, account registration, production test or deployment is claimed.

All other approved providers remain Railway Pro, Supabase Pro plus separate Free
test organisation, B2 EU recovery storage, UptimeRobot Solo, Resend Free, Stripe UK
Checkout and Cloudflare domain/DNS for requested gkcake.com. SvelteKit/Svelte with
TypeScript/HTML/CSS remains the UI stack. Business-owner package approval was
relayed by Sasha earlier; later technical amendments were approved by Sasha and
must not be misrepresented as a new direct owner statement.

### Final packet fingerprints — prepared 2026-10-05

| Maintained source | SHA-256 |
| --- | --- |
| docs/design/provider-research.md | `d8952a76a14c191a0a8e5714c7d6c14c791aed9dd62cb8c7e4ce32730f6e0d5d` |
| docs/design/adr/0002-providers.md | `7693ff208e027f9068bfcd97da72b9c387e334fdc8e74e0ff449d6e3b033a66c` |
| SETUP.md | `c673d6f0015e8804c4e51f49fbfcb5e5bcbcaa3d72302e20b7ed7abce9d83fe4` |
| docs/design/adr/0001-stack.md | `22239ac9b4a8b9383555e2ff4f8430575447fb1df1cb14b4e3e3dd418bcf2615` |

Verified: allowance arithmetic, local Markdown targets, revision references and
whitespace. Live AC1–AC4 checked against GK-004. No integration or restore tests
were run; they are explicitly required in the downstream work. Publication of
this final packet and Done transition have not yet occurred.

## Final publication receipt — 2026-10-05

Sasha explicitly answered “Yes” to accepting the final packet and AC1 price-estimate
exception, publishing the final record and marking GK-004 Done. Final publication
used the source SHA-256 values above, rechecked against the actual file bytes before
writing. The approved maintained sources were not changed during publication.

Live preflight verified card `6ab13b55ae5ee4104632307d` on board
`6ab074a2b3c3763832ddf623`, its In Progress state, four historical comments and the
current Done list. Published the final accepted record in three bounded comments:

- [Final record part 1/3](https://trello.com/c/RotJkR9L#comment-6ac3b768372e3acd20f0e81b) — 2026-10-05T14:42:48.914Z.
- [Final record part 2/3](https://trello.com/c/RotJkR9L#comment-6ac3b7692481acc4a3473a7a) — 2026-10-05T14:42:49.488Z.
- [Final record part 3/3](https://trello.com/c/RotJkR9L#comment-6ac3b769d1e6fb6fc37e3a38) — 2026-10-05T14:42:49.990Z.

All three comment bodies were read back and matched the prepared text exactly.
Then moved the card to the verified **Done** list `6ab074aeb3c3763832de00c3`.
A fresh card read confirmed the expected board, Done list and non-archived card.
This records completed decision-ticket acceptance, not application delivery.

No dependent ticket was changed. No provider account, purchase, external email,
application implementation, deployment or production test was performed. Detailed
recovery reconciliation, real usage/checkout verification, consent integration,
phone alerts and restore exercises remain the documented downstream gates.
