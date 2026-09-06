# Production readiness and task UX audit — September 6, 2026

**Verdict: not ready for a production release.** The task experience has working
paths and meaningful regression coverage, but it still violates basic recipient
and navigation expectations. Passing one substantial coding case does not close
these gaps. Quality and correct delivery precede cost optimization.

## Method and scope

Two independent read-only reviews covered task UX and execution reliability
(`task_ux_audit` and `runtime_quality_audit`). The main audit traced the current
implementation, ran the real Bubble Tea input decoder through the local engine
wire client, reproduced defects with disposable fixtures, and fixed two narrow
navigation defects. This is terminal-specific QA, not a website heuristic score.

Baseline source: `d9da6c48dc6c4b6d00d9b04657fe8a03c4cc1439`, in
`/private/tmp/af-runtime-next`. The interaction prototype added by that commit is
**simulated design, not the live terminal implementation**. Both this baseline
(CI run 33999223712) and the preceding runtime revision `23c98d9fc`
(run 33997640609) passed all remote CI jobs.

The source review covered task entry points, reader subscriptions, composer and
view state, task commands, local/remote direction delivery, reconnect, completion,
branch handling, cancellation, verification, and existing evaluation evidence.
It does not certify every feature in the repository or every operating system.

**Direct inspection of the user's iTerm window was blocked by computer-use safety
policy.** No terminal window was clicked, restarted, replaced or inspected through
another automation mechanism. Test frames came from isolated application fixtures,
not a screenshot of the user's window. Real-terminal accessibility and visual
appearance therefore remain unverified. No paid model calls were made in this audit.

## Why clicking may still fail in the running product

There are three distinct findings; they must not be collapsed into one guessed cause.

| Evidence | Finding | Required action |
| --- | --- | --- |
| The executable at the running `/private/tmp/af-conversation/bin/aforge` process path reports `3cb79f3f8 (dirty)`, built September 5 at 03:12. Its source still gates hosted task opening on `hosted()`. | It predates the fix allowing local engine task readers without a remote hostname. | Verify the actual failing window and put the tested client build in its launch path. Preserve the existing conversation and work. |
| The executable at the other running shared-checkout path reports `d4ac249c (dirty)`, built September 3. The audited binary reports `d9da6c48d`. | Multiple launch paths expose materially different products. A fixed worktree does not update an already running client. | Establish one documented launch/install path and show both client and engine revisions in diagnostics. Test upgrade/reconnect without losing work. |
| `tasksItem.pick()` deliberately returns false for another window's live task. The existing test requires Enter to do nothing. | Some visible task rows are intentionally not openable, even in current source. | Implement navigation to the owning conversation/task, or provide an explicit actionable explanation until that is supported. |

The audit shell also has no `aforge` on PATH. That is evidence about this shell,
not proof about the user's interactive shell or aliases. The shared checkout is
dirty with other work and was not rebuilt or modified.

## Fixes made during this audit

1. **The compact task shortcut stays available for unfinished work.** It previously
   disappeared when the last `TaskRunning` node became `TaskUnverified`, even with
   queued work or a waiting decision. Its visibility now uses the same groups as
   its displayed contents. At phone width the summary names **needs you** before
   running work. Existing wide-terminal behavior remains the task list.
2. **Switching task views releases the previous transcript subscription.** Opening
   a different task previously replaced the room without calling its release
   callback. All room constructors now close the previous view before creating
   the next. Navigation releases a reader; it does not cancel execution.

New regressions fail before these changes. The shortcut test clicks through to the
actual task transcript at 44, 80 and 99 columns, using the local engine wire reader;
the smallest layout goes through the Tasks page. The subscription regression
checks release exactly once. A broader gate also exposed a test-only notification
race, described in the validation appendix. These fixes do **not** implement per-task drafts,
scroll restoration, cross-window navigation or the whole prototype.

## Production work, in priority order

P1 items below are release requirements. P2 items are important UX/operational
work; a release decision must explicitly address their limits. “Confirmed” means
source or reproduction supports the statement. “Unproven” is an acceptance gap,
not an assertion that the behavior is broken.

| Priority | Workstream and current evidence | Definition of done |
| --- | --- | --- |
| **P1** | **One tested build reaches the user.** Confirmed old executable paths above; existing failing window not directly inspected. | A documented launch path opens the tested client, reports client/engine revisions, enters the existing task, sends a direction to the correct task, returns and reconnects with work intact. No destructive host restart or silent old-binary reuse. |
| **P1** | **Recipient-owned drafts and task messages. Confirmed misdelivery.** A disposable probe typed an unsent main draft, clicked task 7 and pressed Enter; the engine received that draft as task steering. Opening/closing rooms does not swap `a.input`. | Main, task A and task B independently preserve text, caret, compact pastes and attachments. Switching views never changes an unsent message's recipient. Submission clears only the accepted recipient's draft. Test Escape, direct task switches, project switches, reconnect and crash recovery. Treat a question as a question; do not silently reinterpret every task message as a scope revision. |
| **P1** | **Every visible task has an understandable opening action. Confirmed gap.** Other-window live rows are unpickable by design. Same-window rail/strip/Tasks entry points do work in tests. | Mouse and keyboard open the same stable task from the rail, Tasks, home/project views and another window. Identify the owning conversation and project. Show a recoverable error for a missing/unsupported owner; do not leave an apparently selectable row inert. |
| **P1** | **Non-blocking send and reliable acknowledgements. Confirmed source risks.** `steer()` synchronously calls the remote method from the UI path; the ordinary call deadline is ten seconds. `TaskSteerArgs` contains only ID/text, and direct `SteerTask` supplies no stable spoken-source ID. | Send runs asynchronously. Show pending, received and applied distinctly; Escape/navigation remain responsive. Retain the draft after a failed or uncertain send. Retry a lost acknowledgement with the same message ID and apply it once, including after reconnect and from another window. A second intentional identical message remains distinct. |
| **P1** | **A retained result remains usable when narration fails. Observed historical failure; integrated recovery unproven.** Artifacts and result references are retained, but a failed final model answer has previously left no substantive visible delivery. | Immediately expose the saved artifact, originating request and truthful failure state. One retry delivers the answer without rerunning completed work or duplicating the completion notice. Test provider failure during final narration, reconnect, and a newer unrelated main-chat question. |
| **P1** | **Respect the requested delivery and branch workflow. Observed failure; latest guidance unproven live.** The recovery trial merged into main without being asked. The new reminder helps, but conflict/stopped branch notes still contain unconditional merge advice. | For branch-and-commit requests, main and the user's checkout remain unchanged through worker completion and parent follow-up. Cover successful checks, inconclusive review, stopped work and conflicts. Remove conflicting advice. Report branch, commit, exact checks and unresolved work. Reading a result must never imply acceptance, merge or publication. |
| **P1** | **Current-request verification through the whole task tree. Component tests pass; integrated proof missing.** Explicit checks and revision invalidation exist; the automatic route-check fix has not completed another realistic live evaluation. | Revise one deliverable during child/parent checking. Affected work gets fresh checks and evidence; unaffected siblings retain their scope. Old evidence cannot satisfy the revised request. Cover partial handoff, failed checks, restore and final delivery. Never scrape arbitrary worker commands into rerunnable checks. |
| **P1** | **Stop the owned execution tree. Unproven end-to-end criterion.** Cancellation paths and component tests exist; this audit has not shown a full hosted-UI process-tree stop. | Stop a parent with child/grandchild tasks, a queued dependent, a foreground subprocess and a background process group. Owned processes stop within the documented grace, no new work starts, partial artifacts survive, and an unrelated task continues. Repeat while parked and during provider recovery. |
| **P1** | **Reconnection, persistence and crash recovery. Mixed evidence.** Live terminal detach succeeded previously, and host/socket tests pass. Hard host crashes and uncertain external effects are separate cases. | Distinguish terminal closure, network loss, graceful shutdown and hard crash. Restore the same task, pending question/direction and unseen result. Deliver each notification once and show interrupted/uncertain work honestly. Do not blindly replay an action whose external outcome is unknown. Cover two windows and idle-host retirement. |
| **P1** | **Independent quality gates on substantial work. Confirmed evidence limitation.** The one substantial repository case passes 220 checks yet exposes a public type-contract defect; the reference shares it. | Freeze multiple substantial repository issues and non-code tasks, with independent negative/interface/integration checks and requested-workflow grading. Repeat correction, interruption, long context and provider failure. Grade the delivered artifact, not whichever hidden child happens to look best. Retain failures and unknown costs. |
| **P2** | **Per-task reading state. Confirmed source gap.** `newRoom()` starts with fresh expansion maps and follows the live edge; closing drops the room. | Return to each task's prior reading anchor and expanded evidence. Preserve a deliberate follow-live choice. New output while away must not jump a reader to the bottom. Include long transcripts and changing widths. Subscription cleanup is fixed separately. |
| **P2** | **Keyboard, narrow-screen and accessibility qualification. Partial automated evidence.** The product has keyboard alternatives, semantic state words and color fallbacks; actual screen-reader behavior was not assessed. | Run the full task journey keyboard-only, on real dark/light terminals, ASCII/no-color, narrow/tall and short/wide windows. Check focus, clipping, selection, contrast and usable recovery text. Test actual assistive technology before claiming accessibility compliance. |
| **P2** | **Responsiveness under realistic load. Measurement missing.** Passing elapsed test times are not UI performance measurements. | Define and measure input-to-paint, task-open, send-ack and reconnect latency; profile CPU/memory during long streams, large pastes, many task rows and repeated open/close. Document limits. Keep expensive work and network waits out of the event loop. |
| **P2, after correctness** | **Simplify execution policy and then optimize cost. Proposed architecture only.** Workers and short forks already use the same Agent core; file-count handoffs and overlapping routing/readers remain. | Retain one owner, current assignment, tool receipts and lifecycle. Remove duplicate decisions incrementally while preserving the production acceptance battery. Keep ordinary tools direct; internal helpers stay collapsed. Compare cost only across successful, equivalent real work with complete pricing. |

Before release, also review the umbrella PR in understandable implementation
slices, run the required CI and platform checks, exercise installation/upgrade and
rollback, and keep the manual aligned with behavior. A draft PR or a local binary
is not a rollout.

## Intended UX after the fixes

- The main view is the conversation; the task list contains recognizable outcomes.
  Internal helper topology is optional detail.
- Clicking a task always gives either its conversation or a clear recovery action.
  The composer continuously names its recipient, and drafts belong to that recipient.
- Opening a task, returning, hiding a list and closing a client change the view,
  not execution ownership. Stop is a distinct action with a visible scope.
- Questions and results are actionable without taking keyboard focus or overwriting
  text. Pending sends remain visible until receipt; receipt and application differ.
- Progress, attention and delivered artifacts are separate facts. “Done” describes
  the requested outcome, including its delivery form and evidence.
- A completed task can be discussed and continued with retained history. The UI
  should not force the user to understand internal revival, checking or worker roles.

This is a completion of the approved conversation-with-tasks direction, not a new
visual redesign or another agent orchestration layer.

## Evidence and coverage

| Journey | Evidence in this audit | Limit |
| --- | --- | --- |
| Same-window task click, type, Enter, Escape | Real Bubble Tea raw SGR mouse/key decoding plus Loopback engine client; baseline three race repetitions passed in 13.959s | Synthetic task backend; no real iTerm interaction |
| Compact task door when attention is needed | New failing-before/passing-after click regressions at 44/80/99 columns | In-memory terminal render and wire reader |
| Phone attention priority | New failing-before/passing-after state test | No physical phone/terminal screenshot |
| Old subscription release | Reproduced missing callback; corrected constructor releases exactly once | Does not certify every stream resource under long-duration load |
| Draft recipient isolation | Desired-behavior probe failed: main text was sent to task 7 | Remains unfixed; temporary failing test was removed after preserving the evidence |
| Other-window task opening | Existing test deliberately asserts an inert row | Missing capability remains |
| Revision, protected branch, detach, reconnect | Independent targeted session/host/remote tests passed (1.424s/1.172s/2.223s) | Component evidence, not complete live-work proof |
| Code generation quality | Prior native trial: 159+61 checks; 823 public cases and 124 subtests; supplemental type-contract failure | Historical trial on an earlier runtime; one task is not a success rate |

The post-fix navigation group passes three race repetitions (11.622s). The broader
strip/phone group passes three race repetitions (30.284s). A full offline check of
terminal UI, session, engine host, remote transport, provider and manual is recorded
in the validation appendix after completion. No test skips or performance caps
were added.

Operator logs are under `/private/tmp/af-production-audit-20260906/`:
`navigation-baseline.log`, `confirmed-navigation-defects.log`,
`shortcut-before.log`, `shortcut-after.log`, `navigation-fixes-focused.log`, and
`full-qa.log`. Permanent shortcut/subscription regressions are in
`internal/tui3/tasknavigation_qa_test.go` and the existing local wire tests are in
`internal/tui3/localtaskroom_test.go`.

## Source map for implementation

- Task entry and view lifetime: `internal/tui3/room.go` (`newRoom`, `openRoom`,
  `openFarRoom`, `closeRoom`, `steer`), `roomorch.go`.
- Composer/durable draft ownership: `internal/tui3/app.go`, `input.go`, `draft.go`,
  `detach.go`, `pastechip.go`, `attach.go`, `roomrefresh.go`.
- Other-window navigation: `internal/tui3/tasksplace.go` (`tasksItem.pick`),
  `place_tasks.go`, `taskaway_test.go`.
- Compact navigation: `internal/tui3/taskstrip.go`, `taskphone.go`, `task.go`
  (`railGroupOf`). Use their existing status projection rather than another state list.
- Message identity and acknowledgement: `internal/remote/wire_task.go`,
  `client.go` (`SteerTask`, `callWithin`), `internal/session/task_room.go`,
  `assignment.go`.
- Result and branch delivery: `internal/session/task_run.go` (`taskNote`,
  `taskMergeNote`, `taskBranchFollowup`), `task_result.go`, `task_result_e2e_test.go`.
- Verification/cancellation/recovery: `internal/session/route_judge.go`,
  `task_checks_contract_test.go`, `cancel.go`, `task_store.go`,
  `internal/enginehost/persistent_test.go`, `internal/remote/tasklane.go`.
- Historical evidence: `QUALITY-20260905.md`, `IMPLEMENTATION.md`, `VALIDATION.md`.

## Reproducing the remaining draft defect

On this branch, put the following disposable test in
`internal/tui3/qa_recipient_probe_test.go` and run
`go test ./internal/tui3 -run '^TestQARecipientIsolation$' -count=1` without a model
key. It expresses the required behavior and currently fails. Remove the disposable
file after the probe; it is not a new skipped test or a passing assertion that
enshrines the bug. The complete fix should retain and extend it to cover all
recipient-owned editor state.

```go
package tui3

import "testing"

func TestQARecipientIsolation(t *testing.T) {
    a, engine := localTaskRoomLab(t)
    a.input.setText("Unsent discussion for the main conversation")
    clickRail(t, a, 0)
    drive(t, a, key("enter"))
    engine.mu.Lock()
    defer engine.mu.Unlock()
    if len(engine.steered) != 0 {
        t.Fatalf("main draft was sent to task %v: %q", engine.ids, engine.steered)
    }
}
```

## Validation appendix

The first full offline check passed the terminal suite in **469.535s**, engine
host in 15.412s, remote transport in 19.608s, provider in 53.609s and manual in
2.580s. Session failed in 190.561s for two recorded reasons:

- `TestSwappingTheModelTellsTheBeat` competed with the real background beat for
  its notification queue. With all audit changes stashed, ten ordinary repetitions
  passed, but 100 race-enabled baseline repetitions reproduced the failure.
  `SetModel` notifies synchronously; the test now checks the locked receipt log
  rather than removing a message intended for the beat. Both routing-on and
  routing-off tests pass 100 race-enabled repetitions in **3.197s**. No production
  routing behavior, deadline or skip was changed.
- The session checkout guard detected the newly created audit document during
  execution. That file was the audit's own edit, not an engine Git mutation. The
  subsequent session check freezes the whole checkout, including documentation.

The failed combined check remains failed evidence. Terminal source was unchanged
after its full passing run. The follow-up
`make check PKGS='./internal/session' TEST_FLAGS='-count=1'` passed: full session
suite **187.739s**, whole-tree vet and formatting, packed manual **1.585s**, canonical
build, and size **50,569,250 / 54,600,000 bytes**. Its log is
`session-build-qa.log`. The notification diagnostics are
`model-beat-baseline.log`, `model-beat-baseline-race.log`, and
`model-beat-fixed-race.log`. `make changelog-check PR=653` passed (256 well-formed
entries); `git diff --check` passed. No new live-model evaluation or actual-user
terminal qualification is implied by these checks.
