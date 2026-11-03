# GK-005 / BO-GK-005 — decision interview r1

Business-question interview completed on 2026-10-05. This is a local decision
supplement, not a Trello Done transition, legal approval, or final customer Terms.
Sasha supplied and approved the replies; do not attribute them directly to Elina.
Read the [source excerpts and question context](interview-sources-2026-10-05.json),
[evidence and revision manifest](interview-manifest-2026-10-05.json), original
[owner decisions](decisions.json) and [source comments](source-comments.json), plus
the current [GK-001](../GK-001/handover.md) and [GK-003](../GK-003/handover.md) handovers.
This supplement resolves the specific earlier unknowns below. Historical source
files, follow-up questions and publication receipts remain unchanged; their older
"missing privacy continuation" and "rating unavailable" statements are historical.

## Identity and evidence

- Sole trader: **Elina Hryhorian trading as GK Cake London**.
- Public contacts: **gk.cake.london@gmail.com**, **@gk.cake.london**.
- The address supplied in the conversation is approved for legal business use.
  Its exact value is intentionally not duplicated in this repository supplement;
  retrieve the authorised value from the conversation for final legal content.
  This approval does not instruct an agent to publish the address to Trello now.
- Official FSA listing: **GK Cake**, FHRS **1963655**, Tower Hamlets, inspection
  **29 June 2026**, rating **5 — Very Good**. The official page was inspected in
  the browser (including the displayed rating) and matched the supplied JSON,
  extracted **5 October 2026**, local authority business reference **218672**.
  [Official listing](https://ratings.food.gov.uk/business/1963655/gk-cake).
  Inspection date is not claimed to be the original registration date.
- Current non-VAT registration/no separately added VAT reconfirmed by Sasha.
  This is an attestation, not an accountant/HMRC status check, exemption finding,
  or zero-rate classification.

## Food content

The supplied workbook's `Вкусы!A7:D14` gives the eight flavour compositions;
`Рецепты компонентов!A7:I43` gives component recipes. Source location, hash and
limitations are recorded in the manifest. Original workbook is unchanged.

| Flavour | Contains, all sizes |
| --- | --- |
| Banana Pudding | EGGS, MILK, SOYA, WHEAT (cereals containing gluten) |
| Chocolate Raspberry | EGGS, MILK, SOYA, WHEAT (cereals containing gluten) |
| Chocolate Cream Duo | EGGS, MILK, SOYA, WHEAT (cereals containing gluten) |
| Chocolate & Banana | EGGS, MILK, SOYA, WHEAT (cereals containing gluten) |
| Vanilla Raspberry | EGGS, MILK, SOYA, WHEAT (cereals containing gluten) |
| White Chocolate Raspberry | EGGS, MILK, SOYA, WHEAT (cereals containing gluten) |
| Chocolate Oreo | Above four + SULPHITES |
| Snickers | Above four + PEANUTS, prominently displayed |

These are user-confirmed declarations, retaining the original owner's wheat
specificity; they are not independent supplier-label verification. No ingredient
is inferred absent because a supplier label was not supplied. The workbook lacks
full supplier sub-ingredient declarations and a finished order label. Its missing
costs and quantity assumptions do not by themselves block this policy interview.

Swiss meringue buttercream is standard for **every size and flavour**. Sugar
pearls, fruit and berries may be added; other edible decorations require prior
discussion/agreement with the chef. Elina approves ingredient/allergen changes.
For an accepted order, changes affecting the agreed composition require explicit
customer agreement. If the customer declines or does not respond before preparation
must start, provide the original cake if feasible; otherwise cancel with a full
refund. No silent ingredient substitution.

Retain the exact original shared-kitchen cross-contact notice separately from
"contains" declarations. Information must be available before purchase and on the
box at handover, matching the actual cake including decoration. A changed recipe
must not silently overwrite the purchased composition/version.

Example allergen block for content preparation, not an approved finished legal label:

> **Snickers** — Contains **EGGS, MILK, SOYA, WHEAT (GLUTEN), PEANUTS**.

Use the original full cross-contact notice alongside this block. Final order-specific
ingredients, decoration, label preview and any required food-storage/date content
must be checked against actual products in implementation/prelaunch verification.

## Money and tax decisions

- Base prices in whole GBP. Display whole amounts as **£100**; preserve fractional
  payable/refund amounts such as **£85.50** rather than hiding or rounding off pence.
- Discounts/refunds round to the **nearest penny**, exact half-pennies **up**.
  The earlier whole-pound-rounding suggestion was superseded and never accepted.
- Keep integer pence for technical money values; no binary float arithmetic.
- Preserve already-agreed customer totals on accepted orders through future VAT
  changes; refund against actual paid amounts. New pricing/tax treatment takes
  effect from a recorded date after applicable tax review. Unpaid quotes needing
  changes require fresh customer agreement. No future tax rate is guessed.

Explicitly approved examples:

| Calculation | Accepted result |
| --- | --- |
| £95 less 10% | £9.50 discount; £85.50 payable |
| Half-penny rounding | £5.005 → £5.01 |
| £101 cake less 15% | £15.15 discount; £85.85 cake payment |
| Above cake + separately paid £20 delivery | £105.85 actual paid total |
| 50% refund of that total | £52.93 = £42.93 cake + £10 delivery |
| Full refund | £105.85 |

£20 is illustrative delivery for this example, not a replacement delivery tariff.
The example does not establish every multi-item allocation/tie-break algorithm;
technical design must preserve agreed rounded totals, component balance limits,
separate payment operations, prior successful refunds and pending reservations.

Use the current GK-001/GK-003 whole-order policy and original handover deadline:
100% strictly more than 120 elapsed hours before original handover, 50% at/after
the boundary, both cake types, including no-show/post-handover; chef cancellation
100%. Moves do not reset the deadline. The 50% target now uses the confirmed penny
rounding. GK-003's specific delivery-failure obligation and statutory rights remain.
Do not revive the old 3/4-day, component or state-only refund defaults.

## Customer data and access

| Category | Agreed period / trigger / outcome |
| --- | --- |
| Enquiry/custom request that never becomes an order | Six months from last substantive customer conversation; automated reminders never restart it; delete/anonymise unless justified legal/dispute need |
| Ordinary order data, messages, delivery instructions, non-personal design references | Two years after fulfilment/cancellation, then delete/anonymise; this replaces the proposed 12 months |
| Personal customer photos | Delete within 30 days after collection/delivery unless needed for active complaint/dispute or separately authorised chef use |
| Personal photos with specific chef-use permission | Three months from collection/delivery, not from permission; permission specifies website/Instagram publication and/or private design-reference use; remove original and controlled public posts showing it at expiry or earlier withdrawal |
| Active dispute | Retain only necessary evidence privately while needed; permission withdrawal does not justify continued public use |
| Customer account/profile/favourites/basic history | While used; after 24 months without login/new order, send warning, then delete/anonymise 30 days later unless customer returns; shorter personal-photo and order-detail schedules still apply |
| Financial/accounting records | Separate applicable HMRC retention; do not retain all other personal data merely because accounting records are needed |

The account rule does not silently extend detailed order data beyond two years.
Basic history can omit deleted personal details/photos. Financial facts must retain
their required evidence even when the public account is removed. Earlier justified
erasure requests and mandatory retention need appropriate handling rather than an
unconditional delete-everything promise. Inherited "when needed" dispute exceptions
need a documented review/release process in implementation, not indefinite holds.

Current business access: Elina. Sasha: only necessary technical support. No other
staff currently. Future kitchen and management workers should have separate scoped
permissions; the architecture must allow extension without granting access now.
If an accountant is appointed, share only necessary financial information.

The retrieved privacy continuation allows necessary order/service emails and no
advertising emails/newsletter/subscription at present. Any later marketing function
requires a separate decision before launch. Its date inside the text is 28.09.2026;
Trello posted it on 2026-10-04 from Sasha's account. Do not misattribute the posting.

Normal sole-trader financial record minimum: five years after the relevant tax
year's 31 January submission deadline; exceptional circumstances must be checked.
[HMRC source](https://www.gov.uk/self-employed-records/how-long-to-keep-your-records),
checked 2026-10-05. Other retention periods are business decisions, not invented
statutory requirements. [ICO storage-limitation guidance](https://ico.org.uk/for-organisations/uk-gdpr-guidance-and-resources/data-protection-principles/a-guide-to-the-data-protection-principles/storage-limitation/)
requires purpose-based justification; the page states it is under review.

## Browser storage and analytics

Approved scope: sign-in/checkout essentials; saved device favourites **one year**;
non-sensitive unfinished design choices **30 days**; user clearing; no persistent
personal photos in browser storage. Six months for favourites is superseded.
Exact session lifetimes and the expiry-reset events for device preferences remain
technical design details to document, not additional guessed owner answers.

Approved statistics: aggregate visits, pageviews and page-loading performance.
Exclude private account, admin and checkout pages; no advertising trackers or
session recordings. Visits are not labelled unique people.

No application or accepted complete provider inventory exists in the inspected
checkout. Before final Privacy/Cookie content, implementation must list actual
technology, purpose, provider, retention, sharing/transfers, necessary/optional
classification and applicable consent/objection controls. Provider retention,
backup expiry and deletion behaviour need verification. Do not claim a verified
cookie inventory or compliance based on the business choices above. Current ICO
[storage/access guidance](https://ico.org.uk/for-organisations/direct-marketing-and-privacy-and-electronic-communications/guidance-on-the-use-of-storage-and-access-technologies/)
was checked 2026-10-05; classification depends on actual use.

## Review deferral, acceptance and next use

Sasha explicitly reports **no qualified legal review yet**, and wants it arranged
after the whole project is implemented so the administrator can update policies
following recommendations. This is a review deferral, not legal approval or release
permission. The original packet's review-before-final-Terms-publication requirement
remains. Policy edits should be versioned, preserving accepted order commitments;
the exact admin workflow belongs to implementation. The user did not specify a
named reviewer or appointment date.

| Acceptance criterion | Evidence now | Still required |
| --- | --- | --- |
| AC1 business evidence/public contact/terms | Official FSA record verified; seller identity and address use approved; public contacts confirmed | Exact final public legal content and deferred legal review |
| AC2 money/tax/allergens | User-approved rounding/refund example and future VAT price policy; all-size declarations; recipe workbook; substitution policy | Actual product/supplier verification and finished purchase/handover content; technical general allocation design; tax treatment verified when applicable |
| AC3 privacy/email/browser/analytics | Versioned business answers above, recovered continuation | Actual provider/storage inventory, retention/deletion evidence, legal-basis review and final Privacy/Cookie text |
| AC4 exact revision/review/Trello | Source excerpts, file hashes, read-only live Trello evidence and local consistency checks | Required review/final acceptance and authorised Trello publication; no Done transition performed |

No further routine business-choice interview question is pending from the groups
asked here. Evidence/review gates remain; this document does not claim both cards
are closable under their unchanged acceptance criteria. To close the input-gathering
stage separately, explicitly scope the remaining review/verification work and its
launch gates in the tracker rather than silently dropping them.

Before downstream execution, reconcile this supplement into canonical decision
revisions, the SQL/admin owner-data contract and dependent design documents, keeping
prior evidence and hashes. Do not implement solely from the older JSON. The current
request collected answers; it did not authorise application implementation, external
publication or status changes. No independent review is claimed for this interview.
