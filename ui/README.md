# GK Cakes UI

SvelteKit customer/admin UI, built as one Node artifact (`@sveltejs/adapter-node`)
and deployed separately from the API. It holds no backend secrets: database,
payment, email, webhook and storage-write credentials stay in the API.

## Toolchain

- Node `26.13.0` (LTS "Lithium"), pinned in `.nvmrc`; `engines` plus
  `engine-strict` reject other majors.
- npm `11.20.0` (bundled with that Node), lockfile `package-lock.json`.

## Commands

Run from `ui/`:

| Purpose                        | Command                       |
| ------------------------------ | ----------------------------- |
| Install exactly from lockfile  | `npm ci`                      |
| Format and lint                | `npm run lint`                |
| Type check                     | `npm run check`               |
| Unit tests                     | `npm test`                    |
| Backend secrets not in bundles | `npm run test:bundle-secrets` |
| Production build               | `npm run build`               |
| Start the built server         | `node build`                  |

`test:bundle-secrets` builds with a random canary in every backend-only variable
and fails if any canary appears in `build/`. Run it before `npm run build`, since
both write `build/`.

## Runtime

`node build` serves on `PORT` (default 3000). Set adapter-node's `ORIGIN` to the
public site origin. Liveness: `GET /healthz`.

Environment variables must be declared in `src/env.ts` (SvelteKit 3) before code
can import them; only deliberately public values may be declared `public`.

| Variable                      | Rule                                                                                   |
| ----------------------------- | -------------------------------------------------------------------------------------- |
| `PUBLIC_API_URL`              | exact API origin, e.g. `https://staging-api.gkcake.com` (https; http only on loopback) |
| `PUBLIC_AUTH_URL`             | identity provider project URL, e.g. `https://<project>.supabase.co`                    |
| `PUBLIC_AUTH_PUBLISHABLE_KEY` | the provider's publishable key (`sb_publishable_...`); a secret key is refused         |

All three are public by design and read at start (one build serves every
environment). They are optional: without them the server still starts and
sign-in says it is not configured.

## Sign-in (GK-021)

`/sign-in` (email and password, or an emailed one-time link), `/auth/callback`,
`/account` and `/admin` (staff). These pages render only a shell on the server
(`ssr = false`); the browser loads data from the API with the signed-in user's
access token as `Authorization: Bearer` (never cookies or URLs).

- The provider SDK (`@supabase/supabase-js`, PKCE flow) keeps the session in
  memory and persists it, access and refresh tokens included, in this origin's
  `localStorage` under `gk-auth` (the provider-supported store; a GK-021
  decision for Creator review, protected by the Content Security Policy).
- Email sign-in links return a one-time code to the exact, allowlisted
  `/auth/callback`, exchanged with the verifier only the requesting browser
  holds. Invitation emails link to `/auth/callback?token_hash=...&type=invite`
  (verified once). The callback removes the code or token hash from the URL and
  history before it calls the provider; same-origin page scripts load first,
  with no referrer.
- When the app starts, `src/hooks.client.ts` removes sign-in secrets from any
  other URL (a provider code, token hash or error, and any implicit-flow
  `#access_token`/`#error` fragment), wherever the provider sent the browser.
- After an email link, the user returns to `/account` or `/admin` only; the
  choice is kept in `localStorage` for 30 minutes (links usually open in a new
  tab), never taken from the URL.
- Sign-out ends the provider session when the provider is reachable (the API
  then refuses that session at once on staff routes) and always clears the
  stored session; the page says so if the provider could not be reached.
  Signing out in one tab sends the others' private pages to sign-in. A 401
  from the API signs out and shows "Your session has ended".
- Messages never reveal whether an email has an account, including when
  sending a link is rate-limited; an unreachable provider is reported.
- Response headers (`src/hooks.server.ts`, checked in CI by
  `scripts/check-headers.sh`): Content Security Policy with
  `script-src 'self' 'nonce-…'`, `style-src 'self'`, `style-src-attr
'unsafe-inline'` (inline style attributes only, for SvelteKit's route
  announcer), `object-src 'none'`, `frame-ancestors 'none'`, `base-uri` and
  `form-action 'self'`, and `connect-src 'self'` plus `PUBLIC_API_URL` and
  `PUBLIC_AUTH_URL`; `nosniff`; `Cross-Origin-Opener-Policy: same-origin`.
  Private routes (decided by the matched route, not the raw path: the
  sign-in pages, `/account`, `/admin`, `/checkout` and `/orders/[id]`) add
  `no-store`, `no-referrer` and `X-Robots-Tag: noindex, nofollow`.
- The bundle secret check needs no new variables: the `PUBLIC_*` values are
  public and read at start.
- Styling of the sign-in and account pages is functional only until the
  account designs (GK-016) are approved.

No browser test runner is configured yet; one is added when a runner is approved
for the interactive screens. CI starts the built server and checks `/healthz`
and the headers instead. GK-030 was checked locally by driving the built server
in headless Chrome against a stub API (not committed; see the checkout section).

## Checkout and order status (GK-030)

Follows the approved checkout sketch GK-017 r01 and the GK-015 r12 visual
foundation (Geist and Geist Mono in `static/fonts`, SIL Open Font License; styles
in `src/lib/shop.css`, scoped to these pages). Both pages render only a shell on
the server (`ssr = false`) and call the API from the browser; the API client is
`src/lib/checkout.ts`, formatting is `src/lib/format.ts`.

| Route          | What it does                                                                                                         |
| -------------- | -------------------------------------------------------------------------------------------------------------------- |
| `/checkout`    | When and how, your details, discount code, before you pay, payment; places the order and starts payment              |
| `/orders/[id]` | The order's authoritative status, polled until settled; opens Stripe Checkout; Stripe returns to `?payment=returned` |

**Temporary basket entry.** Until the discovery pages (GK-016) are approved,
the basket comes from the URL: `/checkout?item=<variant_id>&item=...&lot=<lot_id>`
(a repeated ID adds one more of that cake; at most 20 cakes). Cake details,
allergens and the kitchen notice come from the public catalog (`/api/v1/cakes`,
paged until the variants are found; `/api/v1/ready-cakes` for lots). A cake
without a current allergen declaration, or a ready cake made to a recipe
other than the public one, blocks checkout (K01).

**Flow.** Dates come from `/api/v1/availability` (31 days at a time, London
calendar), times from `/api/v1/schedule` for the date and handover mode; a
chosen time is kept when it is still offered. Every change of date, time, mode
or code asks the server for a quote (`POST /api/v1/checkout/quotes`); the page
shows only the server's totals. The refund deadline before checkout is the
appointment start minus 120 elapsed hours in London time (the order page shows
the order's own `full_refund_deadline_at`); the tick-box consents to the
current policy version (`/api/v1/policies/current`), whose summary is shown.
"Continue to secure payment" refreshes the quote when it is missing, stale or
within a minute of expiry (a changed total stops for review), posts
`/api/v1/checkouts` (202: payment pending, never accepted), reads the order for
its version, and starts the payment (`/payment`) or, for £0, the free
confirmation, then goes to the order page. That page opens the Stripe URL
(`https://checkout.stripe.com` only) once it appears, but only straight after
checkout or after the customer presses Continue there; a return from Stripe
just shows the status. Refusals map to the sketch's messages: `PRICE_CHANGED`
and `QUOTE_EXPIRED` show a fresh quote for review, `SLOT_UNAVAILABLE` and
`LEAD_TIME` reload the times, `CODE_USED_UP` asks to remove the code,
`POLICY_CHANGED` reloads the terms and asks to tick again,
`PAYMENT_IN_PROGRESS` says not to pay again, and 503/network errors say to
try again later.

**Idempotency.** Each user action (quote, checkout, payment, free
confirmation) has its own `Idempotency-Key`, reused when the same action is
retried with the same body (a lost answer replays) and new when the body
changes.

**Guest capability storage (decision for Creator review).** A guest's quote
proof (32 random bytes, fresh for every new quote) becomes the order's
capability. After checkout it is kept in this origin's `localStorage` under
`gk-order-capability:<order_id>` for 30 days (expired entries are removed on
each save) and sent only as `X-Order-Capability`, never in a URL. `localStorage`
rather than `sessionStorage` so the order page still works after the Stripe
return in a new tab or a closed tab; the trade-off is that anyone using the
same browser profile can open the order. It sits behind the same Content
Security Policy as the sign-in session. Signed-in customers use their bearer
token; a guest order placed before signing in falls back to the stored
capability. A one-time hand-off (Stripe redirect allowed, message, poll hint)
goes from checkout to the order page in `sessionStorage`.

**Order page states.** Waiting for payment (Continue to secure payment, or
start one when none is in progress), payment pending while the page is being
prepared, "Still checking payment; do not pay again" (unknown, or pending
without an open page), payment received with confirmation being secured,
confirmed (date, time, items, totals, refund deadline), payment received with
the chef to contact the customer about the date (`paid_awaiting_agreement`),
cancelled with its reason and a link to start a new order with the same cakes,
and a refund block when the API returns one (requested and returned so far;
never "refunded" while pending). Polling starts at the API's
`poll_after_seconds` and backs off to 30 seconds; it pauses in hidden tabs and
stops on settled states.

**Not built yet.** GK delivery (shown as not available online), resale lots
and shorter lead times (not quotable), the full policies page and a contact
link, cancelling or moving an order, the account order list, prefilled details
for signed-in customers, a browser test runner, and the problem states the
sketch deferred to r02 beyond the API refusals above. The 50% amount is not
shown before payment (the client never computes amounts); the policy summary
carries the rule. A lots-only basket still asks availability for one place.

## Dependency audit

`npm audit` reports low/moderate advisories in development tooling only
(eslint/minimatch, and deepmerge used by the Svelte Vite plugin at build time).
None of these packages are present in `build/`, and npm's suggested fixes are
major downgrades. CI therefore does not run `npm audit`; revisit when upstream
releases fixes.
