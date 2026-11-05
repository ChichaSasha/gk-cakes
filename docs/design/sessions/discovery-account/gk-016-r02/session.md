# GK-016 r02b — customer account: sign-in, orders, requests, adding older orders, profile and privacy (Creator-approved 2026-11-05)

2026-11-05. Card GK-016 (Owner: Agent, Kind: Design). r02 covers the **customer account** that r01/r01b left for
later: sign-in and sign-up in one step (email link that creates the account on first use, plus "Continue with
Google"), the account home (orders with clear states, requests, favourites), the entry into the order page, both
ways of adding an older order, profile and contact details, and privacy (retention, the inactivity warning,
deleting the account, signing out everywhere).

**r02b — Creator-approved 2026-11-05 (answers as suggested).** The Creator approved r02 (and r01b) with the open
questions answered as suggested, plus three changes from the Creator-facing review; r02b applies them (see
[r02b — Creator approval](#r02b--creator-approval-2026-11-05)). The branch now includes r01c (`52fa680`, merged), and
the r01c copy changes (8-month requests, chef's collect-by date) are also in this folder's `poc/app.js`. The fresh
integration review (AC3) is still to come.

**Review round 1** (independent design reviewer, PR #31, REVISE on `ad72346`) is addressed in this revision; see
[Review round 1 changes](#review-round-1-changes).

## Execution check

Role: GK-016 design worker, bounded r02 sketch. Card policy requests `gpt-5.6-sol` / high in a fresh scoped worker.
The host reports **`llmplatform/claude-opus-5-5`** (system environment) running as a **fresh subagent** with only the
master's brief and the inputs below — status **mismatch; the current user-selected model takes precedence**. This
worker is the author, so this is not an independent review. Writes: this folder only, worktree branch
`sasha-chycha/gk-016-r02-design` from the r01b head `122e9f9`. `api/`, `ui/`, r01 files and other worktrees were not
touched. No corporate tools (Glean, Slack, Jira, Grafana, Snoocode GitHub tools), no Trello, no PR.
Round 1 fixes: the same author subagent and model, working from the reviewer's findings (relayed by the planning
master, not a file) and the same inputs; no corporate tools.

Readback: B1 — the email sign-in link creates the account on first use; Google sign-in is also offered. B2 — older
orders are added either (a) by a signed-in customer opening the order's private email link and asking to add it,
or (c) for old Instagram/offline orders by the chef's reference plus a note, approved by the chef; short proof →
decline with "contact us" and reveal nothing; the chef may also add orders on request. K00: no automatic
association by email/payer/Instagram; claims need the authenticated customer and chef approval; a pending claim
reveals no order details; generic 404 for absent/foreign proof; guest links are revoked when a claim is approved.
K09: account/profile/favourites/basic history kept until 24 months without sign-in or new order, then a warning
and 30 days; order details/messages 2 years after fulfilment/cancellation; deletion revokes capabilities and
preserves accounting facts through anonymisation. K10: no analytics on account routes. B3–B7 are r01b matters and are
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

r02b: `poc/r02.js` `7e45f8a9f0ad1f97`, `poc/app.js` `87a8ac0d4113db16` (with the r01c copy), `poc/styles.css` `c8ae7778e9feb446`, `shoot.sh` `7933017bac73511b`.
Round 1: `poc/r02.js` `b1505f3606e3233f` (new; r02 first version `fe70ecdbf5744d88`), `poc/app.js` `948c856464ddbbd4`,
`poc/styles.css` `7ed57f5e0c69a1cd` (was `7cdf8c4091f6c04c`), `shoot.sh` `15303a7aecdba3d8` (was `17c83540ad45e955`), `shot.html` `83c8d38d8a032110`; new pages `signin.html`, `claim.html`,
`profile.html`, `privacy.html`, `states.html`; `account.html` and `order.html` now also load `r02.js`. All other
`poc/` files and `poc/assets/` are byte-identical to r01b/r01c. `screens/` holds 124 JPEGs (62 views × desktop 1440 /
mobile 390, about  21MB), all re-shot for r02b (state numbers shifted with the three new delete steps).

## How the prototype is organised

- `poc/` is a copy of r01b. `r02.js` holds every account state and its example data (`FX`, `ORDERS`); `app.js`
  changed only to merge `window.R02.pages()` into its router and call `window.R02.bind()`, use storage keys
  `gk-poc-gk016r02-`, label the review bar "GK-016 r02", drop the old r01 "signed in" demo from the account page,
  and show the order reference on "Order again". An r02 section is appended to `styles.css` (`acc-*` classes).
- Each state is `page.html?state=…`. Every r02 page opens with the **design-review switcher** (striped yellow,
  dashed border, mono type, "Prototype only · design review · not part of the shop"): state picker grouped by
  area, previous/next through all 59 states, "All states" (`states.html`), and a "Simulates" line with the API or
  provider event. Prototype-only hints inside pages use the same dashed yellow "Prototype" label. Neither is part
  of the shop design.
- **Money**: every figure is an example server value (£90, £105, £285, £180 recorded by the chef, refund £45 of
  £90, £90 returned). The page never adds, halves or rounds money.
- **Google button**: a static local SVG "G" and the label "Continue with Google"; no Google script, font or
  request. A prototype note says so; the live button must follow Google's sign-in branding.
- **Account order list mapping** from the API order snapshot to the list chip, line and action is in
  [Order list mapping](#order-list-mapping).
- **No analytics** and no network requests other than the local files. `?shot=1` hides the footer for screenshots
  and stops automatic focus moves/redirects.

## States — what each shows, buttons and destinations

| # | State (`?state=`) | What the customer sees | Buttons → destination |
| --- | --- | --- | --- |
| 1 | `signin` | "Sign in or create an account" (one step). Email field, **Email me a sign-in link**, "New here? The link creates your account the first time you open it", or, **Continue with Google**. Under the buttons: "We use your email address to sign you in and send emails about your orders. With Google, we get your name, email address and profile picture. Privacy notice →". Side card: what an account gives; "You can always order without an account"; "No analytics run on sign-in or account pages". | Email me a sign-in link → client check, then `sent`; Continue with Google → `callback` (Google) → `created-google`; Privacy notice → policies preview |
| 2 | `signin-errors` | Error summary + inline "Enter an email address like name@example.com" (empty: "Enter your email address"), focus on the field. Nothing is sent. | Same as 1 |
| 3 | `sent` | "Link sent to anna@example.com": open it in **this browser** within 30 minutes, works once; first time creates the account; check spam; new link in a minute. Same answer for every address and when rate-limited (never reveals whether an account exists). | Send a new link (countdown 1:00, matching "in a minute", then "Sent again… use the newest link"); Use a different email → `signin`; prototype hints: first time / returning / after 30 minutes |
| 4 | `callback` | "Signing you in…" spinner, keep this tab open. | Goes on by itself → `created`, `created-google` or account home |
| 5 | `created` | "Welcome to GK Cake London · Account created": created for anna@example.com when the link was opened; **"We never add orders to your account automatically, even ones made with this email address. You choose which to add."** Next: favourites on this device (Add to my account / Keep on this device only), Add an older order, Profile & contact. | Go to your account → empty account; merge buttons → inline status |
| 6 | `created-google` | As 5, "with your Google account… Google shares your name, email address and profile picture with us; we use your name and email address"; name prefilled from Google. | As 5 |
| 7 | `link-failed` | "This sign-in link didn't work": expired (30 minutes), used, or another browser. Get a new link form + Google. | Email me a new link → `sent` |
| 8 | `google-cancelled` | "Google sign-in was cancelled, so nothing changed. Try again, or use an email link." | As 1 |
| 9 | `google-error` | "We couldn't sign you in with Google. Please try again, or use an email link instead." | As 1 |
| 10 | `unavailable` | "Sign-in is not available right now. Please try again in a few minutes. You can still order without an account." | Try again |
| 11 | `session-ended` | "Your session has ended. Please sign in again." (built wording) | As 1 |
| 12 | `signed-out` | "You're signed out on this device." Favourites in this browser stay; hint about Sign out everywhere. | Sign in again; Back to the shop |
| 13 | `home` | Account nav (Orders & requests · Favourites · Add an older order · Profile & contact · Privacy & your data · Sign out). **Your orders** (see [Order list mapping](#order-list-mapping)): *In progress* — GK-1123 "Waiting for payment" (Continue to payment), GK-1108 "Still checking payment" (don't pay again), GK-1096 "Payment received · date to agree" (date shown as requested, not confirmed), GK-1079 "Being prepared", GK-1071 "Ready to collect", GK-1087 "Confirmed", GK-1115 "Status not available", GK-1061 "Cancelled · refund pending" (£45 pending); *Past* (behind **Show past orders (3)**, `aria-expanded`; open in `chef-added`) — GK-1042 "Cancelled · refunded" (£90 returned), GK-1009 "Collected", GK-0931 "Collected · added by the chef" (£180 recorded by the chef). Coloured chip with a dot plus words (never colour alone). **Your requests**: GKR-7Q4M "Ready to send" (we can't see whether it was sent; copy again, same request), GKR-3HDX "Offer from the chef" (review and pay by a 24-hour example deadline), GKR-9WPL "Offer expired", GK-1120 "Add an order to your account · from its email link · Waiting for the chef" (only the reference the customer already has, no new details), GKC-28 older-order request "Waiting for the chef"; requests kept 6 months after the last conversation. **Your favourites**: "2 favourites on this device aren't in your account yet" merge choice + account favourites. "Ordered before?" card. | Show past orders / Hide past orders → toggles the past list; View order → order detail state; Continue to payment → `order-unpaid`; Order again → `reorder.html?order=GK-1009`; Copy request & open Instagram/email → status (same request, nothing sent); Review the offer → status (offer page belongs to GK-017/GK-018); Ask the chef for a new one → contact; Add to my account / Keep on this device only → status; View all favourites → favourites (signed in); Add an older order → `old-form` |
| 14 | `empty` | New account: "No orders yet" + Shop cakes / Create your own + "Add an older order"; no requests; nothing saved. | Shop cakes; Create your own; Add an older order |
| 15 | `kept` | Banner: "Welcome back. Your account was due to be deleted on Sat 5 December 2026 because it hadn't been used for 2 years. Signing in has kept it… at least another 24 months". | As 13 |
| 16 | `chef-added` | Banner: "The chef added an order to your account, as you asked: GK-0931…"; that row highlighted "New". | View order GK-0931 → `order-older` |
| 17 | `load-error` | Orders card: "We couldn't load your orders right now. Please try again." Requests and favourites still load. | Try again → `home` |
| 18 | `disabled` | "This account is disabled. Please contact us if you think this is a mistake." (built wording), no account data. | Contact us; Sign out |
| 19 | `signed-out-account` | Opening the account without a session: "Please sign in to see your account. After signing in you come back here." + sign-in form. | As 1 |
| 20–30 | `order-unpaid`, `order-checking`, `order-agreement`, `order-confirmed`, `order-making`, `order-ready`, `order-refund`, `order-refunded`, `order-completed`, `order-unknown`, `order-older` | The order page layout (status card + "Order details" summary) with "← Your orders". Titles and lines follow the built order page and GK-017 r02: "Your order is waiting for payment" (place held 15 minutes; Continue to secure payment); "Still checking payment; do not pay again"; "Payment received" (requested date/time "not confirmed yet", never moved without agreement, everything back (£105) if no date is agreed); "Your order is confirmed" (own courier note, full-refund deadline example 10:00 Sun 22 Nov, 50% after); "Your cake is being prepared" (collection details will show here about 48 h before; being prepared doesn't reduce the refund, 50% (£45) after the deadline); "Your cake is ready" (collect today 14:00–15:00); "This order was cancelled" + "Refund of £45 pending…" + why £45 (50% inside 120 hours); refund of £90 returned; "Your order is complete"; "We can't show this order's status right now" (no actions); older order "added by the chef… paid outside this website… nothing to pay or refund here" with chef-recorded values. | Continue to secure payment → toast (GK-025 opens Stripe); Contact us / Contact the chef; Change your time / Preview cancellation → r01b order page previews; Order again → reorder; Request something similar → custom request; Shop cakes |
| 31 | `link-guest` | Guest order page from the private email link, not signed in. Card "Keep this order in your account": sign in, then open the email link again and choose "Add this order to my account"; the chef checks it; "We never add orders to an account automatically, even when the email address matches." | Sign in or create an account → `signin` |
| 32 | `link-offer` | Same order while signed in: "Add this order to your account? …Ask to add it to anna@example.com and the chef will check it… Until she has, keep using the link in your email." Wrong account → Sign out. | **Add this order to my account** (busy label "Asking…") → `link-asked`; Not now → account |
| 33 | `link-asked` | "Asked. The chef will check it and, if it matches, add it to your account. You'll see the result here and under “Your requests”. Until then, keep using the link in your email." Also listed under "Your requests". | Waiting for the chef (disabled); Back to your account; prototype outcomes → `link-added` / `link-declined` |
| 34 | `link-added` | "GK-1120 is now in your account… The private link in your email no longer works for this order: sign in to see it." | View in your account |
| 35 | `link-yours` | "This order is already in your account." Reachable when the owning account (signed in) opens a guest link that was revoked when its own claim was approved: the API recognises the owner and says so instead of the generic dead link. Anyone else gets `link-dead`. | View in your account |
| 36 | `link-declined` | "We couldn't add this order. The chef couldn't add the order you asked about to your account, so nothing was added. Please contact us… If the link in your order email still works, you can keep using it to see that order." No reason and no order details. | Contact us; Back to your account |
| 37 | `link-dead` | "This order link doesn't work": may have been replaced, or already added to an account; contact us with the reference from the email. **No order details, no reference, no hint which account** — also covers "already in another account". | Contact us; Go to your account |
| 38 | `old-form` | "Add an older order": for Instagram/email/in-person orders. Fields: Order reference from the chef (required, example IG-0613), How did you order (Instagram/Email/In person/Other), When was the cake for (optional month/year), A short note for the chef (required, ≤500; no card details or health information). Side: how it works (3 steps; no details shown until the chef adds it), guest orders use the email link, or ask the chef to add it. | Send to the chef → `old-pending` (after client checks); Cancel → account |
| 39 | `old-errors` | Error summary + inline errors for reference and note; focus to the first. | As 38 |
| 40 | `old-pending` | "Your request GKC-28 is waiting for the chef. Nothing has been added… we won't show any order details until she has checked it. You'll see the result here and under “Your requests”." What you sent (reference, how, when, note). | Back to your account; Withdraw this request → status |
| 41 | `old-approved` | "The chef matched your request GKC-28 and added order GK-0931 (Instagram order, Sat 13 June 2026)". | View order GK-0931 → `order-older`; Back to your account → `chef-added` |
| 42 | `old-declined` | "We couldn't add this order. The chef couldn't match request GKC-28 from the details given, so nothing was added. Please contact us…" Reveals nothing about whether an order exists. | Contact us; Back to your account |
| 43 | `profile` | **Your details**: Full name (optional; prefilled at checkout, editable per order), Phone (optional; only if the chef needs to reach you); name filled in from Google if used, editable, Instagram name (optional, private, "doesn't prove who you are and isn't used to sign in"). **Signing in**: email (sign-in links and order emails), Email link on, Google connected, "One account per verified email address"; Change sign-in email (disclosure). **Emails we send**: sign-in links from the sign-in service; order and account emails can't be switched off once we send them; no marketing. | Save changes → `profile-saved` (or inline errors); Send a confirmation link → `email-change-sent` |
| 44 | `profile-saved` | "Saved." | As 43 |
| 45 | `profile-errors` | Error summary; phone "Enter a phone number like 07700 900123 or +44 7700 900123"; Instagram "letters, numbers, full stops and underscores". Errors are tied to the inputs (`aria-invalid`, hint + error in `aria-describedby`), also when shown live. | As 43 |
| 46 | `profile-conflict` | "These details were changed somewhere else (perhaps in another tab)… loaded the latest… kept your phone number change. Check them and save again." The kept edit is outlined, with the saved value beside it; that note is in the phone input's `aria-describedby`. | Save changes |
| 47 | `email-change-sent` | Confirmation link sent to the new address; changes when opened (30 minutes, this browser); keep using the old address until then; the old address is told. | — |
| 48 | `privacy` | **Your data and how long we keep it** (active account, last sign-in today; table: account/profile/favourites/order list — 24 months in which you don't sign in or order → warning email 30 days before deletion and a reminder 7 days before; details of your orders and messages (name, email, cake, date) until an unfinished order is done, then 2 years after handover/cancellation, even after account deletion (then unlinked); requests 6 months after last conversation; request photos 30 days after handover or 3 months with specific permission; payment records for the legal period, being confirmed; on this device favourites 1 year, draft 30 days without description/photos); "No analytics run on account pages". **Sign out everywhere**, **Delete your account**, **This device** (clear). | See the warning email → `warning-email`; Sign out everywhere… → `signout-all`; Delete my account… → `delete-review`; Clear this device → status |
| 49 | `warning-email` | Two example plain emails, "with no tracking; the button opens the normal sign-in page": (1) warning 30 days before, subject "Your GK Cake London account will be deleted on Sat 5 December 2026"; (2) reminder 7 days before, subject "Reminder: your GK Cake London account will be deleted on Sat 5 December 2026". Both: 2 years without sign-in or order; sign in to keep it; if nothing is done: account, profile and favourites deleted and orders no longer linked to an account; order details kept until 2 years after handover or cancellation, payment records for as long as the law requires; orders in progress carry on. | Sign in to keep my account → `signin`; prototype note → `kept` |
| 50 | `delete-review` | **We delete**: account and sign-in (email link and Google), profile (name/phone/Instagram), account favourites, requests and their photos, the link between your orders and this account and the private order links. **We keep for your orders**: details of your orders (name, email, cake, date) — unfinished orders until they're done, then 2 years after handover or cancellation, no longer linked to an account; anything for an open dispute until resolved. **We keep because the law requires**: payment and refund records for the legal period (being confirmed), with as little personal information as possible. Messages also stay in Instagram/email. Backups cleared within 30 days; favourites in this browser stay until cleared. Tick "I understand that deleting my account can't be undone." (`aria-invalid` when missing). Footnote: "Your earlier orders and favourites won't be in the new account." "Next, we ask you to sign in again (email link or Google) before anything is deleted." | Continue: confirm it's you → `delete-reauth` (needs the tick; inline error otherwise); Keep my account → `privacy` |
| 51 | `delete-blocked` | "You can't delete your account yet. These orders still have money to settle: GK-1108 still checking a payment; GK-1096 payment received, date to agree; GK-1061 refund of £45 on its way." Contact line. Delete button disabled. | Order links → order detail; Back to your account |
| 52 | `delete-warn` | "You have an upcoming order: GK-1087, Fri 27 November 2026, confirmed. If you delete your account now, the order still goes ahead, so we keep the name and email it needs. You won't be able to see, change or cancel it online; the chef will contact you about it." Extra required tick ("…I'll arrange anything about it with the chef directly"). | Continue: confirm it's you → `delete-reauth` (both ticks); Keep my account |
| 53 | `delete-reauth` | "Confirm it's you": sign in again now; **nothing is deleted yet**; Email me a confirmation link, or Continue with Google (static); the link goes to anna@example.com, works once, in this browser, for 30 minutes. | Email me a confirmation link ("Sending…") → `delete-reauth-sent`; Continue with Google → `delete-final`; Keep my account |
| 54 | `delete-reauth-sent` | "We've sent a confirmation link… open it in this browser within 30 minutes; it brings you back here to finish. Nothing is deleted yet. If you didn't mean to delete your account, just ignore the email." | Keep my account; prototype: opened → `delete-final`, after 30 minutes → `link-failed` |
| 55 | `delete-final` | "You've signed in again. You can now delete your account." Short summary of what is deleted and kept; "This can't be undone."; confirmation open for 10 minutes (example), then sign in again. | **Delete my account** (danger, "Deleting…") → `deleted`; Keep my account |
| 56 | `deleted` | "We've signed you out and deleted your account, profile, favourites and requests. Details of your orders are kept, no longer linked to an account, until 2 years after handover or cancellation (unfinished orders until they're done); payment records for as long as the law requires… backups within 30 days." (No confirmation-email promise until order/account emails exist, GK-031.) | Clear this device; Back to the shop |
| 57 | `signout-all` | "Sign out everywhere? …this device and every other one. Orders and favourites aren't affected." | Sign out everywhere (busy label "Signing out…") → `signout-all-done`; Cancel |
| 58 | `signout-all-done` | "You're signed out on all your devices. Other devices are asked to sign in again when they next check, usually within an hour (example; GK-021 confirms the timing)." Never claims instant revocation. | Sign in again |
| 59 | `signout-all-partial` | Built wording: "You are signed out on this device, but the sign-in service could not be reached to end the session everywhere." | Sign in again |

Also linked: `favourites.html?demo=signed-in` (r01b merge panel) and `reorder.html?order=GK-1009` (r01b order again).

Accessibility patterns reused from r01b/GK-017: one `h1`; skip link; native inputs with labels, hints and errors
tied by `aria-describedby`; error summaries with links (`role="alert"`, focused); status changes in polite live
regions and focus moved to the new status on arrival; status chips use words plus a dot (not colour alone; chip text
measured 7.6:1 green, 8.8:1 amber, 8.1:1 red, 9.2:1 grey on their tints); visible focus ring; ≥ 44 px targets in
the shop design (the prototype-only review switcher uses 40 px controls); account nav as a labelled `nav` with `aria-current`, a horizontal
scroller on narrow screens; retention table with caption and row headers (stacked on mobile); reduced motion stops
the spinner.

## Order list mapping

How the account list turns the owner-only order snapshot (`GET /me/orders`, K02) into a chip, one line and one
action. Wording follows `ui/src/lib/checkout/order.ts` on origin/main (built order page) so the list and the order
page never disagree; unknown values enable no action (K02). Group: **In progress** while anything (payment, date,
cake, refund) is still open; **Past** once finished. ✎ = drawn in the prototype list.

| Lifecycle · payment / reason / refund (API) | Chip (tone) | Line in the list | List action | Group |
| --- | --- | --- | --- | --- |
| `awaiting_payment` · no payment yet ✎ | Waiting for payment (amber) | Your place is held for 15 minutes while you pay. Nothing has been paid yet. | **Continue to payment** (primary) → order page | In progress |
| `awaiting_payment` · attempt `failed`/`cancelled` | Payment didn't go through (amber) | No payment was taken for that attempt. You can try again while your place is still held. | **Try paying again** → order page | In progress |
| `awaiting_payment` · payment page open (`checkout_url`, not returned) | Waiting for payment (amber) | Your secure payment page is ready. Your place is held while you pay. | **Continue to payment** → order page | In progress |
| `awaiting_payment` · `created`/`pending`/`unknown` ✎ | Still checking payment (amber) | We're waiting for your payment result. Please don't pay again. | View order | In progress |
| `awaiting_payment` · `succeeded`, or `confirmation_pending` (paid) | Payment received · confirming (grey) | We've received your payment and are confirming your order. | View order | In progress |
| `awaiting_payment` · free, not confirmed / refused `order_changed` | Confirm your order / Couldn't confirm yet (amber) | Your order costs nothing to pay. Confirm it to book your date. / Something changed; you can try again. | **Confirm order** / **Try again** → order page | In progress |
| `awaiting_payment` · free refused `window_closed`/`holds_released`; `confirmation_pending` (free) | Couldn't confirm (red) / Confirming (grey) | Please start a new order. / We're confirming your free order. | View order | In progress |
| `paid_awaiting_agreement` ✎ | Payment received · date to agree (amber) | Your payment arrived after the preparation time for this date. The chef will contact you about the date. (date shown "requested, not confirmed") | View order | In progress |
| `confirmed` ✎ | Confirmed (green) | Collect at St Katharine Docks / Your courier collects / GK delivery confirmed by the chef. | View order | In progress |
| `in_production` ✎ | Being prepared (green) | Your cake is being prepared. The collection details will show here about 48 hours before. | View order | In progress |
| `ready` ✎ | Ready to collect (green); GK delivery: Ready for delivery | Your cake is ready. Collect it at St Katharine Docks at your time. | View order | In progress |
| `completed` ✎ | Collected (grey); Delivered / Handed to your courier | Thank you for ordering with GK Cake London. | View order · Order again | Past |
| `cancelled` · payment `created`/`pending`/`unknown` | Still checking payment (amber) | This order was cancelled, but we're still checking a payment for it. Please don't pay again. | View order | In progress |
| `cancelled` · `payment_not_completed` | Cancelled · not paid (grey) | The payment wasn't completed in time, so your place was released. No payment was taken. | View order | Past |
| `cancelled` · `free_not_confirmed` | Cancelled · not confirmed (grey) | This free order wasn't confirmed in time, so it was cancelled. | View order | Past |
| `cancelled` · payment `succeeded`, no refund block | Cancelled · payment recorded (amber) | We have a payment recorded for this order. Please don't pay again; we'll contact you. | View order | In progress |
| `cancelled` · refund `requested` / `pending` ✎ / `partly_returned` / `needs_attention` | Cancelled · refund requested / pending / partly returned / being checked (red) | The built refund line, e.g. "Refund of £45 pending: we've asked our payment provider to return it." | View order | In progress |
| `cancelled` · refund `returned` ✎ | Cancelled · refunded (grey) | Refund of £90 returned to your payment method. | View order | Past |
| Any other lifecycle with a refund block (e.g. a separately paid delivery refunded) | Lifecycle chip as above | Refund line as a second line under the chip | View order | In progress until returned |
| Unknown lifecycle or payment value ✎ | Status not available (grey) | We can't show this order's status right now. Please refresh later. | View order (nothing else) | In progress |
| Historical order added by the chef ✎ | Collected · added by the chef (grey) | An Instagram order the chef added to your account. Paid outside this website. | View order | Past |

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
- Round 1 re-check (same temporary harnesses): all 59 views render one `h1`, no duplicate IDs, no overflow at 390 px,
  every r02 state has the switcher; plus: unticked delete boxes get `aria-invalid` and lose it when ticked; a live
  profile error sets `aria-describedby` to hint + error; the conflict note is in the phone input's description;
  busy labels "Signing out…" and "Asking…"; "Continue to secure payment" shows a prototype toast; resend shows 1:00.
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
- Independent review round 1 (REVISE) addressed below; no Creator feedback yet.

## Deviations and decisions to note

1. **Claim from the email link waits for the chef** (K00; approved, answer 1): *asked → waiting for the chef →
   added / couldn't add*.
2. **"Already in another account" uses the generic dead-link screen.** Approving a claim revokes the guest link
   (K00), so a second person's link simply stops working; showing "in another account" would reveal something.
3. **Customer sign-in shows email link + Google only.** The live page also has email + password (used by staff).
   Password sign-in stays staff-only and is not on the customer page (approved, answer 5).
4. **Delete account: block vs warn.** Blocked while money is unsettled (payment being checked, paid awaiting a
   date, refund in progress); allowed with a warning and an extra tick for a confirmed upcoming order (approved, answer 2), then a fresh sign-in
   (answer 3).
5. **Return after the email link.** Live returns only to `/account` or `/admin` and keeps a guest order capability
   only in the ordering tab, so the guest path says "sign in, then open the link in your email again" rather than
   promising to return to the order.
6. **Order page reuse.** The account opens a compact order view with the built titles; GK-017 r02 owns the full
   order-page states (refund tracker, timeline, cancellation preview). Change time / Preview cancellation link to
   the r01b order page previews.
7. **"Being prepared"** replaces "being made" in the account list and order entry (Creator-facing review). The built
   order page (`order.ts`, "Your cake is being made") and GK-017 r02 still say "being made"; aligning them is a
   follow-up for GK-025/GK-017.
8. Example dates are fixed (Nov–Dec 2026) except "Order again" (computed by r01b) and "last signed in today".

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
- Email change (`email-change-sent`) needs GK-021 rules: the response must be identical whether or not the new
  address already has an account (no account discovery); the change completes only from the new address; the old
  address is told; what happens to a linked Google identity after the email changes (keep it linked, or require
  Google to be reconnected when its email no longer matches) must be decided and tested.
- The inactivity warning and reminder emails, and any order/account email, depend on email sending (GK-031); the
  prototype makes no "we'll email you" promise elsewhere.
- The fresh sign-in before deletion needs a recent-authentication check in GK-021/API (e.g. session issued within
  10 minutes; example value) and the same email-link rules as sign-in.
- A declined email-link claim (`link-declined`) leaves the guest link as it was; whether repeated requests are
  rate-limited is an API decision.

## Review round 1 changes

Independent design review, PR #31, REVISE on `ad72346`:

| Finding | Change |
| --- | --- |
| M1 deletion/expiry copy | Retention row "Details of your orders and messages (name, email, cake, date)": until an unfinished order is done, then 2 years after handover or cancellation, even after deletion (unlinked). Delete review splits "We delete" / "We keep for your orders" (order details, open disputes) / "We keep because the law requires" (payment records); "the link between your orders and this account" replaces "your order list"; footnote "Your earlier orders and favourites won't be in the new account"; delete-warn keeps "the name and email it needs"; deleted and the warning email say the same. New question 11. |
| M2 path (a) claim | Pending: "will check it and, if it matches, add it… We'll email you either way". New `link-declined` (nothing added, contact us, a still-valid guest link keeps working, no reason or details). The pending email-link claim appears under "Your requests" with only its reference. |
| M3 order list | New [Order list mapping](#order-list-mapping) table from `order.ts`; list now draws unpaid (Continue to payment), being made and ready, with matching order detail states. |
| Google wording | "Google shares your name, email address and profile picture with us; we use your name and email address." |
| `link-yours` reachability | Explained in its Simulates line and the states table. |
| Email-change rules | Recorded in Gaps. |
| Privacy notice on sign-in | Note and "Privacy notice →" link under the sign-in buttons (email and Google). |
| Profile/delete a11y | Live errors update `aria-describedby` (hint + error); conflict note tied to the phone input; `aria-invalid` on unticked delete boxes. |
| Contrast claim | Chip text darkened (green `#165a2e`, amber `#653d00`): measured 7.6 / 8.8 / 8.1 / 9.2:1. |
| 44 px claim | Qualified: the prototype-only switcher uses 40 px controls. |
| Busy labels | "Signing out…" (sign out everywhere), "Asking…" (add this order); sign-in and forms keep "Sending…". |
| Nits | "sign in or order" in the retention row; resend countdown 1:00 matches "in a minute"; warning email "with no tracking; the button opens the normal sign-in page" (button now goes to sign-in); "usually within an hour". |

Screenshots were all re-shot (state numbers shifted with the four new states).

## r02b — Creator approval (2026-11-05)

Checks (r02b, temporary harnesses): `node --check` on both scripts; all 62 views render one `h1`, no duplicate IDs,
no element past the right edge at 390 px, and no "We'll email" text; Show past orders toggles (`aria-expanded`), open
in `chef-added`; delete review → `delete-reauth` → `delete-reauth-sent`; Google → `delete-final` → "Deleting…" →
`deleted`; request calendar reads "Thursday to Sunday, up to 8 months ahead". `check_planning_kit.py` passes.

The Creator approved GK-016 r01b and r02 on 2026-11-05 "as suggested" (relayed by the planning master in this
session; not a separate file). Applied by the same author worker (`llmplatform/claude-opus-5-5`), no corporate
tools, after merging r01c (`52fa680`) into this branch.

| # | Question | Creator answer | Where applied |
| --- | --- | --- | --- |
| 1 | Email-link claim: chef approval? | Keep chef approval (K00). | `link-offer`/`link-asked`/`link-added`/`link-declined` unchanged. |
| 2 | Deleting with unfinished orders | Block while money is unsettled; allow with a warning for a confirmed upcoming order. | `delete-blocked`, `delete-warn` unchanged. |
| 3 | Fresh sign-in before deleting? | Yes: a new email link or Google just before confirming. | New `delete-reauth`, `delete-reauth-sent`, `delete-final`; delete review/warn now "Continue: confirm it's you"; the destructive button moved to `delete-final`. |
| 4 | Google + email link, Google name | One account per verified email; name prefilled from Google, editable. | `created-google` "(you can change it)"; profile name hint; Signing in card "One account per verified email address…". |
| 5 | Password sign-in | Staff-only, not on the customer page. | Customer sign-in unchanged (email link + Google); deviation 3. |
| 6 | Older-order proof | Keep "reference + how you ordered + roughly when + a note". | `old-form` unchanged. |
| 7 | Amount on chef-added orders | Show it, labelled "recorded by the chef". | Account list and `order-older` unchanged. |
| 8 | Chef offers and "Ready to send" | Keep both (24-hour deadline). | Requests card unchanged. |
| 9 | Inactivity emails | One warning 30 days before deletion plus one reminder 7 days before. | Retention row; `warning-email` shows both emails. |
| 10 | Profile fields | Name, phone, Instagram name, all optional. | `profile` unchanged. |
| 11 | Order details after deletion | Keep 2 years after handover/cancellation, unlinked (K09). | Delete lists, `deleted`, retention row unchanged. |

Creator-facing review changes:

- **Past orders** on the account home are behind a **Show past orders (3)** button (`aria-expanded`, toggles the
  list; open in `chef-added` so the new order is visible) to shorten the mobile page.
- **"Your cake is being prepared"** replaces "The chef is making your cake" (list chip "Being prepared", line, order
  entry title and refund note).
- **No "We'll email you…" promises** until order emails exist (GK-031): link claim pending "You'll see the result
  here and under “Your requests”"; older-order pending and how-it-works the same; being prepared "The collection
  details will show here about 48 hours before"; deleted no longer mentions a confirmation email; delete-warn "the
  chef will contact you about it"; profile "Emails we send" reworded. The approved inactivity emails remain as
  designs (gap: GK-031).

r01c copy carried into `poc/app.js`: request calendar "up to 8 months ahead", non-wedding ✱ after 2 months, wedding
hint, date notes, ready-made "collect by …" without "(example)", "the chef's collect-by date", the usual 192-hour
notice unless the chef approved shorter notice; "About this prototype" says r01c.

## Open questions for the Creator

None open: questions 1–11 were answered on 2026-11-05 (see r02b above).
