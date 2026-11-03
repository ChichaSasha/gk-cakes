---
name: gk-plan-review
description: Independently review GK Cakes design artifacts and Trello-ready tickets for requirement coverage, coherent integration, testability and Creator setup readiness without implementing or publishing.
---

# GK Cakes planning review

Read the original user requirements/brief, applicable `AGENTS.md`, the precise
artifact revision, [output contract](../gk-plan/references/artifacts.md),
[ticket contract](../gk-plan/references/trello.md), `SETUP.md` and
`docs/planning/DELIVERY.md`. Read complete artifacts, not just the author's summary.
Use a fresh subagent/context for independent review. If author discussion is
inherited, label the review author-informed; if you authored the artifact, disclose
self-review. Neither is an independent review.
Apply the [model and context check](../gk-plan/references/execution-check.md).
Check the worker's model-selection evidence, input revisions and readback, and
verify your own history isolation. Unknown model identity is not a confirmed match;
compaction or being told to forget author discussion is not independent review.

Do not edit files or publish comments/cards unless separately assigned fixes or
publication. Current board contents are evidence, not permission to change it.

## Review by attempting to execute the plan

- Follow each brief requirement into screen behaviour, owned data, contracts,
  tests and tickets. Check actual design references; label missing assets.
- Trace one complete browse → availability → checkout → payment → fulfillment
  journey and one inspiration → Instagram → manual quote/order → payment journey.
- Trace cancellation racing production start; concurrent full/partial refunds;
  two customers choosing the last capacity/pickup slot; moving pickup at the cutoff;
  price/recipe/policy changes; late or duplicate provider events; provider timeout;
  email outage and worker restart. Check authorization and admin recovery.
- Reconcile names, state enums, amount/currency/rounding rules, timestamp/timezone
  meanings, identifiers and ownership across features. Identify conflicting
  request/response examples, absent consumers and hidden distributed transactions.
- Verify platform claims have primary evidence and planned account/device tests.
  Do not accept “DM sent”, “refund received” or “email received” from UI intent.
- Check separate production/pickup calendars, immutable order commitments,
  authoritative payment/refund metrics, ingredient forecast assumptions and
  exposure of public versus private data.
- Assess actual customer clarity and chef workload, responsive/a11y behaviour,
  error recovery, photography/content provenance and honest price/availability.
- For visual-design work, inspect the rendered sketch and editable source against
  the [design-session criteria](../gk-feature/references/design-sessions.md): colours,
  buttons, responsive states, actual Creator feedback/approval and integration.
  A prose spec is not a ready sketch. Keep visual approval and technical review
  separate; neither alone proves the other or a working application.
- Verify each proposed service/package has a current purpose and operating owner.
  Check provider costs, limitations, recovery and backups rather than trusting
  free-tier claims from the old draft.
- Validate ticket grouping, stable IDs, priority rationale, topological order,
  dependencies, safe parallel groups, Creator/Agent separation, test-first steps,
  integration evidence, release/rollback and Ready versus merely specified status.
- Validate PR/main/release gates, no deploy-on-every-push, separate UI/backend
  compatibility, one controlled migration execution and safe secret boundaries.
- Confirm SETUP has actionable provider-specific instructions for accepted choices,
  verifiable Creator actions, safe credential locations and dependent ticket links.

## Report

Return findings in severity order: blocker (unsafe, contradictory or not executable),
major (missing required behaviour/evidence), minor (local clarity). Each finding
needs a precise path/section or card, trigger, observable consequence and practical
correction. Distinguish optional improvements from readiness requirements.

Include requirement-coverage gaps, dependency cycles/orphans, stale artifacts,
unresolved decisions, and which checks were actually performed. Give a verdict
for each independently usable packet: ready for ticket publication, revise, or
blocked on a named decision. Overall publication readiness does not mean every
future implementation ticket can start today.

After fixes, inspect the new revision and affected dependents. Do not reuse a
verdict for changed contracts. An accepted design is not tested application code,
a merged PR or a production deployment.
