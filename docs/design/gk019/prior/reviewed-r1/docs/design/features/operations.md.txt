# Chef operations, stock, communications and recovery — GK-019 r6

Status: reconciled technical packet; independent review pending. No application tests
or visual approval claimed. Prior assessment retained as [historical text](../gk019/prior/operations.md.txt).

Requirements: A02 A04–A06 A09–A11 S03 S04 S07 D01 D02. Owning tickets: GK-013/014/018/024/032/033/034/035/037/038.
Normative shared contract: [r6 K07/K08/K09/K10/K11](../contracts.md); [typed schema](../gk019/schema.json);
[source decisions](../creator-decisions.md); [open decisions](../gk019/questions.md).

## Scope and ownership

Chef actual component start/assembly deducts selected batches exactly once; auto48h status alone does not. Forecast outstanding use, whole-pack purchases and incomplete measurements. Current sender GK Cakes; immutable recipient preview/send; delivery evidence distinct from receipt. Independent recovery protects free/paid confirmations, changes, blobs and dispatch intents; restore freezes effects and reapplies deletion.

Existing [screen IDs](../screens.md) remain the design-session inputs. GK-015 visual
artifacts are owned by another context; this packet grants no visual approval. Current
behavior changes and exact revisions go through the [GK-015 handoff](../gk019/GK-015-handoff.md).

## Verification and dependency boundary

Component raw consumption not repeated at cake assembly; negative stock visible/reasoned; batch last usable date/DST boundaries; stale preview/recipient denied; send timeout replay after provider retention blocked; lost DB after paid/free confirmation and B2 outage produce no missing promise or duplicate external action.

Use [amended ticket specifications](../gk019/amendments/README.md) for exact acceptance
extensions and prerequisite sources. Existing release/authorization/capability gates
remain. Tests above are required future implementation evidence, not document checks.
