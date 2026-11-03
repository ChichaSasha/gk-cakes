---
name: work-review
description: Review an engineering diff against its original acceptance criteria, repository conventions, tests, and failure modes. Use for requested code or PR review and independent quality gates; report findings without editing code unless fixes are also requested.
---

# Work review

For work tasks, read `~/.agents/work/AGENTS.md` if not already loaded.

Prefer a fresh execution context or a human reviewer for independent review.
The same model can be used in a new context; a different provider is not
required. If the implementation conversation is present, disclose that this
is a self-review, not an independent assessment.

## Required inputs

Read the original requirement/ticket, base and head or precise diff scope,
applicable repository instructions, and relevant reference documents. Inspect
the actual code and its callers, not just the author's explanation. Read the
reported test evidence, distinguishing what was run from what was proposed.

If the scope is ambiguous, establish it before reviewing. Preserve working
tree changes. A review request authorizes inspection and relevant permitted
checks, not edits, production operations, or posting external comments.

## Review the changed behavior

- Check each acceptance criterion and look for missing behavior, scope creep,
  and compatibility changes. Do not add your own feature requirements.
- Trace trust boundaries, error paths, cancellation, resource cleanup, and
  concurrency ownership affected by the change.
- For Kafka changes, inspect acknowledgement/commit timing, partial failure,
  replay, ordering assumptions, poison-record handling, and retry bounds where
  relevant. A shared consumer group does not itself guarantee exactly-once
  effects at the destination.
- For Milvus changes, inspect schema/vector compatibility, primary-key and
  write semantics, result/error handling, visibility assumptions, and retry or
  cutover behavior where relevant. Verify against the deployed/client version
  and repository wrappers; do not assume another version's guarantees.
- Check generated source/output consistency and the actual deployment scope
  if infrastructure changed. Identify required rollout evidence that is absent.
- Assess whether tests would detect the plausible regression, including the
  important failure path. Run focused checks when available; report limitations.
- Flag unnecessary complexity when it has a concrete maintenance or correctness
  cost. Prefer an existing simpler equivalent; avoid taste-only rewrite requests.

## Findings and verdict

Report actionable findings first, ordered by impact. Each finding needs a
precise location, triggering condition, observable consequence, and a practical
fix direction. Label uncertainty and distinguish blockers from optional advice.
Use the repository's severity convention if one exists.

Summarize acceptance coverage and verification limitations. If there are no
actionable findings, say so, while still identifying any material unverified
behavior. Do not invent findings to make the review look thorough or declare
correctness solely because tests pass.

The user merges manually; do not merge or enable auto-merge. A review verdict
does not authorize deployment or Jira changes. After fixes, review
the new diff and require the affected checks again; do not reuse a verdict for
a revision you did not inspect.
