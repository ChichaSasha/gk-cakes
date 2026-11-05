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
  Private routes (decided by the matched route, not the raw path) add
  `no-store`, `no-referrer` and `X-Robots-Tag: noindex, nofollow`.
- The bundle secret check needs no new variables: the `PUBLIC_*` values are
  public and read at start.
- Styling is functional only until the account designs (GK-016) are approved.
- Site icon: `static/favicon.svg` (a cake in the GK-015 ink, white and accent;
  no script or external reference) and the same shapes as a 32×32
  `static/favicon.ico`, both linked from `src/app.html`, so `/favicon.ico` is
  served (CSP `img-src 'self' data:`). It is a placeholder, not the canonical logo
  (still open in GK-015).

## Checkout and orders (GK-025)

`/checkout` and `/orders/[id]` follow the Creator-approved GK-017 r01 checkout
sketch on the GK-015 r12 visual foundation (styles in
`src/lib/checkout/checkout.css`, scoped to these pages). Both render a shell on
the server (`ssr = false`), are private routes (no-store, no-referrer,
noindex) and talk only to `PUBLIC_API_URL`; payment is a top-level navigation
to Stripe's hosted page (`https://checkout.stripe.com` only, checked before
redirecting), with no Stripe script on this site and no analytics.

**Basket (temporary, unlinked).** Until the discovery pages and basket
(GK-016) exist, `/checkout` is not linked from navigation and reads the basket
from its URL: `?v=<variantId>` for a made-to-order cake and `?lot=<lotId>` for
a ready cake, each repeated once per cake (at most 20), plus an optional
`?date=YYYY-MM-DD`. For example
`/checkout?v=<variantId>&v=<variantId>&lot=<lotId>&date=2026-12-22`. Only IDs
are read: names and allergens come from the public catalog (variants are found
by scanning `GET /api/v1/cakes` pages), prices from the server's quote. A
ready cake is offered only when it was made to the variant's current recipe
version (the only one whose allergens the catalog shows), and every quote must
price the recipe versions shown; otherwise the catalog is reloaded and the
confirmation tick-box cleared. Ready-only baskets still ask availability and
times for one place (the APIs need a quantity of at least 1).

**Checkout** has the five r01 sections and the order summary:

1. When and how: open dates from `GET /api/v1/availability` (31-day windows,
   "Show later dates"), collection or own courier, times from
   `GET /api/v1/schedule` (the chosen time is kept when still available). GK
   delivery is shown disabled ("not available online yet"): it needs the chef's
   quote flow, not built yet.
2. Your details: full name, email, optional "someone else is paying".
3. Discount code: applied by requesting a new quote with the code (one per
   order; the server decides; it survives a date or time change or is dropped
   with a message).
4. Before you pay: allergens per cake (prominent ones called out) and the
   kitchen notice from the catalog, the refund terms for the chosen time
   ("Cancel before [time] (London time) on [date]" = appointment start minus
   120 elapsed hours, so the clock time moves across a clock change), the
   statutory-rights notice, the current policy summary from
   `GET /api/v1/policies/current`, and the confirmation tick-box.
5. Payment: six method labels and "Show all 17".

The summary shows only the quote's totals (`subtotal_minor`, `discount_minor`,
`total_minor`); nothing adds up money in the browser. Only a quote for exactly
the date, time, handover option and code on screen is shown or submitted:
changing any of them drops the total until the new quote arrives, Continue
waits while dates, times or the total load, and an out-of-date quote is
refreshed with the tick-box cleared. A discount code needs a time first (it is
checked by a quote). Failed date or quote requests offer "Try again". Choosing a time creates
a quote (`POST /api/v1/checkout/quotes`); Continue validates the form (inline
errors and an error summary), then `POST /api/v1/checkouts` with the consented
policy, then `POST /api/v1/orders/{id}/payment` (or
`/free-confirmation` for a £0 order), waits for `payment.checkout_url` and
redirects to it; otherwise it opens the order page. Refusals are shown in
plain words (`src/lib/checkout/messages.ts`): a changed price, an expired
quote or changed terms refresh the total or terms and ask for review again; a
taken time or a missed lead time asks for another time or date; a used-up
code is removed; a payment already in progress says "don't pay again"; an
unavailable service can be retried; a reused key (an order already started
with other details) links that order. Reviewing the new total and continuing
then places a second order: the first one stays unpaid and keeps its place
(day place, ready cake, code use) until the hold review releases it 15
minutes after it was placed (API step 7, cancelled as
`payment_not_completed`). A problem after the order exists (for
example a lead time missed while paying) is handed to the order page, which
asks for a new order when the price window or lead time has passed: while
that problem (`LEAD_TIME`, `QUOTE_EXPIRED`, handed over or answered to the
page's own payment or confirmation) is shown, the page offers "Start a new
order" (the same cakes, without the date) instead of starting a payment or
"Confirm order"; a payment already under way (an open payment page, or "do not
pay again") keeps its own wording and action. Back from
Stripe via the back button resets a busy Continue.

**Keys and guest secrets.** Every command has an `Idempotency-Key` per user
action, reused when the same action is retried. A guest gets a fresh guest
proof (32 random bytes, unpadded base64url) for each new quote; the proof of
the quote that became the order is kept as that order's capability in this
tab's `sessionStorage` (`gk-order-capability:<orderId>`; never in a URL, log or
analytics) and sent as `X-Quote-Proof` / `X-Order-Capability`. Signed-in
customers use their access token instead. A guest cannot check out when this
tab cannot keep data, and no payment starts if the capability could not be
saved. Closing the tab ends a guest's access to the order page (order emails
come later).

**Order page** reads `GET /api/v1/orders/{id}` and shows the lifecycle in
customer words (`src/lib/checkout/order.ts`): waiting for payment with
"Continue to secure payment"; after returning from Stripe
(`?payment=returned`) "Still checking payment; do not pay again" (the return
never claims success); "Payment received — confirming your order"; the
confirmation with date, time, items, totals and refund deadline; "Payment
received" with the chef to contact the customer about the date
(`paid_awaiting_agreement`: date and time shown as requested, not confirmed,
and "If the date can't be agreed, you get everything you paid back");
cancellation reasons in plain words (a cancelled order whose payment is still
being checked reads "Still checking payment; do not pay again", never "not
completed" or "start a new order"); refused free confirmations worded by
reason; and the refund (`refund.state` requested, pending, partly_returned,
returned or needs_attention, with `target_minor` and `succeeded_minor`) only
as far as the API says. The cancellation and refund terms appear only for
accepted, paid orders.
It polls while the state can change (from 3 s, backing off to 30 s, stopping
after 5 minutes with a refresh button).

**Not built here:** the discovery pages and real basket (GK-016), the GK
delivery request and quote flow, and the GK-017 r02 problem-state designs (not
approved): problem states are shown plainly with the messages above.
Confirmation emails are not sent yet, so the pages do not promise one.

No browser test runner is configured yet; one is added when a runner is approved
for the first interactive screens. Unit tests cover the checkout logic, and
presentational components are rendered on the server in tests
(`svelte/server`). CI starts the built server and checks `/healthz` and the
headers instead.

## Discovery pages (GK-028)

`/` (home), `/shop`, `/cakes/[id]`, `/sizes` and `/bag` follow the
Creator-approved GK-016 r01c sketch (PR #21, `52fa680`) on the GK-015 r12
visual foundation (styles in `src/lib/shop/shop.css`, scoped to `.shop`; the
`(shop)` route group and the home page share `ShopShell`). They are public
pages (cacheable, indexable; `/bag` has `noindex`) and read only the public
catalogue (contracts K01): `GET /api/v1/cakes` (every page for the shop, the
first four for the home page), `/cakes/{id}`,
`/ready-cakes`, `/availability` and `/schedule`, anonymously. The server
renders the first view through a universal load (8 s timeout); a failed load
shows "Try again", never a crash, and a cake that is not active is a 404 with
"This cake is no longer in our collection".

- **Prices and allergens** are the server's: the recipe version's price for a
  made-to-order size, the lot's own price for a ready-made cake; the exact
  allergen declaration, prominent allergens called out ("Contains PEANUTS"),
  and the shared-kitchen notice. A variant without a current recipe and its
  declaration is shown but cannot be bought (K01). A ready-made cake is offered
  only when made to the variant's current recipe version (as checkout), with
  its stock left and the chef's collect-by time (the lot's expiry).
- **Sizes and starting prices** is the owner's GK-002 base price list
  (docs/creator/GK-002), a public guide; it is not used for any total.
- **Date and time picker** (shared dialog on the shop, cake page and bag): Collect /
  Own courier (GK delivery shown disabled, as in checkout), a Monday-first
  month labelled from `/availability` (places left for made-to-order cakes,
  Closed, Too soon, Not open, Full, Too late after a ready-made cake's
  collect-by date), times loaded automatically from `/schedule` with distinct
  loading, no-times (next dates and "Contact the chef") and error ("Try
  again") states, obsolete answers ignored, keyboard arrows and Page Up/Down.
  "Use" checks the time again; a taken time is answered with fresh choices and
  nothing is chosen for the customer; closing the dialog during that check
  is a cancel and saves nothing.
- **Bag** (`src/lib/shop/bag.ts`): catalogue keys (`v:<variantId>`,
  `lot:<lotId>`, each once, at most 20), the date, handover choice and time,
  in this tab's `sessionStorage` (`gk-shop-bag`; no prices, names or personal
  data). The bag page rechecks every cake against the catalogue, removes (and
  says so) what can no longer be bought, shows each current price and leaves
  the total to checkout. "Review checkout" opens
  `/checkout?v=…&lot=…&date=…` (see "Basket" above); checkout asks for the
  time and handover again until it can read them.
- **Colour filter** (approved sketch): a Colour select on the shop, shown only
  when at least one cake has a colour, filtering by its code; "Clear filters"
  resets it. Each cake has at most one main colour, read by `colourOf` in
  `src/lib/shop/catalog.ts` from `"colour": {"code": "pink", "label": "Pink"}`
  or `null` on each cake (code `^[a-z0-9][a-z0-9-]{0,39}$`, label 1–40
  characters; anything else counts as no colour).
- **Not built yet:** favourites, the custom request, reorder and the account
  pages (GK-016 r02).

### TODO (GK-028)

- **TODO — cake photos.** The shop is live with labelled "Photo coming soon"
  placeholders (Sasha's decision, 2026-11-05) because the public catalogue has
  no images yet. Done when `GET /api/v1/cakes` and `/cakes/{id}` send each
  cake's approved photo (K01 images: image revision, url, alt, width, height;
  storage is SETUP.md provider row 04; owner photos in `docs/creator/GK-002`)
  and `src/lib/shop/ui/CakePhoto.svelte` shows it everywhere a placeholder is
  shown now (shop and home cards, cake page, bag rows). The placeholder stays
  only for a cake without an approved photo. Also tracked in a TODO comment in
  that component and on the GK-028 Trello card.
- **TODO — colour data.** The Colour filter is built but stays hidden until
  the API sends the `colour` field above (proposed: a chef-managed colour list
  and one optional colour per cake, set in the chef's catalogue screens) and
  the chef assigns a colour to each cake. Nothing in the UI needs to change
  then.

## Dependency audit

`npm audit` reports low/moderate advisories in development tooling only
(eslint/minimatch, and deepmerge used by the Svelte Vite plugin at build time).
None of these packages are present in `build/`, and npm's suggested fixes are
major downgrades. CI therefore does not run `npm audit`; revisit when upstream
releases fixes.
