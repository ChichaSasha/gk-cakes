# ADR-0001: Stack and module boundaries

Status: accepted
Scope: accepted stack; current integration contracts r6 require GK-019 review; visual approval and executable verification remain separate.
Date: 2026-10-05; revision: r5 (GK-019 current contract alignment)
Decision owner: planning master for reversible technical choices; Creator for policy/spend/account ownership. Technical approver: master based on qualified Astra commerce/operations assessment; independent final review pending. Provider package accepted by Sasha in GK-004 final r13; individual paid activation still requires its applicable approval.
Affected requirements: S06 S07 D02. Consumers: feature packets, shared contracts and mapped tickets.

## Context and options

Go net/http + pgx + versioned SQL migrations and SvelteKit UI; compare Django built-in administration, one-language TS, and static-only UI. PostgreSQL is required by the brief.

## Recommendation

Prefer one modular backend and one UI artifact; customer/admin use the API, jobs reuse domain code. Go is recommended for explicit transactions, not because the POC called it confirmed. No sqlc until a query-generation benefit is demonstrated; no generic repository/broker.

## Evidence and consequences

[Evidence](../provider-research.md) checked 2026-09-21. Documentation research is not an integration test. The option minimizes operated components but still requires explicit authorization, recovery and user-facing workflow work; no free-tier production guarantee. Costs and unknowns are recorded in provider research.

## Verification and change path

Go/SvelteKit technical choice retained; GK-019 independent review checks current integration. Budget/provider selections are accepted in GK-004; no paid activation implied. Bootstrap pins versions and commands; contracts survive a pre-bootstrap stack change.

Supersedes: contradictory POC proposal for this topic only; original input remains unchanged. Superseded by: none. A change invalidates affected contract sections, feature recommendations and ticket readiness; rerun independent integration review before promotion.

Reconciliation r3: contracts.md is authoritative for exact gateway/lock order, late-payment full-return, external observation quarantine, timed-start materialization, notification generation/recipient fencing and exact inventory quantities. Pending statements in the original option analysis refer to Creator/provider/implementation evidence, not acceptance of these technical boundaries.

## UI capability check — 2026-10-05

Sasha requested confirmation that the selected UI technology can deliver a modern,
high-quality, richly interactive interface with animation where useful.
Keep SvelteKit + Svelte, using TypeScript for application logic, HTML for structure
and CSS for visual styling. This is a capability assessment, not visual-design
approval or a claim that a UI has been built/performance-tested.

- [Svelte components and reactive state](https://svelte.dev/docs/svelte/overview)
  support interactive filters, forms, galleries, drawers, live totals and admin
  actions. These are capability examples, not newly approved feature scope.
- [TypeScript support](https://svelte.dev/docs/svelte/typescript) provides typed
  component/application logic; types do not replace server validation.
- Built-in [transitions](https://svelte.dev/docs/svelte/transition) and
  [motion](https://svelte.dev/docs/svelte/svelte-motion) cover entering/leaving
  elements, smooth state changes and spring/tween effects. Use CSS and these native
  capabilities first; add a specialist library only for an approved need.
- SvelteKit supports server-rendered pages and a
  [Node deployment](https://svelte.dev/docs/kit/adapter-node), compatible with the
  selected separate Railway UI service. Interactive browser behavior does not
  require moving business rules from the Go API into the UI server.
- Quality must be verified through responsive layouts, loading/error/success states,
  keyboard/focus behavior and reduced-motion preferences. Framework accessibility
  support helps but does not prove [accessible output](https://svelte.dev/docs/kit/accessibility).

Assessment: sufficient for the requested shop and actionable admin UI. Visual
quality depends on design, assets and implementation; changing framework alone
does not supply it. Pin supported stable versions during bootstrap. No additional
UI framework, paid animation service or recurring cost is introduced here.
