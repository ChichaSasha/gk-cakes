# Required planning artifacts

Create these during the planning loop, not while installing the kit. Use Markdown
in Git, Mermaid for compact diagrams, and machine-readable API/schema files when
the payload detail warrants them. Paths below describe future output, not existing
documents. Link actual files and screen IDs after creation.

## Five design deliverables

| Output | Minimum useful content |
| --- | --- |
| `docs/design/screens.md` | Inventory of customer/admin screens and actual design references; actions mapped to requirements, data, validation, permissions, all UI states, accessibility and next steps |
| `docs/design/architecture.md` | C4 context/container diagrams; UI/admin/backend, owned data, jobs, providers, trust boundaries and deployable units; operating/cost/recovery assumptions |
| `docs/design/features/<feature>.md` | Purpose, responsibilities, non-responsibilities, owned data, business rules, dependencies, screen mapping, technical design, performance needs and tests for each module/service |
| `docs/design/contracts.md` plus schema files as needed | Shared vocabulary, API/event definitions, examples/errors, transition tables and key sequence diagrams; producer/consumer ownership and compatibility |
| `docs/design/adr/NNNN-<decision>.md` | Short decision records for consequential choices, with status, options, evidence, decision owner, consequences and supersession |

“Service” may be a module inside one backend. Do not turn documentation boundaries
into separately operated microservices without a demonstrated requirement.
Context and container views normally establish the initial architecture; add deeper
views only when they explain a real question. [C4 diagram guidance](https://c4model.com/diagrams)

## Screen mapping row

Include: `screen ID | design link/version or proposed | actor/action | requirement
IDs | input/output data | validation | permission | rule/contract | loading/empty/
success/error/recovery | accessibility | verification | ticket`.
Separate fixed-price purchase, request-for-quote and reorder actions in the UI.
Show availability early and total price before commitment. Include chef workflows
and manual/provider-recovery states, not only customer checkout.

If designs exist, inspect them and annotate behaviour gaps. If they do not, create
proposed screen inventory, representative mobile/desktop flows and a prototype
review ticket before claiming design readiness. Link actual artifacts; never
substitute invented screenshots, file paths or Figma URLs.

Visual work includes [iterative sessions with the Creator](../../gk-feature/references/design-sessions.md).
Deliver actual coloured sketches with buttons, editable source and viewable
previews, not only the screen mapping document. Link screen IDs to the approved
sketch version and its feedback/approval evidence. Save session artifacts under
`docs/design/sessions/<flow>/`; session criteria govern visual readiness.

## Data, rules and communication

For each entity define its owner, key, constraints, relationships, retention and
history/snapshot purpose. Distinguish database storage from customer-visible data.
Explain transaction boundaries, lock order/unique constraints and reconciliation
where concurrent operations could charge, refund, consume or reserve twice.
Index actual query shapes rather than every column. Include migration/restore
needs and fixture ownership. Avoid storing redundant provider or personal data.

For each communication contract define:

- Producer, consumer, auth/role/ownership check, transport, version and purpose.
- Request/response/event schema with realistic examples, required/optional/null
  semantics, validation, money currency/unit, time format/timezone and identifiers.
- Error code, customer/admin meaning, retryability and recovery action. For HTTP,
  include status; do not expose provider/SQL secrets in messages.
- Idempotency identity, scope, fingerprint, retention/reuse behaviour, conflicting
  requests, duplicate events and late/out-of-order delivery handling.
- Operation deadline, provider timeout, bounded retry policy/backoff and ownership
  after unknown outcomes. Proposed numbers need rationale and validation plan.
- Atomic local transaction versus external effects, durable intent/inbox if
  needed, recovery after crash, reconciliation and what the user sees while pending.
- Compatibility across separately released clients/backend; breaking change,
  migration and rollback strategy. Assign every event a consumer or omit it.

Draw sequences for checkout/payment/expiry, cancellation/refund versus production,
pickup changes, custom-request conversion and notification dispatch/recovery.
Show at least one consequential failure branch in each relevant sequence.

For each module name the performance-sensitive operations and propose budgets
with workload assumptions, measurement method and approval status. Include
provider latency and image delivery in the user experience. Do not claim an SLA
or guarantee correctness from a process-local lock or a mocked integration test.

## ADR format

```text
ADR ID and title
Status: proposed | accepted | blocked | superseded
Date, owner/approver, affected requirements/modules/contracts
Context and constraints
Options considered, evidence links/date and uncertainty
Decision and why it fits the complete system
Consequences: cost, UX, security, operations, alternatives rejected
Verification/spike and reversible or migration path
Supersedes / superseded by; dependent artifacts/tickets to revisit
```

Use ADRs for stack/providers, auth/data access, payment/refund coordination,
scheduling/capacity, Instagram, notifications and release strategy where material.
Do not write one ADR per helper function or treat research as Creator approval.

## Traceability and completion

Maintain `docs/design/coverage.md` with one row per brief requirement:
`requirement | screen/action | feature/rule | ADR status | contract | planned test
and environment | ticket ID/link | gap or approved deferral`.
No blank coverage disguised as “handled by backend”. Preserve the full scope
even when delivery is phased. Tests are plans until executed against a revision.

Maintain `docs/design/PLAN.md` with input revisions, accepted decisions, open
questions, artifact index, dependency DAG, display order, waves and explicit
parallel-safe groups. A DAG edge means prerequisite → dependent. Verify it is
acyclic and every referenced ticket exists. An order number is not a dependency.
Keep only the Trello identity mapping here; live execution state belongs on Trello.
Record the run's model/role selection and pilot evidence here; put each worker's
compact [model/context check](execution-check.md) in its packet. Keep actual model
evidence distinct from the requested configuration, and include context revisions
in handoffs so resuming does not revive superseded decisions.

Use `docs/design/review.md` for the current revision's review verdict, findings,
fix evidence and unresolved readiness blockers. Keep enough evidence to resume
without treating old review results as approval for a changed design.

The planning run is complete when all five outputs, coverage, reviewed ticket
packets, dependency order and Creator setup instructions exist and their status
is truthful. Unanswered policy choices mean affected items remain blocked; report
partial readiness instead of declaring the whole implementation ready.
