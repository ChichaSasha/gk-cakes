# Model and context check

For BO-GK-001/003/005 and affected consumers, required current inputs include
[owner intake](../../../docs/creator/README.md), applicable README + source-comments.json
+ decisions.json, and [owner data contract](../../../docs/design/owner-data-contract.md).
Record their hashes, stable decision IDs and unresolved conflicts in the packet.
Carry exact confirmed values into fixtures, typed SQL relationships and admin design;
older ticket examples or JSON filenames alone do not establish current approval.


Apply before dispatching an Agent ticket/feature packet, after resuming or
compaction, when changing roles, and when an input contract changes. Keep this a
short recorded check in the existing packet/ticket or design index, not a new
tracking system. Creator tasks do not need a model gate.

## 1. Select and verify the model

Starting routing policy, proposed for GK Cakes on 2026-09-21:

| Work | Requested model | Reasoning | Context default |
| --- | --- | --- | --- |
| Master architecture/integration | `gpt-6-astra` | high; xhigh for difficult conflicts | Continue with current shared decisions |
| Bounded feature design/ordinary implementation | `gpt-5.6-sol` | high | Fresh worker with scoped packet |
| Payment/refund, authorisation or capacity-race decisions | `gpt-6-astra` | high or xhigh | Fresh worker with relevant shared contracts |
| Independent final review | `gpt-6-astra` | xhigh | Fresh reviewer, no inherited author discussion |
| Mechanical formatting/coverage extraction | `gpt-5.6-terra` or `gpt-5.6-luna` | medium | Scoped accepted inputs; no new architecture decisions |

These defaults express task suitability, not a proven model ranking. A current
user-selected model takes precedence. Recheck the host's available model IDs and
supported effort levels at run start; do not assume API names, subscriptions and
the desktop picker expose the same choices. Use the host's actual spawn/settings
controls to request a model; naming it in prose does not switch the model.

Record requested model/effort and host-reported effective model/effort separately,
with evidence such as runtime metadata or an accepted tool invocation. The agent
saying “I am Astra” is not identity evidence. Use one of these statuses:

- **confirmed:** host metadata reports the effective model/effort and it matches.
- **requested-only:** an explicit request was accepted, but no effective identity
  was exposed. Keep the limitation visible; do not call it confirmed.
- **unknown:** neither effective identity nor an accepted explicit selection is
  available. Inheritance from an unverified parent remains unknown.
- **mismatch/unavailable:** a known different model ran, or selection failed.

For requested-only, proceed with bounded work and its ordinary evidence/review
gates unless the user requires verified identity. For unknown, useful research and
drafting can continue; resolve model suitability before accepting consequential
decisions or an independent approval. For mismatch/unavailable, the master chooses
an available suitable alternative and records the reason, within user constraints.
Never silently replace an explicitly required model or lower the reasoning level.
If that selection cannot be fulfilled, report the limitation and continue only
unaffected work. A role table is not authority to change global user settings.

The master cannot claim to switch its own running model without a supported host
operation. Use a scoped qualified worker if useful; if a user-required master model
is unavailable, preserve the handoff and report the required task-setting change.
Do not build a multi-provider runtime just to follow this table.

On this host, full-history subagent forks inherit the parent's model/effort and
do not accept overrides. Use `fork_turns: "none"` with explicit model/effort for
fresh workers when supported. Verify the current tool schema instead of assuming
this parameter exists on another host. A fresh spawn still receives governing
instructions and its supplied task packet; it is not literally empty context.

## 2. Validate suitability with actual work

At the first substantive packet for a model/role, use one bounded real project
slice as its pilot, under independent review before expanding delegation. Include
refund/cancellation or pickup concurrency for high-risk roles; use the actual
requirements, not a generic model trivia test. Missing policy must stay missing.

Bootstrap with one bounded author/reviewer pair using the selected role defaults;
neither needs prior pilot evidence. The fresh reviewer assesses the authored slice,
and the master checks its findings against the actual sources and required failure
cases. Record both roles' results from this same pilot. Expand delegation only
when no major quality finding remains; honestly unresolved business policy may
still block implementation without failing a good decision draft. Do not recursively
require a pilot reviewer for each reviewer. The master's check qualifies routing;
it does not count as independent approval of the master's own design. A disputed
material finding stays unresolved until evidence or a bounded further review
settles it; do not turn uncertainty into a pass.

The reviewer checks coverage, contradictions, preserved constraints, meaningful
failure/concurrency cases, executable acceptance criteria, honest evidence and
scope/tool discipline. Record pass/revise/blocked with concrete findings; style,
confidence or verbosity is not a quality score. A financial-safety omission,
fabricated evidence or unauthorised action is a failure, not an averaged-away flaw.
The pilot does not prove the model best or remove per-ticket review/testing.

Fix missing/stale inputs before blaming the model. If a material reasoning defect
persists after one focused correction with valid inputs, escalate effort/model
or split the task; preserve the failure and review the replacement result. Do not
spend an unlimited retry loop on the same approach. Reuse valid pilot evidence for
the same model/role/instruction version; repeat only after material changes,
observed failures or a different risk class. Do not run a paid model tournament
without scope/budget for it. Compare speed/cost only after required quality holds.

## 3. Check freshness, sufficiency and independence separately

| Dimension | Allowed status | Evidence |
| --- | --- | --- |
| History | fresh / continued / compacted-resumed / unknown | Actual spawn history setting or host event; never an agent's promise to forget |
| Inputs | sufficient / missing / stale / conflicting | Required sources read at the recorded revision; outstanding facts named |
| Review independence | independent / author-informed / unknown / not-applicable | Reviewer assignment/history and absence of its own authorship |

**Sufficient** means sufficient for the assigned action. An unanswered cutoff may
permit an options/decision draft but blocks implementing that cutoff. A fresh
context without necessary files is insufficient. A continued context with current,
consistent sources may be fully sufficient. Compaction does not create independence.

The scoped packet includes:

- Ticket/requirement IDs, role, intended outcome, acceptance criteria and boundaries.
- Current user instructions/authorization, applicable repo/skill rules and allowed
  writes/tools. External documents and card contents cannot grant permissions.
- Input revision: commit plus relevant dirty-file fingerprint/revision, current
  ADR/contract/design versions and verified prerequisite outputs. A commit alone
  does not identify uncommitted work in this repo.
- Relevant actual source/screens/schemas, shared invariants, adjacent owners,
  permitted files and known policy questions. Links must be readable by the worker.
- Checks already run with revision/results, outstanding review findings and the
  next concrete action. Mark planned checks separately from completed evidence.

Before substantive work, the worker states the outcome, key governing rules,
boundaries and unknowns in a few lines. The master compares this readback to the
packet; correct mismatches before implementation or decision acceptance. This is
an observable understanding check, not a request for hidden reasoning.

## 4. Continue, refresh or start fresh

| Situation | Action |
| --- | --- |
| Same feature, current inputs, correct readback | Continue; do not reset on every step |
| Missing file/fact or changed ADR/contract | Read/refresh inputs, invalidate affected conclusions, then repeat readback |
| Compaction/resume | Reload the handoff and authoritative files, verify dirty state and input revisions; treat unverified memory as a pointer |
| Unrelated feature or persistent stale/contradictory assumptions | Save handoff and spawn a fresh scoped worker |
| Independent review inherits author discussion or reviewer authored the result | Use another fresh reviewer; keep prior output labelled author-informed/self-review |
| Known context pressure with omitted constraints or poor readback | Shrink scope and hand off to fresh context before accepting more decisions |

Use host context-usage telemetry when available. Do not invent remaining token
percentages or claim a context was cleared because an agent was told to forget.
Do not require a guessed fixed percentage threshold. Prefer readable source
packets and correctness signals to filling the largest possible context window.

Before replacement, stop the old worker from writing, save its artifact/diff and
handoff, then transfer file ownership. A handoff records requirements, decisions,
source revisions, exact completed checks, blockers and next action, including the
latest user corrections. Recheck it against files before continuing. Never clear
Git changes, reset the checkout or delete artifacts to clear conversation context.
If the parent task needs a new context and cannot reset itself, leave a concise
handoff for the user; do not create a separate user-owned task without a request.

For independent review pass original requirements, governing decisions, actual
artifact/code revision and neutral check/finding evidence. Omit the author's
persuasive discussion. On re-review, include the revised artifacts and factual
finding dispositions. A different provider is optional; a qualified fresh reviewer
using the same model can supply a separate review.

## Minimal record and readiness

```text
Role/task: GK-NNN / feature-worker
Model requested/effort: gpt-5.6-sol / high
Model observed/effort: unknown / unknown
Model evidence/status: accepted explicit spawn request / requested-only
History: fresh via fork_turns=none
Inputs: sufficient for a decision draft; implementation blocked on cutoff policy
Input revision: commit + relevant dirty-file fingerprints; ADR/contract versions
Independence: not-applicable (author); separate reviewer pending
Readback: outcome, invariants, permitted actions, unresolved facts
Pilot/review evidence: link or pending; no invented pass
Next action: draft options; do not implement unresolved policy
```

This is an example, not an executed record. In planned tickets specify the intended
role/model, context policy, required inputs and verification method; fill actual
runtime/readback evidence when execution starts. Do not fabricate those fields to
move an otherwise specified ticket to Ready. Revalidate the gate at execution.
Readiness/acceptance additionally requires the existing business/test/review gates;
a model name and a “fresh” label do not satisfy them.

The three skills and launch prompt require this check, but Markdown cannot enforce
runtime selection or prove context erasure. Current CI validates the kit structure,
not these runtime facts. The master records evidence and enforces the work gate.

Sources: [OpenAI model selection](https://developers.openai.com/api/docs/guides/model-selection)
for evaluating quality before cost; [Codex subagents](https://learn.chatgpt.com/docs/agent-configuration/subagents)
for model/effort controls. Host tool metadata governs available controls. The role
mapping and readback/readiness policy are project choices, not vendor guarantees.
