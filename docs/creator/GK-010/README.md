# GK-010 sandbox setup evidence

Source: [GK-010](https://trello.com/c/5AdmfDVH). Setup evidence revision r14,
2026-10-05, supplied by Sasha in the guided setup conversation. This is a
nonsecret handoff, not a second live ticket-status record. Trello remains the
execution-status source. This record has not been published to Trello.

## Evidence boundary

The agent read GK-010 and GK-004 from Trello. GK-004 was in Done with final r13
acceptance comments; GK-010's description still held its historical provider
selection blocker. The setup observations below are Sasha's confirmations and
copied dashboard parameters, not agent-authenticated inspection or independent
review. The agent subsequently performed the explicitly recorded read-only TLS,
storage, public signing-key and Auth-user checks below. No application, least-privilege, restore or deployment
test is claimed.
No direct business-owner approval statement was collected in this conversation.

## Sandbox resource inventory

| Item | User-reported value |
| --- | --- |
| Provider | Supabase |
| Organisation | `GK Cakes Sandbox`, following the guided creation steps |
| Project name | `gk-cakes-sandbox`, following the guided creation steps |
| Project reference | `flbgymxjelzexgoxrcss` |
| Dashboard | <https://supabase.com/dashboard/project/flbgymxjelzexgoxrcss> |
| Region | London; session-pooler hostname identifies `eu-west-2` |
| Plan | Free, explicitly confirmed by Sasha |
| Direct database host | `db.flbgymxjelzexgoxrcss.supabase.co` |
| Direct port / database / user | `5432` / `postgres` / `postgres` |
| Session pooler host | `aws-0-eu-west-2.pooler.supabase.com` |
| Session pooler port / database / user | `5432` / `postgres` / `postgres.flbgymxjelzexgoxrcss` |
| Catalog bucket | `catalog`, public |
| Attachment bucket | `custom-private`, private |

The guide instructed leaving the new buckets empty with default policies.
The agent later verified both bucket rows/flags and the absence of storage object
policies as recorded below. No object access tests have been performed. The initial
`postgres` credential is administrative, not the future application runtime role.
Direct Free connections use IPv6; the shared session pooler supports IPv4 without
a paid add-on. The session-pooler checks below passed; direct IPv6 connectivity
has not been tested. No production resource or purchase
was performed or verified in this session.

## Account security and recovery decisions

- Sasha uses his personal GitHub account to sign into Supabase. He confirmed
  Supabase MFA and a backup factor/setup-secret recovery method were configured.
- Sasha initially declined a second infrastructure Owner, selected a secure
  recovery vault, then explicitly accepted a separate recovery account after
  identifying the personal GitHub dependency. The later decision supersedes the
  initial refusal. Personal GitHub credentials are not part of the agreed shared
  recovery record.
- Sasha confirmed that the business owner's separate email/password Supabase
  account accepted the invitation and both accounts appeared as Owner.
- Sasha confirmed MFA and backup setup for that recovery account, plus the
  owner's own Free Bitwarden account, two-step login and a recovery code stored
  outside Bitwarden.
- The agreed vault is a Free two-person Bitwarden organisation `GKCake`,
  collection `GK Cakes Recovery`. Sasha confirmed completion of the guided
  invitation/acceptance/member-confirmation and recovery-item storage steps.
  This is ordinary shared access, not Bitwarden's paid delayed Emergency Access.
- Sasha reported that she could access the shared collection and complete a
  fresh email/password plus MFA Supabase login to the sandbox in a private
  browser window. This is user-reported recovery-test evidence.
- Sasha supplied the exact shared database-password reference: Bitwarden
  organisation `GKCake`, collection `GK Cakes Recovery`, item
  `GK Cakes Sandbox — Database admin`. His supplied organisation name corrects
  the earlier guided name `GK Cakes`. Scoped Agent access is recorded below.
  No credential values or MFA secrets were supplied in chat or stored in this
  handoff. Sasha later supplied his application-user email in chat; it is omitted
  from this handoff because the provider subject is sufficient for implementation.
- Sasha explicitly accepted responsibility for creating, replacing and revoking
  Supabase and B2 credentials, including replacing exposed credentials. The
  business owner retains emergency recovery access through the shared Bitwarden
  collection and takes over if Sasha is unavailable. This records Sasha's
  decision; no separate direct business-owner approval was collected.

## Public database CA certificate

Sasha supplied `/Users/sasha.chycha/Downloads/prod-ca-2021.crt` after the download
step. Its parsed public certificate is saved as
[`supabase-root-2021.crt`](supabase-root-2021.crt); no private key is stored.
OpenSSL reports subject/issuer `Supabase Root 2021 CA`, organisation `Supabase Inc`,
`CA:TRUE`, validity 2021-04-28 10:56:53 UTC through 2031-04-26 10:56:53 UTC.
Certificate SHA-256 fingerprint:
`80:70:25:AD:50:D4:ED:21:9D:2C:9C:7D:29:9C:00:4F:82:4E:B0:0C:F7:F6:5A:FE:F6:07:D0:7B:72:E6:CA:FA`.
The supplied file SHA-256 is
`700723581420dd1ac98fd7e9ac529f0ef210eadcaf87fc868a3ad7d114c2f3b7`.

Local parsing and verification using the supplied certificate as its own trust
anchor succeeded. This establishes neither provider provenance independently nor
a live endpoint connection. The intended client setting is `sslmode=verify-full`
with the appropriate CA trust, verified during downstream connection setup.
No machine-wide trust store was changed. The provider's `prod-ca-2021.crt` filename
does not change this task's sandbox scope. After supplying the certificate, Sasha
explicitly answered "Yes" to confirming that "Enforce SSL on incoming connections"
was enabled and saved. The subsequent live session-pooler checks below verified
an encrypted connection and rejection of a plaintext connection.

## First application manager: identity verified, final approval pending

Sasha selected himself and the business owner as the intended sandbox managers,
with separate application logins. He then clarified that the two infrastructure
accounts were sufficient for now and agreed to postpone application identities.
That earlier deferral is superseded by the following setup evidence. Sasha asked
to use his own identity, reported an empty Authentication > Users list, followed
the invitation setup, and supplied the resulting user record with confirmation
and sign-in timestamps. The intended first sandbox application manager is Sasha.
The business owner's application identity remains for later setup; GK-010 only
requires the first manager. Infrastructure Owner access remains separate.

Provider subject: `22ac712e-4e38-48bb-a56d-fa7809ccc13f` in project
`flbgymxjelzexgoxrcss`. The supplied record identifies an email-provider account.
On 2026-10-05 the agent independently queried only this UUID in `auth.users`
using the protected sandbox credential, verified TLS and a read-only transaction:

- `aud=authenticated`.
- `invited_at=2026-10-05 17:38:44.792738+00`.
- `email_confirmed_at=confirmed_at=2026-10-05 17:39:00.290077+00`.
- `last_sign_in_at=2026-10-05 17:39:00.296717+00`.
- `is_anonymous=false`; no currently active ban.
- One matching row, psql exit code 0; no email, password hash, token or other
  users' data was selected.

These provider-controlled timestamps support email verification and a completed
authentication event. The user-editable `raw_user_meta_data.email_verified`
field is not relied on as proof. No agent-performed application login is claimed.
Final Creator approval of this subject as the first sandbox manager remains
pending. No application manager role has been granted; GK-021 owns local role,
callback/session and disable behavior after its implementation prerequisites.

## Agent-assisted access selection

Sasha explicitly selected agent-assisted sandbox setup: he supplies task-scoped
credentials locally; the agent uses them for authorised setup/checks without
printing their values. Full Bitwarden-vault access and personal GitHub/MFA
credentials are outside this selection.

Automatic approval review rejected the proposed macOS Keychain command with
"Do not run macOS security/keychain commands." No Keychain secret was accessed.
The fallback is an owner-readable local file outside Git:
`/Users/sasha.chycha/.config/gk-cakes/sandbox-db-password`.
Directory mode `0700` and empty file mode `0600` were created through approved
filesystem escalation. The file is not encrypted by this mechanism; Bitwarden
remains the recovery vault. Sasha confirmed password entry; the agent verified
file ownership/mode and successfully used the credential for the read-only
checks below. This password represents initial sandbox `postgres` administrative
access, not a least-privilege runtime credential. Runtime/migration/backup roles
remain GK-020 implementation work. Do not use these credentials outside the
identified sandbox or expose them in command arguments/logs.

## Read-only connection verification — 2026-10-05

Target: the recorded session pooler on port 5432, database `postgres`, username
`postgres.flbgymxjelzexgoxrcss`. Client: installed `/opt/homebrew/bin/psql`, with
`-X`, `--no-password`, `ON_ERROR_STOP=1`, 10-second connection timeout and a
25-second process timeout. The password was read from the protected local file
and supplied in the child process environment, never printed or put in command
arguments. No personal vault or Keychain access was used.

- Positive: `sslmode=verify-full` and this packet's `supabase-root-2021.crt`.
  `\conninfo` reported TLS 1.3 / `TLS_AES_256_GCM_SHA384` on the client-to-pooler
  connection. In a read-only transaction, `current_database()` was `postgres`,
  `current_user` was `postgres`, and the current `pg_stat_ssl` row reported
  `ssl=t`, `TLSv1.3`, `TLS_AES_256_GCM_SHA384` for the database connection.
  Exit code 0. No application/customer rows were queried.
- Negative: `sslmode=disable`, no password supplied, no interactive password
  prompt or usable password file. Server rejected the connection with
  `(ESSLREQUIRED) SSL connection is required for user: postgres`; exit code 2.
  psql also warned that `/dev/null`, used to disable password-file discovery,
  was not a plain file. This did not affect the explicit server rejection.

An initial successful multi-statement command printed only `COMMIT`, so the
evidence query was rerun as separate psql commands to capture the actual results.
These checks prove the current bootstrap connection path, not the later runtime
role permissions, direct endpoint, application identity or backup correctness.

## Additional read-only resource verification — 2026-10-05

Using the same verified-TLS connection and a read-only transaction, the agent
selected only `catalog` and `custom-private` from `storage.buckets`. Both names
equal their IDs; `catalog.public=true`, `custom-private.public=false`.
`file_size_limit` and `allowed_mime_types` are NULL for both (no bucket-specific
override; no claim about project-wide limits). A scoped `pg_policies` count for
`storage.objects` returned zero. psql exit code 0. This is configuration evidence,
not application storage authorisation testing.

An unauthenticated HTTPS GET to
`https://flbgymxjelzexgoxrcss.supabase.co/auth/v1/.well-known/jwks.json`
returned one public signing key: `alg=ES256`, `kty=EC`, `crv=P-256`, `use=sig`,
`kid=fc7db0e8-68ca-47e0-b10c-0ecf0f615629`. This records the published verifier key
at inspection time, not a successful user login, issued JWT or role assignment.
No private signing material was requested or recorded.

Expected sandbox user-token verifier configuration, derived from the project
reference and the provider's [JWT claims reference](https://supabase.com/docs/guides/auth/jwt-fields)
(read 2026-10-05): issuer `https://flbgymxjelzexgoxrcss.supabase.co/auth/v1`,
audience `authenticated`, and the JWKS endpoint above. The Auth-user query also
verified this user's audience as `authenticated`. The issuer follows the
provider's documented project URL convention. No issued token was inspected and
no application verifier was installed; those execution checks remain downstream.
Actual callback URLs remain a GK-020/GK-021 output; none are invented here.

## Independent backup destination

Sasha confirmed creating a new Backblaze B2 account and explicitly confirmed its
EU Central region. He subsequently confirmed enabling authenticator MFA, saving
backup codes, and the business owner's access to the B2 login/recovery items in
the shared Bitwarden collection. This is user-reported configuration/access
evidence, not an agent login or a demonstrated B2 recovery login.

Sasha copied these bucket details from the console:

| Field | User-supplied value |
| --- | --- |
| Name | `gk-cakes-sandbox-backups-flbgymxjelzexgoxrcss` |
| Bucket ID | `3cb17ebd0aa001d7a01c0813` |
| Created | 2026-10-05 |
| Type | Private |
| Lifecycle | Keep all versions |
| Snapshots / current files / current size | 0 / 0 / 0 bytes |
| S3 endpoint | `s3.eu-central-003.backblazeb2.com` |

Sasha subsequently explicitly confirmed SSE-B2 default encryption enabled and
Object Lock disabled pending the recovery design. This is user-reported evidence;
the agent has not inspected the B2 account. Keep-all-versions is the temporary
empty sandbox setting, not implementation of the approved 30-day recovery policy.
Credential rotation/revocation ownership is agreed above; the approved access
and destination plan is recorded below.
No paid activation, upload, backup job or restore is claimed.

## Approved privilege and backup destination plan

Sasha explicitly approved this plan in the guided setup conversation:

- The application uses its own restricted database credential.
- Schema migrations use a separate credential for schema changes.
- Backups use separate credentials for reading the required source data and
  writing to the identified sandbox B2 bucket.
- Both database and file backups use
  `gk-cakes-sandbox-backups-flbgymxjelzexgoxrcss`, under separate `database/`
  and `objects/` prefixes respectively.

These are intended privileges and approved destinations, not provisioned roles,
issued backup keys or verified access controls. Implementation must scope the
credentials to their required sandbox operations; the existing administrative
`postgres` credential is not the application credential. Exact permissions and
executable privilege/restore checks remain downstream. This decision does not
change the approved recovery policy or implement retention automation.

## Acceptance review

- AC1: sandbox/project/region, TLS identifiers, provider issuer/audience,
  protected bootstrap secret name/location and resource identifiers are recorded
  above. Configuration evidence is distinguished from downstream token/verifier
  execution checks. No callback URLs are invented.
- AC2: the intended first manager's subject and provider email confirmation are
  verified above; final Creator approval remains pending. Supabase bucket
  rows/flags are agent-verified; application access tests remain downstream.
- AC3: the protected sandbox setup credential, intended app/migration/backup
  privilege separation, exact approved database/object backup destinations and
  rotation/revocation ownership are now documented above. Include them in the
  final Creator review. No executable backup process exists yet; implementation
  and permission/restore verification remain downstream.
- AC4: final Creator review of the exact evidence revision and authorized Trello
  publication. Do not mark Done while required evidence remains unresolved.

The next step is Creator review of r14, approval of the recorded first-manager
subject, and authorization to publish completion evidence on GK-010 and move it
to Done. Until then, no completion or Trello publication is claimed.
Migrations/runtime roles belong
to GK-020; callbacks/local manager checks to GK-021; application storage checks
to GK-022/GK-027; executable recovery verification to GK-034/GK-035 as applicable.

## Input fingerprints

SHA-256 of the locally maintained inputs read for this handoff:

| Input | SHA-256 |
| --- | --- |
| `docs/design/tickets/GK-010.md` | `f9ec80f18f983b371366a4e199dbe6765acc62f9408454fb25bb0bc0c57111c0` |
| `SETUP.md` | `c673d6f0015e8804c4e51f49fbfcb5e5bcbcaa3d72302e20b7ed7abce9d83fe4` |
| `docs/design/adr/0002-providers.md` | `7693ff208e027f9068bfcd97da72b9c387e334fdc8e74e0ff449d6e3b033a66c` |

These planning inputs are uncommitted in this checkout; HEAD alone does not
identify them. Existing staged and untracked user work was preserved.
