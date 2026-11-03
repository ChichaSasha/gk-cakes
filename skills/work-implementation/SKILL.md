---
name: work-implementation
description: Implement an agreed engineering change or bug fix in small verified increments, using acceptance criteria, repository rules, and focused regression checks. Use when code changes are requested; it does not authorize unrelated cleanup, publishing, or deployment.
---

# Work implementation

For work tasks, read `~/.agents/work/AGENTS.md` if not already loaded.

## Establish the starting point

Read applicable AGENTS.md files, the selected work profile, the ticket or prompt,
and relevant reference material before implementation. Check the working tree
and current branch. Identify the existing implementation, its callers, generated
sources, tests, and repository commands. Do not treat examples as live config.

Restate material acceptance criteria and scope boundaries. For a missing
behavioral contract, ask about the part that changes correctness; continue
independent investigation. A small fix needs a small plan, not a mandatory
planning document. Check dependency and target-environment assumptions before
building on them.

## Define, implement, enforce

1. **Define the check.** Choose an observable check for each acceptance criterion.
   For a behavioral bug, add or adapt a focused regression test and run it
   against the old behavior when feasible. Record whether it reproduced the
   intended failure. A dependency/setup error is not a successful red test.
2. **Implement the smallest complete increment.** Fix the shared cause after
   tracing callers. Reuse the codebase, standard library, and existing tools.
   Preserve error propagation, cancellation, resource ownership, and security.
3. **Verify before expanding.** Run focused checks. Resolve failures before
   unrelated refactoring or another feature. Do not weaken the check to fit
   the implementation.
4. **Enforce standards.** Inspect the diff, simplify unnecessary code, format
   changes, and run the relevant repository-required tests and static analysis.
   Recheck affected behavior after refactoring. Regenerate and inspect derived
   outputs when their maintained inputs change.

Acceptance-first development defines the contract; it is not a substitute for
executable tests, and it is not strict test-first TDD unless a failing test was
actually run before the implementation.

## Go quality

Apply these when changing Go, alongside stricter repository policies:

- Use idiomatic, explicit code and `gofmt` on changed Go files. Follow existing
  package boundaries and naming; export only what callers need. Add descriptive
  godoc for exported types and functions.
- Handle errors explicitly. Wrap with useful operation context using `%w` when
  callers need the error chain. Preserve typed/sentinel errors with `errors.Is`
  or `errors.As`; do not branch on error-message text.
- Return an error to the responsible caller or handle/log it at the owning
  boundary; avoid duplicate logging. Do not introduce panic for normal failures.
- Close owned resources. Surface flush, commit, or close errors when they can
  mean lost data; do not let cleanup mask the original operation's failure.
- Pass `context.Context` first when present. Propagate caller cancellation and
  deadlines through I/O. Bound retries and background work; do not replace a
  live request context with `context.Background()` to escape cancellation.
- Make goroutine ownership and shutdown clear. Use channels for communication
  and mutexes for shared state where each is the simpler correct choice; do not
  turn the Go concurrency proverb into a ban on mutexes. Avoid unbounded workers
  and queues. Run relevant race checks when changing concurrency.
- Use table tests for related cases and the repository's existing assertion
  conventions. Test behavior and important failure modes, not private function
  layout. Reuse fakes; mocks alone do not prove distributed-system semantics.
- Comments should explain intent, constraints, or surprising behavior. Do not
  narrate obvious statements or add speculative extension points.

## Evidence and handoff

Report the outcome, exact checks and results, and any unmet criteria. For
multiple criteria, a compact AC → evidence/pending table helps. State the
tested revision or uncommitted state when handing work to another reviewer.
Do not describe a mock-based result as a production or integration result.

Perform a final self-check. For independent review, prepare the original
requirements, base/head or precise diff scope, applicable rules, and validation
evidence for a fresh context or human reviewer. Do not pass only your persuasive
summary as the review input. If separate review is unavailable, state that it
is pending rather than claiming independence.

Commit and push each coherent completed increment after its relevant tests
and required checks pass, using the standing work policy. Inspect the staged
diff, include only task-owned files, and verify the branch/remote. Report the
commit and push result. Do not merge PRs or enable auto-merge; the user merges.

Keep task progress in Jira when asked; otherwise suggest a concrete update
when context or scope changes. Code completion and a pushed branch are not
Done: every PR related to the ticket must be manually merged by the user.

Do not commit solely to clear context. Preserve a handoff before resetting
conversational context. Never clear Git
state as a way to clear an agent's history.
