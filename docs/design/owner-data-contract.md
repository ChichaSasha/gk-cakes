# Owner requirements → data and administration — gk019-owner-r7

Current maintained mapping, reconciled with the approved interviews and setup receipts.
[Shared contracts r6.5](contracts.md) govern behavior. [Input manifest](gk019/input-manifest.json)
identifies source content and prior revisions. The [prior mapping](gk019/prior/owner-data-contract.md.txt)
is retained only as history. Original Ukrainian and later attributed Sasha replies are
preserved in source-comments.json; neither agent interpretation nor Done means legal approval.

Confirmed values and unresolved semantics live in each decision’s business object;
read conditions/exceptions/provenance, not only value. Dates of statements do not
activate policies. No generic settings(key,jsonb): typed columns/FKs own operational
rules. Every version stores UUID, source decision IDs/revision hashes, recording actor/time,
business approver/evidence, effective_at (NULL until activated), predecessor and reason.
Private addresses/identity documents are excluded from public data and fixtures.

## Typed data and admin profiles

### schedule — GK-001/019/023/028/031

schedule_version; weekly_collection_window(weekday smallint, opens/closes time); published_slot(local_date date, starts/ends timestamptz, timezone text, policy_version uuid FK). 60-minute fitting slots, no slot cap; exact weekday starts explicitly configured. 192 elapsed hour ordinary minimum; accepted Q1 allows chef-approved shorter lead for a specific ready-made/resale cake, with original resale handover retained. Typed lead_time_exception binds physical lot/template/version/handover/latest_confirmation_at/chef/reason/approval and revocation. Uncertain month endpoints chef-approved.

Admin authority: current chef business capabilities; necessary technical support only.
Version/key checks and audit required. No policy edit silently rewrites accepted orders.

### capacity — GK-001/003/019/023/025/031

handover_day(date PK, standard_limit integer>=0, version); allocation(order_id FK, cake_quantity positive integer, classification standard|chef_extra, state tentative|committed|released). Soft standard capacity and immutable chef-extra evidence; finite physical stock independently exclusive. Count cakes by handover date, never complexity weights.

Customer selection consumes a derived versioned /schedule slot projection automatically on date/mode/cart change; no separate slot-capacity table. Delivery without agreed feasibility is visibly requestable, not booked. Server authorizes purpose: new purchases apply fresh lead/day/stock admission; owned existing checkout reuses allocation; same-day move reuses accepted lead/payment/day/physical allocation and checks state/version/six-hour/window without fresh admission. Server revalidation and no silent replacement preserve existing commitments.

Admin authority: current chef business capabilities; necessary technical support only.
Version/key checks and audit required. No policy edit silently rewrites accepted orders.

### start — GK-001/003/007/019/030/032

order.paid_at/free_confirmed_at/preparation_effective_at/actual_started_at timestamptz nullable until event; effective start manual or anchor+48h. Auto status is not physical consumption. Separate stock event with chef, selected lot FKs and reason.

Admin authority: current chef business capabilities; necessary technical support only.
Version/key checks and audit required. No policy edit silently rewrites accepted orders.

### refund — GK-001/003/005/019/025/026

order original_handover_at + fixed120h deadline; refund_goal/order FK, per-collection allocation FK and target bigint pence; refund_operation unique key/body, state and reserved amount. Whole-order100/50 with exact aggregate rounding and separate chef delivery execution. Historical examples are evidence, not active rules.

Q5: same refund_operation channel provider|manual_alternative; chef-approved verified recipient/evidence, definitive provider-unavailability proof, shared reservation before external instructions, unknown blocks all routes. Success proof/K11 required to net the obligation; no fake Stripe IDs.

Admin authority: current chef business capabilities; necessary technical support only.
Version/key checks and audit required. No policy edit silently rewrites accepted orders.

### location — GK-001/019/022/024/028

pickup_location_version(public_locality text, private_address_ref protected UUID, instructions_version FK); authorized address notice intent. Public St Katharine Docks, never private full address in catalog/calendar.

Admin authority: current chef business capabilities; necessary technical support only.
Version/key checks and audit required. No policy edit silently rewrites accepted orders.

### move — GK-001/003/019/028/031

pickup_change(order FK, old/new slot FKs, actor, agreement_ref, command/recorded_at, reason); same-day6h equality permitted; date moves chef-only. Never replace original deadline or clock.

Admin authority: current chef business capabilities; necessary technical support only.
Version/key checks and audit required. No policy edit silently rewrites accepted orders.

### delivery — GK-001/003/007/019/025/028

delivery_agreement(order FK, address_ref, fee_minor bigint>=0, currency GBP, mode, window FK, source base_fee>=10000 before discount, discount_application FK); individually agreed distance/feasibility and separate collection. No zone tables or automatic tariffs.

Admin authority: current chef business capabilities; necessary technical support only.
Version/key checks and audit required. No policy edit silently rewrites accepted orders.

### request — GK-003/019/027

custom_request(owner/capability, desired_date, servings or size, flavour/design/palette); request_reference image_revision FK. Customer full name; distinct payer name only when needed; provider owns required billing details. No universal Instagram/contact/document list.

Admin authority: current chef business capabilities; necessary technical support only.
Version/key checks and audit required. No policy edit silently rewrites accepted orders.

### quote — GK-003/019/023/025/027

order_template immutable version, sent_at/expires_at exactly24h, accepted terms/price/recipe/policy FKs, customer proof; attempt holds15m but pending funds persist. Accepted Q2:24h initiation; late paid waits for chef feasibility and explicit customer agreement, otherwise full return. late_payment_resolution binds order/version, feasibility actor/time, agreement evidence and honour/full_return state; waiting blocks preparation/fulfillment. Free confirmation explicit per GK-007. No accepted unpaid order.

Admin authority: current chef business capabilities; necessary technical support only.
Version/key checks and audit required. No policy edit silently rewrites accepted orders.

### identity — GK-003/010/012/019/021/029

profile provider_subject unique/project issuer, staff_capability/local active state, guest digest/revocation, chef-approved order_claim verified subject+safe evidence. Infrastructure role never application authorization; payer never automatic owner.

Admin authority: current chef business capabilities; necessary technical support only.
Version/key checks and audit required. No policy edit silently rewrites accepted orders.

### payment — GK-003/011/019/025/026

collection(order FK, purpose cake|delivery, expected bigint pence GBP, account/env/provider IDs unique, operation key/hash, state, authoritative_success_at, observed_at); accepted method inventory17 labels separate from qualified capabilities. Optional Stripe local presentment accepted; retain amount/currency/conversion evidence separately from GBP integration accounting; non-GBP settlement unapproved. Historical external receipts unique reference; no new offline-paid switch.

Admin authority: current chef business capabilities; necessary technical support only.
Version/key checks and audit required. No policy edit silently rewrites accepted orders.

### legal — GK-003/005/019/037

policy_content_version(status, audience public|internal, source hash, business approver, review_ref, effective_at nullable until activation); version accepted order snapshots. Internal notes not public. Deferred qualified review before final Terms/launch, no compliance assertion.

Admin authority: current chef business capabilities; necessary technical support only.
Version/key checks and audit required. No policy edit silently rewrites accepted orders.

### privacy — GK-003/005/019/021/027/033/034/037

retention_policy_version and category-specific due_at, erasure_job, scoped_legal_hold(reason/review_due/actor), photo_permission(scope/anchor/withdrawn_at), deletion_tombstone; current business periods from canonical JSON and K09. Provider/backup behavior verified in GK-037; Q3 accepted: prior-consent basic checkout-step/repeat-visit statistics, excluding account/admin browsing, names/emails/order details and replay. Typed analytics policy pins event/property allowlists and route exclusion; no business identity mapping.

Admin authority: current chef business capabilities; necessary technical support only.
Version/key checks and audit required. No policy edit silently rewrites accepted orders.

### business — GK-005/012/019/022/037

business_profile_version(legal_seller/trading_name/public contacts, protected legal_address_ref, source hash); food_evidence(FHRS_ID, authority, inspected_date, rating, source retrieval); separate attestation/verification dates, never invented registration date.

Admin authority: current chef business capabilities; necessary technical support only.
Version/key checks and audit required. No policy edit silently rewrites accepted orders.

### allergen — GK-005/007/019/022/030/032/037/038

flavour/variant/recipe_version, ingredient and recipe_allergen join FKs; common four plus Oreo sulphites/Snickers peanuts; exact shared-kitchen notice separately versioned. Label reflects actual accepted order/decor. Customer-agreed changes preserve prior snapshot.

Admin authority: current chef business capabilities; necessary technical support only.
Version/key checks and audit required. No policy edit silently rewrites accepted orders.

### tax — GK-005/007/019/023/025/026

tax_status_version(registration_attested boolean, add_separate_tax boolean, evidence/effective dates); price/discount/refund bigint pence, half-up then largest remainder. Display whole pounds without decimals, fractional amounts2 places. Accepted totals protected from future tax changes.

Admin authority: current chef business capabilities; necessary technical support only.
Version/key checks and audit required. No policy edit silently rewrites accepted orders.

## Complete canonical decision registry

Each pointer resolves in its named packet. Current value, qualifiers and status there
are normative business input; profile above and schema specify typed technical mapping.
Old examples remain labelled example/history and cannot override current refund targets.

### BO-GK-001 registry

Source: [GK-001 decisions](../creator/GK-001/decisions.json).

| Decision / pointer | Status | Typed profile | Revision SHA-256 |
| --- | --- | --- | --- |
| `BO-GK-001.work_week` / `/decisions/work_week/business/value` | confirmed | schedule | `096b57e9ccc7d044494b33e7db3c91d76c329246f0ba664254fe8784695f3cf2` |
| `BO-GK-001.daily_limit` / `/decisions/daily_limit/business/value` | confirmed | capacity | `ad07906a20af0395b985afdbce381d2b5215e9ab6e439984c250ed00e4abba91` |
| `BO-GK-001.capacity_exceptions` / `/decisions/capacity_exceptions/business/value` | confirmed | capacity | `2f22c6c8d2a50b753b4785817cdd864c6ef21890a20ba04c453dd236f1048cf5` |
| `BO-GK-001.lead_time` / `/decisions/lead_time/business/value` | confirmed | schedule | `6aa8767533ab0fb321957af2679142659f5ec14a428fb9e7aaa3deddca0cc138` |
| `BO-GK-001.wedding_recommendation` / `/decisions/wedding_recommendation/business/value` | confirmed | schedule | `e286e04efd417284d8507327ea918790142586b9c0086131713e9356f69ee870` |
| `BO-GK-001.urgent_requests` / `/decisions/urgent_requests/business/value` | confirmed | capacity | `57457f7a3079e4afd6c629de340deab7198fb1e25e4793179ea44fbeb42abb56` |
| `BO-GK-001.booking_horizon` / `/decisions/booking_horizon/business/value` | confirmed | schedule | `73022a4f8f95b17ca4fc798cf1eb67c399cf22a2acc599939b53fabfdf06f56e` |
| `BO-GK-001.production_start` / `/decisions/production_start/business/value` | confirmed | start | `7fdcb5ff344a2b6d59de1b6b71e13d54d08b60c00519679001636ba787d72d4d` |
| `BO-GK-001.ready_preparation` / `/decisions/ready_preparation/business/value` | confirmed | start | `a4c09f17481be398f7a258d34626be025062bf4a83e39caa027326896365f336` |
| `BO-GK-001.ready_cancellation` / `/decisions/ready_cancellation/business/value` | confirmed | refund | `93a75483b1f82df48df6c370f44ec6f1dc6a90e98bb288ed05567e1e992828a8` |
| `BO-GK-001.pickup_location` / `/decisions/pickup_location/business/value` | confirmed | location | `c52e8456fba18f262ac3c85feca6d4a7cb8c2273924815d86152141253422776` |
| `BO-GK-001.address_release` / `/decisions/address_release/business/value` | confirmed | location | `7e50dff561d5c75431c552bb827c026e2a0b191dc3bd70cf20a78c737248c7b9` |
| `BO-GK-001.pickup_hours` / `/decisions/pickup_hours/business/value` | confirmed | schedule | `7b9d9f3ed6a8545d821878be89cf9156bbcba1bac50ebda4cf1847936e95b73b` |
| `BO-GK-001.pickup_slot` / `/decisions/pickup_slot/business/value` | confirmed | capacity | `d1ddb32d476bd41f4cc68a6083d5d368fa2e81e3b293ad6f098a3fefba6d8bc1` |
| `BO-GK-001.move_date` / `/decisions/move_date/business/value` | confirmed | move | `b0fe9fea35c17afa28ec0a0ade7cc0d77c745b08f2944a4a0e97e557fcd8cde0` |
| `BO-GK-001.move_time` / `/decisions/move_time/business/value` | confirmed | move | `7f8b0043a7b01e951fd602e094a9c5138e11db1c9c71950f9d04fd4b52dc58a4` |
| `BO-GK-001.refund_follows_move` / `/decisions/refund_follows_move/business/value` | confirmed | move | `31d16f84ff44fe6cbc850a3f384983369600693b60f5f6a9f8713a779c135eb8` |
| `BO-GK-001.fulfillment_modes` / `/decisions/fulfillment_modes/business/value` | confirmed | delivery | `7c995b690d3ec1e62be3209389caf34a659aa94a5de74417921f98fbf73c9c15` |
| `BO-GK-001.customer_courier` / `/decisions/customer_courier/business/value` | confirmed | delivery | `1dae8c235a2ea77f85e188595ba94a7a7d91c38b69bb0d4474f2589214e72046` |
| `BO-GK-001.gk_delivery` / `/decisions/gk_delivery/business/value` | confirmed | delivery | `ebecd9fd00441cb5a28a531f06b47dc24aab972d694b57b6954c21f2cd4f8eff` |
| `BO-GK-001.delivery_geography` / `/decisions/delivery_geography/business/value` | confirmed | delivery | `2714357c59e2f9f3f5c28a400c3645e09a5079201c6e8b8b12c2ca24f473d8bd` |
| `BO-GK-001.schedule_admin` / `/decisions/schedule_admin/business/value` | confirmed | schedule | `b2be725d4e4b2d5e55706c2dfcd2537bc2bc92cb63af07281a6da9bffa38891a` |
| `BO-GK-001.capacity_concurrency` / `/decisions/capacity_concurrency/business/value` | confirmed | capacity | `ca0b164e85eef5af31fc0a855df6416f0b54873463ac5f53c6577070d012850a` |
| `BO-GK-001.courier_content` / `/decisions/courier_content/business/value` | confirmed | delivery | `9e504fd74ae76aabe76686e4511c3cfc2d0d9d6e4a4e731c078a5032a6c42faa` |

### BO-GK-003 registry

Source: [GK-003 decisions](../creator/GK-003/decisions.json).

| Decision / pointer | Status | Typed profile | Revision SHA-256 |
| --- | --- | --- | --- |
| `BO-GK-003.request_fields` / `/decisions/request_fields/business/value` | confirmed | request | `870636834f4d617b79f1a1ac5080e8da9f9075c777cb6511142d6a1bd91f0fcf` |
| `BO-GK-003.order_contacts` / `/decisions/order_contacts/business/value` | confirmed | request | `6b2b3546181de5a4dbd20e171e8d4bfce4c71bf89b7e8ce0118ba02693320e28` |
| `BO-GK-003.communication` / `/decisions/communication/business/value` | confirmed | request | `0a1ca125294c6705f2f934d639d64ac283c49e6e1390b0da9ee4dae557ead445` |
| `BO-GK-003.agreement` / `/decisions/agreement/business/value` | confirmed | quote | `851a14526b0518376acf7d3e774b56d45b288b05fbc5354f4cdb4e66c6ee8a0c` |
| `BO-GK-003.paid_commitment` / `/decisions/paid_commitment/business/value` | confirmed | quote | `d32d13aaa5e6774a31706df04dd3fceb4ceb15d6560d78e63fe9c612cb0026e8` |
| `BO-GK-003.quote_validity` / `/decisions/quote_validity/business/value` | confirmed | quote | `e73bc0443511f27b76984e7c269c84515d3ce33e649645e476949139b7c65d41` |
| `BO-GK-003.daily_limit` / `/decisions/daily_limit/business/value` | confirmed | capacity | `9262a2df6bf6ace0743cf142f46eeecd63618dd5e0fcb7d28f8952c54e94c908` |
| `BO-GK-003.capacity_exception` / `/decisions/capacity_exception/business/value` | confirmed | capacity | `199a8cd08f3d5138bc6bc6a8372949e6c1fd54e31f7d4df9bc4b4afb9e6fdbd0` |
| `BO-GK-003.pricing_factors` / `/decisions/pricing_factors/business/value` | confirmed | quote | `fd1eb08620900ef822c105bf0d06abeb136cc854c7d6f7d83dc5d2af5d1687a3` |
| `BO-GK-003.guest_purchase` / `/decisions/guest_purchase/business/value` | confirmed | identity | `db66bacedc15c3f6fa14cd444ad2dd60a0b23b53b41f4aa125a369e0224000a2` |
| `BO-GK-003.reorder` / `/decisions/reorder/business/value` | confirmed | identity | `7c3c1552e96d257bd7be528d6fc6169adae1d864b51f3fca0f51d2637d7c6eaa` |
| `BO-GK-003.historical_claim` / `/decisions/historical_claim/business/value` | confirmed | identity | `c84121255514017cb71a9dcd1e1da84d31c2429482f95ab2dbd98067e41df298` |
| `BO-GK-003.payment_methods` / `/decisions/payment_methods/business/value` | confirmed | payment | `b7dcb6a590e92de522d61ad8af380bfd3dc53f6a8f9e83c4a00c8e573b99ee5e` |
| `BO-GK-003.cancellation_early` / `/decisions/cancellation_early/business/value` | confirmed | refund | `92751b1e4b59d9ec750e05776846849bf56679e5ef4e92c787b8296f3f487b70` |
| `BO-GK-003.cancellation_late` / `/decisions/cancellation_late/business/value` | confirmed | refund | `b44d62dccb5defcafed270ced4ff29ad5f8cdd5194873c8c46e2597c2ec28a4b` |
| `BO-GK-003.started_or_ready` / `/decisions/started_or_ready/business/value` | confirmed | refund | `ada433c04f10f14591bc101bb1fa7cf7ca5b38908a2c0519bbf3ad29fd5f8fa4` |
| `BO-GK-003.after_handover` / `/decisions/after_handover/business/value` | confirmed | refund | `9aa41d7dcb5c50aa1e5c744439681190225c0badbee1f16e3a62518170ad34be` |
| `BO-GK-003.no_show` / `/decisions/no_show/business/value` | confirmed | move | `ca5fb5c08f5c27d46de93afc0affefcfc25a70b3672c5d670d19252e23cab73f` |
| `BO-GK-003.business_cancellation` / `/decisions/business_cancellation/business/value` | confirmed | refund | `1d0666613a0155e65c8effc54b053cb75b9633ea4f6066b4f2d07fc73bafd4ba` |
| `BO-GK-003.refund_allocation` / `/decisions/refund_allocation/business/value` | confirmed | refund | `13ac4cf71789897e0d0f22f8031b6b2757527849460201ec4660472a65fdcef3` |
| `BO-GK-003.refund_example_delivery` / `/decisions/refund_example_delivery/business/value` | example | refund | `ca62786ca0843998eca9976382dd185cc13222ffd9d486900b47ef1e4f3b3e2f` |
| `BO-GK-003.refund_example_discount` / `/decisions/refund_example_discount/business/value` | example | refund | `cc9ad49addb8b8c44dd6da2d34c9f45362b3025987aefbc34f66afa4d9c8e7cc` |
| `BO-GK-003.refund_example_remaining` / `/decisions/refund_example_remaining/business/value` | example | refund | `7ca7ac5e3dce8ba0adfadc9a259895306089cbf7f57fd511e4044256c6ad70fb` |
| `BO-GK-003.offline_evidence` / `/decisions/offline_evidence/business/value` | confirmed | payment | `f4d4cfc1169cf31bc513667b2e71cfa8c7841553d54f4a6ddca02345071f0658` |
| `BO-GK-003.legal_questions` / `/decisions/legal_questions/business/value` | confirmed | legal | `4a752427c7d009de03370c4b5852e98ccc960b473c6e0ea333bf247ed99b040e` |
| `BO-GK-003.cancelled_order` / `/decisions/cancelled_order/business/value` | confirmed | refund | `265a1ad16a301a4586cf169d59e7ae6142fcd7f8d073b92212a09b4eb26cac30` |
| `BO-GK-003.resale` / `/decisions/resale/business/value` | confirmed | refund | `9630c59aac99a1edf3b4e8309b05b0e4ba49cf9eb67294b02d1fe4a9ab3b485c` |
| `BO-GK-003.admin_exception` / `/decisions/admin_exception/business/value` | confirmed | refund | `8b0ae0f483a3e6f5cac2e5fcb165b99d3bfdb17e88f17f34395483214a4a7f92` |
| `BO-GK-003.delivery_failure` / `/decisions/delivery_failure/business/value` | confirmed | refund | `84744f10d3be93cfe1ed21c34a2a0fa90a589c9e71a0a50021616557fe0c9ee1` |
| `BO-GK-003.checkout_hold` / `/decisions/checkout_hold/business/value` | confirmed | quote | `9cb711ccd6ccf1a0285e5a1485a9baeab905cce2515108414dedb1e66f524c33` |
| `BO-GK-003.manual_order_link` / `/decisions/manual_order_link/business/value` | confirmed | payment | `a9c38b8b79fe82958e878fbf4add21e23902c8a9d693009b88d3a0354fcae93f` |
| `BO-GK-003.publication_preference` / `/decisions/publication_preference/business/value` | confirmed | privacy | `28f303d9eeaab12a74b76f56be7452ec19133e848a5fcc03dcccce7ae816cae5` |
| `BO-GK-003.refund_rounding` / `/decisions/refund_rounding/business/value` | confirmed | refund | `30cfa5efb56de891b8f82449b0df13d85772038b7f9eaf302b882f576daa8e7b` |
| `BO-GK-003.refund_execution` / `/decisions/refund_execution/business/value` | confirmed | refund | `3128f6b278c963f17c1a45a72d8d0c20aada03125e5cbb78aac54adfdc2fee3d` |

### BO-GK-005 registry

Source: [GK-005 decisions](../creator/GK-005/decisions.json).

| Decision / pointer | Status | Typed profile | Revision SHA-256 |
| --- | --- | --- | --- |
| `BO-GK-005.public_identity` / `/decisions/public_identity/business/value` | confirmed | business | `f45946aaa4ef16d9524c649e04a5d5437874c50bc6918b6e33086ff6c9ba73ba` |
| `BO-GK-005.food_status` / `/decisions/food_status/business/value` | reported_fact | business | `e99c92a01e3cc77bb851bb49ab10d3509770e61caa36f92937ce0a61ad06dee6` |
| `BO-GK-005.identity_documents` / `/decisions/identity_documents/business/value` | confirmed | privacy | `badde9c3e9d37abb6c5c2e49e6e5935705b64c040f7cf656d93353237c8688ef` |
| `BO-GK-005.flavours` / `/decisions/flavours/business/value` | confirmed | allergen | `40dd38fa5b00dd83de4db9b89c44348e6f9afea012e893e6624d9f2cd312f9b0` |
| `BO-GK-005.allergens` / `/decisions/allergens/business/value` | confirmed | allergen | `3dade43fe5452311508474cb3dc3aab2dbdbacadace4e6627105c5a3d1c9a587` |
| `BO-GK-005.cross_contact` / `/decisions/cross_contact/business/value` | confirmed | allergen | `ea64ed35e3a1b117c665c6c2d0629ee267f1bca7a0dac159e9d7f87cb1bdb35a` |
| `BO-GK-005.handover_label` / `/decisions/handover_label/business/value` | confirmed | allergen | `5be2d4916b3e0f88202bed0a2601f5852543efd616635caa8f953451907f9646` |
| `BO-GK-005.vat` / `/decisions/vat/business/value` | reported_fact | tax | `a6a346d0dedb9c579e4ac72387a5b91800b26f002a8ed80e7149de3fc6b80b29` |
| `BO-GK-005.refund_basis` / `/decisions/refund_basis/business/value` | confirmed | refund | `b88edd40a350d7947f14771faa85425748b89735dab04c46dcd1ac470851869b` |
| `BO-GK-005.refund_example` / `/decisions/refund_example/business/value` | example | refund | `fad3b1c0f2e0deeb3f6044e38c1a53f19d8eccb1629bdcb5a98f7c56231a2863` |
| `BO-GK-005.money_precision` / `/decisions/money_precision/business/value` | confirmed | tax | `00d05c48b7740f9c461555d76b0d8b7593346a77554e786cde74833ca53e2d73` |
| `BO-GK-005.policy_confirmation` / `/decisions/policy_confirmation/business/value` | confirmed | refund | `ec00c29bb54823acc4af36fb6ff71fd645e63eeafbb0ec8e37748642b84b200f` |
| `BO-GK-005.statutory_notice` / `/decisions/statutory_notice/business/value` | confirmed | legal | `d25f45b31760cc9f7aea44c1456a0e3349fa1cd7a8b37649b3e65ca136978b6e` |
| `BO-GK-005.internal_legal_note` / `/decisions/internal_legal_note/business/value` | confirmed | legal | `0086fc206db6b5b1a5c5afc71c440eb583c501d74c4af3035b590649c83b2d94` |
| `BO-GK-005.customer_data` / `/decisions/customer_data/business/value` | confirmed | privacy | `d09c5fa04c80a154f6b1b544c01b4c8e56a8337fc8066a6278536f3c19046397` |
| `BO-GK-005.privacy_incomplete` / `/decisions/privacy_incomplete/business/value` | confirmed | privacy | `09a0717c6d27720be4101331d0225a4836d7a37ebbe9630dc5d3226a0238be43` |
| `BO-GK-005.recipe_changes` / `/decisions/recipe_changes/business/value` | confirmed | allergen | `56ef7236b2b40b0f231db66d6145593a53181b2af8acb9b9ee8e5c847e59b6bf` |
| `BO-GK-005.coating_decor` / `/decisions/coating_decor/business/value` | confirmed | allergen | `69be0d5f064eeeb7f97af17f9445e9a7efb44b27d1e6503f5c1d4693b95c7d31` |
| `BO-GK-005.retention_enquiries` / `/decisions/retention_enquiries/business/value` | confirmed | privacy | `e78b76811d39d8a4f217cc5c53da6dc9e09ed97eff49725efc5885506c5fb906` |
| `BO-GK-005.retention_photos` / `/decisions/retention_photos/business/value` | confirmed | privacy | `88d81500fac584c249ee4610a4c2f5dfbbc4a40254fa224ba744246c8b1209d2` |
| `BO-GK-005.retention_orders_accounts` / `/decisions/retention_orders_accounts/business/value` | confirmed | privacy | `fd2391c57cbab22f7ec7038d7c0b48404f311fbbe92e72320f1d937ad91f944a` |
| `BO-GK-005.browser_analytics` / `/decisions/browser_analytics/business/value` | confirmed | privacy | `4857b543dcb411451ce2580e4b1a117b3fe16d6d96db40b997bdbfa2f524dcc5` |
| `BO-GK-005.service_email` / `/decisions/service_email/business/value` | confirmed | privacy | `2a665d626d6165ef44557ae4825dd26304ceb8cb61841898daf24706d6799e13` |
| `BO-GK-005.current_refund_example` / `/decisions/current_refund_example/business/value` | example | refund | `7e75e5793a1a59ffe8d71b1656c2fc89188c3556e30b6d6303a0120acc2c6adc` |

## Cross-card precedence and current gaps

- GK-001/003: fixed original120h refund boundary,100/50, all cake types; later GK-007 free-order exception is explicit. Current website methods include Stripe Revolut Pay; old offsite Revolut invoices remain history.
- GK-005 interview is now canonical in decisions/source JSON. Existing historical refund_example remains original £70 arithmetic only; current_refund_example supplies approved £52.93 target. Common 50% rounding agrees with customer-favour half pennies; arbitrary percentages follow current half-up rule.
- GK-007 supplies approved one-code/free/quantity/component/negative-stock/expiry rules in K03/K07 and schema. GK-038 retains measured yields/conversions/missing costs.
- GK-004 provider/recovery approval and GK-010/011/012 setup supply concrete resources,17 methods, sender and combined Instagram action. They are not application test evidence.
- Personal customer photos require specific use permission; general order-photo opt-out never authorizes those or third-party references.
- Q1/Q2/Q3/Q5 and Q4 optional currency are accepted by Sasha; Q4 current bundle also accepted subject to GBP compatibility; future additions/non-GBP settlement still need explicit approval if encountered. Visible available delivery/pickup slot selection is approved, without repeated Check. GK-037 remains after GK-035 before GK-036 for final legal/privacy/product verification.

Publication snapshots and receipts stay unchanged. Current amended ticket bodies are
[separate drafts](gk019/amendments/README.md), not falsely rewritten historical publications.
