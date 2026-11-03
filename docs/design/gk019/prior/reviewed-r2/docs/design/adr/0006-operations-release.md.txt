# ADR-0006: Durable effects, independent recovery and component releases

Status: proposed integration revision; independent review pending
Date: 2026-10-05; revision: r4 / GK-019
Owner: planning master for technical integration; Sasha/original owner for attributed business decisions.
Affected requirements: C14 A02 A04 A06 A09 A10 A11 S03 S04 S07 D01 D02.

## Context and options

DB-only outbox is insufficient for zero confirmed-order loss if DB disappears before backup. Independent protocol adds explicit pending/repair states without claiming atomic DB/B2/provider commit. It is required by the accepted recovery goal, not a general event-sourcing platform. Daily backup alone rejected.

## Decision proposed for technical acceptance

Transactional intents/inbox/jobs with fencing and observed outcomes. Independent prepared and outcome recovery records before confirmation/external dispatch, essential blobs copied, replay ordered by version with outgoing operations frozen during restore. Chef actual stock events differ from automatic preparation status. Accepted component release policy and current email sender are incorporated.

Governing detail: [contracts r6](../contracts.md) K07/K08/K10/K11; [owner mapping](../owner-data-contract.md); [decision register](../creator-decisions.md). Business values are already approved where attributed; this status concerns their technical integration.

## Consequences and verification

GK-024 implements and tests the minimum K11 recovery coordinator/journal before first dispatch; its scope includes shared recovery schema/code and B2. GK-025/026/027 consume that prerequisite. GK-034 recovery integration depends on GK-024 (disjoint release workflow work may begin earlier); GK-034/035 must prove lost-DB/B2-outage/free-confirmation/change/attachment/side-effect recovery and cost. GK-013 verifies real required checks and independent AI-review enforcement before native auto-merge. Automatic staging after green main; production only separately Sasha-authorized tested ui-v/api-v release. No release/configuration authority from this ADR.

Documentation checks are not implementation or provider tests. No extra vendor, deployment or public policy is activated. Any material contract change invalidates affected ticket readiness and requires re-review.

Supersedes prior r3 execution defaults for this topic; [exact historical text](../gk019/prior/0006-operations-release.md.txt) and original evidence remain. Source and output hashes are recorded in the GK-019 manifests; no old review verdict is carried forward.
