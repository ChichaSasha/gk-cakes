# GK-019 → GK-015 contract handoff — r6, focused re-review pending

Local handoff for the separate visual-design context. No GK-015 visual artifact was
edited or approved here. The exact contract hashes below identify this revision;
[output manifest](output-manifest.json) ties them to the final check/review packet.
Do not infer settled behavior for remaining Q4 qualification in [questions](questions.md).

Consume contracts r6.5, canonical owner-data gk019-owner-r7 and schema r1.5. Important
visible changes from historical r5:

- Accepted payment methods are all17 GK-011 labels; Google Pay disabled. Display
  actual qualified options, not an assumed cards-only product. Q4 optional local
  currency approved: Stripe displays converted total/charges; GBP alternative via
  eligible method, shop prices/accounting GBP. Current Onelink bundle approved subject to GBP compatibility; actual qualification
  remains. Future additions/non-GBP settlement require a new decision. Pending/unknown is
  not paid; return from payment is not confirmation. Separate24h link/15m tentative
  hold; Q2 accepted: late paid shows payment received/date awaiting agreement; chef confirms feasibility and customer explicitly agrees, otherwise full return. No replacement payment while unknown and no fulfillment until agreement.
- Explicit free-order confirmation; no payment receipt or fake capture. GK delivery
  quoted/paid separately and refunded separately by chef; collection remains free.
- Pickup/Delivery time pickers must automatically show available start–end choices
  when opened or date/mode/cart changes. No repeated guess-and-Check. Delivery before
  chef feasibility shows visible requestable times, clearly awaiting confirmation.
  Distinct loading/empty/error states; stale selection refreshes with no silent
  replacement. Existing-order move choices reuse the already accepted day/cake
  allocation, so a full day or last owned cake does not hide valid times.
  Keyboard/mobile usable; six-hour move rule remains. Consume K06
  in your separately owned designs; GK019 has not edited visual artifacts.
- Daily availability counts cakes, not orders; actual remaining standard places,
  chef extras separate, soft overcapacity accepted for admitted successful payments.
  Never sell one finite physical cake twice. No per-pickup-slot count cap.
- Both cake types use original handover−120h:100% before,50% equality/later including
  no-show/post-handover; chef cancellation100%. Moves never reset; production does
  not remove entitlement. Show exact refund amount and confirm cancellation.
- Automatic preparation48h after cake payment/free confirmation; actual chef work
  and inventory deductions are separate. Six-hour same-day self-move, date changes
  through chef. Q1 is accepted: chef may approve a shorter lead for a specific ready/resale cake; preserve original resale handover, physical stock/safety and ordinary192h minimum. Show the specific agreed handover; no blanket short-lead promise.
- One code/order, all cake prices including decor/reduced/resale; percentage excludes
  delivery. Free-delivery eligibility chef-reviewed; no automatic zones/tariffs.
- Primary custom button “Copy request & open Instagram” for @gk.cake.london; user
  pastes/sends, retained selectable fallback; no sent claim. Current sender is
  GK Cakes <orders@gkcake.com>, Gmail Reply-To. Historical sender test differs.
- Specific permission for personal customer photo use; current retention periods
  and expiry must guide copy. Allergen/recipe changes need explicit agreement;
  no-response/decline means original if feasible, otherwise full refund.
- Chef screens must show outstanding vs successful/unknown returns, separate
  delivery action. Q5 approved verified alternative return when provider definitively
  cannot refund: show recipient/amount preview and proof; pending/unknown blocks
  all routes, and submitted never means received. Show selected ingredient/component batches, negative-stock warnings,
  missing-cost evidence and recovery states. Q3 accepted: opt-in basic checkout/repeat-visit statistics; no account/admin browsing or personal/order details/replay. Decline keeps shopping usable; withdrawal stops collection. No new visual edits requested.

GK-015 may continue visual foundation work against settled behaviors. Affected
Q branches stay visibly provisional; contract review is not Creator visual approval.
Implementation receives its own exact-version design approval and tests.

Approved GK-002 logo is currently absent: see [availability evidence](asset-availability.json).
The owner-data README now states this explicitly with the expected hash; no asset was
changed or replacement approved. GK-015/content readiness retains that asset gate.

## Exact shared revision hashes

| File | SHA-256 |
| --- | --- |
| `docs/design/contracts.md` | `b34b0926f371683afbfbd3453de9191efc1df2a625c595ad5c1d5f2b83723512` |
| `docs/design/owner-data-contract.md` | `e71792978a198263aeae6e28ae1520acc4598e1f699235e14aa049664f593a21` |
| `docs/design/creator-decisions.md` | `be01c59a3851603edab31ee369b9034c9c0244550dc1314d4412d567e997affd` |
| `docs/design/gk019/schema.json` | `46fe03910dc9b37e1a246aa2eb8b5e490a99370b612057d2e00296004b27009b` |
| `docs/creator/GK-001/decisions.json` | `6d3112d8349305049377d5b3dd834f78e85d96b95ac357704c7af7daa1f807e9` |
| `docs/creator/GK-003/decisions.json` | `41d7792baa5be64eeaa1e91b5d4fe06154511771b764074e529cf71c3062c27c` |
| `docs/creator/GK-005/decisions.json` | `d6593cfc91948c3294e22fcf112226b4df2838d241abb016a5597c4c88ff6542` |
