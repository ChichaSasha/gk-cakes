

---

## Accepted completion scope — 2026-10-05

Sasha explicitly approved the account-provisioning handoff r16 and this split, authorized publication on GK-011/019/024/025/026, and authorized Done for GK-011 only. This amendment supersedes the original provider/legal/DNS blocker and completion interpretation above; other original requirements remain in their assigned scope.

Completed here: selected UK/GBP Stripe sandbox acct_1UNHEgPqGToF2ulH, working user-tested sandbox API access, recorded default 2026-08-26.dahlia, current enabled methods accepted with Google Pay disabled; saved setup credentials; verified Resend domain gkcake.com, Free/Ireland/Sending on/Receiving off; approved current sender GK Cakes <orders@gkcake.com>, Reply-To and controlled recipient gk.cake.london@gmail.com; supplied Resend delivered event and user-confirmed receipt; domain/MFA/recovery/renewal evidence. Live merchant verification is user-reported completed. The historical email test used the previous GK Cake London display name. Full evidence and source hashes are in the verified GK-011 handoff comment.

Required downstream acceptance work (not waived or claimed passed):
- GK-019 https://trello.com/c/lW3wuYq0 : reconcile the complete accepted payment-method set and current sender into contracts; replace card-only assumptions while preserving full-payment and uncertain-outcome rules.
- GK-024 https://trello.com/c/J6zlPAc9 : runtime email credentials, actual endpoint/events/signing secret and signature/recovery tests; current sender-name verification; exact DNS/DMARC review, terms/safeguards, limits/alerts and key rotation.
- GK-025 https://trello.com/c/vyFTtISc : runtime payment credentials, SDK-compatible API/event versions, exact event allowlist and endpoint; all applicable selected-method qualification, capture/full-return, expiry/late-success and idempotent recovery checks; fees/access/rotation record.
- GK-026 https://trello.com/c/xiEN4jvp : partial/full-remaining refunds and failure/retry/concurrency proof using GK-025's shared refund core.

AC1 accepts account provisioning/default-version evidence here; actual adapter/event configuration and payment/refund qualification belong to the named downstream tickets. AC2 accepts domain/identity/controlled-send evidence; final display-name and DMARC checks belong to GK-024. AC3 separates provisioning from future reachable signed endpoints and deployed runtime secrets. AC4 is explicit Creator acceptance plus revision-linked, verified publication before the Done transition.

GK-020 remains the runtime prerequisite; GK-035/GK-037 retain integrated/prelaunch checks. No application tests, independent review, production readiness, live-money test or deployment are claimed. Other tickets and BO-GK-011 are not moved. See the verified downstream comments for full mandatory handoff details.
