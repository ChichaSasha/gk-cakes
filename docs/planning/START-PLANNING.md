# Launch the planning loop

Copy the following prompt into a new task in this repository. The first prompt
authorizes scoped Trello publication; creating this file does not execute it.

```text
Use $gk-plan to run the GK Cakes end-to-end planning loop. If the skill picker
cannot resolve it, read skills/gk-plan/SKILL.md directly. Read AGENTS.md,
docs/planning/PROJECT-BRIEF.md, SETUP.md, docs/planning/DELIVERY.md and the original
innitial--unreviewed-dry-draft-poc-design.md. Treat the old design as unreviewed
proposals even where it says "confirmed". Preserve unrelated existing work.

Goal: design the complete customer and chef/admin system, then produce detailed,
self-contained, ordered implementation tickets. Make the customer experience
beautiful, appetising, clear and accessible; make daily admin tasks understandable
without technical knowledge. Translate those goals into reviewable screen flows,
prototype tasks and measurable observations, not unsupported conversion claims.

Include interactive visual-design sessions with me as first-class future work,
following skills/gk-feature/references/design-sessions.md. Plan Agent-owned design
tickets where we iterate on visible sketches: colours, typography, layout, imagery,
buttons and key states. Acceptance requires rendered mobile/desktop sketches,
editable source, defined button actions and my approval of an exact revision.
During an invoked session, show the sketch, collect my feedback and revise it;
do not replace this interaction with an agent-only design decision. Disposable
HTML/CSS mockups are allowed planning artifacts; production implementation is not.
Keep affected UI tickets pending until design approval and integration review;
continue independent work while waiting. Do not claim scheduled sessions are done.

You are the coordinating master. Explicitly use subagent workers for bounded
feature investigations and a fresh subagent for independent review. Use the host's
available agent slots; keep the master free to integrate. Do not create separate
user-owned Codex tasks or introduce an orchestration framework. Give workers the
current shared decisions and narrow file ownership; they propose, you reconcile.
If delegation is unavailable, disclose it and do useful drafting, but leave the
independent-review gate pending.

Apply skills/gk-plan/references/execution-check.md before dispatch/resume and
when input contracts change. Use its model-role defaults unless I select another
model, and request the model/effort through supported host controls. Verify model
selection from host evidence, not self-identification; label unknowns honestly.
Use fresh scoped workers and a reviewer without inherited author discussion.
Check separately whether context is fresh, sufficient/current, and independent.
Require a short task/rules/unknowns readback before consequential work. Run one
bounded real pilot before scaling, using the execution check's finite bootstrap
to qualify the author/reviewer pair; reuse valid evidence without recursive reviews.
Refresh missing/stale inputs first; escalate persistent reasoning failures, and
save a handoff before replacing context. Never reset Git to clear context.

First inventory actual screens/assets, code, tools, Trello cards and outstanding
decisions. Ask the few business/design questions that materially change scope or
correctness, while continuing independent work. Preserve every requested feature
in the coverage matrix. Stage delivery into useful waves without silently dropping
requirements or treating guessed policy values as final.

Use $gk-feature for worker packets and $gk-plan-review for independent review.
Compare credible options with evidence and costs before accepting material
architecture choices. Start by evaluating a modular monolith with PostgreSQL;
the old Go/provider choices are candidates, not a mandate. Separate UI/backend
deployment does not require microservices. Verify provider capabilities and prices
from current primary documentation. Mark unavailable evidence explicitly.

Produce these five linked design outputs:
1. Screen-to-behaviour mapping with actual screen references, actions, data,
   validation, permissions, loading/empty/failure/recovery states and accessibility.
2. C4 context/container architecture showing customer UI, admin UI, backend
   modules, data stores, jobs and external systems, with ownership and trust lines.
3. A specification for every module/service: purpose, owned data, rules,
   dependencies, operational needs and proposed performance budgets.
4. Communication contracts and sequences: payloads/errors, auth, timeouts,
   retries, idempotency, ordering, duplicates, versioning and recovery.
5. ADRs recording options, evidence, decisions, consequences and decision status.

Also produce the requirement-to-screen-to-contract-to-test-to-ticket coverage
matrix; a dependency graph with priorities, order numbers and safe parallel waves;
an updated SETUP.md for the Creator; and self-contained Trello ticket descriptions
using the provided template. TDD means both technical design documents and
test-driven implementation: tickets say which behaviour to test first and how to
run the red/green/refactor checks. Never claim tests ran during planning.

Resolve cross-feature conflicts before tickets become Ready. In particular review
production versus pickup scheduling, cancellation versus production-start races,
partial/full refund limits, payment/capacity expiry, discount arithmetic, recipe
history, custom-request-to-paid-order flow, authorisation, email delivery evidence,
and independently deployed frontend/backend compatibility. Keep user-facing,
financial, fulfillment and provider states consistent without conflating them.

Use the existing Trello board "GK Cakes 🍰 AI Workflow Board"
(https://trello.com/b/QEjqyM9i). You may create/update this plan's cards and its
documented labels, preserving existing unrelated content and workflow lists.
Only the master publishes. Creator-owned setup/decision tickets use Owner: Creator;
implementation tickets use Owner: Agent. These ownership labels do not mean
customer user stories. Group cohesive work, never one card per file or endpoint.
Every ticket includes stable ID, P0/P1/P2 rationale, sequence number, wave,
prerequisites, safe parallel work, relevant exact contracts/rules, ordered steps,
acceptance criteria, test plan, integration and completion evidence requirements.
Agent tickets also specify intended model/effort, context policy and required
input revisions; actual execution-check evidence is recorded when work starts.
Look up existing cards before writing; recover ambiguous writes by rereading,
never by blindly creating again. Return real card links only after verification.

Publish independently reviewed, decision-complete implementation tickets to Ready
only if their execution prerequisites are also complete. Put later-wave tickets
in Backlog with dependencies. Blocked drafts stay in Backlog labelled Blocked
and explain the exact decision needed. Creator decision tickets can be Ready
when their requested action is executable. Preserve the board's Done approval rule.

Follow docs/planning/DELIVERY.md: PR CI, required review/checks, native auto-merge,
CI on main, release-published CD, independent UI/backend release paths and safe
secret handling. Interpret "push only on release" as production deployment only
on a published release; ordinary branch pushes remain necessary for PR CI.
Flag this interpretation for the Creator before implementing the release workflow.

This run authorizes planning artifacts, research, subagents and the scoped Trello
writes above. Do not implement application features, register/buy services, change
GitHub settings, push/merge code, create releases or deploy. Make Creator tickets
for required human setup and decisions. Save safe nonsecret handoff evidence.

Run review → revise → re-review until no actionable integration/readiness blockers
remain, or a named external/user decision prevents progress. Never approve your
own changes as an independent review. Stop repeating a cycle that produces no new
evidence; report the unresolved issue and next action while finishing other work.
Conclude with artifact/card links, accepted versus proposed decisions, wave order,
parallel groups, Creator actions, verification performed, and remaining blockers.
```

For local-only planning, append this overriding instruction to the copied prompt:

```text
Override every Trello-write instruction above: Trello access is read-only in this
run. Save complete ticket drafts locally using the Trello template; publish no
cards, labels, comments or status changes. All other planning/review work proceeds.
```

To resume: “Use $gk-plan to resume the existing plan from its recorded input
revision and Trello mapping. Recheck changed decisions and affected tickets;
preserve existing IDs and completed evidence. Continue within the same scope.”
When starting a new task, restate whether Trello publication is authorized.

## Start a planned design session

After choosing a design ticket/flow, use this prompt, replacing the bracketed field:

```text
Use $gk-feature in interactive visual-design mode for [ticket or screen/flow].
Read its requirements, existing decisions and previous feedback. Show me a coloured
sketch with buttons, ask for focused feedback and iterate with me. Follow
skills/gk-feature/references/design-sessions.md. Save editable source and previews;
do not mark the design approved until I approve a specific revision. Keep this
session local; do not publish Trello changes or implement production features.
```
