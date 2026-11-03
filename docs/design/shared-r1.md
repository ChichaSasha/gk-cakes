# Shared planning skeleton — r1

2026-09-21. Draft contract skeleton; input-r1 hashes identify source inputs.
No application/screens/assets exist. Only documentation/skill CI exists. All screen
IDs are proposed. Current authorization: planning/research/subagents/scoped Trello
publication by master only; no app implementation, accounts, Git settings, pushes,
merges, releases or deployment. Preserve all initial staged/untracked work.

Technical baseline for evaluation: modular monolith with PostgreSQL, separately
released customer/admin UI and API; no microservices, broker or courier engine.
Master running identity/effort has no effective host metadata: unknown. Qualified
explicitly selected Astra workers must assess consequential decisions. Worker
selection status stays requested-only unless effective runtime metadata appears.

Vocabulary: inspiration != product/variant != custom request != quote != order.
Orders owns lifecycle and snapshots; scheduling owns production and pickup capacity
separately; payments owns collection/refund operations; catalog owns versioned
recipes/variants; inventory owns ingredient movements; notifications owns delivery
evidence; identity owns authentication with local role/ownership checks.

Proposed lifecycle: awaiting_payment -> confirmed -> in_production -> ready ->
completed; expired/cancelled terminal paths separately. Fulfillment and payment,
refund, notification enums are separate. Positive customer cancellation eligibility
only before production; completed is never eligible. Exact production-start policy
and administrative exceptions need Creator decision. Money integer minor units,
ISO currency; UTC instants + IANA shop timezone + explicit local production date.
Snapshots preserve price, recipes/allergens, delivery and cancellation/pickup policy.
No provider calls inside database locks. Pending/unknown refunds reserve balance.
All processes share PostgreSQL constraints/locks; no process mutex correctness.

Unknown: country/currency/timezone/budget/scale; stock vs made-to-order; capacity units,
cutoffs; quote acceptance/guest/reorder; delivery/tax/refund/discount/inventory policy;
assets; provider selection/account ownership. Sufficient for options/design and
blocked backlog packets, not implementation or business approval.

Adjacent packet ownership: financial pilot and commerce worker; customer/catalog/
identity/visual worker; chef/inventory/platform worker. Only master edits shared files.
Initial pilot: cancellation versus production and cumulative refund concurrency,
then fresh independent review. Pause author/reviewer after short readback until
master checks it. Required output distinguishes future tests from performed checks.
