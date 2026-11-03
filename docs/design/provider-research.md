# Provider and architecture evidence

## GK-004 decision record r13 — 2026-10-05

Status: package approved by Sasha and business-owner approval reported by Sasha
on 2026-10-05: “business owner approved this package”. Listed technical checks
remain pending. The r3 summary was published to Trello with Sasha's authorization;
r4 added processing-region evidence; r5 added the requested Railway/Vercel research;
r6 recorded Sasha's explicit acceptance of ancillary US processing; r7 adds his
photo/traffic forecast and a conditional sizing assessment; r8 records acceptance
of the 30-day recovery window; r9 records visitor analytics as a launch requirement.
r10 records acceptance of a bounded analytics backup residual period and prepares
the outstanding provider inquiry; no message has been sent.
r11 records Sasha selecting PostHog Cloud EU instead of Umami Cloud. The provider
choice was accepted with a retention conflict. In r12 Sasha explicitly accepts
PostHog's standard retention policy, superseding the bespoke analytics expiry limits.
r13 records Sasha's explicit approval of the consolidated recovery approach and
prepares final Creator acceptance/publication, including the AC1 estimate exception.
This is a decision
artifact, not a second ticket-status tracker. [GK-004](https://trello.com/c/RotJkR9L)
was read on 2026-10-05 and was In Progress; publishing comments did not change its
status. No account creation, purchase, implementation or deployment was performed.
The historical research below is superseded where this section differs.

Input: this task's user replies through business-owner package approval, including
advance spending alerts, approval requests and service pausing without approval.
This is relayed owner acceptance, not a direct owner comment or independent review.
Repository HEAD: `bb96a89681bcf2fa1c663a02eacbe039e20e6363`; planning files are
uncommitted, so HEAD alone does not identify this draft. No independent review
is claimed. Creator-owned GK-004 does not require the Agent model gate.

### Selections and cost basis

Sasha explicitly selected Railway Pro, Supabase Pro, UptimeRobot Solo, Resend Free
and Stripe hosted Checkout, and now PostHog Cloud EU instead of Umami Cloud Free.
Use PostHog's Free plan initially with its accepted standard retention policy.
He accepted the complete
approximately GBP61/month proposal including the domain and backup allowance.
Sasha subsequently confirmed the business owner's approval of this package.

| Capability | Selection / proposed location | Monthly GBP allowance |
| --- | --- | ---: |
| Separately deployed UI, Go API, jobs and test hosting | Railway Pro; Amsterdam; USD25 usage allowance (USD20 plan minimum includes usage) | 22.73 |
| Production PostgreSQL, Auth and photos | Supabase Pro Micro; specific London region; USD25 | 22.73 |
| Separate test database | Supabase Free in a separate free organisation; synthetic data; London proposed | 0 |
| Availability and technical alerts | UptimeRobot Solo, 10 monitors; EUR13 monthly; phone push | 13.27 |
| Independent database/photo/order recovery storage | Backblaze B2 EU Central, Amsterdam; GBP1 planning allowance | 1.00 |
| Visitor analytics in separate dashboard | PostHog Cloud EU, Frankfurt; Free initially, 1M analytics events/month; standard retention accepted | 0 |
| Authentication and order email | Resend Free; existing Gmail mailbox for replies | 0 |
| Domain and DNS | Cloudflare Registrar, `gkcake.com`; USD10.46/year observed quote | 0.79 |
| Total of individually rounded allowances | Payment processing excluded | **60.52** |

FX basis: [ECB 2026-10-02](https://www.ecb.europa.eu/stats/shared/pdf/eurofxref.pdf),
EUR1 = USD1.1225 = GBP0.85033. Add a conservative 20% supplier-tax allowance;
this is not an account-specific tax determination or fixed GBP invoice quote.
Unrounded conversion gives GBP60.5095/month averaged, GBP59.7171 in an ordinary
month, GBP69.2256 in a domain-payment month, and GBP726.1136/year. Card FX fees,
future prices and actual usage can change the bill. Domain purchase is annual,
not a GBP0.79 monthly payment; availability is not reserved.

Railway's USD25 is an allowance, not a measured workload quote or plan fee on
top of usage. It includes the proposed test runtime and jobs; measure it before
launch. Supabase Free may pause after inactivity and is not production-equivalent.
Backblaze backup volume, versions, retention and restore egress must be sized;
GBP1 is not a guarantee. No PITR add-on, paid mailbox, paid analytics or recurring
voice-call credits are included. No paid service has been activated.

Pricing evidence checked during the interview:
[Railway](https://railway.com/pricing), [Supabase](https://supabase.com/pricing),
[separate free projects](https://supabase.com/docs/guides/platform/billing-on-supabase),
[UptimeRobot](https://uptimerobot.com/pricing/),
[B2](https://www.backblaze.com/cloud-storage/pricing),
[PostHog](https://posthog.com/pricing), [Resend](https://resend.com/pricing),
[domain search](https://www.cloudflare.com/domains/search?q=gkcake.com).
Rendered UptimeRobot quote was EUR13 on monthly billing; the static web extract
showed different USD pricing, so retain the observed EUR quote pending checkout.

[Stripe UK](https://stripe.com/gb/pricing): standard UK cards 1.5% + GBP0.20;
European cards 2.5% + GBP0.20; other card types/FX differ. Fees are separate from
the infrastructure ceiling. No average order value/card mix was supplied, so no
monthly processing total is asserted. Merchant verification and payout details
belong to the business owner; technical integration belongs to Sasha.

### Confirmed operating decisions

- Forecast: 60 orders/month at launch, 80/month during the first year;
  20,000–30,000 website visits/month. Treat visits as sessions for sizing, not
  verified distinct people or measured traffic.
- Approximately 300–400 cake photos at launch, adding about 100/month. Photos
  only at launch, with video support planned for later. Retain this future need;
  video volume, delivery and cost must be assessed before enabling it.
- Budget baseline GBP61/month; hard ceiling GBP100/month. Minimise spend rather
  than treating the ceiling as a target. Increases greater than GBP10/month,
  cumulatively against the last approved baseline, need business-owner approval.
  At the initial baseline, more than GBP71 needs approval; exactly GBP71 does not
  cross that threshold. An approved increase resets the comparison baseline.
- Send advance alerts and request an increase before the permitted limit is
  reached. If approval has not arrived, stop test services first, then affected
  live services as necessary. Preserve orders and backups. Never interpret this
  as permission to delete stored data or to increase the GBP100 ceiling.
- Translate the aggregate GBP budget into provider limits with tax/FX headroom.
  Alerts alone do not enforce it. Pausing compute does not stop every storage or
  subscription charge; reserve continuing data-retention costs. Exact provider
  thresholds and enforcement latency remain implementation/configuration checks.
- Sasha owns/administers infrastructure service accounts; business owner pays
  and must have emergency recovery access using supported provider mechanisms.
- Customer/order hosting should be UK/nearby Europe, not necessarily UK-only.
- Sasha explicitly accepts retaining the email and monitoring providers with
  documented safeguards and minimal shared data, including the explained US
  processing. This does not establish UK/EU-only processing or waive the agreed
  analytics deletion requirement.
- No existing domain or paid service accounts. Desired domain `gkcake.com`;
  existing business mailbox `gk.cake.london@gmail.com`. Proposed sender
  `orders@gkcake.com`, Reply-To the existing mailbox; no new paid mailbox.
- Resend Free initially: 3,000 emails/month and 100/day, shared by authentication
  and business emails; usage alerts and review before reaching limits.

### Photo and traffic sizing — 2026-10-05

Use the upper forecast of 400 initial photos and 30,000 monthly visits for
planning. After twelve monthly additions, approximately 1,600 photos remain if
none are removed. Counts are confirmed estimates; byte sizes, page depth, consent
rate, bursts and customer attachment volume have not been measured.

- Illustrative storage assumption: 5 MB original plus 1 MB total web variants per
  photo gives 2.4 GB at launch and 9.6 GB after twelve additions. This is a sizing
  assumption, not an upload restriction or measured image size. Private customer
  uploads, old versions, database data and backups are additional.
- Illustrative delivery assumption: 20 loaded images per visit averaging 200 KB
  each gives 80–120 GB/month of image transfer. At 10 MB per visit, it becomes
  200–300 GB/month. Use responsive compressed images, lazy loading and caching;
  avoid serving full-resolution originals for normal browsing. Measure realistic
  galleries before launch, including mobile quality and peak traffic.
- [Supabase Pro pricing](https://supabase.com/pricing), checked 2026-10-05,
  includes 100 GB file storage, 250 GB uncached egress and a separate 250 GB cached
  egress allowance. These egress pools are not interchangeable. Database/auth,
  exports and other traffic also consume allowances; the illustration is not a
  capacity or invoice guarantee. Direct image delivery avoids passing those bytes
  through the Railway API. The USD25 Railway allowance still needs runtime testing.
- Historical Umami sizing, superseded by PostHog selection below:
  [Umami pricing](https://umami.is/pricing), checked 2026-10-05, calls the free
  tier Hobby and includes 100,000 monthly events. Pageviews, custom events and
  stored event properties count. At an assumed four pages/visit, the forecast is
  80,000–120,000 pageviews before consent filtering; a hypothetical 50% opt-in
  rate gives 40,000–60,000 tracked pageviews, plus custom events/properties.
  Neither page depth nor consent rate is a forecast. At that point Free was selected, but
  it cannot be promised to cover this traffic. Review usage and projected totals
  early under the already agreed advance-alert and increase-approval policy.
  Free has no selected API integration; monitoring its quota initially requires
  dashboard review. Verify Free-specific limit behaviour during setup rather than
  assuming the generic paid-overage FAQ applies. Do not silently sample journeys
  or drop required events to fit the allowance.
- B2's GBP1 allowance remains provisional until database export size, retained
  versions and recovery traffic are known. Store unchanged photo bytes once with
  version/manifests for recovery rather than thirty full duplicate photo sets.
  Future videos are not included in these photo-only byte estimates; no additional
  video service or paid analytics subscription is selected by this assessment.

The approved GBP61 baseline is unchanged; this is a conditional budget assessment,
not a new fixed quote or permission to exceed approved limits.

### Recovery and incident contract

- Sasha is the incident responder, alerted by phone notification or call.
- Support daily including weekends, 10:00–23:00 Europe/London. Routine recovery
  target: three support hours; the clock pauses outside that window. Example:
  an outage at 22:00 has a noon-next-day target if unresolved overnight.
- A complete provider/region outage may take longer, provided every confirmed
  order remains recoverable. Zero loss includes complete order/price/fulfilment
  information, not merely payment evidence.
- Up to 24 hours of recent photo uploads may be lost. This exception must not
  remove information essential to fulfilling a confirmed order.
- Pause new checkout/confirmation when required durable order or recovery
  safeguards are unavailable. Browsing may remain available.
- Approved recovery approach: daily DB backups plus independent exports, separate
  photo-byte copies at intervals meeting the 24-hour objective, and durable
  independent recovery records before orders and subsequent changes are treated
  as confirmed. Preserve IDs and versions; reconcile payment/email outcomes on
  restore before resuming side effects. Never make external calls under DB locks.
- The cross-provider order protocol, retention implementation, key recovery and restore
  procedure require GK-019 design reconciliation and later implementation tests.
  Supabase daily DB backups exclude object bytes and do not meet zero-order-loss
  requirements by themselves. No zero-loss implementation or restore test exists.
- Sasha accepted a rolling 30-day window of independent daily
  DB restore points and recoverable photo versions for accidental deletion or
  corruption: “im comfortable with 30 days”. Expiring an old restore point must not remove
  the current independent recovery copy of a retained confirmed order or essential
  attachment. This operational recovery window does not set business-record or
  customer-data retention; those policies remain separate.

### Analytics and integration boundaries

Sasha selected PostHog Cloud EU after reviewing alternatives, citing insufficient
clarity in Umami's retention/backup policy. This supersedes Umami Cloud selection
and the need to send its prepared inquiry. No inquiry has been sent. The existing
separate analytics dashboard arrangement remains the initial scope; selecting a
provider with an API does not itself request new embedded admin charts.
Actual order/revenue charts use
the application database. Analytics covers visits/pages, sources, checkout
start/completion and returning-visitor estimates; optional tracking needs opt-in.
Declining visitors are absent from tracked journeys. Visits/sessions are not
distinct people; cross-visit recognition needs an appropriately consented opaque
identifier and must not expose customer names, email, order details or URLs with
secrets. No session recording or public dashboard sharing is selected.

PostHog Free: 1M analytics events/month, one project, unlimited team members,
community support and one-year advertised event retention. The pricing FAQ says
events/metadata are guaranteed to be retained for one year on Free and may be
deleted after that; it does not promise deletion exactly at one year. Paid plans
advertise seven years. Official data-storage documentation says plan retention
cannot be shortened as a deletion mechanism. Sources: [pricing](https://posthog.com/pricing),
[official documentation source](https://github.com/PostHog/posthog.com/blob/master/contents/docs/privacy/data-storage.mdx).
Sasha explicitly instructed: “Use PostHog’s standard retention policy”. This
supersedes the previous six-month active deletion requirement and maximum 30-day
analytics backup tail. One year is a retention commitment, not a guaranteed purge
deadline; no fixed backup expiry is asserted. Record the applicable standard terms
and account plan at setup and describe them accurately in the privacy information.
This acceptance is separate from shop/order backup retention and does not authorize
a paid-plan change; review the different paid-plan lifecycle before any upgrade.

The 80–120k illustrative monthly pageviews plus deliberately limited business
events have more headroom under 1M events, but are not measured usage. Free drops
additional events after the limit; review forecast usage early under the existing
approval policy. Initial analytics-only setup should keep session replay and
unnecessary autocapture disabled and collect no customer names/email/order contents.
Verify consent configuration before enabling tracking. The GBP0 analytics allowance
and GBP61 package baseline are unchanged
while within Free; paid upgrades remain subject to the agreed spending rule.

Sasha requires visitor analytics to be ready before launch and rejected launching
with analytics disabled while provider retention remains unresolved. Resolve the
selected provider's handling or agree a suitable alternative before launch;
neither a provider change nor additional spending is automatically authorized.
The launch check must cover consent gating and the agreed visit/source/checkout
and returning-visitor measurements, alongside capacity and retention evidence.

Historical policy, now superseded for analytics only: six-month active deletion
plus at most 30 further days in isolated backups. The separately accepted shop
policy of 30 days of recovery points remains in force.

### Historical Umami inquiry — superseded, not sent

Superseded by Sasha's PostHog Cloud EU selection. Retained as research history;
Sasha does not need to send this inquiry for the newly selected package.

Rechecked the [DPA](https://umami.is/dpa) on 2026-10-05: section 8.3 still specifies
ordinary backup cycles without a numerical expiry bound. The current
[Cloud FAQ](https://docs.umami.is/docs/cloud/faq) does not settle Free-specific
region selection or over-limit behaviour. Obtain a dated written response or
equivalent official documentation before treating these facts as verified.
Contact: privacy@umami.is (DPA contact observed in the rendered site during this
interview); ask for routing of plan/region questions to product support as needed.

Subject: Umami Cloud Hobby — retention, EU hosting and event-limit confirmation

> Hello, we are preparing a UK online shop and considering Umami Cloud Hobby
> (Free), with consent-based analytics. Could you confirm:
>
> 1. Does the six-month retention limit automatically delete detailed event,
>    session and visitor-identifier data from active systems, rather than merely
>    hide it from reports? What is the deletion schedule?
> 2. Are residual copies removed from backups/archives within at most 30 further
>    days, isolated from routine use, with expired-data deletion reapplied before
>    restored data is used? Please identify any exceptions and their scope.
> 3. Can a new Hobby account select EU hosting, and where are analytics data and
>    backups stored? Please distinguish primary storage from ancillary processing.
> 4. What exactly happens on Hobby when 100,000 monthly events are reached: are
>    events rejected, paused, retained or charged? Which advance usage alerts are
>    available without API access or a paid plan?
>
> Please link the applicable documentation or confirm in writing. If another
> team handles the hosting or quota questions, please route those questions to it.

No external communication is authorized or performed by preparing this text.
If the response fails the accepted requirements, compare alternatives and obtain
any changed provider/budget acceptance rather than weakening the requirements.

### Documented integration checks

Documented compatibility, not executed integration tests:

| Connection | Finding / required follow-through |
| --- | --- |
| Railway to Supabase | Opt-in outbound IPv6 supports the default direct DB connection; IPv4 session pooler is an alternative. Verify TLS and connection limits. [Railway](https://docs.railway.com/networking/outbound-networking), [Supabase](https://supabase.com/docs/guides/troubleshooting/supabase--your-network-ipv4-and-ipv6-compatibility-cHe3BP) |
| Supabase Auth to Resend | Official SMTP integration; configure sender/DNS, rate limits and redirects separately for test/production. [Integration](https://supabase.com/partners/resend) |
| Stripe/Resend to API | Signed HTTPS webhooks; retries, duplicates and unknown outcomes must be handled. Resend Free webhook allowance must cover environment design. [Stripe](https://docs.stripe.com/webhooks), [Resend](https://resend.com/docs/webhooks/introduction) |
| Website/API to PostHog EU | Official browser JavaScript and Go SDKs are available. Consent defaults can disable capture; verify EU endpoints, client-only initialization in SvelteKit, route events, withdrawal and queued retries, returning-visitor identifiers and checkout attribution in the pinned SDK version. Do not infer payment success from a return-page view or bypass consent with server events. Documentation compatibility only; no test executed. [Browser SDK source](https://github.com/PostHog/posthog.com/blob/master/contents/docs/libraries/js/index.mdx), [Go SDK](https://posthog.com/docs/libraries/go) |
| API to B2 | S3-compatible storage/Object Lock available; independent recovery records and restoring remain custom implementation. [B2](https://www.backblaze.com/docs/en/cloud-storage-get-started-with-a-backblaze-integration) |
| UptimeRobot to API/phone | Authenticated JSON assertions support technical thresholds; native app push supported. Proposed backend metrics: backup age, recovery failures, overdue money/email jobs and errors. [Assertions](https://help.uptimerobot.com/en/articles/13628553-uptimerobot-api-monitoring), [phone setup](https://help.uptimerobot.com/en/articles/11360953-uptimerobot-personal-notification-channels-setup-guide) |

UptimeRobot documents [recurring notifications on paid plans](https://help.uptimerobot.com/en/articles/11361289-recurring-postponed-notifications-in-uptimerobot).
Support hours govern response obligations and the recovery clock. The interview
did not establish a separate prohibition on overnight notifications or require
an alert at exactly 10:00; those were extra constraints in draft r2, not confirmed
user requirements. Proposed setup: continuous monitoring with recurring phone
push for unresolved incidents; verify delivery during support hours and incident
visibility at the start of the next support window in GK-014. Do not use overnight
maintenance windows to hide outages. Exact recurrence/device settings remain
setup details; no live phone test has been performed.

### Further provider checks — 2026-10-05

- Resend Free can send from Ireland, but [stored email content, logs, webhook
  payloads and account records remain in the US](https://resend.com/security/gdpr).
  The sending region does not provide EU storage. Its documentation describes
  30-day email/log retention on Free and contractual transfer mechanisms; this is
  provider evidence, not a project legal-compliance determination.
- [UptimeRobot processing](https://uptimerobot.com/dpa/) is primarily EU with US
  and other subprocessor processing. Supply only health booleans/aggregate technical
  metrics, never customer/order records in monitoring response bodies.
- [Umami Cloud FAQ](https://docs.umami.is/docs/cloud/faq) lists US/EU servers but
  does not establish the Free account's selectable region. The rendered
  [DPA](https://umami.is/dpa), updated August 2, 2026, permits deletion during the
  term and describes backup copies isolated from ordinary use until normal expiry;
  it does not name an exact backup-expiry interval. Its subprocessor list includes
  US/EU providers and does not prove every function stays in the EU. Six months of
  reporting history alone is still insufficient evidence of the requested deletion.
- Sasha explicitly answered “yes, i'm ok” to retaining these providers with
  documented safeguards and minimal shared data rather than requiring UK/EU-only
  processing. Record the applicable provider terms/transfer safeguards during
  setup and keep customer/order data out of monitoring payloads. This acceptance
  does not waive the separate analytics deletion requirement.
- UI capability assessment is recorded in [ADR-0001 r4](adr/0001-stack.md):
  SvelteKit/Svelte with TypeScript, HTML and CSS can support the requested rich,
  actionable and animated interface. No framework change is needed on that basis.

### Acceptance and handoff still required

Decision-level recovery self-review (not independent review or a restore test):
the selected services support the proposed database exports and separate object
copies. Daily backups alone leave an unacceptable confirmed-order gap. Independent
versioned order recovery records must bridge that gap; payment evidence alone
cannot reconstruct cake specifications, fulfilment details or essential attachments.
The plan must also cover confirmations/changes racing with an outage, unavailable
recovery storage, lost account/key access and replay after an older DB restore.
GK-019 must settle that protocol and reconciliation before implementation. Preserve
retained orders independently when old restore points expire. Reopen checkout only
after restoring DB/object records and reconciling payments and pending side effects.
This is a conditionally feasible decision-level approach, not proof of a zero-loss
system or a guaranteed three-hour recovery. Sasha explicitly answered “yes” to
approving the consolidated approach as the GK-004 plan. This approves the decision
and safeguards; it does not assert that implementation/restore tests have passed.

- AC1: Sasha accepted package/baseline and reported business-owner approval.
  Account-specific invoice tax/FX and sized usage remain estimates; emergency
  access mechanisms need setup evidence. Ancillary email/monitoring processing
  locations are documented above and accepted; select and verify the PostHog EU
  project/endpoint during setup, not inference from the shop DB location.
  Final acceptance explicitly requests an exception to an exact all-in usage quote:
  use the dated allowances and approved ceiling, requiring actual checkout/usage
  validation before paid activation. Do not represent this exception as already
  separately accepted or the estimates as a fixed supplier invoice.
- AC2: recovery tolerances and consolidated approach accepted by Sasha after the
  decision-level self-review above. Detailed design/tests go to GK-019 and
  setup/verification dependents; no remaining recovery-policy question is identified.
- AC3: measurement, consent, separate dashboard and PostHog's standard retention
  are accepted. No bespoke analytics deletion deadline remains unresolved.
  Analytics readiness and verified consent/measurement integration remain launch
  requirements, with configuration/testing assigned to later tickets.
- AC4: business-owner approval is recorded as relayed by Sasha. Exact output
  fingerprints and a proposed Trello comment are in the accompanying
  [handoff](GK-004-handoff.md). The r3 summary and owner brief were published and read
  back; final r13 Creator acceptance and revision-linked publication remain outstanding.
  No direct owner comment, independent review or completed integration is claimed.
- ADR-0002 and SETUP now reflect the selected package and conditional verification
  gates. Do not
  silently change published ticket snapshots or mark dependent tickets Ready.
- Registration, production configuration and controlled integration/restore tests
  belong to later tickets. This decision task does not authorize purchases or
  production changes. Only the authorized r3 summary has been published so far.

## Analytics alternatives — research, 2026-10-05

Sasha asked for clearer alternatives such as New Relic and Grafana. This is an
options assessment prepared before the r11 provider change. Sasha subsequently
selected PostHog Cloud EU; the original comparison below is historical rationale,
not the current selection. r12 separately accepts PostHog's standard retention
policy, superseding the six-month constraint used in this historical comparison.
No new spend, account or deployment is authorized. Sources below were checked on
2026-10-05.

Visitor analytics must cover visits/pages, sources, checkout journeys and returning
visitor estimates, with consent and six months of detailed history followed by
active deletion plus at most 30 days in isolated recovery backups. Operational
telemetry and business analytics can share tooling, but short-lived technical
events are not a substitute for the accepted analytics history.

| Candidate | Documented fit and limits | Assessment for GK Cakes |
| --- | --- | --- |
| Grafana Cloud + Faro | Browser performance/errors, custom events and alerts. Free frontend allowance is 50,000 sessions/month; free metrics/logs/traces retention is 14 days. Pro starts at USD19/month plus usage with independently metered services. [Frontend overview](https://grafana.com/docs/grafana-cloud/observe-and-act/monitor-applications/frontend-observability/), [pricing](https://grafana.com/pricing/) | Useful operational platform; 20–30k visits may fit its session allowance, but sessions and visits need not map one-to-one and other quotas apply. Six-month business funnels/visitor history would need additional storage/design or a suitable paid arrangement. Not a direct free replacement. |
| New Relic | Free 100 GB/month ingest, one full-platform user and unlimited basic users. Default browser retention is 8 days; custom events 30 days. Browser events can be queried/charted. [Pricing](https://newrelic.com/pricing), [retention](https://docs.newrelic.com/docs/data-apis/manage-data/manage-data-retention/), [browser events](https://docs.service.newrelic.com/docs/data-apis/understand-data/event-data/events-reported-browser-monitoring/) | Can combine technical monitoring with custom business dashboards, but the free/default history does not meet this requirement. Six-month retention, backup deletion and access costs need a scoped quote/design; no claim of six-month free analytics. |
| PostHog Cloud EU | Managed web/product analytics, funnels and returning-user analysis; EU service in Frankfurt. Free includes 1M analytics events/month and one-year event retention; paid plans advertise seven years. Official documentation says plan retention cannot be shortened as a deletion mechanism. [Pricing](https://posthog.com/pricing), [privacy](https://posthog.com/docs/privacy), [analytics](https://posthog.com/docs/product-analytics), [official data-storage documentation source](https://github.com/PostHog/posthog.com/blob/master/contents/docs/privacy/data-storage.mdx) | Strong managed functional candidate with more event headroom than Umami Free. Not a compliant drop-in under the accepted six-month deletion rule; individual deletion APIs do not by themselves establish complete rolling expiry including backups. No policy change proposed as already accepted. |
| Plausible | Managed traffic/conversions; advanced funnels and API are in Business. It explicitly has no cross-day new-versus-returning visitor segment. [Plan features](https://plausible.io/docs/subscription-plans), [documented limitations](https://plausible.io/when-not-to-use-plausible) | Simpler reporting, but misses an accepted measurement. Do not select it without a scope decision; a live tier/volume quote and retention check would also be needed. |
| Self-hosted Umami | Node application plus PostgreSQL, available as a container; API is available when self-hosted. Default retention is indefinite unless data is deleted. [Installation](https://docs.umami.is/docs/install), [FAQ](https://docs.umami.is/docs/faq), [API](https://docs.umami.is/docs/api) | Clearest candidate for direct control of deletion scheduling and backup expiry while retaining Umami. Requires maintaining another service and validating purge coverage, restored-data deletion, host backup lifetimes, resource isolation, upgrades and consented identifiers. No built-in six-month purge or automatic cost fit is claimed. |

Recommendation under the unchanged requirements: do not adopt New Relic or Grafana
solely to replace visitor analytics. Keep independent uptime alerts in the selected
UptimeRobot package while assessing self-hosted Umami if direct lifecycle control
is worth the maintenance. PostHog is the stronger managed functional alternative,
but its documented retention conflicts with the accepted policy. Self-hosted Umami
is an engineering recommendation for further sizing, not a proven implementation.

Use a separate analytics database/access role and bound its resources so analytics
does not consume shop order capacity; the exact isolation mechanism and added
runtime/database cost remain unselected. Retention must cover events, identifiers,
session data, logs and backups rather than deleting only pageviews. An authenticated
server-side API can support future admin charts; this is not automatic secure iframe
embedding or an already approved dashboard implementation. No numerical extra
monthly charge is asserted without sizing. These alternatives do not silently
change the approved GBP61 baseline or the GBP100 ceiling.

## Railway versus Vercel — detailed assessment, 2026-10-05

Requested by Sasha to assess suitability beyond launch and avoid an early move.
Scope: hosting the accepted SvelteKit UI, Go API and same-code jobs; Supabase,
payments, analytics and other providers remain as selected. This research does
not change the accepted package, publish another Trello update or provision a
benchmark. Creator-owned decision research: no Agent model gate; current project
inputs read, no independent review or measured application performance claimed.

### Recommendation and confidence

Keep Railway Pro for UI/API/jobs and keep primary data in Supabase. Confidence is
moderate: strong documented architecture fit, uncertain measured operating cost
and application performance, and material provider incident history. No identified
capacity constraint implies a move during the stated first-year forecast. Neither
provider offers a promise that a move will never be needed.

The justification is conventional Go/container execution, shared deployment/job
tooling and budget headroom. It is not a claim that Railway is more reliable than
Vercel, that Vercel cannot run Go, or that 80 orders/month requires Pro capacity.
Pro's included workspace seats, support and log history fit the operating/access
requirements. Its advertised availability target is not an end-to-end shop or
three-hour recovery guarantee. [Railway pricing](https://railway.com/pricing).

Vercel is a credible long-term storefront host. Its strongest argument is integrated
frontend delivery, previews and optimisation; the strongest argument against putting
our entire selected stack there is the current Beta status of its Go runtime and
the additional work to validate jobs under function execution limits. Moving only
the UI to Vercel would not cure a Railway API outage preventing checkout.

### Comparison against this project

| Concern | Railway Pro | Vercel Pro | Consequence for GK Cakes |
| --- | --- | --- | --- |
| Rich SvelteKit UI | Node adapter/container, independently deployed service | Dedicated SvelteKit adapter and frontend tooling | Both support the requested UI; animation quality is independent of host |
| Go API | Conventional server/container | Standard net/http server entry points supported; runtime marked Beta | Railway introduces fewer runtime-specific uncertainties for this accepted backend |
| Scheduled payment/email/recovery work | Starts backend image command; exits after completion | Cron invokes HTTP endpoint; inherits function duration limits | Both need durable work records, retries and concurrency protection |
| Public content delivery | Native CDN caching documented | CDN, previews and image optimisation integrated with SvelteKit | Earlier shorthand that only Vercel has CDN benefits is incomplete |
| UK proximity | Amsterdam compute region; London DB is across providers | London function region available | Vercel has a locality advantage; latency must be measured, not guessed |
| Scaling | Resource limits and additional replicas; no rewrite required to add capacity | Automatic function scaling within limits | Neither removes DB connection/capacity bottlenecks |
| Spend controls | Usage alerts and hard compute limit take workloads offline | Alerts/webhook; optional production pause, not instantaneous | Neither enforces the aggregate GBP100 across all suppliers by itself |
| Emergency second administrator | Pro workspace seats included | Additional Owner/Member seat costs USD20/month; Viewer cannot operate deployments | Compare the same recovery-access arrangement, not one seat against two |
| Portability | Standard images, Node server and Go commands | SvelteKit remains portable; adapters/caching and any proprietary services need migration work | Preserve standard interfaces and avoid unnecessary hosted dependencies |

Primary evidence: [Vercel SvelteKit](https://vercel.com/docs/frameworks/full-stack/sveltekit),
[Railway SvelteKit](https://docs.railway.com/guides/sveltekit),
[Vercel Go](https://vercel.com/docs/functions/runtimes/go),
[Railway cron](https://docs.railway.com/cron-jobs),
[Vercel cron management](https://vercel.com/docs/cron-jobs/manage-cron-jobs),
[Railway CDN](https://docs.railway.com/networking/cdn),
[Railway regions](https://docs.railway.com/deployments/regions),
[Vercel regions](https://vercel.com/docs/regions),
[Railway scaling](https://docs.railway.com/deployments/scaling),
[Railway cost control](https://docs.railway.com/pricing/cost-control),
[Vercel spend management](https://vercel.com/docs/spend-management),
[Vercel Pro seats](https://vercel.com/docs/plans/pro-plan).

Concrete implementation implications, not newly implemented features:

- Vercel documents a 4.5 MB function request/response payload limit. Customer phone
  photos should use authorized direct-to-storage uploads with size/content checks,
  rather than relying on a large proxy request. This is sensible on either host.
  [Function limits](https://vercel.com/docs/functions/limitations).
- Vercel cron does not retry failed invocations and can overlap runs. Railway skips
  a scheduled run if the previous one is still active. Neither is a substitute for
  the existing durable job/reconciliation design.
- Do not apply Node/Python Fluid-compute durations or billing assumptions to Go
  without checking that runtime. Current documentation includes extended-duration
  beta features; stale comparisons claiming every function has a tiny fixed timeout
  are not a sound decision basis.
- Keep personalized account/cart/admin/checkout responses out of shared caches.
  Cache public assets deliberately and revalidate price/availability in the API.
  Region placement and caching cannot repair incorrect transactions or lost orders.

### Costs: compare the whole package

Use the already approved FX/tax allowance above to isolate the hosting choice.
Amounts below are approximate monthly averages, not quoted or measured bills.
Keep the USD25 Railway allowance unchanged for the hybrid example; moving UI off
Railway may reduce use, but savings cannot be assumed and Pro retains a USD20 floor.
Keep the existing independently registered domain and all other services unchanged.
No promotional first-year domain credit is counted as a recurring saving.

| Option | Hosting assumption | Approximate whole package/month |
| --- | --- | ---: |
| Selected Railway package | USD25 usage allowance, Pro minimum included | GBP60.52 (approved baseline GBP61) |
| Vercel-only alternative, one operating seat | USD20 base, usage within credit; job/runtime suitability not yet validated | GBP56 floor, not an equivalent validated quote |
| Vercel-only, two full-access seats | USD40 base; same caveats | GBP74 floor |
| Vercel UI + Railway API/jobs, one Vercel operating seat | USD20 + USD25 | GBP79 |
| Same hybrid, owner also has separate full-access Vercel seat | USD40 + USD25 | GBP97 |

Vercel Pro includes one deploying seat and USD20 usage credit; extra owners/members
cost USD20 each. Read-only viewers are free but cannot recover/configure a service.
A documented emergency recovery mechanism may avoid keeping a second active paid
seat; it is not equivalent to free full administrator access and must be verified.
Vercel Hobby is [non-commercial only](https://vercel.com/docs/plans/hobby), so it
does not make the storefront free. The two-seat hybrid would be approximately
GBP106 in the annual domain-payment month, above the current ceiling. These
alternatives have not received a new budget approval.

Railway's resource model currently lists RAM USD10/GB-month, CPU USD20/vCPU-month,
and service egress USD0.05/GB. The following sensitivity calculation uses combined
average resources across UI/API/jobs/test, not a per-service quote:

| Hypothetical aggregate usage | Resource charge before Pro floor | Approximate whole package |
| --- | ---: | ---: |
| 1 GB RAM, 0.10 vCPU, 50 GB egress | USD14.50; billed at USD20 minimum | GBP56 |
| 2 GB RAM, 0.25 vCPU, 100 GB egress | USD30 | GBP65 |
| 4 GB RAM, 0.50 vCPU, 200 GB egress | USD60 | GBP92 |

This is arithmetic, not a forecast. Additional storage/build/other products, data
transfer paths and supplier bills must be included where applicable. Photo bytes
served directly by Supabase consume Supabase allowances, not Railway service
egress. More memory or traffic can require budget approval without requiring a
host migration. [Railway resource pricing](https://docs.railway.com/pricing).

### Comparable projects and first-hand feedback

No independently audited case matching a London cake shop, 60–80 monthly orders,
SvelteKit + Go and this exact budget was found. The relevant evidence is adjacent
architecture, direct operator reports and commerce deployments; none is a benchmark
for GK Cakes. Vendor customer stories are selected marketing evidence. Public
comments are self-selected, sometimes anonymous, and often use older pricing.

| Evidence | What it supports | Limits and implication |
| --- | --- | --- |
| [ShipAid, Railway customer interview, July 2024](https://blog.railway.com/p/shipaid) | Commerce-related application with backend business logic, notifications and third-party integration; operator values deployment simplicity | Vendor-published, different Node stack and no comparable bill/uptime measurements; supports architecture fit, not reliability superiority |
| [Tekla with Vercel and Medusa, October 2023](https://vercel.com/customers/teklas-ecommerce-evolution-harnessing-flexibility-with-vercel-and-medusa) | Established commerce storefront using Vercel with a separate commerce backend | Larger brand, vendor marketing; validates the split pattern, not a need for us to buy both hosts |
| [Vercel's SvelteKit commerce template](https://vercel.com/templates/svelte/sveltekit-commerce) | Direct SvelteKit commerce support | A template using Shopify, not a production case with our Go backend or recovery rules |
| [Thilo Maier's SvelteKit Vercel-to-Railway migration, November 2025](https://maier.tech/posts/it-aint-easy-to-move-a-side-project-off-big-tech) | Named developer found Railway's workflow good, but encountered prerendering/redirect work during migration | A content site, not commerce; demonstrates that an adapter swap does not guarantee a trivial migration. Historical platform limitations may have changed |
| [Small marketplace operator discussion, January–May 2026](https://www.reddit.com/r/devops/comments/1q7qlxu/anyone_running_a_full_production_app_on_railway/) | Closely related Railway backend + Supabase question; commenters report reliability-driven moves and recommend separating the database | Small anonymous sample; do not repeat incident counts as verified uptime or treat the question itself as successful production evidence |
| [Vercel cost experiences, November 2024](https://www.reddit.com/r/nextjs/comments/1gl1u8e/at_what_point_exactly_should_i_be_worried_about/) | Operators report low bills even for substantial commerce traffic and value avoiding premature self-managed infrastructure | Self-reported, old billing, unknown workload; rebuts universal “Vercel is expensive” claims, not a quote for our project |
| [Railway production discussion](https://station.railway.com/questions/feedback-about-railway-in-production-0f64aea5) | Users praise ease/pricing while one reports regional I/O latency and redeployment disruption | Vendor community and anecdotal; positive workflow and operational risk can both be real |

Additional lower-weight evidence: a [Railway + Supabase migration report](https://www.reddit.com/r/replit/comments/1q4ixku/successfully_migrated_from_replit_to_railway/)
was positive about setup, but it is a fresh migration without longitudinal uptime
proof. Do not inherit its unsupported uptime guarantees or connection-pool advice.
An [enterprise Vercel discussion](https://www.reddit.com/r/ExperiencedDevs/comments/1ievfxc/anyone_here_have_actual_experience_with_hosting/)
includes both productivity praise and deployment/support complaints; its proposed
30,000 orders/day and enterprise contracts are not comparable to our costs.

### Reliability evidence and recovery consequences

Railway's own [May 19–20, 2026 incident report](https://blog.railway.com/p/incident-report-may-19-2026-gcp-account-outage)
describes approximately eight hours of platform-wide disruption after an upstream
account suspension. A routing/control-plane dependency made workloads across regions
unreachable even when their compute was still running. The report proposes
remediation; this research did not independently verify completion. Multiple
Railway replicas alone are not proof against a platform-wide outage.

Vercel also has incidents. Its [September 2026 history](https://www.vercel-status.com/history)
includes deployment problems and an Edge-runtime build/500-error incident. Those
events differ in affected services, scope and duration from the Railway incident;
counting all incident entries is not an apples-to-apples availability comparison.
No comparative uptime ranking is established here.

For this project, independent Supabase data and B2 recovery storage reduce dependence
on the application host, but they do not by themselves guarantee zero confirmed-order
loss. Exportable deployment configuration, artifacts and securely recoverable secrets
must be available outside Railway; recovery cannot depend on logging into an
unavailable dashboard. Database/auth/storage availability also affects checkout.
Splitting UI/API across Vercel/Railway may keep browsing available during an API
outage but does not make ordering work or provide backend failover.

The approved longer-provider-outage exception makes this risk compatible with the
stated tolerance in principle, subject to complete order recovery. If the business
instead needs an unconditional three-hour recovery during a total provider outage,
neither ordinary single-provider plan establishes that; the architecture/budget
would need reconsideration. This is not a new demand for 24/7 human support.

### Growth, migration triggers and evidence before launch

At 80 orders/month, illustrative conversion of 1%, 2% or 4% implies 8,000, 4,000 or
2,000 visits/month. These are sensitivity scenarios, not researched industry averages
or an accepted traffic forecast. Photos, bots, campaigns and admin work can dominate
cost more than order count. Ten times the orders does not itself identify a platform
limit; workload shape and measured resource use decide when scaling is needed.

Plan to stay on the selected host while it meets measured needs. Before launch,
validate representative catalog/checkout/admin operations and a plausible campaign
burst, DB connections, job failures, deploy/rollback and independent recovery. A
proposed seven-day runtime sample including both environments would improve the
USD25 estimate; this is future validation, not a completed test or a prerequisite
requiring application implementation inside GK-004. Keep production API awake for
checkout and validate pooled DB connectivity. Pin versions and deployment artifacts.

Revisit hosting only for observed triggers: recurring service incidents incompatible
with business tolerance; forecast spend beyond approved limits after reasonable
optimisation; measured latency/resource ceilings; an unavoidable runtime limitation;
or a changed residency/SLA requirement. Do not migrate solely because order count
grows or another vendor markets a new feature.

Preserve SvelteKit Node deployment, ordinary Go server/commands, PostgreSQL schemas
and migrations, external object storage, custom domains and scoped environment
configuration. Avoid adding vendor-specific queues/workflows merely for future
flexibility. This keeps a future move bounded if ever needed; it does not build or
pay for a second production platform now.

Decision implication: retain Railway Pro at this stage, explicitly acknowledge the
reliability caveat, and verify the cost/performance assumptions before launch. The
research strengthens architectural justification; it does not close the remaining
GK-004 region/deletion/recovery acceptance checks or change the approved spend.

## Historical provider and architecture evidence — r1

Checked 2026-09-21 against primary pages. Research is not account, sandbox, device,
price quote or production evidence. Creator confirmed London UK, GBP, Europe/London and up to GBP100/month, aiming lower initially. Volume and recovery requirements are missing. No paid provider is accepted or provisioned.

| Candidate | Evidence and cost boundary | Recommendation / unresolved check |
| --- | --- | --- |
| PostgreSQL modular monolith | Brief explicitly requires PostgreSQL. One database transaction can coordinate orders, holds and refund balances; independent UI/backend deploys do not imply microservices | Prefer one backend plus same-code job runner. Compare Go and Django below |
| Supabase Postgres + Auth + Storage | [Pricing](https://supabase.com/pricing): Pro from USD25/month with one project; extra projects from USD10; daily backups seven days; PITR from USD100/month. Free pauses after inactivity and lacks automatic backups | Candidate consolidation; choose region, restore targets, object backup and environment costs before purchase. [Backup documentation](https://supabase.com/docs/guides/platform/backups) says DB backups do not back up storage objects; plan both |
| Render UI + API + scheduled job | [Pricing](https://render.com/pricing) search extract lists USD7/month 512MB compute, but rendered price table unavailable in browser extract. [Cron](https://render.com/docs/cronjobs) bills runtime with USD1/month per-job minimum | Provisional two web services + one cron floor USD15/month; verify live plan/limits before acceptance. Avoid sleeping free API for checkout commitments |
| Resend transactional email | [Pricing](https://resend.com/pricing) verifies free 100/day, webhook events, 30-day retention; monthly tier prices unavailable in captured dynamic table | Candidate only; capture an actual transaction-tier quote before budget acceptance. No guessed fixed USD20 promise. Compare [Postmark pricing](https://postmarkapp.com/pricing) in provider decision task |
| Stripe hosted Checkout | [Pricing](https://stripe.com/pricing) is country/method dependent. Original processing fees generally not returned on refund | Candidate for UK after business eligibility, local fees/currency/methods and sandbox late-payment/full/partial-refund checks. Hosted payment avoids handling card details in our forms |
| Cloudflare Web Analytics | [Official overview](https://developers.cloudflare.com/web-analytics/about/) describes free service without moving DNS/proxy | Candidate privacy-friendly visit/pageview metric. Does not establish distinct daily people; consent and metric acceptance remain Creator decisions |

Illustrative recurring floor for one Supabase Pro project + two smallest Render web
services + minimum cron: USD40/month, or USD50 with second paid DB project. Excludes
email, domain, payment fees, taxes, bandwidth/compute overage, storage-object backup,
PITR and any workspace-plan fee. This is arithmetic on provisional rates, not an
approved budget or production sizing. A free development environment has different
availability/backup properties and cannot be sold as production-equivalent.

## Stack options to assess in ADR-0001

- Go stdlib HTTP + pgx + SQL migrations + one UI application: transparent shared
  transactions, small runtime; custom chef screens are real work. sqlc is optional
  until query maintenance benefits justify generation. No POC framework mandate.
  [Go routing](https://go.dev/doc/go1.22#enhanced_routing_patterns).
- Django + PostgreSQL + separate UI: built-in ORM/auth/admin reduces commodity
  configuration work. [Django admin guidance](https://docs.djangoproject.com/en/5.2/ref/contrib/admin/)
  positions admin as an internal model-centric tool; process-centric chef journeys
  still require custom views. Separate identity/UI architecture must avoid two
  competing authorization sources.
- A single TypeScript backend plus SvelteKit UI reduces languages but needs explicit
  transaction discipline and backend/job boundaries; no existing TS stack to reuse.
- SvelteKit with [Node adapter](https://svelte.dev/docs/kit/adapter-node) is a proposed
  UI choice for public server-rendered catalog and interactive customer/admin flows.
  Browser and SSR only call the authorized API, never business database tables.
  Static-only UI is cheaper but needs a deliberate current-catalog/SEO strategy;
  page build releases must not be the way a chef publishes prices/availability.

Recommendation under review: Go modular API + SvelteKit shared customer/admin UI,
Supabase capability bundle, Render separate runtimes, Stripe and Resend candidates.
No microservices, Redis/broker, courier orchestration, generic CRUD layer or custom
identity issuer. Pin actual supported versions and exact check commands in bootstrap,
not by copying the POC's old version examples. Changing stack before bootstrap is
reversible because these artifacts specify domain contracts independently.

## UK update — Creator decision r2

GBP/Europe-London and hybrid stock are confirmed. [Stripe UK pricing](https://stripe.com/gb/pricing)
currently shows 1.5% + GBP0.20 for standard UK cards (other cards/methods differ).
Example GBP60 purchase fee under that rate is GBP1.10, before other services/taxes;
processing fees scale with sales and should be separated from the GBP100 hosting
budget. Exact account eligibility/fee schedule still requires Creator verification.

UK Creator policy evidence: [food-business registration](https://www.gov.uk/guidance/food-business-registration)
requires registration with the relevant local authority in advance (guidance says
at least 28 days). Confirm existing registration, not duplicate it. [Food delivery
allergens](https://www.gov.uk/government/publications/food-safety-for-food-delivery/food-safety-for-food-delivery)
requires allergen information before purchase and at delivery. Record chef-approved
ingredients/cross-contact and fulfillment labelling. [Distance selling](https://www.gov.uk/online-and-distance-selling-for-businesses)
is input for trading/contact/price/terms review, not automatic entitlement to cancel
perishable/custom goods. Website's promised before-production cancellation policy
must be explicit, with statutory rights reviewed separately. No generic 14-day
cake refund promise is inferred.

[Current ICO guidance](https://ico.org.uk/for-organisations/direct-marketing-and-privacy-and-electronic-communications/guidance-on-the-use-of-storage-and-access-technologies/what-are-storage-and-access-technologies/)
covers cookies/local storage and similar technologies. Creator/privacy review must
classify draft/favourite/auth storage and chosen analytics under current rules;
“cookie-free” does not establish legal compliance. All checks dated 2026-09-21;
legal/business sign-off remains Creator work before live sales.
