# GK-003 acceptance cases — manual-handover-r2

Document calculations and technical acceptance cases; no real provider operations
or application tests ran. Amounts are GBP integer pence. Eligibility is determined
by current GK-001/GK-003 policy, never by old illustrative amounts.

T = applicable total obligation; S = actually successful refunds; P = pending or
unknown operations which may succeed. Remaining=T−S; unreserved=max(0,T−S−P),
bounded by captured balance. Whole-obligation preference waits for an unresolved
operation on the same collection rather than intentionally dispatching a fragment.
Separate GK delivery is a separate paid collection with chef-only refund execution.

| ID | Trigger / input | Expected outcome |
| --- | --- | --- |
| M01 | Original chef example: cake 10000 + delivery 2000, old eligible 50% cake + 100% delivery | Historical target 7000; arithmetic preserved only. Current ordinary 50% rule gives 6000 (M03). |
| M02 | Cake original 12000, discount 2000, applicable 50% | Paid cake 10000; cake allocation 5000, not 6000. No separately added VAT deduction. |
| M03 | Cake collection 10000 + separately paid GK delivery 2000; ordinary 50% entitlement | Target 6000: cake 5000 and chef delivery 1000, each against its actual payment. Do not refund 6000 from each payment. |
| M04 | Same payments; customer cancels >120h before original handover, or chef cancels | Target 12000: cake 10000 + separate chef delivery 2000. |
| M05 | Single-collection target 5000, S=0, P=0 | Attempt 5000 once, not intentional 2000+3000. Verified failure logs error and leaves 5000 owed; unknown response requires reconciliation before retry. |
| M06 | Historical target 7000; S=2000; P=5000 | Remaining 5000, unreserved 0, dispatch 0. Historical partial is a real fact despite new no-intentional-splitting rule. |
| M07 | M06 P verified failed / succeeded | Failed: P=0, still owe 5000; safely retry whole remaining 5000. Succeeded: S=7000, owe 0; no new payout. |
| M08 | One historical collection paid 12000; full-return target 12000, S=2000, P=5000 | Still owe 10000; 5000 reserved and 5000 unreserved; wait instead of deliberately dispatching a fragment on that collection. |
| M09 | M08 P verified failed / succeeded | Failure leaves 10000 owed, attempt after reconciliation. Success leaves S=7000 and 5000 owed. Target remains 12000 in both branches; never resend 12000. |
| M10 | Eligible 50% of 10001 | Exact 5000.5p rounds for customer to 5001p (£50.01). |
| M11 | Two eligible lines of 10001 each, 50% | Aggregate 10001p; floors 5000+5000, one remaining penny to first stable line = 5001+5000. Exact sum, customer-favour aggregate rounding, no invented extra penny per line. This is developer allocation design. |
| M12 | No-show / after handover, cake 10000 + GK delivery 2000 | Target 6000, same separate 5000+1000 allocation. Old no-show/post-handover exclusions are superseded. |
| M13 | Delivery 2000 unperformed through GK fault | Delivery target 2000. If 1000 already returned under ordinary cancellation, only 1000 still owed; pending reserve prevents duplicate payment. Cake target separate. |
| M14 | M03 cake refund succeeds, delivery refund verified fails | T=6000, S=5000, P=0, remaining=1000; show incomplete refund and delivery error; chef retries full 1000 delivery obligation. This is not intentional splitting of one payment. |
| M15 | M04 cake refund succeeds, delivery pending | T=12000, S=10000, P=2000, remaining=2000; no new delivery operation until reconciliation. Do not show “fully refunded”. |
| M16 | Applicable separately paid components 10001 cake + 2001 delivery at 50% | Aggregate target 6001; floors 5000+1000, equal remainders: stable cake-first allocation 5001+1000. Both component targets sum to 6001 and stay <= paid amounts. |

Rounding method: ceil aggregate entitlement to pence, allocate floors then remaining
pence by largest fractional remainder with stable item/payment tie order. Persist
allocation. This technical method expresses customer-favour rounding; it is not a
chef quotation. If external results unexpectedly show partial success, record actual
successes and remaining obligation; never erase money facts or promise atomic results.
VAT is not separately added in these fixtures per current owner-attested GK-005 data;
future tax changes must preserve historic component/policy snapshots.

| ID | Scenario | Expected result |
| --- | --- | --- |
| A01 | Template link sent at instant X, customer unpaid | Payment window expires X+24h; quote alone reserves no capacity; entity is template, not accepted order. |
| A02 | New initiation immediately before / at / after X+24h | Before: valid subject to ordinary checks. At/after: disabled. Server equality rule is explicit developer interpretation of within 24h. |
| A03 | Already paid template revisited | No second collection; show actual order/payment state. |
| A04 | Payment starts while valid | 15-minute technical hold; timer expiry cannot itself establish provider failure. Reconcile in-flight outcomes. |
| A05 | Valid full payment succeeded but capacity now full | Order accepted; no automatic rejection solely for capacity. |
| A06 | Unpaid expired template / cancelled order | No payment on old template; fresh agreement/template can be issued. Cancelled accepted order cannot reopen; new order required. |
| A07 | Cancellation at D−1 second / exactly D / D+1 second | 100% / 50% / 50%. D=original handover−120 elapsed hours for both cake types. |
| A08 | Date/time moved in either direction; production started early | Original D remains fixed; production state alone never changes 100/50 entitlement. |
| A09 | Unverified account or only matching Instagram/reference | Do not link/disclose old order. Chef confirmation requires verified identity plus order proof. |
| A10 | Verified account but insufficient order proof | Remain unlinked/pending review; safe audit evidence, no disclosure. Different payer identity alone does not own account history. |
| A11 | Unexpected transfer claimed while online outcome unknown | Reconcile real evidence; no arbitrary paid switch or duplicate ledger fact. If both genuinely succeeded, preserve both and expose excess for chef resolution. |
| A12 | Reorder with changed price/composition/date | Reconfirm current price, allergens/composition, design, availability and terms. |
| A13 | Customer opts out of photo publication | Requested preference prevents publication. Default wording/rights still requires GK-005 review before publishing; third-party references not authorized. |
| A14 | Chef chooses resale of cancelled started/ready cake | 50% stated cost, unchanged handover dates, separate inventory/order linkage; no automatic stock duplication or original order reopen. |
| A15 | Instagram conversation finalised | Chef creates template; customer confirms terms and pays on official website within 24h; no current Revolut collection option. |
| A16 | No Instagram; payer is another person | Require customer full name and distinct payer details; no fabricated Instagram or identity/bank document upload. Minimal exact provider fields decided in GK-019. |
