# GK Cakes UI

SvelteKit customer/admin UI, built as one Node artifact (`@sveltejs/adapter-node`)
and deployed separately from the API. It holds no backend secrets: database,
payment, email, webhook and storage-write credentials stay in the API.

## Toolchain

- Node `26.13.0` (LTS "Lithium"), pinned in `.nvmrc`; `engines` plus
  `engine-strict` reject other majors.
- npm `11.20.0` (bundled with that Node), lockfile `package-lock.json`.

## Commands

Run from `ui/`:

| Purpose                        | Command                       |
| ------------------------------ | ----------------------------- |
| Install exactly from lockfile  | `npm ci`                      |
| Format and lint                | `npm run lint`                |
| Type check                     | `npm run check`               |
| Unit tests                     | `npm test`                    |
| Backend secrets not in bundles | `npm run test:bundle-secrets` |
| Production build               | `npm run build`               |
| Start the built server         | `node build`                  |

`test:bundle-secrets` builds with a random canary in every backend-only variable
and fails if any canary appears in `build/`. Run it before `npm run build`, since
both write `build/`.

## Runtime

`node build` serves on `PORT` (default 3000). Set adapter-node's `ORIGIN` to the
public site origin. Liveness: `GET /healthz`.

Environment variables must be declared in `src/env.ts` (SvelteKit 3) before code
can import them; only deliberately public values may be declared `public`.

| Variable                      | Rule                                                                                   |
| ----------------------------- | -------------------------------------------------------------------------------------- |
| `PUBLIC_API_URL`              | exact API origin, e.g. `https://staging-api.gkcake.com` (https; http only on loopback) |
| `PUBLIC_AUTH_URL`             | identity provider project URL, e.g. `https://<project>.supabase.co`                    |
| `PUBLIC_AUTH_PUBLISHABLE_KEY` | the provider's publishable key (`sb_publishable_...`); a secret key is refused         |

All three are public by design and read at start (one build serves every
environment). They are optional: without them the server still starts and
sign-in says it is not configured.

## Sign-in (GK-021)

`/sign-in` (email and password, or an emailed one-time link), `/auth/callback`,
`/account` and `/admin` (staff). These pages render only a shell on the server
(`ssr = false`); the browser loads data from the API with the signed-in user's
access token as `Authorization: Bearer` (never cookies or URLs).

- The provider SDK (`@supabase/supabase-js`, PKCE flow) keeps the session in
  memory and persists it, access and refresh tokens included, in this origin's
  `localStorage` under `gk-auth` (the provider-supported store; a GK-021
  decision for Creator review, protected by the Content Security Policy).
- Email sign-in links return a one-time code to the exact, allowlisted
  `/auth/callback`, exchanged with the verifier only the requesting browser
  holds. Invitation emails link to `/auth/callback?token_hash=...&type=invite`
  (verified once). The callback removes the code or token hash from the URL and
  history before it calls the provider; same-origin page scripts load first,
  with no referrer.
- When the app starts, `src/hooks.client.ts` removes sign-in secrets from any
  other URL (a provider code, token hash or error, and any implicit-flow
  `#access_token`/`#error` fragment), wherever the provider sent the browser.
- After an email link, the user returns to `/account` or `/admin` only; the
  choice is kept in `localStorage` for 30 minutes (links usually open in a new
  tab), never taken from the URL.
- Sign-out ends the provider session when the provider is reachable (the API
  then refuses that session at once on staff routes) and always clears the
  stored session; the page says so if the provider could not be reached.
  Signing out in one tab sends the others' private pages to sign-in. A 401
  from the API signs out and shows "Your session has ended".
- Messages never reveal whether an email has an account, including when
  sending a link is rate-limited; an unreachable provider is reported.
- Response headers (`src/hooks.server.ts`, checked in CI by
  `scripts/check-headers.sh`): Content Security Policy with
  `script-src 'self' 'nonce-…'`, `style-src 'self'`, `style-src-attr
'unsafe-inline'` (inline style attributes only, for SvelteKit's route
  announcer), `object-src 'none'`, `frame-ancestors 'none'`, `base-uri` and
  `form-action 'self'`, and `connect-src 'self'` plus `PUBLIC_API_URL` and
  `PUBLIC_AUTH_URL`; `nosniff`; `Cross-Origin-Opener-Policy: same-origin`.
  Private routes (decided by the matched route, not the raw path) add
  `no-store`, `no-referrer` and `X-Robots-Tag: noindex, nofollow`.
- The bundle secret check needs no new variables: the `PUBLIC_*` values are
  public and read at start.
- Styling is functional only until the account designs (GK-016) are approved.

No browser test runner is configured yet; one is added when a runner is approved
for the first interactive screens. CI starts the built server and checks
`/healthz` instead.

## Dependency audit

`npm audit` reports low/moderate advisories in development tooling only
(eslint/minimatch, and deepmerge used by the Svelte Vite plugin at build time).
None of these packages are present in `build/`, and npm's suggested fixes are
major downgrades. CI therefore does not run `npm audit`; revisit when upstream
releases fixes.
