# Ticket template and example

Bracketed text in the template is authoring guidance. Resolve it before calling
a ticket ready; keep any unresolved material decision explicit in Context.
Context, To dos, and Acceptance Criteria are required. Omit Not to dos when
there is no meaningful scope exclusion. Ticket length scales with complexity.

## Template

Title: [Verb + component + intended outcome]

```markdown
## Context

Today, [current behavior and its consequence].
We want [desired behavior] so that [benefit or reason].

Affected scope: [components and environment].
Constraints and dependencies: [compatibility, prerequisites, ownership].
Open decisions: [only if unresolved; otherwise omit].
References: [verified links with a short description; optional].

## To dos

- T1. [Actionable deliverable]. Acceptance: AC1, AC2.
- T2. [Actionable deliverable]. Acceptance: AC3.

## Not to dos

- [Specific adjacent work excluded from this ticket; known follow-up if any].

## Acceptance Criteria

- AC1. Given [condition], when [action], [observable success result].
  Verify using [check and environment].
- AC2. Given [relevant failure condition], [observable failure behavior].
  Verify using [check and environment].
- AC3. [Additional outcome and concrete verification].
```

## Kafka/Milvus example

This is a fictional, complete example for discussing ticket quality, not a
claim about the current repository. Names and dimensions below are example
requirements, not defaults for real infrastructure.

Title: Validate embedding dimensions before the post-embedding consumer writes to Milvus

### Context

Today, the `post-embedding-indexer` consumer reads `post-embeddings-v1` with
consumer group `post-embedding-indexer-v1`. Records contain `post_id` and an
`embedding` vector. It writes to the existing Milvus collection
`post_embeddings_v1`, whose vector field requires exactly 768 dimensions.
An incorrectly sized vector currently reaches the sink and can make a write
batch fail, delaying valid records in that batch.

We want the consumer to reject wrong-sized vectors before the Milvus sink so
that valid records in the same batch can continue and invalid records remain
inspectable through the existing durable DLQ.

The existing framework can classify permanent validation errors and persist
them to a DLQ. It acknowledges handled invalid records only after durable DLQ
delivery and retries on DLQ delivery failure. Reuse that behavior; do not add
another acknowledgement or retry mechanism. Confirm it with the integration
check below. Existing transient Milvus error handling remains in use.

This ticket delivers code, tests, and an isolated integration verification.
Production deployment is separate. The work uses the existing consumer,
collection schema, DLQ configuration, and test harness; no new infrastructure
or schema migration is required. All test data is synthetic.

### To dos

- T1. Validate vector length against the configured 768-dimension requirement
  before records reach the Milvus sink. Acceptance: AC1, AC2.
- T2. Route wrong-sized vectors through the existing permanent-error/DLQ path
  while allowing valid records in mixed batches to proceed. Acceptance: AC2, AC3.
- T3. Add focused regression coverage and verify acknowledgement behavior with
  the existing isolated Kafka/Milvus/DLQ integration harness. Acceptance: AC1–AC5.

### Not to dos

- Do not change the embedding model, collection schema, topic, or consumer group.
- Do not replay historical data, reset offsets, or backfill existing records.
- Do not create a new retry framework, DLQ service, or observability dashboard.
- Do not deploy to production. A rollout follow-up has not yet been filed.

### Acceptance Criteria

- AC1. Given a record with exactly 768 finite vector elements and a valid
  `post_id`, validation permits the existing sink call with the same ID and
  vector. A focused unit test verifies the input passed to the sink.
- AC2. Given vectors of length 0, 767, or 769, no Milvus write is attempted for
  those records. Each is classified as a permanent validation error with an
  actionable reason including expected and actual dimensions. A table test
  verifies all three cases without requiring a live datastore.
- AC3. Given a batch containing one valid record and one wrong-sized record,
  the isolated integration test observes the valid ID in Milvus and the
  invalid record in the existing DLQ, with no invalid Milvus row. Queries use
  the harness's explicit consistency setting and bounded wait for visibility.
- AC4. Given a wrong-sized record and an unavailable DLQ, the consumer does not
  commit an offset that skips that unhandled record. In the isolated harness,
  restore DLQ availability and restart/resume processing; the record is delivered
  to the DLQ before the committed position advances past it. Duplicate delivery
  during retry is permitted by the existing at-least-once contract.
- AC5. The focused tests and existing transient-sink-failure regression pass.
  Record the exact commands, tested revision, and isolated integration result
  in the PR. If the required integration environment is unavailable, record
  the blocker and leave integration acceptance pending.

## Why this is stronger than "tests pass"

The ticket specifies what succeeds, what fails, which existing guarantees are
being relied on, and where they are demonstrated. It separates a handler test
from broker acknowledgement and datastore visibility checks. In a real ticket,
verify the described framework behavior first; if it does not exist, revise
the scope and estimate instead of silently adding infrastructure.
