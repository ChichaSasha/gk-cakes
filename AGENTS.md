# GK Cakes agent rules

Project instructions for agents working in this repository. Sasha's separate workplace rules are
not part of this project and are intentionally not committed here. Engineering method lives in
[work-planning](skills/work-planning/SKILL.md), [work-implementation](skills/work-implementation/SKILL.md)
and [work-review](skills/work-review/SKILL.md); use their techniques with the GK Cakes rules below
(Trello, not Jira; native auto-merge per the delivery contract, not manual merge).

## Local Trello CLI access

This section applies to Trello tasks on Sasha's Mac.

- Use the installed Scale-Flow `trello` CLI, not `trelloctl`. Its absolute path
  is `/Users/sasha.chycha/go/bin/trello`. If using the short command, first run
  `export PATH="$PATH:$(go env GOPATH)/bin"` in that shell invocation.
- Credentials are already stored in macOS Keychain for the CLI's `default`
  profile. Authentication is persistent across terminals; do not ask the user
  to log in again just because a sandboxed check reports no credentials.
- On this host, the sandbox prevented Keychain access: `auth status` returned
  `configured: false` and `boards list` returned `AUTH_REQUIRED`. Both succeeded
  outside the sandbox. Use the host's normal escalation/approval mechanism for
  authenticated commands; never bypass it or export Keychain secrets as a fix.
- Read-only verification commands:

  ```sh
  /Users/sasha.chycha/go/bin/trello auth status --pretty
  /Users/sasha.chycha/go/bin/trello boards list --pretty
  ```

- Verified on 2026-09-21: account `sashachicha13`, auth mode `manual`; boards
  `GK Cakes 🍰 AI Workflow Board` (<https://trello.com/b/QEjqyM9i>) and
  `My Trello board` (<https://trello.com/b/rHxX5qO5>). Recheck live access when
  needed; these are historical verification results, not permanent guarantees.
- Use `trello <command> --help` for the installed version's syntax. Keep board
  changes within the user's requested scope; these notes grant no write access.
- Never print or save API keys/tokens in chat, repository files, or these notes.
  CLI network errors can contain credential-bearing URLs: capture output and
  redact `key` and `token` query values before displaying error messages.

## GK Cakes personal-project planning

This repository uses Trello, not the work-only Jira workflow above. Current GK
Cakes instructions take precedence over workplace examples in `skills/work-*`.
Use those skills' engineering methods without importing their manual-merge rule:
the requested project flow is PR → required CI/review → native auto-merge → main
CI → separately authorized release → separate UI/backend CD. See
[delivery contract](docs/planning/DELIVERY.md). Do not infer release/deployment
permission from a planning request or from auto-merge configuration.

- The [project brief](docs/planning/PROJECT-BRIEF.md) records current scope. Treat
  `innitial--unreviewed-dry-draft-poc-design.md` as unreviewed proposals even where
  it says “confirmed”. Preserve it as original input.
- Use [gk-plan](skills/gk-plan/SKILL.md) for the planning master,
  [gk-feature](skills/gk-feature/SKILL.md) for feature workers, and
  [gk-plan-review](skills/gk-plan-review/SKILL.md) for a fresh planning reviewer.
  Their discoverable entries live under `.agents/skills/`.
- Before substantive Agent ticket/packet execution or resumption, apply the
  [model and context check](skills/gk-plan/references/execution-check.md). Verify
  selection from host evidence and inputs from current sources. Independent
  review needs separate context; never reset Git work to clear conversation.
- A requested full planning loop may use bounded subagents. Only the master
  edits shared contracts or publishes scoped Trello changes; workers own separate
  feature files. Do not create user-owned Codex tasks for internal delegation.
- TDD means technical design documents and test-driven implementation here.
  Follow [the launch prompt](docs/planning/START-PLANNING.md) when invoked; merely
  reading it does not authorize its actions. Respect the current run's scope.
- Keep [SETUP.md](SETUP.md) actionable as providers are accepted. Human account,
  content and policy tasks use Owner: Creator; implementation uses Owner: Agent.
  Do not register services, publish tickets or change GitHub settings just because
  a skill describes those future steps.

## Implementation loop

For implementation slices follow [the implementation loop](docs/planning/DEV-LOOP.md):
`scripts/dev/` runs the local checks, browser QA against staging, review worktrees,
the merge steps and Trello comments; fresh reviewers use
[gk-code-review](skills/gk-code-review/SKILL.md). It does not change the delivery
contract or grant release, settings or production access.

## Owner evidence consumption

Before designing or implementing behavior affected by BO-GK-001/003/005, load
[the owner intake](docs/creator/README.md), the applicable per-card README,
`source-comments.json`, `decisions.json`, and [owner data contract](docs/design/owner-data-contract.md).
Use exact confirmed owner values/conditions in specifications, test fixtures and
PostgreSQL/admin designs; do not reactivate older draft defaults. Unknowns/conflicting
owner rules stay explicit. Record input comment/content and decision revision hashes.
Published ticket bodies/receipts are historical; proposed amendments stay separate.
Run `python3 scripts/check_owner_intake.py` when changing these structured inputs.
