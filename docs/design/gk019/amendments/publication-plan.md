# Proposed Trello publication — exact scope, awaiting Sasha review

This file is a proposal, not authorization or a publication receipt.

1. Update only the25 card descriptions enumerated in [manifest](manifest.json), using
   each complete `.description.txt` exactly. Preserve title, owner, labels and unrelated
   text. Refresh descriptions/comments/prerequisite statuses and abort affected writes
   on drift rather than overwrite human changes.
2. Add exactly this [acceptance evidence comment](GK-019-evidence-comment.txt) to
   [GK-019](https://trello.com/c/lW3wuYq0). Read it back and verify the25 descriptions
   against reviewed hashes. Reconcile uncertain writes before retrying.
3. Only after those checks and accepted review evidence satisfy AC1–AC4, move GK-019
   to the board's verified Done list. Do not change any other card's status, labels,
   title or owner. If a write/check is incomplete, leave GK-019's status unchanged.

No application implementation, Git publication, provider configuration, release or
 deployment is included. GK-015 visual artifacts remain separately owned; the exact
contract handoff is the only cross-context change requested here.

The96-file design revision was independently reviewed at
`0b294af7c7888e7c2acb598ea548870e9a6ebfa9121148c6a751e8a008f34f94`.
The final packet adds only this publication scope and exact evidence comment/index;
its output manifest pins those additional bytes for final review.
