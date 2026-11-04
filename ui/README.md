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
  memory and persists it in this origin's `localStorage` under `gk-auth`, the
  provider-supported store. Email links return a one-time code to the exact,
  allowlisted `/auth/callback`; the page removes it from the URL and history
  before any other request and exchanges it with the verifier only this
  browser holds. Where to go next is kept in `sessionStorage`, limited to
  `/account` and `/admin`, never taken from the URL after the link.
- Sign-out ends the provider session (the API refuses that session at once on
  staff routes) and clears the stored session.
- Messages never reveal whether an email has an account.
- Content Security Policy: script and style only from this site plus
  SvelteKit's nonce'd inline code, `frame-ancestors 'none'`, `object-src
'none'`; `connect-src` adds `PUBLIC_API_URL` and `PUBLIC_AUTH_URL` at runtime
  (`src/hooks.server.ts`). Private pages are `no-store` and `no-referrer`.
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
