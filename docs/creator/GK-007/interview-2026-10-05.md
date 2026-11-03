# GK-007 — interview-r1

Date: 2026-10-05. Individual decisions: supplied by Sasha in this conversation.
Consolidated readback and completion boundary: pending. Source questions and exact
answer excerpts are in `interview-2026-10-05.json`. Current local packet only;
no Trello publication, application work or independent approval is claimed.

## Discounts

- Support percentage and free-delivery codes, with only one code per order.
- Percentage codes apply to all cakes, including custom orders, already-reduced
  cakes and cancelled-cake resale. Apply to the current agreed cake price,
  including decoration in that price. Do not apply the percentage to GK delivery.
- Each code can have an optional expiry, total usage limit and per-customer limit.
  Codes with per-customer limits require sign-in; guest checkout remains available
  without those codes. No claim that one account identifies one real person.
- Cancelling an accepted paid order does not restore either usage allowance,
  regardless of full or partial refund. Confirmed free orders follow the same rule.
  Confirmed-unpaid abandoned checkout releases reserved use; a timer alone is not
  evidence that a pending payment failed.
- A free-delivery code waives the full GK delivery charge within its configured
  maximum driving distance from the kitchen, subject to ordinary delivery eligibility.
  It cannot waive a separately paid third-party courier. No actual promotion radius,
  route provider or new service-area expansion is approved by this interview.
- Calculate a percentage on the eligible subtotal, round to the nearest penny with
  halves upward, then allocate proportionally to eligible lines so they sum exactly.
  Existing K03 largest-remainder allocation, with stable line-ID tie-break, is the
  technical realization for final review; not an additional quoted business answer.
- Refunds use actual amounts paid after discount. Retain current GK-003 entitlement,
  separate delivery collection/chef refund and customer-favouring fractional-penny
  refund rounding. This interview does not change the refund rate or deadline.

## Free orders and preparation

- Permit a zero-total order through a no-payment confirmation path, explicitly
  identified as free. Never fabricate a successful payment capture.
- Confirmation immediately commits cake/date capacity and consumes code usage;
  chef approval is not a prerequisite. This is an explicit exception to the earlier
  accepted-orders-must-be-paid rule. A cancelled free order does not restore use.
- Where the 48-hour preparation clock applies, free-order confirmation replaces
  successful payment as its anchor. Keep automatic preparation status at the
  scheduled time; preserve ordinary earlier manual preparation semantics.
- Automatic status is separate from stock accounting: only chef-confirmed actual
  production with selected batches causes ingredient/component deductions.
- A cake reduced to zero may still have separately payable GK delivery. There is
  no cash refund of an unpaid cake; actual separately paid charges retain their
  existing refund rules. These are consequences of actual-paid accounting.

## Recipes, scaling, precision and costs

- Use a separately entered recipe for each cake size; do not scale cakes between
  sizes automatically. Component batch recipes may scale proportionally.
- For quantity ranges, use the midpoint for planned demand and estimated cost.
  Actual use remains separately recorded and may differ.
- Store mass/volume quantities to 0.01 g/ml. Round calculated quantities to nearest
  0.01, with exact halves upward. Whole-item ingredients require whole quantities:
  if a multiplier needs 2.5 eggs, choose another batch size or a separately approved
  recipe. Never silently round the count.
- Keep mass, volume and count separate. Do not use the workbook's provisional
  1 g/ml or 50 g/egg factors as verified conversions. Affected calculations remain
  incomplete until ingredient-specific measured evidence is supplied.
- Shopping estimates use the latest recorded purchase price. Existing agreed
  customer prices do not change automatically. For this workbook, 2026-10-05 is
  the user-assigned provisional price as-of date, not an asserted purchase date.
- Buy whole packs: round the shopping quantity up to the pack size and budget the
  full purchase cost. Recipe cost uses the quantity consumed. Missing price/pack
  data is incomplete, not zero. Finished-component costing needs a known yield.

## Component and ingredient stock

- The chef prepares components in advance for multiple cakes. At component
  production start, deduct planned raw ingredient quantities from chef-selected
  batches. Record actual finished output and its expiry when ready.
- At cake assembly start, deduct planned quantities from chef-selected finished
  component batches, plus directly used ingredients. Do not deduct the component's
  raw ingredients again. Record differences from planned consumption afterward.
- The chef selects every batch; no automatic earliest-expiry or oldest-purchase
  selection is approved. Insufficient recorded stock can go negative with a warning
  and required reason, remaining visibly flagged for reconciliation.
- Every received ingredient batch and every newly made component batch requires
  an expiry date. The chef enters component expiry manually; no assumed shelf life.
- The entered expiry date is the last usable day. Block use from 00:00 on the
  following day in Europe/London. Exclude expired stock from usable stock, but
  retain physical quantity until chef-recorded disposal or a reasoned date correction.
- After cancellation following production start, keep deductions unchanged and
  require a chef review of used, wasted and actually returned reusable quantities.
  No automatic restock from a cancellation/refund. Cancelled remaining demand is
  removed from the future plan independently of that physical-stock review.
- Confirmed orders retain their original recipe version. Recipe edits affect new
  orders; existing orders require an explicit amendment. Retain current customer
  agreement rules for changes to agreed ingredients/allergens.
- Existing K07 invariant: selling/reselling an already-produced cake consumes its
  ready-cake availability, not a second recipe's raw ingredients or future production
  demand. Preserve original consumption history and any actual chef corrections.

## Supplied recipe example

Workbook: `source/GK_Cake_Costs.xlsx`, SHA-256
`1898cdff50c151081c21e9690350dce2af1b4015c2c4c1bb4ce2a7e97fae30f2`.
Source locations: `Рецепты тортов!A28:J31` and `A69:J69`.
Example: vanilla–raspberry, 6-inch Small, VR-6. Sasha supplied **7–10 servings**.

| Component | Source quantity | Planned quantity under midpoint policy |
| --- | --- | --- |
| Baked vanilla sponge | 300 g | 300.00 g |
| Milk for soaking | 40–50 g | 45.00 g |
| Classic cream-cheese filling | 550 g | 550.00 g |
| Raspberry compote | 200 g | 200.00 g |
| Swiss meringue buttercream coating/decor | 500–700 g | 600.00 g |

Total planned components: **1695.00 g**, not a measured finished cake weight.
Range sum: 1590–1800 g. No duplicate coating allowance is added.
Known purchase example: `Ингредиенты!A13:D13`, 1000 g butter at £6.78,
provisional price as-of 2026-10-05. This is not a complete cake-cost calculation.

## Explicitly deferred inputs and source limitations

Sasha: "unmeasured for now, but will be measured in the future" and
"record them as inputs to supply later alongside the measured yields".

1. Measured finished/cooled batch yields for vanilla and chocolate sponge, compote,
   caramel, ganache, all cream-cheese fillings and Swiss meringue buttercream.
   The workbook's 1000 g cream-cheese outputs assume no loss; not measured evidence.
2. Banana purchase price/quantity and edible-versus-peel treatment; Oreo pack
   weight/price; exact vanilla product, amount per sponge batch, pack size and price.
3. Ingredient-specific conversion measurements wherever recipes and purchasing
   units differ. Until then preserve original units and incomplete calculations.

These future inputs have no invented due date or fabricated completion evidence.
Further source limitations stay visible when preparing any additional catalog
fixture: missing flavour/size quantities, unmeasured finished cake weights and
source inconsistencies such as mini white-chocolate/raspberry filling totals.
Do not treat empty rows or costing assumptions as approved complete recipes.
Packaging/labour gaps do not become zero full-business costs; they are outside the
chosen ingredient example. File upload does not approve every workbook statement.

## Acceptance walkthroughs for consolidated review

These are arithmetic and policy walkthroughs, not application test results.

| Case | Expected outcome and basis |
| --- | --- |
| £10.01 + £10.00, 10% code | £2.00 discount, £1.00 per line; £18.01 payable. Explicitly approved in interview. |
| £10.05, 10% code | £1.005 rounds to £1.01 discount; £9.04 payable. Explicitly approved. |
| £50 reduced cake, 10% code | £5 discount; £45 cake charge. Agreed eligibility includes reduced cakes. |
| Eligible 50% refund of £18.01 actually paid | Total target £9.01, rounded in customer's favour; retained GK-003 rule. Allocation must reconcile to that one target, not independently round every line upward. |
| £0 collection order | Confirm without capture; consume code usage and reserve capacity; automatic preparation due +48 hours; no stock movement until chef action. |
| Scale compote by 2 | 2000 g raspberries +720 g sugar +108 g starch; actual finished yield still measured separately. |
| Scale a 5-egg recipe by 0.5 | Do not silently round 2.5 eggs; require another multiplier or a separate approved recipe. |
| Calculated quantity 12.345 g | Plan 12.35 g; 12.344 g becomes 12.34 g. |
| Need 500 g butter; packs 1000 g at £6.78 | Shopping: one pack, £6.78. Consumed-quantity ingredient estimate: £3.39. |
| 1000 g stock; planned use 800 g | Before start: 1000 g physically recorded, 800 g demand. Chef start deducts 800 g, leaving 200 g and no duplicate outstanding demand for that deducted quantity. |
| Actual use was 750 g after 800 g deduction | Chef records a 50 g correction to the selected batch if physically unused/reusable; stock becomes 250 g. Keep the original movement and correction history. |
| 300 g recorded; start needs 500 g | Allow -200 g with warning and required reason; no silent clipping to zero. Chef batch selection still required. |
| Cancel before any actual production | Remove future demand; no consumed ingredients exist to return. |
| Cancel after component/cake production starts | Remove cancelled outstanding demand; deductions remain pending chef review of use/waste/returns. No automatic refund restock. |
| Batch expires 10 October 2026 | Last usable day is 10 October; block from 11 October 00:00 Europe/London. Keep remaining physical quantity until disposal/correction is recorded. |
| Component prepared, then 200 g used in a cake | Raw inputs deducted during component preparation; later use deducts 200 g component only. |
| Already-made cake sold/resold | Do not produce duplicate raw/component consumption or new production demand. |
| Recipe v1 order, then v2 published | Existing order keeps v1; new orders use v2. Existing-order amendment is explicit, with required customer agreement. |

## Completion evidence and remaining boundary

| Criterion | Evidence / remaining work |
| --- | --- |
| AC1 | Discount policy and exact examples explicitly approved. Decimal arithmetic checked locally; no application is implemented. |
| AC2 | Workbook source, variant quantities, 7–10 servings, exact units and provisional cost date captured. Full validated ingredient/cost derivation remains incomplete because measured yields, conversions and named ingredient inputs are deferred. |
| AC3 | Explicit stock/preparation/cancellation/expiry/recipe-edit decisions; combined walkthrough readback pending. |
| AC4 | Local answer/source revision evidence; consolidated approval and Trello recording pending. No independent review claimed. |

Under the original ticket criteria, deferred inputs must remain visible outstanding
work; no Done transition is inferred. A separately agreed input-stage/follow-up
split could change the completion boundary, but none has yet been requested.

GK-019 reconciliation must incorporate the free-order exception, separate delivery
code application, automatic-status versus physical-stock distinction, component
batches, mandatory expiry, quantity rounding and whole-pack shopping into shared
contracts/SQL/admin designs. Retain GK-001/003/005 owner-source versions and legal
gates; do not edit historical publication snapshots as if they were current approvals.
