# GK-015 r04 — independent bounded review

2026-10-06. **PASS for Creator review of this local POC.** No unresolved blocker,
major or minor finding remains within the requested r04 corrections. This verdict
does not approve the finished visual design or establish production readiness.

## Independence and boundary

Reviewer `gk015_r03_review` continues the separate, initially history-isolated
reviewer assignment. Requested `gpt-6-astra` / `xhigh` was accepted; effective
identity was not exposed, so selection remains **requested-only**. This is an
independent continued review, resumed after compaction, with no POC authorship.
Only this report was written for r04. Source, r03, owner records, shared contracts
and external systems were not edited by the reviewer.

Applied root instructions, `gk-plan-review`, the execution check and the existing
design-session criteria. Read the r04 JavaScript/CSS, verification source, session,
manifests, selected reference and current GK-019 handoff/K06 and approval/review
records. The accepted correction is neutral silver, the explicitly selected
`composite.png` in both chrome positions, the reference-derived cake/stand hero,
and a styled usable date calendar. The newly accepted automatic time-choice
behavior is included to the bounded extent recorded below.

## Exact reviewed candidate

| Artifact | SHA-256 |
| --- | --- |
| `poc/app.js` | `135ee6b252bc9115e6e209b4999cf5097ff700dbeda6179f43bff4ae3bf33e36` |
| `poc/styles.css` | `ab3e38c325c33488abf65c8ec19e6c4ab8d8507b3fe0a483a159651a666faa0b` |
| `output-manifest.json` | `54e2f7c8272bf8501c7e98b9f966f2c42c0e576b9f116cc8047a9d3351b63f62` |
| `input-manifest.json` | `fbd450a85b83321834e4874b3eb745b5709d0fdebda2abe44ec15ff085f0f303` |
| `verification.json` | `2bda3e1e40bace7818fb8fe79bb4e486d35c5744c205213846221b0e53b40c96` |
| `verify.cjs` | `4dd92090eb40d4c1db3d3cb53f5e46ec56cc7f00bccbd3064b2ae1b1f706a4fd` |
| `poc/assets/hero-reference.webp` | `97f9c76907f1ddabf564724ac8ffeaecb04f9a91e7bf623166966be2e01f3da7` |
| `poc/assets/composite.png` and original owner file | `2b79f2f9f21e44fe6f4a6f377b3694ec87fd2896eaacb9f63e0003190e51fd9c` |

Independently verified all 27 output files and all 25 current input hashes. The
latest shared GK-019 publication manifest
`547e37d4d1126fb4abb477a6f467381617ee3393ab1fa100d2515e9349bb2db8`
also matches all 98 files. Its current independent review accepts contracts r6.5,
owner mapping r7 and schema 1.5, with publication still separately authorized.
The preserved 96-file design manifest identifies the earlier reviewed design;
its amendment README was subsequently extended for the separately reviewed
publication packet. This does not change the consumed K06 behavior.

The r03 output manifest remains
`4064e85685a890a841f41145b18ff12bbbe69b84a66159d9b4975d89ac703817`;
all 25 of its POC file hashes still match. Earlier reviewed work is preserved.

## Findings resolved before this verdict

| Finding and consequence | Verified disposition |
| --- | --- |
| New error red lacked normal-text contrast against the darker silver surface; transparent controls also needed stronger boundaries. | Error is now `#4a0808`; relevant boundaries are `#363838`. Independent calculations against the conservative `#8c8c8c` background give 4.70:1 for error text, 3.51:1 for boundaries, 3.90:1 for focus and 4.52:1 for muted text. Updated test pairs replace the stale demo-bar colors. |
| Browsing another month left an off-screen selected date hidden behind generic Check/Use text. | Automatic loading replaces Check. Available-result and Use text explicitly name the selected date; Use also names the chosen time. Month navigation does not silently choose a new date. |
| Checkout fulfilment changes retained the old collection time and label. | `bindCheckout` clears and disables choices during refresh, discards obsolete completion, labels delivery as a request subject to chef confirmation and requires a fresh selection. Delivery cannot produce a confirmed payment preview. |
| The final large emblem rule overrode mobile sizing; overflow clipping could conceal this from a document-width assertion. | A final mobile override uses 110px banner / 130px footer boxes. The refreshed mobile homepage visibly fits both banner content and emblem. |
| Earlier saved previews still showed a stretched servings field and the PNG's black rectangle. | Refreshed desktop calendar shows a normal-height servings input. Refreshed homepage/calendar previews show the silhouette clip and current neutral silver styling. |

The author also corrected transparent SVG hitboxes that intercepted nearby
controls. Final CSS confines cutouts and removes their pointer targeting while
their containing product links remain interactive.

## Coverage and evidence

- The desktop and mobile homepage previews visibly use neutral silver and one
  cake/stand image with consistent perspective. The source no longer renders the
  old separate cake and CSS stand. The downloaded hero visibly follows the chosen
  reference's cake, dark flowers and metal turntable. It is generated imagery;
  pixel-identical extraction and final artistic approval are not asserted.
- Both chrome positions call `seal()` with the byte-identical selected composite.
  CSS clips its black surround without rewriting the file. The user's current
  explicit composite choice resolves this POC's logo selection. The historical
  absent `3000px-03.png` is not a blocker for this r04 choice; canonical owner-record
  reconciliation remains separately owned.
- `bindCalendars` uses named native day buttons with explicit pressed state and
  roving focus. Arrows, Home/End and PageUp/PageDown navigate; month changes clamp
  invalid dates. Escape closes the inline request calendar and returns focus.
  The modal is named. Missing custom date exposes an alert and focuses the trigger;
  selecting a day clears the error and editing retains the selected date.
- Date-picker opening/selection automatically loads fixture times. A changed
  selection clears old times and acceptance immediately; request revision checks
  discard late results. Loading, empty and failure/retry differ visibly. A time
  must be explicitly selected before Use. Custom date/mode changes reset preferred
  times; delivery remains clearly subject to chef confirmation. Optional custom
  times can remain “To agree.”
- Current approved product prices, compositions/allergens and the distinction
  between purchasable examples and gallery inspiration remain intact. Generic
  custom routes do not acquire a reference product. Existing unknown/late payment
  guards still prevent a replacement payment preview. All booking, payment,
  account, upload and communication boundaries remain explicit simulations.

Inspected final `index-desktop.png`, `index-mobile.png`, `calendar-desktop.png`,
`calendar-mobile.png`, `slots-mobile.png`, the logo preview and hero/reference
images. The inspected final homepage hashes are
`0b3977994b0e2e02759b1e63533c522c6a72dc861fc55820a936438a140ab1a1`
and `9e4d6082bd30f8faa744e40d47bb1b8f6807cdd923b8fac72ecc2780c8026237`;
desktop/mobile calendar hashes are
`8659b80725acb313a9eb5efa74e374125b977112d8ba0ff31946f48df2b41182`
and `c0c94c05daedbbcdab6c5443dcdb4973e58deb730736038603f280f3b09df35d`.
Also independently inspected `banner-320.png`: its CTA and emblem fit the narrow
banner without clipping. Repository planning-kit checks passed after this report
was written; the report has no trailing whitespace.

The author's final browser run, recorded at `2026-10-06T00:02:43.411Z`, passes
16 groups, 16 route variants at 320/390/768/1440 widths and 11 contrast pairs.
Independently inspected its assertions and verified that its JavaScript/CSS
hashes match this candidate. Assertions include month-boundary keyboard movement,
Escape/focus, missing-date validation, mode reset, stale-response rejection,
empty/error results, explicit off-screen date, delivery blocking and retained
pending-payment guards. The reviewer independently recalculated the changed
contrast pairs and checked source/asset/manifests, but could not rerun live r04
browser interactions because the reviewer browser surface was unavailable.
The browser results are author-run evidence, not an invented independent run.
This is not full screen-reader, browser-matrix or accessibility certification.

## Remaining acceptance and implementation gates

1. Creator approval of the exact finished r04 visuals remains pending. The request
   to make these changes is not approval of the resulting revision.
2. Fixtures do not implement real schedule data, cart-dependent revalidation,
   authoritative slot IDs/versions, holds, stock/capacity or server submission
   revalidation. Existing-order moves remain text-only. Their implementation must
   reuse the owned day/cake/accepted lead allocation, preserve six-hour eligibility
   and show valid same-day choices even when fresh admission would be full.
3. Current Onelink bundle approval is now settled subject to GBP compatibility;
   it must not be described as an unanswered business question. Provider/method/FX
   qualification, future additions or non-GBP settlement decisions and actual
   verified refund operations remain separate. No such operations are in this POC.
4. Higgsfield generation metadata/cost are recorded author observations in
   `higgsfield-evidence.json`; the reviewer verified the resulting asset/hash and
   appearance, not the provider billing ledger. No external publication or live
   service acceptance follows from this report.
