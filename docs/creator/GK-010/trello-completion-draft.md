# GK-010 completion comment — unpublished draft

Publish only after Sasha approves evidence r14 and the first-manager subject,
and authorizes posting this handoff and moving GK-010 to Done. At publication,
record his actual approval; this draft does not claim approval already exists.
Target: https://trello.com/c/5AdmfDVH

## Setup evidence r14 — 2026-10-05

Scope: selected sandbox data, identity, storage and recovery setup. No production
purchase, application implementation or deployment is claimed. GK-004 was read
as Done with final r13 acceptance; GK-010's older provider-selection blocker is
historical.

Evidence artifact: `docs/creator/GK-010/README.md`, r14.
SHA-256: `21bef0d3ab0cf22cbe53448ee33a8e108362001c2d0df4c34081a96fbc0fb77f`.
Public CA artifact: `docs/creator/GK-010/supabase-root-2021.crt`.
SHA-256: `700723581420dd1ac98fd7e9ac529f0ef210eadcaf87fc868a3ad7d114c2f3b7`.
These files are currently local, uncommitted; this comment supplies the usable
nonsecret setup summary, and the exact local packet must accompany implementation.

### AC1 — resources, TLS, identity configuration and protected access

- Supabase Free sandbox, London/eu-west-2, project `flbgymxjelzexgoxrcss`.
  Dashboard: https://supabase.com/dashboard/project/flbgymxjelzexgoxrcss
  Guided names: organisation `GK Cakes Sandbox`, project `gk-cakes-sandbox`.
- Session pooler `aws-0-eu-west-2.pooler.supabase.com:5432`, database `postgres`,
  user `postgres.flbgymxjelzexgoxrcss`. Administrative bootstrap access only.
  Direct endpoint `db.flbgymxjelzexgoxrcss.supabase.co:5432`, user `postgres`,
  database `postgres`; direct IPv6 was not tested.
- Agent verified TLS 1.3 with `sslmode=verify-full` and the supplied public CA.
  A plaintext connection was rejected with ESSLREQUIRED.
- Issuer configuration: `https://flbgymxjelzexgoxrcss.supabase.co/auth/v1`, derived
  from the provider's documented project URL convention. Audience `authenticated`
  is also verified in the selected Auth user's database row.
  JWKS: `https://flbgymxjelzexgoxrcss.supabase.co/auth/v1/.well-known/jwks.json`.
  Public key inspected: ES256, kid `fc7db0e8-68ca-47e0-b10c-0ecf0f615629`.
  No issued token or application verifier was tested; no callback URLs invented.
- Database recovery password: Bitwarden organisation `GKCake`, collection
  `GK Cakes Recovery`, item `GK Cakes Sandbox — Database admin`.
  Authorized Agent setup uses the owner-readable local file
  `/Users/sasha.chycha/.config/gk-cakes/sandbox-db-password` outside Git
  (directory 0700, file 0600). This mechanism is plaintext; Bitwarden is the
  recovery vault. No credential value is included here.

### AC2 — first manager and storage

- Intended first sandbox manager: Sasha. Project Auth subject
  `22ac712e-4e38-48bb-a56d-fa7809ccc13f`; email address omitted from publication.
- The supplied invitation record and direct read-only lookup agree:
  email confirmed 2026-10-05 17:39:00.290077 UTC; last sign-in
  17:39:00.296717 UTC; not anonymous and no active ban. Exactly one UUID was
  queried; no email, token or password hash was selected. psql exit 0.
- This supersedes the earlier deferral of the first-manager identity. The owner's
  separate app identity may be added later. Infrastructure Owner accounts remain
  separate; no application manager role has yet been granted.
- Agent verified `catalog` public and `custom-private` private. Neither has
  bucket-specific size/MIME overrides; zero policies on `storage.objects`.
  No application object-access tests are claimed.

### AC3 — recovery, separation and backup destinations

- Sasha reports two Supabase infrastructure Owners, MFA and recovery material,
  shared Bitwarden recovery access, and a successful fresh recovery-account login
  by the business owner. B2 MFA/recovery storage is user-confirmed; no independent
  B2 recovery login test is claimed.
- Approved separate credentials: restricted application database access; schema
  migrations; backup source reads and backup destination writes. These roles and
  keys are a documented implementation plan, not already provisioned privileges.
- Approved DB and object backup destination: private B2 EU Central bucket
  `gk-cakes-sandbox-backups-flbgymxjelzexgoxrcss`, ID `3cb17ebd0aa001d7a01c0813`,
  endpoint `s3.eu-central-003.backblazeb2.com`; `database/` and `objects/` prefixes.
  User confirms SSE-B2 enabled, Object Lock disabled, empty bucket, temporary
  Keep all versions lifecycle. The approved recovery policy is unchanged;
  retention automation and backups/restores have not been implemented.
- Sasha creates, rotates and revokes credentials, including exposed credentials;
  business owner has emergency recovery access and takes over if unavailable.

### AC4 and downstream boundaries

Final Creator approval and publication are required to complete this handoff.
No code PR or deployment is part of this Creator-owned setup ticket. Document
checks passed and the three recorded local input fingerprints remain unchanged.
GK-020 owns migrations/runtime roles; GK-021 owns callbacks, sessions, local
manager authorization and disable tests; GK-022/GK-027 own application image
access tests; GK-034/GK-035 own executable recovery verification as applicable.
Completion of GK-010 does not establish that every other dependent-ticket
prerequisite is ready.
