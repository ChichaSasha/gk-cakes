# GK-006 — approved release and access policy

Decision interview: 2026-10-05, revision `interview-r1`.
Source: Sasha's explicit answers in the current Codex conversation, preserved with
question context and answer fingerprints in [interview evidence](interview-2026-10-05.json).
Live ticket: [GK-006](https://trello.com/c/npWFafNi).
This is a decision handover, not a second execution-status tracker.

Publication verified 2026-10-05: Sasha approved the final handover and closure.
The exact summary was published and GK-006 was reread in Done; see the
[publication receipt](closure-publication-2026-10-05.json). GitHub capability
verification remains a mandatory GK-013 gate. The interview JSON preserves the
pre-publication evidence state; this receipt records the later authorization.

## Approved policy

1. Production changes only through an explicitly authorized published release.
   A merge or successful CI run alone never deploys production.
2. Maintain a small staging environment with the UI and backend, test data,
   sandbox payments, and credentials/data separate from production. Automatically
   deploy each merged revision after CI passes on that exact main revision.
   Test there before release; production receives the tested version. Subsequent
   changes require staging verification again.
3. Sasha is the sole production-release approver. The agent may prepare a release
   and publish the specific version after Sasha explicitly authorizes it; Sasha
   may also publish it directly. This interview authorizes no actual release.
4. Required checks plus a repeating independent specialist AI review → feedback →
   fixes loop precede native auto-merge. Reviewers use separate contexts and
   expertise appropriate to the affected areas. Rerun affected checks and reviews
   after fixes; unresolved blocking findings or failed required checks prevent
   merge. Bring decisions requiring Sasha's input back to Sasha. A separate
   routine human approval on every PR was not selected. This policy is not yet
   implemented or proven enforceable on this repository.
5. Use `ui-vX.Y.Z` for the customer/admin interfaces and `api-vX.Y.Z` for backend
   and background jobs. Components can release independently when compatible
   with the deployed counterpart. Sasha may authorize both together.
6. Sasha owns/administers repository and deployment access, controls grants and
   revocation, and is the only routine human administrator initially. Separate
   UI/API deployment permissions and staging/production credentials. PR checks
   and routine agent work have no production secrets. Authorized production jobs
   obtain scoped credentials from protected deployment stores. Database, payment,
   email and other private credentials stay out of browser code.

The existing GK-004 business-owner billing/account-identity and emergency-recovery
arrangements remain in place; no revocation or ownership transfer was requested.
The small staging environment uses the already accepted test-runtime allocation;
this interview does not approve extra spend, accounts, or infrastructure changes.

## Acceptance evidence and limitations

| Criterion | Evidence / remaining work |
| --- | --- |
| AC1 | Explicit interview approval of production release-only flow, sole approver/delegated publisher, and component naming. |
| AC2 | Approved review/check/native-auto-merge policy. Named limitation: read-only `gh repo view` against github.com returned HTTP 401, including a retry without inherited GH_TOKEN/GITHUB_TOKEN. Repository visibility, plan, permissions, protection and native-auto-merge support remain unverified. No bypass is permitted. |
| AC3 | Explicit approval of Sasha's administration and separately scoped environments/credentials; merging and production authorization remain distinct. |
| AC4 | Current source snapshots and fingerprints, exact interview answers, output manifest and same-session document self-check. Sasha approved publication and closure; the comment and Done list were verified by rereading Trello (publication receipt above). No independent review or application tests are claimed. |

This packet records the named AC2 limitation, rather than declaring platform
support. GK-013 must verify real repository access/plan, enforceable review/check
gates and native auto-merge with GK-020 B1 check names before GK-020 B2 proceeds.
An AI persona's review text alone does not prove GitHub gate enforcement.

## Downstream handover

- GK-019: consume this approved input and reconcile shared contracts and dependent
  packets, including the automatic staging path. Historical ticket snapshots remain
  historical; do not infer changed acceptance from their old proposals.
- GK-013: resolve the repository-access limitation and validate enforcement, stale
  or missing checks/review evidence, native auto-merge, and scoped permissions.
- GK-020 / GK-034: implement exact-revision CI, independent review evidence,
  automatic staging after green main CI, separately authorized production releases,
  component compatibility and secret isolation. Verify in nonproduction first.
- GK-004 / hosting setup: retain the approved test budget and existing account
  recovery arrangements; size staging during setup within that budget.

Live card and board instructions were reread on 2026-10-05; the card had no comments.
The exact [Trello summary](trello-summary.txt) was published as comment
`6ac3b1e8ae646f35812f54f0`; GK-006 was moved to Done after explicit approval.
The original card description was preserved. No repository settings, service
configuration, deployments or releases were changed.
