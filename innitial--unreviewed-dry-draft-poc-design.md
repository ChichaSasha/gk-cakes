# Cake shop POC — Go backend architecture and decisions

Revision 5 · 21 September 2026 · Supersedes the previous Python/Django backend proposal

**Confirmed scope:** Go backend; ready-made cakes with finite stock; guest checkout; full online payment in a sandbox; pickup and delivery; staff manually update delivery statuses; delivery fees vary by area; demo users and test data.

**Architecture:** one Go web application, PostgreSQL, object storage, managed staff authentication, and hosted sandbox checkout. A scheduled maintenance command uses the same Go module and business logic. The design below is a researched implementation proposal; application code has not been built or tested in this task.

One shop, one stock location, modest traffic, and one demo currency remain assumptions. Exact country, currency, delivery-area boundaries, fees, and hosting budget are configuration inputs. Exact postal-code mappings are the proposed area-matching method until another definition is supplied.

## 1. Revised decisions

| Area | Choice | Reason and tradeoff |
| --- | --- | --- |
| Application | Go modular monolith; one repository and Go module | Fits the POC without distributed transactions or service-to-service infrastructure. |
| HTTP | Standard-library `net/http` and `http.ServeMux` | Method/path routing is sufficient for this API. Keep handlers small; add a router/framework only for a concrete missing capability. |
| Database | Supabase PostgreSQL | Keeps catalog, stock, orders, payment records, delivery areas, and audit history transactional. |
| Database access | `pgx/v5`, `pgxpool`, and `sqlc` | Explicit PostgreSQL queries with generated typed Go methods; stock and locking behavior remain visible in SQL. |
| Migrations | Versioned SQL migrations using `goose` | Schema changes are reviewed and run as a controlled release step. `sqlc` generates query code; it does not replace migrations. |
| Staff UI | Small custom admin pages served by Go, with HTML templates and minimal JavaScript | Build catalog, inventory, delivery-zone, and order screens explicitly. Go does not supply the prior framework's automatic admin. |
| Staff identity | Supabase Auth plus local `staff_members` authorization | Delegate passwords and token issuance; the Go application decides who may edit the shop. |
| Customer identity | Guest checkout with an opaque order-access token | Customer registration and profiles are unnecessary for the current demo. |
| Payments | Official `stripe-go` SDK and hosted Checkout in a sandbox | Full payment includes the server-calculated delivery fee. Store outcomes and references locally. |
| Files | Supabase Storage via its S3-compatible endpoint | Use AWS SDK for Go v2's S3 client. Keep image metadata in PostgreSQL. R2 remains a later storage-provider option. |
| Maintenance | `cmd/maintenance`, invoked by a scheduler | Reconcile payments, retry durable webhook receipts, and release confirmed expired holds without a queue service. |
| Logs | Standard-library `log/slog` JSON output | Structured diagnostics with request IDs and redaction. |
| Hosting | Render web service plus a scheduled job; Supabase for persistence | Low-cost demo defaults, with free-tier cold starts and limits acknowledged. |

This is a project-specific selection rather than a universal Go stack. The standard router supports method patterns and wildcards; sqlc supports pgx and transaction-bound query objects. Sources: [Go routing](https://go.dev/doc/go1.22#enhanced_routing_patterns), [sqlc with pgx](https://docs.sqlc.dev/en/latest/guides/using-go-and-pgx.html), [sqlc transactions](https://docs.sqlc.dev/en/latest/howto/transactions.html), [goose](https://github.com/pressly/goose).

## 2. Project structure and Go conventions

Use the latest supported stable Go release and security patch at implementation time; pin the toolchain, dependency versions, and code-generation tools. Commit `go.mod` and `go.sum`. Do not copy an old example's toolchain version simply because its routing API is sufficient.

| Proposed path | Responsibility |
| --- | --- |
| `cmd/server/main.go` | Load configuration, construct dependencies, start HTTP, coordinate shutdown |
| `cmd/maintenance/main.go` | Run bounded reconciliation/retry/expiry work, then exit |
| `internal/shop/` | Catalog, checkout, stock, delivery pricing, and order-transition rules; separate files initially |
| `internal/httpapi/` | Routing, authentication middleware, request/response types, error mapping |
| `internal/httpapi/templates/`, `internal/httpapi/assets/` | Storefront/admin HTML and assets, embedded in the server build |
| `internal/store/` | Generated sqlc query code; never edit generated files manually |
| `internal/auth/` | Token verification and authenticated staff identity |
| `internal/payments/` | Stripe adapter and provider-event translation |
| `internal/filestore/` | Object-storage adapter |
| `db/migrations/`, `db/queries/` | Versioned schema changes and named SQL queries |
| `sqlc.yaml` | PostgreSQL input, pgx/v5 output, and generated-code configuration |

The `cmd`/`internal` structure follows Go's server-layout guidance. The exact packages are our design choice. Start with a compact `shop` package; split it when ownership or size warrants it, rather than creating interfaces and layers for every table. [Go module layout](https://go.dev/doc/modules/layout)

Construct concrete dependencies in `main` and pass them through constructors. Introduce small interfaces at the consumer when there is a useful boundary, such as a payment provider or clock. Avoid global mutable clients, a generic CRUD repository, and a dependency-injection container. Keep HTTP types out of business operations. Return errors with useful context, preserve causes with `%w`, and inspect with `errors.Is`/`errors.As`. Log failures once at the appropriate boundary. [Go code review guidance](https://go.dev/wiki/CodeReviewComments)

Pass `context.Context` as the first argument to operations that perform I/O. Propagate request cancellation to SQL and provider requests; always call cancellation functions. A scheduled run gets its own bounded context. Background work must not inherit an already-finished HTTP request or run indefinitely with no owner. [Go context](https://pkg.go.dev/context)

## 3. Data model and ownership

Use UUID keys, explicit foreign keys, `timestamptz` for instants, and a shop IANA timezone for local display. Monetary amounts are PostgreSQL `bigint` and Go `int64` minor units, accompanied by currency. Validate quantity/amount limits and arithmetic; do not use floating point for checkout totals. Currency determines the minor-unit exponent.

| Table | Main fields and constraints |
| --- | --- |
| `cakes` | `id`, unique `slug`, name, description, ingredients, validated allergen codes, active flag. Archive referenced products. |
| `cake_variants` | `id`, `cake_id`, unique SKU, size/servings, price, currency, active flag. Every stocked purchase configuration has a variant. |
| `cake_images` | Cake ID, bucket, unique object key, MIME type, byte size, display order, alt text, upload state. File bytes live in object storage. |
| `stock_levels` | Variant primary key, `unsold_qty`, `reserved_qty`, timestamp. Require `0 <= reserved_qty <= unsold_qty`. |
| `stock_reservations` | Order, variant, quantity, state (`active`, `consumed`, `released`), expiry. Unique order/variant pair for this POC. |
| `orders` | Unique order number, current status, version, currency, item subtotal, tax, delivery fee, total, contact snapshot, fulfillment method, location/address snapshot, delivery-zone reference/snapshot, instructions, dispatch/completion timestamps, guest token hash. |
| `order_items` | Order, variant reference, purchased name/configuration snapshots, unit price, quantity, line total. Positive quantity; consolidate duplicate variant lines. |
| `checkout_requests` | Opaque caller/cart scope hash, idempotency key, request fingerprint, order reference, state, timestamps. Unique scope/key pair; a changed request cannot silently reuse a key. |
| `payments` | Order, provider/account/environment, unique provider payment/session references, idempotency key, expected amount/currency, local and provider state, timestamps. Full-order collection only; preserve attempts and permit at most one active collection per order. |
| `webhook_receipts` | Provider/account/environment/event identity, type, object reference, optional private payload, state, attempts, next retry, lease owner/expiry, processed time/error. Unique event identity. |
| `order_status_history` | Order, from/to status, actor type/ID, reason, timestamp. Append with the status update. |
| `staff_members` | Internal ID, unique Supabase user subject, role (`manager` or `fulfillment`), active flag. Changes restricted to authorized managers/bootstrap tooling. |
| `audit_events` | Staff actor, action, entity ID, bounded change summary, timestamp; operation key and request fingerprint for retriable mutations, with a unique actor/key pair when present. Covers price, stock, and delivery-configuration edits. |
| `shop_settings` | Singleton currency/timezone, pickup address/instructions, delivery-enabled flag. |
| `delivery_zones` | ID, unique stable code, name, nonnegative fee, currency, active flag, pricing version. Preserve referenced zones. |
| `delivery_zone_postcodes` | Zone, country code, normalized postal code. Unique country/postal-code pair in this single-shop design. |

Keep money, quantities, relations, statuses, and searchable dates typed. JSONB is appropriate for validated historical address/configuration snapshots; it is not a substitute for foreign keys. Client input never owns price, fee, payment outcome, or staff role.

Current order state supports efficient lists; append-only history explains changes. This is ordinary relational storage, not event sourcing. Add indexes for order status/date, order items, payment/order references, history by order/time, active reservation expiry, and pending receipt retry time. Add further indexes based on observed query plans.

Refunds remain optional for the demo. If enabled, add separate refund records and reconcile their outcomes; a cancelled order is not proof of a refund. Keep customers, persistent carts, production slots, quotes, loyalty, and a general analytics store outside the initial implementation. Browser cart contents are only purchase suggestions revalidated by the backend.

## 4. Stock, transactions, and connection management

`available_qty = unsold_qty - reserved_qty`

Here unsold is inventory not yet sold, including active holds. A paid cake waiting for pickup is already sold.

| Event for one cake | Unsold | Reserved | Available |
| --- | ---: | ---: | ---: |
| Initially | 5 | 0 | 5 |
| Checkout reserves one | 5 | 1 | 4 |
| Payment succeeds | 4 | 0 | 4 |
| Alternatively, unpaid checkout expires | 5 | 0 | 5 |

Handover never decrements stock a second time. Refunds do not automatically restock perishable goods. The reservation rows and aggregate stock counters must change in the same transaction.

Use a bounded `pgxpool`, initially with small configurable limits, for example five server connections and two maintenance connections, leaving headroom for migrations and provider tools. These are starting estimates, not measured requirements. Reuse the pool and provider HTTP clients; never open a new pool per request. Pool limits apply across all instances. [pgxpool](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool)

Connect over TLS with server identity verification. Use the direct PostgreSQL endpoint if connectivity permits, otherwise Supabase's session pooler for an IPv4-compatible connection. Choose session mode initially; switching to transaction pooling requires checking pgx prepared-statement behavior against the provider's restrictions. Keep the application database role separate from migration privileges and business tables outside exposed Data API schemas. [Supabase connections](https://supabase.com/docs/guides/database/connecting-to-postgres)

The checkout service owns the transaction. Begin it, bind sqlc queries with `WithTx`, and use that bound query object for every operation in the unit of work. Always roll back on failure and check the commit result. For pgx, context cancellation does not substitute for explicit transaction cleanup; give rollback a bounded cleanup context if the request context is already cancelled. [sqlc transaction binding](https://docs.sqlc.dev/en/latest/howto/transactions.html), [pgxpool transaction behavior](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool#Pool.Begin)

At PostgreSQL's default Read Committed isolation, explicitly lock the relevant configuration/catalog and stock rows and read authoritative values after acquiring their locks. Use a consistent lock order across all operations, with stock rows sorted by variant ID. Reserve the entire cart atomically or roll it back. Conditional SQL updates and check constraints provide additional protection. Staff stock adjustments use the same locks and cannot reduce unsold stock below active reservations.

Do not use an in-process mutex to enforce stock correctness: maintenance and future server instances are separate processes. Do not hold SQL locks during Stripe or storage requests. Retry only identified transient database conflicts with bounded backoff, replaying the complete transaction without external side effects. After an ambiguous commit outcome, consult the durable idempotency record before attempting the operation again.

## 5. Checkout and payment processing

Use the maintained official `stripe-go` SDK and pin its major version and webhook API version together. Wrap it behind the small interface the checkout service needs; use context-aware SDK calls and configure timeouts. [Stripe Go SDK](https://github.com/stripe/stripe-go)

1. Validate cart, contact information, fulfillment method, and delivery eligibility. Compute item prices, tax configuration, and delivery fee on the server. If the shown quote changed, return the revised total for customer review.
2. In one transaction, register the scoped checkout idempotency key/fingerprint, reserve stock, create the awaiting-payment order and immutable item/fulfillment snapshots, and create a local payment record.
3. After commit, create hosted Checkout using a stable provider idempotency key for that logical payment. Persist its references. A timeout is an unknown outcome: retry with the same key or reconcile; do not create a second collection blindly.
4. Align reservation expiry with provider checkout expiry. The browser uses sandbox test cards. Returning to the success page does not mark the order paid.
5. Verify the webhook signature with the official SDK against the bounded raw request body. Persist its unique receipt before returning a successful acknowledgment. Normally try the small business update immediately; an unfinished durable receipt is retried by maintenance.
6. For a verified successful payment, validate account/environment, order association, provider object, amount, and currency. In one transaction, lock the order and affected stock/reservations, record payment success, consume active holds, decrement both unsold and reserved quantities, confirm the order, append history, and complete the receipt.
7. Duplicates and out-of-order events must converge to the same result. Guard business transitions and provider references as well as event IDs; different event IDs can describe the same payment. An old event must not regress successful payment state.

Stripe's delivery guarantees and idempotency retention shape this design. Provider request keys are not a permanent deduplication database. Keep local payment identity durable, and reconcile an old uncertain operation before retrying beyond the provider's key-retention window. [Stripe webhooks](https://docs.stripe.com/webhooks), [idempotent requests](https://docs.stripe.com/api/idempotent_requests)

Keep order and payment statuses separate. Payment states may include created, pending, requires action, succeeded, failed, and cancelled, with the provider state retained. A declined card can be retried within checkout; do not release its stock hold just because one attempt failed. Only successful collections count toward paid totals. Excess or mismatched collections go to staff resolution rather than silently confirming another order.

For expiry, confirm the provider checkout is terminal and unpaid before releasing stock. If the provider is unavailable or payment is processing, retain the hold and retry reconciliation. A late success after release requires atomic stock reacquisition or an explicit refund/manual-resolution path. Start with sandbox card methods to keep delayed bank settlement out of scope.

## 6. Pickup, delivery areas, and staff transitions

Pickup: `awaiting_payment → confirmed → ready → completed`.

Delivery: `awaiting_payment → confirmed → ready → out_for_delivery → completed`.

Unpaid orders can expire or be cancelled. Ready means packed. Completed means collected or delivered. A delivery issue is recorded with its reason and remains unresolved; it does not automatically imply completion, refund, or restocking. Dispatch is valid only for an eligible paid delivery order.

A transition command checks actor permission, order version/current state, fulfillment method, and payment requirements. It updates current state, operational timestamps, and history in one transaction. Stale staff screens receive a conflict response. Expose explicit actions rather than arbitrary updates to status fields.

For delivery, normalize the country and postal code and resolve one active zone on the backend. The unique mapping prevents ambiguous exact matches. No match means delivery is unavailable; offer pickup. Pickup requires a shop-location snapshot, no delivery zone, and zero delivery fee. Delivery requires a recipient/address/phone snapshot, zone reference, zone code/name/pricing-version snapshot, and saved fee in the order currency.

Lock/recheck relevant mapping and pricing configuration during order creation. Fee changes affect new checkouts; already-created orders keep their saved fees. Increment the pricing version and audit fee, mapping, and activation changes. Clients cannot select a cheaper zone by sending its ID. Exact postcode matching is an eligibility rule, not proof that an address exists.

A structured address form and three configurable demo zones are sufficient. No map or courier service is required. Switching to polygons or named districts would require explicit matching and overlap rules. No guaranteed time slots are assumed; if promised delivery/pickup windows become necessary, add capacity reservations for them separately from cake stock.

## 7. Staff UI and authentication

The custom staff UI has five small screens: catalog/images, stock adjustment, order list/detail/timeline, delivery-zone/postcode configuration, and shop settings. Use Go `html/template` with automatic escaping for pages and small JavaScript calls to the API. Treat page shells as public assets; protected staff data is fetched only through authenticated endpoints. The storefront can use the same server or an existing frontend. Do not embed untrusted content as trusted HTML.

Supabase Auth handles staff sign-in and token refresh. Use its browser SDK for the login/session flow. The Go API accepts access tokens in the Authorization header and uses `golang-jwt/jwt/v5` with `keyfunc/v3` for cached, rotating JWKS keys. Configure asymmetric signing in Supabase. Explicitly check the permitted algorithm, issuer, audience, required expiry, not-before when present, signature, and user subject. Fetch keys only from the configured trusted project URL, with bounded refresh/cache behavior; never follow a token-supplied key URL. [Supabase JWT verification](https://supabase.com/docs/guides/auth/jwts), [Go JWT library](https://github.com/golang-jwt/jwt), [JWKS helper](https://github.com/MicahParks/keyfunc)

After authenticating, load active `staff_members` membership. Manager may edit catalog, stock, delivery settings, and orders; fulfillment may view operational order information and perform permitted fulfillment actions. JWT `role=authenticated` and user-editable profile metadata do not establish staff privileges. Provision staff explicitly. Disabling local membership removes staff access even while an access token remains cryptographically valid.

The backend never stores staff passwords or implements token signing. Keep service/storage secrets and database credentials on the server. Browser code uses only the intended public Auth configuration. Use a restrictive content-security policy and avoid tokens in URLs or logs. The default API authenticates bearer tokens, not cookies; if cookie sessions are added, include CSRF protection and secure cookie settings.

Guests receive a high-entropy opaque order-access token with only its hash persisted. Reading an order requires that token; knowing an order number or email is insufficient. Do not expose personal/payment data through public catalog endpoints or the Supabase Data API.

## 8. HTTP and API behavior

Configure an explicit `http.Server` with header/read/write/idle timeouts and bounded headers/bodies; choose upload-specific limits for images. Set outbound client deadlines and reuse transports. Propagate operation deadlines into database and provider calls; transport timeouts alone do not define business-operation cancellation. Shut down gracefully on process signals with a bounded drain period, then close the pool. [Go HTTP server/client](https://pkg.go.dev/net/http)

Decode into dedicated request types and validate allowed fields, lengths, quantities, currency, and enum values. Reject unknown fields on our own mutation APIs; keep webhook parsing compatible with provider schema evolution. Separate API responses from generated database structs. Paginate catalog/admin lists. Map malformed input to 400, validation failures to 422, authorization to 401/403, state/stock/quote conflicts to 409, and unexpected failures to 500 without SQL/provider details. Preserve request IDs for diagnostics.

| Endpoint family | Behavior |
| --- | --- |
| `GET /api/cakes`, `GET /api/cakes/{id}` | Public catalog; current availability is informative, rechecked at checkout |
| `POST /api/checkout/quote` | Server-calculated items, eligibility, and delivery fee; no stock reservation yet |
| `POST /api/checkouts` | Idempotent order/hold creation and hosted checkout |
| `GET /api/orders/{id}` | Guest-token or authorized staff access; reads current state |
| `POST /webhooks/stripe` | Raw-body signature verification and durable receipt |
| `/api/admin/...` | Authenticated catalog, stock, order, and zone operations |
| `POST /api/admin/orders/{id}/transitions` | Explicit action, expected version, and optional reason |
| `/health/live`, `/health/ready` | Process liveness and bounded dependency readiness; no sensitive details |

Use parameterized SQL; allowlist dynamic sort keys. Limit abusive checkout creation and staff login attempts. Keep cross-origin access restricted if a separate frontend is used. Choose idempotency keys for retriable mutations; stock adjustments need an operation identity to prevent a double-click/retry from incrementing inventory twice.

## 9. Files, jobs, and observability

Use AWS SDK for Go v2's S3 client with the configured Supabase endpoint, region, credentials, and addressing settings. Keep this adapter small so R2 can be substituted without altering order/catalog rules. Public catalog images use a public-read bucket with staff-only writes. Store object keys, never expiring signed URLs, in PostgreSQL. [Supabase S3 authentication](https://supabase.com/docs/guides/storage/s3/authentication), [Go S3 endpoint configuration](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-endpoints.html)

Staff uploads pass through authenticated backend operations with size/type/decoded-image checks. Use unique keys, create display sizes once, and track pending/ready metadata. Object storage and PostgreSQL do not share a transaction: cleanup/retries handle abandoned uploads. Keep private customer attachments in a separate bucket if introduced later.

`maintenance reconcile` claims pending receipts with row locks and persisted leases, fetches provider state outside long SQL transactions, and calls the same idempotent finalization operations as the webhook handler. Both entry points use the same claim mechanism and verify current business state at commit. Bound batch size, external-call concurrency, attempts, and total run time. Expired leases can be recovered; repeated failures become visible to staff. Reconcile pending payments even when no webhook receipt arrived. Expiry and cleanup operations must also be idempotent.

Every goroutine needs an owner, cancellation, and an exit path. A goroutine launched after checkout is not a durable job. Store required work in PostgreSQL before acknowledging it. Introduce an outbox/jobs table if receipt email or other asynchronous effects are added; the current webhook inbox covers payment processing. [Go concurrency guidance](https://go.dev/wiki/CodeReviewComments#goroutine-lifetimes)

Emit `slog` JSON with request ID, operation, latency, outcome, and safe error classification. Exclude tokens, card data, full addresses, and raw payment payloads. Business audit/history remains in PostgreSQL independently of log retention. [Structured logging](https://pkg.go.dev/log/slog)

Start with hosting metrics and SQL demo dashboards. Useful signals: request latency/errors, pool saturation, payment failures, pending receipt count, oldest receipt age, and overdue reconciliation. Use bounded labels such as route template and result; order/customer IDs belong in access-controlled logs, not metric dimensions. Add a monitoring service only if needed. Compute simulated sales/refunds from authoritative payment records, not telemetry counters.

## 10. Verification and delivery workflow

Use table-driven tests for pricing, postal normalization/matching, permissions, state transitions, and integer-money validation. Use `httptest` for handlers and controlled provider failures. Test SQL behavior against real PostgreSQL with production migrations; mocks or SQLite cannot establish that PostgreSQL locking prevents overselling.

Essential integration scenarios: two concurrent carts for the last cake; rollback of a partially available multi-item cart; repeated checkout creation; duplicate/different/out-of-order events for one payment; provider timeout after creating checkout; worker crash and lease recovery; unpaid expiry; late payment after release; stale zone quote; altered client fee; inactive staff; guest access to another order; and image-upload interruption.

CI should format/check Go, run `go vet ./...`, `go test ./...`, and `go test -race ./...` in a supported runner, run `govulncheck ./...`, regenerate sqlc code and reject unexplained diffs, and build both commands. The race detector catches in-process memory races, not database overselling; both forms of verification are required. [Race detector](https://go.dev/doc/articles/race_detector), [Go security tooling](https://go.dev/doc/security/)

Run SQL migrations once as a controlled release action, not independently on every replica startup. Test them on an empty database and the prior schema. Deploy the web service and maintenance command from the same revision and use compatible additive schema changes. Keep seed/reset tooling explicit and restricted to the demo environment.

## 11. Hosting and cost

Build `server` and `maintenance` binaries from the same module. A multi-stage container provides a reproducible deployment: pin build/runtime images, run as a non-root user, include TLS root certificates, and embed required templates/assets. Inject credentials at runtime. Stateless binaries need no writable persistent disk. Render supports Go runtimes and Docker deployment. [Render deployment options](https://render.com/docs/docker)

A free Render web service is acceptable for this disposable demo if its idle sleep and roughly minute-long wake-up are acceptable. Keep all business data and uploaded files in Supabase; local filesystem changes are ephemeral. A faster Go startup does not remove the hosting platform's wake-up delay. [Render Free](https://render.com/docs/free)

Run one scheduled job, initially every five minutes, with the command `/app/maintenance reconcile` from the same built image. It connects to PostgreSQL and Stripe directly, so it works independently of a sleeping web process. Render Cron Jobs are billed for execution, with a $1/month minimum per service. That is a billing floor, not a fixed total. [Render Cron Jobs](https://render.com/docs/cronjobs)

Supabase Free currently includes a 500 MB database and 1 GB storage, subject to bandwidth and other quotas; inactive projects can pause and automatic database backups are not included. These allowances can fit a small demo. Provider facts checked on 21 September 2026. [Supabase pricing](https://supabase.com/pricing)

The cost target is free web/database/storage allowances plus scheduled execution from $1/month, with usage and optional email/domain costs separate. Strictly zero-cost guided demonstrations can run maintenance from a developer machine, accepting that unattended cleanup stops when that machine is off. Go is selected for the backend; this proposal makes no benchmark-based claim of a lower bill.

Keep migrations, seed definitions, and original demo images reproducible. Before reset, stop new checkout creation and reconcile/expire sandbox sessions. Reseed with new IDs; late events for removed orders must be handled safely. No deployment, provider account setup, reset, or real payment has been performed by this design revision.

## 12. Implementation order and remaining inputs

1. Bootstrap the Go module, configuration, HTTP server, SQL migrations, generated queries, and staff authentication.
2. Build catalog/image and stock screens; add delivery zones/postcode mapping.
3. Implement quote, transactional reservations, hosted sandbox checkout, webhook receipt/finalization, and maintenance reconciliation together.
4. Add the order timeline and validated pickup/delivery actions.
5. Exercise the failure/concurrency scenarios, seed the demo, and deploy the web and scheduled commands.

The remaining delivery configuration is the country/currency and area-to-postcode-to-fee mapping. Postal-code matching remains an assumption; named districts or geographic boundaries require a different matcher. Hosting budget and any existing frontend can refine deployment without changing the core data model. Real orders would require a separate review of recovery, availability, perishable-stock expiry, refund operations, and live payment configuration.
