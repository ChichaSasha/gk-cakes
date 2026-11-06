# GK-018 r04b-claims — chef side of adding orders to customer accounts

Status: **r04b — Proposal — not approved.** r04 (a338fd2) was drafted on 2026-11-06 while the Creator (Sasha) was
away. The planning master pushed it as **draft PR #59**. A fresh independent design reviewer (gk-plan-review)
returned **REVISE** on a338fd2 (3 major, 10 minor, nits). r04b, in the same folder, addresses every finding; see
[Review round 1 changes](#review-round-1-changes). r04b still needs a fresh re-review and the Creator's
exact-version approval. No implementation ticket may treat it as approved.

Example data only: every name, phone number, Instagram name and ID is made up. The prototype saves, sends and
links nothing, and it makes no network calls.

**Today and future.** The default example data now shows what customers can actually send **today**: requests
from "Add an older order" (path c), including guest website orders sent through that form with the order's
reference. Requests from an order page or order email (path a) are accepted by the API, but the customer UI can't
send them yet. They appear only under the review bar's "+ Future order-page requests" and are marked **Future**
everywhere: they arrive once GK-016's "Add this order to my account" (on the order page) and order emails (GK-031)
exist.

**Background.** The Creator-approved GK-018 r03b2 sketch (`../gk-018-r03/`) left "account claims" out (its "Not
done in r03b"). Its approval evidence, as cited by the reviewer:

- GK-018 card approval comment 6aece431658dec491dacf724 and answers comment 6aece262dae822c733c7df31
  (2026-11-05);
- PR #15 head 5df924b.

I didn't verify these myself: this run has no Trello access. `../gk-018-r03/session.md` still opens with "Not
approved", from before that approval.

Customers can ask to add an older order on staging (GK-029, from the Creator-approved GK-016 r02b sketch), so the
chef needs a way to answer. r04b designs that chef side in r03b2's visual language: unchanged tokens (GK-015 r12),
Geist fonts, sidebar, cards, chips and wording style, in the "GK Cake London" workspace. `../gk-018-r01/`,
`../gk-018-r02/` and `../gk-018-r03/` are unchanged.

## Execution check

```text
Role/task: GK-018 r04b-claims / design worker (round 1 revision of a bounded proposal sketch; Creator asleep)
Model requested/effort: card policy for bounded feature design is gpt-5.6-sol / high; the brief named none
Model observed/effort: claude-opus-5-5 (host-reported: llmplatform/claude-opus-5-5) / effort not exposed
Model evidence/status: host system environment / mismatch with the card policy; the user's host selection takes precedence
History: continued author session (r04 then r04b); no subagents. Review findings read from pr59-round1.md
Inputs: sufficient for a proposal sketch; not sufficient for implementation (open questions, API follow-ups)
Input revision: origin/main f0f2b5a; chef frame from origin/sasha-chycha/chef-ui-2 a559e5d (unmerged, see Gaps)
Independence: not-applicable (author). Round 1 was an independent review; a fresh re-review of r04b is pending
Writes: only docs/design/sessions/chef-operations/gk-018-r04-claims/ on sasha-chycha/gk-018-r04-claims-design
Tools: local files, git (read and local commit), headless Chrome, python3 http.server on 127.0.0.1:4196.
       No push, PR edit, GitHub comment, Trello, Glean, Slack, Jira, Grafana or Snoocode GitHub tools
```

**Why this model is suitable here (review finding 10).** The work touches an authorisation boundary (who may link
an order to an account), so model suitability matters:

- **No new rule is decided.** Every authorisation rule shown is reused from accepted sources: K00, the GK-016 r02b
  answers, and the live API's chef-only routes and error codes, all read at the hashes below. Every change to a
  rule (option B, a website choice, new API fields) is put to the Creator or the master as a question.
- **Track record.** The same host model authored the GK-016 r02b and GK-018 r03b2 sketches, which passed
  independent review and Creator approval. That is evidence for this role, not proof.
- **Independent check.** Round 1 was independent, and a re-review of r04b is still required.

The card's gpt-5.6-sol/high wasn't selectable in this host session. The mismatch is recorded, not hidden.

Readback:

- **Outcome.** A chef queue of requests to add orders, a decision page for older-order requests (today) and
  order-page requests (future, labelled), and a proposal for the chef adding an order herself.
- **Rules (K00).** Nothing is linked automatically: not by email, payer, address, name or Instagram. Only the
  chef approves. A waiting request reveals no order details to the customer, and a decline is generic. Approving
  revokes the order's guest capabilities: today that's the order page in the browser that placed it. Requests are
  chef only (fulfilment and technical support get 403).
- **Profile data (K02, K09).** The profile's Instagram name and phone are private plain text, not proof. The
  chef's internal note is never customer text.
- **K10.** No analytics on admin routes.
- **Boundaries.** No account lookup by email. No manual or historical order feature (GK-027/GK-038).

## Inputs (SHA-256, first 16 hex)

| Input | Hash |
| --- | --- |
| Review findings `pr59-round1.md` (round 1, REVISE on a338fd2) | 8bba039551095f32 |
| AGENTS.md | d357f39d8b39ec72 |
| skills/gk-feature/SKILL.md / references/design-sessions.md | b5a68cb7c520cf1f / 915e72b29842e6b4 |
| skills/gk-plan/references/execution-check.md | 97d49fb1ad2ec4c2 |
| gk-018-r03/session.md (r03b2) | 255d23147b57aa66 |
| gk-018-r03/poc admin.css (copied unchanged into `poc/`) / core.js / orders.js / order.js / support.js / orders.html | 94d2888df66b50ca / c7441546c219380e / b8ecf16763ed4f01 / 8283ce7fecce45a5 / ae25c87fa361f123 / 3e9b401a61e1e3f5 |
| Built chef frame, `git show origin/sasha-chycha/chef-ui-2` (a559e5d): ChefShell.svelte / chef.css / admin/orders/+page.svelte / lib/admin/orders.ts | df1912051f50aab7 / 0c403b935ea9e0d7 / 3f120b25534fad22 / c8efa495b838e536 |
| discovery-account/gk-016-r02/session.md (r02b; states 31–42, answers 1 and 6) | af41729299f910b6 |
| ui/src/routes/account/older/+page.svelte / account/requests/[id]/+page.svelte | 66dee1152ac6cd39 / 1af03f7700d81475 |
| ui/src/lib/account/claims.ts / older.ts | 6a02cd7ac048814a / 97c223297d3d8da2 |
| ui/src/routes/orders/[id]/+page.svelte (the guest order page shows "Order reference: {full order ID}"); ui/README.md lines 339–340 ("website orders are added from their order page later") | read at f0f2b5a |
| api/README.md (whole) / "Customer accounts (GK-016 r02b)" (lines 2471–2717) / "Staff order reads" (2198–2277) | 8f029c8665bd7992 / c8d0962fe16fb26e / 25c7239600bbe263 |
| api/migrations/0033_customer_accounts.sql (order_claim, staff list, decide, owner_assign, deletion lines 1140–1162) | bb58871225007876 |
| api/internal/checkout/checkout.go (409/422 codes); accounts.go `StaffClaims` (closed refused); httpapi/catalog.go (HTTP codes) | 5c89d5d7f5e633ff / read at f0f2b5a |
| docs/design/contracts.md (r6.5: K00, K02, K09, K10) | a2817cb743c8d5e6 |
| docs/creator/README.md / docs/design/owner-data-contract.md (checked for relevance only) | 67b15691fdd19f05 / e71792978a198263 |

No owner value (BO-GK-001/003/005) is used. The amber "waiting" chip at 3 days is a visual choice, not a business
target (question 9).

### Previous outputs: r04 at a338fd2 (SHA-256, first 16 hex)

| File | Hash |
| --- | --- |
| session.md | aebc2491995bbcdb |
| poc/core.js / requests.js / request.js / order.js | c711c19504dc14b1 / ab5ae4adb9004fb1 / ef318f63183c936c / 77841f4395d644f0 |
| poc/states.js / index.js / claims.css | e661e5cf3ede60c0 / 1788ef9512e1a692 / c2f74d8247e15bf9 |
| check.html / shoot.sh | 0dfb7a8d3bd65efe / 65021a173e727b57 |

The r04 screenshots are in a338fd2's tree; r04b replaces them (all re-shot, below).

## Review round 1 changes

| Finding | Change in r04b |
| --- | --- |
| **M1** Email-link path and order-email links shown as if they exist | Default data is today's: 5 waiting older-order requests, two of them guest website orders sent through "Add an older order" with the order reference (one full order ID, one short). Email-link requests appear only with "+ Future order-page requests" (review bar), tagged **Future** in the queue, with a future note above the list and a "Future state" banner on their pages: "Arrives once GK-016's 'Add this order to my account' (on the order page) and order emails (GK-031) exist. Customers can't send this kind of request today." Their states are grouped under "Future" in the index. Approve wording, true today: "Guest access to this order stops working: today that's the order page in the browser that placed it (and, once order emails exist, the links in them). They see the order by signing in." The same goes for the result, option B's confirmation and decided pages. Decline (future path): "Their guest access to the order keeps working as before." The queue's empty text and banner describe what customers can send today. Gaps updated; PR description text below. |
| **M2a** No single-request read | Mapping: today the page finds a request by paging the four list states. The deep link, "Next waiting request", the same-order banner link, the order page's link and "Show the latest" need the proposed `GET /api/v1/admin/order-claims/{id}` (chef only, all states including closed). "Show the latest" says so. |
| **M2b** `erased` invented | Marked in code and here as a **proposed** staff-row field (`erased`, or `erased_at`); today the chef sees null reference and note without knowing why. |
| **M2c** Deletion closes waiting claims; the list refuses `closed` | New example: a request closed by account deletion. New simulated answer "Changed: account deleted" (S43): the stale message now reads "decided on another device, withdrawn by the customer, or closed because they deleted their account". New closed view (S44) explains that closed requests appear in no list, so without the proposed read the request just disappears from Waiting. STATE_CHEF gains "Closed · account deleted". |
| **M2d** Deletion unlinks orders, but S33/S06 said "Added to their account" | S33 and the Added tab show "Account deleted since: the order is no longer in any account" (deletion unlinked all its orders; the order stays for its retention period), via a proposed `account_deleted` flag. The internal note reads "Erased with the account", and "the customer sees" reads "Nothing: the account was deleted". The order page never says such an order is in an account. |
| **M3** Q3's reason for A was wrong about the live customer page | Option A's card now warns that A works smoothly only after a GK-016 wording change. Today's form is titled for Instagram, email or in-person orders, asks for the "Order reference from the chef (e.g. IG-0613)" and offers only Instagram or "Email, phone or in person", while its side panel promises "She can add an order for you". The suggested reply explains the workaround: choose "Email, phone or in person" and paste the "Order reference" from the order page. The "keeps that promise" claim is removed. Question 3 now says A means changing live customer wording, and question 6 is its prerequisite. |
| Minor 1: approved request with a note, but approve had no note field | Approve now has the same optional internal note (one line, ≤500) as decline. The API accepts `note` on approve. The result echoes it. Question 5 is widened. |
| Minor 2: website orders via the older form would read "From the order's email link" | Labels go by **path** first: `email_link` → "From the order page or email" (+ Future), and `evidence` → its channel, with `website_email_link` reading "On this website". References that look like an order ID (8 hex characters or a full UUID) get "Looks like a website order reference". |
| Minor 3: S41 still showed how to add | When the order is in an account (from a request; this assumes the proposed chef-only yes/no, question 8), the card says "This order is in a customer's account. Nothing to add here." and hides options A and B. An approved request whose account was deleted isn't counted. |
| Minor 4: option B's evidence error went into `#acct-err`; no `aria-describedby` | Evidence has its own `#ev-err` (`role="alert"`) linked by `aria-describedby`, with `aria-invalid` on the field at fault. Focus goes to the first wrong field. Too long (over 500) is checked too. |
| Minor 5: stale status, and r03b2 called "approved" without evidence | Status cites draft PR #59 (pushed by the master); "Not done" no longer says "no PR". The r03b2 approval evidence is cited as given by the reviewer, marked unverified in this run. |
| Minor 6: privacy | Question 8 says chef only. Question 2 separates display from data: the API sends name, phone and Instagram on every row whatever the UI shows. New question 10: a one-line notice on the customer form ("The chef sees your profile name, phone and Instagram with this request"), a GK-016 wording change. |
| Minor 7: Q4 trade-off | Question 4 and the no-match panel say a decline shows "couldn't add an order from the details given", which can read as if the details were wrong, and that the customer would have to ask again once older orders can be recorded. |
| Minor 8: decided tabs oldest first | Decided tabs say "oldest received first (as the API returns them; newest first is a proposed follow-up)". Also in Gaps and API follow-ups. |
| Minor 9: `GET /admin/orders` gives at most 100 a page | Find the order loads every page in the months before filtering (up to 10 × 100, like the built Orders page). The hint says "every order in those months is loaded before filtering". Mapping updated. |
| Minor 10: model check | The execution check records the card policy, the host model, the mismatch status and why the model is suitable for this bounded, reuse-only authorisation design. |
| Nit: "recorded with your name" | "…recorded with your staff account and the time" (the API records an account ID; there's no staff-name lookup). |
| Nit: approving an order already in their account revokes nothing | The approve confirmation adds: "If the order is already in their account, it's just marked added; nothing else changes." |
| Nit: note `maxlength` vs counter units; counter announced every keystroke | No `maxlength`. The counter counts code points (as the API does), says "N characters too many" when over, and is no longer a live region. Submitting an over-long note shows an alert and focuses the field. |
| Nit: mobile decision card at the very bottom; "Account requests" off-screen in the top bar | At one column the decision comes right after the main card ("Find the order" or the order they asked from), before "What they sent" and "Who's asking". On mobile the current section scrolls into view in the horizontal top bar. Both are checked by script. |
| Nit: Today line described, not drawn | New `today.html` fragment (S45): the chef's "Needs you" line "5 requests to add orders to accounts · Oldest 3 days (Priya Shah)" with "Review requests". The rest of Today stays r03b2. The sidebar's Today now opens this fragment. |

**For the PR #59 description** (the master edits it; I don't). r04b, still not approved:

- Default example data = older-order requests, which is all customers can send today, including guest website
  orders sent through "Add an older order".
- Order-page and order-email requests are shown only as labelled Future states.
- Approving revokes guest access, which today is only the order page in the browser that placed the order.
- API follow-ups proposed: a single-request read (including closed), an erased / account-deleted flag, newest-first
  decided lists, and a chef-only "in an account" yes/no.
- Option A needs a GK-016 wording change (question 6).

## How the prototype is organised

Serve the `chef-operations` folder so the sidebar can also open the r03b2 prototype:

```sh
python3 -m http.server 4196 --bind 127.0.0.1 --directory docs/design/sessions/chef-operations
```

Open http://127.0.0.1:4196/gk-018-r04-claims/poc/index.html (the states index).

| File | What it is |
| --- | --- |
| `poc/index.html`, `index.js`, `states.js` | States index: S01–S46, grouped, with a separate "Future" group. `states.js` is the single list, also used by `shoot.sh` and `check.html` |
| `poc/requests.html`, `requests.js` | Screen 1, **Requests to add orders**: tabs, table (cards on mobile), loading, error, empty and paging states |
| `poc/request.html`, `request.js` | Screen 2, **decide a request**: older-order requests (today) and order-page requests (future), confirm with an optional note, decline, busy, results, refusals, closed |
| `poc/order.html`, `order.js` | Screen 3: the order page's **Customer account** card. Option A (recommended, needs GK-016 wording) and option B (needs a GK-016 change) |
| `poc/today.html`, `today.js` | Today fragment: the new "Needs you" line only |
| `poc/core.js` | Shared example data (today / future), words, the r03b2 shell with the new sidebar item, review bar |
| `poc/admin.css` | r03b2's stylesheet, **byte-identical** |
| `poc/claims.css` | Additions only: sidebar count, tabs, queue table, decision column, pick list, spinner, Future tags, 44 px targets, one-column order |
| `poc/assets/` | r03b2's Geist fonts (with their OFL files) and emblem; the four cake photos downscaled to 240 px |
| `shot.html`, `shoot.sh` | Screenshot tooling (frames a page at an exact width) |
| `check.html` | Scripted checks (drives the prototype in a same-origin iframe; see Checks) |
| `screenshots/` | Desktop (laid out at 1440 px, saved at 1120 px) and mobile (390 px) JPEGs |

The review bar switches the role (Chef, Fulfilment, Technical support) and the **example data** (Today, the
default, or "+ Future order-page requests"; kept for the tab). Depending on the page it also picks the list state
(queue), the **simulated API answer** (decision page) or the option A/B proposal (order page). Every simulated
action shows "Prototype: nothing is saved", and the bar repeats "Proposal … not approved". The preview clock is
Fri 6 Nov 2026, 10:30 London time. Order references are the live short IDs (the first 8 characters of the order's
UUID); the guest order page shows the full ID as "Order reference".

## The three screens

### 1. Requests to add orders (queue)

- **Where it lives:** a new chef-only sidebar item, **Account requests**, with a count; the page title is
  "Requests to add orders". It's recommended over a group in Needs attention (question 1). Today's "Needs you"
  gets one summary line (S45).
- **Tabs:** Waiting (default, oldest first), Added, Couldn't add, Withdrawn (the API states `pending`, `approved`,
  `declined`, `withdrawn`). The decided tabs are oldest first, as the API returns them; newest first is proposed.
- **Each row:**
  - Received (date, time and the request's short reference, which the customer also sees).
  - Who: profile name, Instagram, phone, labelled "(their profile)"; the footer says they're typed by the
    customer, not checked, not proof, and that the workspace doesn't store the customer's email.
  - How: the channel ("Instagram" / "Email, phone or in person" / "On this website"), "Their reference",
    "Roughly <month>", and "Looks like a website order reference" where it does. Future rows read "From the order
    page or email" with the order.
  - Their note (clamped).
  - Waiting time (amber from 3 days).
  - Review / View.
  - Decided rows add the outcome, when, "by you", the internal note, and "Account deleted since: the order is no
    longer in any account" where proposed data says so.
- **States:** loading, couldn't load, nothing waiting, and more than a page (live: 50 at a time from
  `next_cursor`).
- A banner says only website orders can be added for now: Instagram, email, phone and in-person orders aren't in
  the system yet (GK-027/GK-038), while guest website orders sent with their reference can be found and added.

### 2. Decide a request

**Layout.** On desktop the facts are on the left and "Your decision" is sticky on the right. On one column the
decision follows the main card.

**Older-order request (today).** "What they sent" (channel, their reference, roughly when, note), "Who's asking ·
from their profile", then **Find the order**:

- **Search:** the start of the order ID, or the name on the order. Two "Search for" buttons fill in their
  reference (the full order ID works) or their surname; the chef presses them herself.
- **Months:** handover from/to, starting at their month ±1, up to 3 months. Every order in those months is loaded
  before filtering.
- **Results:** a radio list: "choosing one is your judgement"; "this list can't show whether an order is already
  in someone's account".
- **No match** ("No matching order in the system yet"): Decline (with the trade-off in the reviewer's words) or
  Leave it waiting.

**Order-page request (Future).** A Future banner, "The order they asked from" (summary and link), the same-order
banner when two requests name it, then what they sent and who.

**Approve.** The confirmation says, in plain words:

- the order appears in their account;
- guest access stops working (today, the order page in the browser that placed it; later also the order-email
  links), and they see it by signing in;
- their request shows "Added to your account";
- if it's already in their account it's just marked added;
- there's no undo here.

There's an optional internal note.

**Decline.** The confirmation says:

- nothing is added;
- they see only "Couldn't add … from the details given" and are asked to contact us, with no reason and nothing
  about any order;
- on the future path, their guest access keeps working.

There's an optional internal note.

**After saving.** Busy labels read "Adding…" or "Declining…". On success the header changes, the left column
becomes read only, focus moves to the result, and there's a "Next waiting request" button.

**Refusals.** Each says "Nothing changed":

| Refusal | What the chef sees | Next |
| --- | --- | --- |
| `ORDER_OWNED` | Already in another customer's account; the workspace doesn't say which | Choose another order, or Decline |
| `STALE_VERSION` / `CONFLICT` | Decided elsewhere, withdrawn, or closed because the customer deleted their account | Show the latest (needs the proposed read): withdrawn (S26) or closed (S44) |
| 422 | Older order: "That order can't be found any more" (Search again). Future path: "That isn't the order they asked from" (Reload) | As shown |
| `ACCOUNT_UNAVAILABLE` | Their account is disabled | Decline |
| 503 | Paused while the data is restored | Try again |
| Answer lost | "We couldn't tell whether it was saved"; trying again is safe | Try again (same key) |

### 3. Add an order to a customer's account (chef-initiated)

The API exists (`POST /admin/orders/{id}/owner` with `account_id | account_subject` and evidence). But the customer
can't see any account identifier, and looking an account up by email is account discovery (not allowed). The
order page gets a **Customer account** card, tagged "proposal · not approved":

- **A, recommended, but needs GK-016 wording:**
  - The chef asks the customer to send the order's reference through "Add an older order", then adds it from the
    request. There's a copyable reply: "Prototype: nothing is copied or sent".
  - A warning states the prerequisite: today's form is written for Instagram, email or in-person orders, with
    "Order reference from the chef (e.g. IG-0613)" and no website choice. Question 6 must be settled first.
  - Until then, the reply explains the workaround ("Email, phone or in person", plus the "Order reference" from
    the order page).
- **B, needs a GK-016 change (no API change):** the customer's profile shows "Your account ID" (their provider
  subject). The chef pastes it with a required evidence line and confirms. Nothing about the account is shown
  before or after adding. Answers: added, `DUPLICATE`, `ORDER_OWNED`, 422 "no active account has that ID",
  `STALE_VERSION`, 503.
- **When the order is already in an account** (S41; assumes the proposed yes/no): "This order is in a customer's
  account. Nothing to add here."

## States table

C = chef. Fulfilment and technical support see only a role note on these pages, and no sidebar item (S09).
"Simulates" is the live call the state stands for; the prototype makes no call. **F** = Future (not possible
today).

| State | What the chef sees | Buttons | Destination | Simulates |
| --- | --- | --- | --- | --- |
| S01 Queue · Waiting | 5 older-order requests, oldest first; two flagged "Looks like a website order reference" | Review; tabs | S17/S18/S46 | `GET /admin/order-claims` (state=pending) |
| S02 Loading | "Loading requests…" + skeleton | — | — | the same, in flight |
| S03 Couldn't load | Alert, nothing changed | Try again | S01 | failed (network/5xx) |
| S04 Nothing waiting | Empty text: how customers ask today | tabs | — | `{claims: []}` |
| S05 More than a page | "Showing the oldest 5 · more below" | Show the next requests | rows appended, focus to the first new row | `?cursor=<next_cursor>` |
| S06 Added | Outcome, when, by you, internal note; "Account deleted since" | View | S31/S33 | `?state=approved` (+ proposed `account_deleted`) |
| S07 Couldn't add | The same for declined | View | S32 | `?state=declined` |
| S08 Withdrawn | "Withdrawn by the customer" | View | read only | `?state=withdrawn` |
| S09 Other roles | Role note: chef only (403) | — | — | 403 `FORBIDDEN` |
| S10 **F** Order-page request | Future banner; the order they asked from | Add to their account; Decline | S11 / decline | (path a row) |
| S11 **F** Confirm add | Consequences (guest access), optional note | Yes, add…; Not yet | S12 → S13 | — |
| S12 **F** Adding | "Adding…" | — | S13 or refusal | `POST …/decision {expected_version, decision: approve, order_id, note?}` |
| S13 **F** Added | Result | Next waiting request | — | 200 |
| S14 Decline (Instagram) | Consequences + optional one-line note (counter in code points) | Decline request; Not yet | S15 / S17 | — |
| S15 Declined | Header "Couldn't add"; internal note echoed | Next waiting request; Back | — | `decision: decline, note?` → 200 |
| S16 **F** Same order twice | No profile name; amber banner naming the other request | as S10 | — | two path-a rows with one `order_id` |
| S17 Instagram, no match | Find the order (May–Jul 2026): none; guidance with the decline trade-off | Decline; go back | S14 / S01 | `GET /admin/orders?from&to` (all pages, empty) |
| S18 Guest website order | Their reference is a full order ID; orders Sep–Nov listed | Search for … | S19/S20 | `GET /admin/orders` (all pages) |
| S19 Two matches | "8b2" finds 8b2a1f03 and 8b20d4e6 | choose one | S20 | browser filter |
| S20 Chosen | "Chosen: Raspberry ribbons · Sat 17 Oct …" | Add order 8b20d4e6…; Decline | S21 | — |
| S21 Confirm add | Consequences true today + optional note | Yes, add…; Not yet | busy → S22 | — |
| S22 Added | "The order you added" + result | Next waiting request | — | `decision: approve, order_id` → 200 |
| S23 In another account | Refusal (Daniel Okafor, a41c09e7) | Choose another order; Decline | — | 409 `ORDER_OWNED` |
| S24 In another account (choose another) | Refusal | Choose another order; Decline | S19 / S14 | 409 `ORDER_OWNED` |
| S25 Changed meanwhile | Decided, withdrawn or closed; nothing saved | Show the latest | S26 | 409 `STALE_VERSION` / `CONFLICT` |
| S26 Latest: withdrawn | Read only | Back to the list | S08 | proposed `GET /admin/order-claims/{id}` |
| S27 **F** Not that order | Only the order they asked from can be added | Reload | S10 | 422 `INVALID_INPUT` (order_id) |
| S28 Account disabled | Nothing can be added | Decline; Back | — | 409 `ACCOUNT_UNAVAILABLE` |
| S29 Paused | Restore in progress | Try again | busy → S22 | 503 `UNAVAILABLE` |
| S30 Answer lost | Couldn't tell if saved; retry is safe | Try again | busy → S22 | network loss; same key replays |
| S31 Decided · added | Outcome, internal note, customer sees | Back to the list | S06 | approved row |
| S32 Decided · couldn't add | Outcome, internal note, the exact customer words | Back to the list | S07 | declined row |
| S33 Added, account deleted since | "The order is no longer in any account"; note erased; customer sees nothing | Back to the list | S06 | approved row + proposed `erased`/`account_deleted` |
| S34 Chef add · A | Steps, copyable reply, GK-016 prerequisite warning | Show a reply; Copy | — | none (uses S17–S22) |
| S35 Chef add · B form | Account ID + required evidence (separate errors) | Add to their account | S36 | — |
| S36 Chef add · B confirm | Consequences (guest access); "can't show whose account" | Yes, add…; Not yet | S37 | `POST /admin/orders/{id}/owner {expected_version, account_subject, evidence}` |
| S37 B added | Added; guest access stopped | — | — | 200 `{order_id, linked}` |
| S38 B unknown account | No active account has that ID | Back to the form | S35 | 422 (account_id) |
| S39 B another account | Already in another account | Back to the form | S35 | 409 `ORDER_OWNED` |
| S40 B already there | Already in that account | — | — | 409 `DUPLICATE` |
| S41 Order in an account | "This order is in a customer's account. Nothing to add here." | request link | S31 | proposed chef-only yes/no (question 8) |
| S42 **F** Queue with future rows | 8 waiting; 3 tagged Future, with a note | Review | S10/S16 | path a rows |
| S43 Changed: account deleted | Same stale message | Show the latest | S44 | 409 `STALE_VERSION` (claim closed by deletion) |
| S44 Closed | "Closed · account deleted"; nothing added; not in any list | Back to the list | S01 | proposed `GET /admin/order-claims/{id}` (state closed) |
| S45 Today fragment | "5 requests to add orders to accounts · Oldest 3 days" | Review requests | S01 | the first queue page |
| S46 Guest website order (upcoming) | Reference a41c09e7 flagged; orders Oct–Dec | Search for … | S23 path | `GET /admin/orders` |

## Screen-to-API mapping

| Screen element | API (truth: api/README.md, 0033) | Notes |
| --- | --- | --- |
| Queue tabs, rows, paging | `GET /api/v1/admin/order-claims?state=pending\|approved\|declined\|withdrawn&cursor&limit` → `{claims, next_cursor?}`, oldest first, `limit` 1–50 | Row: `id`, `version`, `state`, `path`, `channel`, `reference`, `approximate_date`, `note`, `order_id`, `created_at`, `decided_at`, `decided_by`, `decision_note`, `account {id, full_name, instagram, phone}`. `closed` is refused (422) |
| "How" label | `path` first, then `channel` | `email_link` = Future; `evidence` + `website_email_link` = "On this website" |
| One request (`request.html?id`, Next waiting, same-order link, order page link, Show the latest) | **none today**: page through the four lists | **Proposed:** `GET /api/v1/admin/order-claims/{id}` (chef only, every state including `closed`) |
| Erased / account deleted | not in the row (reference, note and date null, `decision_note` null) | **Proposed:** `erased` (or `erased_at`) and `account_deleted` on staff rows |
| Sidebar count, Today line | (no count route) | Count the first page; "50+" with `next_cursor` |
| Decided tabs | oldest received first | **Proposed:** newest decided first for approved, declined and withdrawn |
| "by you" | `decided_by` (an account ID) = the signed-in chef | No staff-name lookup |
| Order summary (future path) | `GET /api/v1/admin/orders/{id}` (chef detail) | |
| Find the order | `GET /api/v1/admin/orders?from&to` (≤93 days, 100 a page): load every page (up to 10, like the built Orders page), then filter by ID prefix or `customer_full_name` | Over 1,000 orders: ask to narrow the months (the built page's rule) |
| Approve | `POST /api/v1/admin/order-claims/{id}/decision {expected_version, decision: "approve", order_id, note?}` + Idempotency-Key → 200 `{claim_id, state, order_id}` | Links the order, bumps its version, revokes every guest capability (today, the placing browser's), audit `order.owner_linked`. Already in the claimant's account: approved, nothing revoked |
| Decline | the same with `decision: "decline", note?` | Nothing linked. `decision_note` 1–500, one line; internal; erased on account deletion |
| Refusals | 409 `ORDER_OWNED`, `ACCOUNT_UNAVAILABLE`, `STALE_VERSION` (includes a claim closed by deletion or withdrawn), `CONFLICT`; 422 `INVALID_INPUT` (`order_id`, `note`); 503 `UNAVAILABLE`; 404; 403 | `KEY_REUSED` shouldn't happen (keys below) |
| "Is this order in an account?" (order page, find list) | not in staff views (`added_by` only in the customer summary) | **Proposed:** chef-only yes/no plus how and when, never whose (question 8) |
| Chef add (option B) | `POST /api/v1/admin/orders/{id}/owner {expected_version, account_subject, evidence}` → 200; 409 `DUPLICATE`, `ORDER_OWNED`, `STALE_VERSION`; 422; 503 | Needs GK-016 to show the subject on the profile |

**Keys.** One Idempotency-Key per decision attempt. It's kept for "Try again" after a lost answer or a 503. A new
key is made when the decision, the chosen order or the note changes.

## Chef words ↔ customer words

| State | Chef sees | Customer sees (built `claims.ts`) |
| --- | --- | --- |
| pending | Waiting · "Waiting N days" | "Waiting for the chef" |
| approved | Added to their account (+ "Account deleted since") | "Added to your account" |
| declined | Couldn't add (+ internal note) | "Couldn't add": "The chef couldn't add an order from the details given, so nothing was added. Please contact us if you think this is a mistake." |
| withdrawn | Withdrawn by the customer | "Withdrawn" |
| closed | Closed · account deleted | (no account) |
| path c `instagram` / `offline` / `website_email_link` | Instagram / Email, phone or in person / On this website | Instagram / Email, phone or in person / On this website |
| path a (future) | From the order page or email · Future | "Add an order to your account" |

## Decisions and deviations

- **Today first.** Default data is what customers can send now; the future path stays designed but labelled.
- **Own sidebar item**, not Needs attention, which is technical support's home (support gets 403 here).
- **44 px targets in this flow.** `claims.css` raises them without changing a token. Inline links are exempt, and
  the prototype-only review bar keeps r03b2's smaller pills.
- **Live short IDs** replace r03b2's illustrative `GK-1036`-style numbers.
- **"Account requests" (nav) / "Requests to add orders" (h1).**
- **No automatic suggestions.** The chef presses "Search for …" herself. "Looks like a website order reference"
  is a format hint, not a match.
- **Profile data shown, labelled** (question 2).
- **One column order:** main card, then the decision, then the secondary cards.
- **Partial pages.** The order page and Today are fragments; the rest stays r03b2.
- **"Leave it waiting"** is a real option with no API call.

## Accessibility

- One `h1` per view. Every control has a visible label. The tables have captions and row headers and become
  labelled cards on mobile. Tabs are links with `aria-current`.
- Focus moves to the confirmation heading, the busy status and the result. Results use live regions: alert for
  refusals, status for success. The note counter isn't live; over-long notes raise an alert on submit.
- Option B's errors are tied to their own fields.
- Chips always carry words. Contrast: "how" chip 10.9:1, inactive tab count 7.4:1, internal note 9.3:1, warning
  text and Future tag 6.1:1, muted text 5.9:1, sidebar count 12.9:1.
- Reduced motion stops the spinner.

## Gaps

- **The built chef frame isn't on main.** It's on the unmerged `sasha-chycha/chef-ui-2` (a559e5d).
- **Path a doesn't exist for customers yet.** No UI route sends `X-Order-Capability` with a claim; the order-page
  button (GK-016) and order emails (GK-031) come first. The guest capability lives only in the browser that placed
  the order, so approving today revokes that page's access only.
- **No single-request read.** Deep links and "Show the latest" depend on it (proposed).
- **No erased / account-deleted flag** in staff rows (proposed). Today an erased request shows empty fields
  without saying why.
- **Closed requests vanish.** Requests closed by account deletion appear in no list; deciding one gives
  `STALE_VERSION`.
- **Ownership isn't in staff order views** (question 8).
- **Decided lists are oldest first** (newest first proposed).
- **No count route, no staff names** for `decided_by`.
- **No undo:** removing an order from an account isn't built.
- **No customer notification** (no order emails yet, GK-031).
- **Historical orders** don't exist (GK-027/GK-038).
- **Repeated requests aren't rate-limited** (API decision).
- **Option A's wording, option B's account ID and the customer notice** (questions 6, 3 and 10) are GK-016
  changes that aren't designed on the customer side here.

## Open questions for the Creator

1. **Where should the queue live?** Recommended: its own chef-only sidebar item, "Account requests", with a count,
   plus the Today line (S45). Needs attention is technical support's home, and support can't open requests.
2. **Show the customer's phone and Instagram in the queue?** Recommended: yes, labelled "from their profile, typed
   by the customer, not proof"; it's the only contact detail the chef has. Note the difference between display
   and data: the API sends name, phone and Instagram on every row whatever the UI shows. Hiding them in the list
   is a display choice only; minimising the data needs an API change.
3. **Chef-initiated add: A or B?** Recommended: **A**, but A means changing live customer wording (a GK-016
   amendment). Today the "Add an older order" form is written for Instagram, email or in-person orders, asks for
   the "Order reference from the chef (e.g. IG-0613)" and offers no website choice, while its side panel promises
   "She can add an order for you". Question 6 is a prerequisite of A. B (a customer-visible account ID) is a
   bigger GK-016 change, and it puts a 36-character ID in customer messages. Looking an account up by email is not
   proposed.
4. **Instagram and offline requests before older orders exist: decline now, or leave them waiting?** Recommended:
   decline, with an internal note. The trade-off: the customer sees "The chef couldn't add an order from the
   details given…", which can read as if their details were wrong, and once GK-027/GK-038 exist they'd have to
   ask again. Leaving them waiting keeps them in "Waiting for the chef" for months and holds 1 of their 5
   requests. Sub-question: should the customer page say for now that only website orders can be added (GK-016
   copy)? Recommended: yes.
5. **Keep the chef's internal note, on both approve and decline?** Recommended: yes, optional on both. The API
   stores it for either decision, it's never shown to the customer, and it's erased if the account is deleted.
6. **Prerequisite of A: add "On this website (as a guest)" to the customer form** with the order reference, and
   reword the title and hint for website orders? The API already accepts that channel on path c. Recommended: yes,
   as a small GK-016 amendment, until the order-page button exists.
7. **Two accounts asking for the same order** (future path)? Recommended: no automatic rule. The chef decides one;
   adding it for the other is refused with `ORDER_OWNED`, and she declines it.
8. **Should the chef (only) see whether an order is already in an account** (yes/no, how and when, never whose)?
   Recommended: yes, as an API follow-up for the chef's order views, not technical support's or fulfilment's.
9. **Waiting-time highlight** (amber after 3 days)? Recommended: keep it visual only, and promise customers no
   time.
10. **Tell customers what the chef sees?** Recommended: add one line to the "Add an older order" form: "The chef
    sees your profile name, phone and Instagram with this request." A GK-016 wording change.

The API follow-ups (a single-request read, the erased / account-deleted flag, newest-first decided lists, the
in-an-account yes/no) are for the planning master to schedule, not Creator questions.

## Checks run (r04b, 2026-11-06)

- `node --check` passes on every `poc/*.js` (core, requests, request, order, today, index, states).
- **Rendering.** Headless Chrome (`--dump-dom` with console logging) rendered the 46 states plus the index as the
  chef, fulfilment and technical support: 141 page loads. Every load had exactly one `h1`, no duplicate IDs and no
  script or console errors.
- **`check.html`** (same-origin iframe, chef, data=today unless named), 90 checks, all PASS:
  - **Data.** Today data has 5 older-order rows and no email-link rows; references that look like an order ID are
    flagged. Future data tags 3 rows with the note, and order-page requests carry the Future banner.
  - **Approve.** The confirmation names the order and account and takes focus. Its wording is true today and
    doesn't say "emails stop working". A note over 500 code points is counted and refused with an alert and
    `aria-invalid`. Then "Adding…", the result echoing the note with focus on it, the header showing "Added", the
    prototype label, and "staff account" wording.
  - **Instagram request.** The no-match guidance includes the trade-off. The decline counter works and isn't a
    live region, and the declined result echoes the note.
  - **Find the order.** Add is disabled until an order is chosen; "8b2" finds two; choosing one enables Add with
    its ID; "their reference" (a full order ID) finds the one order; more than 3 months is refused.
  - **Refusals.** `ORDER_OWNED`. A lost answer, then a retry that succeeds. The stale message names account
    deletion, and "Show the latest" shows closed.
  - **Decided and paging.** Added-then-deleted shows the order in no account. Paging adds 3 rows.
  - **Roles.** Technical support gets no item and a role note.
  - **Order page.** An order in an account hides the how-to; option A states its GK-016 prerequisite. Option B
    puts the missing-evidence error on the evidence field (`aria-describedby`, `aria-invalid`), refuses an empty
    account ID, and confirms.
  - **Today.** The fragment line shows.
  - **Sizes and layout.** Controls are at least 44 px on the queue, find and order pages. At 390 px the decision
    follows "Find the order", before the profile, and "Account requests" is scrolled into view in the top bar. All
    46 states have no horizontal overflow at 390 px.
- **Screenshots.** `shoot.sh` with headless Chrome took 33 states (desktop + mobile, 66 JPEGs), all re-shot because
  the review bar changed on every page. Looked at: S00, S01, S10 (desktop and mobile), S17 (mobile), S21, S33,
  S34, S44, S45 (desktop and mobile) and S46 (mobile). Fixes after looking:
  - the Future banner said "Future" twice;
  - the closed view said "Already decided" and linked to an invalid tab;
  - the Today design note touched the card.
- `python3 scripts/check_planning_kit.py` passes (run before the commit).

## Screenshots (`screenshots/`)

`<state>-<slug>-desktop.jpg` / `-mobile.jpg` for:

- S00 index;
- S01–S07, S09, S42 (queue), S45 (Today);
- S46, S17, S19, S21, S22, S14, S15 (older-order requests);
- S23, S25, S28–S30, S44 (refusals and closed);
- S32, S33 (decided);
- S34–S36, S38, S41 (chef add);
- S10, S16 (Future).

The other states are reachable from the index.

## Not done

- No Creator review or approval; the re-review of r04b is pending.
- PR #59 (draft, pushed by the master) isn't edited by me; suggested description text is above.
- No Trello change.
- No customer-side changes (questions 3, 4, 6, 10 would be GK-016 amendments).
- No API changes (the proposed follow-ups).
- No manual or historical order recording (GK-027/GK-038).
- No implementation in `ui/` or `api/`.
