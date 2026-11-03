# GK-015 r05 — form controls and uninterrupted silver

Candidate for Creator review, 2026-10-06. Supersedes r04 as the current proposal; r04 preserved byte-for-byte. The Creator said “Much better” about r04 and requested five corrections; that is directional feedback, not exact final approval of r05.

## Execution/context check

Continued then compacted-resumed bounded GK-015 visual author. Requested/effective master model and effort: unknown; no host identity evidence or model switch claimed. Prior qualified independent reviewer context is reused for the same bounded role; its requested-only Astra/xhigh evidence remains separate from author identity. Applicable root instructions, gk-feature design-session guidance and gk-plan execution check consumed. Inputs sufficient for this local design iteration: all 25 pinned r04 owner/contract/asset inputs were rehashed and unchanged (input-manifest.json). Existing staged/untracked work preserved. Only r05 visual artifacts written. No shared contract/canonical owner data/ADR edits, production implementation, deployment, Git publication or Trello publication.

## Creator changes implemented

1. Removed decorative horizontal boundaries on the header, hero, promise strip, bespoke banner, footer, preview ribbon and shop controls on every route; removed the aside rule. Useful dark form boundaries, selected navigation and keyboard focus remain.
2. Availability calendars use actual disabled buttons for every unavailable day. Only scripted 22/24 October 2026 can be selected. Keyboard skips unavailable days; empty months retain usable month navigation and explain no dates. Custom **requested** dates remain subject to chef agreement, including a full ordinary day; this does not grant availability or reserve anything. Error/empty/retry examples moved into explicitly labelled POC controls instead of making unavailable dates clickable.
3. Serving ranges 3–4, 7–10, 12–14, 17–20, 30–32 and 40–45 come from confirmed GK-002 single-tier intake. Native radio choices, no default. Other reveals a required text field, rejects whitespace and supports larger/tiered requests. Returning to a preset excludes retained Other text from the summary. These are request preferences, not an automatic price or physical-cake size substitution.
4. Image area supports Browse images, drag/drop, local thumbnails, filenames and remove actions. JPG/PNG/WebP only, visible unsupported-format feedback, retained previews on edit, focus return after removal. Files stay in page memory, are not uploaded or included in copied messages, and are separately attached by the customer. No service or production upload contract added.
5. Preferred channel uses native radio cards with Instagram and Gmail-style monochrome icons, selected checkmark, keyboard navigation and visible focus. Email remains the functional channel label. The selected channel drives the simulated handoff; neither card opens an app or sends a message.

## References and design rationale

Read 2026-10-06: [GOV.UK radios](https://design-system.service.gov.uk/components/radios/) for one-choice groups and conditional questions; [Carbon file uploader](https://www.carbondesignsystem.com/building-blocks/core/components/file-uploader/guidelines) for a browse/drop area, selected-file list, removal and error feedback. Applied these interaction patterns in the existing silver visual style. Two visible contact choices avoid hiding either option in a dropdown. No new dependency or external icon fetch.

## Editable preview and integration

Sources: poc/app.js, poc/styles.css, 13 HTML entrypoints and preserved original assets. Serve `python3 -m http.server 8769 --bind 127.0.0.1 --directory docs/design/sessions/visual-foundation/gk-015-r05/poc`; open http://127.0.0.1:8769/custom.html and shop.html. All ordering, times, payment and handoff actions remain labelled simulations.

Consumed unchanged GK-019 contracts r6.5/owner-r7/schema1.5 and accepted slot handoff. Automatic time loading, no silent replacement, request-only delivery and unknown-payment restrictions remain. Actual server availability/cart revalidation, owned-order time-move picker and payment-provider qualification remain separate integration work, as recorded in r04. Existing canonical logo-record discrepancy belongs to the master; this POC retains the Creator's explicit composite.png choice. No shared policy changed in this iteration.

## Verification and acceptance

Automated browser results: verification.json; source and asset fingerprints: output-manifest.json. Independent review: review-r05.md. These are bounded POC checks, not live-service or full accessibility certification. Contrast and focus are checked against the actual silver surfaces. Exact-version Creator approval remains pending. Trello amendments remain a local review draft; GK-015 is not marked Done.

Final candidate manifest SHA256: `8f8970048cc1dcf70e2db5586bff2d55514b3e85263373a9763d87f96f93f109`. Final automated result: **PASS — 19 groups, 16 route variants × 4 widths, 11 contrast pairs**, source fingerprints in verification.json. JS syntax and planning-kit checks passed. Reviewer found two remaining pale dividers in mobile navigation and content banners; both removed, assertions added and full relevant suite rerun successfully. Source and asset hashes of preserved r04 still match its original manifest.

Independent review: **bounded PASS for Creator review**, no outstanding findings. Exact report SHA256 `bf1a531b06ff7d4685f4137211727b8ab95558430f5e340b840d2017f415c631`. Report separates independently inspected source/renders/hashes from author-run browser evidence. Creator approval is still pending.
