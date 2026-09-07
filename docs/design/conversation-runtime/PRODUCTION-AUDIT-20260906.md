# Production readiness and task UX audit — September 6, 2026

**Verdict: not ready for a production release.** The task experience has working
paths and meaningful regression coverage. Recipient isolation and owner-aware
navigation, durable sending and chat-sidebar fixes are integrated. The combined
offline gate is recorded in the final integration section below. Passing one substantial coding case does not close
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
appearance therefore remain unverified. No new live Aforge developer-task evaluation was run
in this audit; the authorized Claude CLI implementation and review work is separate.

## Initial click findings — historical launch observations

These observations were made at the start of the audit. The third source defect
has since been fixed; the first two launch paths have not been replaced or rechecked
in the user's window. They must not be collapsed into one guessed cause.

| Evidence | Finding | Required action |
| --- | --- | --- |
| The executable at the running `/private/tmp/af-conversation/bin/aforge` process path reports `3cb79f3f8 (dirty)`, built September 5 at 03:12. Its source still gates hosted task opening on `hosted()`. | It predates the fix allowing local engine task readers without a remote hostname. | Verify the actual failing window and put the tested client build in its launch path. Preserve the existing conversation and work. |
| The executable at the other running shared-checkout path reports `d4ac249c (dirty)`, built September 3. The audited binary reports `d9da6c48d`. | Multiple launch paths expose materially different products. A fixed worktree does not update an already running client. | Establish one documented launch/install path and show both client and engine revisions in diagnostics. Test upgrade/reconnect without losing work. |
| `tasksItem.pick()` deliberately returns false for another window's live task. The existing test requires Enter to do nothing. | At the initial audit revision, visible other-conversation rows were intentionally inert. Local owner navigation is now implemented. | Qualify the implemented local owner view in the actual launch path. Remote other-conversation navigation remains unsupported and needs an actionable path. |

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
| **P1** | **Recipient-owned drafts and task messages. Draft and send ownership integrated.** Main, task and run composers now retain their own text, caret, paste blocks and attachment tray, including structured crash recovery. The original misdelivery regression is covered by permanent tests. Guest recipient separation, queued delivery, stale-save refusal and uncertainty recovery have focused race coverage; see the final integration gate below. | Main, task A and task B independently preserve text, caret, compact pastes and attachments. Switching views never changes an unsent message's recipient. Submission clears only the accepted recipient's draft. Test Escape, direct task switches, project switches, reconnect and crash recovery. Treat a question as a question; do not silently reinterpret every task message as a scope revision. |
| **P1** | **Every visible task has an understandable opening action. Partial integration.** Local other-conversation views now join their owner without starting work or taking the keyboard. Guest identity, status and draft isolation pass focused tests. The right-hand chat column now opens visible rows, bounds hit targets to drawn content, orders attention/live work first and preserves the open page on repeated selection. A rendered-frame/raw-SGR test opens exact parent/child engine transcripts at 100, 119, 120 and 160 columns. Remote other-conversation navigation and actual-user-window validation remain gaps. | Mouse and keyboard open the same stable task from the rail, Tasks, home/project views and another window. Identify the owning conversation and project. Show a recoverable error for a missing/unsupported owner; do not leave an apparently selectable row inert. |
| **P1** | **Non-blocking send and reliable acknowledgements. Backend and UI integrated.** Stable message scope/sequence/time and expected conversation reach durable engine admission before acknowledgement. Owner mismatch and duplicate identity tests pass. The asynchronous composer saves the same outbox snapshot before wire delivery; retries retain identity after uncertain receipts. Some draft-clearing, quit and takeover writes remain synchronous, so arbitrary disk stalls are not fully qualified. | Send runs asynchronously. Show pending, received and applied distinctly; Escape/navigation remain responsive. Retain the draft after a failed or uncertain send. Retry a lost acknowledgement with the same message ID and apply it once, including after reconnect and from another window. A second intentional identical message remains distinct. |
| **P1** | **A retained result remains usable when narration fails. Observed historical failure; integrated recovery unproven.** Artifacts and result references are retained, but a failed final model answer has previously left no substantive visible delivery. | Immediately expose the saved artifact, originating request and truthful failure state. One retry delivers the answer without rerunning completed work or duplicating the completion notice. Test provider failure during final narration, reconnect, and a newer unrelated main-chat question. |
| **P1** | **Respect the requested delivery and branch workflow. Observed failure; latest guidance unproven live.** The recovery trial merged into main without being asked. The reminder and conflict/stopped branch notices now preserve the requested workflow; the corrected guidance still needs a fresh live evaluation. | For branch-and-commit requests, main and the user's checkout remain unchanged through worker completion and parent follow-up. Cover successful checks, inconclusive review, stopped work and conflicts. Remove conflicting advice. Report branch, commit, exact checks and unresolved work. Reading a result must never imply acceptance, merge or publication. |
| **P1** | **Current-request verification through the whole task tree. Component tests pass; integrated proof missing.** Explicit checks and revision invalidation exist; the automatic route-check fix has not completed another realistic live evaluation. | Revise one deliverable during child/parent checking. Affected work gets fresh checks and evidence; unaffected siblings retain their scope. Old evidence cannot satisfy the revised request. Cover partial handoff, failed checks, restore and final delivery. Never scrape arbitrary worker commands into rerunnable checks. |
| **P1** | **Stop the owned execution tree. Unproven end-to-end criterion.** Cancellation paths and component tests exist; this audit has not shown a full hosted-UI process-tree stop. | Stop a parent with child/grandchild tasks, a queued dependent, a foreground subprocess and a background process group. Owned processes stop within the documented grace, no new work starts, partial artifacts survive, and an unrelated task continues. Repeat while parked and during provider recovery. |
| **P1** | **Reconnection, persistence and crash recovery. Mixed evidence.** Live terminal detach succeeded previously, and host/socket tests pass. Hard host crashes and uncertain external effects are separate cases. | Distinguish terminal closure, network loss, graceful shutdown and hard crash. Restore the same task, pending question/direction and unseen result. Deliver each notification once and show interrupted/uncertain work honestly. Do not blindly replay an action whose external outcome is unknown. Cover two windows and idle-host retirement. |
| **P1** | **Independent quality gates on substantial work. Confirmed evidence limitation.** The one substantial repository case passes 220 checks yet exposes a public type-contract defect; the reference shares it. | Freeze multiple substantial repository issues and non-code tasks, with independent negative/interface/integration checks and requested-workflow grading. Repeat correction, interruption, long context and provider failure. Grade the delivered artifact, not whichever hidden child happens to look best. Retain failures and unknown costs. |
| **P2** | **Per-task reading state. Partial improvement; source gap remains.** Repeated sidebar selection preserves the existing page and draft. Switching away and reopening still creates fresh expansion maps and follows the live edge; closing drops the room. | Return to each task's prior reading anchor and expanded evidence. Preserve a deliberate follow-live choice. New output while away must not jump a reader to the bottom. Include long transcripts and changing widths. Subscription cleanup is fixed separately. |
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
| Draft recipient isolation | Original desired-behavior probe failed; permanent recipient/durable-draft regressions now pass in the full UI suite at `810f6e4f8` | Final guest/send integration evidence appears below; real hard-crash qualification remains |
| Other-window task opening | Local owner Join/Watch and identity regressions now cover opening, replacement and refusal | Remote other-conversation navigation and actual-user-terminal qualification remain |
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

## Reproducing recipient and crash recovery checks

The original disposable probe exposed a main draft being sent into task 7.
The implementation at `810f6e4f8` closes that defect; it is no longer an expected
failure. Permanent tests in `internal/tui3/recipientdraft_test.go`,
`draftkeep_test.go` and `draftcrash_test.go` cover recipient switching, caret,
paste and attachment state, write failures, ordered writes and stale legacy
exports. Run them with no model key:

```sh
env -u OPENROUTER_API_KEY go test -race ./internal/tui3 \
  -run 'Test.*Draft|Test.*Recipient|Test.*Composer|Test.*Paste|Test.*Reunion|Test.*Recover|Test.*Save' \
  -count=1 -timeout15m
```

Those draft-only tests predate send integration. The final integration below adds
persist-before-send and guest-recipient coverage; they remain separate assertions.

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


## Follow-up quality and UX wave — September 6

The completed integration revision `1e0b4fd4b` adds three reviewed changes:

- A task page's footer reports that task's state, including waiting and stopped
  states, instead of the main conversation's state and clock. Copy feedback keeps
  priority. Leaving the task restores main-chat status. Focused race tests cover
  44, 80 and 120 columns and disagreeing task/main states.
- Retained-branch notices for stopped, conflicted, moved and detached work offer
  inspection without directing an unrequested merge or checkout change. The full
  completion note names the retained branch and preserves the requested workflow.
  This removes conflicting guidance; a fresh substantial live workflow evaluation
  is still required before closing that production requirement.
- Streaming benchmarks now begin a new turn after the fixture's settled history.
  The old promoted-stream benchmark indexed `live == -1` and panicked; ordinary
  streaming and append benchmarks also incorrectly fed a settled turn. The failed
  initial run is retained as `ui-bench-before.log`.

The full offline `make check` gate passed: **tui3 476.519s**, **session 173.516s**,
engine host 14.132s, remote 18.737s, provider 55.105s, manual 3.264s, whole-tree
vet, packed manual 1.458s, canonical build and binary size **50,569,250 /
54,600,000 bytes**. The invocation retained the repository's existing lock-law
skip; this wave added no skips and changed no performance caps. Log:
`/private/tmp/af-opus-production-20260906/root-quality-gate.log`.

### Rendering measurements

Three samples per case, Apple M3 Max / darwin-arm64, 250ms benchmark duration;
figures below are medians. Fixtures contain prior user/assistant turns, tool
outputs and, for the live-tool case, an edit diff. They are application rendering
measurements, **not terminal input-to-paint, network acknowledgement latency,
model speed, or a substantial developer-task success rate**.

| Application operation | Median |
| --- | ---: |
| Full redraw with 20 settled turns | 0.195 ms |
| Full redraw with 60 settled turns | 0.643 ms |
| Streaming reply above 20 settled turns | 0.237 ms |
| Promoted Markdown stream above 20 settled turns | 0.214 ms |
| Live edit above 20 settled turns | 0.254 ms |

A repeated pointer move over the same row remains about 35ns with either a one-line
or 4,000-line draft; this is the cached no-change path, not click latency. A
single diagnostic append run of 4,000 deltas allocated about 50.6MB; no optimization
or general latency claim follows from that one sample. Logs: `ui-bench-fixed.log`
and `append-bench.log` in the same operations directory.

### Historical parallel implementation checkpoint — before integration

At this checkpoint, three user-requested **Claude CLI Opus 5** lanes are editing
isolated worktrees for recipient drafts, asynchronous reliable sending, and
cross-window navigation plus UI organization. Their patches are **not part of
`1e0b4fd4b` and have not passed the combined gate**. Written tests are not treated
as passing tests. The first sending compilation failed and was returned for
correction. The first cross-window navigation patch passes focused race tests in
UI/remote/host/command packages, but older-engine negotiation and binding later
reads to their original conversation still need review.

The populated Tasks fixture confirms excessive identity/status marker variety,
internal type labels competing with progress, lengthy header prose, and failed
work grouped under `done today`. Needs-attention and running sections already
precede history on that page; the organization pass must preserve that useful
order and check the rail, home, mixed families, focus and narrow layouts. One
subdued identity marker is being implemented. No updated actual-user terminal
or release is implied: the existing iTerm inspection restriction and old-running-
binary findings above still apply. No new live Aforge developer-task evaluation
was run in this follow-up; the authorized Claude CLI work is separate.

## Recipient draft integration checkpoint — `810f6e4f8`

The Claude CLI Opus draft implementation was reviewed, corrected and integrated.
Each recipient owns text, cursor, compact paste blocks, attachment paths and a
slot for unresolved sends. Structured JSON is authoritative; the plain text file
is a legacy export. An empty record prevents cleared or submitted text from
returning after a crash between those writes. A newer failed save retires older
queued writes. Unknown records are preserved, missing paste blocks block sending,
and write failures are shown. No new skips or storage caps were introduced.

The focused race suite passed in **46.655s**, including the coordinator's two
previously failing stale-export crash cases. The integration's full offline
`make check PKGS='./internal/tui3 ./internal/manual' TEST_FLAGS='-count=1'` passed:
**UI 497.447s**, manual 2.015s, whole-tree vet, packed manual 1.568s, canonical
build and **50,653,298 / 54,600,000 bytes**. Logs: `drafts-focused-third.log` and
`drafts-integration-gate.log` under the Opus operations directory above.

The broader navigation review deliberately retained five failures in
`navigation-focused-third.log`: missing guest-loss explanation, task-row cost
priority, duplicate live/history work, and two observer/protocol tests. Sending
review also found a premature recall entry and an acknowledgement that preceded
durable message identity. These are being corrected in separate worktrees; their
changes are not in this checkpoint. No live-model quality or actual-terminal
qualification follows from the draft gate.

## Integrated navigation checkpoint — `5460283a0`

The local task-owner callback, protocol-12 Join/Watch enforcement, bound status
subscription and organized Tasks list are integrated. The canonical build passes.
Focused combined ownership tests passed with race detection: UI 17.621s, remote
2.951s and manual 3.987s. All repository structural laws pass after registering
the new waiting command in the test harness. Final manual retrieval and untagged
end-to-end wording checks also passed in the navigation source worktree.

This does not close the user's chat-sidebar complaint. The user specifically
identified the right-hand task column inside chat; an additional Opus pass is
checking full-row clicks, explicit folding, mixed families and overflow there.
The full combined gate will run after reliable-send and sidebar integration.

CLI help, command dispatch and the fetched dev source did not reveal a built-in
screenshot command. The repository's `scripts/frame.sh` is a fixture capture
script, not a confirmed native CLI capture command. The user was asked for the
command they recalled. Interim images are labelled application-render fixtures;
none show or operate the user's terminal.


## Final integrated sidebar and delivery wave — `48201dce2`

The user-requested Claude CLI Opus lanes are integrated, including root review
corrections. The right-hand chat task column is now the explicitly tested surface:
visible rows open their task; a fold only acts where its disclosure is drawn;
the inactive resize seam at 100–119 columns no longer swallows a click; and the
column stops capturing clicks at the composer/footer boundary. Attention and
running work precede settled work within the existing hierarchy. Redundant `◆`
markers are removed from this task-only column, with titles and detail lines aligned.
Repeated sidebar clicks and Enter retain the same room, draft and reading position.
A guest task with the same number cannot select or toggle the local task.

`railwire_test.go` derives its mouse coordinates from the actual rendered sidebar,
feeds raw SGR bytes through Bubble Tea, and requires the exact parent then child
identifier and local engine transcript at 100/119/120/160 columns. It failed before
the sidebar patch. The first integrated run then exposed a test-coordinate bug:
the child label also appeared in the transcript header, so the helper now restricts
its independent search to the drawn sidebar columns. The corrected exact-task,
repeat-selection and colliding-owner race tests pass in **3.496s**. Both failed
runs are retained (`rendered-rail-before.log`, `chat-rail-root-focused.log`), alongside
`chat-rail-root-recovery.log`; no assertion was weakened to “any task opened”.

The composer outbox now writes the actual pending message snapshot before sending.
Per-record ordered writes keep queued identities tied to the record that landed;
a newer empty or superseding record cannot authorize an older transmission.
Unanswered delivery preserves identity, while definitely refused text remains a
recoverable draft. Owner/session replacement is checked at durable admission.
A joined reader is bound before attachment, cannot take the keyboard, and cannot
use another conversation's task number after replacement. Pipe joins refuse
before engine boot. A finished guest page continues to detect owner replacement,
then keeps the last known state and ignores late reads.

These are guarantees about direction admission, not exactly-once tool effects.
Some lifecycle draft writes still run synchronously. General per-task scroll
restoration, hard-crash/external-effect recovery and a fresh substantial developer
campaign remain in the release list above.

The first combined check remains **failed** evidence: UI **543.040s** failed on
three cases. An unnamed-window test accidentally inherited the newly named fixture's
transcript; it now explicitly reopens an unnamed window. The tree-keyboard test
expected a finished child ahead of a running child; it now asserts the first
visible running child. The third was a product defect: refused owner attachment
showed only a status line instead of opening the promised fallback card. Refused
and wrong-owner attachments now open that read-only card with the owner details
and reason. The full session **172.355s**, remote **21.025s**, host **16.351s**,
CLI **49.594s**, manual **3.621s** and untagged e2e **2.294s** suites passed in
that same run. Log: `final-combined-gate.log`.

The final read-only Claude Opus review found two additional colliding-owner cases:
local design progress/completion could alter a guest page and forward navigation
could stay on a guest when the local task had the same number. Both were reproduced
in `guest-collision-before.log` (**0.944s**, failed), then fixed with owner guards.
The weaker raw-wire click test now requires task 1 rather than any opened task.
Combined focused recovery passes with race detection in **4.951s**; full manual
**1.740s** and untagged e2e **0.526s** pass, including the new double-click help
probe. The stale manual sentence promising click-to-close was corrected as well.
Logs: `final-review-recovery.log`, `final-manual-recovery.log`. The earlier focused
manual command matched no tests and is not counted as manual coverage.

The final `make check PKGS='./internal/tui3 ./internal/manual ./internal/e2e'
TEST_FLAGS='-count=1'` passes on `48201dce2`: full UI **543.344s**, manual
**1.507s**, untagged e2e **0.610s**, whole-tree vet/formatting, packed manual
**1.487s**, canonical build and binary size **50,821,794 / 54,600,000 bytes**.
The backend packages above were unchanged by the UI recovery fixes and were not
needlessly rerun. The checkout, including documentation, remained frozen during
each guarded check. Log: `final-ui-recheck.log`. No test skips or performance caps
were added; the repository's existing lock-law skip remains unchanged.

Final chat and open-task render fixtures are under
`/private/tmp/af-opus-production-20260906/chat-rail-final/` at 100, 120 and 160
columns, with ANSI originals and PNG views. Their labels explicitly identify
application fixtures, not the user's terminal. The image renderer's font does not
cover every spinner glyph; those boxes are not proof of a product font defect.
No built-in native CLI screenshot command has been confirmed. The repository
capture helper is `scripts/frame.sh`, whose private-terminal approach is distinct
from inspecting the user's blocked iTerm window.


## Real-terminal state audit and multi-module trial, follow-up

The tested baseline `e4a614d15` was launched in a private terminal created for this
work (`tmux -L af-state-ux-20260906`), with isolated Aforge state. This did not
inspect or control the user's existing iTerm window. Captures use the same native
terminal-pane mechanism as `scripts/frame.sh`, preserving both ANSI and plain text;
PNG renderings are derived from those captures. Seeded-demo frames are labelled
separately from the paid live run. Evidence root: `/private/tmp/af-state-ux-20260906`.

The live dutylog run exercised a four-module CSV-to-weekly-report repair through
real chat, durable-task admission, sidebar mouse entry, task transcripts, audits,
repair and parent integration. Captures cover 160, 120, 80 and 60 columns. This is
a moderate integration/UX trial, not a new DeepSWE comparison or a quality ranking.

Independent outcome: all **29 judge cases pass**, and all **7 protected files are
unchanged**. The requested `fix/dutylog-pipeline` branch exists and `main` remains at
the input commit `452d8199ae0c47eec55233a475ef755ed010abcd`. **The workflow failed:**
HEAD remains at that input commit, with the repairs staged and no requested final
commit. The first task's audit rejected a non-importable regression suite. The
parent's cherry-pick was then treated as broad editing and automatically handed to
a second task, whose new worktree did not match the integration brief. That task
ended with a canceled-stream error. The final screen was idle with two task reports.
This result blocks a production-readiness claim despite passing repair behavior.

The guard recorded 125 admitted requests and 124 settled receipts: 97 completed, 9 upstream errors, 4 upstream
failures, 1 lost connection and 13 client disconnects. Known returned cost totals
$0.198172850036; **26 settled requests have unknown cost and one admitted request has no settled receipt**, so this is a lower bound rather
than a paid total. The displayed footer cost was lower and is not the billing source.
Some fixture/generated bytecode caused cleanup churn; do not generalize that latency
into a runtime or model performance score.

This follow-up fixes reproduced state/navigation defects:

- A timed task proposal said `waiting · your call` while it automatically approved.
  Its footer now says `starting task`; required-input presence uses the real deadline.
  Holding the clock asks for input, and a separate required question still surfaces.
- A foreign finished task could expose and answer a same-numbered local task's
  settlement card. The shared room decision lookup rejects guest ownership.
- A conversation-level question reclassified unrelated workers as needing a person.
  The task list now uses each task's own reading, preserving complete live facts
  rather than losing design approvals through a historical index row.
- A retained branch alone demanded an unrequested merge. It remains inspectable;
  conflicts and unresolved human reviews retain their attention state.
- Successful model review handoffs and automatic settlement now say `awaiting review`.
  Failed handoffs keep the human choices. A working parent alone never hides a child
  decision; guest ids cannot borrow this local handoff state.
- Adaptive fuel gates now publish a root-only pause with live replay and clear it on
  answer/settlement. Child workers retain their states. Pause and answer publication
  are serialized, and a late root progress update cannot reopen a settled run.

Release work remaining from the live trial: prevent final integration from triggering
another durable task; verify successful commit delivery end to end; diagnose the
provider retry/disconnect behavior and its accounting gaps; make first-run setup
respect `--one-model`; verify status and task clocks during audit/repair; broaden the
real-work matrix to a substantial external issue, interruption and reconnect. Existing
14-workstream priorities above remain open unless explicitly discharged by evidence.

Focused tests passed during implementation; the full final gate and rebuilt-terminal
confirmation are recorded in the next validation addendum when complete.

The first full follow-up command included a nonexistent `internal/host` package;
that setup error is preserved in `full-check.log`. The real `internal/enginehost`
package passed separately (14.108s). The named session (368.896s), remote (21.498s),
orchestrate (2.065s), manual (9.601s), e2e (3.947s) and CLI (98.844s) packages passed.
UI failed seven assertions that assumed retained branches stayed under attention;
the assertions now use an actual conflict for urgency and expand retained reports
before checking their details. All eight focused recovery cases pass (1.490s).
The final UI/manual/e2e gate below is a fresh run after those corrections.

Final UI/manual/e2e `make check` passed after recovery, including whole-tree vet,
format, packed-manual verification, canonical build and size budget. Targeted race
checks passed (session 7.067s; UI 3.243s). A final copy correction makes the synthesized
empty-report fallback follow automatic review too; the automatic-review regression
asserts both its displayed wording and machine-owned state. The targeted final check
and structural-law results are recorded with the final commit.

Final results: UI 597.946s, manual 2.058s, untagged e2e 0.864s; packed manual 1.605s.
The final fallback-copy regression passes (UI 1.148s; manual 1.266s), and structural
laws pass in all 14 packages (UI 21.397s). Changelog validation passes 256 entries.
The canonical binary was rebuilt after the copy correction. Reopening the saved paid
run in that binary shows both retained task reports without a false `needs you` count.
A raw sidebar click opens the exact task journal; a second click on that row keeps it
open. Rebuilt replay captures cover 160/120/80/60 columns. These are saved-run views,
not a second successful model trial. During this confirmation the long, repeated task
composer label and raw Markdown visible in replay remained polish follow-ups; the
current wave does not claim every chat layout is finished.

## Conversation hierarchy follow-up — 2026-09-07

The owner clarified the information architecture: the main chat is the task a
person comes back to; work it delegates belongs beneath that chat, with deeper
subtasks beneath their actual parents. Home already has conversation rows, but
its work preview flattened children. The Tasks place started at worker roots and
split one conversation between status sections, losing that relationship.

The intended reading is conversation → task → subtask. Urgency orders whole
conversations while each child retains its own truthful state. Opening the
conversation must return to its main chat; opening a child must keep the existing
owner-safe task navigation. Filtering and folds must preserve enough ancestry to
explain a result. Native terminal frames and interaction regressions are required
before considering this UI follow-up complete.

This follow-up does not discharge the live trial's missing final commit, provider
accounting gaps, or the remaining production workstreams above.

The hierarchy is implemented using the existing conversation and task identities,
without another execution layer. Two actual Claude CLI Opus workers implemented
Home and Tasks independently; integration review corrected live parent loss,
main-chat navigation, truthful state refresh, duplicate-name click targets,
ancestor-preserving search and record return. Main chats with no delegated work
remain selectable. Home retains its three-name desktop preview; phone work bands
fold whole families. Each child keeps its own state while urgency orders the
conversation containing it.

Native terminal mouse checks opened the main chat, folded and expanded its work,
opened an exact child, returned through the main-chat row, and searched for a live
deep descendant with both ancestors visible at 60 columns. Home's right-hand
child row opened its exact record. These are seeded UI checks, not a new model
quality trial. Captures and failed reproductions are retained in
`/private/tmp/af-chat-tree-20260907`; PNGs are rendered from native pane cells.

Validation: the full UI/manual/untagged-e2e `make check` passed (UI 543.562s,
manual 2.166s, e2e 0.435s), including vet, format, packed manual, canonical build
and size budget. It followed corrections to legacy assertions that assumed a
flat first worker row or always-expanded live children; no skip was added.
A final native check reproduced the same main-chat return defect in Home.
The shared Home/Search/Tasks correction then passed its focused `make check`
(UI 2.083s), with a three-door regression asserting main and child draft retention.
That final adapter correction has focused coverage after the full run; it is not
represented as a second complete local UI-suite run. The final binary is
50,905,810 bytes against the unchanged 54,600,000-byte budget.

The 40-chat / 5,120-task history benchmark measured 22.48 ms and 93.55 MB per
reading on the baseline and 15.91 ms and 53.09 MB on the final implementation
(Apple M3 Max, five iterations, fixture outside measurement). Allocation count
increased from 5,817 to 18,266. This measures local history processing, not
input-to-paint latency, model speed, workflow completion or total cost. The
missing final commit and other release requirements above remain open.

Final structural laws passed across 14 packages (UI 21.676s), and changelog
validation passed all 256 entries. Native Home confirmation now returns from a
child view to the main conversation. The private seeded terminal was closed
when QA finished; the user's other terminals were not changed.
