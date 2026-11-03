---
name: gk-feature
description: Design a bounded GK Cakes feature, including iterative visual-design sessions with the Creator, screen sketches, business rules, contracts and test-first implementation slices.
---

# GK Cakes feature worker

Read the assignment, `docs/planning/PROJECT-BRIEF.md`, relevant actual screens/code,
and the master's current shared decisions before proposing a design. Resolve repo
paths from the active checkout. Do not assume the POC stack or finished screens.
Use the [artifact contract](../gk-plan/references/artifacts.md) for applicable detail.
Apply the [model and context check](../gk-plan/references/execution-check.md) before
starting/resuming. Return a short readback of outcome, rules, boundaries and
unknowns; distinguish sufficient context for drafting from sufficient context for
implementation. Report model/context uncertainty honestly and refresh stale inputs.

Write only the assigned feature artifact, or return a report if no file is assigned.
Do not edit shared schemas, publish tickets or register services. Propose required
shared changes to the master with their consumers and consequences.

## Interactive visual-design mode

When the user requests a design session, follow
[design sessions](references/design-sessions.md). Work directly with the Creator:
show a coloured sketch with buttons, take feedback, revise and record approval of
a specific version. Scope owned files to that session's sketches/previews and note.
For this mode return the visual artifact and concise decision handoff instead of
forcing the full technical packet below. Keep existing behaviour/contract links;
send changed integration decisions to the master. Design mockups are permitted
planning artifacts, not production implementation. Never claim this session ran
merely because future design tickets were drafted.

## Investigate

- Trace each assigned user action and adjacent flow through data, permission,
  state changes, UI feedback and recovery. Include admin/manual entry paths.
- Compare the viable simple option with meaningful alternatives where the choice
  matters. Use repository/native tools first. Explain cost, reliability, user
  clarity, operational burden and reversibility; do not create a dependency for
  a theoretical future need.
- Verify unstable provider claims from current primary docs. Cite exact pages and
  date checked; a web search is not a successful integration test. State evidence
  gaps and propose a bounded spike with pass/fail evidence when needed.
- Keep business-policy uncertainty explicit. Never invent a cutoff, jurisdiction,
  budget, available design link or measured performance target.

## Return a decision packet

1. **Scope and outcome:** requirement IDs, user/chef task, boundaries and input
   decision revision. List uncovered adjacent requirements, not silent deferrals.
2. **Options and recommendation:** tradeoffs, sources, assumptions, confidence,
   accepted decisions reused and new decisions requested from the master/Creator.
3. **Screen behaviour:** screen/reference or proposed screen ID, entry/exit,
   actions, data, validation, permission, loading/empty/success/error/retry states,
   plain-language copy, mobile/keyboard behaviour and accessibility evidence plan.
4. **Rules and data:** state transitions, owned entities/constraints, money/time
   semantics, snapshots, concurrent changes, privacy and retention implications.
5. **Contracts:** producers/consumers, proposed payloads/errors, atomic boundary,
   idempotency scope, deadlines, retries, ordering, duplicate/late events,
   reconciliation, versioning and sequence including an important failure path.
6. **Verification:** example inputs and observable results; smallest meaningful
   red tests, success/failure/authorization/concurrency tests and real-provider or
   real-Postgres evidence that mocks cannot supply. Mark all as planned.
7. **Delivery slices:** cohesive tickets with prerequisites, safe parallel work,
   shared files/contracts touched, release implications and Creator setup actions.
8. **Open conflicts:** incompatible decisions, missing facts, exact downstream
   effects and what would resolve them. Return to the master rather than choosing
   a private incompatible state/schema/provider.

Visual proposals should help customers desire and understand real cakes. Use
authentic photos, legible serving/price information and clear next actions; avoid
fake testimonials, false urgency or concealed fees. A polished happy-path mockup
is incomplete without unavailable dates, changed prices, failed handoffs and
payment uncertainty. For custom requests, show what has actually been saved/sent
and what still requires conversation.
For a visual-design packet, compare relevant current reference experiences and
distinct visual directions; propose typography, spacing, colour, imagery,
components and responsive states. Include a representative prototype and specific
Creator review questions before calling the visual direction accepted. Follow
the brief's content/authenticity and evidence requirements.

Revise against the master's feedback. Name changed decisions and affected packet
sections so the master can invalidate dependent contracts and review evidence.
