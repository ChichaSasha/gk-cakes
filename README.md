# GK Cakes

[Developer requirements (English) and business-owner tasks (Українською)](docs/roles/README.md).

[Start here: Creator/Agent order and handoffs](START-HERE.md).

Planning kit for a cake storefront and an admin workspace that a nontechnical
chef can use confidently. The original POC is research input, not an approved
architecture. No application has been implemented yet.

## Start planning

Paste the prompt in [START-PLANNING.md](docs/planning/START-PLANNING.md) into a new
task opened in this repository. It runs feature workers, a coordinating master,
an independent review, and publication of reviewed tickets to
[GK Cakes 🍰 AI Workflow Board](https://trello.com/b/QEjqyM9i).
Use the documented draft-only variation if you want local review first.

The kit itself does not start that loop or change Trello. TDD here means both
**technical design documentation** and **test-driven development**.

| Read | Purpose |
| --- | --- |
| [Project brief](docs/planning/PROJECT-BRIEF.md) | Full scope, requirement IDs, unresolved business decisions |
| [Creator setup](SETUP.md) | Account/service setup sequence, safe credentials, verification |
| [Delivery contract](docs/planning/DELIVERY.md) | PR → CI → auto-merge → main CI → release → separate UI/backend deployments |
| [Output contract](skills/gk-plan/references/artifacts.md) | Screens, architecture, module specifications, contracts, ADRs and traceability |
| [Trello contract](skills/gk-plan/references/trello.md) | Labels, priority/order/waves, self-contained tickets and safe publication |
| [Model/context check](skills/gk-plan/references/execution-check.md) | Role defaults, runtime evidence, context freshness/sufficiency, pilots and handoffs |
| [Design sessions](skills/gk-feature/references/design-sessions.md) | Iterate with the Creator until coloured sketches, buttons and behaviour are approved |

## Skills

| Skill | Use |
| --- | --- |
| [gk-plan](skills/gk-plan/SKILL.md) | Master: reconcile the whole system, coordinate workers, sequence tickets |
| [gk-feature](skills/gk-feature/SKILL.md) | Worker: feature specifications or interactive visual-design sessions with sketches and feedback |
| [gk-plan-review](skills/gk-plan-review/SKILL.md) | Fresh reviewer: check coverage, contradictions and integration readiness |
| [work-planning](skills/work-planning/SKILL.md) | Existing useful guidance for Context / To dos / Acceptance Criteria |
| [work-implementation](skills/work-implementation/SKILL.md) | Existing acceptance-first increments, executable checks and final self-check |
| [work-review](skills/work-review/SKILL.md) | Existing evidence-based code review when implementation begins |

The existing `work-*` skills contain workplace-specific Jira/manual-merge rules.
Use their engineering techniques here with **Trello and the GK Cakes delivery
contract**; do not import workplace policy into this personal project.
Ponytail is useful for choosing the smallest complete solution; it must not cut
requested capabilities, accessibility, payment correctness or recovery.
Use imagegen for visual exploration only when needed, visualize for interactive
flow exploration, and OpenAI Docs/skill-creator when evolving agent instructions.
These are available environment skills, not dependencies installed by this kit.

The three GK skills are maintained in `skills/` and linked from `.agents/skills/`
for repository discovery, following [official skill guidance](https://learn.chatgpt.com/docs/build-skills).
If the picker has not refreshed, restart the task or explicitly ask the agent to
read the linked `SKILL.md`. No custom agent runtime or API key is needed.

## Checks

Run `python3 scripts/check_planning_kit.py --self-test` and
`python3 scripts/check_planning_kit.py` from the repository root.
The [CI workflow](.github/workflows/ci.yml) runs these on PRs and pushes to main.
This checks the kit, not a future application's behaviour or a deployed system.
Repository rules, auto-merge and release deployment still need the setup and
implementation work described in the delivery contract.
