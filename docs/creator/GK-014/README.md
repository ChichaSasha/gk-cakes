# GK-014 — monitoring, recovery ownership and analytics setup

Live ticket: [GK-014](https://trello.com/c/47tZfEoI). Owner: Creator (Sasha). The
Agent prepares settings, verifies what it can without provider logins, and records
non-secret evidence. Trello remains the live status record.

## Decisions — 2026-11-02 (Sasha)

| Topic | Decision |
| --- | --- |
| PostHog Cloud EU | Create now on the Free plan; configure privacy settings (AC3). |
| UptimeRobot Solo | Buy later, once GK-034 has staging targets. AC1 stays open until then. |
| Restore target | A separate Supabase Free project plus a separate B2 bucket. Restores never write to the sandbox or the backup bucket. |
| Incident contact | Sasha only: phone push from the UptimeRobot app, recurring notifications until resolved. |

## Starting facts (verified 2026-11-02)

- Prerequisites GK-004 and GK-020 are Done (GK-020 merged as `148b56e`, main CI green).
- No approved hostname resolves (`gkcake.com`, `api.`, `staging.`, `staging-api.`), so
  there is no endpoint to monitor yet. Endpoint monitors arrive with GK-034.
- Supabase sandbox `flbgymxjelzexgoxrcss` is on Free. Supabase backs up only Pro and
  higher plans (Pro: 7 daily backups). Database backups never include Storage objects,
  and daily backups don't keep custom-role passwords
  ([docs](https://supabase.com/docs/guides/platform/backups), checked 2026-11-02).
  The sandbox therefore depends entirely on our own B2 exports (GK-024/GK-034).
- B2 backup bucket (from GK-010): `gk-cakes-sandbox-backups-flbgymxjelzexgoxrcss`, EU
  Central, SSE-B2 on, prefixes `database/` and `objects/`, temporarily keeping all versions.
- No UptimeRobot or PostHog account exists yet.

## Step A — PostHog Cloud EU Free (Creator, now)

1. Sign up at <https://eu.posthog.com/signup> and choose the **EU** region. Organization
   and project: `GK Cakes`. Don't add a payment method: Free has no charges, and
   events beyond 1M/month are dropped rather than billed.
2. **Settings → Organization → General → IP data capture default:** discard. EU
   organizations default to this; confirm it.
3. **Settings → Project → General → IP data capture configuration:** discard.
4. **Settings → Project → Autocapture & heatmaps:** turn off interaction autocapture,
   heatmaps, dead clicks, web vitals and exception autocapture.
5. **Session replay:** recording off.
6. **Data pipelines → Transformations:** report whether GeoIP is enabled. Country data
   is not an approved measurement; leave it off unless you decide otherwise.
7. Report back (non-secret): app host, organization/project names, numeric project ID,
   plan, "no payment method", and the state of each setting above. The project token
   (`phc_…`) is public by design but stays out of Trello; GK-028 uses it.

These project settings are guard rails. Consent gating and route exclusions are
enforced in the UI code (GK-028) per K10: **no SDK load, storage or request before
consent**, no account/admin/order routes, and allowlisted checkout-step events only.
Note for GK-028: PostHog's docs recommend loading the SDK opted-out by default. K10 is
stricter and governs.

## Step B — isolated restore target (Creator, now)

1. Supabase, in the same Free organization as the sandbox: new project
   `gk-cakes-restore-test`, region London, Free. Store its database password only in
   the `GKCake` Bitwarden vault. Leave the project empty. Free projects pause after
   inactivity; resume it before a restore exercise.
2. B2: new private bucket `gk-cakes-restore-test-<restore project ref>`, default
   encryption SSE-B2 on, Object Lock off, lifecycle "Keep only the last version".
   Don't create application keys yet: the Agent receives scoped keys only for the
   restore exercise.
3. Report back: project ref, region, plan; bucket name, bucket ID, encryption and
   lifecycle.

## Step C — budget alerts (Creator, now for B2)

- **B2 → Caps & Alerts:** enable the daily caps with email alerts at 75% and 100%.
  Proposal: storage $0.03/day, download $0.03/day, Class B $0.01/day, Class C
  $0.01/day (about $2.40/month at most). Adjust if you prefer. A cap stops sandbox
  backups when reached; production caps are revisited at production provisioning.
- **PostHog:** Free, no payment method, so no spend. **UptimeRobot:** a fixed EUR 13/month
  once bought.
- **Railway and Supabase Pro:** not provisioned yet. Set their usage alerts and spend
  caps when they are created (GK-034 / production setup), so the provider alerts sum
  below the GBP71 forecast threshold (GBP61 baseline + GBP10). Any increase above GBP10
  still needs approval; the GBP100 ceiling stays.
- Monthly: compare invoices against the GBP71 forecast. Provider alerts don't add
  up into one cap automatically.

## Step D — UptimeRobot Solo (Creator, later, with GK-034)

When staging targets exist:
1. Buy Solo.
2. Install the UptimeRobot app on Sasha's phone and add it as the only alert contact,
   with recurring notifications enabled.
3. Monitors: staging API `/healthz` and `/readyz`, staging UI `/healthz`. Production
   monitors come at launch. Authenticated JSON metric checks come with GK-024/GK-033:
   health and aggregate values only, never customer or order data.
4. Controlled test: break a monitor deliberately and confirm the phone push. Leave an
   incident open past 23:00 and confirm it is still alerting after 10:00.
5. Report back: alert contact type, recurrence interval, monitor URLs and intervals,
   test times.

## Evidence received

Reported by Sasha on 2026-11-02 and checked by the Agent without logging in:

| Item | Value | Agent check |
| --- | --- | --- |
| Supabase sandbox | `flbgymxjelzexgoxrcss` | Was paused (its hostname didn't resolve); Sasha restored it. The API now answers 401 "No API key"; the database is in AWS eu-west-2 (London). |
| Supabase restore project | `gk-cakes-restore-test`, ref `gjrhgikkvxrvvogvxnrs`, `https://gjrhgikkvxrvvogvxnrs.supabase.co` | Active (API 401). Database in AWS **eu-central-1 (Frankfurt)**, checked against AWS's published IP ranges. |
| B2 restore bucket | `gk-cakes-restore-test`, ID `4c816e4d6ae091e7a01c0813`, private, encryption on, endpoint `s3.eu-central-003.backblazeb2.com`, empty | Sasha changed the lifecycle to "Keep only the last version". No application keys exist. |
| PostHog Cloud EU | Organization and project `GK Cakes`, project ID `332069`, Free | Sasha confirmed every Step A setting applied as instructed: IP data discarded at organization and project level; interaction autocapture, heatmaps, dead clicks, web vitals, exception capture and session replay off; GeoIP left off; no payment method. The Agent has no PostHog access and can't verify these independently. |

Creator-accepted exceptions (2026-11-02):

1. **Restore project in Frankfurt.** It stays in eu-central-1 instead of London. Condition:
   only synthetic sandbox data is ever restored there. Restoring production or personal
   data needs a London target and a new decision.
2. **No B2 caps or alerts for now (Step C).** B2 usage is unbounded until caps are set.
   Revisit at production provisioning, together with the Railway and Supabase Pro
   alerts, to meet the GBP71 forecast-alert rule.

## Acceptance mapping (current)

| AC | Status |
| --- | --- |
| AC1 alert reaches named owner; thresholds | Moved to GK-034 by Sasha's decision (2026-11-02). Contact named (Sasha, recurring push). GK-034 records the Step D evidence once staging targets exist; Sasha still buys UptimeRobot Solo and installs the app. |
| AC2 DB + object backups, retention, restore destination, access | Recorded for the sandbox: no provider backups on Free; independent B2 bucket and prefixes; 30-day retention target implemented with the GK-024/GK-034 backup jobs; restore destination is the Frankfurt project and B2 bucket above (exception 1); Sasha holds admin access, the owner has recovery access, and the Agent gets scoped keys only at restore time. Production records come at production provisioning. |
| AC3 analytics privacy | Project-level settings applied (Creator-reported). Consent gating, network-payload and private-route checks happen in GK-028. |
| AC4 evidence on Trello, Creator acceptance | Steps A–C evidence on Trello (comment 6ae9149fe9b962a8cab55836); Sasha accepted GK-014 on 2026-11-02 with AC1 moved to GK-034 and the two exceptions above. |

Access summary: Sasha administers every account; the business owner holds recovery
access through the `GKCake` Bitwarden vault (GK-010). The Agent has no provider
logins and receives only scoped keys for specific tasks.
