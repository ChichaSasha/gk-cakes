# GK-018 r01 — daily chef operations sketch (Today + order detail)

Status: shown to Creator; feedback pending. Not approved. Example data only; the prototype
saves, sends, charges and refunds nothing and makes no network calls.

- Card: GK-018 https://trello.com/c/  (moved Backlog → In Progress 2026-11-03 on the user's "Yes, take GK-018").
- Base: origin/main 784a717. Visual tokens/fonts reused from approved GK-015 r12.
- Model check: host model llmplatform/claude-opus-5-5 (user-selected) vs card policy gpt-5.6-sol/high; user selection takes precedence.
- Bounded r01 scope: A-ORDERS/A-FULFILLMENT Today view and order detail, desktop + mobile.
  Shows standard-capacity count vs private chef extras, "Needs you" recovery queue (unknown payment,
  late payment awaiting agreement, delivery quote request, unconfirmed refund, bounced email),
  automatic 48h preparation vs chef actual start, allowed next action, same-day move rule,
  chef-cancel 100% refund preview (customer 100%/50% deadline shown), separate delivery refund,
  email delivery statuses that never claim inbox receipt, stale-version lock and manager/support limits.
- Later revisions: calendar & capacity editing, manual order entry, ingredients/batches/stock,
  groceries/cost, custom message compose/preview, money dashboard.
- Run: `python3 -m http.server 8780 --bind 127.0.0.1` in `poc/`, open `index.html`.
