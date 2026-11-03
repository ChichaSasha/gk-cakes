# GK-011 payment and email setup — guided session

Source: [GK-011](https://trello.com/c/ZhcwMqTc), 2026-10-05, revision r17.
**GK-011 is verified Done for the explicitly approved account-provisioning scope.**
Sasha replied "approve" to the concrete split/publication/Done request. See the
[accepted scope snapshot](closure-scope-accepted.md),
[published input revision r16](publication-source-r16.md), and
[verified publication receipt](closure-publication-receipt.json).
The accepted scope file records the pre-publication snapshot; its pending-publication
wording is historical and is superseded by this receipt. The original proposal and
published source hashes are preserved. All five comments and the compact parent
scope amendment were re-read exactly before moving GK-011 alone to Done. The four
downstream descriptions/statuses and BO-GK-011 were unchanged.
Observations below are Sasha's explicit replies in this conversation, not
agent-authenticated dashboard inspection or independent review. Business-owner
actions are reported by Sasha; no direct Elina approval was collected here.

## Confirmed decisions and reported setup

| Item | Current evidence |
| --- | --- |
| Stripe ownership | Sasha registered a different Stripe account using the business owner's email, superseding his earlier temporary personal-ownership choice |
| Stripe organisation | `GK Cake London` |
| Stripe account/project | `GK Cake`, using the user's terminology; selected sandbox account ID is recorded below |
| Stripe sandbox | User confirmed he is inside `GK Cake London` sandbox |
| Stripe access | Sasha's own login has Super Administrator rights |
| Stripe MFA | User confirmed both his and the owner's separate logins have MFA |
| Live merchant verification | Asked whether Elina completed Stripe business and payout verification, Sasha replied "completed". User attestation only; no agent live-account inspection, real-money test or launch authorization |
| Stripe default API version | User copied API versions panel: `2026-08-26.dahlia` marked Default / Latest; no upgrade performed |
| Stripe sandbox credential | Following instructions to save the existing `sk_test_` sandbox key in shared Bitwarden as `GK Cakes Sandbox — Stripe API key`, user reported "Found and saved it"; secret value not accessed |
| Stripe account identity/configuration | User supplied filtered output from guided GET `/v1/account`: `acct_1UNHEgPqGToF2ulH`, country `GB`, default currency `gbp`; command required an `sk_test_` key and requested API version `2026-08-26.dahlia` |
| Launch payment-method decision | Latest reply: "I'm ok with all enabled options. Let's move on" accepts the complete enabled set copied below, superseding the narrower cards plus Apple Pay selection. Google Pay remains disabled. This does not authorize future automatic additions or enable currently disabled methods. Integration qualification pending |
| Payment-method dashboard readback | User copied Cards and Apple Pay Enabled; Google Pay Disabled, plus the other enabled methods listed below; user subsequently accepted keeping that current enabled set |
| Resend account | User reached the dashboard after guided registration; subsequently confirmed Free plan; exact login/team not independently verified |
| Resend domain settings | User explicitly confirmed Ireland (`eu-west-1`), Sending Enabled and Receiving Disabled |
| Approved sender name | `GK Cakes`, explicitly selected by Sasha in the GK-012 interview after resolving the cross-ticket name conflict; supersedes the earlier `GK Cake London` selection |
| Approved From | `orders@gkcake.com` |
| Approved Reply-To | `gk.cake.london@gmail.com` |
| Controlled test recipient | `gk.cake.london@gmail.com` |
| Domain purchase | User purchased `gkcake.com` via selected Cloudflare Registrar for US$10.46 for one year |
| Domain status and renewal | User copied Active; expiration 5 October 2027; renewal price $10.46/year (USD in purchase context); auto-renew scheduled 5 September 2027 for one year. Current dashboard quote, not a guaranteed future charge |
| Domain contacts | User supplied the Cloudflare Contact page showing Sasha as registrant, administrator, technical and billing contact. Personal address, phone, account identifier and contact email are intentionally not copied here. The excerpt contains no email-verification indicator |
| Domain contact email verification | User confirmed the matching Cloudflare profile email is marked "verified" after checking the registrant/profile match; no distinct outstanding registrant-verification requirement was reported |
| Cloudflare and Resend MFA | User confirmed both |
| Emergency recovery | User confirmed Elina can recover both Cloudflare and Resend if Sasha is unavailable; mechanism not specified or tested here |
| Resend domain | User first reported Pending, then Verified after guided Cloudflare DNS setup |
| Resend sending credential | User confirmed creation of `gk-cakes-sandbox`, Sending access restricted to `gkcake.com`, saved in the shared Bitwarden collection as `GK Cakes Sandbox — Resend sending key`; secret value not accessed |
| Controlled email test | Sasha confirmed mailbox receipt and supplied the Resend email object: ID `01a10dbc-6000-76de-9d3e-e6a5843ba602`, created `2026-10-05 20:23:31.746000+00`, last event `delivered`; From address, recipient and Reply-To match. Display name was the then-approved `GK Cake London`; later changed to `GK Cakes` in GK-012 |

Earlier `Gaykovna sandbox / Gaykovna` and the first organisation's pending
invitations are historical observations, not the selected new sandbox's evidence.
Do not reuse earlier account identifiers or assume those invites were accepted.
Owner and developer login emails were supplied in the conversation; they are
intentionally not duplicated here. No credentials or recovery codes were collected.

## Guided settings versus verified observations

The guide specified Resend Free, Ireland (`eu-west-1`), sending enabled, receiving
disabled, and automatic Cloudflare DNS setup for `gkcake.com`. The user confirmed
dashboard access and subsequent domain verification, then explicitly answered
"Yes" to the four-item readback of Free, Ireland, Sending Enabled and Receiving
Disabled. These are user-confirmed settings, not agent-authenticated inspection.
The initial three sender fields were explicitly approved together; recipient was
explicitly supplied separately. The subsequent GK-012 name amendment is recorded
below and preserved. The domain price is an actual user-reported
purchase amount, not an agent-verified receipt. A later copied Cloudflare readback
separately supplies the current $10.46/year renewal quote, Active status and renewal
schedule. The subsequent explicit profile-email verification confirmation supplies
the contact-email check; Active status alone was not treated as verification proof.

## Required downstream evidence

These obligations were explicitly retained and published on GK-019, GK-024,
GK-025 and GK-026 under the approved completion split. They do not imply unfinished
Creator account provisioning or successful application integration tests.

- Qualify the complete currently enabled method set accepted below, including
  method-specific processing, expiry and refund behavior; Google Pay remains
  disabled. The old proposed card-only integration assumptions need reconciliation
  in GK-019/GK-025 before implementation. Record event selection and adapter-compatible pinned version with the
  actual adapter. Dashboard default `2026-08-26.dahlia` is recorded, not proof of
  future SDK or webhook event-version configuration. Panel also listed
  `2025-06-30.basil;updated_requirement_errors_beta=v1` and `2025-06-30.basil`;
  neither was reported as the default and neither was selected in this session.
- Live merchant business/payout verification is user-reported completed; future
  authorized go-live checks must independently verify current production readiness.
  Owner supplies identity/banking details only to Stripe.
- Saved setup credentials are named above. The future backend secret store must
  receive `PAYMENT_SECRET` and `EMAIL_API_KEY` with application-appropriate scopes;
  webhook secrets are created only with actual endpoint configurations. Record
  deployment/access and rotation procedures under GK-025 and GK-024. A Bitwarden
  setup item is not evidence of configured application runtime secrets.
- Collect public/provider DNS configuration and review DMARC, plus applicable
  provider terms. Resend plan, region and sending/receiving settings are now
  user-confirmed.
  [Read-only public DNS observations](public-email-dns-2026-10-05.json) record
  the DKIM TXT answer and send-host MX/SPF alias answers from the host resolver.
  The DMARC TXT query returned no answer; authoritative absence and appropriate
  policy remain unverified. No DNS records were changed by the agent.
- Signed application email webhook tests remain separate from the supplied
  Resend delivery record and user-confirmed mailbox receipt.
- Domain matching-profile email verification, renewal arrangements and current
  quoted price are recorded from the user's confirmations/readback.
- Signed webhook reachability and integration tests await real sandbox endpoints;
  never invent URLs or mark future payment/refund/email adapter checks passed.
- Sasha accepted the completed provisioning evidence and scope split. Verified
  publication and the GK-011 Done transition are recorded in the receipt. Trello
  remains the live status source; no other ticket was transitioned.

## Acceptance mapping

| Criterion | Evidence now | Still pending |
| --- | --- | --- |
| AC1 payment test account/configuration | Selected sandbox, access/MFA, saved credential, approved current method settings and copied default API version; supplied read-only API output confirms account ID, GB and gbp | Backend credential deployment, method qualification and actual adapter/event version configuration under proposed split |
| AC2 domain and sender | User-reported verified domain/settings; approved addresses and recipient; public DNS observations plus GK-012 provider-export comparison; message ID, delivered event and user-confirmed receipt | Current display-name verification in implementation; DMARC review/hardening retained in GK-024 |
| AC3 endpoint separation | Deferred until actual sandbox targets exist | Endpoint/event configuration and later integration evidence |
| AC4 revision/acceptance/Trello | Sasha approved r16 handoff/split; all five handoff comments, compact parent description amendment and GK-011 Done state verified | No remaining publication step for this scope; no independent review claimed |

## Controlled email test

Sender-name amendment, 2026-10-05: Sasha explicitly chose **GK Cakes** when asked
to resolve the different names in GK-011 and GK-012. Use
`GK Cakes <orders@gkcake.com>` for subsequent order/login email configuration.
From address and Reply-To are unchanged. The test below predates this amendment;
its actual `GK Cake London` payload and provider evidence are preserved verbatim.
See [GK-012 interview](../GK-012/README.md). No provider setting was changed by
this document amendment, and the old test is not a test of the new display name.

User ran the guided controlled email test using the saved scoped credential.
Prepared payload: From `GK Cake London <orders@gkcake.com>`, To and Reply-To
`gk.cake.london@gmail.com`, subject `GK-011 email setup test`, text
`Controlled setup test for GK Cake London. No customer order or payment is involved.`
Idempotency key `gk011-email-20261005-r1`; Resend documents 24-hour retention.
Sasha subsequently replied: "Everything worked out and I received email on
gk.cake.london\\@gmail.com". This confirms user-observed receipt; the exact API
response was not supplied at that point. He subsequently supplied the Resend
email object with these values (Markdown escapes removed when interpreting):

```json
{
  "object": "email",
  "id": "01a10dbc-6000-76de-9d3e-e6a5843ba602",
  "to": ["gk.cake.london@gmail.com"],
  "from": "GK Cake London <orders@gkcake.com>",
  "created_at": "2026-10-05 20:23:31.746000+00",
  "subject": "GK-011 email setup test",
  "html": "",
  "text": "Controlled setup test for GK Cake London. No customer order or payment is involved.",
  "bcc": null,
  "cc": null,
  "reply_to": ["gk.cake.london@gmail.com"],
  "headers": null,
  "last_event": "delivered",
  "scheduled_at": null
}
```

This is user-supplied provider evidence, not an authenticated agent API read.
The delivered event records provider delivery to the recipient server; Sasha's
separate observation confirms receipt for this one test. No general inbox-delivery
guarantee or signed application-webhook verification is inferred.

The temporary difficulty finding Reveal was resolved by the user. An alternative
restricted-key flow was researched but no creation steps were completed or
reported. Do not infer that a new restricted key was created. The saved existing
key is a broad sandbox setup credential; least-privilege application permissions
remain part of the actual adapter configuration.

User supplied the following filtered result from the guided read-only check:

```json
{
  "id": "acct_1UNHEgPqGToF2ulH",
  "country": "GB",
  "default_currency": "gbp"
}
```

This records successful user-run API access and the expected country/default
currency. It does not establish accepted payment methods, successful payment or
refund processing, or live merchant activation.

## Payment-method dashboard readback

Sasha supplied Cards and Apple Pay Enabled, Google Pay Disabled. Cartes Bancaires
was also Enabled in category Cards. Other Enabled entries were Amazon Pay,
Kakao Pay, MB WAY, Naver Pay, Onelink (verbatim supplied label), PAYCO,
Revolut Pay, Samsung Pay, Satispay, Bancontact, BLIK, EPS, Klarna and Pix.
After the assistant proposed disabling the extra methods, Sasha replied:
"I'm ok with all enabled options. Let's move on". This explicitly supersedes
the earlier narrow selection and accepts keeping all enabled entries listed
above, including Klarna and the bank/real-time methods. No disabling was reported.
Google Pay and the other entries copied as Disabled remain disabled. Future
provider-added methods are not automatically approved by this snapshot decision.
The pasted text
also included "Recurring payments"; it does not change the shop's one-time
purchase model or establish that subscriptions are intended.

Next work: GK-019 reconciles the accepted configuration; GK-024/025/026 perform
their retained implementation/verification work. No further routine Creator
setup question remains for this accepted GK-011 scope. The later
Checkout integration must preserve the Google Pay exclusion; a generic card-only
API method selection alone is not evidence that this wallet is disabled.
