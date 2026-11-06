# GK-018 r04-claims — chef side of adding orders to customer accounts

Status: **Proposal — not approved.** A design worker drafted this on 2026-11-06 while the Creator (Sasha) was
away. Nobody has reviewed it, and the Creator hasn't seen it. It is a coloured, clickable proposal sketch for her
review, not an accepted design. No implementation ticket may treat it as approved. Example data only: every name,
phone number, Instagram name and ID is made up. The prototype saves, sends and links nothing, and it makes no
network calls.

GK-018 r03b2 (`../gk-018-r03/`, the approved chef workspace sketch) left "account claims" out (its "Not done in
r03b"). Customers can now ask to add an older order to their account on staging (GK-029, built from the approved
GK-016 r02b sketch), so the chef needs a way to answer. r04-claims designs that chef side in r03b2's visual
language: unchanged tokens (GK-015 r12), Geist fonts, sidebar, cards, chips and wording style, in the "GK Cake
London" workspace. `../gk-018-r01/`, `../gk-018-r02/` and `../gk-018-r03/` are unchanged.

## Execution check

```text
Role/task: GK-018 r04-claims / design worker (bounded proposal sketch; Creator asleep)
Model requested/effort: none named in the brief; the host session's model ran
Model observed/effort: claude-opus-5-5 (host-reported: llmplatform/claude-opus-5-5) / effort not exposed
Model evidence/status: host system environment / confirmed model, effort unknown
History: one continued author session from the brief; no subagents
Inputs: sufficient for a proposal sketch; not sufficient for implementation (open questions below)
Input revision: origin/main f0f2b5a; chef frame from origin/sasha-chycha/chef-ui-2 a559e5d (unmerged, see Gaps)
Independence: not-applicable (author). A fresh design review and the Creator's exact-version approval are pending
Writes: only docs/design/sessions/chef-operations/gk-018-r04-claims/ on sasha-chycha/gk-018-r04-claims-design
Tools: local files, git (read and local commit), headless Chrome, python3 http.server on 127.0.0.1:4196.
       No push, PR, GitHub comment, Trello, Glean, Slack, Jira, Grafana or Snoocode GitHub tools
```

Readback:

- **Outcome.** A chef queue of requests to add orders, a decision page for both request paths (the order's email
  link, and an older order), and a proposal for the chef adding an order herself.
- **Rules (K00).** Nothing is linked automatically: not by email, payer, address, name or Instagram. Only the
  chef approves. A waiting request reveals no order details to the customer, and a decline is generic ("Couldn't
  add", contact us). Approving revokes the order's guest links. Requests are chef only: fulfilment and technical
  support get 403.
- **Profile data (K02, K09).** The profile's Instagram name and phone are private plain text, not proof. The
  chef's internal note is never customer text.
- **K10.** No analytics on admin routes.
- **Boundaries.** No account lookup by email (account discovery isn't allowed). No manual or historical order
  feature (GK-027/GK-038).
- **Unknowns.** The questions below.

## Inputs (SHA-256, first 16 hex)

| Input | Hash |
| --- | --- |
| AGENTS.md | d357f39d8b39ec72 |
| skills/gk-feature/SKILL.md / references/design-sessions.md | b5a68cb7c520cf1f / 915e72b29842e6b4 |
| skills/gk-plan/references/execution-check.md | 97d49fb1ad2ec4c2 |
| gk-018-r03/session.md (r03b2) | 255d23147b57aa66 |
| gk-018-r03/poc admin.css (copied unchanged into `poc/`) / core.js / orders.js / order.js / support.js / orders.html | 94d2888df66b50ca / c7441546c219380e / b8ecf16763ed4f01 / 8283ce7fecce45a5 / ae25c87fa361f123 / 3e9b401a61e1e3f5 |
| Built chef frame, `git show origin/sasha-chycha/chef-ui-2` (a559e5d): ChefShell.svelte / chef.css / admin/orders/+page.svelte / lib/admin/orders.ts | df1912051f50aab7 / 0c403b935ea9e0d7 / 3f120b25534fad22 / c8efa495b838e536 |
| discovery-account/gk-016-r02/session.md (r02b; states 31–42, answers 1 and 6) | af41729299f910b6 |
| ui/src/routes/account/older/+page.svelte / account/requests/[id]/+page.svelte | 66dee1152ac6cd39 / 1af03f7700d81475 |
| ui/src/lib/account/claims.ts / older.ts | 6a02cd7ac048814a / 97c223297d3d8da2 |
| api/README.md (whole) / its "Customer accounts (GK-016 r02b)" section (lines 2471–2717) / "Staff order reads" (2198–2277) | 8f029c8665bd7992 / c8d0962fe16fb26e / 25c7239600bbe263 |
| api/migrations/0033_customer_accounts.sql (order_claim, order_claim_staff_list, order_claim_decide, order_owner_assign) | bb58871225007876 |
| api/internal/checkout/checkout.go (409/422 codes: `order_owned`, `account_unavailable`, `duplicate`, `stale_version`, `state_conflict`) | 5c89d5d7f5e633ff |
| docs/design/contracts.md (r6.5: K00, K02, K09, K10) | a2817cb743c8d5e6 |
| docs/creator/README.md / docs/design/owner-data-contract.md (checked for relevance only) | 67b15691fdd19f05 / e71792978a198263 |

No owner value (BO-GK-001/003/005) is used. The sketch shows no prices beyond the example order totals and sets no
retention or time rule. The amber "waiting" chip at 3 days is a visual choice, not a business target (question 9).

## How the prototype is organised

Serve the `chef-operations` folder so the sidebar can also open the r03b2 prototype:

```sh
python3 -m http.server 4196 --bind 127.0.0.1 --directory docs/design/sessions/chef-operations
```

Open http://127.0.0.1:4196/gk-018-r04-claims/poc/index.html (the states index).

| File | What it is |
| --- | --- |
| `poc/index.html`, `index.js`, `states.js` | States index: S01–S41, grouped, each with a link. `states.js` is the single list, also used by `shoot.sh` |
| `poc/requests.html`, `requests.js` | Screen 1, **Requests to add orders**: tabs, table (cards on mobile), loading, error, empty and paging states |
| `poc/request.html`, `request.js` | Screen 2, **decide a request**: email-link and older-order requests, confirm, decline, busy, results, refusals |
| `poc/order.html`, `order.js` | Screen 3: the order page's **Customer account** card. Option A (recommended) and option B (needs a GK-016 change) |
| `poc/core.js` | Shared example data (orders and requests shaped like the API rows), words, the r03b2 shell with the new sidebar item, review bar |
| `poc/admin.css` | r03b2's stylesheet, **byte-identical** |
| `poc/claims.css` | Additions only: sidebar count, tabs, queue table, decision column, pick list, spinner, 44 px targets |
| `poc/assets/` | r03b2's Geist fonts (with their OFL files) and emblem; the four cake photos downscaled to 240 px |
| `shot.html`, `shoot.sh` | Screenshot tooling (frames a page at an exact width) |
| `check.html` | Scripted checks (drives the prototype in a same-origin iframe; see Checks) |
| `screenshots/` | Desktop (laid out at 1440 px, saved at 1120 px) and mobile (390 px) JPEGs |

The dark review bar switches the role (Chef, Fulfilment, Technical support). Depending on the page it also picks
the list state (queue), the **simulated API answer** (decision page) or the option A/B proposal (order page). Every
simulated action shows "Prototype: nothing is saved", and the bar repeats "Proposal … not approved".

The preview clock is Fri 6 Nov 2026, 10:30 London time. Order references are the live short IDs (the first 8
characters of the order's UUID, as the built chef order list and the customer's request page show them).

## The three screens

### 1. Requests to add orders (queue)

- **Where it lives:** a new sidebar item, **Account requests**, for the chef only, with a count badge. The page
  title is "Requests to add orders". It sits after Needs attention. The label isn't just "Requests", because
  custom-cake requests will need that word. Recommended over a group inside Needs attention (question 1).
- **Tabs:** Waiting (default, oldest first), Added, Couldn't add, Withdrawn. They mirror the API states
  `pending`, `approved`, `declined` and `withdrawn`, and the customer's chips ("Waiting for the chef", "Added to
  your account", "Couldn't add", "Withdrawn").
- **Each row:**
  - Received (date, time and the request's short reference, which the customer also sees).
  - Who: profile name, Instagram, phone. The column header says "(their profile)", and the footer says they're
    typed by the customer, not checked and not proof. The workspace doesn't store the customer's email.
  - How: "From the order's email link" plus the website order's short ID and summary, or "Instagram" / "Email,
    phone or in person" plus "Your reference" and "Roughly <month>".
  - Their note, clamped to 4 lines; the full text is on the request page.
  - Waiting time as a chip, amber from 3 days. Plus "Another request asks for this order" when two waiting
    requests name the same order.
  - Review (or View).
- Decided tabs show the outcome, when, "by you", and the chef's internal note (marked with an icon, never
  customer text).
- **States:** loading (status line + skeleton), couldn't load (alert + Try again), nothing waiting (empty text),
  more than a page ("Show the next requests"; live: 50 at a time from `next_cursor`).
- **Erased requests:** the customer deleted their account, so the profile and every free-text field show "Details
  erased".
- A banner says plainly that Instagram, email, phone and in-person orders aren't in the system yet. Requests for
  older orders usually end in "Decline", or wait.

### 2. Decide a request

**Layout.** The left column shows the facts; the right column, "Your decision", is sticky on desktop and comes
last on mobile.

**Email-link request.** "The order from their link" comes first: photo, short ID, cake, flavour and size, handover,
mode, status chip, name on the order, total, and a link to the order. A note says only this order can be added.
Then "What they sent" (with "having the link shows they can see the order; it doesn't prove it's theirs") and
"Who's asking · from their profile". If another waiting request asks for the same order, an amber banner names it
and says only one account can have it.

**Older-order request.** "What they sent" shows the channel, "Your reference", roughly when, and the note, then
"Who's asking", then **Find the order**:

- **Search:** the start of the order ID, or the name on the order. Two "Search for" buttons fill it with the
  customer's reference or surname; the chef presses them herself, nothing is prefilled or matched automatically.
- **Handover months:** from and to, starting at the customer's month ±1 (or the last 3 months). Up to 3 months,
  because the order list reads at most 93 days.
- **Results:** a radio list of website orders. "Choosing one is your judgement: a matching name or reference
  isn't proof"; "This list can't show whether an order is already in someone's account".
- **No match** ("No matching order in the system yet"): Instagram and pre-website orders aren't in the system;
  recording older orders by hand comes later (GK-027/GK-038, not built). The two honest choices are **Decline**
  (the customer sees only "Couldn't add" and is asked to contact us) or **Leave it waiting** (they keep seeing
  "Waiting for the chef", and it counts towards their limit of 5).

**Approve.** The confirmation ("Add order 3f9c71a2 to Amelia Rose's account?") says, in plain words:

- the order appears in their account;
- the private links in this order's emails stop working for everyone who has them, and they see the order by
  signing in;
- their request shows "Added to your account";
- there's no undo here, because removing an order from an account isn't built.

Buttons: "Yes, add to their account" / "Not yet".

**Decline.** The confirmation says:

- nothing is added;
- they see only "Couldn't add" and are asked to contact us, with no reason and nothing about any order;
- for an email-link request, the link in their order email keeps working.

There's an optional **internal note**: one line, at most 500 characters, with a counter, "only you see it, never
the customer". Buttons: "Decline request" / "Not yet".

**After saving.** Busy labels read "Adding…" or "Declining…". On success the header chip and eyebrow change to the
new state, the left column becomes read only ("The order you added"), focus moves to the result, and there's a
"Next waiting request" button.

**Refusals.** Each says "Nothing changed":

| Refusal | What the chef sees | Next |
| --- | --- | --- |
| `ORDER_OWNED` | Already in another customer's account; the workspace doesn't say which | Choose another order (older order), or Decline |
| `STALE_VERSION` or `CONFLICT` | "Changed while you were looking": decided on another device, or withdrawn by the customer | Show the latest (S26: withdrawn) |
| 422 | Email link: "That isn't the order from their email link" (Reload). Older order: "That order can't be found any more" (Search again) | As shown |
| `ACCOUNT_UNAVAILABLE` | Their account is disabled | Decline |
| 503 | Paused while the data is restored (the restore freeze) | Try again in a few minutes |
| Answer lost | "We couldn't tell whether it was saved"; trying again is safe | Try again (same key) |

### 3. Add an order to a customer's account (chef-initiated)

The API exists: `POST /admin/orders/{id}/owner` with `account_id | account_subject` and an evidence note. But the
customer can't see any account identifier, and looking an account up by email would be account discovery (not
allowed; the workspace doesn't even store the email). The order page gets a **Customer account** card, tagged
"proposal · not approved", with two options for the Creator (question 3):

- **A, recommended: nothing new to build.** The chef replies to the customer with the order's reference (a
  copyable suggested reply; "Prototype: nothing is copied or sent"). The customer sends it from Account → Add an
  older order, and the chef adds it from the request. This keeps the promise the live customer page already makes
  ("She can add an order for you", and "ask the chef to add it for you" for guest website orders) with the same
  checks and the same "Added to your account" result.
- **B, needs a GK-016 change (no API change):** the customer's profile shows "Your account ID", their sign-in
  provider subject, which the UI already has from the token. The customer copies it to the chef, who pastes it
  here with a required evidence line, then confirms. Nothing about the account is shown before or after adding.
  The confirmation warns that the workspace can't show whose account it is. Answers: added, `DUPLICATE` ("already
  in that account"), `ORDER_OWNED`, 422 ("no active account has that ID"), `STALE_VERSION`, 503.

A design note on the card says the staff order view doesn't yet show whether an order is in an account (Gaps).

## States table

C = chef. Fulfilment and technical support see only a role note on these pages, and no sidebar item (S09).
"Simulates" is the live call the state stands for; the prototype makes no call.

| State | What the chef sees | Buttons | Destination | Simulates |
| --- | --- | --- | --- | --- |
| S01 Queue · Waiting | 5 waiting requests, oldest first; profile, how, note, waiting chip; "another request asks for this order" on two | Review; tabs | S10/S16/S17/S18 | `GET /admin/order-claims` (state=pending) |
| S02 Loading | "Loading requests…" status + skeleton | — | — | the same, in flight |
| S03 Couldn't load | Alert: couldn't be loaded, nothing changed | Try again | S01 | the same, failed (network/5xx) |
| S04 Nothing waiting | Empty text: how customers ask | tabs | — | `{claims: []}` |
| S05 More than a page | "Showing the oldest 5 · more below" | Show the next requests | rows appended, focus to the first new row | `?cursor=<next_cursor>` |
| S06 Added | Outcome chip, when, by you, internal note; an erased request | View | S31/S33 | `?state=approved` |
| S07 Couldn't add | The same for declined | View | S32 | `?state=declined` |
| S08 Withdrawn | "Withdrawn by the customer", when | View | read-only request | `?state=withdrawn` |
| S09 Other roles | Role note: chef only (403) | — | — | 403 `FORBIDDEN` |
| S10 Email-link review | The order from their link first, then what they sent and who | Add to their account; Decline | S11 / S14 | — |
| S11 Confirm add | Plain-words consequences | Yes, add to their account; Not yet | S12 → S13 / S10 | — |
| S12 Adding | "Adding…" disabled, status line focused | — | S13 or a refusal | `POST …/decision {expected_version, decision: approve, order_id}` + Idempotency-Key |
| S13 Added | Header "Added to their account"; result (links stopped, customer sees "Added") | Next waiting request; Back to requests | next request / S01 | 200 `{claim_id, state: approved, order_id}` |
| S14 Decline | Consequences + optional one-line internal note with counter | Decline request; Not yet | busy → S15 / S10 | — |
| S15 Declined | Header "Couldn't add"; note echoed as internal | Next waiting request; Back to requests | — | `POST …/decision {expected_version, decision: decline, note?}` → 200 |
| S16 No name; same order twice | "A customer asks…", profile "Not given"; amber banner linking the other request | as S10 | — | (two pending rows with the same `order_id`) |
| S17 Instagram, no match | Find the order (May–Jul 2026): no orders; "No matching order in the system yet" + Decline / Leave it waiting | Decline; Not sure? go back | S14 / S01 | `GET /admin/orders?from&to` (empty) |
| S18 Offline, orders in range | All website orders Sep–Nov 2026 as a radio list | Search for … | S19 | `GET /admin/orders?from=2026-09-01&to=2026-11-30` |
| S19 Two matches | "8b2" finds 8b2a1f03 (Tess K.) and 8b20d4e6 (Tom B.) | choose one | S20 | browser filter (no text search in the API) |
| S20 Chosen | "Chosen: Raspberry ribbons · Sat 17 Oct …" | Add order 8b20d4e6 to their account; Decline | S21 / S14 | — |
| S21 Confirm add (older) | Consequences for 8b20d4e6 | Yes, add…; Not yet | busy → S22 | — |
| S22 Added (older) | "The order you added" + result | Next waiting request | — | `decision: approve, order_id: <8b20d4e6…>` → 200 |
| S23 In another account (link) | Refusal; workspace doesn't say which | Decline | S14 | 409 `ORDER_OWNED` |
| S24 In another account (older) | Refusal | Choose another order; Decline | S19 / S14 | 409 `ORDER_OWNED` |
| S25 Changed meanwhile | Decided elsewhere or withdrawn; nothing saved | Show the latest | S26 | 409 `STALE_VERSION` (or `CONFLICT`) |
| S26 Latest | Read only: "Withdrawn by the customer", nothing to decide | Back to the list | S08 | re-read the request |
| S27 Not that order | Only the email-link order can be added | Reload the request | S10 | 422 `INVALID_INPUT` (order_id) |
| S28 Account disabled | Nothing can be added | Decline; Back to requests | S14 / S01 | 409 `ACCOUNT_UNAVAILABLE` |
| S29 Paused | Restore in progress; try in a few minutes | Try again | busy → S13 | 503 `UNAVAILABLE` |
| S30 Answer lost | Couldn't tell if saved; retrying is safe | Try again | busy → S13 | network loss; same Idempotency-Key replays |
| S31 Decided · added | Outcome, order added, internal note, customer sees | Back to the list | S06 | (row from `?state=approved`) |
| S32 Decided · couldn't add | Outcome, internal note, the customer's exact decline words | Back to the list | S07 | (row from `?state=declined`) |
| S33 Decided · erased | "Request (details erased)"; profile, reference, note and internal note erased; "customer sees: nothing, the account was deleted" | Back to the list | S06 | erased row |
| S34 Chef add · A | Customer account card: ask for a request, 3 steps, copyable reply | Show a reply you can copy; Copy the reply | — | none (uses S10–S22) |
| S35 Chef add · B form | Account ID + required evidence | Add to their account | S36 (or inline errors) | — |
| S36 Chef add · B confirm | Consequences; "can't show whose account it is" | Yes, add…; Not yet | busy → S37 | `POST /admin/orders/{id}/owner {expected_version, account_subject, evidence}` |
| S37 B added | Added; links stopped | — | — | 200 `{order_id, linked}` |
| S38 B unknown account | No active account has that ID | Back to the form | S35 | 422 (account_id) |
| S39 B another account | Already in another account | Back to the form | S35 | 409 `ORDER_OWNED` |
| S40 B already there | Already in that account; nothing changed | — | — | 409 `DUPLICATE` |
| S41 Order added from a request | Customer account card names the request that added it | link | S31 | (proposal: needs ownership in the staff order view) |

Option B also handles a stale order version ("This order changed while you were looking": Reload) and 503
("paused": Try again), selectable in the review bar.

## Screen-to-API mapping

| Screen element | API (truth: api/README.md, 0033) | Notes |
| --- | --- | --- |
| Queue tabs, rows, paging | `GET /api/v1/admin/order-claims?state=pending\|approved\|declined\|withdrawn&cursor&limit` → `{claims, next_cursor?}`, oldest first, `limit` 1–50 | Row fields: `id`, `version`, `state`, `path`, `channel`, `reference`, `approximate_date`, `note`, `order_id`, `created_at`, `decided_at`, `decided_by`, `decision_note`, `account {id, full_name, instagram, phone}` |
| Sidebar count | (no count route) | Count the first page; show "50+" when `next_cursor` is present |
| "Another request asks for this order" | two pending rows with the same `order_id` (email-link path) | Only within the loaded pages |
| "by you" | `decided_by` (an account ID) = the signed-in chef | No staff-name lookup; any other ID reads "another chef account" |
| Order summary (email link) | `GET /api/v1/admin/orders/{id}` (chef detail) | Short ID, items, date and slot, mode, lifecycle, `customer_full_name`, total |
| Find the order | `GET /api/v1/admin/orders?from&to` (≤93 days, 100 a page), filtered in the browser by ID prefix or `customer_full_name` | The built Orders page does the same (`matchesSearch`) |
| Approve | `POST /api/v1/admin/order-claims/{id}/decision {expected_version, decision: "approve", order_id}` + Idempotency-Key → 200 `{claim_id, state, order_id}` | Links the order, bumps its version, revokes every guest capability, audit `order.owner_linked`. An order already in the claimant's account is approved without a new link (same "Added" words) |
| Decline | the same with `decision: "decline", note?` (`decision_note`: 1–500, one line, no surrounding spaces) | Nothing linked. The note is internal and erased if the account is deleted |
| Refusals | 409 `ORDER_OWNED`, `ACCOUNT_UNAVAILABLE`, `STALE_VERSION`, `CONFLICT`; 422 `INVALID_INPUT` (`order_id`, `note`); 503 `UNAVAILABLE`; 404 unknown request; 403 non-chef | `KEY_REUSED` shouldn't happen (below); shown as the general problem |
| Chef add (option B) | `POST /api/v1/admin/orders/{id}/owner {expected_version (order's), account_subject, evidence}` → 200 `{order_id, linked}`; 409 `DUPLICATE`, `ORDER_OWNED`, `STALE_VERSION`; 422; 503 | Evidence 1–500, one line; the only field ever changed (erased on account deletion) |

**Keys.** One Idempotency-Key per decision attempt. It's kept for "Try again" after a lost answer or a 503, which
replays safely: the freeze rolls back before the key is recorded. A new key is made whenever the decision, the
chosen order or the note changes, because the same key with another body is 409 `KEY_REUSED`.

## Chef words ↔ customer words

| State | Chef sees | Customer sees (built `claims.ts`) |
| --- | --- | --- |
| pending | Waiting · "Waiting N days" | "Waiting for the chef" |
| approved | Added to their account | "Added to your account" |
| declined | Couldn't add (+ internal note) | "Couldn't add": "The chef couldn't add an order from the details given, so nothing was added. Please contact us if you think this is a mistake." |
| withdrawn | Withdrawn by the customer | "Withdrawn" |
| channel `instagram` / `offline` / `website_email_link` | Instagram / Email, phone or in person / From the order's email link | Instagram / Email, phone or in person / On this website |

## Decisions and deviations

- **Own sidebar item, not Needs attention.** Needs attention is technical support's home and covers behind-the-scenes
  problems, while claims are chef-only business decisions (support gets 403). Proposed; question 1.
- **44 px targets in this flow.** r03b2's `.btn` is 40 px. `claims.css` raises buttons, chips, inputs and selects
  to 44 px here without changing a token. Inline text links (order IDs, "go back") are exempt as inline links; the
  prototype-only review bar keeps r03b2's smaller pills.
- **Live short IDs** (8 hex characters) replace r03b2's illustrative `GK-1036`-style numbers, matching the built
  Orders page and the customer's request reference.
- **"Account requests" (nav) / "Requests to add orders" (h1)**, so that "Requests" stays free for custom-cake
  requests.
- **No automatic suggestions.** The find panel never prefills a match; the chef presses "Search for …" herself.
  The name on the order and the profile name are shown side by side for her judgement only.
- **Profile data is shown but labelled.** It's what the API gives the chef and the only contact detail she has;
  it's labelled "typed by the customer, not proof" everywhere. Question 2.
- **The order page is shortened.** Only the Customer account card is new; the rest is r03b2's order page, which
  isn't redrawn.
- **Sidebar sections** other than Account requests open the r03b2 prototype.
- **Cake images** are 240 px copies of r03b2's photos, to keep the folder small.
- **"Leave it waiting"** is a real option (no API call), not a button that pretends to do something.

## Accessibility

- One `h1` per view. Every control has a visible label. The tables have captions and row headers and become
  labelled cards on mobile.
- Tabs are links with `aria-current`.
- Focus moves to the confirmation heading, the busy status and the result. Results and counts use live regions.
  A refusal uses `role="alert"`, a success uses `role="status"`.
- Chips always carry words, never colour alone. Measured contrast: the "how" chip 10.9:1, inactive tab count
  7.4:1, internal note 9.3:1, warning text 6.1:1, muted text 5.9:1, sidebar count 12.9:1.
- Reduced motion stops the spinner.

## Gaps

- **The built chef frame isn't on main.** The brief named `ui/src/lib/admin/ui/ChefShell.svelte`, `chef.css` and
  `routes/admin/orders/+page.svelte` "on main". At f0f2b5a they don't exist; they're on the unmerged
  `sasha-chycha/chef-ui-2` (origin a559e5d), read with `git show`. Its sidebar matches r03b2, so this sketch adds
  one item to it.
- **Ownership isn't in the staff order views.** `added_by`/`added_at` are only in the customer's summary. The
  chef can't see beforehand whether an order is in an account; `ORDER_OWNED` is the only signal. Proposed API
  follow-up: a yes/no plus source and time in the chef order detail and list, never whose account (question 8).
- **No count route for the sidebar badge.** The first page is counted.
- **No staff names for `decided_by`.**
- **No undo:** removing an order from an account isn't built.
- **No customer notification.** No order emails yet (GK-031), so the customer sees the result only in their
  account.
- **Path a is limited.** The customer's "add from the email link" button and the emailed private link aren't
  built, so path a works only in the browser that placed the order (README "Not yet").
- **Historical orders** (Instagram, offline) don't exist (GK-027/GK-038). Those requests can only be declined or
  left waiting, which this sketch says honestly. No manual-order feature is invented.
- **Repeated requests** for the same order after a decline aren't rate-limited yet (an API decision, GK-016 r02b
  gap).
- **Today's "Needs you"** summary line ("N requests to add orders · oldest N days") is proposed, not drawn.
- **Option B's "Your account ID"** would be a GK-016 customer-UI change: Profile showing the provider subject. Not
  designed on the customer side here.

## Open questions for the Creator

1. **Where should the queue live?** Recommended: its own chef-only sidebar item, "Account requests", with a count,
   plus one line on Today's "Needs you". Alternative: a group inside Needs attention. That page is technical
   support's home, and support can't open requests (403), so the group would be empty or confusing for Sasha.
2. **Show the customer's phone and Instagram name in the queue?** Recommended: yes, labelled "from their profile,
   typed by the customer, not proof". They're the only way to recognise or contact the person, since the workspace
   has no email. Alternative: show them only on the request page.
3. **Chef-initiated add: A or B?** Recommended: **A**. Defer a separate tool: the chef asks the customer to send a
   request with the order's reference, then adds it from the queue. It's the same checks and the same result, with
   nothing new to build or to keep safe. B (a customer-visible account ID pasted by the chef) is a GK-016 change and
   puts a 36-character ID in customer messages. Looking an account up by email is not proposed (account discovery).
4. **Instagram and offline requests before older orders exist (GK-027/GK-038): decline now, or leave them
   waiting?** Recommended: decline, with an internal note such as "Instagram order from May: not in the system
   yet". The customer sees "Couldn't add" and is invited to contact us, and their 5-request limit isn't held for
   months. Sub-question: should the customer's "Add an older order" page say for now that only website orders can
   be added (a GK-016 copy change)? Recommended: yes. It would stop requests that can only be declined.
5. **Keep the chef's internal note on decline?** Recommended: yes, optional. The API stores it, it's never shown to
   the customer, and it's erased if the account is deleted. It helps when the customer gets in touch. It's one line,
   at most 500 characters.
6. **Website guest orders sent through "Add an older order".** Today the customer form offers Instagram and "Email,
   phone or in person" only. A guest website order sent that way still works, because the chef chooses the order.
   Should the form offer "On this website (as a guest)" with the order reference until the email-link button exists?
   The API already accepts that channel. Recommended: yes, as a small GK-016 amendment.
7. **Two accounts asking for the same order.** Recommended: no automatic rule. The chef sees the amber banner,
   decides one, and the other is refused with `ORDER_OWNED` if she tries to add it there too. She declines it.
8. **Should the chef see whether an order is already in an account?** That means a yes/no, how it was added and
   when, never whose. Recommended: yes, as an API follow-up for the staff order views. It removes surprise
   `ORDER_OWNED` refusals and lets the order page say "In a customer's account (added from request c7e1a904)".
9. **Waiting-time highlight.** The sketch turns the chip amber after 3 days. Is there a response time you want to
   aim for? Recommended: keep it visual only, and promise customers no time.

## Checks run (2026-11-06)

- `node --check` passes on every `poc/*.js`.
- **Rendering.** Headless Chrome (`--dump-dom` with console logging) rendered all 41 states plus the index as the
  chef, fulfilment and technical support: 126 page loads. Every load had exactly one `h1`, no duplicate IDs and no
  script errors or console errors. An injected `null.x` page proved the error capture works.
- **`check.html`** (scripted, same-origin iframe, role forced to chef), 67 checks, all PASS:
  - **Email-link approve.** The confirmation names the order and the account; focus moves to it; "Adding…"; then
    the result, with focus on it. The header changes to "Added to their account", and "Prototype: nothing is saved"
    is shown.
  - **Decline.** The note counter updates, and the declined result echoes the internal note.
  - **Older order.** Add is disabled until an order is chosen. "8b2" finds two orders, and choosing one enables
    "Add order 8b20d4e6…". More than 3 months is refused. The Instagram request shows the no-match guidance.
  - **Refusals and retries.** `ORDER_OWNED` shows its refusal. After a lost answer, Try again succeeds. "Show the
    latest" after a stale answer shows the request withdrawn.
  - **Queue and roles.** Paging adds the next rows (8). Technical support gets no sidebar item and sees the role
    note.
  - **Option B.** An empty account ID is refused (`aria-invalid`); a valid one reaches the confirmation.
  - **Sizes.** Every button, input and select on the queue, find and order pages is at least 44 px tall. All 41
    states have no horizontal overflow at 390 px.
- **Contrast** of the new colour pairs was computed (Accessibility).
- **Screenshots.** `shoot.sh` with headless Chrome took 31 states (desktop + mobile, 62 JPEGs). Images were looked
  at for S00, S01, S02, S05, S06, S09, S10, S11, S13, S14, S16, S17, S19, S22, S23, S29, S32, S33, S34, S35, S36
  and S38. Fixes made after looking:
  - mobile card cells put the second line in the label column (each cell is now wrapped);
  - "Waiting since today" was shown for a request from the previous evening (London calendar days now);
  - the email-link order was the last card (it's now first);
  - the eyebrow uppercased the hex reference;
  - the option B form was indented by a class clash (`.tight`);
  - a short mobile page had a tall empty sidebar row;
  - after saving, the header still said "Waiting" and the search stayed active;
  - the erased request still showed an internal note and "the customer sees";
  - the disabled Add said "on the left", which is wrong on mobile.
- `python3 scripts/check_planning_kit.py` passes (run before the commit).

## Screenshots (`screenshots/`)

`<state>-<slug>-desktop.jpg` / `-mobile.jpg` for: S00 index; S01–S07, S09 (queue); S10, S11, S13–S16 (email
link); S17, S19, S21, S22 (older order); S23, S25, S27–S30 (refusals and pauses); S32, S33 (decided); S34–S36,
S38 (chef add). The other states are reachable from the index; their layouts match a shot state.

## Not done

- No Creator review or approval, and no independent design review. Both are needed before any implementation
  ticket uses this.
- No Trello card, comment or PR; nothing pushed.
- No customer-side changes: options B and question 6 would need GK-016 amendments.
- No API changes (questions 8, count, staff names).
- No manual or historical order recording (GK-027/GK-038).
- No Today summary line drawing.
- No implementation in `ui/` or `api/`.
