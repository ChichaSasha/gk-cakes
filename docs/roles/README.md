# Developer and business-owner ticket context

Current owner answers for BO-GK-001/003/005 are captured in the
[existing intake](../creator/README.md) and [owner data contract](../design/owner-data-contract.md).
Load original comments and structured decisions before using the role packets below.
Their published instructions/examples remain historical source snapshots, not owner
answers or current defaults. Record the input revision used; proposed ticket changes
are separate in the contract. Owner attribution and incomplete/conflicting answers
are recorded in the intake; no parent completion or live update is implied.


Revision roles-r4-owner-publication, 2026-09-27; original role split 2026-09-26. The 30 Ukrainian owner contributions are now published on the user-selected [GK Business Owner Board](https://trello.com/b/cAQFJIOQ/gk-business-owner-board), with a [Ukrainian start guide](https://trello.com/c/oRYKmiyZ). The existing private board and five columns were reused. No membership, engineering-board, service or application-code changes were made.

## Read the right packet

| Audience | Packet | Scope |
| --- | --- | --- |
| Sasha, developer | [English developer requirements](developer.en.md) | 34 guided packets: concrete outputs, worked examples, verification, owner handoff and exact original acceptance criteria |
| Business owner | [Українські завдання та інструкція](business-owner.uk.md) | 30 business contributions, plain-language steps, required evidence and completion criteria |
| Agent coordinating both boards | [mapping.json](mapping.json) and this contract | All 34 source IDs/URLs, order/dependencies, 30 owner-card destinations, section markers and source/published-body hashes |

`DEV-GK-001` and `BO-GK-001` are local role-section keys, not new real Trello IDs. The existing GK-001 remains the engineering/overall ticket. These are role-specific views of existing scope, not a second implementation backlog. Original full ticket Context, mandatory extensions, acceptance tests and dependencies still apply. Developer packets reproduce source acceptance criteria for reference; refresh them when the source changes rather than letting copies drift.

The original label **Creator** combined two people. For this requested split:

- **Developer — Sasha:** technical design, service selection research and recommendations, configuration through authorized access, implementation/agent coordination, automated tests, technical review and evidence. Technical acceptance is a developer responsibility, subject to the actual reviewer/acceptance authority agreed in GK-006.
- **Business owner:** business rules, genuine content/food information, spend and business-account ownership decisions, personal verification, exact visual approval, business usability/acceptance and business launch decision within GK-006's agreed authority.
- **Agent:** an executor helping the developer, not another human owner or substitute for either person's approval.

Account ownership, billing payer, access grants, named incident contacts, delegated acceptance authority and release operator are still decisions in GK-004/006. This document assigns work categories; it neither invents those answers nor grants access. Website/admin product language is also unchanged: Ukrainian here is the collaboration language only. The owner does not need repository access, English technical vocabulary or model/hash worksheets.

Four source tickets have no standalone owner task: **GK-013, GK-019, GK-020, GK-034**. Their business inputs already come from the mapped policy/setup/design tasks. Technical completion still needs authorized human acceptance; the owner later evaluates the resulting product in GK-035. Do not silently weaken existing review or Done gates. A newly discovered business question returns to its original owner decision card.

## Developer output guidance

Each English packet explains the outcome, prerequisites, specific deliverables,
steps, a worked example, verification and human handoff. Start with its expected
outputs: decision tasks produce approved rules and cases; setup tasks produce
configuration evidence; design tasks produce reviewable sketches; implementation
tasks produce working behavior and test/delivery evidence. The guide's opening
table identifies where to put each result using the existing intake, design and
source-ticket records.

Examples describe the required quality and shape of a result. They are not
executed tests, approved policies, actual prices or permission to perform a later
task. Replace placeholders with evidence, preserve unresolved decisions and use
the full source Context/extensions alongside the copied acceptance criteria.
The developer prepares the Ukrainian explanation/demo needed by the matching
owner task and records both contributions; neither replaces the other.

## Owner card quality and developer preparation

Each Ukrainian card is now a standalone guided task: business purpose, a concrete
final deliverable, when to start, what the developer supplies, numbered actions,
a clearly illustrative example, a task-specific copyable answer form, completion
criteria and the next handoff. Technical source IDs are traceability only; she
must not need the English parent card or repository access to understand her work.

Before marking a review/setup contribution Ready, provide the actual cost proposal,
official service instructions, numbered mobile/desktop sketch or safe test example
promised by that card. Include working links, a version label she can copy, test
accounts/data via appropriate secure channels, and expected results. The owner
should not create faults, calculate technical concurrency, guess account settings,
write legal text unaided or assemble an end-to-end test from 30 parent tickets.
Prepare those materials, explain the choices and ask questions one at a time when
needed. Missing developer preparation is a blocker for the developer to resolve.

Partial answers and “please explain” are valid submissions, not completion.
Illustrative numbers/options do not become business defaults or approval.
Required unresolved items stay open; visual acceptance names the actual revised
version. For critical business/food/legal facts, preserve uncertainty until verified.
Owner-facing review does not replace technical tests or independent review.

## How the handoff works

1. Developer checks the live source ticket, its comments/attachments, current approved decisions and prerequisites. At the 2026-09-27 publication preflight, all 34 descriptions matched the saved publication and all mapped comments/attachment lists were empty. This dated check is not permanent proof of readiness or human approval; refresh actual evidence before execution.
2. Developer supplies a short Ukrainian question, cost proposal, screenshot/prototype or safe sandbox demonstration. For a review task, this deliverable makes the owner contribution actionable while the source ticket remains In Progress/Review. Never require the parent to be Done before asking for its own acceptance.
3. Owner answers in Ukrainian on her card (or sends a message with BO-GK-NNN/GK-NNN while no owner board exists). She attaches approved assets or accessible links, states version/date, separates approved/proposed/unknown and says what earlier revision is superseded. A developer may help record her words but cannot invent her approval.
4. Developer/agent checks access and records the original Ukrainian evidence plus its checked English interpretation under `docs/creator/GK-NNN/README.md`; assets stay in that existing ticket's folder. Record owner/source comment or attachment link, exact revision/date, usage rights and approved purpose. Technical configuration evidence is a separate developer section, not falsely attributed to the owner. Sensitive originals remain in approved secure storage.
5. Developer sends a brief Ukrainian readback for material ambiguity and gets clarification before adopting a changed rule. Accepted business meaning enters the shared decision/contracts/design records through the existing planning review process. Keep the original language as evidence; an English paraphrase cannot override it. Do not create a second durable inbox or silently overwrite earlier approval.
6. Record the owner contribution and technical contribution against the source card. Recheck all dependencies and acceptance criteria before a parent transition. File upload, owner-card completion, green CI, merge, production release and deployment remain distinct events.

Use the existing [Creator input instructions](../creator/README.md) and [combined order guide](../../START-HERE.md). Sources remain [ticket metadata](../design/ticket-plan.json), [published source mapping](../design/trello-map.json), full tickets and current live evidence. The original publication/review manifests are historical snapshots; entry-guide role corrections do not change the original technical design verdict.

## Published owner board and ongoing coordination

Destination: **GK Business Owner Board**, ID `6ab846cadeac31eff3e42507`. Publication read-back verified 30 tasks plus the permanent start guide. At publication, five initial-information tasks (BO-GK-001/003/005/007/002) were in Ready, the other 25 in Backlog, and the guide was first in Ready. Each populated list follows O order. These are publication facts, not a second live status ledger. Only Sasha's account was a board member at verification; the owner's identity has not been supplied and no invitation was sent. Grant access only to the explicitly selected person when sharing is requested.

The 30 blocks between `<!-- BEGIN BO-GK-NNN -->` and `<!-- END BO-GK-NNN -->` in the Ukrainian document supply the task cards. Each title is the card name and the remaining block is its full description, without boundary comments. English developer criteria and internal operational evidence stay off the owner board. The introduction supplies the separate start guide, with explanations of the existing English column names. Source links are traceability aids; each Ukrainian task remains usable without access to the technical board.

Existing columns and their Ukrainian meanings (names were preserved):

| List | Meaning |
| --- | --- |
| 📥 Backlog — Заплановано | Specified contribution awaiting its prerequisite or the developer's brief/demo |
| ✅ Ready — Можна починати | Required inputs are accessible and this particular owner contribution can start |
| 🚧 In Progress — У роботі | Owner is working on it |
| 🔎 Review — На перевірці | Submitted response/material awaits completeness/interpretation check |
| 🏁 Done — Виконано | Owner explicitly approved her required contribution and developer verified evidence/coverage; no unresolved required item |

For visual/product review cards, submitting feedback moves the contribution to «На перевірці»; required fixes must be resolved and the exact resulting version explicitly accepted before «Виконано». Comments alone are not acceptance. **BO-GK-036 is a decision exception:** a complete explicit approval or refusal can finish the owner's decision contribution. Refusal leaves release authorization denied; it cannot be interpreted as launch readiness or auto-complete missing parent requirements.

Use an optional **Потрібне уточнення** label and a concrete blocker comment rather than adding another workflow. Preserve O order within lists; W remains a dependency stage, not a due date. No invented deadlines or estimates. Do not mechanically copy the parent list or W into readiness: owner work may need an unfinished parent's prototype, quote or test demo. Initial policy collection can begin independently; BO-GK-004 needs the developer's dated provider proposal, BO-GK-006 an authority proposal. A decision task may start with questions; a configuration task requires its selected provider/settings first.

The map's `source_dependencies` are the **original engineering prerequisites**, not a claim that owner-only completion satisfies them. An owner card can stay blocked by a developer-only source such as GK-020, even though it has no corresponding owner card. Record that external prerequisite and the exact needed deliverable in the owner card; never fabricate an owner card for it. Design/implementation review contributions require their own developer output, **not completion of their own parent**. Bootstrap remains GK-020 B1 → GK-013 G1 → GK-020 B2. All other GK-020 consumers require B2.

### Status and evidence ownership

| Record | Authority |
| --- | --- |
| Existing engineering board | Overall technical/source-ticket progress and complete acceptance evidence |
| Owner board | Progress and evidence for the owner's contribution only |
| Repository packets | Requirements/context snapshots, source-to-role mapping and approved input references; no competing live status ledger |

No automatic synchronization is assumed or requested. Begin with manual coordination: when the owner submits/approves a contribution, the developer links its exact evidence and updates only the authorized affected cards. An owner Done card does **not** automatically finish or unblock its parent. A parent Done card does **not** fabricate an owner approval. A missing required approval or contradiction stops promotion and is surfaced; preserve existing history.

For a changed answer, record a new version and affected evidence/tickets. Reopen only affected owner contributions and invalidate affected parent acceptance under the authorized workflow. Resolved/Done cards are not silently overwritten with changed requirements. Technical-only acceptance routes through the developer; product/visual/business acceptance requires the owner or an explicitly recorded delegate. GK-036's business decision never executes a release by itself.

### Publication procedure and duplication checks

1. Re-read applicable instructions, the chosen existing/destination boards and all mapped source cards/comments/attachments. Compare source description and local-file hashes with this packet. Resolve changed source content; do not overwrite human edits with the snapshot.
2. Confirm the requested publication/sharing scope and actual member identity. Create or reuse the explicitly selected board; verify its privacy and access. These are future actions, not actions authorized by drafting this context.
3. Search by the exact local `BO-GK-NNN` key and source URL before creating. If the destination exists, reuse its real ID; multiple matches or an ambiguous failed write require a reread, not a blind retry. Use the installed `trello` CLI and safe credential redaction.
4. Reuse the five lists and maintain O order. Check each proposed entry condition against live evidence before placing it in Ready («Можна починати»). Do not label all packets Ready just because their text is complete. Avoid assigning members before their actual identities are verified.
5. Record returned destination board/card IDs and URLs in `mapping.json`; `null` means unpublished, never a placeholder URL. Link owner card to source. Add the reciprocal source link/comment only within explicit source-board write authorization, preserving existing description and labels.
6. Read back Ukrainian title/body/list/access and real links; verify all 30 owner blocks have exactly one destination and the four technical-only sources have none. Recheck all 34 source mappings. Report actual publication results and remaining blockers, not attempted requests.

Transport preflight: an earlier publication encountered HTTP 414 because the CLI encoded descriptions in request URLs. For this publication, installed v1.1.0 still used that transport. The CLI created short identified cards; the authenticated browser editor accepted the full Markdown through ordinary paste/save. CLI read-back verified all 31 complete descriptions, allowing only blank-line changes, Trello's empty link-title addition and one automatic link on the displayed domain GKCake.com. That domain remains explicitly unverified in BO-GK-012. Mapping hashes preserve the actual saved text. Recheck transport on future updates; never truncate requirements, export credentials or blindly repeat an ambiguous create.

No custom fields, paid dashboard, translation service or synchronization code is used. Future agents can use the two documents and real destination mappings directly; existing live card content must be checked before updates.


## Verification

Run `python3 scripts/check_role_packets.py` from the repository root. It validates
all source IDs/URLs/order/dependencies/hashes, original acceptance text in developer
packets, guided sections/examples in both languages, packet anchors and destination
consistency/completeness.
It does not prove live readiness, human approval, translation quality or Trello
publication. Those require the recorded review and actual evidence.
