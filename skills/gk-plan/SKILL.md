---
name: gk-plan
description: Coordinate GK Cakes feature design, architecture reconciliation, independent planning review, Creator setup and ordered Trello tickets. Use for the project planning loop, not application implementation.
---

# GK Cakes planning master

Resolve repository paths from the active checkout root. Read `AGENTS.md`,
`docs/planning/PROJECT-BRIEF.md`, `docs/planning/DELIVERY.md`, `SETUP.md` and the
original dry draft before planning. Current user instructions and the brief
supersede contradictory POC scope. Read [output requirements](references/artifacts.md)
for design work and [Trello requirements](references/trello.md) for ticket work.
Before dispatch/resume, apply the [model and context check](references/execution-check.md).
Record requested versus observed model, input freshness/sufficiency and reviewer
independence; verify each worker's short readback before consequential work.

## Establish the run

Inspect working tree, actual code/tooling/screens and current board before assuming
anything exists. Record input revision/dirty files and whether this run authorizes
Trello writes. Drafting skills never grant publication permission themselves.
Use the selected scope; a kit-creation request is not a full planning run.

Reuse the three `skills/work-*` engineering methods when relevant. GK Cakes is
personal: Trello replaces Jira, and the requested native auto-merge delivery
contract replaces workplace manual-merge policy. Do not edit shared work skills.
The master accepts reversible technical decisions with evidence; business policy,
spend, identity/account ownership and major scope choices need Creator input.

Create `docs/design/PLAN.md` as the concise design index, decisions/blockers and
resume point. Trello remains the execution-status source; do not create a competing
status tracker. Record IDs and artifact mappings so another context can continue.

## Coordinate bounded workers

For an invoked full planning loop, delegate independent packets to
[gk-feature](../gk-feature/SKILL.md). Respect actual concurrency slots; one master
and up to three workers is a starting arrangement, not a requirement to use all.
Group domains according to dependencies, for example:

- Customer experience, catalog/inspiration, custom requests, accounts/favourites.
- Orders, production/pickup capacity, checkout, discounts/refunds, delivery.
- Chef operations, recipes/inventory, email/audit/metrics and platform delivery.

Split these further only when the boundary permits independent decisions. Start
with the shared vocabulary/state/ownership skeleton before detailed parallel work.
Workers may investigate shared questions; only the master changes shared contracts.

Every assignment includes requirement IDs, actual screen/code references, current
decision revision, known constraints/open questions, adjacent owners, expected
output, requested model/effort, context policy and permitted files. Use the role
defaults and pilot/escalation rules in the execution check; never infer model
identity from the worker's prose. Assign separate `docs/design/features/<feature>.md`
files or request a report; never give two workers the same writable file.
Workers cannot create Trello cards, register services, modify global architecture
or silently delegate extra work. Use host subagents, not user-owned task creation.

Require each worker to state options and recommend the smallest complete design,
with primary evidence for unstable provider facts. New packages/services must
solve a current requirement. Reusing existing tools does not mean preserving a
POC decision that fails the complete scope.

## Reconcile, then review

1. Merge proposals into shared vocabulary, entity ownership, state transitions,
   money/time rules, APIs/events, dependencies and operational boundaries.
2. Maintain a decision register with ID, status, evidence, affected features and
   rationale. Statuses: proposed, accepted, blocked, superseded. Master acceptance
   is technical acceptance, not fabricated Creator approval. Record approver.
3. Return incompatible proposals to their workers with the exact conflict and
   governing contract. When a shared decision changes, mark dependent artifacts
   stale and update/re-review them before advancing their tickets.
4. Walk complete customer and chef scenarios through actual proposed messages,
   database transactions and failure recovery. Cover the brief's invariants.
5. Give a fresh [gk-plan-review](../gk-plan-review/SKILL.md) reviewer the original
   requirements, exact artifact revision and complete packet, without a persuasive
   author summary. Verify actual history isolation, not a request to “forget”.
   Fix actionable findings and re-review affected output.

If no separate reviewer is available, disclose self-review and leave independent
approval pending. Stop a repeated unresolved cycle when a specific external fact
or Creator decision is needed; continue unaffected work. Never turn guessed facts
into accepted choices merely to finish a loop.

## Build an executable backlog

Include [Creator design sessions](../gk-feature/references/design-sessions.md)
as first-class planned work. Sequence the shared visual direction and bounded
screen/flow sessions before their affected UI implementation. Agent-owned design
tickets produce visible coloured sketches/buttons and iterate with the Creator;
Creator approval of an exact revision is required. Do not substitute an autonomous
agent verdict for that feedback. Keep affected work pending while allowing
independent backend/contract work to continue. Disposable visual prototypes are
in scope for planning, without building or deploying application features.

Derive tickets from reviewed vertical outcomes, grouping common work. Put schema,
API, UI, meaningful tests and operational evidence together where they deliver
one outcome. Separate work with independent prerequisites/rollout or a human owner.
Record requirement → screen → module/contract → test → ticket coverage.

Assign a stable local ID, priority rationale, display order and dependency wave.
Dependencies determine execution order; numbers and priority alone do not.
Mark safe parallel work only when contracts are agreed and files/data/release
ownership do not conflict. Later waves can be fully specified but still Backlog.
Never send a worker a ticket that depends on an undecided interface as Ready.

Update `SETUP.md` when a provider/operating decision is accepted. Create separate
Creator-owned setup/decision tickets with prerequisites and verification evidence;
do not ask the Creator to register every candidate vendor.
Publish only within the current run's authorization using the Trello reference.

## Finish and resume

Verify all requirement IDs have coverage, all dependency edges resolve without
cycles, all ready tickets have observable acceptance/test steps, and all five
design outputs plus setup/delivery instructions are usable. Distinguish completed
design checks from future implementation tests and provider sandbox checks.
Return artifact links, real Trello links, accepted/proposed decisions, parallel
waves and the next Creator action. Save input/output revisions and unresolved
dependencies in the design index; revalidate only changed areas on resume.
