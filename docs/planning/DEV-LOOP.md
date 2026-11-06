# Implementation loop

How an agent takes one implementation slice from branch to staging quickly
without skipping a gate. The [delivery contract](DELIVERY.md) stays
authoritative; this page is the working routine and the scripts that run it.
Planning work follows the [gk-plan](../../skills/gk-plan/SKILL.md) skills instead.

## The loop at a glance

| Step | Command or rule | Typical time |
|---|---|---|
| Branch and worktree | own worktree, `sasha-chycha/<task>` from `origin/main` | 1 min |
| Build and quick checks | `scripts/dev/ui-preflight.sh --fast` while iterating | 10 s per run |
| Full local checks | `scripts/dev/ui-preflight.sh` (UI) or the api/README commands | 20 s UI |
| Browser QA | `scripts/dev/qa/with-ui.sh -- node <script>` | 1–3 min |
| Draft PR | sections below | 2 min |
| Fresh review | `scripts/dev/review-worktree.sh`, short prompt, [gk-code-review](../../skills/gk-code-review/SKILL.md) | 10–20 min a round |
| Merge | `scripts/dev/merge-flow.sh <pr> <reviewed-sha> --wait` | CI + staging |
| Report | `scripts/dev/trello-comment.sh <card> <file>` | 1 min |

While CI, auto-merge and the staging deployment run, start the next slice
(see "Overlap"). Waiting is the largest cost in this loop.

## 1. Slice

- One user-visible outcome per PR, ideally under about 400 changed lines
  without tests. Smaller PRs review faster and fail less.
- Work in your own worktree, never in another agent's. Do not edit files
  another open PR owns; shared files (`ui/src/lib/api.ts`, `ui/src/lib/headers.ts`,
  root layout, `app.html`, `ui/scripts/check-headers.sh`, READMEs) change only
  minimally and additively.
- Before designing behaviour, read the ticket, the approved design revision on
  its Trello card and the contracts it cites. An unapproved design allows only
  non-visual groundwork.

## 2. Build and check

- Iterate with `scripts/dev/ui-preflight.sh --fast` (check, lint, tests,
  bundle-secret check). Before the PR, run it without `--fast`: it also builds,
  checks `/healthz` on a server started without sign-in settings and runs the
  header check, as the PR's `ui` job does. Only the container image build is
  left to CI.
- API changes: the commands in `api/README.md`, including the PostgreSQL
  integration tests for anything touching migrations, reservations or money.

## 3. Browser QA

- `scripts/dev/qa/setup.sh` once (puppeteer-core outside the repository).
- `scripts/dev/qa/with-ui.sh [--auth none] -- node <script>` builds the UI,
  serves it on 4192 against the staging API through a read-only proxy on 4191
  and stops both afterwards. `scripts/dev/qa/lib.cjs` has `open`, `sweep`
  (widths and sideways scrolling), `ctl` (proxy delay, failure or fixed answer
  per path) and staff and customer sign-in. `scripts/dev/qa/smoke-shop.cjs`
  is a full shop sweep; `scripts/dev/qa/smoke-account.cjs` sweeps the
  signed-in account pages (read only; it skips the delete page, whose
  opening asks the API for a deletion preview).
- Staff and customer pages: read the test password from the Keychain into
  the command's environment only:
  `STAFF_PW="$(security find-generic-password -a gk-test-staff@gkcake.com -s gk-supabase-test-staff -w)" scripts/dev/qa/with-ui.sh -- node check.cjs`
  (customer: `CUSTOMER_PW` from `-a gk-test-customer@gkcake.com -s gk-supabase-test-customer`).
- The read-only proxy covers only API requests in local runs. Calls to the
  sign-in provider go to it directly (password sign-in, sign out — global or
  local — and email-link sends), and against a deployed site
  (`GK_QA_BASE`) there is no proxy at all, so every write a script triggers
  is real. The staging test accounts are shared: answer non-GET API requests
  and provider calls such as global sign-out and email links inside the
  browser (request interception), and never let account deletion, global
  sign-out or email links through.
- The same scripts check a deployed environment (Node from `ui/.nvmrc` on
  `PATH`; puppeteer-core needs it):
  `GK_QA_BASE=https://staging.gkcake.com node scripts/dev/qa/smoke-shop.cjs`.

## 4. Pull request

Open a draft PR with personal `gh` (`GH_CONFIG_DIR=$HOME/.config/gh`,
`-R ChichaSasha/gk-cakes`). Body sections: **Summary** (what and the approved
input it follows), **Decisions** (small, reversible choices made on the
owner's behalf), **Tests** (what ran, with results) and, after review,
**Review** (rounds, verdicts, what changed, nits left).

## 5. Review

- `scripts/dev/review-worktree.sh review-pr-<n> <n> --node-modules-from <your worktree>`
  creates a detached worktree of the PR head outside every clone (in
  `GK_SCRATCH`, default `$TMPDIR`) and reuses your installed dependencies when
  the lockfile is the same. Targets are a PR number, a commit ID (use it right
  after a push, while GitHub may still report the old head) or a branch of
  origin; local branches are never used. Use one name per reviewer (for
  example `review-pr-52-api`); only a review worktree your checkout created,
  detached and without changes, is ever refreshed or removed.
- Start a fresh reviewer (a new subagent or session, no author context) with a
  short prompt: "Use the gk-code-review skill. PR #n, head, base, worktree path,
  PR description file, round, boundaries, ports, focus". The skill holds the
  checklist, severity scale and output format.
- A PR that spans areas, for example API money or migrations and UI, gets a
  reviewer (or at least a stated focus) for each area: GK-006 asks for review
  expertise appropriate to the affected areas.
- **Stop rule.** A PASS (no blocker or major finding) ends the review. Do not
  change code in that PR for minor findings or nits unless they touch money,
  privacy, security or data loss; list them under Review and fix them in the
  next slice. If code changes after a PASS, a focused review of that delta is
  required before merging (Creator decision GK-006: fixes get affected
  rechecks).
- Save each reviewer's final report to a file in your scratch folder. After
  REVISE: fix, rerun checks, then give the next round that report file and your
  disposition of each finding to verify.

## 6. Merge

1. Post a short review record on the PR (rounds, verdicts, checks, merge head).
2. `scripts/dev/merge-flow.sh <pr> <reviewed-sha> --wait`, with the full
   commit ID the reviewer saw (never a branch name). It refuses a head that is
   not the reviewed code (it accepts clean merges of main commits whose trees
   equal Git's own merges), updates a branch that is behind main (again while
   waiting, if main moves) and checks that again, marks the PR ready, enables
   native auto-merge (merge commit) and posts `ai-review` = success only on a
   checked head, waits for the merge and the Staging run and prints its jobs. `--repost` re-posts the status when auto-merge
   stalls on a green PR; `--verify-only` checks a head against the reviewed
   commit.
3. Never merge by hand. Production releases are separate and need Sasha.

## 7. Overlap and parallel work

- While step 6 waits (about 25 minutes of CI and deployment), branch the next
  slice from `origin/main`. Branch from the open PR only when the next slice
  needs its code, and then rebase onto main after it merges.
- Two agents can work at once on separate tickets or areas, each with its own
  worktree, branch and PR. Coordinate shared files and migrations through the
  ticket's Trello comments before editing them.

## 8. Staging and Trello

- Check the deployed result with the QA scripts against staging and record
  what you saw (pages, widths, flows, errors). Staging data changes only as the
  labelled test users and only for test data.
- Report on the ticket's card with `scripts/dev/trello-comment.sh <card-id> <file>`:
  what merged (PR, merge commit), the review record link, staging evidence,
  decisions taken, open questions. Comments only; cards move to Done only with
  the board's approval.
- Creator questions: ask with the structured question tool (recommended option
  first), record the answers verbatim on the card, and post an approval only
  when Sasha explicitly says so.

## Pitfalls seen in practice

- `git -C <repo> worktree add <relative path>` resolves the path inside that
  repository; use `scripts/dev/review-worktree.sh`, which refuses that.
- Importing a script that runs its work on import (no `__main__` guard) runs
  it; read a helper before reusing it.
- SvelteKit's server-side render checks CORS on cross-origin API calls like a
  browser and sends the page origin (as `https://127.0.0.1:<port>`); a local
  proxy that does not allow it makes the page fail in confusing ways (an
  unknown cake showed "couldn't load" instead of a 404).
- Background processes started in one agent shell call may be killed when the
  call returns; start servers and the check in the same command
  (`with-ui.sh` does).
- A GitHub registry "unknown blob" error in the Staging run's image push is
  transient: rerun that run's failed jobs once.
