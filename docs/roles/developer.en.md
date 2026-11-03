# Developer responsibilities — English

For GK-001/003/005 and dependent work, first load the [owner intake](../creator/README.md)
and [owner data contract](../design/owner-data-contract.md), including source-comments
and decisions JSON. Use exact confirmed values/qualifiers in SQL/admin/tests and
record input hashes. Packet bodies below reproduce publication-era requirements;
see the separate proposed amendment list for changed X/zone/guest/refund assumptions.


Revision: developer-guided-r3, 2026-09-27. This guide turns each existing ticket into a concrete developer deliverable. Sasha owns technical preparation/delivery; the Ukrainian-speaking business owner owns business inputs and product/visual approval. AI agents may execute scoped work, but neither their confidence nor green tests substitutes for required human acceptance.

**Use one packet at a time.** Each section states the outcome, start conditions, expected outputs, work steps, an illustrative result and how to verify/hand it off. The original source acceptance criteria are retained verbatim. Full source Context and mandatory reconciliation extensions remain authoritative; examples below explain them and do not replace them. No code, service registration, settings change, Trello publication or release is authorized merely by this document.

## How to produce a complete result

1. Read the live source ticket/comments/attachments, current approved decisions, linked full requirements and actual prerequisite evidence. Use O as display order; dependencies and milestone outputs determine readiness. Record input revisions and apply the existing model/context check before Agent execution/resumption. Missing policy permits drafting questions, not implementation of a guessed answer.
2. Collect the owner input using the corresponding [Ukrainian packet](business-owner.uk.md). Prepare its promised examples, proposal, prototype or safe test data before asking her to act. Preserve her original response and a checked English interpretation; return ambiguity in Ukrainian for clarification.
3. Produce the smallest complete outputs listed below, reusing existing intake/design/source areas. Decision tickets produce approved rules and worked examples; setup tickets produce verified nonsecret configuration; design tickets produce visible editable artifacts; implementation tickets produce behavior, regression evidence and delivery records. Do not add application code to a decision task or ask a human setup task for a fake red/green test.
4. Verify the concrete result against every source acceptance criterion and mandatory extension. For implementation, use meaningful failing-then-passing behavior checks, actual PostgreSQL for database races and controlled real providers for integration claims. GK-020 supplies exact pinned commands; proposed commands or infrastructure errors are not test evidence. Record revision, environment, expected/observed result and evidence, including failures/blockers.
5. Collect required independent review, owner contribution and authorized technical/human acceptance. Follow PR → required CI/review → native auto-merge → main CI. Release/deployment evidence is additional only where separately authorized and in scope. Do not wait for the parent to be Done before preparing its owner review; do not auto-complete the parent when her contribution finishes.

### Where outputs belong

| Output | Destination and evidence |
| --- | --- |
| Original owner answer, assets, setup evidence | Existing `docs/creator/GK-NNN/` intake as material arrives; link exact source comment/file/version and approval. Store only safe references for private data. |
| Accepted shared rules and technical decisions | Existing decision register, ADRs/contracts and affected tickets through GK-019 or a scoped reviewed successor; preserve unresolved states. |
| Design source, previews and feedback | Existing `docs/design/sessions/` convention, exact source/preview version, owner approval and integration review. |
| Implementation and checks | Actual repository paths/PRs and CI/test evidence at the tested revision; no speculative folder hierarchy prescribed here. |
| Live progress and acceptance | Existing source Trello card and, after separate publication, the linked owner contribution. Documents are context snapshots, not a parallel live status ledger. |

Every example below is **illustrative expected output, not an executed result, approved policy, actual account, measurement or permission**. Bracketed fields must be replaced with verified evidence before claiming completeness. An unresolved example stays unresolved. Do not create a file for every example; use these shapes inside the existing record that owns the result.

Useful terms: RPO = maximum acceptable data loss; RTO = target time to restore service. An immutable snapshot retains agreed historical values. Idempotency makes retries return/recover the same logical operation; it does not promise that an external provider can never duplicate a physical effect. A sandbox is a controlled nonproduction environment. Preserve precise technical semantics when explaining these to the owner.

[Role/board contract](README.md) · [Source mapping](mapping.json). Source “Creator” combined two humans: technical configuration/acceptance routes through the developer and agreed reviewer policy; business/visual decisions route through the owner or explicitly recorded delegate. Account ownership and release authority remain GK-004/006 decisions.

## Packet index

| O | Developer packet |
| --- | --- |
| 010 | [DEV-GK-001 — Set production, pickup and London delivery policy](#dev-gk-001) |
| 020 | [DEV-GK-003 — Approve customer agreement, accounts and refund policy](#dev-gk-003) |
| 030 | [DEV-GK-004 — Select provider package and recovery budget](#dev-gk-004) |
| 040 | [DEV-GK-005 — Verify UK business, allergen, tax and privacy content](#dev-gk-005) |
| 050 | [DEV-GK-006 — Confirm release-only deployment and repository access policy](#dev-gk-006) |
| 060 | [DEV-GK-007 — Set discounts, recipe scaling and ingredient accounting rules](#dev-gk-007) |
| 070 | [DEV-GK-002 — Supply brand and cake content for chrome visual exploration](#dev-gk-002) |
| 080 | [DEV-GK-010 — Provision selected data, identity and storage sandbox](#dev-gk-010) |
| 090 | [DEV-GK-011 — Provision selected payment and email test accounts](#dev-gk-011) |
| 100 | [DEV-GK-019 — Reconcile approved policies into executable contracts](#dev-gk-019) |
| 110 | [DEV-GK-012 — Verify domain, Instagram ownership and approved origins](#dev-gk-012) |
| 120 | [DEV-GK-015 — Approve chrome-led visual foundation with the Creator](#dev-gk-015) |
| 130 | [DEV-GK-017 — Review checkout and order recovery sketches](#dev-gk-017) |
| 140 | [DEV-GK-020 — Bootstrap separately buildable UI and transactional API](#dev-gk-020) |
| 150 | [DEV-GK-016 — Review discovery, bespoke and account flow sketches](#dev-gk-016) |
| 160 | [DEV-GK-018 — Review daily chef operations sketches](#dev-gk-018) |
| 170 | [DEV-GK-013 — Configure required CI review and native auto-merge gates](#dev-gk-013) |
| 180 | [DEV-GK-014 — Configure monitoring and verify restore ownership](#dev-gk-014) |
| 190 | [DEV-GK-021 — Authorize customers, staff and safe historical claims](#dev-gk-021) |
| 200 | [DEV-GK-024 — Deliver durable transactional email and evidence](#dev-gk-024) |
| 210 | [DEV-GK-022 — Publish cake catalog, variants and recipe history](#dev-gk-022) |
| 220 | [DEV-GK-023 — Reserve production, ready stock and pickup independently](#dev-gk-023) |
| 230 | [DEV-GK-034 — Build verified independent component release pipelines](#dev-gk-034) |
| 240 | [DEV-GK-025 — Collect full payment with safe holds and discounts](#dev-gk-025) |
| 250 | [DEV-GK-028 — Deliver appetising discovery and public availability](#dev-gk-028) |
| 260 | [DEV-GK-026 — Cancel and refund safely across production races](#dev-gk-026) |
| 270 | [DEV-GK-027 — Convert bespoke agreements and manual orders to payment](#dev-gk-027) |
| 280 | [DEV-GK-031 — Run chef fulfillment and atomic pickup changes](#dev-gk-031) |
| 290 | [DEV-GK-029 — Deliver account history, favourites and profile UI](#dev-gk-029) |
| 300 | [DEV-GK-033 — Expose email recovery, audit and business metrics to chef](#dev-gk-033) |
| 310 | [DEV-GK-030 — Deliver checkout, order timeline and refund recovery UI](#dev-gk-030) |
| 320 | [DEV-GK-032 — Track fridge ingredients and grocery budget by date](#dev-gk-032) |
| 330 | [DEV-GK-035 — Verify complete customer and chef launch scenarios](#dev-gk-035) |
| 340 | [DEV-GK-036 — Authorize the first production component release](#dev-gk-036) |

<a id="dev-gk-001"></a>
<!-- BEGIN DEV-GK-001 -->
## DEV-GK-001 — Set production, pickup and London delivery policy

### Context

Source: [GK-001](https://trello.com/c/eI8UpD6l/16-gk-001p0o010w00-set-production-pickup-and-london-delivery-policy) · [complete local requirements](../design/tickets/GK-001.md). Order O010; source wave W00; P0. Dependencies: none; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-001](business-owner.uk.md#bo-gk-001); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Turn the owner’s operating practices into unambiguous scheduling and delivery rules that later code can implement without guessing.

### Before you start

Begin by reading the live card/comments and the owner’s current answers. Missing answers permit an interview and options document; they block final policy acceptance. Do not choose X, trigger arbitration or rescheduling effects yourself.

### Expected outputs

O1. A dated policy table: made-to-order lead time/capacity, ready-stock preparation start, manual/automatic start arbitration, cancellation boundaries, pickup duration/capacity/rescheduling and London delivery zones/fees/windows/handover responsibility. Mark each row proposed, approved or unresolved with its evidence. (Supports the source AC1–AC4 below.)

O2. A worked-case table: exact cutoff equality, late timer, earlier manual start, both UK clock changes, two last-slot requests, full target pickup, reduced capacity below commitments and inside/outside delivery boundaries. State the intended result and which owner decision it depends on. (Supports the source AC1–AC4 below.)

O3. Owner’s original Ukrainian answers and checked English interpretation under docs/creator/GK-001/README.md when supplied; accepted decision references plus affected contract/ticket follow-up for GK-019. Preserve private address details appropriately. (Supports the source AC1–AC4 below.)

### To dos

T1. Turn owner answers into dated scheduling/delivery examples, including exact equality, both DST transitions, late scheduler, ready-stock preparation and pickup-move effects; explain options without selecting missing policy. (AC1–AC4 below.)

T2. Specify the intended concurrency and immutable-commitment test cases for the existing dependent implementation tickets; do not implement those tests in this policy task; record safe public versus private address use. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Example of a CASE RECORD, not an approved policy:
Case: manual start occurs before configured automatic deadline.
Option to review: first event closes customer cancellation.
Owner decision/source: [not supplied; do not substitute recommendation].
Ready-stock preparation event: [separate owner decision required].
Time example: pickup Sunday 12:00 Europe/London; X=[pending].
Expected cutoff: calculate only after elapsed-hours/clock-change rule is agreed.
Verification now: review the case with the owner.
Future regression: cancellation at equality and delayed scheduler, GK-026/031.
```

### Verify and hand off

Confirm every policy field has an explicit accepted value or remains a named blocker. Walk all cases against the same version, then reconcile affected contracts through the planning gate. These are reviewed business examples now; concurrency/application tests are implemented later in their assigned tickets.

**Human handoff:** Prepare BO-GK-001’s one-question-at-a-time interview, plain choices and date/time illustrations. Return a Ukrainian summary for approval. Include the separate ready-cake preparation and cancellation decisions; the existing production-trigger question is still unanswered.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Approved version gives X, effective start examples, cutoff equality and both DST transitions; no missing policy represented as a default.
AC2. Two simultaneous last-slot requests, target-full pickup move and policy reduction below commitments have explicit intended outcomes.
AC3. Delivery fees/eligibility and fulfillment handover windows are supplied with London boundary examples.
AC4. Required acceptance evidence is tied to exact input/output revision, checked by the appropriate reviewer/Creator and recorded on Trello. For implementation include relevant tests, all related PRs, required review/checks, merge and main CI; authorized release/deployment/smoke only when separately in scope.

<!-- END DEV-GK-001 -->

<a id="dev-gk-003"></a>
<!-- BEGIN DEV-GK-003 -->
## DEV-GK-003 — Approve customer agreement, accounts and refund policy

### Context

Source: [GK-003](https://trello.com/c/ftGtoGNB/17-gk-003p0o020w00-approve-customer-agreement-accounts-and-refund-policy) · [complete local requirements](../design/tickets/GK-003.md). Order O020; source wave W00; P0. Dependencies: none; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-003](business-owner.uk.md#bo-gk-003); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Define customer agreement and refund behavior as explicit business rules backed by exact examples.

### Before you start

Use existing owner intent and GK-001 timing decisions; leave unapproved cancellation/exception/refund allocation choices open. Obtain appropriate legal input through GK-005 where needed.

### Expected outputs

O1. State/action matrix covering unpaid, paid confirmed, in-production, ready, completed, expired and cancelled orders, including customer actions, manager exceptions and offline/manual provenance. (Supports the source AC1–AC4 below.)

O2. Quote/custom/guest/claim/reorder rules: required fields, agreement and expiry, proof-based association, current-price/allergen/date revalidation and commitment point. (Supports the source AC1–AC4 below.)

O3. Exact pence examples for full/partial refunds including tax/delivery/discount allocation, succeeded versus pending versus failed amounts and durable remaining obligation; dated owner decision evidence in the existing intake/decision records. (Supports the source AC1–AC4 below.)

### To dos

T1. Prepare state-by-state quote/account/cancellation/admin-exception and allocation examples; map business approval into existing money/identity contracts. (AC1–AC4 below.)

T2. Keep proof-based claim and online/offline reconciliation safety with development; retain full-return shortfall obligations and no arbitrary paid switch. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Illustrative refund-obligation record:
Captured: 10000p. Partial A: 4000p, pending.
Full-return operation has returned the other 6000p.
Then partial A fails: remaining full-return obligation is still 4000p.
Expected report: show the shortfall; do not claim full refund complete.
Business eligibility/allocation approval: [required source].
Separate offline case: receipt proof + actual-return proof, no fake provider capture.
```

### Verify and hand off

Check every state and edge case has an intended outcome, with authority and customer wording. Identity proof is not just matching an email/Instagram handle. Carry technical money/claim invariants forward without representing these document examples as executed tests.

**Human handoff:** Prepare BO-GK-003’s state table and easy-to-read money examples. Ask the owner to decide eligibility/exceptions and approve the precise agreement terms; you own translating them into technical rules.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Versioned policy covers paid confirmed, in-production, ready, completed, expired, cancelled and unpaid states plus manual/offline orders.
AC2. Prior partial refund and pending-then-failed partial examples state the full-return target and remaining obligation.
AC3. Customer claim requires verified identity plus order proof; Instagram handle never proves identity. Custom conversation becomes commitment only through reviewed quote/order/payment path.
AC4. Required acceptance evidence is tied to exact input/output revision, checked by the appropriate reviewer/Creator and recorded on Trello. For implementation include relevant tests, all related PRs, required review/checks, merge and main CI; authorized release/deployment/smoke only when separately in scope.

<!-- END DEV-GK-003 -->

<a id="dev-gk-004"></a>
<!-- BEGIN DEV-GK-004 -->
## DEV-GK-004 — Select provider package and recovery budget

### Context

Source: [GK-004](https://trello.com/c/RotJkR9L/18-gk-004p0o030w00-select-provider-package-and-recovery-budget) · [complete local requirements](../design/tickets/GK-004.md). Order O030; source wave W00; P0. Dependencies: none; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-004](business-owner.uk.md#bo-gk-004); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Give the owner a costed, operable provider choice with explicit recovery and ownership tradeoffs.

### Before you start

Research can start now. Selection needs current primary evidence and owner budget/account approval; this decision ticket does not provision services.

### Expected outputs

O1. Dated comparison covering each required capability, provider/plan/region, fixed and usage costs, GBP conversion/tax basis, two environments, DB AND object backup costs, and payment fees separately. (Supports the source AC1–AC4 below.)

O2. Recommended package with reasons, concrete limits, recovery-point/recovery-time targets, incident ownership, analytics definitions/privacy and exit/recovery approach. Label assumptions and unverified capabilities. (Supports the source AC1–AC4 below.)

O3. Recorded owner selection/spend exceptions and account/billing responsibilities; update accepted-provider ADR/SETUP instructions for later GK-010/011/012/014 actions. (Supports the source AC1–AC4 below.)

### To dos

T1. Refresh official provider eligibility/prices; present a dated GBP costed recommendation including tax/FX, two environments, object+DB backups, usage and separate payment fees. (AC1–AC4 below.)

T2. Select technical fit/region with evidence, translate RPO/RTO into business scenarios, document analytics semantics/privacy and account/billing roles; hand only accepted choices to the later setup tickets; do not provision accounts in this decision task. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Example cost-sheet structure; fill from verified current sources:
Capability | selected candidate/plan | region | fixed GBP/month | usage basis
Database + object recovery | [candidate] | [region] | [quote] | [limits]
UI/API/jobs + email | [candidate(s)] | [region] | [quote] | [limits]
Tax/FX and additional environment: [dated calculation]
Payment transaction fees: [separate schedule, not hidden in hosting total]
RPO/RTO: [approved maximum data loss / recovery time]; approval: [link].
```

### Verify and hand off

Reconcile the total with the agreed budget or explicit exception. Confirm the recovery plan includes object bytes, not just DB rows. Explain analytics visits/pageviews versus people. Do not call a proposed or stale quote an accepted current configuration.

**Human handoff:** Provide BO-GK-004 one short Ukrainian recommendation with a complete cost table and failure/recovery examples. She chooses spend and business tolerances; you research provider fit. No account registration belongs to this decision outcome.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. One named selection per capability, owner, region and exact dated recurring/usage quote fits agreed budget or receives explicit exception.
AC2. RPO/RTO and database plus object restore plan are approved; daily DB backups alone are not called complete recovery.
AC3. Analytics measurement (visits/pageviews vs distinct people), retention and consent assessment agreed; only selected accounts requested.
AC4. Required acceptance evidence is tied to exact input/output revision, checked by the appropriate reviewer/Creator and recorded on Trello. For implementation include relevant tests, all related PRs, required review/checks, merge and main CI; authorized release/deployment/smoke only when separately in scope.

<!-- END DEV-GK-004 -->

<a id="dev-gk-005"></a>
<!-- BEGIN DEV-GK-005 -->
## DEV-GK-005 — Verify UK business, allergen, tax and privacy content

### Context

Source: [GK-005](https://trello.com/c/OWHhKPBT/19-gk-005p0o040w00-verify-uk-business-allergen-tax-and-privacy-content) · [complete local requirements](../design/tickets/GK-005.md). Order O040; source wave W00; P0. Dependencies: none; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-005](business-owner.uk.md#bo-gk-005); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Translate verified business/legal/food decisions into publishable content and implementable data/calculation requirements.

### Before you start

Collect known facts first. Legal/tax uncertainty remains open until supported by appropriate evidence; do not guess exemptions, business status or allergen safety.

### Expected outputs

O1. Content/decision register: public trading/contact information, food-business status evidence, product and handover allergen wording, terms/refund rights, tax treatment and reviewed arithmetic examples. (Supports the source AC1–AC4 below.)

O2. Data-purpose/retention/deletion table for orders, custom requests/quotes and pinned images, browser storage, analytics and transactional versus marketing email. Identify required expert questions and implementation effects. (Supports the source AC1–AC4 below.)

O3. Versioned copy and example references with owner/expert/date/source approval; a Ukrainian review pack and safe source links, excluding identity/banking documents. (Supports the source AC1–AC4 below.)

### To dos

T1. Prepare a dated primary-source checklist and unresolved expert questions, rather than deciding legal/tax exemptions. (AC1–AC4 below.)

T2. Translate approved tax/allergen/retention/privacy/transactional-versus-marketing rules into exact content, calculation fixtures and storage/analytics configuration requirements; retain deletion requirements for request/quote images. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Illustrative decision-register entry:
Topic: retained custom-request inspiration photo
Purpose: preserve the customer's agreed reference
Retention/deletion rule: [pending verified business/legal decision]
Implementing consumers: GK-022/027 cleanup and authorized retrieval
Evidence/approver/version: [actual references]
Readiness: blocked for cleanup policy until approved; no invented duration.
```

### Verify and hand off

Check that every public claim and tax/allocation rule has a source, reviewed examples and an approver. Both before-purchase and handover allergen information must be covered. Translate accepted choices into requirements; do not configure analytics or implement deletion in this decision task.

**Human handoff:** Prepare drafts, tax/rounding/refund examples and precise expert questions for BO-GK-005. Preserve her original wording and explicit approval; do not ask her to write legal documents or technical storage policy unaided.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Chef/business owner records registration/status evidence and approved public contact/terms without exposing identity documents.
AC2. Tax calculation/rounding and refund allocation examples are signed off; product and fulfillment copy includes allergens before purchase and at handover.
AC3. Transactional versus marketing email, browser storage and analytics/retention decisions are explicit and versioned; unresolved legal points stay blocked.
AC4. Required acceptance evidence is tied to exact input/output revision, checked by the appropriate reviewer/Creator and recorded on Trello. For implementation include relevant tests, all related PRs, required review/checks, merge and main CI; authorized release/deployment/smoke only when separately in scope.

<!-- END DEV-GK-005 -->

<a id="dev-gk-006"></a>
<!-- BEGIN DEV-GK-006 -->
## DEV-GK-006 — Confirm release-only deployment and repository access policy

### Context

Source: [GK-006](https://trello.com/c/npWFafNi/20-gk-006p0o050w00-confirm-release-only-deployment-and-repository-access-policy) · [complete local requirements](../design/tickets/GK-006.md). Order O050; source wave W00; P0. Dependencies: none; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-006](business-owner.uk.md#bo-gk-006); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Make the release and access authority model explicit before configuring it.

### Before you start

Prepare the policy now; actual owners, authorized reviewers/publishers and delegated business acceptance require explicit agreement. The current documentation request grants no repository setting changes.

### Expected outputs

O1. Authority matrix naming technical reviewer, business approver, release publisher/operator, incident/access owners and permitted delegation; separately state who can accept each type of ticket. (Supports the source AC1–AC4 below.)

O2. Delivery policy showing PR checks/review → native auto-merge → main CI → separately authorized published component release → component CD, with no deploy on ordinary push/merge. (Supports the source AC1–AC4 below.)

O3. Repository plan/permission capability assessment, UI/API environment/secret separation and accepted tag naming or explicit alternative; list unsupported required protection as a blocker. (Supports the source AC1–AC4 below.)

### To dos

T1. Own repository plan/permission assessment, ui-v/api-v naming, CI/review/native-auto-merge policy and UI/API environment/secret separation. (AC1–AC4 below.)

T2. Present a clear release/business/technical acceptance authority matrix for both humans to approve. Do not interpret this role split as release permission or a change to required reviews. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Illustrative authority row:
Action: approve production UI release
Approver: [named person/delegate from agreed policy]
Executor: [named authorized operator]
Evidence required: exact version/target + reviewed main-CI artifact + business decision
Merge approval grants production release permission: no
Repository limitation: [verified issue or none; never bypass required checks]
```

### Verify and hand off

Ensure authority is named rather than inferred from “Creator”. Check repository capability supports the desired rules. A policy agreement enables later scoped configuration work; it is not an approval of any actual release.

**Human handoff:** Give BO-GK-006 a Ukrainian “who may do what” proposal and a sample launch request. She approves business authority/account access; technical tag/check mechanics are yours.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Creator approves release-only deployment interpretation, authorized publishers and proposed ui-v/api-v naming or exact alternative.
AC2. Repository plan/permissions support required review/checks/native auto-merge or named limitation is recorded without bypass.
AC3. UI/API environment ownership and secret access separation are defined; release authorization is distinct from merge.
AC4. Required acceptance evidence is tied to exact input/output revision, checked by the appropriate reviewer/Creator and recorded on Trello. For implementation include relevant tests, all related PRs, required review/checks, merge and main CI; authorized release/deployment/smoke only when separately in scope.

<!-- END DEV-GK-006 -->

<a id="dev-gk-007"></a>
<!-- BEGIN DEV-GK-007 -->
## DEV-GK-007 — Set discounts, recipe scaling and ingredient accounting rules

### Context

Source: [GK-007](https://trello.com/c/iJqMyuN1/21-gk-007p0o060w00-set-discounts-recipe-scaling-and-ingredient-accounting-rules) · [complete local requirements](../design/tickets/GK-007.md). Order O060; source wave W00; P0. Dependencies: none; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-007](business-owner.uk.md#bo-gk-007); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Produce exact discount, recipe and inventory rules that can be tested consistently.

### Before you start

Use the owner’s actual recipes and accounting practices; unanswered rounding, stock-use and zero-total choices stay unresolved. This task produces policy/fixture specifications, not application code.

### Expected outputs

O1. Discount matrix with eligibility, expiry/use limits, stacking, free-delivery zones, reinstatement on cancellation, deterministic allocation and zero-total behavior. (Supports the source AC1–AC4 below.)

O2. Recipe/quantity/cost fixtures with sizes/yield, canonical units, precision, valid ingredient-specific conversions, version and cost-as-of; no assumed linear scaling without evidence. (Supports the source AC1–AC4 below.)

O3. Planned-versus-actual inventory examples for receipts/lots/expiry/waste/negative balances, production/cancellation and ready-stock demand, linked to owner approval and later test tickets. (Supports the source AC1–AC4 below.)

### To dos

T1. Build exact pence allocation and decimal-quantity examples for owner review, including zero-total, expiry, negative stock and actual-use policy. (AC1–AC4 below.)

T2. Turn approved recipes/units/scaling/discount rules into versioned worked fixture specifications for later tests; preserve historical recipe demand and prevent double production demand for ready stock. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Proposed arithmetic example requiring owner-approved rule:
Items: 1001p + 1000p; percentage discount: 10%.
Proposed total discount: 200p; item allocations must sum to 200p.
Record allocation/rounding rule and expected totals, not only percentage.
Zero-total outcome: [reject combination OR approved no-payment-required path].
Inventory case: actual ingredient use remains consumed after cancellation;
ready-made stock does not create the same future production demand again.
```

### Verify and hand off

Check money examples in integer pence and quantity examples with explicit units/precision. Reject unresolved conversions rather than inventing grams-to-volume factors. Keep planned demand, physical consumption and compensation distinct.

**Human handoff:** Prepare BO-GK-007’s calculated examples and a small recipe sheet. Ask the owner to confirm real quantities and operating rules; you own exact arithmetic and translating them into future regression fixtures.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. 1001+1000 pence at 10% yields reviewed 200 pence discount with deterministic allocation, or approved replacement rule with equivalent exact examples.
AC2. Recipe/variant fixture supplies quantities/servings, units and cost as-of; no grams-to-volume conversion without ingredient-specific factor.
AC3. Order cancellation, production start, stock expiry and recipe edits each have agreed effects on commitments and actual movements.
AC4. Required acceptance evidence is tied to exact input/output revision, checked by the appropriate reviewer/Creator and recorded on Trello. For implementation include relevant tests, all related PRs, required review/checks, merge and main CI; authorized release/deployment/smoke only when separately in scope.

<!-- END DEV-GK-007 -->

<a id="dev-gk-002"></a>
<!-- BEGIN DEV-GK-002 -->
## DEV-GK-002 — Supply brand and cake content for chrome visual exploration

### Context

Source: [GK-002](https://trello.com/c/JsXtAhB2/22-gk-002p1o070w00-supply-brand-and-cake-content-for-chrome-visual-exploration) · [complete local requirements](../design/tickets/GK-002.md). Order O070; source wave W00; P1. Dependencies: none; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-002](business-owner.uk.md#bo-gk-002); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Deliver an accessible, permissioned source pack that a designer can actually use.

### Before you start

Collect originals now; keep missing assets visible. A request for chrome is an exploration direction, not approval of a new logo or page.

### Expected outputs

O1. Asset inventory: exact file/revision, accessible source, fingerprint when useful, rights/owner, approved use, original versus derivative and missing items. (Supports the source AC1–AC4 below.)

O2. Representative product-content table covering made-to-order, ready stock and unavailable cakes, with variants/servings/prices and chef-verified ingredients/allergens/cross-contact wording. (Supports the source AC1–AC4 below.)

O3. Approved usable assets in docs/creator/GK-002/assets/ or access-checked approved external storage; ticket intake README links the originals and relevant decisions. Do not duplicate large/private collections without need. (Supports the source AC1–AC4 below.)

### To dos

T1. Inventory accessible original assets, rights, revisions and missing content under docs/creator/GK-002; preserve originals and approved immutable references. (AC1–AC4 below.)

T2. Prepare representative content for design sessions; mark conceptual imagery and do not infer visual approval. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Illustrative inventory row, not a supplied asset:
File: [owner's original logo filename]
Source and rights: [actual source + permitted uses]
State: original; chrome derivative not yet approved
Product fixture: [real cake + variant + GBP price + servings]
Allergen wording approved by/date: [required evidence]
Missing: [specific file or content field]; design impact: [affected screen]
```

### Verify and hand off

Open the selected files in the receiving context, verify content provenance and identify exactly which design work is blocked by gaps. Source-approved photos do not prove a visual design has been accepted.

**Human handoff:** Give BO-GK-002 a simple content sheet and file list; help organize her originals. Ask her to approve usage and food content, not image processing or storage implementation.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Logo/photo sources and rights are accessible to the design agent; temporary illustrative imagery is explicitly labelled.
AC2. Representative cake content covers made-to-order, ready stock and unavailable example, with chef-reviewed allergens.
AC3. A source inventory distinguishes actual assets from missing ones; no invented approval.
AC4. Required acceptance evidence is tied to exact input/output revision, checked by the appropriate reviewer/Creator and recorded on Trello. For implementation include relevant tests, all related PRs, required review/checks, merge and main CI; authorized release/deployment/smoke only when separately in scope.

<!-- END DEV-GK-002 -->

<a id="dev-gk-010"></a>
<!-- BEGIN DEV-GK-010 -->
## DEV-GK-010 — Provision selected data, identity and storage sandbox

### Context

Source: [GK-010](https://trello.com/c/5AdmfDVH/23-gk-010p0o080w01-provision-selected-data-identity-and-storage-sandbox) · [complete local requirements](../design/tickets/GK-010.md). Order O080; source wave W01; P0. Dependencies: GK-004; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-010](business-owner.uk.md#bo-gk-010); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Provide selected sandbox resources and safe identity/access evidence for later application work.

### Before you start

GK-004 selection plus approved account/billing ownership and scoped access are required. Execute setup only under the later task’s authorization; do not create every candidate account.

### Expected outputs

O1. Safe resource inventory: selected provider/project/environment/region, TLS method, issuer/audience and bucket IDs/default access, with verified access and separate production approval state. (Supports the source AC1–AC4 below.)

O2. First-manager identity evidence recorded securely, with owner approval; intended app/migration/backup role and backup-destination ownership, secret names/locations and revocation/rotation responsibility. (Supports the source AC1–AC4 below.)

O3. Provisioning handoff identifying what exists versus what will be proved by GK-020 migrations, GK-021 exact callbacks/local roles and GK-022/027 image authorization. (Supports the source AC1–AC4 below.)

### To dos

T1. Provision the selected sandbox data/auth/storage resources and safe resource/region/TLS/issuer/audience identifiers after approved ownership and scoped access. (AC1–AC4 below.)

T2. Record protected secret locations and privilege/backup ownership; verify first-manager subject securely. Leave migrations to GK-020, exact callbacks/role tests to GK-021 and image tests to GK-022/027. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Example safe setup record (placeholders, no real resources):
Environment: sandbox; project ID/region: [verified IDs]
DATABASE_URL location: [protected backend store path], value omitted
Identity issuer/audience: [verified provider values]
First-manager subject evidence: [restricted reference + approval]
Callback URLs: pending GK-020/021 exact implementation output
Local manager role behavior: not yet tested; owned by GK-021
```

### Verify and hand off

Verify selected resources and scoped access without printing credentials. Provisioning acceptance does not wait for downstream application checks, and it must not claim they passed. Keep production resources/access separately approved.

**Human handoff:** Give BO-GK-010 official links and only the personal verification/ownership/manager actions she must perform. You handle resource configuration and safe IDs; she keeps personal recovery credentials.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Selected sandbox account/project/region, TLS connection/resource identifiers, provider identity issuer/audience and protected secret names are recorded without credential values. Actual callback URLs remain an explicit GK-020/GK-021 output, with no guessed or wildcard production allowlist.
AC2. Intended first manager’s provider subject/contact ownership and Creator approval are verified and recorded securely; this does not yet grant an application role. Selected bucket identifiers/default access configuration are recorded. GK-021 proves local role/disable behavior; GK-022/GK-027 prove authorized image access and denied writes after their application code exists.
AC3. Protected scoped access for Agent setup, intended app/migration/backup privilege separation and approved object+DB backup destinations/rotation/revocation ownership are documented. Agent migrations and executable privilege/restore verification remain GK-020/GK-034/GK-035 outcomes.
AC4. Required acceptance evidence is tied to exact input/output revision, checked by the appropriate reviewer/Creator and recorded on Trello. For implementation include relevant tests, all related PRs, required review/checks, merge and main CI; authorized release/deployment/smoke only when separately in scope.

<!-- END DEV-GK-010 -->

<a id="dev-gk-011"></a>
<!-- BEGIN DEV-GK-011 -->
## DEV-GK-011 — Provision selected payment and email test accounts

### Context

Source: [GK-011](https://trello.com/c/ZhcwMqTc/24-gk-011p0o090w01-provision-selected-payment-and-email-test-accounts) · [complete local requirements](../design/tickets/GK-011.md). Order O090; source wave W01; P0. Dependencies: GK-004, GK-005; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-011](business-owner.uk.md#bo-gk-011); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Make payment/email test accounts usable by the later adapters without confusing provisioning with integration proof.

### Before you start

GK-004 selected providers and GK-005 business evidence precede setup. Owner performs legal/bank checks herself; use only approved sandbox access and controlled recipients.

### Expected outputs

O1. Payment test-account/currency/API-event version record, account/environment separation and secret names/locations; owner verification status without sensitive documents. (Supports the source AC1–AC4 below.)

O2. Verified sender domain/DNS evidence, approved From/Reply-To and controlled recipient; email secret locations and access/rotation ownership. (Supports the source AC1–AC4 below.)

O3. Endpoint checklist separating account setup from reachable signed webhooks and capture/refund/delivery checks, which wait for actual sandbox endpoints and implementation. (Supports the source AC1–AC4 below.)

### To dos

T1. Configure selected GBP test mode/event versions, protected payment/email secrets, sender DNS and safe configuration evidence. (AC1–AC4 below.)

T2. Register/verify webhook URLs only after real sandbox endpoints exist; distinguish account readiness from later capture/refund/delivery integration checks. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Example provisioning handoff:
Currency: GBP; environment: selected provider's test mode
PAYMENT_SECRET / PAYMENT_WEBHOOK_SECRET: [protected locations; no values]
EMAIL_API_KEY / EMAIL_WEBHOOK_SECRET: [protected locations; no values]
Sender verification: [dated provider result]; From/Reply-To: [approved values]
Actual webhook reachability: [pending endpoint OR verified evidence]
Capture/refund/email fault tests: future GK-025/026/024 evidence, not this label.
```

### Verify and hand off

Read back correct account/environment and sender verification. Do not guess webhook URLs or mark future provider tests passed because credentials exist. No real customer email or real payment is needed for this setup outcome.

**Human handoff:** Prepare BO-GK-011’s official personal-check instructions and sender/recipient questions. Use provider invitations for access; never ask her to paste keys, bank details or identity documents into Trello.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Selected payment test account and event/API version configuration recorded; dependent agent can run GBP capture and full/partial refund sandbox checks.
AC2. Selected sender domain verified via exact provider DNS; From/Reply-To and controlled recipient recorded.
AC3. Webhook endpoint reachability waits on deployed sandbox endpoint; account provisioning evidence and later endpoint verification are separate.
AC4. Required acceptance evidence is tied to exact input/output revision, checked by the appropriate reviewer/Creator and recorded on Trello. For implementation include relevant tests, all related PRs, required review/checks, merge and main CI; authorized release/deployment/smoke only when separately in scope.

<!-- END DEV-GK-011 -->

<a id="dev-gk-019"></a>
<!-- BEGIN DEV-GK-019 -->
## DEV-GK-019 — Reconcile approved policies into executable contracts

### Context

Source: [GK-019](https://trello.com/c/lW3wuYq0/25-gk-019p0o100w01-reconcile-approved-policies-into-executable-contracts) · [complete local requirements](../design/tickets/GK-019.md). Order O100; source wave W01; P0. Dependencies: GK-001, GK-003, GK-004, GK-005, GK-006, GK-007; preserve exact source milestone deliverables.

No separate owner card: Technical integration review belongs to the developer; unresolved business questions return to the original decision card.

**Outcome:** Convert accepted decisions into a coherent, independently reviewed implementation baseline.

### Before you start

Collect approved GK-001/003/004/005/006/007 outputs and current ADR/contracts/provider evidence. Unapproved values remain blockers; this is a bounded technical-design task with no application implementation.

### Expected outputs

O1. Reconciled decision/ADR/contract revisions with explicit money/time/identity/capacity/state ownership, approved provider/session choices and dispositions of contradictions. (Supports the source AC1–AC4 below.)

O2. Updated 40-row requirement → screen → contract → test → ticket coverage and affected self-contained ticket values/prerequisites/readiness recommendations. (Supports the source AC1–AC4 below.)

O3. Exact input/output revision fingerprints, qualified model/context readback and fresh independent review with findings resolved; preserve unapproved policy questions. (Supports the source AC1–AC4 below.)

### To dos

T1. Reconcile approved owner inputs, provider/identity choices and all shared contracts; explain any newly exposed business question through its existing owner decision packet. (AC1–AC4 below.)

T2. Preserve all 40 requirement mappings, update affected executable tickets, apply model/context qualification and fresh independent review before acceptance. No application code. (AC1–AC4 below.)

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Example reconciliation row:
Input: GK-001 approved scheduling decision [revision + source]
Affected: K04/K05/K06; GK-017/018/023/026/031
Value still missing: [named field] → affected implementation remains blocked
Test obligation: exact cutoff sampled under order lock, including delayed timer
Output acceptance: [revised artifact hashes + qualified assessment + fresh review]
```

### Verify and hand off

Walk cross-feature scenarios and check every producer/consumer uses the same accepted values. Compare all 40 requirements; review changed dependents at their actual revision. Technical approval never substitutes for the owner’s missing business decision.

**Human handoff:** No new standalone owner card. Send newly exposed questions back to their existing BO decision packet in plain Ukrainian; do not ask her to approve SQL locks or token verification mechanics.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Qualified Astra assessment settles stack/authorization/payment/capacity decisions and no contradictory field/state/timeout/lock order remains.
AC2. All 40 requirement rows still map to screens/contracts/tests/tickets; affected self-contained tickets embed approved values and exact prerequisites.
AC3. Fresh independent reviewer examines revised hashes and all affected dependents; missing decisions stay blocked and no application code is implemented.
AC4. Required acceptance evidence is tied to exact input/output revision, checked by the appropriate reviewer/Creator and recorded on Trello. For implementation include relevant tests, all related PRs, required review/checks, merge and main CI; authorized release/deployment/smoke only when separately in scope.

<!-- END DEV-GK-019 -->

<a id="dev-gk-012"></a>
<!-- BEGIN DEV-GK-012 -->
## DEV-GK-012 — Verify domain, Instagram ownership and approved origins

### Context

Source: [GK-012](https://trello.com/c/nveiC5IF/26-gk-012p1o110w01-verify-domain-instagram-ownership-and-approved-origins) · [complete local requirements](../design/tickets/GK-012.md). Order O110; source wave W01; P1. Dependencies: GK-002, GK-004; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-012](business-owner.uk.md#bo-gk-012); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Establish actual domain/social ownership and a tested, truthful Instagram handoff.

### Before you start

Use GK-002 content/account details and GK-004 selected providers. Verify the actual domain; the proposed name does not prove ownership or availability.

### Expected outputs

O1. Owned-domain/DNS manager and approved site/admin/API/sender origin inventory, with environment and renewal/access owner; exact callbacks coordinated with GK-020/021 when available. (Supports the source AC1–AC4 below.)

O2. iOS/Android/desktop account-device matrix recording actual open/copy/failure behavior and supported fallback, not a promised automated DM. (Supports the source AC1–AC4 below.)

O3. Owner-approved public address/account and Ukrainian customer instruction references, including unsupported or untested combinations. (Supports the source AC1–AC4 below.)

### To dos

T1. Verify owned domain/DNS and derive exact site/admin/API/sender origins; coordinate identity callback values with GK-020/021 without guessing them. (AC1–AC4 below.)

T2. Lead actual iOS/Android/desktop account-device handoff tests and record supported fallback/unsupported cases; no Instagram API messaging scope added. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Illustrative device-test record:
Device/browser/account state: [actual combination]
Action: copy summary → open approved Instagram destination
Observed destination: [result]; clipboard result: [result]
Fallback when unavailable: selectable retained summary + documented manual action
Website claims DM sent: never based on opening/copying
Unverified device: [named gap, not a pass]
```

### Verify and hand off

Run the actual scoped device/account checks and confirm no duplicate domain purchase. Identity callbacks must be exact verified outputs, with later denial tests; do not introduce an Instagram messaging API project into the manual flow.

**Human handoff:** Ask BO-GK-012 for ownership/public-address decisions and assisted checks on her available devices. You arrange missing device coverage and explain fallback; she need not configure DNS or own every test device.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Actual owned domain and DNS manager identified, approved hostnames recorded, no duplicate registration.
AC2. Device/account evidence identifies what opens, clipboard fallback and user-sent responsibility; nothing claims website sent DM.
AC3. Nonsecret origin/callback list aligns with selected environment; invalid callbacks denied in later auth test.
AC4. Required acceptance evidence is tied to exact input/output revision, checked by the appropriate reviewer/Creator and recorded on Trello. For implementation include relevant tests, all related PRs, required review/checks, merge and main CI; authorized release/deployment/smoke only when separately in scope.

<!-- END DEV-GK-012 -->

<a id="dev-gk-015"></a>
<!-- BEGIN DEV-GK-015 -->
## DEV-GK-015 — Approve chrome-led visual foundation with the Creator

### Context

Source: [GK-015](https://trello.com/c/POD3I6sM/27-gk-015p1o120w01-approve-chrome-led-visual-foundation-with-the-creator) · [complete local requirements](../design/tickets/GK-015.md). Order O120; source wave W01; P1. Dependencies: GK-002; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-015](business-owner.uk.md#bo-gk-015); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Produce an owner-approved visual foundation reusable by later flows.

### Before you start

Read the source prerequisites and exact owner-approved inputs. You may prepare drafts for feedback while the parent is In Progress/Review; missing required policies/assets must remain visible and block final approval.

### Expected outputs

O1. Two distinct chrome-led directions using genuine accessible assets, with gallery/detail mobile and desktop previews and a recommendation explaining the difference. (Supports the source AC1–AC4 below.)

O2. Editable source and a compact visual specification for colours, typography, spacing, image treatment and default/focus/loading/disabled/unavailable controls. (Supports the source AC1–AC4 below.)

O3. Feedback-to-change record, exact-version owner approval and independent integration findings; store together in the existing visual-session area. (Supports the source AC1–AC4 below.)

### To dos

T1. Create two chrome-led visual directions using approved assets, with editable mobile/desktop coloured sketches and complete control states. (AC1–AC4 below.)

T2. Run accessibility/contrast/focus checks, record owner feedback and exact approved revision, then obtain independent behavior-integration review. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Illustrative session output:
Version: [actual revision]; direction A/B preview links: [working links]
Owner task: find price, servings/allergens/date; distinguish buy/custom actions
Button: Buy matching cake → [defined next view]; unavailable → [visible recovery]
Feedback: [verbatim Ukrainian comment] → [change or agreed disposition]
Approved exact source/previews: [approval reference, or pending]
```

### Verify and hand off

Render and inspect actual mobile/desktop coloured artifacts. Verify focus/contrast/keyboard, loading/disabled/error states and button destinations. Resolve feedback, obtain explicit approval of the exact version, then pass fresh independent integration review. Mock interactions remain labelled; design approval is not implemented behavior.

**Human handoff:** Prepare BO-GK-015’s two visible alternatives, numbered version and simple review route. Explain chrome/readability tradeoffs. Do not ask the owner to choose CSS values or infer approval from “looks nice”.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Rendered mobile/desktop coloured sketches with visible labels, imagery, default/focus/loading/disabled controls and unavailable state are shown to Creator.
AC2. Editable source specifies colour values, type, spacing and button actions; contrast/focus checked and chrome not used for unreadable body text.
AC3. Feedback applied or resolved; Creator approves exact revision and fresh review verifies behavior integration. Silence is not approval.
AC4. Required acceptance evidence is tied to exact input/output revision, checked by the appropriate reviewer/Creator and recorded on Trello. For implementation include relevant tests, all related PRs, required review/checks, merge and main CI; authorized release/deployment/smoke only when separately in scope.

<!-- END DEV-GK-015 -->

<a id="dev-gk-017"></a>
<!-- BEGIN DEV-GK-017 -->
## DEV-GK-017 — Review checkout and order recovery sketches

### Context

Source: [GK-017](https://trello.com/c/1YGOcOHR/28-gk-017p0o130w02-review-checkout-and-order-recovery-sketches) · [complete local requirements](../design/tickets/GK-017.md). Order O130; source wave W02; P0. Dependencies: GK-015, GK-001, GK-003, GK-007; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-017](business-owner.uk.md#bo-gk-017); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Make checkout, cancellation and uncertain-money recovery understandable before UI implementation.

### Before you start

Read the source prerequisites and exact owner-approved inputs. You may prepare drafts for feedback while the parent is In Progress/Review; missing required policies/assets must remain visible and block final approval.

### Expected outputs

O1. Mobile/desktop coloured flow from early availability and agreed total/allergens/policy through pickup/delivery, hosted payment and order timeline. (Supports the source AC1–AC4 below.)

O2. Visible branches for stale price, last slot lost, pending/late payment, cancellation/refund pending, exact cutoff and full pickup target; separate lifecycle and money states. (Supports the source AC1–AC4 below.)

O3. Editable button/state specification, accessibility checks, owner feedback and exact-version approval plus independent timed-start/pickup integration review. (Supports the source AC1–AC4 below.)

### To dos

T1. Build checkout/recovery sketches tied to approved money, timed-start and pickup contracts; expose uncertainty honestly. (AC1–AC4 below.)

T2. Verify accessible controls, stale-price/last-slot/cutoff/full-slot cases; record exact owner approval and independent integration review. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Expected screen-state example:
Order lifecycle: cancelled. Refund: pending provider evidence.
Show both states; do not display “money returned” from cancellation alone.
If price or slot changes before commitment → explain change and request new consent.
Payment return page → fetch authoritative result; uncertainty stays understandable.
Each control: label + enabled condition + destination + error/recovery state.
```

### Verify and hand off

Render and inspect actual mobile/desktop coloured artifacts. Verify focus/contrast/keyboard, loading/disabled/error states and button destinations. Resolve feedback, obtain explicit approval of the exact version, then pass fresh independent integration review. Mock interactions remain labelled; design approval is not implemented behavior.

**Human handoff:** Prepare BO-GK-017’s normal purchase plus named exceptional examples and full expected totals. The owner should assess meaning and next action, not trigger provider errors herself.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Mobile/desktop prototype displays total GBP cost and snapshot allergens/policy before commit; disabled/loading/focus states and button destinations defined.
AC2. Last slot lost, stale price, provider uncertainty, exact cutoff and pending refund remain understandable without database terminology.
AC3. Creator feedback applied and exact revision approved; fresh integration review checks timed-start/pickup semantics before UI implementation.
AC4. Required acceptance evidence is tied to exact input/output revision, checked by the appropriate reviewer/Creator and recorded on Trello. For implementation include relevant tests, all related PRs, required review/checks, merge and main CI; authorized release/deployment/smoke only when separately in scope.

<!-- END DEV-GK-017 -->

<a id="dev-gk-020"></a>
<!-- BEGIN DEV-GK-020 -->
## DEV-GK-020 — Bootstrap separately buildable UI and transactional API

### Context

Source: [GK-020](https://trello.com/c/W8q5svCh/29-gk-020p0o140w02-bootstrap-separately-buildable-ui-and-transactional-api) · [complete local requirements](../design/tickets/GK-020.md). Order O140; source wave W02; P0. Dependencies: GK-019; preserve exact source milestone deliverables.

No separate owner card: Bootstrap acceptance is technical; the owner reviews product flows in design and acceptance packets.

**Outcome:** Create the minimal genuinely runnable UI/API foundation and establish the real checks used by later work.

### Before you start

GK-019 accepted baseline and local execution/model prerequisites permit B1. Bootstrap supplies its own first runners; do not require nonexistent application commands before creating the minimal harness. G1 gates are required for B2, not for starting B1.

### Expected outputs

O1. Pinned supported toolchains/lockfiles, separately buildable UI and API/maintenance, health/config validation, private schema/runtime-role migrations and exact working test/build commands. (Supports the source AC1–AC4 below.)

O2. B1: scoped bootstrap PR with meaningful failing-then-passing assertions, reviewed revision, green real component checks and exact check names; keep parent Review for GK-013 G1. (Supports the source AC1–AC4 below.)

O3. B2: after G1, revalidate current PR/base, satisfy required reviews/checks, native auto-merge, resulting main CI and authorized technical acceptance. No push-triggered production deployment. (Supports the source AC1–AC4 below.)

### To dos

T1. Bootstrap the approved UI/API and protected PostgreSQL commands with pinned real checks, configuration validation, migrations and separate builds. (AC1–AC4 below.)

T2. Deliver B1 green reviewed PR/check names; await GK-013 G1; then B2 revalidate/native-auto-merge/main CI and technical acceptance. Preserve protected-role and secret-boundary tests. (AC1–AC4 below.)

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Illustrative milestone receipt; fill with actual evidence:
B1 PR/revision: [real link/SHA]; runners/toolchain: [verified commands/versions]
Red assertion: unsafe/missing configuration rejected for intended reason
PostgreSQL role checks: direct protected writes denied; permitted command succeeds
B1 exact green check names: [actual names] → GK-013 G1
G1 evidence: [link] → B2 latest-base checks → auto-merge → exact main CI
No-op/pass-through CI job: not acceptable evidence.
```

### Verify and hand off

Run real PostgreSQL empty/previous-schema migration and role/atomic-command checks, component builds, configuration mixing/secret redaction and graceful shutdown fixtures. Infrastructure failure is not a successful red test. Downstream GK-020 consumers need B2 except GK-013, which consumes B1.

**Human handoff:** No separate business-owner bootstrap review. Record technical acceptance under GK-006 authority; product design/acceptance comes through its existing owner packets.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Red config test rejects missing secret/origin and unsafe production/test mixing; secret values never enter errors/frontend bundles.
AC2. Bootstrap pins supported toolchain/lockfiles, supplies exact unit/API/UI/Postgres/migration commands and demonstrates failing assertion then green; no SQLite replacement for Postgres.
AC3. PR CI and main CI build both components; empty/previous-schema migration and graceful job shutdown fixtures run. No branch push deploys.
AC4. Record exact input/tested revision, results, independent review and Creator acceptance on Trello; account for all related PRs, required review/checks, native auto-merges and main CI. Release/deploy/smoke only when separately authorized and in scope.

<!-- END DEV-GK-020 -->

<a id="dev-gk-016"></a>
<!-- BEGIN DEV-GK-016 -->
## DEV-GK-016 — Review discovery, bespoke and account flow sketches

### Context

Source: [GK-016](https://trello.com/c/oapKvUNJ/30-gk-016p1o150w02-review-discovery-bespoke-and-account-flow-sketches) · [complete local requirements](../design/tickets/GK-016.md). Order O150; source wave W02; P1. Dependencies: GK-015, GK-003; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-016](business-owner.uk.md#bo-gk-016); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Produce approved discovery, custom-request and account flow sketches with complete recoverable journeys.

### Before you start

Read the source prerequisites and exact owner-approved inputs. You may prepare drafts for feedback while the parent is In Progress/Review; missing required policies/assets must remain visible and block final approval.

### Expected outputs

O1. Click-through and editable mobile/desktop source covering gallery/detail/public schedule, custom source image/changes/summary/copy/open/failure, sign-in, favourites, profile, history and reorder. (Supports the source AC1–AC4 below.)

O2. Action/state matrix including archived/empty/error/session/claim states; requests retain exact inspiration and do not pretend copy/open sent a DM or proved identity. (Supports the source AC1–AC4 below.)

O3. Accessibility results, owner feedback/revisions, exact approval and independent contract-integration review stored in the bounded session. (Supports the source AC1–AC4 below.)

### To dos

T1. Prepare the approved-foundation discovery/custom/account click-through with all actions and empty/error/archived states. (AC1–AC4 below.)

T2. Preserve selected inspiration provenance, truthful copy/open behavior and claim semantics; save accessibility evidence, owner approval and independent integration review. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Expected sketch behavior example, not an executed test:
Custom request uses selected inspiration image A + requested changes.
Clipboard/Instagram opening fails → summary remains selectable and recoverable.
Reorder → current price/variant/allergens/date review; no automatic payment.
Archived favourite → labelled safe historical reference and defined next action.
Approval record: exact preview/source version + owner response + review evidence.
```

### Verify and hand off

Render and inspect actual mobile/desktop coloured artifacts. Verify focus/contrast/keyboard, loading/disabled/error states and button destinations. Resolve feedback, obtain explicit approval of the exact version, then pass fresh independent integration review. Mock interactions remain labelled; design approval is not implemented behavior.

**Human handoff:** Give BO-GK-016 a numbered route for buy versus custom, failed Instagram handoff, account/history and reorder. Ask for the actual confusing step; show and reapprove the revised version.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Creator sees coloured mobile/desktop click-through and can distinguish buy matching cake, request changes and reorder with revalidation.
AC2. Custom summary remains recoverable after clipboard/Instagram failure; identity/favourites/claim states do not falsely imply account linking or DM send.
AC3. Editable source/previews, accessibility checks, feedback and exact-version Creator approval plus integration review saved.
AC4. Required acceptance evidence is tied to exact input/output revision, checked by the appropriate reviewer/Creator and recorded on Trello. For implementation include relevant tests, all related PRs, required review/checks, merge and main CI; authorized release/deployment/smoke only when separately in scope.

<!-- END DEV-GK-016 -->

<a id="dev-gk-018"></a>
<!-- BEGIN DEV-GK-018 -->
## DEV-GK-018 — Review daily chef operations sketches

### Context

Source: [GK-018](https://trello.com/c/jjP7btz9/31-gk-018p1o160w02-review-daily-chef-operations-sketches) · [complete local requirements](../design/tickets/GK-018.md). Order O160; source wave W02; P1. Dependencies: GK-015, GK-001, GK-003, GK-007; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-018](business-owner.uk.md#bo-gk-018); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Produce chef screens that support actual daily tasks without exposing database/payment jargon.

### Before you start

Read the source prerequisites and exact owner-approved inputs. You may prepare drafts for feedback while the parent is In Progress/Review; missing required policies/assets must remain visible and block final approval.

### Expected outputs

O1. Editable coloured mobile/desktop sketches for orders/manual quotes, production versus pickup calendars, fulfillment, refund preview/recovery, ingredient/grocery/cost views and email trace/custom preview. (Supports the source AC1–AC4 below.)

O2. Task/state/role matrix covering commitments, stale edits, inventory discrepancies, unknown refunds and bounced mail, with safe next actions. (Supports the source AC1–AC4 below.)

O3. Owner walkthrough observations, resolved feedback, exact-version approval and independent integration/accessibility review. (Supports the source AC1–AC4 below.)

### To dos

T1. Produce task-based chef sketches covering manual quotes/orders, capacity/pickup, refunds, inventory/groceries and email investigation/preview. (AC1–AC4 below.)

T2. Check stale/conflict/unknown/error states, role limits and accessibility; retain editable source, exact owner feedback/approval and independent review. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Illustrative review route:
Create manual agreed order → find pickup → start production → preview refund.
Role: fulfillment staff → permitted fulfillment action; refund control unavailable.
Concurrent edit → explain stale view and recovery; do not overwrite silently.
Email recipient preview → [exact identity shown] before any send.
Owner feedback / revised version / approval: [actual references]
```

### Verify and hand off

Render and inspect actual mobile/desktop coloured artifacts. Verify focus/contrast/keyboard, loading/disabled/error states and button destinations. Resolve feedback, obtain explicit approval of the exact version, then pass fresh independent integration review. Mock interactions remain labelled; design approval is not implemented behavior.

**Human handoff:** Prepare BO-GK-018’s chef workday with test-like mock records. Demonstrate each failure branch and collect exact confusing labels/actions; do not substitute a generic database admin UI.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Coloured desktop/mobile sketches let chef enter manual order, adjust capacity, start production, find pickup, trace email and preview refund without DB/payment jargon.
AC2. Existing commitments, stale version, ingredient discrepancy, unknown refund and bounced email have visible recovery states and role limits.
AC3. Editable source/tokens/buttons/previews and exact Creator-approved revision with fresh integration review saved.
AC4. Required acceptance evidence is tied to exact input/output revision, checked by the appropriate reviewer/Creator and recorded on Trello. For implementation include relevant tests, all related PRs, required review/checks, merge and main CI; authorized release/deployment/smoke only when separately in scope.

<!-- END DEV-GK-018 -->

<a id="dev-gk-013"></a>
<!-- BEGIN DEV-GK-013 -->
## DEV-GK-013 — Configure required CI review and native auto-merge gates

### Context

Source: [GK-013](https://trello.com/c/KpxlodVh/32-gk-013p0o170w03-configure-required-ci-review-and-native-auto-merge-gates) · [complete local requirements](../design/tickets/GK-013.md). Order O170; source wave W03; P0. Dependencies: GK-006, GK-020; preserve exact source milestone deliverables.

No separate owner card: Repository gates are developer administration; business release authority is covered by BO-GK-006/036.

**Outcome:** Deliver G1 repository gate configuration so the bootstrap PR can safely complete B2.

### Before you start

Consume GK-006 policy and GK-020 B1: reviewed green bootstrap PR, exact real check names/commands. GK-020 stays Review; waiting for it to be Done creates a cycle.

### Expected outputs

O1. Exact repository/ruleset/check-name/review-policy configuration evidence and repository-plan capability/permissions assessment. (Supports the source AC1–AC4 below.)

O2. Controlled bad/absent/stale-check and missing-review examples showing merge is blocked; native auto-merge availability only behind the required gates. (Supports the source AC1–AC4 below.)

O3. G1 acceptance record by authorized technical human and handoff to GK-020 B2. No merged application PR is required to complete G1 itself. (Supports the source AC1–AC4 below.)

### To dos

T1. Configure and verify required GitHub checks/reviews and native auto-merge availability using GK-020 B1 names. Collect G1 evidence without waiting for GK-020 Done. (AC1–AC4 below.)

T2. Own technical acceptance of settings under the authority agreed in GK-006; hand G1 back so GK-020 B2 can merge/main-CI. No business-owner GitHub setup task. (AC1–AC4 below.)

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Example G1 handoff:
Input B1: [PR + tested revision + successful exact job names]
Required checks: [actual names, not guessed display labels]
Negative gate cases: [links/results for bad, absent, stale checks and missing review]
Native auto-merge available: [verified configuration evidence]
G1 accepted by: [authorized technical approver]
Next: GK-020 B2 revalidates latest PR/base, merges and verifies main CI.
```

### Verify and hand off

Read back settings and prove the gates reject the listed failure cases. If the repository plan cannot enforce them, record the blocker rather than bypassing. G1 configuration completion is distinct from B2 actual auto-merge proof.

**Human handoff:** No separate owner review card. Business authority remains in BO-GK-006; route any policy change there. Technical settings acceptance follows the named human/reviewer policy.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Bad/absent/stale checks prevent merge and required review cannot be bypassed under configured policy.
AC2. G1 evidence shows native auto-merge is available only behind the configured required checks/reviews, with blocked bad/absent/stale-check examples. Record the green reviewed bootstrap PR and exact names; this Creator configuration deliverable does not wait for its merge. GK-020 B2 owns enabling auto-merge after G1, rechecking the latest revision, merge/main-CI evidence and bootstrap acceptance.
AC3. Permissions and settings evidence contains no secrets; Done still requires board acceptance.
AC4. Creator accepts G1 configuration evidence and records exact repository/rules/check names and review policy on Trello. This is a non-code setup outcome; no merged application PR is a completion prerequisite. GK-020 B2 records the first actual auto-merge/main-CI proof before downstream application work.

<!-- END DEV-GK-013 -->

<a id="dev-gk-014"></a>
<!-- BEGIN DEV-GK-014 -->
## DEV-GK-014 — Configure monitoring and verify restore ownership

### Context

Source: [GK-014](https://trello.com/c/47tZfEoI/33-gk-014p0o180w05-configure-monitoring-and-verify-restore-ownership) · [complete local requirements](../design/tickets/GK-014.md). Order O180; source wave W05; P0. Dependencies: GK-004, GK-020; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-014](business-owner.uk.md#bo-gk-014); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Make operational alerts, backup resources and recovery ownership concrete before launch testing.

### Before you start

Use approved GK-004 costs/recovery targets, GK-005 privacy choices and GK-020 B2 output. Configuration requires authorized provider access.

### Expected outputs

O1. Alert/incident runbook with approved thresholds, named recipients, response expectations and a controlled delivered alert, including financial pending/job signals. (Supports the source AC1–AC4 below.)

O2. DB AND object backup inventory with retention, access, rotation ownership and an isolated restore destination; distinguish existence from restoration proof. (Supports the source AC1–AC4 below.)

O3. Analytics configuration/evidence excluding private admin/order routes and using approved measurement definitions; owner-facing recovery memo and later GK-034/035 verification links. (Supports the source AC1–AC4 below.)

### To dos

T1. Configure provider-native alerts, budget thresholds, DB+object backup resources/retention/isolated destination and scoped access; deliver a controlled alert. (AC1–AC4 below.)

T2. Apply approved analytics/privacy exclusions and capture configuration evidence; distinguish backup provisioning, GK-034 harness and GK-035 domain restore proof. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Example evidence record:
Alert: [controlled event]; recipient/channel: [named]; observed time: [actual]
DB backup: [resource/retention]; object-byte backup: [resource/retention]
Restore destination: [isolated target; production overwrite forbidden]
RPO/RTO: [approved targets]; measured restore: pending GK-035
Analytics private-route exclusion: [configuration/check evidence]
```

### Verify and hand off

Confirm the controlled alert reaches the intended person and backup resources include both data and objects. Do not equate a backup notification with a restore test. Keep GK-034 infrastructure harness and GK-035 integrated domain recovery separate.

**Human handoff:** Give BO-GK-014 a short Ukrainian incident/recovery memo naming contacts, retention and safe restore location. Ask her to confirm alert receipt and response understanding, not to restore PostgreSQL.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Controlled alert reaches named owner and thresholds reflect approved response/financial pending states.
AC2. Database AND object backup resources, retention, restore destination and access are recorded; backup-notification evidence alone is not a restore test.
AC3. Analytics/privacy configuration matches GK-004/005 and excludes private admin/order routes.
AC4. Required acceptance evidence is tied to exact input/output revision, checked by the appropriate reviewer/Creator and recorded on Trello. For implementation include relevant tests, all related PRs, required review/checks, merge and main CI; authorized release/deployment/smoke only when separately in scope.

<!-- END DEV-GK-014 -->

<a id="dev-gk-021"></a>
<!-- BEGIN DEV-GK-021 -->
## DEV-GK-021 — Authorize customers, staff and safe historical claims

### Context

Source: [GK-021](https://trello.com/c/zEfCJtWD/34-gk-021p0o190w05-authorize-customers-staff-and-safe-historical-claims) · [complete local requirements](../design/tickets/GK-021.md). Order O190; source wave W05; P0. Dependencies: GK-020, GK-010, GK-003; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-021](business-owner.uk.md#bo-gk-021); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Deliver authenticated customer ownership, staff permissions and safe historical claims.

### Before you start

GK-020 B2 runners, GK-010 resource/subject evidence and GK-003 accepted claim rules are required. Use the reviewed GK-019 identity/session/provider contract; do not invent callbacks or token storage policy.

### Expected outputs

O1. Working identity/role/claim/profile/favourite behavior integrated with approved API contracts and least-privilege storage; exact provider callback settings and verified local manager bootstrap/disable. (Supports the source AC1–AC4 below.)

O2. Automated token/ownership/role/replay and non-enumeration tests, with controlled real-provider callback/session checks and safe evidence. (Supports the source AC1–AC4 below.)

O3. Owner walkthrough for manager versus fulfillment access and claim explanations; reviewed delivery evidence tied to the tested source revision. (Supports the source AC1–AC4 below.)

### To dos

T1. Implement verified identity, active local roles, safe claims and owner-scoped profile/history/favourites, including current-actor checks on replay. (AC1–AC4 below.)

T2. Configure exact provider callbacks with authorized access; prove forged-role, inactive-staff, foreign-order, replay and token-validation rejection. Translate only business role choices for the owner. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Expected test record:
Customer A reads customer B's order → 404, no disclosed details.
Inactive staff / user-editable forged role → 403, no privileged effect.
Verified subject + valid claim proof → one association; replay/other subject denied.
Disable local manager membership → subsequent operation denied even if token lives.
Actual evidence: [test command + revision + output link], not this example.
```

### Verify and hand off

Include issuer/audience/signature/expiry/rotation, valid/invalid callbacks, current actor authorization on idempotent replay, profile validation and archived favourites. Separate provider logout behavior from local staff disable; no blanket promise that sign-out revokes every issued JWT.

**Human handoff:** Provide BO-GK-021 controlled accounts and an understandable role demonstration. Obtain her real staff/role approval through a safe channel; she does not configure callbacks or verify cryptography.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Red API fixtures: customer A cannot read/cancel/claim B order (404), inactive staff and metadata-forged role cannot act (403).
AC2. Valid token checks issuer/audience/signature/expiry/rotation; verified subject + claim proof links once; replay/reuse/other subject rejected without data leak.
AC3. Profile/favourite CRUD validates escaped handle and ownership, handles archived cake, current actor authorization on idempotent replay, and least-privilege storage access.
AC4. Record exact input/tested revision, results, independent review and Creator acceptance on Trello; account for all related PRs, required review/checks, native auto-merges and main CI. Release/deploy/smoke only when separately authorized and in scope.

<!-- END DEV-GK-021 -->

<a id="dev-gk-024"></a>
<!-- BEGIN DEV-GK-024 -->
## DEV-GK-024 — Deliver durable transactional email and evidence

### Context

Source: [GK-024](https://trello.com/c/J6zlPAc9/35-gk-024p0o200w05-deliver-durable-transactional-email-and-evidence) · [complete local requirements](../design/tickets/GK-024.md). Order O200; source wave W05; P0. Dependencies: GK-020, GK-011; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-024](business-owner.uk.md#bo-gk-024); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Make transactional emails durable and their recorded delivery status truthful.

### Before you start

GK-020 B2 and all source email/provider/identity prerequisites must be accepted; use approved notices, recipient/generation and retention behavior with a controlled recipient.

### Expected outputs

O1. Durable notice intent committed with business state, fenced dispatch/reconciliation and signed provider evidence with immutable recipient/generation handling. (Supports the source AC1–AC4 below.)

O2. Tests for rollback, restart, lease overlap, lost response, stale generation, bounce/out-of-order evidence and provider idempotency-window limits, plus actual controlled-provider evidence. (Supports the source AC1–AC4 below.)

O3. Safe operational status/recovery evidence and owner-approved message examples; no guaranteed inbox/read receipt or globally exactly-once email claim. (Supports the source AC1–AC4 below.)

### To dos

T1. Implement durable transactional email intent, fenced dispatch and signed evidence reconciliation with immutable recipient/generation rules. (AC1–AC4 below.)

T2. Prove rollback/restart/lost-response/bounce/retention behavior and controlled-recipient provider results; obtain business wording feedback without live customer sends. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Expected failure record:
Business transaction rolls back → no notice intent survives.
Commit succeeds; send response lost → status unknown; reconcile same logical send.
Past provider idempotency retention → no blind retry that could duplicate mail.
Provider accepts/delivers-to-server/bounces → record only observed evidence.
Recipient or generation changed → apply reviewed fencing, not an old draft send.
```

### Verify and hand off

Use actual provider sandbox/control-recipient outcomes and real DB transaction/lease checks; signature, authorization and PII-redaction tests remain mandatory. A mocked provider does not establish transport behavior.

**Human handoff:** Provide BO-GK-024 representative confirmation/cancellation/refund emails and simple status explanations. She approves wording; you demonstrate failures and operate the safe test recipient.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Red DB rollback test leaves neither business change nor notice; commit+worker restart sends one logical operation with stable provider key.
AC2. Lost response keeps unknown and reconciles original provider reference; past idempotency retention blocks blind resend, bounce/late accepted preserves truthful evidence.
AC3. Selected provider sandbox controlled recipient demonstrates accepted/delivery-or-bounce evidence and valid/invalid signature handling; unauthorized access and log PII rejected.
AC4. Record exact input/tested revision, results, independent review and Creator acceptance on Trello; account for all related PRs, required review/checks, native auto-merges and main CI. Release/deploy/smoke only when separately authorized and in scope.

<!-- END DEV-GK-024 -->

<a id="dev-gk-022"></a>
<!-- BEGIN DEV-GK-022 -->
## DEV-GK-022 — Publish cake catalog, variants and recipe history

### Context

Source: [GK-022](https://trello.com/c/tcV2Ilpz/36-gk-022p0o210w06-publish-cake-catalog-variants-and-recipe-history) · [complete local requirements](../design/tickets/GK-022.md). Order O210; source wave W06; P0. Dependencies: GK-020, GK-010, GK-007, GK-016, GK-018, GK-021; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-022](business-owner.uk.md#bo-gk-022); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Deliver editable catalog/variants/recipes/images while preserving every purchased or retained reference.

### Before you start

Use all source prerequisites, including accepted recipe/asset policies, auth/storage setup, GK-020 B2 and approved customer/chef sketches. Missing approved allergen disclosure blocks buying.

### Expected outputs

O1. Integrated manager catalog/variant/immutable-recipe/image workflow and paginated public API/screens, including validated recoverable uploads and image revision IDs. (Supports the source AC1–AC4 below.)

O2. Real authorization, snapshot, archive/upload/cleanup and query/performance evidence; prove the catalog side of C12 image pinning shared with GK-027. (Supports the source AC1–AC4 below.)

O3. Owner-visible content walkthrough matching the approved designs; migration/delivery/review evidence with exact source and asset revisions. (Supports the source AC1–AC4 below.)

### To dos

T1. Implement catalog/variant/recipe/image APIs and chef workflows; validate uploads and authorized writes, preserve immutable historical snapshots and C12 pinned image bytes. (AC1–AC4 below.)

T2. Run storage interruption, archival, cleanup/pin boundary, accessibility and query/performance checks; provide an owner-facing demo of approved content. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Expected fixture:
Order O bought recipe v1/price P1. Publish recipe v2/price P2.
Read O → original recipe/allergens/price remain v1/P1.
Replace image A with B → new immutable image revision/key for B.
If retained request/quote pins A → catalog cleanup must preserve A bytes.
Invalid/oversized/anonymous upload → denied; interrupted upload is recoverable.
```

### Verify and hand off

Inspect actual screens and source role/storage behavior, including same-row pin/cleanup serialization and deletion tombstone admission. Record query/index results against the stated fixture rather than claiming an unmeasured performance pass.

**Human handoff:** Prepare BO-GK-022 one genuine content fixture and an older order before changing recipe/price/image. Demonstrate failed upload and history preservation; do not ask her to manufacture concurrency.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Red fixture changes active recipe/price then reads earlier order snapshot: purchased ingredients/allergens/price remain unchanged; archive preserves references.
AC2. Manager upload valid photo publishes alt/dimensions/variants; invalid MIME/oversize/anonymous write rejected, interrupted storage upload recoverable without broken public card.
AC3. Chef and customer screens match approved sketch; keyboard/error/loading/empty states verified; paginated query/index plan measured on proposed 500-product fixture.
AC4. Record exact input/tested revision, results, independent review and Creator acceptance on Trello; account for all related PRs, required review/checks, native auto-merges and main CI. Release/deploy/smoke only when separately authorized and in scope.

<!-- END DEV-GK-022 -->

<a id="dev-gk-023"></a>
<!-- BEGIN DEV-GK-023 -->
## DEV-GK-023 — Reserve production, ready stock and pickup independently

### Context

Source: [GK-023](https://trello.com/c/NHHBokl1/37-gk-023p0o220w06-reserve-production-ready-stock-and-pickup-independently) · [complete local requirements](../design/tickets/GK-023.md). Order O220; source wave W06; P0. Dependencies: GK-020, GK-001, GK-007, GK-018, GK-021; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-023](business-owner.uk.md#bo-gk-023); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Reserve production, finite ready stock, pickup and delivery resources consistently.

### Before you start

Use accepted GK-001/007 policies, approved chef sketch, identity and GK-020 B2. Read every dependency and capacity contract before editing shared resources.

### Expected outputs

O1. Atomic versioned production-day/ready-lot/pickup/delivery operations and admin/public behavior that respects existing commitments. (Supports the source AC1–AC4 below.)

O2. Real multi-process PostgreSQL tests for last unit/slot, combined rollback, below-commitment edits and timezone/DST validity; privacy-safe public schedule evidence. (Supports the source AC1–AC4 below.)

O3. Owner-readable examples using her exact capacities/lead times/zones, with tested revision and reviewed delivery record. (Supports the source AC1–AC4 below.)

### To dos

T1. Implement atomic production/ready-stock/pickup/delivery resources using approved policy and role gates. (AC1–AC4 below.)

T2. Prove multi-process last-slot exclusion, rollback, protected commitments, public privacy and DST validity; provide business examples without asking owner to inspect locks. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Expected concurrency fixture:
Capacity remaining: one pickup slot. Two processes attempt reservation.
Exactly one succeeds; the other receives a conflict; no partial resource holds.
A ready-stock cake consumes its ready unit + required handover resources,
not a second future production reservation.
Reduce capacity below existing commitments → reject/resolve per approved policy,
never silently invalidate booked orders.
```

### Verify and hand off

Prove all-resource atomicity and authorized admin operations in real PostgreSQL, not an in-process mutex. Validate server delivery-zone fee selection, blackout edits and absence of private order data in public availability.

**Human handoff:** Prepare BO-GK-023 a ready cake, made-to-order cake, full slot, reduced-capacity and delivery-boundary examples. Show business results; you own the race/rollback testing.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Real PostgreSQL red test races two processes for last unit/slot: one succeeds and one 409 with no partial holds or overbooking.
AC2. Hybrid ready-stock order does not reserve fresh production; made-to-order reserves snapshot weight; combined resource failure rolls all back.
AC3. Policy/blackout capacity edit cannot invalidate existing bookings; public shareable schedule exposes no customer/order data; DST slots use valid instants/labelled timezone.
AC4. Record exact input/tested revision, results, independent review and Creator acceptance on Trello; account for all related PRs, required review/checks, native auto-merges and main CI. Release/deploy/smoke only when separately authorized and in scope.

<!-- END DEV-GK-023 -->

<a id="dev-gk-034"></a>
<!-- BEGIN DEV-GK-034 -->
## DEV-GK-034 — Build verified independent component release pipelines

### Context

Source: [GK-034](https://trello.com/c/L9d2rl39/38-gk-034p0o230w06-build-verified-independent-component-release-pipelines) · [complete local requirements](../design/tickets/GK-034.md). Order O230; source wave W06; P0. Dependencies: GK-020, GK-013, GK-014, GK-006; preserve exact source milestone deliverables.

No separate owner card: Pipeline mechanics are developer work; business recovery targets and release permission remain BO-GK-004/014/036.

**Outcome:** Build independently releasable UI/API delivery and recovery infrastructure with enforced safeguards.

### Before you start

All source delivery/hosting/monitoring/access prerequisites and GK-020 B2 are required. Use the agreed release-only policy; validation is nonproduction and does not grant a production release.

### Expected outputs

O1. Component pipelines/manifests identifying trusted main commit, immutable artifact digest, target/version and counterpart/schema compatibility; least-privilege separate secrets and pinned Actions. (Supports the source AC1–AC4 below.)

O2. Nonproduction event/permission/compatibility/migration-serialization/replay/rollback evidence, including denial of bad triggers and red exact-main CI. (Supports the source AC1–AC4 below.)

O3. Infrastructure restore harness with DB AND object isolation, dispatch freeze/epoch/old-worker fencing and reconciliation hooks; explicit handoff to GK-035 real-domain recovery proof. (Supports the source AC1–AC4 below.)

### To dos

T1. Implement/test separate release pipelines, trusted exact artifacts, compatibility, migration serialization, rollback and least-privilege secrets in nonproduction. (AC1–AC4 below.)

T2. Prove infrastructure restore harness with dispatch freeze; GK-035 must still prove integrated real domain/provider recovery. Technical pipeline acceptance is not production release approval. (AC1–AC4 below.)

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Expected release-event matrix:
Branch push / draft release / wrong prefix / untrusted or red-main artifact → no deploy.
Valid authorized component event in sandbox → deploy exact verified artifact.
Repeated event → no duplicate migration/deploy; concurrent release cannot cancel migration.
Rollback → compatible prior artifact; no destructive data downgrade.
Restore harness evidence alone → insufficient for GK-035 financial/email recovery.
```

### Verify and hand off

Run the event denial cases, previous/current counterpart matrix, one controlled migration and target/version smoke evidence. Keep production credentials/release authorization behind their required gate. The later integrated restore must reconcile actual domain/provider effects.

**Human handoff:** No standalone owner pipeline task. Owner recovery/spend/authority decisions come from BO-GK-004/014/036; route changes there. Technical acceptance requires the agreed reviewer/human evidence.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Nonproduction red event fixtures: branch push/draft/wrong-prefix/untrusted/red-main release cannot deploy; valid component event deploys exact artifact and replay does not duplicate migration/deploy.
AC2. UI/API previous-current compatibility matrix passes; API rollback does not delete data and concurrent releases cannot cancel migration or downgrade unexpectedly.
AC3. Job/PR secrets isolated, Actions pinned, least permissions, target/version/digest/smoke evidence recorded in sandbox; production credentials wait for authorized release.
AC4. Record exact input/tested revision, results, independent review and Creator acceptance on Trello; account for all related PRs, required review/checks, native auto-merges and main CI. Release/deploy/smoke only when separately authorized and in scope.

<!-- END DEV-GK-034 -->

<a id="dev-gk-025"></a>
<!-- BEGIN DEV-GK-025 -->
## DEV-GK-025 — Collect full payment with safe holds and discounts

### Context

Source: [GK-025](https://trello.com/c/vyFTtISc/39-gk-025p0o240w07-collect-full-payment-with-safe-holds-and-discounts) · [complete local requirements](../design/tickets/GK-025.md). Order O240; source wave W07; P0. Dependencies: GK-021, GK-022, GK-023, GK-024, GK-011, GK-003, GK-007; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-025](business-owner.uk.md#bo-gk-025); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Collect full approved payment once logically, with safe reservations and recovery for uncertainty or late capture.

### Before you start

All source catalog/capacity/identity/email/provider/discount/policy prerequisites plus GK-020 B2 must be accepted. Build from approved server money/resource contracts.

### Expected outputs

O1. Quote/checkout/reservation/collection/webhook/reconciliation behavior with immutable price/recipe/policy snapshots, server integer-pence calculation and stable operation identity. (Supports the source AC1–AC4 below.)

O2. Real PostgreSQL resource/discount/key races and provider sandbox duplicate/out-of-order/lost-response/terminal-expiry/late-capture evidence. (Supports the source AC1–AC4 below.)

O3. Owner-readable expected totals and paid/pending/late outcome demonstration; exact tested revision, independent review and complete delivery links. (Supports the source AC1–AC4 below.)

### To dos

T1. Implement server-priced full-payment checkout with atomic resource/discount holds and durable provider operations. (AC1–AC4 below.)

T2. Prove exact money allocation, concurrency, duplicate/out-of-order/unknown outcomes, terminal-unpaid expiry and late-capture full-return recovery; give owner a readable sandbox demonstration. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Expected payment recovery fixture:
Checkout quote changes → show new total and require renewed consent.
Provider create response lost → reconcile/retry original logical operation safely.
Browser success return alone → does not confirm payment.
Only verified terminal-unpaid releases holds.
Capture arrives after terminal expiry → retain terminal order, create full-return
recovery goal/manager resolution; no silent fulfillment or replacement order.
```

### Verify and hand off

Check wrong currency, overflow, client price tampering, exact discount allocation, concurrent usage caps and atomic all-resource rollback. Run provider faults with actual controlled identifiers. Unknown funds remain reserved/actionable according to contract.

**Human handoff:** Give BO-GK-025 exact expected GBP examples and a safe test-payment walkthrough, including late payment. Ask her to validate business meaning; do not make her cause provider timeouts.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Red money fixtures verify 1001+1000p at 10%=200p under approved policy, allocations sum, usage cap concurrent, wrong currency/overflow/client price rejected; changed quote needs explicit reapproval.
AC2. Last resource and duplicate key tests on real PostgreSQL leave one order/collection and no partial holds. Provider timeout reuses original operation; return URL alone never confirms.
AC3. Expiry releases only verified terminal-unpaid; late capture preserves terminal lifecycle and creates durable full-return goal/manager resolution without fulfillment; replacement order needs separately approved consent. Provider sandbox duplicate/out-of-order/lost-response cases recorded.
AC4. Record exact input/tested revision, results, independent review and Creator acceptance on Trello; account for all related PRs, required review/checks, native auto-merges and main CI. Release/deploy/smoke only when separately authorized and in scope.

<!-- END DEV-GK-025 -->

<a id="dev-gk-028"></a>
<!-- BEGIN DEV-GK-028 -->
## DEV-GK-028 — Deliver appetising discovery and public availability

### Context

Source: [GK-028](https://trello.com/c/t4HD6N0Z/40-gk-028p1o250w07-deliver-appetising-discovery-and-public-availability) · [complete local requirements](../design/tickets/GK-028.md). Order O250; source wave W07; P1. Dependencies: GK-022, GK-023, GK-016; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-028](business-owner.uk.md#bo-gk-028); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Deliver the approved public cake-discovery experience with usable availability and recovery.

### Before you start

Use approved discovery design/content and source catalog/capacity prerequisites, plus verified GK-020 B2 commands. Do not invent product/allergen copy or performance results.

### Expected outputs

O1. Responsive gallery/detail/public availability with real approved images, price/servings/allergens, early date choice, buy/custom actions and accessible states. (Supports the source AC1–AC4 below.)

O2. Recorded mobile/desktop/keyboard/reduced-motion/contrast walkthroughs, public-data privacy checks and image/LCP results with actual network/device fixture. (Supports the source AC1–AC4 below.)

O3. Owner observations and design comparison tied to the actual UI revision; issue disposition and reviewed delivery evidence. (Supports the source AC1–AC4 below.)

### To dos

T1. Implement approved responsive discovery, accessible states and privacy-safe public availability. (AC1–AC4 below.)

T2. Record real mobile/desktop/keyboard walkthrough and image/LCP measurements against agreed fixtures, plus owner feedback. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Example observation/evidence row:
Scenario: new customer finds cake, checks servings/allergens and selects date.
Device/network: [actual fixture]; observed path/result: [recorded]
Unavailable or failed API: visible recovery action, not a blank gallery.
Metric: LCP [measured value] against [agreed target]; no invented conversion lift.
Public payload inspection: no order/customer data.
```

### Verify and hand off

Measure the agreed fixture rather than claiming “fast”. Verify archived/unavailable/error/loading/focus states and exact approved visual revision; include genuine owner walkthrough observations.

**Human handoff:** Give BO-GK-028 a target cake, expected fields, comparable approved previews and safe error examples. Ask where she got stuck; you own instrumentation and browser/accessibility verification.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. New customer can find a cake, inspect servings/allergens and choose date early on mobile/desktop; instrument observed walkthrough, no invented conversion metric.
AC2. Unavailable/archived option and failed API have visible recovery; loading/focus/error states and reduced-motion/contrast checks match approved revision.
AC3. Anonymous schedule/catalog payloads leak no order/customer data; responsive image/LCP proposal measured with recorded device/network fixture.
AC4. Record exact input/tested revision, results, independent review and Creator acceptance on Trello; account for all related PRs, required review/checks, native auto-merges and main CI. Release/deploy/smoke only when separately authorized and in scope.

<!-- END DEV-GK-028 -->

<a id="dev-gk-026"></a>
<!-- BEGIN DEV-GK-026 -->
## DEV-GK-026 — Cancel and refund safely across production races

### Context

Source: [GK-026](https://trello.com/c/xiEN4jvp/41-gk-026p0o260w08-cancel-and-refund-safely-across-production-races) · [complete local requirements](../design/tickets/GK-026.md). Order O260; source wave W08; P0. Dependencies: GK-025, GK-024, GK-001, GK-003; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-026](business-owner.uk.md#bo-gk-026); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Implement cancellation and refunds that remain correct across production, pickup and provider races.

### Before you start

Use approved positive cancellation/exception/ready-stock/amount policies and all source payment/email/provider prerequisites. Unresolved owner policy blocks implementation acceptance.

### Expected outputs

O1. Authoritative order-lock cutoff arbitration, durable full-return obligation and per-capture active/succeeded reservations, with purchased-component allocation and offline-return evidence. (Supports the source AC1–AC4 below.)

O2. Deterministic real PostgreSQL interleavings and real-provider fault/reconciliation evidence; include external refunds/disputes and restored-state dispatch quarantine. (Supports the source AC1–AC4 below.)

O3. Customer/chef recovery outcome examples distinguishing order status, pending/succeeded refunds and remaining obligation; complete reviewed delivery record. (Supports the source AC1–AC4 below.)

### To dos

T1. Implement approved cancellation/production arbitration, cumulative refund reservations, full-return goals and evidence-based offline returns. (AC1–AC4 below.)

T2. Prove exact-cutoff and pickup races, pending-then-failed partial shortfall, provider uncertainty/retention and external-refund/dispute/restore quarantine; keep all technical invariants mandatory. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Expected interleaving examples:
Cancel wins before effective start → cancelled + durable refund intent/goal.
Start wins / exact cutoff reached (even if timer late) → cancellation rejected.
A 4000p partial remains pending while other 6000p is returned from 10000p capture.
If partial fails → full-return goal still owes 4000p; it cannot disappear.
Unknown provider refund → keep reservation; do not free money for another refund.
```

### Verify and hand off

Run the source’s cumulative-limit and simultaneous-request fixtures, cutoff-equality and pickup races, lease overlap/retention expiry and offline paths. Restore/external observation reconciliation must finish before unsafe dispatch resumes; cancellation is never proof money was returned.

**Human handoff:** Prepare BO-GK-026 the approved before/at/after-start cases and a labelled refund balance table. Explain unknown/failed/offline examples using controlled tests without real payouts.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Real PostgreSQL red interleavings: cancel-first cancels with goal/intent, start-first rejects with zero refund; exact cutoff rejects even when job delayed; pickup race cannot reopen saved eligibility.
AC2. Capture10000p, succeeded2000p, unknown3000p and simultaneous4000p requests accepts only one; full return reserves remaining1000p. Pending partial4000 fails after6000 refund: full-return goal remains short4000 and visible.
AC3. Provider timeout/lease overlap/retention expiry does not pay twice; external refund/dispute/restore freezes dispatch for complete reconciliation; offline payments use separate evidence-based return path. Unknown/failed money and cancelled lifecycle never conflated.
AC4. Record exact input/tested revision, results, independent review and Creator acceptance on Trello; account for all related PRs, required review/checks, native auto-merges and main CI. Release/deploy/smoke only when separately authorized and in scope.

<!-- END DEV-GK-026 -->

<a id="dev-gk-027"></a>
<!-- BEGIN DEV-GK-027 -->
## DEV-GK-027 — Convert bespoke agreements and manual orders to payment

### Context

Source: [GK-027](https://trello.com/c/G8hLmSBU/42-gk-027p1o270w08-convert-bespoke-agreements-and-manual-orders-to-payment) · [complete local requirements](../design/tickets/GK-027.md). Order O270; source wave W08; P1. Dependencies: GK-021, GK-022, GK-023, GK-024, GK-025, GK-016, GK-018, GK-012, GK-005; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-027](business-owner.uk.md#bo-gk-027); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Preserve the exact bespoke agreement and inspiration through manual order, payment and reorder.

### Before you start

Use all source prerequisites, especially GK-021/022/023/024/025, approved customer/chef flows, Instagram ownership and retention/deletion policy. Test provider payment remains controlled.

### Expected outputs

O1. Custom draft/summary/recovery and manual agreed quote-to-order/payment flow with proof-based association, explicit quote version consent, capacity checks and offline provenance. (Supports the source AC1–AC4 below.)

O2. Exact selected-image C12 integration: paired source fields, atomic PATCH/freeze, server-owned object resolution, pin/cleanup lock/tombstone, authorized retained retrieval and approved deletion. (Supports the source AC1–AC4 below.)

O3. Tests and owner walkthrough for changed/expired quotes, duplicate conversion, full test payment, offline receipt/return evidence, actual mobile/desktop copy/open failures and current-choice reorder. (Supports the source AC1–AC4 below.)

### To dos

T1. Implement exact quote-to-order/payment conversion, proof-based association, current-choice reorder and offline provenance boundaries. (AC1–AC4 below.)

T2. Preserve selected immutable image revision across draft/PATCH/freeze/quote/retrieval and pin/cleanup races, plus safe generic-null case and approved deletion; test mobile/desktop handoff failure honestly. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Expected C12 and conversion fixture:
Customer views A; catalog replaces it with B before summary freeze.
Explicit A selection → preserve valid A or REFERENCE_UNAVAILABLE/reselection,
never silently substitute B. Half-pair source fields → validation failure.
Freeze A; later archive/cleanup → authorized owner/associated manager retain A;
foreign user cannot retrieve it. Pin/cleanup race cannot leave a broken reference.
Same approved quote version retried → at most one order; test full payment.
Offline receipt/return → separate actual evidence, not invented provider capture.
```

### Verify and hand off

Cover generic null source, source-association/visibility validation, stale summary revision, quote image provenance and all retained links before pin release. No client object keys/arbitrary URLs. Reorder revalidates current options and never auto-pays; opening Instagram never proves DM sent.

**Human handoff:** Provide BO-GK-027 one end-to-end bespoke example, gallery image replacement, full test payment and separate offline receipt/return demonstrations. Collect her approval of the exact agreed details and recovery wording.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Red retry converts one quote version at most once; expired/changed quote and lost capacity return revalidation without duplicate order/payment.
AC2. Clipboard/Instagram unavailable retains selectable summary and same request reference; mobile/desktop account-device check records actual handoff with no DM-sent claim.
AC3. Reorder from history/gallery resolves current variants/price/allergens/availability; archived source offers request; chef cannot bypass capacity or attach order to guessed customer.
AC4. Record exact input/tested revision, results, independent review and Creator acceptance on Trello; account for all related PRs, required review/checks, native auto-merges and main CI. Release/deploy/smoke only when separately authorized and in scope.

<!-- END DEV-GK-027 -->

<a id="dev-gk-031"></a>
<!-- BEGIN DEV-GK-031 -->
## DEV-GK-031 — Run chef fulfillment and atomic pickup changes

### Context

Source: [GK-031](https://trello.com/c/6moC1CyN/43-gk-031p0o280w09-run-chef-fulfillment-and-atomic-pickup-changes) · [complete local requirements](../design/tickets/GK-031.md). Order O280; source wave W09; P0. Dependencies: GK-025, GK-026, GK-018, GK-001; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-031](business-owner.uk.md#bo-gk-031); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Make chef fulfillment and pickup changes safe, atomic and understandable.

### Before you start

Use approved pickup/production policy, payment/refund prerequisites, chef sketch and GK-020 B2. Keep all manager and fulfillment-role boundaries from the source.

### Expected outputs

O1. Validated role-scoped production/ready/dispatch/complete actions and delivery-exception recording, with transactional history/notifications. (Supports the source AC1–AC4 below.)

O2. Atomic pickup move and source/target slot locking tests, exact-cutoff/manual/timer arbitration, rollback and stale-version behavior. (Supports the source AC1–AC4 below.)

O3. Chef-facing recovery walkthrough and tested source revision, independent review and delivery evidence. (Supports the source AC1–AC4 below.)

### To dos

T1. Implement validated role-scoped fulfillment and atomic pickup moves under approved cutoff rules. (AC1–AC4 below.)

T2. Prove target-slot contention/rollback, timer/manual deduplication, unchanged cancellation eligibility and no refund/role-edit privilege for fulfillment staff. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Expected pickup test:
Original slot: 12:00. Two orders race for final 13:00 slot.
Winner moves; loser retains 12:00 and receives conflict.
Failure after reserving target → neither appointment changes.
Later pickup does not reopen cancellation contrary to the approved saved rule.
Fulfillment staff can mark permitted progress, cannot refund or edit roles.
```

### Verify and hand off

Use deterministic real PostgreSQL interleavings for target contention, partial rollback and duplicated manual/timed start. Validate notification/history deduplication and customer/chef error wording without leaking internals.

**Human handoff:** Give BO-GK-031 controlled old/new/full slots and a restricted staff account. Demonstrate exact-boundary and delivery-exception cases; she checks her workday, you prove the races.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Real PostgreSQL red race for target pickup: exactly one move succeeds, loser keeps original; rollback after target reservation changes neither appointment.
AC2. At exact cutoff or after manual start move/cancel follows approved policy; moving later cannot reopen cancellation. Duplicate timer/manual start one transition/history/notification.
AC3. Fulfillment staff can valid ready/dispatch/complete and record delivery exception, cannot refund/role-edit; chef screens show stale/conflict/recovery without technical jargon.
AC4. Record exact input/tested revision, results, independent review and Creator acceptance on Trello; account for all related PRs, required review/checks, native auto-merges and main CI. Release/deploy/smoke only when separately authorized and in scope.

<!-- END DEV-GK-031 -->

<a id="dev-gk-029"></a>
<!-- BEGIN DEV-GK-029 -->
## DEV-GK-029 — Deliver account history, favourites and profile UI

### Context

Source: [GK-029](https://trello.com/c/VLFYxaee/44-gk-029p1o290w09-deliver-account-history-favourites-and-profile-ui) · [complete local requirements](../design/tickets/GK-029.md). Order O290; source wave W09; P1. Dependencies: GK-021, GK-016, GK-027; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-029](business-owner.uk.md#bo-gk-029); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Deliver account/profile/history/favourites UI that respects ownership and explains recovery.

### Before you start

Use source identity/custom/reorder dependencies, approved account sketches and GK-020 B2. Interpret source identity/session contract exactly, not from UI assumptions.

### Expected outputs

O1. Approved responsive account UI including editable Instagram handle, favourites/archived cases, own order history, proof-based claim and current-option reorder. (Supports the source AC1–AC4 below.)

O2. Authorization, non-enumerating claim, expiry/reuse/other-user, sign-out/private-cache/session-expiry and accessibility evidence. (Supports the source AC1–AC4 below.)

O3. Owner walkthrough that actually edits/saves a handle, revisits history/favourites, handles expired login and sign-out/re-entry; complete delivery/approval references. (Supports the source AC1–AC4 below.)

### To dos

T1. Implement approved account/history/favourites/profile and proof-based claim UI; revalidate reorder through GK-027. (AC1–AC4 below.)

T2. Verify session expiry/sign-out/private-cache boundaries, non-enumerating errors, keyboard/focus and archived/empty states; record owner walkthrough. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Expected walkthrough/test fixture:
Sign in as A → change Instagram handle → save/reload → see validated value.
Claim B's order / expired or reused proof → no association or leaked order data.
Session expires → clear re-entry path; logout → private data not served to next user.
Archived favourite remains safely labelled; reorder requires current validation.
A handle value never authorizes identity or an automatic Instagram message.
```

### Verify and hand off

Check escaped/validated handle and owner-specific reads/mutations, token-proof single use, real browser private cache boundaries and keyboard/focus/empty/error states. UI sign-out alone is not a claim of universal token revocation.

**Human handoff:** Provide BO-GK-029 controlled accounts/history and the explicit handle-edit, expired-session, logout and re-entry tasks. Supply the faults and proof examples; she evaluates the wording/usability.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Customer can sign in, save favourite, revisit archived item, update private handle and inspect only own orders; no handle proof claim.
AC2. Claim flow requires prescribed evidence; expired/reused token and other-user order have non-enumerating errors with no hidden association.
AC3. Approved mobile/desktop buttons, keyboard/focus and empty/history/error/session-expiry flows verified; no cached private pages exposed across sign-out.
AC4. Record exact input/tested revision, results, independent review and Creator acceptance on Trello; account for all related PRs, required review/checks, native auto-merges and main CI. Release/deploy/smoke only when separately authorized and in scope.

<!-- END DEV-GK-029 -->

<a id="dev-gk-033"></a>
<!-- BEGIN DEV-GK-033 -->
## DEV-GK-033 — Expose email recovery, audit and business metrics to chef

### Context

Source: [GK-033](https://trello.com/c/3YbFGdmp/45-gk-033p1o300w09-expose-email-recovery-audit-and-business-metrics-to-chef) · [complete local requirements](../design/tickets/GK-033.md). Order O300; source wave W09; P1. Dependencies: GK-024, GK-025, GK-026, GK-018, GK-014, GK-005; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-033](business-owner.uk.md#bo-gk-033); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Expose useful, privacy-safe email recovery, audit and money/activity reports to the chef.

### Before you start

Use source email/financial/identity/chef prerequisites, approved metric/privacy rules and GK-020 B2. Custom message scope is transactional unless separately approved.

### Expected outputs

O1. Authorized custom-message preview/send with bound recipient/content evidence, durable send state and actionable recovery; restricted redacted audit trail. (Supports the source AC1–AC4 below.)

O2. Date-bounded authoritative captured/refunded/known-fee metrics, separate pending/unknown amounts and honest approved analytics definitions. (Supports the source AC1–AC4 below.)

O3. Changed-recipient, unauthorized-role, duplicate-send and money-fixture evidence plus owner-readable reports and delivery/acceptance record. (Supports the source AC1–AC4 below.)

### To dos

T1. Implement authorized custom-message preview/send with recipient fencing, audit, email recovery and bounded metrics. (AC1–AC4 below.)

T2. Prove changed-recipient rejection, duplicate send safety, exact captured/refunded/fee accounting and privacy-safe analytics; provide owner-readable reporting examples. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Expected fixtures:
Preview recipient A, then change to B → stale send rejected; no job created.
Captured 10000p − successful refunds 2000p = receipts 8000p.
Known fee 100p → after-known-fees 7900p; do not subtract discount twice.
Pending refunds/unknown fees shown separately; this is not business profit.
Accepted/delivered-to-server/bounced/unknown remain distinct email evidence.
```

### Verify and hand off

Run permission/recipient fencing/replay and arithmetic fixtures against authoritative records. Avoid claiming email read/inbox receipt, unique people from pageviews, or accounting completeness. Verify personal data stays out of public analytics/logs.

**Human handoff:** Prepare BO-GK-033 controlled email states/recipient and a precomputed £100/£20/£1 example. Explain next actions and metric limits; let her review the actual preview before any controlled send.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Red custom-message test changes recipient after preview: send rejects, no job; unauthorized role and duplicate send cannot leak or send twice.
AC2. Capture10000p/refund2000p/knownfee100p yields receipts8000p/after-known-fees7900p, discount not subtracted again; pending refund and unknown fees labelled separately.
AC3. Chef traces accepted vs delivered_to_server/bounced/unknown, sees actionable queue and redacted audit references; analytics scope/privacy and blocked metrics are honest.
AC4. Record exact input/tested revision, results, independent review and Creator acceptance on Trello; account for all related PRs, required review/checks, native auto-merges and main CI. Release/deploy/smoke only when separately authorized and in scope.

<!-- END DEV-GK-033 -->

<a id="dev-gk-030"></a>
<!-- BEGIN DEV-GK-030 -->
## DEV-GK-030 — Deliver checkout, order timeline and refund recovery UI

### Context

Source: [GK-030](https://trello.com/c/BkYDridR/46-gk-030p0o310w10-deliver-checkout-order-timeline-and-refund-recovery-ui) · [complete local requirements](../design/tickets/GK-030.md). Order O310; source wave W10; P0. Dependencies: GK-025, GK-026, GK-027, GK-031, GK-017; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-030](business-owner.uk.md#bo-gk-030); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Deliver checkout and order/refund screens that faithfully reflect authoritative state.

### Before you start

Use approved GK-017 sketches and all source checkout/refund/bespoke/fulfillment dependencies with GK-020 B2. Do not replace approved policy with convenient UI assumptions.

### Expected outputs

O1. Responsive checkout, hosted-payment transition, order timeline and refund/pickup recovery screens with full saved cost/allergen/policy disclosure and consent. (Supports the source AC1–AC4 below.)

O2. Browser/API/provider sandbox evidence for changed quotes/last slots, reload/double-click stable operation recovery, return-page uncertainty, cutoff/full-slot/session states and accessibility. (Supports the source AC1–AC4 below.)

O3. Owner walkthrough of actual implementation versus the approved sketch, exact UI/API revision and delivery/acceptance record. (Supports the source AC1–AC4 below.)

### To dos

T1. Implement approved checkout/order/refund UI against authoritative operation status and consented snapshots. (AC1–AC4 below.)

T2. Verify stale-price/slot conflicts, reload/double-click recovery, cutoff/full-slot/session states and accessibility with sandbox evidence. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Expected screen fixture:
Total/allergens/policy shown before payment; stale quote → renewed consent.
Return from provider while result unknown → “checking payment”, not “paid”.
Reload/double click → retrieve the original operation result, not a new charge.
Cancelled order + pending refund → show both states separately.
Pickup target full → preserve original appointment and explain next action.
```

### Verify and hand off

Test actual operation polling/recovery and server-authoritative values, not only static screenshots. Include automatic start/exact cutoff and expired-session behavior in the recorded mobile/desktop keyboard walkthrough.

**Human handoff:** Prepare BO-GK-030 full expected totals, current numbered previews, safe payment credentials and named failure states. Ask her to explain what has happened and what the customer should do next.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Mobile/desktop shows full GBP total/tax/delivery/discount/allergens/policy before payment; stale price/last-slot conflict requires new consent.
AC2. Return URL/timeout does not show paid; cancelled order shows refund pending until evidence; double click/reload same key retrieves original outcome.
AC3. Cutoff-equality, automatic start, pickup-full and expired-session states match contracts and approved sketch; accessibility checks and sandbox walkthrough recorded.
AC4. Record exact input/tested revision, results, independent review and Creator acceptance on Trello; account for all related PRs, required review/checks, native auto-merges and main CI. Release/deploy/smoke only when separately authorized and in scope.

<!-- END DEV-GK-030 -->

<a id="dev-gk-032"></a>
<!-- BEGIN DEV-GK-032 -->
## DEV-GK-032 — Track fridge ingredients and grocery budget by date

### Context

Source: [GK-032](https://trello.com/c/lRzYGTEM/47-gk-032p1o320w10-track-fridge-ingredients-and-grocery-budget-by-date) · [complete local requirements](../design/tickets/GK-032.md). Order O320; source wave W10; P1. Dependencies: GK-022, GK-023, GK-031, GK-007, GK-018; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-032](business-owner.uk.md#bo-gk-032); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Keep physical inventory and future recipe demand accurate enough for a dated grocery plan.

### Before you start

Use approved GK-007 units/precision/consumption/cost policy, catalog/recipe/capacity/chef design dependencies and GK-020 B2. Do not guess conversions or silently clamp discrepancies.

### Expected outputs

O1. Immutable recipe-version demand, exact unit-validated stock movements/lots/expiry/waste and role-scoped corrections with operation identity. (Supports the source AC1–AC4 below.)

O2. Bounded-date groceries view separating usable on-hand, committed/planned demand, shortages and estimated cost with as-of assumptions. (Supports the source AC1–AC4 below.)

O3. Real DB duplicate/concurrent movement, version/cancellation/actual-use and invalid-unit tests; chef walkthrough and delivery evidence. (Supports the source AC1–AC4 below.)

### To dos

T1. Implement immutable recipe-based demand, exact inventory movements, expiry/waste and bounded grocery/cost forecasts. (AC1–AC4 below.)

T2. Prove duplicate/concurrent adjustments, incompatible units and cancellation-versus-actual-use behavior; keep ready-stock demand from double counting and expose discrepancies. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Expected fixture:
Order keeps recipe v1 after active recipe changes to v2.
Usable on-hand: 800g; relevant outstanding demand: 1000g → shortage 200g.
Already consumed goods stay consumed after order cancellation.
Ready stock does not add future recipe demand twice.
Same consumption operation replayed → one movement; incompatible units rejected.
Estimated cost records price date/assumptions, not an accounting guarantee.
```

### Verify and hand off

Check planned versus actual outstanding use under approved rules, expiry eligibility, audited discrepancy/negative-stock policy and snapshot recipes. Compare the owner-readable result to a manual worked fixture, not just implementation-derived expected values.

**Human handoff:** Provide BO-GK-032 a small authentic recipe/order/stock fixture and independently calculated totals. Help her record actual-use/receipt/waste examples; you supply concurrent/duplicate failure demonstrations.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Red fixture recipe v1 order survives v2 edit; future demand uses v1, ready stock adds no future recipe demand and cancellation releases plan without restocking actual consumed goods.
AC2. Duplicate consumption/movement key applies once; simultaneous stock adjustments and unit-incompatible input rejected or explicit audited discrepancy per policy.
AC3. Chef can compare usable on-hand, committed demand, shortage and estimated cost by date with units/as-of/cost assumptions and expiry/waste; no negative quantities silently clipped.
AC4. Record exact input/tested revision, results, independent review and Creator acceptance on Trello; account for all related PRs, required review/checks, native auto-merges and main CI. Release/deploy/smoke only when separately authorized and in scope.

<!-- END DEV-GK-032 -->

<a id="dev-gk-035"></a>
<!-- BEGIN DEV-GK-035 -->
## DEV-GK-035 — Verify complete customer and chef launch scenarios

### Context

Source: [GK-035](https://trello.com/c/qtUe3uom/48-gk-035p0o330w11-verify-complete-customer-and-chef-launch-scenarios) · [complete local requirements](../design/tickets/GK-035.md). Order O330; source wave W11; P0. Dependencies: GK-028, GK-029, GK-030, GK-032, GK-033, GK-034; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-035](business-owner.uk.md#bo-gk-035); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Demonstrate that the complete candidate works for customers and the chef, including actual failure recovery.

### Before you start

All source launch slices, accepted visual revisions, business policies and controlled providers/devices must be available. Use exact candidate UI/API/schema versions; this ticket does not authorize live customer activity or production release.

### Expected outputs

O1. Executed 40-requirement acceptance matrix with scenario, fixture, exact revision/environment, expected/actual outcome, evidence, reviewer and explicit remaining blocker. (Supports the source AC1–AC4 below.)

O2. Real PostgreSQL/provider/device/accessibility end-to-end evidence plus isolated DB+object restore, complete lost-window reconciliation, rollback/alerts and measured recovery versus approved targets. (Supports the source AC1–AC4 below.)

O3. One coherent Ukrainian owner walkthrough and actual product acceptance; exact candidate manifest and unresolved-risk handoff to GK-036, with all related PR/merge/main-CI evidence. (Supports the source AC1–AC4 below.)

### To dos

T1. Run all source requirement scenarios against exact UI/API/schema versions with actual PostgreSQL/provider/device/accessibility evidence and independent review. (AC1–AC4 below.)

T2. Execute integrated DB+object restore with provider-effect reconciliation before dispatch, full lost-window coverage, alert/rollback checks and measured recovery; give owner a Ukrainian walkthrough and retain distinct technical/product verdicts. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Illustrative acceptance row, not a result:
Requirement: C06; scenario: cancel while production start contends.
Candidate UI/API/schema: [exact versions]; fixture/command: [actual]
Expected: approved single winning transition and correct refund obligation.
Observed/evidence: [must be filled by execution, not “pass” copied from this guide].
Restore case: restored pre-success queue + real provider effects → reconcile entire
lost window with dispatch frozen; prove no duplicated charge/refund/email before resume.
```

### Verify and hand off

Run complete browse/pay/fulfill and custom/Instagram/manual quote/full-test-pay paths, ready stock, reorder, groceries and recovery. Restore into isolated data+objects with revoked old workers/new epoch; replay deletion/revocation evidence and reconcile before controlled dispatch. Measure recovery, resolve blockers and obtain separate technical and product acceptance.

**Human handoff:** Prepare BO-GK-035 one self-contained plan, safe test accounts/data, version labels and understandable restore results. The owner should not assemble a release test from 30 separate cards or operate fault injection.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Every requirement maps to executed evidence or explicit unresolved blocker; all related PR/merge/main-CI revisions and independent review recorded.
AC2. Real PostgreSQL deterministic concurrency, payment/email sandbox fault evidence, actual device handoff and mobile/desktop keyboard walkthrough pass for released candidate artifacts.
AC3. Isolated DB+object restore reconciles provider effects before dispatch; counterpart rollback and alert exercise pass. Creator accepts product evidence before Done, production release separate.
AC4. Record exact input/tested revision, results, independent review and Creator acceptance on Trello; account for all related PRs, required review/checks, native auto-merges and main CI. Release/deploy/smoke only when separately authorized and in scope.

<!-- END DEV-GK-035 -->

<a id="dev-gk-036"></a>
<!-- BEGIN DEV-GK-036 -->
## DEV-GK-036 — Authorize the first production component release

### Context

Source: [GK-036](https://trello.com/c/Dtl6mdxP/49-gk-036p0o340w12-authorize-the-first-production-component-release) · [complete local requirements](../design/tickets/GK-036.md). Order O340; source wave W12; P0. Dependencies: GK-035, GK-005; preserve exact source milestone deliverables.

Owner contribution: [BO-GK-036](business-owner.uk.md#bo-gk-036); source AC1–AC3 business-facing inputs/observations and explicit acceptance as applicable. Technical assertions and all mandatory Context extensions remain yours. Owner completion never replaces technical evidence.

**Outcome:** Obtain a precise first-production-release decision with enough evidence to approve or decline safely.

### Before you start

GK-035 and GK-005 evidence and named GK-006 authority are prerequisites. Prepare the proposal without executing it. Missing production readiness blocks a positive approval; a refusal can complete the owner’s decision without satisfying unmet parent criteria.

### Expected outputs

O1. Exact candidate packet: component version/tag proposal, commit/artifact/schema/counterpart bounds, production target, operator/time scope, test/review/business readiness, payment/email/domain/spend/recovery evidence and rollback owner. (Supports the source AC1–AC4 below.)

O2. Explicit approve/decline/pending decision attributed to the authorized human and exact packet; known risks remain visible and owner-board completion never auto-completes parent readiness. (Supports the source AC1–AC4 below.)

O3. If subsequently authorized and actually executed, a separate release/deployment receipt with real tag, deployed target/digest/version, smoke result and recovery/rollback evidence. Do not fabricate this future receipt. (Supports the source AC1–AC4 below.)

### To dos

T1. Prepare exact component version/commit/artifact/schema/target/time/operator/rollback evidence and production-readiness checks for the designated approver. (AC1–AC4 below.)

T2. Record approve/decline separately from execution. If later authorized, execute only the named release scope and attach actual tag/deploy/smoke/rollback evidence; this drafting run authorizes none. (AC1–AC4 below.)

T3. Prepare the owner’s Ukrainian brief/demo, consume her exact approved evidence, resolve interpretation conflicts, and attach both contributions to the parent acceptance record (AC4).

### Worked output example

Illustrative only: expected behavior or record shape, not evidence that work ran or a policy was approved.

```text
Illustrative decision record:
Candidate packet: [immutable component/version/commit/digest + target]
Proposed operator/time: [named, scoped]; positive readiness evidence: [links]
Decision: pending / approve THIS packet / decline because [reason]
If decline: no release permission; missing parent criteria remain unmet.
If approve: execute only named scope later; acceptance is not a deploy receipt.
Actual release/deploy/smoke: [not executed unless separate evidence exists]
```

### Verify and hand off

Check actor/target/version/time scope, real account/domain/secret/content readiness and recovery evidence. A changed candidate needs revalidation of affected evidence/authorization. Read back the actual authorized operation only after it occurs; silence or a green CI run is not release approval.

**Human handoff:** Give BO-GK-036 a concise Ukrainian launch brief and copyable packet label. She can approve or refuse under the agreed authority; explain unresolved readiness honestly and keep execution distinct from her decision.

Attach the exact output location/revision, AC-to-evidence links, review/approval references and remaining blockers to the source record when authorized. Planning examples are checked documents; implementation results require the actual source tests/review/PR/merge/main-CI evidence. Release and deployment remain separate.

### Acceptance Criteria

Original source criteria (including source term “Creator”; apply the role interpretation above, without waiving any test):

AC1. Creator explicitly accepts or declines exact versions/targets and names release operator; unresolved risks remain visible.
AC2. Production payment/email/account/domain policy and recovery evidence complete; no test-mode secret or fake content is live.
AC3. If authorized, separate release operation records published tag, deployment/smoke evidence and rollback owner; no automatic deployment from this decision card.
AC4. Required acceptance evidence is tied to exact input/output revision, checked by the appropriate reviewer/Creator and recorded on Trello. For implementation include relevant tests, all related PRs, required review/checks, merge and main CI; authorized release/deployment/smoke only when separately in scope.

<!-- END DEV-GK-036 -->

