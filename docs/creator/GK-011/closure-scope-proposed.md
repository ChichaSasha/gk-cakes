# GK-011 proposed completion split

Draft r1, 2026-10-05. Not approved, published or applied to Trello.
Evidence: [account-provisioning handoff r16](README.md), including the newer
GK-012 sender-name decision. This is a scope proposal, not a claim that the
unchanged original acceptance criteria are all complete. Self-check only; no
independent review is claimed.

## Proposed completed scope

- Stripe owner-created organisation/account and selected sandbox; Sasha's
  Super Administrator access; both logins use MFA; business/payout verification
  reported completed by Sasha. Selected sandbox `acct_1UNHEgPqGToF2ulH` returned
  country `GB` and default currency `gbp` in a user-run read-only API request.
- Default API version `2026-08-26.dahlia` recorded. Preserve the exact accepted
  dashboard-enabled method set in the handoff; Google Pay remains disabled.
  Acceptance of methods is not proof every method is qualified by application tests.
- Stripe sandbox setup key and domain-restricted Resend Sending key saved in
  named shared Bitwarden items; no credential values copied into task artifacts.
- Resend Free, Ireland, sending enabled, receiving disabled and verified
  `gkcake.com`, all user-confirmed. Domain bought through Cloudflare for
  US$10.46/year, Active, expires 5 October 2027, auto-renews 5 September 2027 at
  currently quoted US$10.46/year. Matching profile/contact email verified.
- Cloudflare/Resend MFA and Elina's emergency recovery access user-confirmed.
- Current approved sender `GK Cakes <orders@gkcake.com>`, Reply-To and controlled
  recipient `gk.cake.london@gmail.com`. Test under the previous display name
  `GK Cake London` returned the supplied Resend object with event `delivered`,
  ID `01a10dbc-6000-76de-9d3e-e6a5843ba602`; Sasha confirmed mailbox receipt.
- Public email DNS observations saved; GK-012 separately compared the supplied
  Cloudflare zone export with public DNS. No private account inspection by this
  agent, new-display-name test, signed webhook test or real-money payment claimed.

## Required downstream work, retained rather than waived

Publish scoped handoff comments on these four existing tickets before closing
GK-011. Keep their statuses and unrelated requirements unchanged.

| Ticket | Required deliverable before its acceptance |
| --- | --- |
| [GK-019](https://trello.com/c/lW3wuYq0) | Reconcile the accepted method set and current sender into contracts. Replace proposed card-only assumptions where necessary; retain full upfront payment, authoritative provider success, unknown-state handling and current owner payment-window policy. Surface any method incompatibility for an explicit decision rather than silently dropping a method or accepting uncertain funds as paid. |
| [GK-024](https://trello.com/c/J6zlPAc9) | Deploy appropriately scoped `EMAIL_API_KEY` in the real sandbox backend secret store; use `GK Cakes` sender name and approved addresses. Configure actual signed email webhook URL/events/`EMAIL_WEBHOOK_SECRET`; verify valid/invalid signatures, delivery/bounce/unknown states, idempotency retention and recovery. Review exact DNS and DMARC before customer sending; the current public DMARC query returned no answer, not a verified enforcement policy. Document applicable provider processing/transfer safeguards, usage alerts/limits and credential rotation/access ownership. Verify the final display name in the implemented email flow. |
| [GK-025](https://trello.com/c/vyFTtISc) | Qualify the exact selected methods in GBP, including Apple Pay device eligibility and delayed/redirect method behavior; preserve Google Pay exclusion. Pin an SDK-compatible API version and record actual webhook event version/event allowlist, account/environment and endpoint. Deploy least-privilege runtime `PAYMENT_SECRET` and endpoint-specific `PAYMENT_WEBHOOK_SECRET`. Prove capture/full-return core, terminal-unpaid expiry, late success and lost-response/idempotent recovery against the real sandbox adapter. Record fee schedule and credential rotation/access ownership. No invented endpoint or live-money authorization. |
| [GK-026](https://trello.com/c/xiEN4jvp) | Verify partial/full remaining refunds and failure/retry/concurrency behavior for accepted applicable methods using the shared GK-025 refund core. Credential availability is not refund-test evidence. |

GK-020 remains the actual runtime prerequisite through these tickets' existing
dependencies. GK-035 retains integrated scenario checks; GK-037 retains the
existing prelaunch legal/provider/privacy verification; GK-036 still needs
separate production-release authorization. No new release/deployment permission,
implementation execution, or automatic Ready/Done status is implied.

## Proposed acceptance interpretation

- AC1 closes account provisioning, usable user-tested API credentials, selected
  method settings and recorded default API version here. Actual adapter/event
  configuration and method/capture/refund qualification move explicitly to the
  named implementation tickets instead of being represented as already passed.
- AC2 accepts verified sending domain, DNS evidence, current approved identity,
  recipient and the historical controlled-send evidence. Final current-name
  rendering and DMARC work remain mandatory in GK-024.
- AC3 explicitly separates provisioned accounts from later reachable, signed
  endpoints. Runtime secrets and webhook setup wait for the real application.
- AC4 needs Sasha's explicit approval of this split and handoff revision plus
  verified publication. Append this accepted scope amendment to GK-011 while
  preserving its original description, then verify all four downstream comments
  before moving GK-011 alone to Done. Do not close BO-GK-011 or other cards.

No application code or provider integration was implemented in this interview.
The proposed closure means the account-provisioning input is complete, not that
checkout, all payment methods, refunds, webhooks or production are ready.
