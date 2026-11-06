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

**Order changes (GK-030 slice 1)** follow the Creator-approved GK-017 r02b
states 12, 13 and 18 (`src/lib/checkout/changes.ts`, components in
`src/lib/checkout/ui/`, styles in `src/lib/checkout/changes.css`). They are
offered to the signed-in owner or this tab's guest capability (checkout binds
it with read, pay, cancel and move_pickup; the API checks it again).

- **Cancel**: "Preview cancellation" on a confirmed, in-production or ready
  order (never after a GK delivery was sent out, never while another change
  is processed; while a GK delivery choice is open, `delivery_choice.open` or
  `delivery_failure.open`, it says "To cancel this order, please contact the
  chef" instead, as those choices can return everything) opens "Cancel your
  order?" with
  `POST /orders/{id}/cancellation-preview`: the preview's `amount_minor`
  exactly (never computed here), the side of the deadline from its `rate`,
  the time left as of its `computed_at`, the labelled rounding example after
  the deadline, and for a paid GK delivery the allocations back to the
  payment method beside `delivery_pending_minor` sent by the chef.
  `POST /orders/{id}/cancel` sends the preview's version, ID and hash with one
  Idempotency-Key per preview. A preview is asked again at the deadline or
  its 10-minute expiry and after `PREVIEW_STALE` / `STALE_VERSION`; when the
  rate dropped, the boundary alert ("The full-refund deadline has just
  passed…") takes focus and the button shows the new amount; another amount
  at the same rate is announced ("We've refreshed your refund amount…").
  "Keep my order" is inactive while a confirmation is being sent. Only an
  outcome newer than the order as it was when the panel opened (or the move
  was sent) counts, so an earlier refusal or move is never shown as the new
  one. The page then
  reads "Cancelling your order" (`cancellation_pending`, polled) until the
  refund appears.
- **Refund progress**: a cancelled order with a refund block shows why (by
  `cancel_reason`: customer before/after the deadline, chef's cancellation,
  date not agreed, couldn't deliver), the refund line, a three-step tracker
  from `refund.state` only (past tense only once finished), the chef's
  delivery refund on its own line, and "Refund due" / "Returned so far".
- **Change your time** (collection and own courier): the move choices
  (`GET /schedule?purpose=pickup_move&order_id=…`) as a radio list of
  start–end times with the current one marked; disabled after the six-hour
  cutoff; `POST /orders/{id}/pickup`, polled until moved or refused; a stale
  choice keeps the booking and reloads the choices with that time struck
  through. GK delivery orders have no time control ("please contact the
  chef").

**Not built here:** the discovery pages and real basket (GK-016), the GK
delivery request and quote flow, and the other GK-017 r02b problem states
(approved 2026-11-05; only states 12, 13 and 18 are built, above): those are
shown plainly with the messages above.
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
  `src/lib/shop/catalog.ts` from
  `"colour": {"code": "pink", "label": "Pink", "position": 10}` or `null` on
  each cake (code `^[a-z0-9][a-z0-9-]{0,39}$` other than `all`, label 1–40
  Unicode code points without surrounding whitespace, position an integer
  0–10000; anything else counts as no colour). Colours are listed by position,
  then label.
- **Favourites** (`src/lib/shop/favourites.ts`, approved sketch): the heart
  on every cake card saves or removes the cake on this device ("Saved on this
  device for a year." / "Removed from favourites.", a polite status message).
  Only cake IDs are kept, in `localStorage` (`gk-shop-favourites`, at most
  200), until the end of the same London date one year after the last save or
  removal (29 February clamps to 28 February; contracts K09); reading never
  extends it, an expired or damaged entry is removed and a stored expiry
  beyond a year from now is capped. They are read again before every change,
  when the page is shown again and when another tab changes them, so
  favourites that expired while a page stayed open never come back. A browser
  that blocks storage gets "This browser cannot keep things on this device."
  and nothing changes. `/favourites` (noindex) shows the saved cakes as
  current shop cards, with the date they are kept until; cakes no longer in the
  collection are left out, and removing one moves focus to the next heart.
  Adding favourites to an account (K02 `PUT/DELETE /me/favourites/{cake_id}`)
  comes with the account pages.
- **Not built yet:** the custom request, reorder and the account pages
  (GK-016 r02).

### TODO (GK-028)

- **TODO — cake photos.** The shop launches with labelled "Photo coming soon"
  placeholders (Sasha's decision, 2026-11-05) because the public catalogue has
  no images yet. Done when `GET /api/v1/cakes` and `/cakes/{id}` send each
  cake's approved photo (K01 images: image revision, url, alt, width, height;
  storage is SETUP.md provider row 04; owner photos in `docs/creator/GK-002`)
  and `src/lib/shop/ui/CakePhoto.svelte` shows it everywhere a placeholder is
  shown now (shop and home cards, cake page, bag rows). The placeholder stays
  only for a cake without an approved photo. Also tracked in a TODO comment in
  that component and on the GK-028 Trello card.
- **TODO — colours in production.** The API sends each cake's `colour`
  (`0028_catalog_colours.sql`, api/README "Catalog"), and the Colour filter
  appears as soon as a cake has one (on staging: the confirmed list Pink,
  Silver, Red, Chocolate, White, with Pink on the STAGING TEST cake). Still to
  do: the chef creates that list in production and gives each cake a colour,
  in the chef's catalogue screens (GK-018, not built yet). The shop needs no
  change.

## Customer account (GK-029)

Built from the Creator-approved GK-016 r02b sketch
(`docs/design/sessions/discovery-account/gk-016-r02/`) on the account API
(api/README "Customer accounts"). Private pages (`/account…`: `no-store`,
`noindex`, `ssr = false`), in the shop frame with the sketch's account
navigation (`src/lib/account/ui/AccountShell.svelte`, styles in
`src/lib/account/account.css`). No analytics (K10).

- **Client** (`src/lib/account/client.ts`): `GET /me`, `/me/orders` (every
  page), `/me/order-claims`, `/me/profile`, and the profile, claim and
  withdraw commands with an Idempotency-Key and only the documented fields.
  `src/lib/account/session.ts` uses the browser's session token;
  `src/lib/account/guard.ts` handles no session, an ended session (401: sign
  out here, sign in again), a disabled or deleted account and signing out in
  another tab.
- **Account home** (`/account`): who is signed in; a staff-area card for staff
  accounts; **Your orders** in the sketch's "Order list mapping"
  (`src/lib/account/orders.ts`, worded as the order page): In progress, and
  Past behind "Show past orders (n)" (`aria-expanded`); each row has the start
  of the order ID, the cakes, date, time and handover, a chip (words and a
  dot), the line, the server's total and one action (Continue to payment,
  Try paying again, Confirm order or View order, all opening
  `/orders/[id]`); an unknown lifecycle or payment state shows "Status not
  available" and enables nothing. **Your requests**: requests to add older
  orders with their state (`src/lib/account/claims.ts`; never the chef's note,
  nothing about an order before approval). **Your favourites**: the device's
  favourites (no account favourites API yet). A failed orders read shows
  "Try again" (focus returns to the card) while the other cards still load.
  Each request links to its page (View →); the orders card ends with "Can’t
  see an order? Add an older order" and an **Ordered before?** card opens the
  form.
- **Add an older order** (`/account/older`, sketch states 38–39): the chef’s
  reference, how the customer ordered, roughly when (optional month and
  year, sent as YYYY-MM; one without the other is an error) and a short note
  (no card details or health information), checked before sending with the
  API's path c rules and the sketch's words (`src/lib/account/older.ts`:
  reference 1–64 and note 1–500 characters, trimmed, line breaks only in the
  note). "How did you order?" offers only what the API records: Instagram
  (`instagram`) or Email, phone or in person (`offline`); website orders are
  added from their order page later. One Idempotency-Key per request (kept
  while the same request is retried after a lost answer). Error summary and
  inline errors as on the profile; a 409 (5 requests already waiting, or
  already asked) or a failure shows a notice. Sent → the request page. Side
  card: how it works (3 steps), the guest-order note (adding from the order
  email isn't available yet) and asking the chef.
- **A request** (`/account/requests/[id]`, states 40–42): waiting for the
  chef (what was sent, Back to your account, **Withdraw this request** with
  the version seen and one Idempotency-Key per withdrawal; "Request
  withdrawn…" in a polite status region, focused; a request that changed
  meanwhile is shown as it now is); added (the order's reference and View
  order, from `order_id`, which the API gives only once approved); couldn't
  add (contact us, nothing about any order); withdrawn. Another account's
  request, or none, is "Request not found". The state's notice takes focus.
- **Profile & contact** (`/account/profile`, sketch states 43–46): **Your
  details** — full name, phone and Instagram name, all optional, checked
  before sending with the API's rules and the sketch's words
  (`src/lib/account/profile.ts`: trimmed, an empty field clears it, a typed
  `@` is dropped from an Instagram name); the PATCH carries only the changed
  fields, the version the customer saw and one Idempotency-Key per save
  (kept while that same save is retried). Error summary with links (focused,
  `role="alert"`), errors tied to their inputs and updated as the customer
  types; "Saved." in a polite status region (focused). A 409 STALE_VERSION
  loads the latest details, keeps the customer's own changes that differ
  (outlined, "Saved elsewhere: …" in the input's description) and says which
  were kept. **Signing in** shows the session's email and "one account per
  verified email address"; **Emails we send** has the r02b wording (no
  marketing). The name hint about Google and the checkout prefill waits for
  those features.
- **Privacy & your data** (`/account/privacy`, states 48 and 57–59): the
  r02b retention table (caption, row headers, stacked on narrow screens;
  K09 and the owner's GK-005 rules, payment records "being confirmed") and
  "No analytics run on account pages". **Sign out everywhere** asks first,
  then calls the provider's global sign-out from the page (this device is
  always signed out; other devices "usually within an hour", never
  instantly) and shows the built partial wording when the provider can't be
  reached. **Clear this device** removes the favourites (localStorage), the
  bag (this tab) and the deletion marker (`src/lib/account/privacy.ts`
  `clearDevice`); never the session or a guest order's access.
- **Delete your account** (`/account/delete`, states 50–56): the API's
  preview (`POST /me/deletion-preview`) decides the step
  (`deletionStep`): a staff account can't be deleted here; unsettled money
  blocks (each order with the sketch's reason, Delete disabled); confirmed
  upcoming orders warn with an extra tick; otherwise the review (what is
  deleted and kept, "can't be undone" tick; `aria-invalid`, focus on the
  first missing tick). Then a fresh sign-in by email link
  (`signInWithOtp` to the signed-in address, `shouldCreateUser: false`,
  return path `/account`) and a marker in localStorage holding only its
  expiry and a SHA-256 of the account's subject (30 minutes, as long as the
  link; the link usually opens in a new tab). It counts only for that
  account, and the account pages' Sign out, Sign out everywhere and an ended
  session remove it, so someone
  else signing in on this browser never skips the review. Back on `/account` with the marker and a session signed in by email
  link or Google within 10 minutes (the token's `amr`, as the API reads it),
  a notice leads on to the final step, which asks for a new preview,
  requires the upcoming-order tick again if there is one, and deletes with
  that preview's ID and hash and one Idempotency-Key per deletion (kept on
  a lost answer). Refusals: fresh sign-in needed → the sign-in step again;
  blocked or stale → checked again and shown; staff → its message.
  Deleted: signed out here, the r02b wording, Clear this device, Back to
  the shop. "Keep my account" forgets the marker.
- **Not built yet:** changing the sign-in email (needs GK-021 rules);
  Google for the fresh sign-in (Google sign-in isn't live) and the
  inactivity emails (state 49, GK-031); the email-link claim
  ("Add this order to my account") belongs on the order page and waits for
  the emailed order link; the chef's claim queue (admin); the sign-in page
  changes of the sketch (account on first use, Google) belong to GK-021;
  custom requests and reorder wait for their APIs.

## Staff catalogue (GK-018 groundwork)

`src/lib/admin/catalogue.ts` is a typed client for the chef's catalogue in the
staff API (api/README "Catalog"): reads (cakes in every state, all pages; one
cake; a recipe version; flavours; the current allergen notice; colours;
sizes, allergen codes and units) and commands (flavour, allergen notice,
colour create/update, cake create/update/state, variant create/archive,
recipe version create). Commands carry an Idempotency-Key and send exactly
the fields given (a PATCH names only what changes; `colour_id: null` clears),
path IDs must be UUIDs, and prices must be whole pence. `apiPatch` in
`src/lib/api.ts` sends the PATCH commands. `src/lib/admin/session.ts` calls it
with the signed-in staff member's token.

`/admin/catalogue` (private, linked from `/admin`) is a plain read-only list
of cakes by state (drafts, live, archived) with their colour and the sizes
still offered, and the colour list. It is groundwork, not the chef's screens:
those follow the GK-018 design once Sasha approves it (draft PR #15, r03b2 is
not approved), and no catalogue change can be made from the UI yet.

## Chef workspace: Orders (GK-018 r03b2, GK-031)

`/admin/orders` (private, `ssr = false`) is the first chef-workspace page,
built from the Creator-approved GK-018 r03b2 sketch (draft PR #15, head
`5df924b`; approved on the GK-018 card on 2026-11-05) in the workspace frame
(`src/lib/admin/ui/ChefShell.svelte`, styles `src/lib/admin/chef.css` scoped
to `.chef`; sections not built yet show "later"). It lists orders by handover
date from `GET /api/v1/admin/orders` (`src/lib/admin/orders.ts`, all pages)
for chef, technical support and fulfilment, each seeing only what the API
gives their role (money for the chef only; no attention flags for
fulfilment). Filters: When (from today or before today, 93 days each, the
API's longest range; this week; next week), How (handover mode) and the
status chips ("Needs a manager" = a refund needing attention, a mismatching
payment or a refund made outside GK Cakes, each named under the order's
status in the sketch's words; paid late has its own chip);
released orders nobody paid for are hidden unless "Show released orders that
weren't paid" is ticked (Sasha's answer to r03 Q5; the chef's view only, as
only the chef's rows can tell an unpaid release from a returned payment). Search matches the
loaded orders by name or the start of the order ID in the browser (the API
has no text search, K09) and is not put in the URL; the When, How, status and
released choices are. Technical support's rows carry no money or payment
requirement, so its wording says only what it knows ("Waiting for payment or
confirmation", "Confirming the order", "Confirmed · automatic start later",
"Released", "After payment or confirmation"); free orders read "Free · …"
and "After confirmation" for the chef, never "Paid". Read
only for now: no order page and no actions yet.

Differences from the sketch, each because the API doesn't give it yet:
no order number (the first 8 characters of the order ID are shown), no
"Needs action" chip (collection-details emails), no "Dates by production" or
"All dates", no source/extra/short-notice tags, no PEANUTS flag in the row, no
"New order" button, and "Cancelling · not confirmed yet" for
`cancellation_pending` and "Free · not confirmed yet" / "Free · confirming
the order" for free orders (the sketch has no wording for these). The money
column is the chef's only (the API sends money to no other role; the sketch
also showed it to technical support); a partly refunded order shows the
server's two amounts ("Partly refunded · £35 of £90"), never a difference
worked out here. Who is signed in shows the role (the API has no staff
names); the brand links to Orders (Today isn't built). The "later" labels and
input borders are darker than the sketch's for contrast.

## Chef workspace: Account requests (GK-018 r04d-claims)

Built from the Creator-approved GK-018 r04d-claims sketch (draft PR #59, head
`c372c9c`; approved "as suggested" on the GK-018 card on 2026-11-06) in the
same workspace frame. Chef only: the sidebar shows **Account requests** with
the waiting count (the first waiting page, "50+" when there are more; there
is no count route) to the chef only, and other roles see a role note (the API
refuses them, 403).

- `/admin/requests` — "Requests to add orders": customers' requests from
  "Add an older order" (and, later, from an order page or order email,
  tagged **Future**) in tabs Waiting, Added, Couldn't add and Withdrawn
  (`?tab=`, the only thing in the URL), oldest received first, 50 at a time
  with "Show the next requests" (focus moves to the first new row). Each row:
  received and the request's short reference; the profile's name, Instagram
  and phone, labelled as the customer's own typing, not proof (Creator answer
  2); how they ordered, their reference, roughly when and "Looks like a
  website order reference" (a format hint, never a match); their note;
  waiting time, amber from 3 days as a visual cue only (answer 9); decided
  rows add the outcome, when, by you / another chef account and the internal
  note. A banner says only website orders can be added for now (answer 4).
- `/admin/requests/[id]` — one request: what they sent, who's asking (from
  their profile) and, once decided, the outcome with what the customer sees.
  The API has no single-request read yet, so the page pages the four lists
  (`findClaim` in `src/lib/admin/claims.ts`); a request closed by account
  deletion is in none of them and reads "Request not found". Deciding
  (finding the order, the contact-email check, adding or declining) is the
  next slice; until then a waiting request says so.

`src/lib/admin/claims.ts` holds the typed reads, `decideClaim` (strict JSON:
`expected_version`, `decision`, `order_id` for approve only, optional
`note`) and the sketch's wording. Not built from the sketch yet: the Today
line (Today isn't built), deciding, the order page's "Customer account" card,
and the proposed API fields (single-request read, erased / account-deleted
flags, newest-first decided lists, the chef-only "already in an account"
flag of answer 8).

## Dependency audit

`npm audit` reports low/moderate advisories in development tooling only
(eslint/minimatch, and deepmerge used by the Svelte Vite plugin at build time).
None of these packages are present in `build/`, and npm's suggested fixes are
major downgrades. CI therefore does not run `npm audit`; revisit when upstream
releases fixes.
