# Current checklist for the system diagram

Updated 2026-09-09. Numbers match [SYSTEM.svg](SYSTEM.svg). This is the current
inventory, not new primitives or implementation teams. Confirmation sources remain
in [DECISIONS.md](DECISIONS.md); simulation evidence and limitations remain in
[EXPECTATIONS.md](EXPECTATIONS.md).

The goal is one personal AI environment for direct work, parallel collaboration
and ongoing responsibilities across domains. Cover the useful complete task space
of comparable systems and enable useful combinations with less repeated explanation,
inconsistent assumptions and steering effort. Coverage remains to demonstrate under
real tool, access, hosting, cost and reliability constraints; the diagram and model
agreement do not establish it.

## Agreed versus still open

“Agreed” means product behavior is accepted or its explicit example is understood.
It does not mean implemented. No entire diagram area is fully complete.

| Area | Agreed / clear | Still to groom or prove |
| --- | --- | --- |
| 1 — Organization | Start anywhere; predictable return; automatic reversible discovery; one current artifact through multiple entry points; history preserved; correct mistaken associations without deleting content; moves differ from references. | Exact folder/chat/artifact views and retrieval quality; unclear or multiple memberships; implementation. |
| 2 — Decisions and memory | Explicit decisions need no extra save; preserve source/scope; exceptions change only conflicting guidance; unchanged rereading respects corrections; explicit current-folder rules change future guidance after moves while item-specific direction remains. | Ambiguous intent and general conflicts; inferred context versus accepted direction; forgetting, retention and memory-off behavior; actual scope enforcement. |
| 3 — Relevant context and changes | Readable does not necessarily mean applicable. The explicit API rename falls within the already-authorized active feature, including its completed docs, without requesting that same permission again. | General reaction to observed changes; affected-work discovery; running/idle/completed outputs; refresh timing and actual adoption. The full automatic-revision policy remains proposed. |
| 4 — Ongoing work and activation | Finite work differs from ongoing responsibility and an individual run. Existing authorization persists within scope; learning does not create obligations. | Events that justify a check, run, notification or nothing; responsibility lifetime; permission boundaries; closure/restart; duplicates, overlap, budgets and stop cutoff. |
| 5 — Cooperation | Parallel workers fit existing execution; communication is not person authorization; permanent managers or rosters are unnecessary. | When consult or delegate; discovery/addressing, ownership, disagreement, shared work, bounded exchanges and retries. |
| 6 — Actions, outputs and reuse | Artifacts, sources and methods can persist; current outputs differ from historical messages; actual tools determine available actions. | Artifact identity/version/dependencies; external effects and uncertain outcomes; maintenance versus new work; method reuse/improvement; no silent publishing or authority expansion. |
| 7 — Human experience and control | Find, inspect, correct and stop; views share underlying state; no mandatory organization ceremony. | Concrete TUI arrival, history/current-output display, progress, scope inspection, notification and correction; autonomy presentation, custom-view boundaries and actual usability. |

The API permission explanation is narrow: the person already authorized the
feature across backend/frontend/docs and explicitly changed its field. Updating
those parts completes that assignment; deployment remains excluded. It does not
settle every approval default or authorize independent new work.

## Work completed in this discussion

- Preserved the whole-system goal, existing concepts and seven-area diagram.
- Recorded confirmed organization, retention and scope through C16, with sources
  and boundaries; C17 preserves the task-coverage goal.
- Defined three software and two nonsoftware journeys as tests of general rules.
- Built the reusable multi-model probe; completed four rounds and a focused
  rationale check. Paired answers and counterfactuals exposed defects and open
  choices, not universal convergence or proof of usability.
- Preserved isolated-build, real-E2E and reintegration rules in DELIVERY.md.

## Implementation evidence is separate

The first backend wave has bounded evidence for collections/membership, owner
lookup, sourced records, revision/withdrawal, explicitly resumed consumers and
ordinary workers. Scope-removal evidence distinguishes readable from applicable
information. It does not prove automatic decision capture, semantic discovery,
triggers, cross-chat cooperation, the new TUI or all five complete journeys.

The latest inspected implementation handoff is `c63e03b7f`, draft #662 unmerged.
It records a full 12-chat working-day pass at `3f678b893`, worker/conflicting-source
reruns at `fb95cd197`, and five bounded first-wave domain cases with stronger
unrelated-chat scope assertions at `c9b9ff388`. These do not establish five complete
final-product journeys. Completion-judgment issue #672 remains open.

The independent behavioral audit on production commit `11c92fd1d6` reported
failures in honest collection-add reporting, disclosure of the existing `/land`
step for a follow-up correction, and recovery from watch setup failure. One run
misreported a source suggestion as accepted; no unauthorized migration ran.
Observed passes included quoted-source isolation, draft-local correction/reopen,
and stop/withdrawal/reopen. The watch case never approved its valid setup card,
so it does not establish successful ongoing monitoring. Ordinary CI passed;
that is distinct from the behavioral audit. These are recorded evidence reviewed
from the implementation handoff, not new runs by this discussion task or final
acceptance. See the owner's HANDOFF.md and DESIGN-STATUS.md for full receipts.

Existing v3 standing work already has background scheduled execution, setup
approval, history and controls. The new workspace context is not yet consumed by
scheduled firings, and a zoomable whole-workspace map is not implemented.
[YEAR-ONE.md](YEAR-ONE.md) and [YEAR-ONE.svg](YEAR-ONE.svg) now illustrate the
person's broader question: what a startup CTO's accumulated personal workspace
might look like after a year. The person found those large boxes unhelpful.
[INFORMATION-GRAPH.md](INFORMATION-GRAPH.md) and its SVG now focus on the simpler
structure: records, typed relationships and progressively selected subsets.
Neither study adds primitives or resolves pending behavior and permission choices.

## Next steps without changing direction

1. Resume the pending change-response choice: how active assignments, completed
   standalone work and explicitly maintained work react to a relevant change.
   Resolve needed permission, notification and stop boundaries in that same
   concrete flow; do not detour into a separate approval-system design.
2. Turn the smallest complete confirmed behavior bundle into reviewable acceptance
   and an isolated build task after inspecting reuse, interfaces, ownership and a
   stable integration base. Keep the current builder on its existing repair/E2E
   scope while we groom the remaining checklist.
3. Continue one evolving diagram and demonstration through memory conflicts,
   coordination and real effects as needed. Design the human view with each
   behavior rather than leaving the frontend to a final phase.
4. Before calling the whole system done, require the five full product journeys,
   negative/restart/stop cases, real TUI evidence and task-coverage gap review.
   Include actual access, unattended runtime and resource limits. Integrate only
   after the agreed evidence; no dev/staging/main promotion or release is implied.

This is a working focus, not a rigid multi-phase roadmap. A counterexample can
change the next experiment. Explain concrete consequences, viable alternatives
and a recommendation before asking the person to decide.
