# GK-012 — domain, addresses and Instagram interview

Interview r4, 2026-10-05. Source: Sasha's explicit replies in the current
GK-012 guided interview. This preserves decisions and reported observations;
it is not independent verification or a live status tracker. Nothing from this
interview has been published to Trello yet. Sasha replied “yes” to approving the
consolidated handoff and authorizing publication to GK-012 while keeping the ticket
open for the remaining technical evidence. Publication verification is pending.

Ticket: [GK-012](https://trello.com/c/nveiC5IF).
Related owner contribution: [BO-GK-012](https://trello.com/c/xaVN5VOR).
The original publication snapshot remains unchanged.

## Approved decisions and source replies

### Domain

Sasha first selected “No—still unpurchased, and it remains the domain I want.”
His later reply supersedes that purchase status:

> I've already bought `gkcake.com` through Cloudflare

Record domain registration as **purchased, reported by Sasha**. Sasha subsequently
answered yes to DNS dashboard access, enabled automatic renewal with a valid
payment method, two-factor authentication and Elina having a way to recover/manage
the domain if he is unavailable. These are user confirmations; the agent did not
inspect the account settings or perform a recovery exercise. No purchase, account
creation or DNS mutation was performed by the agent.

The accepted GK-004 arrangement is Sasha administering services, the business
owner paying and receiving emergency recovery access. Sasha's latest answers
confirm this arrangement is set up. The recovery mechanism was not specified;
no secret values are required in the handoff.

### DNS evidence

Sasha supplied a Cloudflare zone export dated `2026-10-05 20:25:51` (timezone not
specified), showing two nameservers, two CNAMEs and a DKIM TXT public key.
The agent independently queried these four public record sets with `dig`
on 2026-10-05. Every query returned exit code 0 and matched the supplied export:

| Record | Published value |
| --- | --- |
| `gkcake.com` NS | `keenan.ns.cloudflare.com.`, `serenity.ns.cloudflare.com.` |
| `rsend.gkcake.com` CNAME | `rsend-euw1.forge.rmta.net.` |
| `send.gkcake.com` CNAME | `send.forge.rmta.net.` |
| `resend._domainkey.gkcake.com` TXT | Public key exactly matched Sasha's supplied `p=MIGf…IDAQAB` value |

The initial sandboxed DNS query was denied socket access; the normal approved
escalation succeeded. These checks establish public DNS publication, not private
registrant identity, provider-side verification, email delivery or recovery access.
The supplied export contains no storefront/API/staging routing records. No
application deployment is claimed or required merely to approve the hostnames.

### Address layout

Sasha: “approve this address layout”. All addresses below are approved intent,
not evidence of configured DNS, routing, certificates or deployments.

| Purpose | Approved address |
| --- | --- |
| Customer website | `https://gkcake.com` |
| Alternate hostname | `https://www.gkcake.com`, redirects to the customer website |
| Admin | `https://gkcake.com/admin` |
| Production API | `https://api.gkcake.com` |
| Test website/admin | `https://staging.gkcake.com`, admin at `/admin` |
| Test API | `https://staging-api.gkcake.com` |

Admin shares the UI deployment and requires staff authorization. Test credentials
and synthetic data remain separate from production. Exact authentication return
paths, local development URLs and provider project endpoints are still pending
the actual implementation/configuration; do not invent them from this layout.

### Email identity

Sasha replied “Approve” to all three settings:

- Sender display name: **GK Cakes**.
- From address for order and login emails: **orders@gkcake.com**.
- Reply-To: **gk.cake.london@gmail.com**.

Cross-ticket conflict resolved: GK-011 previously approved/tested **GK Cake London**.
Asked explicitly which name should appear on all order/login emails, Sasha replied
**“GK Cakes”**. This supersedes the earlier GK-011 display-name decision. Its current
sender selection is amended, while its historical test payload is preserved.
The From and Reply-To addresses are unchanged.

[GK-011 r4](../GK-011/README.md) also records user-reported Resend domain verification,
a supplied provider object with a delivered event and Sasha's controlled-test inbox
receipt. The GK-012 agent has not authenticated to Resend or inspected the mailbox.
The existing delivery test used the old display name. No paid mailbox is implied.

### Instagram control and revised handoff

Sasha confirmed:

> Yes—Elina controls it and can log in and read DMs

The referenced account is **@gk.cake.london**, superseding the old ticket's
`@gk.cake` spelling. This is Sasha's report, not a direct new Elina statement.

Sasha rejected the proposed separate manual copy/open instructions as too
complicated and requested the direct link `https://ig.me/m/gk.cake.london`.
He then replied “Yes” to this revised proposal:

- Primary custom-request button: **Copy request & open Instagram**.
- One website tap attempts to copy the request summary and open the DM link.
- Customer pastes and sends in Instagram.
- Manual copying remains the fallback, with the summary/reference retained.
- General contact links can open the DM without copying a request.
- Opening the link is not evidence that a message was sent.

The combined action is approved product intent, **not tested behavior**. Existing
K02's separate-action wording needs an explicit reconciliation for GK-027 before
implementation. Do not silently claim that contract was already updated.
No reliable prefilled-text behavior is assumed. The linked third-party guides
disagreed, and Meta documentation could not be retrieved during research.

## Reported chat-link tests

These test only opening `https://ig.me/m/gk.cake.london`. They do not test a
website button, clipboard access, pasting, message sending or fallback handling.

| Device | Sasha's observation | Evidence qualification |
| --- | --- | --- |
| Mac | Opened DM with @gk.cake.london in Instagram web | Explicit device, destination and web result |
| iPhone | “Works as expected. Device: iPhone, Opened dm in Instagram app” | Correct-chat success reported in response to that check |
| Android | “Works as expected!” in response to the Android check | Android success inferred from question/reply context; app/browser and model not supplied |

The agent did not independently observe these devices or send test DMs.

## Remaining acceptance evidence and next steps

No unresolved business-choice question remains in this interview. The final
sender-name clarification is incorporated above and into the GK-011 record.

1. Derive and approve exact environment origin/callback settings from the actual
   identity implementation and selected provider endpoints; no production wildcard.
   DEV-GK-012 explicitly coordinates exact callbacks with GK-020/021 when available;
   this is developer work, not an unanswered business-choice question.
2. Test the combined copy/open/paste action and meaningful failure fallbacks on
   the supported devices when a representative button exists. Keep this distinct
   from the completed user-reported chat-opening checks.
3. Reconcile the revised interaction into affected specifications (including K02
   and GK-027) and prepare a revision-linked acceptance summary.
4. Publish the accepted handoff under Sasha's explicit authorization and verify it.
   Closure is not authorized or claimed; the technical gaps remain outstanding.

AC1 evidence is collected: domain purchase, access, renewal, MFA and recovery
reported; DNS publication independently checked; hostnames approved. AC2 is partial:
control reported, primary chat opening
reported on three platforms, combined action/fallback untested. AC3 is partial:
hostnames and email identity approved, exact callback configuration pending.
AC4: Sasha accepted the consolidated handoff and authorized publication; publication
verification and remaining technical evidence are pending. No independent review
is claimed.

Accepted publication text: [English handoff](handoff-summary.en.txt), with
[Ukrainian owner brief](handoff-summary.uk.txt). Publication to the parent GK-012
only is authorized; no owner-card mutation is included.
