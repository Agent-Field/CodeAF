# Conversation runtime implementation

Local integration line at `15ebb5d61`. This is a working record of what has merged into that
line, what is still open, and what the available evidence does and does not establish. It
describes the local integration only; nothing here is published or claimed as released.

## Merged

| Boundary | What it does now | Where |
| --- | --- | --- |
| Local delivery | One addressed hand-over (`conversationID`, `messageOrigin`, `messageKind`) returning one receipt: nobody / closed / accepted. Progress lands on the ambient queue and cannot start a paid turn. | `internal/session/mailbox.go` |
| Durable acknowledgement | A sender owed an answer about the recipient's *record* gets one when the recipient's journal holds the line, not when a queue took it. Ids are composed from checkpoint facts so a resume can tell a replay from a second event. | `internal/session/mailbox.go`, `task_store.go` |
| Assignment revisions | The admitted spec is frozen; corrections land as an overlay. Only a person's direction may move the done-condition; an agent's coordination is recorded but cannot re-aim the work. A landing may not publish while a person's direction is unread. | `internal/session/assignment.go`, `assignment_tool.go` |
| Admission context | One bounded, attributed selection per admission door: quotes with speaker and source, tool handles with call/input/outcome including an explicit unknown outcome. Versioned and checkpoint-durable. | `internal/session/admission.go`, `admission_compile.go` |
| Task result | The full answer is kept apart from the compact card, with a bounded excerpt and a retrievable overflow reference; the outcome qualification is retained. | `internal/session/task_result.go` |
| Tool-result compaction | Older frozen tool results are reduced to a bounded head and tail with the exact count of bytes cut and a pointer the read tool can open (`Agent.fullResultPointer`), falling back to the journal and then to an honest "cannot be read" rather than naming a place that does not exist. A pass folds to a headroom target below the threshold instead of re-firing at every step, and the budget walk carries a running total rather than recomputing it. The live transcript, the system prompt and the running turn are never rewritten, and no result is dropped. | `internal/session/toolcompact.go`, `stub.go`, `turnfold.go` |
| Status projection | One pure `ProjectTask` over facts, separating presence, wait-on, change disposition, fault and attention, with three-valued liveness. The surfaces adapt to it instead of each deriving meaning. Wire and checkpoint enums unchanged. | `internal/session/task_status.go`, `internal/tui3/taskstatus.go` |
| Persistent host and truthful detach | `Welcome.Persistent` is the engine's statement about its own lifetime. A window closing (including SIGHUP) detaches; work, standing questions, drafts and parked messages survive. `/close` and an explicit stop remain endings, and the quit hint promises "keeps running" only where it is true. | `internal/remote/client.go`, `internal/tui3/keeper.go`, `quitarm.go` |
| Foreground handoff | The end-of-turn reader asks whether this request's work went to a live task, so a turn that correctly handed work off is not re-opened into polling its own task. Request-scoped, no polling. | `internal/session/turnhandoff.go` |
| Workspace ownership | Alternate spellings of the same physical location resolve to one working copy when binding contract paths and checking parallel writable claims. | `internal/session/taskgit.go` and the ground ladder |
| Evaluation rig | A cross-harness battery with two doors that are never mixed (print and a real TUI in a tmux pane), calibrated screen markers per arm, `unsupported` recorded rather than substituted where no interactive door can be driven, guarded upstream calls pinned to an open model, exact answer checks, and a cost meter that reconciles admitted requests instead of counting missing usage as zero. It spends real money, is on demand, and is outside `make check`; `test/selftest.sh` exercises it with fakes and no network. | `bench/conversation/` (`run.sh`, `lib/`, `scenarios/`, `test/`) |

Each merge has been rebuilt with `make build`, and each boundary above has behavioral tests
alongside it (`mailbox_test.go`, `assignment_test.go`, `admission_test.go`,
`task_status_test.go`, `task_result_e2e_test.go`, `turnhandoff_test.go`,
`internal/remote/detach_test.go`, `internal/tui3/detachexit_test.go`). Focused tests are not
a substitute for the integrated suite or a live product walkthrough.

## Open — assigned, not shipped

These lanes are active elsewhere. **Nothing below is in this line yet, and none of it should
be described as working here.**

- **Verification / replay.** A live run found that the audit door treated a work receipt as
  permission to replay the action, re-running a two-minute script. A fix is assigned to the
  verification lane; still active, not integrated.
- **Causal wake.** The same run found that the main completion reader judged a task's result
  against the person's latest, unrelated question rather than against the request that caused
  the work. Fix code exists in a separate lane and is under test there: a landing is taken
  whole — the notice a surface draws, the attempt it belongs to and its reply tag together
  under one lock (`TaskNode.resultOf`) — and carried so a woken turn is judged against the
  request the result was owed to, with assignment-revision tests alongside it. **Pending until
  root confirms integration**; it is not in this line and this document does not claim it
  works.
- **Final integrated live validation.** The rig exists (above); the candidate's own final
  integrated interactive run has not been made. Existence of the harness is not a result from
  it.

## What the live evidence actually shows

From root's `host-live-02` run, two behaviors were observed to pass: the main conversation
answered an unrelated question in a few seconds while a task ran, with no polling; and a task
continued with the terminal closed, with no interruption marker and the same start time. The
same run produced three defects — a path-token trimming bug (since fixed), the audit replay
above, and the completion-reader causality above. The run's own note stands: **it does not
prove overall task efficiency or all steering behaviors.**

## What is measured, and what is not claimed

Measured and test-backed, in-tree: compaction bounds a long frozen tool history to a
sub-linear prompt, a pass reaches its headroom target and then leaves the next step alone,
and the budget walk carries the same total it would recompute. Those are specific
improvements on specific costs, and they are the only performance claims this document makes.

Not claimed: **no general Pareto superiority over another harness.** The preliminary *print*
comparison states outright that this system is not consistently cheaper or faster on the
small tasks measured; it exercised the print door rather than the interactive product, had
infrastructure and checker failures, undercounted cost before the meter fix, and ran too few
repetitions to support a claim either way.

The interactive peer comparison (`INTERACTIVE-COMPARISON-PRELIMINARY.md`, 2026-09-04, pinned
open model, one repetition per peer and case) records peer results only: both peers passed
the mid-work revision case, and both failed the answer-while-working case — one answered only
after the work finished, one displayed a corrupted answer. **Aforge's own final integrated
run is pending**, so this is not yet a paired comparison, and one repetition with differing
context sizes, caching and provider variability cannot establish a frontier in any direction.
The earlier peers-01 run is superseded and must not be counted.

Any future comparison must name the tested door, workload, model, quality checks,
repetitions, latency and actual cost. Unmeasured cells are not successes.

## Design limits

- **Accepted is not read, and settled is not exactly-once.** Durable acknowledgement bounds
  re-telling, not re-doing. A session with no journal, or a journal line that never reached
  disk, is told again on resume. **No mechanism here makes an external effect idempotent.**
- **Admission context is deliberately partial.** It is not a durable universal constraint
  ledger and does not guarantee that every relevant earlier statement is selected. A reference
  is only useful if the recipient can open it.
- **Steering is per-room.** A person steering *inside a task room* works: the line is recorded
  on that task as a person-origin direction and moves what the work is judged by. A correction
  typed in the **main chat and addressed at a task is not implemented** — nothing decides which
  running tasks a main-chat line concerns, and nothing broadcasts it. This is the candid UX gap.
- **Attempt identity and assignment revision answer different questions** and must stay
  distinct.
- **Cross-session communication is out of scope.** The addressing and origin shape should
  permit a later authenticated router, but there is no discovery service, no message bus and
  no cross-session permission model here. See `MODULES.md` §5 for what such a step would still
  have to add.
- **The seams are files, not packages.** `internal/session` has not been split.
