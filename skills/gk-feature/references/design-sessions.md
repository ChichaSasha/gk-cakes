# Iterative design sessions with the Creator

Use for an explicitly requested GK Cakes visual-design session or a planned
`Kind: Design` ticket. The outcome is an approved, coloured screen sketch with
buttons and enough behaviour detail to implement it. A written specification,
moodboard or greyscale wireframe alone does not meet that outcome.

## Session loop

1. Agree a bounded screen/flow and its user task: for example gallery → cake
   detail, the custom-request constructor, or admin order detail. Read its current
   rules, approved visual direction, real assets and previous feedback. Reuse
   existing decisions; ask about missing preferences only where they matter.
2. For the first visual direction, show a small set of meaningfully different
   options when useful, with a recommendation. For later sessions, start from the
   accepted direction. Do not make the Creator repeatedly reselect fonts/colours.
3. Produce and **show** a visual sketch early: rendered colours, typography,
   spacing, imagery, controls and button labels. Prefer a small editable HTML/CSS
   mockup when no design tool is already established; use an existing design file
   when available. This prototype needs no production stack, backend or deployment.
   Use image-generation/visualization skills only when relevant to the artifact.
4. Invite focused feedback on the displayed revision: visual feel, hierarchy,
   colours, button wording/placement and the ease of the intended action. Include
   free-form feedback. Wait for the Creator's answer before claiming agreement;
   silence, time elapsed and an agent review are not visual approval.
5. Revise the sketch itself, show the updated preview and briefly identify changes.
   Preserve previous feedback and the last accepted version. Repeat until the
   Creator approves a specific revision or leaves named changes pending.
6. Check the approved sketch against behaviour/accessibility and shared contracts.
   Record the version, approval evidence, choices, unresolved items and affected
   implementation tickets. A changed shared rule goes back to the planning master;
   visual approval does not invent or settle missing business policy.

Present previews in the conversation or available design/browser panel, with a
usable source link; do not hand the Creator only a path or a prose description.
If preview tooling is unavailable, supply a viewable export and disclose what
could not be inspected. Do not call unrendered markup a visually verified sketch.

## Acceptance criteria: design ready for implementation

Use these in each design ticket, scoped to the agreed screens:

- **AC1 — Visible sketch:** the agreed flow has rendered mobile and desktop
  sketches with actual colours, text, spacing and cake imagery or clearly labelled
  temporary imagery. No blank placeholder boxes for the main interaction.
- **AC2 — Visual decisions:** exact colour values, typography and spacing choices
  are available in editable source or a compact specification. Text/button
  contrast and visible focus have been checked; meaning is not conveyed by colour
  alone. Distinguish proposed choices from approved ones.
- **AC3 — Buttons and actions:** primary/secondary buttons and relevant controls
  have visible labels and a defined destination/action. Include relevant default,
  focus, disabled and loading appearances. A click-through prototype is required
  where navigation/interaction is the decision under review; otherwise annotated
  states are sufficient. Label simulated actions so they cannot imply a real order,
  payment, email or Instagram message was sent.
- **AC4 — Important states:** show the states that affect this flow, such as an
  unavailable date, validation error, empty selection, successful request summary
  or uncertain payment. Link each to the screen-to-behaviour mapping. Keep the
  existing complete behaviour contract even when not every state needs a mockup.
- **AC5 — Iteration and approval:** Creator feedback has been applied or explicitly
  resolved, and the Creator approved the identified sketch revision. Record actual
  evidence of that approval; “agent thinks it is ready” is insufficient.
- **AC6 — Handoff:** editable source plus viewable previews/exports are linked;
  screen IDs, chosen tokens, button behaviour and outstanding implementation
  details are clear. A fresh planning review checks integration consistency before
  affected UI implementation tickets become Ready.

Readiness is scoped: an approved visual direction does not approve every screen.
Unresolved behavioural decisions may leave a visually approved sketch awaiting
integration; the corresponding implementation ticket remains blocked. A prototype
is not a working app, and its fake data is not production evidence.

## Save, resume and sequence

Save under `docs/design/sessions/<flow>/`, for example `session.md`,
`v01.html`, `v01-mobile.png` and `v01-desktop.png`, using the chosen tool's native
format where appropriate. These are future artifact examples, not existing files.
Keep one concise session note: goal/screens, source revisions, latest preview,
feedback → change, chosen values, approval status/version and next open question.
Do not overwrite the only copy of an approved revision during later exploration.

Stay in the same context while iterating this flow. If a fresh context is needed,
load the session note, actual latest/approved previews, governing contracts and
pending feedback using the model/context check. Do not restart visual direction
research or discard the Creator's prior choices simply because context changed.

Create one cohesive Agent-owned design ticket per useful flow/session outcome,
with the Creator as visual reviewer. Put it in Review while awaiting feedback;
do not mark it Done or infer approval from time passed. Separate Creator decision
or asset-supply tickets only when they are independently actionable blockers.
Visual-foundation decisions precede dependent sessions; backend work may run in
parallel where contracts are settled. Implementation of affected UI depends on
the exact approved sketch revision and integration review. Global style changes
invalidate dependent visual approvals where they materially change the design.
