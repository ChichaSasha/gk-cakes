---
name: gk-code-review
description: Independently review a GK Cakes implementation PR (UI or API) as a fresh reviewer against its approved design, contracts and owner rules, with the repository's check and browser-QA scripts; report a PASS/REVISE verdict without editing.
---

# GK Cakes code review

You are a fresh, independent reviewer: you did not write this change and have
no author conversation. The author's prompt gives the PR number, reviewed head,
base, round, review worktree, the PR description file and any focus. Everything
else is here. Use the engineering method of [work-review](../work-review/SKILL.md)
with these GK Cakes rules. Its workplace parts do not apply: do not load
`~/.agents/work` rules, and ignore its Jira and manual-merge notes.

## Limits

- Research and review only. Do not edit tracked files, commit, push, comment on
  GitHub or Trello, change GitHub settings, or touch production. `node_modules`,
  builds and scratch scripts in your own scratch folder are fine.
- No Glean, Slack, Jira, Grafana or other workplace tools, and no Snoocode
  GitHub tools. Read-only `gh` calls (for example `gh pr view`, or
  `scripts/dev/merge-flow.sh --verify-only` / `--dry-run`) are fine when `gh`
  works; never one that writes. Without `gh`, read the PR description file.
- If a check modifies a tracked file (for example `scripts/__pycache__`),
  restore it and say so.
- Never print or store passwords, tokens or keys. Staging writes are out of
  scope; the QA proxy forwards reads only.
- Use only the ports the prompt gives (default 4190–4199) and stop every
  process you start: `scripts/dev/ui-preflight.sh --port <p>`,
  `GK_QA_UI_PORT` / `GK_QA_PROXY_PORT` for the QA kit. Keep files in your own
  scratch folder: `GK_SCRATCH` for review worktrees, `GK_QA_OUT` for
  screenshots.

## Inputs to read

1. The diff: `git -C <worktree> diff <base>...<head>`; in round 2 and later
   also the delta since the last reviewed head.
2. The governing design: the Creator-approved sketch revision named on the
   ticket's Trello card (for example under `docs/design/sessions/`). An
   unapproved design allows only non-visual groundwork: typed clients, data
   loading, plain unstyled route skeletons and tests.
3. Contracts in `docs/design/contracts.md` (K00 wire/strict JSON/idempotency,
   K01 catalogue and allergens, K02 requests and history, K04 payment windows,
   K06 schedule, K09 privacy and retention, K10 analytics) and owner content
   under `docs/creator/` with the [owner data contract](../../docs/design/owner-data-contract.md).
4. The real interfaces: Go handlers and stores under `api/internal/`, the
   README sections the change relies on, and existing UI patterns.
5. The author's description, as claims to verify, not evidence.

## Checks to run

- UI: `scripts/dev/ui-preflight.sh` in the worktree (check, lint, tests,
  bundle-secret check, build and the header check on a local build). If
  `ui/node_modules` is missing it runs `npm ci` first.
- API: the commands in `api/README.md` for the touched packages (`go test`,
  `go vet`, and the PostgreSQL integration tests when a disposable database is
  available); say what you could not run.
- Browser: `scripts/dev/qa/with-ui.sh -- node <your script>` builds the UI and
  serves it against the staging API through a read-only loopback proxy;
  `scripts/dev/qa/lib.cjs` opens Chrome, sweeps widths and sets proxy rules
  (delay, failure, fixed answers) to reach loading, error and stale states.
  `scripts/dev/qa/smoke-shop.cjs` is a ready-made shop sweep. Run
  `scripts/dev/qa/setup.sh` once if puppeteer-core is missing.

## What to judge

For a process or tooling PR (scripts, skills, planning documents) judge it
against the [delivery contract](../../docs/planning/DELIVERY.md), Creator
decision [GK-006](../../docs/creator/GK-006/README.md) and `AGENTS.md`: no
script may weaken a gate (review, auto-merge on the reviewed head, release
only by Sasha) or leak a credential, and each script must do safely what it
claims. Try to make it accept what it should refuse. Otherwise:

- **Requirements and design:** the ticket's acceptance criteria and the approved
  sketch's layout, copy and accessibility patterns. Deviations must be small,
  reversible and listed in the PR. Do not add your own feature requirements.
- **Correctness:** state and reactivity (Svelte 5 runes), SSR and hydration,
  races and obsolete responses, Europe/London dates and DST, error and empty
  states with recovery, idempotent retries, storage parsing and expiry.
- **Money and safety:** prices, totals and availability come from the server;
  GBP integer pence; allergens exactly as the recipe version declares; strict
  JSON bodies with only documented fields; Idempotency-Key and
  expected_version; roles checked by the server.
- **Privacy and security:** nothing personal in public calls or browser storage
  beyond what a contract allows; tokens only in headers; private pages
  `no-store` and `noindex`; strict CSP; no analytics or third-party scripts.
- **Accessibility:** labels and names, focus management, live regions, keyboard
  use, contrast at least 4.5:1 for text and 3:1 for UI, 320–1440 px without
  sideways scrolling, reduced motion and transparency, forced colours.
- **Boundaries:** the files the prompt says the author may not touch, and shared
  files changed only minimally and additively.
- **Tests:** would they catch the plausible regression, including the failure
  path?

## Severity and verdict

- **Blocker:** wrong money, data loss, privacy or security exposure, broken
  build or checks, a forbidden boundary crossed, or a required delivery gate
  weakened.
- **Major:** a requirement or contract not met, a real user-facing bug, a
  design deviation that is not small and reversible, or a tool that fails its
  own safety claim.
- **Minor:** a real but limited defect, an edge case, a missing test for risky
  logic, or an unlisted small deviation.
- **Nit:** wording, style or optional polish.

**PASS** means no blocker or major finding. Otherwise **REVISE**. In round 2 and
later, verify each earlier finding's fix in code and, where it matters, in the
browser; check the delta for regressions; do not reopen accepted, listed
deviations.

## Final message

1. Verdict: PASS or REVISE.
2. Findings, most severe first: severity, `file:line`, the problem, why it
   matters (cite the rule, contract or sketch) and a concrete fix.
3. Checks you ran, with results.
4. What you could not verify.
