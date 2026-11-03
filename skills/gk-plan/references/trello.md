# Trello publication and ticket contract

Target: [GK Cakes 🍰 AI Workflow Board](https://trello.com/b/QEjqyM9i), board ID
`6ab074a2b3c3763832ddf623`. Snapshot verified read-only on 2026-09-21:

| List | ID | Meaning |
| --- | --- | --- |
| 📥 Backlog | `6ab074adb3c3763832de0077` | Draft, waiting for a decision or prerequisite |
| ✅ Ready | `6ab074adb3c3763832de0087` | Fully specified and executable now |
| 🚧 In Progress | `6ab074adb3c3763832de0099` | Work started |
| 🔎 Review | `6ab074aeb3c3763832de00ad` | Evidence ready for review/acceptance |
| 🏁 Done | `6ab074aeb3c3763832de00c3` | Finished and accepted |

Existing labels were Template, Milestone and Example. Two existing reference
cards were “Instructions (open this card)” and “Approved workflow states”. Preserve
them. Re-read lists/labels/cards at run start; snapshot IDs are not live proof.
The board's instruction card requires approval before Done. CI/auto-merge alone
does not prove product acceptance or production deployment.

## Labels and sequencing

Reuse exact matching labels, creating missing ones only when authorized:

- Exactly one ownership label: **Owner: Creator** or **Owner: Agent**.
- Exactly one priority: **P0**, **P1**, **P2**, with rationale in the description.
- One kind: **Kind: Implementation**, **Kind: Setup**, **Kind: Decision** or
  **Kind: Design**. Use **Blocked** in addition when a decision/dependency prevents work.

Creator means the human owner/chef doing setup, supplying content or deciding
business policy. A customer user story still has an Agent implementation owner.
Do not infer Trello member IDs or assign deadlines/estimates without evidence.

P0 = safety/correctness or foundational blocker for the first usable flow;
P1 = other agreed launch requirements; P2 = explicitly agreed later improvements.
The master proposes priorities; user requirements are not silently downgraded.

Title: `[GK-012][P1][O020][W02] Enable customer pickup rescheduling`.
`GK-012` is a stable planning key, not a fabricated Trello ID. `O020` is sortable
display order; gaps allow insertion. `W02` is a dependency wave, not a date.
Use numeric metadata in the body too; no paid custom fields are required.
Within a wave list explicit parallel-safe groups (for example W02-A and W02-B),
shared contracts/files and integration owner. Same wave alone never grants
parallel execution. Sort by dependencies, then priority and display order.

## Self-contained ticket template

Save complete drafts under `docs/design/tickets/GK-NNN.md` for publication. After
publishing, Trello is the live execution record; mark local copies as snapshots,
record the verified URL and avoid maintaining a second live status history.

```markdown
# [GK-NNN][P1][O020][W02] Action and user-visible outcome

## Context
Owner: Agent or Creator; Kind: Implementation/Setup/Decision/Design
Priority: P1 — reason; Order: 20; Wave: 2; Parallel group: W02-A or none
Requirements: concrete IDs; Design/contract revision: exact input
Agent execution policy: role + requested model/effort + context policy
Required context: exact requirements/ADRs/contracts/files and prerequisite evidence
Execution check: method/reference; actual model/history/readback evidence at start
Depends on: stable IDs + actual card links, and exact deliverable needed
Unblocks: IDs; Safe in parallel with: IDs + why; Conflicts with: files/contracts
Readiness: ready now / specified but dependent / blocked and exact question

Current and desired behaviour; customer/chef reason; scope boundaries.
Embed the applicable rules, screen action, state transitions, payload examples,
constraints, permission, money/time semantics and important failure outcomes.
Links supplement this content; do not say only "implement the design".
Include setup prerequisites and nonsecret config names/locations.
For Agent work, apply the model/context check before execution and after resuming;
record observed model status, current input revisions and correct readback.

## To dos
T1. Specify/run the smallest failing behavioural check (AC1, AC2); state fixture,
    input, expected failure and command or exact bootstrap prerequisite.
T2. Implement the coherent data/API/UI behaviour (AC1–AC4), including validation,
    transaction/idempotency, user feedback, permissions and recovery.
T3. Integrate producer/consumer, migration and operational handling (AC3–AC5).
T4. Run green checks; refactor; rerun affected tests. Capture revision/results.
T5. Complete review and delivery evidence applicable to this slice (AC6).

## Not to dos
Only meaningful boundaries; link separate follow-up work without omitting safety
or correctness necessary for this outcome. Omit this section when unnecessary.

## Acceptance Criteria
AC1. Given [fixture/actor], when [action], then [observable result]; verify by
     [test/command/environment]. Map every T item to an AC and every AC to scope.
AC2. Invalid/unauthorised request yields [specific result] and no prohibited change.
AC3. Relevant duplicate/concurrent/timeout case yields [specific safe outcome].
AC4. UI shows [loading/success/error/recovery], including mobile/keyboard checks.
AC5. Integration/migration/recovery has [specific observable evidence].
AC6. Required tests and review for the tested revision pass; PR/merge/main CI and
     release evidence are recorded where applicable. Deployment is separate.
```

Replace every bracketed field before calling an implementation ticket Ready.
If no stack/test runner exists, depend on a bootstrap ticket that supplies exact
commands; keep the dependent card in Backlog until the command is known/verified.
An infrastructure failure is not a successful red test. Use real PostgreSQL for
transaction/locking claims and provider sandbox checks for integration behaviour.

Creator tickets use the same Context/To dos/Acceptance Criteria shape, with
concrete human steps and verification instead of a forced red/green code cycle.
Agent model/context fields are not applicable to Creator tickets. For Agent tickets
use the [execution check](execution-check.md); future runtime evidence is filled
at execution, not invented during planning or required before dispatch exists.
Example: verify sending-domain DNS → send controlled email → inspect outcome;
record configuration/secret names and evidence, never secret values.

## Interactive design tickets

Use **Owner: Agent** + **Kind: Design**, naming the Creator as visual reviewer.
Group a coherent screen/flow and its iterations in one ticket; do not create a
card for every feedback comment. Use Context / To dos / Acceptance Criteria with
the [design-session acceptance criteria](../../gk-feature/references/design-sessions.md).
Replace implementation red/green steps with sketch → show → feedback → revise →
Creator approval → integration review. Include actual colours, buttons/actions,
mobile/desktop previews, editable source and the exact approved revision.

Keep the card in Review while awaiting Creator feedback; missing design approval
blocks affected UI implementation. Design approval does not imply payment/API
tests ran. Add separate Creator-owned decision/content tasks only when needed;
independent backend work may proceed on settled contracts. Reference the approved
sketch version as an implementation prerequisite and acceptance comparison.

## Example acceptance detail: cancellation/refund

This is an illustrative slice, not a ready policy decision. First resolve eligible
states, production-start meaning, payment provider and refund allocation in ADRs.
Then embed the resolved values in the card and test these concrete outcomes:

- Customer owning a paid, eligible order cancels; order transition, audit and
  durable refund intent commit together; UI says refund pending until confirmed.
- Another customer cannot cancel/read it. Completed and in-progress orders reject
  customer cancellation according to the approved policy; admin exceptions are separate.
- Cancellation racing production start yields exactly one permitted outcome;
  no refund is created if production-start wins under that policy.
- Repeated action/provider timeout reuses one refund operation. Concurrent partial
  and full refunds reserve pending amounts and cannot exceed captured funds.
- Provider failure/unknown outcome remains actionable in admin; customer email
  distinguishes requested/initiated from confirmed refund; retry cannot pay twice.

Do not split refund correctness and its recovery into an optional future ticket.

## CLI and publication safety

Use `/Users/sasha.chycha/go/bin/trello`, never `trelloctl`. Use installed command
help before operations. Credentials already live in macOS Keychain; authenticated
calls may need normal sandbox escalation. Never export credentials or ask for a
new login solely because the sandbox cannot reach Keychain. Capture stdout/stderr
and redact `key` and `token` URL query parameters before displaying any error.

Read commands (wrap output with that redaction policy):

```text
trello boards list --pretty
trello lists list --board 6ab074a2b3c3763832ddf623 --pretty
trello labels list --board 6ab074a2b3c3763832ddf623 --pretty
trello cards list --board 6ab074a2b3c3763832ddf623 --pretty
```

The verified installed CLI supports `cards create --list --name --desc --labels`,
`cards update --card --name --desc --labels`, and `cards move` (check its help).
Pass multiline descriptions as structured process arguments, e.g. Python
`subprocess.run([... , '--desc', path.read_text()], capture_output=True, text=True)`.
Do not interpolate Markdown into a shell command or assume a nonexistent file flag.

Only the master writes to Trello. First prepare/review all descriptions and the
dependency map. Read existing cards by stable planning key and preserve unrelated
content. Create missing labels, then cards in dependency order; reread each created
card and record its actual URL/ID before linking dependents. After all exist, fill
verified reciprocal dependency links and recheck content/labels/list.
Reread before updates to preserve human edits and unrelated labels; flag conflicting
edits instead of overwriting them. Do not delete/archive cards or restructure lists.

On timeout, search/re-read for the planning key before retrying. If multiple matches
exist or outcome cannot be established, stop that write and report ambiguity;
continue other safe work. Never repeat a blind create or claim sync from an attempted
request. If access is blocked, retain clearly unpublished local drafts and report
the limitation; do not call them Trello tickets already created.
