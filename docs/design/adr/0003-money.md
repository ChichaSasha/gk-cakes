# ADR-0003: Payment, cancellation and refunds

Status: proposed integration revision; independent review pending
Date: 2026-10-05; revision: r4.3 / GK-019
Owner: planning master for technical integration; Sasha/original owner for attributed business decisions.
Affected requirements: C05 C06 A01 S01 S02 S07.

## Context and options

Compared synchronous success-on-return with durable operations/inbox/reconciliation. Select durable state because external timeouts cannot establish failed funds. Soft workload overage does not automatically cancel paid orders. Production-start no longer denies refunds. Separate chef delivery operation remains mandatory.

## Decision proposed for technical acceptance

Keep one order-level obligation allocated across actual cake/delivery collections; preserve provider observations and unknown reservations. Adopt current fixed120h100/50 policy, chef100%, explicit free confirmation,17 accepted method inventory and authoritative full merchant-payment evidence.25%-style arbitrary admin exceptions follow current half-up pence, not old ceil-every-component.

Governing detail: [contracts r6](../contracts.md) K03/K04/K05; [owner mapping](../owner-data-contract.md); [decision register](../creator-decisions.md). Business values are already approved where attributed; this status concerns their technical integration.

## Consequences and verification

Q2 payment timing is accepted: late paid requires feasible chef/customer agreement or full return. Q4 optional local presentment with GBP pricing/accounting is accepted; current Onelink bundle is accepted subject to GBP compatibility; actual qualification remains gated, and future additions/non-GBP settlement need a separate decision. Q5 verified alternative payout accepted under the same serialized obligation/reservation and K11 recovery; business entitlement survives provider limits. Real PostgreSQL sum/duplicate/quarantine tests and each applicable method sandbox qualification required.

Documentation checks are not implementation or provider tests. No extra vendor, deployment or public policy is activated. Any material contract change invalidates affected ticket readiness and requires re-review.

Supersedes prior r3 execution defaults for this topic; [exact historical text](../gk019/prior/0003-money.md.txt) and original evidence remain. Source and output hashes are recorded in the GK-019 manifests; no old review verdict is carried forward.
