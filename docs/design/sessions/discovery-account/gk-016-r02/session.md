# GK-016 r02 — customer account: sign-in, orders, requests, adding older orders, profile and privacy

2026-11-05. Card GK-016 (Owner: Agent, Kind: Design). r02 covers the **customer account** that r01/r01b left for
later: sign-in and sign-up in one step (email link that creates the account on first use, plus "Continue with
Google"), the account home (orders with clear states, requests, favourites), the entry into the order page, both
ways of adding an older order, profile and contact details, and privacy (retention, the inactivity warning,
deleting the account, signing out everywhere). **Not approved.** It needs exact-version Creator approval and then a
fresh integration review (AC3). It is built on r01b (`122e9f9`), which is itself not yet approved.

## Execution check

Role: GK-016 design worker, bounded r02 sketch. Card policy requests `gpt-5.6-sol` / high in a fresh scoped worker.
The host reports **`llmplatform/claude-opus-5-5`** (system environment) running as a **fresh subagent** with only the
master's brief and the inputs below — status **mismatch; the current user-selected model takes precedence**. This
worker is the author, so this is not an independent review. Writes: this folder only, worktree branch
`sasha-chycha/gk-016-r02-design` from the r01b head `122e9f9`. `api/`, `ui/`, r01 files and other worktrees were not
touched. No corporate tools (Glean, Slack, Jira, Grafana, Snoocode GitHub tools), no Trello, no PR.

Readback: B1 — the email sign-in link creates the account on first use; Google sign-in is also offered. B2 — older
orders are added either (a) by a signed-in customer opening the order's private email link and asking to add it,
or (c) for old Instagram/offline orders by the chef's reference plus a note, approved by the chef; short proof →
decline with "contact us" and reveal nothing; the chef may also add orders on request. K00: no automatic
association by email/payer/Instagram; claims need the authenticated customer and chef approval; a pending claim
reveals no order details; generic 404 for absent/foreign proof; guest links are revoked when a claim is approved.
K09: account/profile/favourites/basic history kept until 24 months without sign-in or new order, then a warning
and 30 days; deletion revokes capabilities. K10: no analytics on account routes. B3–B7 are r01b matters and are
unchanged here.

### Inputs (SHA-256, first 16 hex)

| Input | Hash |
| --- | --- |
| `AGENTS.md` | `73502bdeedc5697d` |
| `skills/gk-plan/references/execution-check.md` | `97d49fb1ad2ec4c2` |
| `docs/design/contracts.md` r6.5 (K00, K02, K09, K10) | `b34b0926f371683a` |
| `gk-016-r01/session.md` (r01b) | `598baf1a9a6aeec4` |
| `gk-016-r01/poc/app.js` · `styles.css` (copied as the base) | `931d6a48f3ac2ce4` · `cfde3e63c3d84ff0` |
| GK-016 card export `gk016-card.md` · description `gk016-desc.md` | `47b4b38c56c8b81c` · `46ebf832033ae667` |
| GK-016 comments export `gk016-comments.md` (preparation + Creator answers 2026-11-04) | `139dce13c786b3c1` |
| `answers-1104/GK-016.md` (Creator answers 2026-11-04, B1–B7) | `380b7f8866662df2` |
| `origin/main` (`bed0a67`) `ui/src/routes/sign-in/+page.svelte` | `8a9e321987d083cc` |
| `origin/main` `ui/src/routes/auth/callback/+page.svelte` | `718737c1d67d6669` |
| `origin/main` `ui/src/routes/account/+page.svelte` | `0de253f9e1282a68` |
| `origin/main` `ui/src/routes/orders/[id]/+page.svelte` · `ui/src/lib/checkout/order.ts` | `3b02ed2e2a4fc5dd` · `e9ca13d1dfd140dd` |
| `origin/main` `ui/src/lib/guest.ts` · `ui/README.md` | `11f61ce0ae207e2c` · `ccaaa66ee3cb411c` |
| GK-017 r02 sketch on `origin/sasha-chycha/gk-017-r02-design` (`dc2bd09`): `session.md` · `poc/r02.js` | `e5766ee6970b4369` · `8e143fb23765dda3` |

(`ui/` in this worktree is the older base without `orders/[id]`, so the live pages were read from `origin/main` with
`git show`.)

What exists live today (origin/main): `/sign-in` with email + password or an emailed one-time link
(`shouldCreateUser: false`, invitation-only); "If an account exists… It works once, in this browser, for 30
minutes"; `/auth/callback` ("This sign-in link didn't work… expired, been used already, or been opened in a
different browser"); `/account` shows only the account ID, staff link and Sign out; sign-out reports when the
provider couldn't end the session everywhere; `/orders/[id]` reads with the tab's guest capability (sessionStorage)
or the account token and shows the lifecycle titles reused here.

### Outputs (SHA-256, first 16 hex)

`poc/r02.js` `fe70ecdbf5744d88` (new), `poc/app.js` `948c856464ddbbd4`, `poc/styles.css` `7cdf8c4091f6c04c`,
`shoot.sh` `17c83540ad45e955`, `shot.html` `83c8d38d8a032110`; new pages `signin.html`, `claim.html`,
`profile.html`, `privacy.html`, `states.html`; `account.html` and `order.html` now also load `r02.js`. All other
`poc/` files and `poc/assets/` are byte-identical to r01b. `screens/` holds 110 JPEGs (55 views × desktop 1440 /
mobile 390, about 18 MB).

## How the prototype is organised

- `poc/` is a copy of r01b. `r02.js` holds every account state and its example data (`FX`, `ORDERS`); `app.js`
  changed only to merge `window.R02.pages()` into its router and call `window.R02.bind()`, use storage keys
  `gk-poc-gk016r02-`, label the review bar "GK-016 r02", drop the old r01 "signed in" demo from the account page,
  and show the order reference on "Order again". An r02 section is appended to `styles.css` (`acc-*` classes).
- Each state is `page.html?state=…`. Every r02 page opens with the **design-review switcher** (striped yellow,
  dashed border, mono type, "Prototype only · design review · not part of the shop"): state picker grouped by
  area, previous/next through all 52 states, "All states" (`states.html`), and a "Simulates" line with the API or
  provider event. Prototype-only hints inside pages use the same dashed yellow "Prototype" label. Neither is part
  of the shop design.
- **Money**: every figure is an example server value (£90, £105, £285, £180 recorded by the chef, refund £45 of
  £90, £90 returned). The page never adds, halves or rounds money.
- **Google button**: a static local SVG "G" and the label "Continue with Google"; no Google script, font or
  request. A prototype note says so; the live button must follow Google's sign-in branding.
- **No analytics** and no network requests other than the local files. `?shot=1` hides the footer for screenshots
  and stops automatic focus moves/redirects.

## States — what each shows, buttons and destinations

| # | State (`?state=`) | What the customer sees | Buttons → destination |
| --- | --- | --- | --- |
| 1 | `signin` | "Sign in or create an account" (one step). Email field, **Email me a sign-in link**, "New here? The link creates your account the first time you open it", or, **Continue with Google**. Side card: what an account gives; "You can always order without an account"; "No analytics run on sign-in or account pages". | Email me a sign-in link → client check, then `sent`; Continue with Google → `callback` (Google) → `created-google` |
| 2 | `signin-errors` | Error summary + inline "Enter an email address like name@example.com" (empty: "Enter your email address"), focus on the field. Nothing is sent. | Same as 1 |
| 3 | `sent` | "Link sent to anna@example.com": open it in **this browser** within 30 minutes, works once; first time creates the account; check spam; new link in a minute. Same answer for every address and when rate-limited (never reveals whether an account exists). | Send a new link (countdown 0:45, then "Sent again… use the newest link"); Use a different email → `signin`; prototype hints: first time / returning / after 30 minutes |
| 4 | `callback` | "Signing you in…" spinner, keep this tab open. | Goes on by itself → `created`, `created-google` or account home |
| 5 | `created` | "Welcome to GK Cake London · Account created": created for anna@example.com when the link was opened; **"We never add orders to your account automatically, even ones made with this email address. You choose which to add."** Next: favourites on this device (Add to my account / Keep on this device only), Add an older order, Profile & contact. | Go to your account → empty account; merge buttons → inline status |
| 6 | `created-google` | As 5, "with your Google account… Google shares your name and email address with us, nothing else"; name prefilled from Google. | As 5 |
| 7 | `link-failed` | "This sign-in link didn't work": expired (30 minutes), used, or another browser. Get a new link form + Google. | Email me a new link → `sent` |
| 8 | `google-cancelled` | "Google sign-in was cancelled, so nothing changed. Try again, or use an email link." | As 1 |
| 9 | `google-error` | "We couldn't sign you in with Google. Please try again, or use an email link instead." | As 1 |
| 10 | `unavailable` | "Sign-in is not available right now. Please try again in a few minutes. You can still order without an account." | Try again |
| 11 | `session-ended` | "Your session has ended. Please sign in again." (built wording) | As 1 |
| 12 | `signed-out` | "You're signed out on this device." Favourites in this browser stay; hint about Sign out everywhere. | Sign in again; Back to the shop |
| 13 | `home` | Account nav (Orders & requests · Favourites · Add an older order · Profile & contact · Privacy & your data · Sign out). **Your orders**: *In progress* — GK-1108 "Still checking payment" (don't pay again), GK-1096 "Payment received · date to agree" (date shown as requested, not confirmed), GK-1087 "Confirmed", GK-1115 "Status not available", GK-1061 "Cancelled · refund pending" (£45 pending); *Past* — GK-1042 "Cancelled · refunded" (£90 returned), GK-1009 "Collected", GK-0931 "Collected · added by the chef" (£180 recorded by the chef). Coloured chip with a dot plus words (never colour alone). **Your requests**: GKR-7Q4M "Ready to send" (we can't see whether it was sent; copy again, same request), GKR-3HDX "Offer from the chef" (review and pay by a 24-hour example deadline), GKR-9WPL "Offer expired", GKC-28 older-order request "Waiting for the chef"; requests kept 6 months after the last conversation. **Your favourites**: "2 favourites on this device aren't in your account yet" merge choice + account favourites. "Ordered before?" card. | View order → order detail state; Order again → `reorder.html?order=GK-1009`; Copy request & open Instagram/email → status (same request, nothing sent); Review the offer → status (offer page belongs to GK-017/GK-018); Ask the chef for a new one → contact; Add to my account / Keep on this device only → status; View all favourites → favourites (signed in); Add an older order → `old-form` |
| 14 | `empty` | New account: "No orders yet" + Shop cakes / Create your own + "Add an older order"; no requests; nothing saved. | Shop cakes; Create your own; Add an older order |
| 15 | `kept` | Banner: "Welcome back. Your account was due to be deleted on Sat 5 December 2026 because it hadn't been used for 2 years. Signing in has kept it… at least another 24 months". | As 13 |
| 16 | `chef-added` | Banner: "The chef added an order to your account, as you asked: GK-0931…"; that row highlighted "New". | View order GK-0931 → `order-older` |
| 17 | `load-error` | Orders card: "We couldn't load your orders right now. Please try again." Requests and favourites still load. | Try again → `home` |
| 18 | `disabled` | "This account is disabled. Please contact us if you think this is a mistake." (built wording), no account data. | Contact us; Sign out |
| 19 | `signed-out-account` | Opening the account without a session: "Please sign in to see your account. After signing in you come back here." + sign-in form. | As 1 |
| 20–27 | `order-checking`, `order-agreement`, `order-confirmed`, `order-refund`, `order-refunded`, `order-completed`, `order-unknown`, `order-older` | The order page layout (status card + "Order details" summary) with "← Your orders". Titles and lines follow the built order page and GK-017 r02: "Still checking payment; do not pay again"; "Payment received" (requested date/time "not confirmed yet", never moved without agreement, everything back (£105) if no date is agreed); "Your order is confirmed" (own courier note, full-refund deadline example 10:00 Sun 22 Nov, 50% after); "This order was cancelled" + "Refund of £45 pending…" + why £45 (50% inside 120 hours); refund of £90 returned; "Your order is complete"; "We can't show this order's status right now" (no actions); older order "added by the chef… paid outside this website… nothing to pay or refund here" with chef-recorded values. | Contact us / Contact the chef; Change your time / Preview cancellation → r01b order page previews; Order again → reorder; Request something similar → custom request; Shop cakes |
| 28 | `link-guest` | Guest order page from the private email link, not signed in. Card "Keep this order in your account": sign in, then open the email link again and choose "Add this order to my account"; the chef checks it; "We never add orders to an account automatically, even when the email address matches." | Sign in or create an account → `signin` |
| 29 | `link-offer` | Same order while signed in: "Add this order to your account? …Ask to add it to anna@example.com and the chef will check it… Until she has, keep using the link in your email." Wrong account → Sign out. | **Add this order to my account** → `link-asked`; Not now → account |
| 30 | `link-asked` | "Asked. The chef will check this order and add it… We'll email you… keep using the link". | Waiting for the chef (disabled); Back to your account |
| 31 | `link-added` | "GK-1120 is now in your account… The private link in your email no longer works for this order: sign in to see it." | View in your account |
| 32 | `link-yours` | "This order is already in your account." | View in your account |
| 33 | `link-dead` | "This order link doesn't work": may have been replaced, or already added to an account; contact us with the reference from the email. **No order details, no reference, no hint which account** — also covers "already in another account". | Contact us; Go to your account |
| 34 | `old-form` | "Add an older order": for Instagram/email/in-person orders. Fields: Order reference from the chef (required, example IG-0613), How did you order (Instagram/Email/In person/Other), When was the cake for (optional month/year), A short note for the chef (required, ≤500; no card details or health information). Side: how it works (3 steps; no details shown until the chef adds it), guest orders use the email link, or ask the chef to add it. | Send to the chef → `old-pending` (after client checks); Cancel → account |
| 35 | `old-errors` | Error summary + inline errors for reference and note; focus to the first. | As 34 |
| 36 | `old-pending` | "Your request GKC-28 is waiting for the chef. Nothing has been added… we won't show any order details until she has checked it." What you sent (reference, how, when, note). | Back to your account; Withdraw this request → status |
| 37 | `old-approved` | "The chef matched your request GKC-28 and added order GK-0931 (Instagram order, Sat 13 June 2026)". | View order GK-0931 → `order-older`; Back to your account → `chef-added` |
| 38 | `old-declined` | "We couldn't add this order. The chef couldn't match request GKC-28 from the details given, so nothing was added. Please contact us…" Reveals nothing about whether an order exists. | Contact us; Back to your account |
| 39 | `profile` | **Your details**: Full name (optional; prefilled at checkout, editable per order), Phone (optional; only if the chef needs to reach you), Instagram name (optional, private, "doesn't prove who you are and isn't used to sign in"). **Signing in**: email (sign-in links and order emails), Email link on, Google connected; Change sign-in email (disclosure). **Emails we send**: service emails needed for orders can't be switched off; no marketing. | Save changes → `profile-saved` (or inline errors); Send a confirmation link → `email-change-sent` |
| 40 | `profile-saved` | "Saved." | As 39 |
| 41 | `profile-errors` | Error summary; phone "Enter a phone number like 07700 900123 or +44 7700 900123"; Instagram "letters, numbers, full stops and underscores". | As 39 |
| 42 | `profile-conflict` | "These details were changed somewhere else (perhaps in another tab)… loaded the latest… kept your phone number change. Check them and save again." The kept edit is outlined, with the saved value beside it. | Save changes |
| 43 | `email-change-sent` | Confirmation link sent to the new address; changes when opened (30 minutes, this browser); keep using the old address until then; the old address is told. | — |
| 44 | `privacy` | **Your data and how long we keep it** (active account, last sign-in today; table: account/profile/favourites/order list — 24 months without sign-in or order → warning email → 30 days; order details 2 years after handover/cancellation; requests 6 months after last conversation; request photos 30 days after handover or 3 months with specific permission; payment records for the legal period, being confirmed; on this device favourites 1 year, draft 30 days without description/photos); "No analytics run on account pages". **Sign out everywhere**, **Delete your account**, **This device** (clear). | See the warning email → `warning-email`; Sign out everywhere… → `signout-all`; Delete my account… → `delete-review`; Clear this device → status |
| 45 | `warning-email` | Example plain email (no tracking images or links): subject "Your GK Cake London account will be deleted on Sat 5 December 2026"; 2 years without sign-in or order; in 30 days; sign in to keep it; what is deleted, what the law requires us to keep; orders in progress carry on. | Sign in to keep my account → `kept` |
| 46 | `delete-review` | **We delete**: sign-in (email link and Google), name/phone/Instagram, account favourites, requests and their photos, order list and private order links. **We keep, because we must**: payment and refund records for the legal period (name removed where we can), anything for an open dispute; messages stay in Instagram/email. Backups cleared within 30 days; favourites in this browser stay until cleared. Tick "I understand that deleting my account can't be undone." | Delete my account → `deleted` (needs the tick; inline error otherwise); Keep my account → `privacy` |
| 47 | `delete-blocked` | "You can't delete your account yet. These orders still have money to settle: GK-1108 still checking a payment; GK-1096 payment received, date to agree; GK-1061 refund of £45 on its way." Contact line. Delete button disabled. | Order links → order detail; Back to your account |
| 48 | `delete-warn` | "You have an upcoming order: GK-1087, Fri 27 November 2026, confirmed. If you delete your account now, the order still goes ahead, but you won't be able to see, change or cancel it online. The chef will contact you by email." Extra required tick for the order. | Delete my account → `deleted` (both ticks); Keep my account |
| 49 | `deleted` | "We've signed you out and deleted your account details… last email to confirm… backups within 30 days… records we must keep stay, with as little personal information as possible." | Clear this device; Back to the shop |
| 50 | `signout-all` | "Sign out everywhere? …this device and every other one. Orders and favourites aren't affected." | Sign out everywhere → `signout-all-done`; Cancel |
| 51 | `signout-all-done` | "You're signed out on all your devices. Other devices are asked to sign in again when they next check, at the latest within an hour (example; GK-021 confirms the timing)." Never claims instant revocation. | Sign in again |
| 52 | `signout-all-partial` | Built wording: "You are signed out on this device, but the sign-in service could not be reached to end the session everywhere." | Sign in again |

Also linked: `favourites.html?demo=signed-in` (r01b merge panel) and `reorder.html?order=GK-1009` (r01b order again).

Accessibility patterns reused from r01b/GK-017: one `h1`; skip link; native inputs with labels, hints and errors
tied by `aria-describedby`; error summaries with links (`role="alert"`, focused); status changes in polite live
regions and focus moved to the new status on arrival; status chips use words plus a dot (not colour alone, chip
text ≥ 7:1); visible focus ring; ≥ 44 px targets; account nav as a labelled `nav` with `aria-current`, a horizontal
scroller on narrow screens; retention table with caption and row headers (stacked on mobile); reduced motion stops
the spinner.

## Preview

```sh
python3 -m http.server 8794 --bind 127.0.0.1 --directory docs/design/sessions/discovery-account/gk-016-r02
```

Open <http://127.0.0.1:8794/poc/states.html> (index) or e.g. <http://127.0.0.1:8794/poc/account.html?state=home>,
<http://127.0.0.1:8794/poc/signin.html>, <http://127.0.0.1:8794/poc/claim.html?state=link-offer>. Screenshots: with
that server running, `sh docs/design/sessions/discovery-account/gk-016-r02/shoot.sh [filter]` (headless Chrome;
`shot.html` frames each page at exactly 1440 or 390 px and reports the full height; JPEG quality 62).

## Checks

- `node --check poc/app.js poc/r02.js` passes.
- Headless Chrome DOM check of all 55 listed views: each renders exactly one `h1`, every r02 state shows the
  review switcher, no duplicate IDs, no horizontal overflow at 390 px (`scrollWidth` 390, no element past the edge
  outside the scrolling account nav). Temporary harness, not committed.
- Interaction check (temporary harness): empty email → "Enter your email address" with focus; valid email → `sent`;
  Google → `callback` → `created-google`; older-order form empty → two errors, focus on the reference; filled →
  `old-pending`; delete with an upcoming order → two tick errors, then `deleted`; merge → status; invalid phone →
  inline error with focus; "Add this order to my account" → `link-asked`.
- No external URLs in `poc/` (only the SVG namespace inside a CSS data URI); no Google, font or analytics requests.
- Screenshots viewed for sign-in, created, account home (desktop and mobile), order confirmed, link offer, older-order
  errors, profile conflict (mobile), delete review/blocked; fixes made: full-width sign-in button, pill buttons
  without capitals, delete-button size, neutral bullets for "We delete", optional labels on one line, valid list
  markup in the blocked notice.
- `python3 scripts/check_planning_kit.py` passes (run before commit).
- No independent review and no Creator feedback yet.

## Deviations and decisions to note

1. **Claim from the email link waits for the chef.** The brief lists "Add this order to my account, success";
   K00 requires chef approval for every claim, so the sketch shows *asked → waiting for the chef → added*. If the
   Creator wants the private link alone to be enough, `link-asked` collapses into `link-added` (question 1).
2. **"Already in another account" uses the generic dead-link screen.** Approving a claim revokes the guest link
   (K00), so a second person's link simply stops working; showing "in another account" would reveal something.
3. **Customer sign-in shows email link + Google only.** The live page also has email + password (used by staff).
   The sketch leaves password sign-in off the customer page (question 5).
4. **Delete account: block vs warn.** Blocked while money is unsettled (payment being checked, paid awaiting a
   date, refund in progress); allowed with a warning and an extra tick for a confirmed upcoming order (question 2).
5. **Return after the email link.** Live returns only to `/account` or `/admin` and keeps a guest order capability
   only in the ordering tab, so the guest path says "sign in, then open the link in your email again" rather than
   promising to return to the order.
6. **Order page reuse.** The account opens a compact order view with the built titles; GK-017 r02 owns the full
   order-page states (refund tracker, timeline, cancellation preview). Change time / Preview cancellation link to
   the r01b order page previews.
7. Example dates are fixed (Nov–Dec 2026) except "Order again" (computed by r01b) and "last signed in today".

## Gaps (outside this sketch)

- B1 needs live changes: `shouldCreateUser: true` (account on first use), the Google provider and OAuth client
  (Creator task already in SETUP.md on main `be27b4d`; GK-021 records it), and a callback for Google errors and
  cancel.
- The "private link in your order email" (guest capability in an email link) doesn't exist yet: live keeps the
  capability in the ordering tab only. Claims, the chef's approval queue, and chef-initiated association need
  API and admin design (the chef side is not in this sketch).
- API names in the "Simulates" lines (`/me/orders`, `/me/custom-requests`, `/me/favourites`, `/me/profile`) are
  assumptions for review, not contracts.
- Legal retention for payment records is pending GK-037; deletion wording says "being confirmed".
- "Sign out everywhere" timing depends on the session/JWT settings GK-021 qualifies.
- The chef-offer review page (request with an offer) belongs to GK-017/GK-018.

## Open questions for the Creator

1. Adding an order from its private email link while signed in: should the chef still approve it (as sketched,
   following the contract), or should the link alone add it straight away?
2. Deleting an account with unfinished orders: is it right to **block** while a payment or refund is unsettled and
   to **allow with a warning** when there is a confirmed upcoming order? Or block in both cases?
3. Should deleting the account ask for a fresh sign-in (a new email link or Google) just before confirming?
4. Google and email link with the same address: one account (as sketched), and may we fill in the name from Google?
5. Password sign-in: keep it only for staff (separate from the customer page), or offer it to customers too?
6. What reference does the chef give on Instagram/offline orders (the sketch uses an example "IG-0613")? Is
   "reference + how you ordered + roughly when + a note" the right proof to ask for?
7. Older orders added by the chef: show the amount she recorded ("£180 · recorded by the chef"), or no amount?
8. Should the account list custom requests with a chef offer and its 24-hour deadline (as sketched), and is "Ready
   to send" a clear label for a request we can't see being sent?
9. Inactivity: one warning email 30 days before deletion (as sketched), or also a reminder closer to the date?
10. Profile: are name, phone and Instagram name (all optional) the right fields, with phone used only when the chef
    needs to reach the customer?
