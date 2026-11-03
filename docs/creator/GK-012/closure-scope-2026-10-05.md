# GK-012 accepted completion split — r5

Sasha replied **“yes”** to: “Do you approve this completion split and authorize
me to record it on the affected tickets, then mark GK-012 Done?” on 2026-10-05.
This supersedes the earlier instruction to keep GK-012 open. It authorizes scoped
updates to GK-012, GK-020, GK-021 and GK-027 and a Done transition for GK-012 only.
It does not authorize application implementation, deployment or owner-card closure.

## Completed GK-012 scope

- Purchased `gkcake.com` through Cloudflare; Sasha confirms DNS access,
  auto-renewal/payment method, MFA and Elina's recovery access. Public NS/CNAME/DKIM
  queries matched the supplied export. Account assertions remain user-reported.
- Approved HTTPS addresses: `gkcake.com`, `www.gkcake.com` redirect,
  `gkcake.com/admin`, `api.gkcake.com`, `staging.gkcake.com`,
  `staging.gkcake.com/admin`, `staging-api.gkcake.com`.
- Email identity: `GK Cakes <orders@gkcake.com>`, Reply-To
  `gk.cake.london@gmail.com`. Sender-name conflict explicitly resolved by Sasha;
  historical GK-011 test payload retained.
- Elina controls `@gk.cake.london` and can read DMs, reported by Sasha.
  Approved direct link: `https://ig.me/m/gk.cake.london`.
- Approved interaction: “Copy request & open Instagram”; customer pastes and
  sends. Retain summary/reference and manual-copy fallback. No prefill guarantee
  or assertion that opening/copying sent a DM.
- User-reported chat opening succeeded on Mac/web and iPhone/app, and Android
  in response to that device's question (app/browser detail not supplied).
- Revision-linked handoff published and verified; Sasha accepts the scoped
  completion. No application checks or independent review are claimed.

## Required downstream work, retained rather than waived

| Owner ticket | Required deliverable before its acceptance |
| --- | --- |
| [GK-020](https://trello.com/c/W8q5svCh) | Establish actual UI/API/local-development environment values and bootstrap configuration from the approved hostname inventory and selected provider/session contract. Supply exact runtime inputs to GK-021; reject missing/unsafe cross-environment config. Do not guess login callback paths or use production wildcards. |
| [GK-021](https://trello.com/c/zEfCJtWD) | Derive exact login callback URLs from GK-020 and the chosen identity implementation; record/configure the precise environment allowlists and prove valid callbacks succeed and invalid/foreign redirects are denied. |
| [GK-027](https://trello.com/c/G8hLmSBU) | Reconcile K02/feature/ticket wording with the approved account and combined button before implementation. Implement and test copying, opening, pasting and manual fallback on iOS/Android/desktop, retaining the same summary/reference on failure and making no DM-sent claim. |

These remain implementation acceptance requirements. The earlier chat-opening
checks do not prove clipboard/paste/failure behavior. Closure supplies the GK-012
prerequisite only; all other dependencies and review/test/delivery gates remain.
No implementation ticket is automatically Ready or Done.

## Acceptance interpretation

- AC1: scoped domain/DNS ownership/access and hostname evidence accepted.
- AC2: account control, actual chat-opening observations, approved handoff/fallback
  policy and user-sends responsibility accepted here; implementation proof assigned
  explicitly to GK-027.
- AC3: approved nonsecret environment/hostname and email inventory accepted here;
  exact implementation-derived callback configuration and denial tests assigned
  explicitly to GK-020/021.
- AC4: explicit Creator acceptance, revision fingerprints and verified publication;
  closure receipt must verify the downstream comments before the Done move.

Original ticket descriptions and r4 comments remain historical. This approved
scope amendment supersedes their requirement to keep GK-012 open for the named
downstream application checks. The record does not fabricate successful tests.
