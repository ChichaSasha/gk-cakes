# GK Cakes planning brief

Status: user-requested scope recorded on 2026-09-21; architecture and operating
policies remain to be designed. This brief supersedes conflicting scope in the
[unreviewed POC](../../innitial--unreviewed-dry-draft-poc-design.md).
The old document's Go, Supabase, Stripe, Render, guest-only, finite-stock and
sandbox-only choices are candidates, not accepted decisions.

## Product and experience

Two product surfaces: GKCake.com for customers and an admin UI for a nontechnical
chef. Domain ownership/availability is unverified. Separately deployable UI and
backend are requested; admin placement and frontend technology are undecided.
PostgreSQL stores business records; file storage holds cake images. UI reads and
mutations use an authorised API or an explicitly reviewed restricted data API,
never browser-held database credentials or a privileged service key.

Aim for exceptional cake photography, easy discovery, understandable choices,
clear availability/pricing and a reassuring purchase experience. “Best on the
planet” is an aspiration, not a verifiable claim. Inventory actual design assets
first: the reference to ready-made web design does not prove screens exist.
If absent, create a proposed screen inventory and visual-direction/prototype
work; never invent Figma links or mark designs approved.

Design reviews should observe whether a new customer can find a cake, understand
servings/allergens, check a date early, distinguish buying from requesting a
bespoke cake, see total cost, and recover from an unavailable option. Chef tasks
include adding a manual order, changing capacity, checking pickup, tracing email,
and issuing a refund without database or payment-provider terminology.
Use representative customer and chef walkthroughs, mobile and desktop layouts,
keyboard/focus/error announcements, readable contrast, reduced motion, responsive
image loading and honest content. Agree usability/performance budgets during
planning; do not invent measured conversion improvements, reviews or scarcity.

For visual direction, research a small relevant set of current bakery/luxury-food
and ordering experiences, recording exact examples and useful interaction lessons.
Compare distinct directions using the chef's actual brand/photos and the same core
journey; explain the recommendation. Do not copy another brand or claim to have
surveyed every site. Propose concrete typography, spacing, colour, image treatment,
component states and responsive behaviour, then review a representative prototype
with the Creator before applying the direction across all screens. Use authentic
cake photography for purchase expectations; label conceptual generated imagery.

Interactive design sessions with the Creator are required future work. The agent
shows actual coloured sketches with buttons, receives feedback and revises them
until a specific version is approved. Plan separate design outcomes before
dependent UI implementation; a text-only description is not an accepted sketch.
Follow the [design-session contract](../../skills/gk-feature/references/design-sessions.md).

## Requirement ledger

These IDs are traceability keys, not Trello card IDs or a prescribed ticket count.
Every row needs a design, tests and ticket coverage, or an explicitly approved
deferral with reason. Group related rows into coherent outcomes.

| ID | Required outcome | Planning detail that must be resolved |
| --- | --- | --- |
| C01 | Browse photographs/examples of cakes made by the chef | Image storage, upload/admin flow, alt text, gallery versus currently purchasable products |
| C02 | Buy a matching cake or choose size/servings | Variant availability, recipe/pricing and order snapshots |
| C03 | Build a custom cake request and hand its summary to @gk.cake on Instagram | Required form fields, inspiration reference, draft recovery, user-sent handoff and failure path |
| C04 | Reorder a previous cake | Source is incomplete in original request; propose history and gallery entry points, revalidate current price/allergens/availability |
| C05 | Pay 100% for website orders | Price agreement, currency, payment confirmation, late/failed payment and retries |
| C06 | Customer cancellation/refund before production starts | Explicit eligible states; completed must not become eligible merely because it is not in_progress |
| C07 | Sign in and see own order history | Identity provider, guest checkout coexistence and safe linking of manual/guest orders |
| C08 | Track order status | Customer-friendly timeline and authorised access |
| C09 | See ingredients and allergens | Recipe/variant sources, changes, cross-contact wording supplied by chef |
| C10 | Check chef availability early in ordering | Production capacity, dates, lead time, blackout days and reservation policy |
| C11 | View a simple shareable chef schedule | Public availability only; no private order/customer data |
| C12 | Ask for changes to an inspiration cake | Preserve reference/image and requested differences through manual conversation |
| C13 | Save and revisit favourites | Account ownership, signed-out behaviour and archived cake handling |
| C14 | Receive confirmation/cancellation/refund emails | Durable intent, initiation versus completion notices, provider outcomes, reconciliation |
| C15 | Choose paid chef delivery or arrange delivery themselves | Service areas/fees, responsibility, details and effect on pickup workflow |
| C16 | Book a pickup time from separate pickup slots | Capacity distinct from production scheduling |
| C17 | Change pickup time within admin policy | Deadline definition, timezone/DST, simultaneous moves and policy changes |
| C18 | Save an Instagram handle in a profile | Validation, privacy; a handle does not prove Instagram identity or authorise API messaging |
| A01 | Admin cancels an order and returns full payment safely | Authorisation, reason, confirmation, progress/failure and reconciliation |
| A02 | Trace operations and investigate discrepancies | Business audit, request/provider references, redacted logs and actionable recovery |
| A03 | Admin manually updates order status | Allowed transitions/roles, stale views and customer notifications |
| A04 | Business activity and money-flow dashboard | Gross/net/refunds/discounts/fees definitions and data freshness; no invented accounting guarantees |
| A05 | Website activity such as daily users | Metric definition, privacy, existing analytics option and cost |
| A06 | See ingredients/products currently in the fridge | Units, adjustments, waste/expiry needs and stock source of truth |
| A07 | Enter orders agreed manually with customers | Safe customer association, agreed price/details, capacity and payment recording |
| A08 | Set working schedule and cakes-per-day capacity | Existing commitments and exceptions; count versus weighted capacity decision |
| A09 | Inspect customer email status | Queued, accepted, delivered-to-server, bounced, failed and unknown; never claim inbox receipt |
| A10 | Keep delivery easy to connect to future courier APIs | Small current boundary; no speculative courier engine or integrations |
| A11 | Compose a custom email to a customer | Preview, recipient checks, permissions, audit, send status; distinguish marketing |
| A12 | Set pickup scheduling/rescheduling policy | Allowed windows, cutoff, slot count and effects on already booked orders |
| S01 | Percentage/free-delivery discount codes | Separate discount records, eligibility, expiry/usage/stacking and order application snapshot |
| S02 | Partial refunds | Cumulative limits, allocation of tax/discount/delivery, retries and admin evidence |
| S03 | Cake recipes and ingredients support planned usage | Quantities by size, planned versus actual consumption, cancellation and substitutions |
| S04 | Plan groceries and budget over a date timeline | On-hand versus committed/planned demand, recipe version and cost assumptions |
| S05 | Admin defines available sizes/servings | Size-to-variant/recipe/pricing relationships and history preservation |
| S06 | Efficient, proportionate PostgreSQL design | Keys/constraints, query-driven indexes, snapshots, retention, backup/restore |
| S07 | Reliable operations | Durable side effects, retries, deduplication, observability and recovery ownership |
| D01 | PR → CI → auto-merge → main CI → release-only CD | Required checks/review; deploy tested release commit |
| D02 | Deploy UI and backend separately; protect secrets | Contract compatibility, migrations, rollback, environment and credential boundaries |
| UX01 | Iterate on visual design with the Creator and finish with ready coloured sketches and buttons | Visible mobile/desktop artifacts, editable source, button behaviour, feedback, exact-version approval and implementation handoff |

## Cross-feature invariants to settle together

- Distinguish inspiration, orderable product, custom request, quote and order.
  Trace custom request → conversation → agreed quote → admin-entered order →
  payment → customer history and fulfillment. Decide what creates a commitment.
- Separate order/production, payment, refund, fulfillment and notification states.
  One module owns each transition. Persist history with authoritative state.
- Calculate monetary values, discounts and availability on the server. Use
  integer minor units and currency; define rounding/allocation and immutable
  order totals. Pending/successful refund reservations must prevent over-refunds.
- Arbitrate cancellation versus production start atomically. Define refund
  eligibility positively, admin exceptions separately, and external refund
  uncertainty honestly. Never equate cancelled with money returned.
- Coordinate stock/production/pickup holds with payment deadlines. Recheck on
  commit, handle late payment and admin manual orders, and prevent overbooking
  across processes. Refund does not automatically restore perishable stock.
- Treat production date, pickup appointment and delivery availability separately.
  Rescheduling reserves the new slot and releases the old slot atomically.
  Calendar availability is advisory until reservation succeeds.
- Preserve purchased configuration, price, recipe/allergen and policy commitments
  when catalog/admin settings change. Reorders use current validated options.
- Commit required notification intent with the business transaction and use a
  provider for transport. Record retries and delivery evidence without promising
  an atomic transaction spanning email and PostgreSQL or guaranteed inbox receipt.
- Plan authenticated ownership, staff roles, public/private storage, secret
  handling and audit access. Operational logs must not expose personal data.

## Open decisions for Creator tickets

Ask only what blocks the current decision; do not make the Creator answer all at once.

1. Launch country, currency, shop timezone, pickup address, delivery area, legal
   business details, budget and expected scale. Verify local food/allergen,
   privacy, tax and refund requirements once jurisdiction is known.
2. Brand assets and actual screen source; whether visual design is already
   approved. Product positioning and authentic photography/content availability.
3. Ready-made stock, made-to-order cakes, or both; lead times/capacity units;
   definition of production start and rules for cancellation/admin exceptions.
4. Custom-request mandatory details, quote acceptance/payment flow, guest checkout
   and account linking, and meaning of the incomplete reorder requirement.
5. Pickup cutoffs/windows, slot capacity, policy changes, chef/customer delivery
   responsibilities and promised delivery dates/windows.
6. Discount rules, refund allocations and ingredient units/recipe scaling,
   inventory consumption and grocery/cost forecasting expectations.
7. Instagram ownership/capabilities and approved handoff fallback; email domain,
   analytics consent, operational recovery targets and service budget.
8. Confirm release-only **deployment** interpretation and who may publish releases.

Until answered, represent the options in proposals and blocked tickets rather
than inventing values or reducing the complete scope to the old demo.

## Evidence boundaries

Meta's [official Instagram Send API collection](https://www.postman.com/meta/instagram/folder/uxudqu0/send-api)
requires the recipient to have messaged the professional account first. It does
not establish that a website can send a DM as the customer. Validate any link,
prefill or automation on supported devices/accounts before promising it. A
copy-summary + open-conversation flow is a candidate fallback, not proof of send.
Do not silently replace the requested Instagram journey with email.

Provider capability/pricing research is dated evidence, not a permanent guarantee.
The complete design must recheck candidates at selection time. No provider account,
real payment, final visual design or production service is established by this kit.
