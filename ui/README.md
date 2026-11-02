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
