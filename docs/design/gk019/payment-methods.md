# Accepted payment-method inventory — GK-019 r1

Source: GK-011 r17 and approved main-card comment
[6ac4131e1104732c928699bd](https://trello.com/c/lW3wuYq0#comment-6ac4131e1104732c928699bd),
exact content hash in input/evidence manifests. **All 17 labels remain accepted.**
Google Pay remains disabled. Disabled methods and future automatic additions are
not accepted. Current nonzero shop agreement/accounting currency is GBP; optional local presentment is approved by [Q4 currency source](q4-currency-acceptance.json);
non-GBP settlement and customer-credit implementation are not inferred.

Dashboard enabled, customer eligible, and integration qualified are separate facts.
GK-025 records an outcome for every row. Unsupported customer/currency/amount context
may legitimately not show a method, but a confirmed incompatibility must be brought
to Sasha with options before changing the accepted set. No card-only fallback.

| Accepted copied label | Required mapping/qualification evidence | Current result |
| --- | --- | --- |
| Cards | Card type/network, GB/GBP, capture and refund identities, authentication/cancel | Accepted; sandbox behavior untested |
| Cartes Bancaires | Prove card-network routing; do not invent a separate API method enum | Accepted; mapping/integration untested |
| Apple Pay | Wallet/card identity, supported device/browser/domain and refund | Accepted; integration untested |
| Amazon Pay | GB/GBP method-specific capability, asynchronous return and refund limit | Accepted; integration untested |
| Kakao Pay | Actual method ID, supported currency/customer context and GBP consistency | Accepted; qualification pending |
| MB WAY | Same currency/customer/amount/expiry/refund qualification | Accepted; qualification pending |
| Naver Pay | Same currency/customer/amount/expiry/refund qualification | Accepted; qualification pending |
| Onelink | Stripe confirms UK name for Link; preserve UK label. Qualify API/account identity and bundled funding controls | Accepted; name verified, integration qualification pending; current bundle accepted within GBP limits |
| PAYCO | Actual method ID and currency/customer/amount/expiry/refund qualification | Accepted; qualification pending |
| Revolut Pay | Website Stripe method; distinct from historical offsite Revolut invoice path | Accepted; integration untested |
| Samsung Pay | Actual method ID, supported customer/device/currency and refund | Accepted; qualification pending |
| Satispay | Actual method ID, supported customer/currency/expiry and refund | Accepted; qualification pending |
| Bancontact | EUR context in provider table; resolve compatibility with GB/GBP agreement | Accepted; incompatibility check pending |
| BLIK | PLN context in provider table; resolve compatibility with GB/GBP agreement | Accepted; incompatibility check pending |
| EPS | Actual method ID/currency/customer eligibility; approved optional FX; qualify integration | Accepted; qualification pending |
| Klarna | Merchant full-payment evidence versus buyer repayment terms; amount/refund support | Accepted; integration untested |
| Pix | BRL presentment; GB settlement can be GBP. Optional local presentment accepted; qualify provider evidence | Accepted; incompatibility check pending |

## Mandatory qualification record per applicable method

Provider API/event/SDK pinned version (dashboard default2026-08-26.dahlia is setup
evidence only), configuration ID/hash, exact method/network/wallet mapping, account/
environment, agreement/provider/presentment/settlement currencies, eligible country/
device/amount, session+underlying object identities, success predicate and timestamp,
processing/authorization/not-paid outcomes, create/expire/cancel uncertainty, full and
partial refund capability/time limit, duplicate/out-of-order event handling, and
sandbox evidence references. Store reasons for ineligibility and required decisions;
no unexecuted row is marked pass. Do not advertise every method on every browser.

Exercise redirect before webhook, completed-but-unpaid, delayed success/failure,
webhook-before-create-response, duplicate event IDs/objects, outage at15m and24h,
near192h lead boundary, wrong account/environment/amount/currency, pending refund,
expired provider refund window and method configuration drift. Never use actual
customer payments as a substitute for sandbox tests without separate authorization.

## Current primary evidence — read 2026-10-05

- [Checkout object](https://docs.stripe.com/api/checkout/sessions/object): complete
  does not necessarily mean paid. K04 requires matching authoritative funds for
  nonzero orders; local free confirmation has no fabricated collection.
- [Create session](https://docs.stripe.com/api/checkout/sessions/create): configured
  expiry30 minutes–24h after provider creation; this is not the local15m timer or
  sent-link24h clock. Dedicated method configuration/allowlist compatibility must
  be verified with the pinned version; no automatic future additions.
- [Expire session](https://docs.stripe.com/api/checkout/sessions/expire): explicitly
  expire open sessions; inability to expire is reconciliation, not failed payment.
- [Provider support table](https://docs.stripe.com/payments/payment-methods/payment-method-support):
  context-sensitive currencies/regions; validate method-specific pages and account
  configuration when tables disagree, not a blanket removal decision.
- [Amazon Pay](https://docs.stripe.com/payments/amazon-pay): method-specific GB/GBP
  support and asynchronous refunds with a90-day limit need integration qualification.
- [Klarna](https://docs.stripe.com/payments/klarna): customer repayment schedule does
  not itself imply the merchant receives installments. Verify full merchant funds.
- [Pix](https://docs.stripe.com/payments/pix): BRL presentment and GBP settlement for
  GB businesses are different currency facts. Optional local presentment now explicitly accepted; qualify actual conversion evidence.

These are documentation findings, not authenticated sandbox capability tests or
proof of every payment/refund outcome. Q2 timing is accepted; Q5 alternative return is accepted; Q4 method/bundle/settlement qualification remains.

## Q4 naming evidence — 2026-10-05

[Stripe support](https://support.stripe.com/questions/what-is-onelink?locale=en-GB)
confirms Onelink is the UK name for Link, a naming-only change from30May2026.
[Onelink FAQ](https://support.stripe.com/questions/link-faq?locale=en-GB) says
supported funding choices are bundled by default and most cannot be disabled
individually. The current bundle has a bounded approval; the no-future-additions policy remains.
Actual account/surface/version controls and GBP compatibility require qualification
before activation. Documentation alone approves no expansion. Sasha separately approved optional FX; current bundled funding scope is now separately approved subject to GBP compatibility ([source](q4-bundle-slots-acceptance.json)); future additions/non-GBP settlement need a new decision.

## Q4 currency approval — 2026-10-06

[Exact approval](q4-currency-acceptance.json) permits optional provider-managed local
presentment, keeping GBP shop pricing/accounting. [Stripe Adaptive Pricing](https://docs.stripe.com/payments/currencies/localize-prices/adaptive-pricing?payment-ui=stripe-hosted)
documents integration-currency amounts plus presentment details, customer conversion
charges, and original-rate returns. [Currency units](https://docs.stripe.com/currencies)
apply to presentment; do not label every minor unit pence. Qualify each accepted method
against the selected account/API and success/refund evidence; no runtime pass claimed.
GBP settlement remains the approved route. [Stablecoins through Onelink](https://support.stripe.com/questions/pay-with-stablecoins-on-link?locale=en-GB)
may settle USD and cannot be individually disabled per current support guidance;
retain this specific incompatibility as a gate if applicable, never silently change
settlement or remove the accepted wallet. Provider capability must be established
before proposing that separate settlement decision.
