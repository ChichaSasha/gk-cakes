# GK-016 r01 — discovery, cake page, custom request and shared date-and-time picker (first sketch)

2026-11-04. Card GK-016 (Owner: Agent, Kind: Design). r01 covers **browsing and custom
requests**: one shared date-and-time picker, the cake page, the custom request and its Instagram/email handoff,
a public "Sizes and starting prices" guide and reorder from history. Account pages (sign-up/sign-in with Google,
history, claims, favourites merge, Instagram handle) are r02. **Not approved.**

## Execution check

Role: GK-016 design worker. Card policy requests gpt-5.6-sol/high in a fresh worker; this sketch was produced by
**claude-opus-5-5, host-reported** (`llmplatform/claude-opus-5-5`), in a fresh scoped worker session —
**model mismatch; the current user-selected model takes precedence**. Not an independent review.
Writes: this folder only, on branch `sasha-chycha/gk-016-discovery-design` from main `e0d8bc4`.
Nothing pushed; no Trello, GitHub or Slack changes.

## Inputs (SHA-256, first 16 hex)

| File | SHA-256 |
| --- | --- |
| `AGENTS.md` | `73502bdeedc5697d` |
| `skills/gk-feature/SKILL.md` / `references/design-sessions.md` | `b5a68cb7c520cf1f` / `915e72b29842e6b4` |
| GK-016 card description (Trello export used by this worker) | `47b4b38c56c8b81c` |
| GK-016 comments export: "GK-016 preparation / 2026-11-04" (`6aeac5bdc096768bb78b251e`) and "GK-016 Creator answers / 2026-11-04" (`6aeb3137ddc723cd2158ab84`) | `139dce13c786b3c1` |
| GK-015 r12 `session.md` / `output-manifest.json` (approved visual foundation) | `76a002210540fd2c` / `fdadcfd144388c0e` |
| GK-017 r01 `session.md` / `poc/app.js` / `poc/styles.css` (base of this sketch) | `e4617c32417b8cdc` / `07834eae2fb1970a` / `6a54c554e5c7bc95` |
| `docs/creator/GK-001/decisions.json` (input_revision `3797d07285893423`) | `6d3112d834930504` |
| `docs/creator/GK-002/README.md` | `b5327c013772a753` |
| `docs/creator/GK-005/decisions.json` (input_revision `eee247cc0d3d8cfc`) | `d6593cfc91948c32` |
| `docs/design/contracts.md` r6.5 (K01, K02, K06) | `b34b0926f371683a` |
| `docs/design/screens.md` / `docs/design/owner-data-contract.md` | `d2a9c9db693ab865` / `e71792978a198263` |
| `api/README.md` (Catalog, Schedule/capacity, Discount codes and quotes) | `3bcc9cbbae5391d3` |

Owner decision revisions used (first 16 hex of `revision_sha256`): GK-001 work_week `096b57e9ccc7d044`,
pickup_hours `7b9d9f3ed6a8545d`, pickup_slot `d1ddb32d476bd41f`, lead_time `6aa8767533ab0fb3`, booking_horizon
`73022a4f8f95b17c`, wedding_recommendation `e286e04efd417284`, daily_limit `ad07906a20af0395`, fulfillment_modes
`7c995b690d3ec1e6`, customer_courier `1dae8c235a2ea77f`, gk_delivery `ebecd9fd00441cb5`, move_time
`7f8b0043a7b01e95`; GK-005 allergens `3dade43fe5452311`, cross_contact `ea64ed35e3a1b117`, flavours
`40dd38fa5b00dd83`, browser_analytics `4857b543dcb41145`, retention_photos `88d81500fac584c2`, coating_decor
`69be0d5f064eeeb7`, public_identity `f45946aaa4ef16d9`.

Note: `api/README.md` on main has no "Orders" section yet (`/checkouts` is not documented), so the sketch mirrors
`/cakes`, `/availability`, `/schedule`, `/ready-cakes` and `/checkout/quotes` only.

Outputs: `poc/app.js` `63fcd4f1546cbb10`, `poc/styles.css` `cad781f8676433cc`, `poc/sizes.html` `a87e4180068164d8`,
`poc/reorder.html` `ec6ed774f4de949d`; the other `poc/` files and `poc/assets/` are byte-identical copies of GK-017 r01.

## What changed against the base (GK-017 r01 poc)

**Design-review bar** (under the demo bar): "Show state" picks a demo state for the page (also `?demo=…`), and the
bar shows the example "today". Dates are worked out from the real current date in London; `?now=YYYY-MM-DDTHH:MM`
(London time) pins it, e.g. `?now=2026-11-05T12:00` makes **Fri 13 Nov** the first bookable day, as the preparation
comment expects.

**(a) One shared date-and-time picker** (cake page, shop, bag and checkout "Change date"; the custom request uses
the same calendar and time list in "request" mode):

- Calendar Thursday to Sunday; Monday to Wednesday show "Closed"; past days are disabled; days whose every start is
  less than 192 hours away show "Too soon"; days after the example last bookable date (today + 2 months, labelled
  "the chef sets it") show "Not open"; open days show **places left** ("3 left", "1 left", "Full") from an example
  7-a-day limit. Ready-made cakes don't use places (K01/API).
- Collect · Free / Own courier / GK delivery · from £100 switch. GK delivery times are labelled **"Requested, the chef
  confirms"**; collect/courier times "Available".
- Start–end times appear automatically when the picker opens and whenever the date, mode or cart changes — no
  "Check" button. Radio list (keyboard arrows), visible focus, live status. Loading (skeleton + "Loading times for
  …", `aria-busy`), no times ("No times left on … Choose another date, or contact the chef." + "Try [next dates]"
  buttons and a chef-contact link, never moving the date silently) and error ("We couldn't load the times… Your other
  choices are kept." + Try again) are distinct. Obsolete responses are discarded. Earlier same-day times within 192
  hours are hidden with a note.
- A taken time answered with fresh choices: "Day fills up while choosing" → "Sorry, Fri 13 Nov has just filled up…
  Your cake and how you'll get it are kept. Please choose another date; we haven't picked one for you." The day turns
  "Full" and next dates are offered. "Chosen time passes the 192-hour notice" → that time disappears and the
  remaining times on the same day are shown. No per-slot cap is implied (there is none).
- Times are 60 minutes, ending within pickup hours: Thu 11:00–16:00 starts, Fri 10:00–16:00, Sat 12:30/13:00/13:30/
  14:00, **Sun 11:00 only**; every picker says Thursday, Friday and Sunday times are examples.
- **Checkout:** the GK-017 time dropdown is replaced by the same radio list (GK-017 note: radio list, not dropdown)
  with the same loading/no-times/error states. Everything else in checkout is unchanged: section order, pay for the
  cake now/delivery quoted afterwards, payment methods, refund wording, payer fields, discount rules and summary
  promises. The time is still retained across collect/courier/delivery when available, and the refund deadline
  still follows the chosen time.

**(b) Cake page:** Snickers shows a prominent black **"Contains PEANUTS"** panel under the price (plus the shop card
"Contains peanuts" and the full contains line before Add to bag); "Not available right now" (Raspberry ribbons);
"This cake is no longer in our collection" for an archived cake (state `archived`: no photo, request-something-
similar and browse buttons, note that an earlier order can be used after sign-in); ready-made example (Chocolate
swirl, "Ready-made · 1 left (example)", doesn't use a day's place, 192 hours still apply unless the chef approved a
shorter lead); an example second size chosen by the chef for Pink piping (S £90 / M £105, base prices).
**"Request changes to this design"** (inspiration: "Request this design") links to the request with the exact photo
revision (`custom.html?ref=pink&rev=3`) and says "Your request will include this exact photo (photo version 3)".
State `image-changed`: the chef updated the photo (version 4, mirrored in the demo so the change is visible); new
requests carry version 4, a request already started keeps its photo.

**(c) Custom request:** a pinned starting-point photo with its version and "We won't swap it without asking you";
"image changed" (both photos side by side, "Keep the photo I chose" / "Use the current photo", kept by default) and
"photo no longer available" (must choose the current photo or none before review) states. Date calendar Thursday to
Sunday from tomorrow up to 8 months; dates under 8 days or more than 2 months ahead are allowed with "the chef will
confirm" (✱ in the calendar, a note under the field); a "This is a wedding cake" tick explains wedding dates up to 8
months. All three handover modes with a note each; optional preferred time from the shared list (example times,
"the chef confirms"). Servings, flavour (Snickers shows "Contains PEANUTS"), colours, description, own photos
(private design references; finished-cake photo permissions are asked later on the order page). Review shows a
**summary with request reference** (`GKR-XXXX`, version), a **thumbnail** of the pinned photo (or the customer's
first photo, or "No photo"), the key facts, "Not sent yet…" and the selectable request text.
**"Copy request & open Instagram"** (@gk.cake.london) and **"Copy request & open email"**
(gk.cake.london@gmail.com, subject "Cake request GKR-XXXX"); the chosen channel is the primary button. Outcomes:
copied (+ "Instagram didn't open? Try again"), copy failed (text selected for manual copy), app didn't open (Try
again), both failed (request kept, both contacts). Every outcome repeats "Same request GKR-XXXX · attempt n: trying
again doesn't create a new request. Nothing has been sent." Editing after review keeps the reference and makes
version 2. **Draft restored:** the draft (no photos) is kept on this device for 30 days; returning shows "Draft
restored. We kept your request from … Photos you added aren't kept, so please add them again." with Start again.

**(d) Sizes and starting prices** (`sizes.html`, linked from footer, shop, cake page, request): GK-002 single-tier
and tiered starting prices, "the chef chooses the sizes each cake comes in and sets its price", decoration shown
included, larger sizes by request, some decoration costs extra.

**(e) Reorder from history** (`reorder.html`; signed-in example account → "Order again"): per cake "Same"/"Changed"/
"Allergen"/"Unavailable" tags (photo updated, now ready-made with 1 left, PEANUTS re-check, Raspberry ribbons can't be
added → "Request changes to this design"), old total £285 vs today £180 "checked again at checkout", and "Not copied:
date and time, discount code, photo permissions". "Add 2 cakes to bag" adds only the available cakes and clears the
date so a new one must be chosen.

**Account and favourites (kept minimal, consistent with answers 1–2):** signed out, the account page offers "Email me
a sign-in link" ("If you're new, the link creates your account") and "Continue with Google" (previews), explains
adding older orders (private order-email link, or reference + note checked by the chef, or ask the chef). Favourites
are saved on this device for a year (`localStorage` with expiry) and, in the signed-in example, an explicit "Add to my
account" / "Keep on this device only" choice.

## Contradictions from the preparation comment, fixed

| # | Was | Now |
| --- | --- | --- |
| 1a | Custom form offered closed days and past dates | Both calendars allow Thu–Sun only; past days disabled |
| 1b | Sunday 12:00–13:00 offered | Sunday 11:00–12:00 only; every slot ends within pickup hours |
| 2 | Custom form had Collection and Delivery only | Collect / Own courier / GK delivery everywhere |
| 3 | Peanuts not prominent on the cake page | Black "Contains PEANUTS" panel, contains line before Add to bag, shop card tag, request form flag |
| 4 | "Copy request & preview handoff" | "Copy request & open Instagram" (@gk.cake.london) and "Copy request & open email", with fallbacks |
| 5 | Inspiration tracked by cake ID only | `ref` + `rev` pinned through the request and summary; image-changed and unavailable states; never swapped silently |
| 6 | Favourites "in this browser tab" | "Saved on this device for a year", explicit merge into the account |
| 7 | Example dates in the past | Computed from today in London with 192 h lead and a 2-month example horizon |
| 8 | "Welcome back" while signed out | Signed-out page says "Sign in or create an account"; nothing implies a sign-in |
| 9 | (Flagged for GK-017) order page offered a time change on delivery orders | Delivery orders show no time-change button, only "contact the chef". Minimal change to the order page; GK-017 should confirm |

## Buttons and destinations

| Control | Prototype | Live site |
| --- | --- | --- |
| Show state (review bar) | Reloads the page with `?demo=` | Not present |
| Choose your date and time / Change date / Choose date | Opens the shared picker | Same, from `/availability` + `/schedule` |
| Collect / Own courier / GK delivery | Reloads times for the chosen date | `/schedule?fulfillment_mode=` |
| Calendar day | Selects the date; times load automatically | Same |
| Try [date] (no times) | Selects that date explicitly | Same |
| Contact the chef | contact.html | Contact page |
| Try again (times) | Refetches | Refetch |
| Use [date · time] | "Checking your time…", saves date/time/mode and reloads; demo states show taken/too-soon with fresh choices | Server revalidates slot at quote/checkout; 409 with fresh choices |
| Size (S / M) | Switches to that size's listing | Variant of the listing |
| Add to bag · £X | Adds to the demo bag (needs a date) | Same, then quote |
| Request changes to this design / Request this design | custom.html with `ref` and `rev` | Custom request with paired inspiration/image revision |
| Keep the photo I chose / Use the current photo / Continue without a photo / Remove this photo | Changes the pinned photo explicitly | PATCH with expected_version |
| Review my request | Validates (error summary), creates or keeps the reference, shows the summary | POST /custom-requests + summary freeze |
| Copy request & open Instagram | Copies (or simulated failure), shows the outcome; opens nothing | Clipboard + `https://ig.me/m/gk.cake.london` in one gesture |
| Copy request & open email | Same; opens nothing | Clipboard + `mailto:gk.cake.london@gmail.com?subject=Cake request GKR-XXXX` |
| Try again (handoff) | Repeats with the same reference | Same request, no new one |
| Edit request / Start a new request / Start again | Back to the form / clears the draft | Same |
| Sizes and starting prices | sizes.html | Public guide |
| Order again (signed-in example) | reorder.html | Owner-only history reorder |
| Add 2 cakes to bag (reorder) | Adds available cakes, clears date, goes to bag | New quote at today's prices |
| Email me a sign-in link / Continue with Google | Status "Preview… Nothing is sent/opened" | r02 (GK-021) |
| Add to my account / Keep on this device only | Status message | Per-item favourite PUT (K02) |
| Heart (save) | Saves on this device for a year | Device list; merge on sign-in |

## Owner rules applied

GK-001: Thu–Sun; pickup Thu 11:00–17:00, Fri 10:00–17:00, Sat 12:30–15:00, Sun 11:00–12:00; 60-minute appointments
ending within the hours (Saturday starts 12:30–14:00 exact); 192-hour lead (equality allowed); ordinary horizon two
months (example date, chef-set), wedding up to 8 months; 7 standard places a day (example remaining counts; never
negative); three modes — free collection at St Katharine Docks, customer's own courier (responsibility passes at
handover), GK delivery from £100 quoted by the chef and paid separately; no delivery self-service time change.
GK-002: sizes and base prices (Mini 5in 3–4 £70, S 6in 7–10 £90, M 7in 12–14 £105, L 8in 17–20 £130, XL 10in 30–32
£180, XXL 12in 40–45 £245; tiered from £180/£250/£320/£330/£600), decoration shown included, approved example cakes.
GK-005: EGGS, MILK, CEREALS CONTAINING GLUTEN (WHEAT), SOYBEANS for every flavour; Chocolate Oreo adds SULPHITES;
Snickers adds PEANUTS, prominent; the exact shared-kitchen notice; favourites on the device for 1 year; drafts 30
days; personal photos not kept in the browser. K01/K02/K06 as listed above.

## Creator answers applied (comment "GK-016 Creator answers / 2026-11-04")

1. Email sign-in link creates the account on first use; plus Continue with Google — shown on the signed-out account
   page (preview; full design r02).
2. Older orders: private email link while signed in; old Instagram/offline orders by chef reference + note approved by
   the chef; the chef may add orders on request — explained on the account page; the short-proof "contact us" path is
   r02.
3. The chef chooses sizes and prices per listing — Pink piping example S/M; public "Sizes and starting prices" guide.
4. Uploads are private design references; finished-cake photo permissions belong on the order page — stated under the
   upload field (order-page permissions not designed here).
5. Custom-request dates Thu–Sun; under 8 days or beyond 2 months allowed with "the chef will confirm"; weddings up to
   8 months — request calendar and notes.
6. Example start times in 30-minute steps (Thu 11:00–16:00, Fri 10:00–16:00, Sun 11:00 only), labelled as examples.
7. "Copy request & open email" to gk.cake.london@gmail.com with the reference in the subject.

## Checks

Local headless Chrome walk-through (temporary harness outside the repo, not committed): 68 scripted checks plus
every page in every demo state render without errors, e.g. first bookable day Fri 13 Nov for `now=2026-11-05T12:00`;
Mon closed, Thu 12 too soon; Fri 13 times 12:00–16:00 starts (9) with the 192-hour note; Sunday only 11:00–12:00;
Saturday 4 times; delivery "Requested, the chef confirms"; horizon month limit; taken day → "Full" + next dates; too
soon → fresh same-day list; empty/error/loading distinct; retry works; obsolete response discarded; calendar arrows
skip closed/full days; peanuts panel; request link carries `rev=3`; archived/unavailable/image-changed; request
calendar ✱ and closed days; validation summary; reference format, button texts, summary text and thumbnail;
retry keeps the same reference (attempt 2); edit → version 2; draft restored after reload; favourite expiry ≈365 days;
explicit merge; no "Welcome back"; reorder → bag with 2 cakes; checkout radio list keeps 13:00–14:00 across modes and
drives the refund deadline; delivery order has no time change; no horizontal overflow at 390px on the main pages.
`node --check poc/app.js` passes. Screenshots were viewed locally only and are not committed. No independent review
and no Creator feedback yet.

## Open questions for the Creator

1. Custom requests that aren't weddings: may they also go up to 8 months ahead (as the sketch allows, with "the chef
   will confirm" after 2 months), or should they stop earlier?
2. The request draft keeps the description on this device for 30 days. Descriptions may mention allergies; is that
   fine, or should the description be left out of the saved draft?
3. Inspiration photos: is "Request this design" right for gallery cakes, with "Request changes to this design" for cakes
   you can buy?
4. Order page (flagged for GK-017): delivery orders now show "contact the chef" instead of a time change. Keep?

## How to view

`python3 -m http.server 8778 --bind 127.0.0.1 --directory docs/design/sessions/discovery-account/gk-016-r01/poc`
→ http://127.0.0.1:8778/shop.html, `cake.html?id=chocolate`, `cake.html?id=pink`, `custom.html?ref=pink&rev=3`,
`sizes.html`, `reorder.html`, `account.html`. Use "Show state" in the dark review bar for each state; add
`&now=2026-11-05T12:00` to pin the example date. "Clear demo data on this device" is on policies.html.
