# aforge-as-worker trial

Can a Claude session hand its lane work to the aforge CLI (`aforge do`, default crew,
plain prompt) instead of an Opus subagent or codex? Three queued issues, handed off
verbatim, judged as a reviewer would judge them. Nothing merges: the WIP freeze is on
and every PR opens as a draft.

Binary: `~/af-trial/bin/aforge`, built from `origin/dev` at `b95f8c29f`.
Crew and profile: the owner's own `~/.aforge/config.json` — work `deepseek/deepseek-v4-pro`,
plan `moonshotai/kimi-k3:high`. One scratch worktree per item, cut from `origin/dev`.

## The table

| item | complexity | crew | quality | cost | wall | calls / rounds | exit | hand-work still needed | DX friction | Opus lane est. |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| #510 | **M** — 4 files across two seams (`tools.go` result type, `linear.go` round loop, `executor.go` reason table, `docs/HEADLESS.md`), a scripted-brain test that must drive a real timeout, and a house convention (`outcome.Meter`) not discoverable from the issue | work `deepseek-v4-pro` · plan `kimi-k3:high` | **not mergeable** after 2 steering prompts — one blocking gap left | **$4.5297** (runs: 1.9783 / 1.3738 / 1.1776) | **135m** — 3 × 45m00s, every run to the wall | **291 calls** (104 / 105 / 82), leaf 73 / 58 / 59; 5.60M prompt, 248k completion; *window* attribution | run 1 not captured · run 2 **2** · run 3 **2** | set `outcome.Meter` so the command and count reach the gate and the stream; swap `landing = 1` for `landingTurns`. ~10 min | 13 new | **≈$4.30** for a mergeable PR in one pass |
| #515 | — | — | — | — | — | — | — | — | — | — |
| #520 | — | — | — | — | — | — | — | — | — | — |

### How rounds and calls are counted

`aforge do --json` reports spend, seconds, nodes and the two model seats, but **no call
count and no round count**. The run's own store under `~/.aforge/runs/aforge-do-<n>/`
holds a graph scratch and a trace log, and **no call rows**. So every calls/rounds figure
in the table is reconstructed from `~/.aforge/logs/calls.jsonl` over the run's timestamp
window — a log shared with every other session on this box, whose `start` rows carry no
run id. Each row says which method it used; all of them, so far, say *window*.

### A figure I got wrong, corrected

An earlier reading of this trial said "roughly 12% of each run's spend is retry and hedge
waste". That conflated two things. Measured from `waste_usd` on the call rows, the wasted
spend is **8.4% / 14.0% / 5.2%** of the three #510 runs, **9.3%** over all three
($0.4194 of $4.5297). The 429s counted beside it are **not** part of that: a refused call
bills nothing, so the 8-9 upstream rate-limit responses per run cost **$0** and show up as
wall time, not money. #543 measures the same waste engine-side at 6.4% of spend and finds
that the settle line does not show it at all, which is why it is easy to miss from
outside.

### How the Opus-lane column is estimated

A comparable Claude Opus 5 subagent lane on this repository — one that reads the seam,
edits three to five files, writes a named test, runs the package tests two to four
times and writes the change entry — is priced from the token counts this harness
reports for such a lane, at the published Opus 5 rates: **$5.00 / MTok input,
$25.00 / MTok output**, with cache reads at a tenth of the input rate. The estimate
is stated per item with the token figures it was built from, so the owner can
disagree with the figures rather than with the arithmetic.

## The runs


---

## 2026-09-03 · #510 — three identical timeouts end the round with a named reason

Three hand-offs, 135 minutes of wall, $4.5297, 291 model calls. The verdict is
**not mergeable**, and it is one small commit short.

**Where it ended.** `go build ./...` passes, `go test ./internal/exec/` is green in 24.8s,
`bash scripts/laws.sh` is green across all 13 packages, and the named test
`TestIdenticalTimeoutsStopTheRound` passes. The diff is 5 files, 59 insertions:
`Result.TimedOut` as a typed fact in `tools.go` (not a string match — this is the right
seam), a per-round `stuckTally` in `linear.go` keyed on the call's name and arguments,
`StopStuckTimeout` and `MaxIdenticalTimeouts = 3` beside the other reasons in
`executor.go`, both stop-value lists in `docs/HEADLESS.md`, and a changelog entry with a
correct `invalidates` block.

**The one blocking defect.** The issue requires the reason to carry "the command and the
count, so the gate and the person-facing stream both say why" — the whole point being that
"the round never named what had happened". The command and count go to `trace.note` only,
which is the debug trace. Every other landing reason in this file sets `outcome.Meter`
(`StopDeadline` at :811, `StopBudget` at :1196, `StopNoProgress` at :1260, `StopTurnCap`
at :1272) precisely so that, in the file's own words, "the journal and the line a person
reads are composed from one fact rather than from two guesses". `StopStuckTimeout` is the
only one that does not. The gate and the stream get the bare word `stuck-timeout`.

**One review nit.** `landing = 1` is a bare literal where every other landing site in the
package uses `landingTurns`.

**Judged fairly on one point it got right and I expected it to get wrong:** it did *not*
touch `internal/manual/chat/`. The chat corpus does not enumerate executor stop reasons,
`stuck-timeout` reaches no person-facing string yet, and `docs/HEADLESS.md` — which does
enumerate them — was updated in both places. That is the correct call, and my prompt had
told it to edit the manual.

### Run 1 — first pass. $1.9783, 2700.05s, 104 calls, wall, nothing on the tree.

156 shell calls across two leaves, every one of them `grep` / `find` / `sed -n` / `cat`.
Zero `write`, zero `edit`, no heredoc, `tee`, `sed -i` or `patch` anywhere. The second leaf
reached `internal/exec/tools.go:1440-1470` at command 95 of 104, and then the wall came.
The gate refused with "The only file this run changed is CLAUDE.md" — **nothing was changed
at all**; the verdict was right and its evidence invented.

Prompt, verbatim:

```
Fix issue #510 in this repository (Agent-Field/aforge-v2): "leaf: three identical timeouts of one command end the round with a named reason, not the wall".

OBSERVABLE FAILURE
A headless run's leaf wrote a test that hangs, ran it, got "command timed out after 60s", diagnosed, ran the same command again, got the same timeout, and again, then kept working until the run's 900 s wall ended it. The work on the tree was already correct and its tests green at the wall; the run reported a wall, not a delivery. Three identical tool timeouts on one command cost 180 s of the wall and the round never named what had happened.

LAW
Three identical timeouts of the same command in one round are not three observations, they are one fact: this command does not finish here. The round ends on that fact with a named reason the person can read ("the same command timed out three times; stopped rather than run it again"), and the work on the tree is judged as it stands. A round may not spend the wall repeating a command it has already learned does not return.

FIX SHAPE
In the leaf harness, count identical timed-out tool calls (same command text, same timeout) within a round; at the third, end the round with a stop reason of its own kind (distinct from the wall and from a wire failure), carrying the command and the count, so the gate and the person-facing stream both say why. The threshold is one constant, interpolated into the reason text and the manual. No change to the tool's own timeout.

ACCEPTANCE
A scripted-brain test in the leaf harness: a tool that times out three times on the same command ends the round with the named reason and no fourth call.

LAWS IN FORCE FOR THIS CHANGE
- Fix it generally, at the seam where the fact is known. No band-aid, no special case at a call site, no string matching on an error message where a typed reason exists.
- Low complexity: the smallest change that makes the law true. Do not refactor beyond it.
- Add the named test the acceptance asks for. It must fail before the change and pass after.
- One source of truth for the threshold: one exported/package constant, interpolated into the reason text — never a literal 3 written twice.
- Write a change entry under docs/changes/unreleased/. Use `make changelog-new PR=510 KIND=fix SLUG=<slug>` if that target works; otherwise copy the shape of an existing file in that directory. It must say what was true before and what is true now.
- Do NOT edit internal/manual/chat/ or internal/manual/pages/ unless a person-facing string a person reads actually changes; if the new stop reason IS person-facing, then the manual page that describes how a round ends must gain it, in an existing "## " section rather than a new heading.
- Read CLAUDE.md at the repository root before you start; its rules bind you.
- Do not touch unrelated files. Do not run `git add -A`. Do not commit or push; leave the change in the working tree.
- When you are done, run `go build ./...` and the tests of every package you touched, and say in your deliverable which test names you added and the exact command that runs them.
```

### Run 2 — steering 1. $1.3738, 2700.05s, 105 calls, exit 2. A name and a red test, no mechanism.

`StopStuckTimeout`, `MaxIdenticalTimeouts = 3` and `TestIdenticalTimeoutsStopTheRound`
reached disk; nothing read either constant, and the test failed
(`stop = done, want stuck-timeout`). A tree that builds and reads as progress while the
feature is absent — worse for a reviewer than an empty one.

Prompt, verbatim:

```
Fix issue #510 in this repository (Agent-Field/aforge-v2): "leaf: three identical timeouts of one command end the round with a named reason, not the wall".

WHAT WENT WRONG LAST TIME — READ THIS FIRST
A previous run spent its whole 45-minute wall and 156 shell calls on nothing but `grep`, `find`, `sed -n` and `cat`, looking for the seam. It never called `write` or `edit` once and left the tree unchanged. Do not repeat that. The seam is named below, exactly. Go to those four files, read only the lines you need, and start writing within your first few turns. Your tools are `sh`, `job`, `write`, `edit`, `web`, `recall`, `capabilities` — there is no `read` tool; use `sh` with `sed -n`.

THE SEAM — these are verified file and line facts, do not go looking for them
- `internal/exec/executor.go:552` declares `type StopReason string`, and lines 555-596 declare the existing reasons: `StopDone`, `StopTurnCap`, `StopBudget`, `StopDeadline` ("ran out of wall clock"), `StopError` ("the provider failed"), `StopPromote`, `StopPaused`, `StopCancelled`, `StopEmpty`, `StopOverrun`, `StopSplit`. `StopNoProgress` is declared separately at `internal/exec/noprogress.go:70`. The new reason belongs with these as a typed value.
- `internal/exec/tools.go:1450`, in `func (r shellRun) result(seconds int) Result`, is where a timed-out command becomes what the model reads: `errorf("command timed out after %ds. Partial output:\n%s", seconds, r.body)` guarded by `if r.timedOut`. That is where the fact "this command timed out" is known.
- `internal/exec/linear.go` is the round loop. `landingStop := StopReason("")` is at line 739.
- `internal/exec/linear_test.go` is where the scripted-brain tests live; `internal/exec/noprogress.go` is the worked example of a stop reason that is decided by watching the turns go by — read it before you design yours, and follow its shape.

THE LAW
Three identical timeouts of the same command in one round are not three observations, they are one fact: this command does not finish here. The round ends on that fact with a named reason the person can read ("the same command timed out three times; stopped rather than run it again"), and the work on the tree is judged as it stands. A round may not spend the wall repeating a command it has already learned does not return.

WHAT TO BUILD
Count identical timed-out tool calls — same command text, same timeout — within a round. At the third, end the round with a new `StopReason` of its own kind, distinct from `StopDeadline` and from `StopError`, carrying the command and the count so the gate and the person-facing stream both say why. The threshold is one constant (for example `MaxIdenticalTimeouts = 3`) interpolated into the reason text with `fmt` — never a second literal `3` anywhere. Do not change the tool's own timeout.

ACCEPTANCE — the named test
In `internal/exec/linear_test.go`, a scripted-brain test: a tool that times out three times on the same command ends the round with the new reason and there is no fourth call. It must fail before your change and pass after.

LAWS IN FORCE
- General fix at the seam. No band-aid, no special case at a call site, no string-matching the error prose where the typed `timedOut` fact already exists.
- Low complexity: the smallest change that makes the law true. Do not refactor beyond it.
- One source of truth for the threshold.
- Write a change entry under `docs/changes/unreleased/` (`make changelog-new PR=510 KIND=fix SLUG=<slug>`, or copy the shape of an existing file there). It says what was true before and what is true now.
- The stop reason is person-facing, so add it to the manual section that describes how a round ends — in an EXISTING `## ` heading, never a new one, because a new heading breaks unrelated retrieval tests.
- Do not touch unrelated files. Never `git add -A`. Do not commit or push; leave the change in the working tree.
- Finish by running `go build ./...` and `go test ./internal/exec/`, and say which test names you added and the exact command that runs them.
```

### Run 3 — steering 2. $1.1776, 2700.05s, 82 calls, exit 2. The mechanism, less its naming.

Prompt, verbatim:

```
Finish issue #510 in this repository. The working tree ALREADY HOLDS a half-done attempt. Your job is to finish it, not to start again.

WHAT IS ALREADY ON THE TREE — verified by running it
`git diff` shows two modified files and nothing else:
- `internal/exec/executor.go` declares `StopStuckTimeout StopReason = "stuck-timeout"` and `MaxIdenticalTimeouts = 3`, with comments. Keep both.
- `internal/exec/linear_test.go` holds `TestIdenticalTimeoutsStopTheRound`, a scripted-brain test that drives three identical `sh` calls of `{"cmd":"sleep 2","t":1}` and asserts the round's stop is `stuck-timeout` with no fourth model call. Keep it.

`go build ./...` passes. `go test ./internal/exec/ -run TestIdenticalTimeoutsStopTheRound` FAILS:
    linear_test.go:604: stop = done, want stuck-timeout
`grep -rn 'StopStuckTimeout\|MaxIdenticalTimeouts' --include='*.go' .` finds them ONLY in their own declaration. Nothing reads them.

SO: THE BEHAVIOUR WAS NEVER IMPLEMENTED. There is a name and a red test and no mechanism. That is the whole of what is missing, and it is what you must add.

WHERE IT GOES — verified line facts in `internal/exec/linear.go`
- Line 723: `outcome := &Outcome{Stop: StopDone}`. Line 739: `landingStop := StopReason("")`.
- Line 1075: `for index, call := range calls {` — the loop that executes the turn's tool calls; line 1094 is `results[index] = tools.Execute(...)`.
- Line 1106: `for index, call := range calls { outcome.record(call, results[index].IsError) }` — the pass over the finished results.
- Line 801 sets `landingStop = StopDeadline`; line 1170 sets `landingStop = StopBudget`; line 1192 is `outcome.Stop = landingStop`. That is the established way a round ends on a named reason — follow it.
- `internal/exec/noprogress.go` is the worked example of a stop decided by watching turns go by. Read it and follow its shape.
- The timeout fact itself is made in `internal/exec/tools.go` around line 1450, inside `func (r shellRun) result(seconds int) Result`, guarded by `if r.timedOut`, producing `errorf("command timed out after %ds. Partial output:\n%s", seconds, r.body)`. Decide there or in the loop how the loop learns "this result was a timeout" — but carry it as a typed fact, NOT by matching that sentence as a string, unless the seam genuinely offers nothing else, in which case say so in a comment and use the one constant.

WHAT TO ADD
A per-round tally keyed on (command text, timeout). When the same key reaches `MaxIdenticalTimeouts` timed-out results, end the round with `StopStuckTimeout`, carrying the command and the count into the reason a person reads — "the same command timed out three times; stopped rather than run it again" — with the threshold interpolated from `MaxIdenticalTimeouts` via `fmt`, never written as a second literal `3`. Reset the tally per round. Do not change the tool's own timeout.

DONE MEANS ALL FIVE
1. `go test ./internal/exec/ -run TestIdenticalTimeoutsStopTheRound -count=1` PASSES.
2. `go build ./...` passes and `go test ./internal/exec/` is green.
3. A change entry exists under `docs/changes/unreleased/` — `make changelog-new PR=510 KIND=fix SLUG=<slug>`, or copy the shape of an existing file there. It says what was true before and what is true now.
4. The person-facing reason is added to the manual section on how a round ends, under an EXISTING `## ` heading — never a new heading, which breaks unrelated retrieval tests.
5. Nothing else on the tree is touched. Never `git add -A`. Do not commit or push.

Your tools are `sh`, `job`, `write`, `edit`, `web`, `recall`, `capabilities`. There is no `read` tool — use `sh` with `sed -n`. Do not spend turns re-discovering the seam: every file and line above is verified. Start editing in your first few turns.
```
