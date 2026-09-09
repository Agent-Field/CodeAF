# One evolving demonstration

Updated 2026-09-09. The user accepted this working method after rejecting the
earlier multi-step delivery plan as inefficient. Keep one system drawing and one
real demonstration together. Choose the next change by the uncertainty it
resolves; use the five domains as substitutions and counterexamples. This
accepts the process, not unresolved product behavior.

## The current experiment

A source chat records a response-contract finding. Backend, frontend and
documentation chats each belong to an explicitly targeted collection. They
produce reports using the same sourced record. The source changes from
`receipt_id` to `operation_id`; consumers are explicitly resumed sequentially
and produce new reports with revision 2. The original outputs remain separate
files. A subsequent withdrawal/reopen exercises absence of current context.

This is the existing `api_contract` functional case, not a new demo engine or
accepted-instruction implementation. Its fixture uses three collections and
three consumers. It exercises production agent/tool/storage behavior with a real
model; the domain consumer case is not an interactive TUI session. Binary tool
assembly and ordinary task workers have separate existing cases.

Run from the implementation worktree using the existing documented runner:

```sh
ORGANIZATION_TEST_RUN='^TestOrganizationE2E$/api_contract$' make test-organization-live
```

The runner builds normally, uses a disposable profile and requires an already
available OpenRouter key. Keep existing runtime and ledger limits. See the
integration branch's `FUNCTIONAL-TESTS.md` and `TEST-RESULTS.md` for prerequisites,
model pins, evidence and limits. Do not copy credentials or use real user data.

The implementation owner ran this one existing case at
`12017ee2ab6021de4ef8a1c9c817b2d08f4325fb`. It passed in 208.34 seconds with
DeepSeek V4 Flash and reported $0.034454. The source record was
`a9bdf7e6ad4c880426f2d54a62e851b5`, originating conversation `42b0e800945d71c0`.
All three initial artifacts cite revision 1; all three resumed artifacts cite
revision 2 of that same record/source. After withdrawal, the resumed backend
reports unavailable with revision 0 and empty record/source IDs.

Evidence directory: `/Users/santoshkumar/af-personal-ai-demonstration-20260909/`.
`api-contract.jsonl` is the full run receipt; `evidence.json` indexes seven
artifact contents extracted from the logged, asserted files. Copies under
`artifacts/` are extractions from that log, not the original disposable fixture
files. Root inspected this evidence; it did not independently rerun the model.
No production or test semantic change was requested for this demonstration.

## Drawing and evidence must stay distinguishable

The interactive discussion drawing is:
`/Users/santoshkumar/.codex/visualizations/2026/09/09/01a08653-1fcf-7880-b64f-dae46f29b86a/shared-work-loop.html`.

It substitutes the five existing domain fixtures into the same structure and
illustrates initial reads, source revision, one consumer resuming, and remaining
consumers resuming. The API selection replays the observed values and timestamps
from the fresh run; other domains are labeled illustrations. It has no live model
or product connection. UI interactions
can validate the illustration; they cannot validate aforge behavior.

The existing test observes sequential resumed reads and their artifact revisions.
Collections and membership are fixture setup. The source is explicitly asked to
record/revise information with supplied targets. Withdrawal is a direct store
operation. None is proof of inferring an ordinary decision or its scope.
It does not independently assert that an idle consumer remained unchanged or
that background work was never admitted. Those intermediate states are identified
as an illustration, not a new tested guarantee. It also does not establish
automatic decision retention, recursive folder scope, automatic activation,
unlinked discovery, peer consultation, real calendar mutations or a final UI.

## First missing transition: a person's decision becomes applicable direction

### Counterexample from the expanded workday test

The implementation owner subsequently reported a failure within the EXISTING
explicit applicability contract. After removing both memberships and reopening
a long chat, the turn wrote unavailable. Its completion path then read a
remembered record ID and rewrote the old value as current because the record
still existed and was not withdrawn. Global record currency was confused with
applicability to the current consumer.

The reported failing receipt is
`/Users/santoshkumar/af-personal-ai-complex-evidence-20260909/working-day-confirm.jsonl`.
The owner's later handoff `ca2a59d5a` records a repair at `c43119be5` plus
receipt-display clarification `3a6140a72`. Forced remembered-ID reading passed
against unchanged production `3a6140a72`: the old body remained readable,
`applicable_here=false`, final current value empty, and no membership restored.
The longer repaired workday also exercised removal/rejoin/withdrawal correctly.
The COMPLETE workday remains red for a separate completion-driven numeric-to-string
artifact regression, recorded as issue #672. Large retrieval and conflicting
sources passed. The bundle README distinguishes these results; original failures
remain preserved. This is the owner's report, not a new root rerun.

Keep global reads available. A source can be current and discoverable while not
applying to this chat. Acceptance for the repair must cover the whole turn,
including completion, and rejoining under existing explicit membership rules.
This is not a new privacy boundary, recursive inheritance rule or automatic
accepted-decision feature. It exercises the diagram's “select relevant state”
arrow and shows why that arrow needs precise semantics.

### The next product behavior beyond that repair

The ordinary request is: “For this project, use operation_id from now on.”
The current test instead explicitly asks the model to record shared information
with supplied targets, and the resulting store treats it as information. Therefore
the first gap in the full product experience appears before automatic wakeups:
the person's accepted decision must retain its source and understood scope and
be distinguishable from a quoted finding or another agent's suggestion.

C09 already confirms retention without a separate save command. The open behavior
is exactly what the person meant the decision to govern and how the runtime uses
that authority. “Current record” must not be relabeled “accepted instruction” in
the drawing or code to conceal this gap.

Proposed behavior to discuss with the person:

- The response briefly exposes what aforge understood, for example “I'll use
  operation_id for this project's API work.” Source and scope are inspectable
  and correctable without another mandatory save step.
- Preserve one identifiable decision when it is revised. A reference from
  elsewhere makes it available for consideration; it does not extend its scope.
- Once source and applicability are unambiguous, the next resumed relevant
  effort uses the current decision under its existing delegation.

These are proposals, not confirmation of folder-subtree inheritance, scope after
moving work, conflict resolution, live interruption or automatic new work.
Resolve only the concrete uncertainty needed for the next experiment. Any new
feature slice gets the user-requested isolated build task after its behavior is
confirmed; the current owner can demonstrate already-built behavior now.

## How this becomes the whole-system demonstration

Keep the same records and visible work while extending the missing transitions.
As activation, consultation or correction is agreed and built, replace the
corresponding illustrated transition with a real receipt. Sketch observing,
entering, steering and stopping at the same time. Select the next experiment by
how much it clarifies the common behavior, not by a fixed subsystem checklist.
Do not require every journey or the final interface before learning from one
working transition. Do not declare the whole environment complete from one
shared-context case either.
