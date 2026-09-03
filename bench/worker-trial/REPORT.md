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
| #515 | **M** — one package, 3 files, but the seam needs a typed rule identity that does not exist: the refusal reaches `repair` as a plain `error` and `why()` flattens it to a sentence, so every attempt landed on a string match | work `deepseek-v4-pro` · plan `kimi-k3:high` | **not mergeable** after 2 steering prompts — green, and band-aided at the seam | **$4.3903** (runs: 1.8774 / 0.1961 / 2.3168) | **110.9m** — 45m00s, **20m54s** (died on an internal context deadline), 45m00s | **220 calls** (92 / 26 / 102), leaf 57 / 14 / 67; 3.79M prompt, 353k completion; *window*, and all three windows match the JSON exactly | 2 · **1** · 2 | replace the prose substring match with a typed refusal at the caller's contract (~30-60 min, wider than the issue scoped); delete the comment citing `quoteDestination`, which does not exist; drop a stray `//` | 6 new | **≈$2.75** for a mergeable PR in one pass |
| #520 | **L** — two packages and a shared seam, four authorities that must agree, and the acceptance test lives in `internal/tui3`, whose suite is 458s | work `deepseek-v4-pro` · plan `kimi-k3:high` | **not mergeable** after 2 steering prompts — everything green, one requirement quietly unmet | **$4.8651** (runs: 1.8297 / 1.9946 / 1.0408) | **128.8m** — 45m00s, 38m47s, 45m00s | **288 calls** (96 / 98 / 94), leaf 52 / 71 / 68; 4.69M prompt, 200k completion; *window*, all three match the JSON exactly | 2 · 2 · 2 | sort the assembled items within each section by the `Work()` key so `mine` and `away` obey it too, and widen the test fixture so a `mine` entry sits among two or more file entries in one section. ~20-30 min | 5 new | **≈$5.10** for a mergeable PR in one pass |

### How rounds and calls are counted

`aforge do --json` reports spend, seconds, nodes and the two model seats, but **no call
count and no round count**. The run's own store under `~/.aforge/runs/aforge-do-<n>/`
holds a graph scratch and a trace log, and **no call rows**. So every calls/rounds figure
in the table is reconstructed from `~/.aforge/logs/calls.jsonl` over the run's timestamp
window — a log shared with every other session on this box, whose `start` rows carry no
run id. Each row says which method it used; all of them, so far, say *window*.

### The `-timeout` flag never reached the leaf — and I reported the wall wrongly because of it

**#549**, filed from this trial's #515 run, finds that a leaf's room is
`context.WithTimeout(900s)` from the subharness fifteen-minute floor and never reads the
errand's `-timeout`. So `-timeout 45m` bought **no leaf more than fifteen minutes**. The
"45m to the wall" runs in this table were the door cycling leaves, each with a fifteen-minute
room; the flag I set governed only how long the door kept starting new ones.

Measured against my own call log, per leaf, first call to last:

| run | leaf | span | what ended it |
| --- | --- | --- | --- |
| #510 run 1 | task-2 | 176.0s | finished |
| #510 run 1 | task-2-x1 | 1876.9s across a resume | the wall |
| #510 run 2 | task-2 / x1 / x2 | 610.9s / 603.1s / 154.7s | the wall |
| #510 run 3 | **task-2-n1** | **900.0s** | **the leaf's room** |
| #510 run 3 | task-2-n4 / n2 | 487.3s / 121.0s | the wall |
| #515 run 1 | task-2 / x1 / x2 | 758.7s / 838.3s / 177.5s | the wall |
| #515 run 2 | **task-2** | **900.0s** | **the leaf's room** |
| #515 run 3 | task-2 / x1 | 1179.2s across a resume / 888.6s | the wall |

No single leaf room ever exceeded 900.0s. The two spans above it belong to nodes the stream
shows being resumed, so they held more than one room.

**Five of the six runs ended on the wall. One ended on a leaf's room — #515 run 2, the run
that broke the build.** Its single leaf ran exactly 900.0s, reported `context deadline
exceeded`, and left `internal/shaped/shaped.go` with a call site whose function was never
written. That failure is not the wall I chose; it is a fifteen-minute room I could not see
and could not change from the command line.

This also revises something I claimed earlier in this file. I recorded "45m wall" as though
each hand-off had forty-five minutes of working room. It did not. The only lever I actually
had over a leaf's chances was the prompt — which is why "start editing in your first few
turns" moved the results and the timeout flag never could have.

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

---

## 2026-09-03 · #515 — a re-ask names the rule the answer broke

Three hand-offs, 110.9 minutes, $4.3903, 220 model calls. **Not mergeable**, and unlike
#510 it is not a small commit short — what is left is the part the issue was actually
about.

**Where it ended.** `go build ./...` passes, `go test ./internal/shaped/` is green in
0.005s, `bash scripts/laws.sh` is green across 13 packages, and `shaped_test.go` is
+182/−0, so no existing test was edited to make anything pass. The changelog carries a
real `invalidates`. Acceptance test 2 — the re-ask prompt names the rule the first answer
broke — is genuinely met: `reask(ask, joined, why(refused))`.

**The blocking defect: the seam is a substring match on prose.**

```go
func findQuotationField(answer string, note string) string {
	if note == "" { return "" }
	lower := strings.ToLower(note)
	if !strings.Contains(lower, "quote") && !strings.Contains(lower, "quotation") {
		return ""
	}
	// "quote" is the field name every caller in this codebase uses for the
	// quotation field, and the tag that quoteDestination carries.
	return "quote"
}
```

It decides *whether the failed rule is a quotation rule* by looking for the substring
"quote" in the refusal's English, and then returns the hardcoded literal anyway — so the
`fieldName` parameter threaded through `mergeQuote` has exactly one possible non-empty
value. The hardcoded field name was moved, not removed. This is what the issue and the
prompt both forbid in as many words.

**In fairness, the clean seam does not exist yet.** The refusal arrives at `repair` as a
plain `error`; `why()` (`shaped.go:601`) returns its first line as a string. There is no
typed rule identity to branch on, so a general fix means *creating* one at the caller's
contract — wider than the "low complexity" the issue asked for. The honest review outcome
is: the tool found the real constraint and then papered over it silently instead of
saying so.

**And `quoteDestination` does not exist.** `grep -rn quoteDestination` across the
repository returns exactly that comment. The justification for the hardcoded literal cites
a symbol that was invented.

### The recurring finding, now three times over

The code converges under steering. The **prose about the code does not.**

1. #510 run 1 — the gate refused with "The only file this run changed is CLAUDE.md" when
   no file was changed.
2. #515 run 1 — a comment reading "THE RULE IT NAMES IS THE CONSTANT, never a duplicated
   prose string" over code that interpolated `RepairReshaped`, a journal *kind*, so the
   model was told "The rule your answer broke: reshaped." The changelog restated the
   claim. Three artefacts agreeing with each other and not with the behaviour.
3. #515 run 3 — a comment citing `quoteDestination`, a symbol that does not exist.

Every one of the three is confident, plausible, in-house-style, and false. None is caught
by a build, a test or the laws, because none of them is code. A reviewer who reads the
comments to understand the diff is being actively misled, and that is a worse failure than
a red test.

### Run 1 — plain prompt. $1.8774, 2700.05s, 92 calls, exit 2.

Wrote a real implementation on the first pass, unlike #510: `internal/shaped/` +159/−3,
a `quotationRepair` road, `reask` carrying the note. **It also broke an existing test and
did not notice** — `TestTheJournalSaysWhyAnAnswerCouldNotBeRead` failed with two journal
entries where one is required, and `git diff` on that test file was +92/−0.

Prompt, verbatim:

```
Fix issue #515 in this repository (Agent-Field/aforge-v2): "shaped: a re-ask names the rule the answer broke and repairs a quotation without asking the whole verdict again".

OBSERVABLE FAILURE
At a delivery gate the judge wrote a 1647-token answer that did not quote the words of the request it was failing, so the shaped-output reader could not accept it ("parse response: a fail must quote the words of the request it is a failure of"), journaled "the answer was not readable — asked again", and re-asked. The first answer took 47 s; the re-ask answered in under a second. Those 47 s came immediately before a 900 s wall on a run that was converging.

LAW
A re-ask is bounded by what the first answer cost, and the rule the answer broke is told to the model in the re-ask, so the second answer is short and right. When the first answer is long and only the quoting rule failed, the reader repairs by asking for the quotation alone rather than the whole verdict again.

FIX SHAPE
internal/shaped, the re-ask road: carry the parse note into the second prompt as the one rule to satisfy; when the failed rule is a quotation rule, ask only for the missing quotation over the answer already held, and accept the merged result.

ACCEPTANCE — named tests
1. A first answer that fails only the quotation rule is repaired with one short call: the re-ask asks for the quotation alone over the answer already held, and the merged result is accepted.
2. The re-ask prompt names the rule the first answer broke.

LAWS IN FORCE FOR THIS CHANGE
- Fix it generally, at the seam where the parse note is known. No band-aid, no string matching on prose where a typed note or a named rule can carry the fact.
- Low complexity: the smallest change that makes the law true. Do not refactor beyond it.
- The named tests must fail before the change and pass after.
- One source of truth: no fact written in two places; interpolate from the constant.
- Write a change entry under docs/changes/unreleased/. Use `make changelog-new PR=515 KIND=fix SLUG=<slug>` if that target works; otherwise copy the shape of an existing file in that directory. It must say what was true before and what is true now.
- Do NOT edit internal/manual/chat/ or internal/manual/pages/ unless a string a person actually reads changes.
- Read CLAUDE.md at the repository root before you start; its rules bind you.
- Do not touch unrelated files. Do not run `git add -A`. Do not commit or push; leave the change in the working tree.
- When you are done, run `go build ./...` and `go test ./internal/shaped/`, and say in your deliverable which test names you added and the exact command that runs them.
```

### Run 2 — steering 1. $0.1961, **1254.24s**, 26 calls, exit 1. It broke the build.

Died at 1254s of a 2700s wall on `context deadline exceeded`, mid-edit:

```
$ go build ./...
internal/shaped/shaped.go:266:14: undefined: findQuotationField
```

It wrote the call site and died before writing the function. Run 1's tree compiled with
one failing test; this tree did not compile at all — **the tool made its own previous
output worse.** `linear.go`'s own comments document this hazard for the budget stop ("it
left files syntactically broken with the model's final, already-emitted repairs discarded
unexecuted"); here it arrived from an internal context deadline instead, at 46% of the
wall. The run reported `"settled": true` alongside exit 1.

It did correctly fix three of the four faults named: the double-journal regression, the
hardcoded `raw["quote"]`, and the over-broad gate.

Prompt, verbatim:

```
Finish issue #515 in this repository. The working tree ALREADY HOLDS your previous attempt. Fix what is wrong with it; do not start again and do not revert it. Four faults, all verified by running the tree.

FAULT 1 — YOU BROKE AN EXISTING TEST AND DID NOT NOTICE. This is the blocking one.
`go test ./internal/shaped/` FAILS:

    --- FAIL: TestTheJournalSaysWhyAnAnswerCouldNotBeRead (0.00s)
        shaped_test.go:373: a contract the caller refused: journaled [{Lane:gate Kind:reasked Round:1 Spent:40 Ceiling:8192 Note:parse response: a fail must quote one of the behaviours this request states, and "not a stated behaviour" is not one of them} {Lane:gate Kind:reasked Round:1 Spent:40 Ceiling:8192 Note:parse response: ...}]

Two journal entries where the test requires one. `git diff internal/shaped/shaped_test.go` is +92 lines and −0: that test is UNCHANGED and it passed before your change. The cause is in `repair` in `shaped.go`: your new quotation branch calls `note(ctx, Repair{... Kind: RepairReasked ...})` at line ~266, and when `mergeQuote` does not produce a decodable answer it FALLS THROUGH to the ordinary re-ask, which journals the same kind again at line ~288. One repair must leave one journal entry. Do not edit the test to accept two.

FAULT 2 — `mergeQuote` HARDCODES THE FIELD NAME. This is a band-aid, and the laws forbid it.
`raw["quote"] = json.RawMessage(quote)` assumes every shaped contract names its quotation field literally `quote`. The fix has to be general at the seam: the caller's contract is what knows which rule failed and which field carries the quotation, so take the field from there rather than writing a literal string into the merge.

FAULT 3 — THE BRANCH FIRES FOR THE WRONG ANSWERS.
Your gate is `if provider.DecodeJSONObject(joined, &struct{}{}) == nil` — true for ANY valid JSON object, whatever rule the contract refused it for. So an answer refused for a wrong enum, a missing unrelated field, or any other reason is sent down the road that says "Reply with ONLY the missing quotation". The issue is explicit: *when the failed rule is a quotation rule*, ask only for the missing quotation. Decide the branch on WHICH RULE FAILED, read from the refusal, not on whether the payload happens to parse.

FAULT 4 — THE PROSE RE-ASK NOW TELLS THE MODEL A WORD THAT MEANS NOTHING.
`prose.go:115` builds `"The rule your answer broke: " + string(RepairReshaped)`. `RepairReshaped` is `RepairKind = "reshaped"` (prose.go:36) — a JOURNAL KIND, not a rule. The prompt the model receives now reads "The rule your answer broke: reshaped." The comment you put above it says "THE RULE IT NAMES IS THE CONSTANT, never a duplicated prose string", and your changelog repeats the claim — but the constant it names is the wrong constant, so the comment and the changelog both state a law the code does not implement. Either name the actual broken rule there, or take the sentence out; do not leave a comment asserting something untrue.

KEEP WHAT IS RIGHT. `reask(ask, offending, note)` carrying the note that names the broken rule is correct and is acceptance test 2 — keep it. The quotation-repair road is the right idea — keep it, fix its gate, its merge and its journaling. Keep the changelog file, but correct the sentence that claims the rule name is interpolated from a constant if that stops being true, and fill in `invalidates:` — it is currently `[]`, and a change entry has to say what somebody now believes wrongly.

DONE MEANS ALL FOUR
1. `go test ./internal/shaped/ -count=1` is GREEN — every test, including `TestTheJournalSaysWhyAnAnswerCouldNotBeRead`, which you must not modify.
2. Both named acceptance tests hold: a first answer that fails only the quotation rule is repaired with ONE short call over the answer already held; and the re-ask prompt names the rule the first answer broke.
3. `go build ./...` passes.
4. Nothing outside `internal/shaped/` and `docs/changes/unreleased/` is touched. Never `git add -A`. Do not commit or push.

Your tools are `sh`, `job`, `write`, `edit`, `web`, `recall`, `capabilities` — there is no `read` tool; use `sh` with `sed -n`. Every file and line above is verified: do not spend turns rediscovering them, start editing in your first few turns. Run the tests yourself before you say you are done — the last run did not, and that is how fault 1 shipped.
```

### Run 3 — steering 2. $2.3168, 2700.05s, 102 calls, exit 2. Green, and band-aided.

**41.1% of this run's spend was waste** ($0.9527 of $2.3168) — 19 errors, 10 of them 429s
— and it made **19 gate calls** against 2 in run 1. The gate then could not be reached at
all:

```
note: the delivery gate did not judge task-2-x1 — the gate could not be reached:
context deadline exceeded; delivering unjudged
```

Prompt, verbatim:

```
Finish issue #515. The working tree holds your work and IT DOES NOT COMPILE. Your last run stopped mid-edit. Two things remain, and the first is one function.

FAULT A — THE TREE IS BROKEN. This is everything that matters; fix it first.

    $ go build ./...
    # github.com/Agent-Field/aforge-v2/internal/shaped
    internal/shaped/shaped.go:266:14: undefined: findQuotationField

You wrote the call site and never wrote the function. `grep -rn 'findQuotationField' internal/shaped/` finds exactly one hit — line 266, the call. The call site is:

    if field := findQuotationField(joined, why(refused)); field != "" {

So the function you still owe has the signature `func findQuotationField(answer string, note string) string`, and its contract is set by how line 266 uses it: given the answer already held and the refusal note, return the name of the JSON field that should carry the quotation, or `""` when the refusal is NOT about a quotation — because `""` is what keeps every non-quotation refusal on the ordinary re-ask road. Write it, in the house's comment style, saying WHY in full sentences. Do not change line 266 to work around it and do not delete the branch.

WHAT YOU ALREADY GOT RIGHT — KEEP ALL OF IT, it fixed three of the four faults
- The journal now notes `RepairReasked` only on the branch's SUCCESS path, so a repair that falls through is journaled once at line ~288 and not twice. That was the blocking regression and your shape is correct.
- `mergeQuote(original, quote, fieldName string)` takes the field as a parameter instead of hardcoding `raw["quote"]`.
- The branch is gated on the refusal note rather than on whether the payload parses as JSON.
- `reask(ask, joined, why(refused))` carries the rule the answer broke. That is acceptance test 2 and it is done.

FAULT B — STILL UNFIXED. `prose.go:115` reads:

    contract += "\n\nThe rule your answer broke: " + string(RepairReshaped) + ".\n\n..."

`RepairReshaped` is `RepairKind = "reshaped"` (prose.go:36) — a JOURNAL KIND, not a rule. The model receives the sentence "The rule your answer broke: reshaped." The comment you added directly above says "THE RULE IT NAMES IS THE CONSTANT, never a duplicated prose string", and your changelog says the rule name is "interpolated from the rule's constant". All three agree with each other and none of them agrees with what the code does. Either name the actual rule that was broken there, or delete the added sentence and the comment and the changelog clause together. A comment asserting a law the code does not implement is worse than no comment.

ALSO: `invalidates:` in `docs/changes/unreleased/515-shaped-reask-broken-rule.md` is `[]`. A change entry has to say what somebody now believes wrongly. Fill it in.

DONE MEANS ALL FOUR
1. `go build ./...` passes.
2. `go test ./internal/shaped/ -count=1` is GREEN — every test. `TestTheJournalSaysWhyAnAnswerCouldNotBeRead` must pass and you must NOT modify it.
3. The two acceptance tests hold: a first answer failing only the quotation rule is repaired with ONE short call over the answer already held, and the re-ask prompt names the rule the first answer broke.
4. Nothing outside `internal/shaped/` and `docs/changes/unreleased/` is touched. Never `git add -A`. Do not commit or push.

RUN `go build ./...` AND `go test ./internal/shaped/` YOURSELF BEFORE YOU SAY YOU ARE DONE. The previous two runs each ended with a claim of progress over a tree that did not pass — once a failing test, once a failing build. Build first, early, and often; the package is small and `go test ./internal/shaped/` takes under a second.

Your tools are `sh`, `job`, `write`, `edit`, `web`, `recall`, `capabilities` — there is no `read` tool; use `sh` with `sed -n`. Everything above is verified. Start editing in your first turn.
```

---

## 2026-09-03 · #520 — the tasks list is ordered by the work's own facts

Three hand-offs, 128.8 minutes, $4.8651, 288 model calls. **Not mergeable**, and this is
the closest of the three: everything is green and one requirement is quietly unmet.

**Where it ended.** `go build ./...` clean. `go test ./internal/tui3/` green in **458.2s**,
the whole package. `go test ./internal/session/` green in 112.1s. `bash scripts/laws.sh`
green across 13 packages. A change entry with a real `invalidates`. The seam is right:

```go
func (w World) Work() []WorkEntry     // {Entry, Row}, ordered by task facts
```

```go
for _, we := range world.Work() {
	put(tasksKeyOf(we.Entry), tasksItem{entry: we.Entry, row: we.Row, runs: we.Row.Runs(we.Entry)})
}
```

The issue proposed `Work() []TaskIndexEntry`; carrying the `SessionRow` alongside is a
deviation from the proposal and a better one — it is what stops the page losing a row.

**The blocking gap.** The issue is explicit that the order must hold across all four
authorities: *"the `mine` and `away` authorities are appended after the world pass … so
whatever order is chosen has to hold across all four authorities, not just the file one —
otherwise this window's own unlanded work stays pinned to the bottom of its section."*
It does not hold. `grep -c 'sort\.' internal/tui3/tasksplace.go` is still **0**; `put` records
first-arrival order, file entries are put first, `mine` and `away` after, and the sections are
assembled in arrival order. A `mine` entry is still drawn below every file entry in its
section regardless of its own facts.

**And the test that looks like it covers this cannot detect it.** The second half of
`TestTasksOrderedByWorkFactsNotConversationRecency` adds one `mine` row at `at(25,11)` —
`now` — so it lands in `done today` beside a single file entry, and one `away` row whose
only assertion is which section it is filed under. It then re-checks the `earlier` section,
which contains no `mine` or `away` entry at all. Every assertion passes with the defect
present. This is the one requirement I repeated verbatim in the prompt.

### Run 1 — plain prompt. $1.8297, 2700.04s, 96 calls, exit 2. The right piece, unconnected.

`World.Work()` written and correct, two session tests passing — and `grep -rn '\.Work()'`
found no caller. `tasksplace.go` was untouched, so `/history` ordered exactly as before and
the defect was untouched. Its own tui3 test failed, partly on a fixture bug of its own: it
labels a task "newest landed" at 2h, which lands in `done today`, while asserting four
items in `earlier`. No change entry. **22.5% of the run's spend was waste** ($0.4123, 23
errors, 18 of them 429s) and it made **22 `recalibrate` calls** against 1-3 in every other
run of the trial.

Prompt, verbatim:

```
Fix issue #520 in this repository (Agent-Field/aforge-v2): "tasks: the list is ordered by which conversation was touched last, not by the work's own facts".

THE LAW
Work is ordered by the work's own facts (state, age, need), never by which conversation was touched last.

THE MECHANISM (this is measured, not guessed)
The tasks page (`/history`, `ctrl+.`) breaks the law. Inside a section, the row order is a fact about conversations, not about the work. Three sorts stack and none of them is about a task:
1. Projects are sorted by the newest conversation in them — internal/session/world.go:507, `sort.SliceStable(world.Projects, ...)` on `Project.At()`, which is the newest `SessionRow.At` in the bucket, and `SessionRow.At` is `meta.LastUserAt` — when a person last spoke. That ordering is correct for the home page, whose sections are projects and whose rows are conversations. It is wrong for tasks.
2. Conversations inside a project are sorted by triage then recency — `sortSessions`, world.go:709. Also a conversation fact.
3. Nothing at all sorts the work itself. `rollUp` keeps the index rows in file-append order (world.go:632).
internal/tui3/tasksplace.go:153 then consumes that nesting verbatim, files each item into one of four sections and keeps arrival order within each (tasksplace.go:227-236). `grep -c 'sort\.' internal/tui3/tasksplace.go internal/tui3/place_tasks.go` is 0.
Observable: `earlier` puts work that ended 8d ago above work that ended 2d ago, because one project's newest conversation is two minutes old and the other's is twenty. Touch one conversation — the same `lastUserAt` write an ordinary user message makes — and the oldest row in a section jumps to the top, with no task fact having changed.

WHERE THE FIX BELONGS
internal/tui3 is not the only surface that will ask this question, so put the shared reading in one place instead of two. Add `func (w World) Work() []TaskIndexEntry` in internal/session/world.go, beside the existing `func (w World) Sessions() []SessionRow` (world.go:130) and written the same way: flatten every project's index rows and order them by the work's own facts — needs a person, then running, then by `EndedAt`/`StartedAt` newest first — with `(SessionID, ID)` as the identity. `readTasks` then seeds its pass from `world.Work()` rather than the `Projects → Sessions → Tasks.Rows` nesting, and keeps its four sections and its family grouping, which are already task facts.
Note: the `mine` and `away` authorities are appended after the world pass (tasksplace.go:175-190), so the chosen order has to hold across all four authorities, not just the file one — otherwise this window's own unlanded work stays pinned to the bottom of its section.

ACCEPTANCE — named tests
1. A test on `World.Work()`: rows come back ordered by the work's own facts, and touching a conversation's `LastUserAt` does not change the order.
2. A test on the tasks page: with two projects whose conversations were last spoken to in one order and whose tasks ended in another, the rows within a section are in the tasks' order, and the order holds for rows from the `mine` and `away` authorities too.

LAWS IN FORCE FOR THIS CHANGE
- Fix it generally, at the seam: the shared reading goes in internal/session, not a sort bolted onto the page.
- Do not change the home page's ordering. Its sections are projects and its rows are conversations, and recency is correct there.
- Low complexity: the smallest change that makes the law true. Do not refactor beyond it.
- The named tests must fail before the change and pass after.
- Write a change entry under docs/changes/unreleased/. Use `make changelog-new PR=520 KIND=fix SLUG=<slug>` if that target works; otherwise copy the shape of an existing file in that directory. It must say what was true before and what is true now.
- Do NOT edit internal/manual/chat/ or internal/manual/pages/ unless a string a person actually reads changes.
- Read CLAUDE.md at the repository root before you start; its rules bind you. Note in particular that `go test ./internal/tui3/` is slow — give it `-timeout 15m` and run only the tests you need while iterating.
- Do not touch unrelated files. Do not run `git add -A`. Do not commit or push; leave the change in the working tree.
- When you are done, run `go build ./...` and the tests of every package you touched, and say in your deliverable which test names you added and the exact command that runs them.
```

### Run 2 — steering 1. $1.9946, 2326.6s, 98 calls, exit 2. Wired, and it broke two tests.

It wired `readTasks` to `world.Work()` correctly and **reported all five of my acceptance
items green, itemised**. Running the full `tui3` package — which it had not run — showed two
pre-existing tests failing:

```
--- FAIL: TestTheTasksSectionsAreSeparatedByABlankLineAndNothingElse
    tasksplace_test.go:196: the fixture drew 3 section words, want 4
--- FAIL: TestTheTasksCursorOnlyOpensTaskRows
    tasksplace_test.go:222: row 3 lost its entry or its conversation: {entry:{… SessionID:} row:{ID: Dir: …}}
```

Verified as genuine regressions rather than assumed: stashed the change, both went green,
restored, dropped the stash entry. Neither is in `.github/known-red.txt`. The cause was the
one line it changed — the old triple loop held the `SessionRow` as its loop variable, and
the new version re-derived it through `tasksRowFor`, which falls back to a zero
`SessionRow` when the entry's `SessionID` does not match.

It also wrote `internal/enginehost/host.log` into the source tree. Gitignored, so harmless.

Prompt, verbatim:

```
Finish issue #520. The working tree holds your work. You built the right thing and then did not connect it, so the defect the issue is about is still there.

FAULT 1 — NOTHING CALLS `World.Work()`. This is the whole fix, and it is missing.

    $ grep -rn '\.Work()' --include='*.go' .

returns only pre-existing hits on a DIFFERENT method (`place.Work()` in cmd/aforge). Your `World.Work()` in internal/session/world.go has no caller anywhere. `git status --short` shows `internal/tui3/tasksplace.go` is UNMODIFIED. So the tasks page still reads the `Projects → Sessions → Tasks.Rows` nesting, still inherits project-by-conversation-recency, and `/history` orders exactly as it did before your change. A reviewer running the page sees no difference at all.

Where to connect it, verified line facts in `internal/tui3/tasksplace.go`:
- Line 151: `func readTasks(world session.World, mine tasksMine, win session.UsageWindow, seen, now time.Time) tasksReading`.
- Lines 165-167 are the nesting to replace:
      for _, project := range world.Projects {
          for _, row := range project.Sessions {
              for _, entry := range row.Tasks.Rows {
  Seed the pass from `world.Work()` instead. Keep `tasksRowFor(world, mine, entry)` (line 244) to recover the `SessionRow` each entry needs, since `Work()` returns entries and the item still wants its row.
- Line 175 (`for _, row := range mine.rows`) and line 180 (`for _, task := range mine.away`) append the `mine` and `away` authorities AFTER the world pass. The issue is explicit that the chosen order has to hold across all four authorities, not just the file one — otherwise this window's own unlanded work stays pinned to the bottom of its section. Your own test asserts this in its second half.
- Keep the four sections and the family grouping. They are already task facts.

FAULT 2 — YOUR OWN ACCEPTANCE TEST FAILS, and its fixture is part of why.

    $ go test ./internal/tui3/ -run TestTasksOrderedByWorkFactsNotConversationRecency -count=1 -timeout 15m
    tasksplace_order_test.go:70: earlier section has 3 items, want 4. Page:
        done today
          ✓ newest landed        slow chat 2h
        earlier
          ✓ old task             fast chat 3d
          ✓ slightly newer       fast chat 2d
          ✓ mid landed           slow chat 1d
    --- FAIL

Two things are wrong and you must fix both. The ordering is wrong because of fault 1. But the fixture is also wrong on its own terms: the task you label "newest landed" is 2h old, so it lands in `done today`, not in `earlier` — yet the assertion wants all four labels in `earlier`. Age the fixture so every task the assertion names falls in the section the assertion reads, or read the sections the fixture actually produces. Do not weaken the assertion to match the broken order.

FAULT 3 — NO CHANGE ENTRY. `docs/changes/unreleased/` has no file for 520. Write one — `make changelog-new PR=520 KIND=fix SLUG=<slug>`, or copy the shape of an existing file there. It says what was true before and what is true now, and `invalidates:` must not be empty.

KEEP EVERYTHING THAT IS RIGHT. `World.Work()` is well-shaped and correctly commented, and both `TestWorkOrdersByTaskFactsNotConversationRecency` and `TestWorkTiebreakIsSessionIDThenID` PASS. Do not rewrite them. Do not change the home page's ordering — its sections are projects and its rows are conversations, and recency is correct there.

DONE MEANS ALL FIVE
1. `readTasks` seeds its pass from `world.Work()`, and `grep -rn 'world.Work()' internal/tui3/` finds it.
2. `go test ./internal/tui3/ -run TestTasksOrderedByWorkFactsNotConversationRecency -count=1 -timeout 15m` PASSES.
3. `go test ./internal/session/ -count=1` stays green.
4. `go build ./...` passes.
5. A change entry exists under `docs/changes/unreleased/`.

`go test ./internal/tui3/` takes about 150s on a quiet box and much longer on a loaded one — ALWAYS give it `-timeout 15m`, never the default, and while iterating run only your own test with `-run`. Never `git add -A`. Do not commit or push.

Your tools are `sh`, `job`, `write`, `edit`, `web`, `recall`, `capabilities` — there is no `read` tool; use `sh` with `sed -n`. Everything above is verified: start editing in your first turn, and run the tests yourself before you say you are done.
```

### Run 3 — steering 2. $1.0408, 2700.05s, 94 calls, exit 2. Green, and one gap.

Fixed the regression at the seam exactly as asked, by carrying the row with the entry
rather than teaching `tasksRowFor` to guess better.

Prompt, verbatim:

```
Finish issue #520. The wiring is now correct and it BROKE TWO EXISTING TESTS. Fix the regression; do not undo the wiring.

WHAT IS RIGHT — KEEP ALL OF IT
`World.Work()` in `internal/session/world.go` is good and its two tests pass. `readTasks` now seeds from `world.Work()` at `internal/tui3/tasksplace.go:165`, which is the fix the issue asked for. `TestTasksOrderedByWorkFactsNotConversationRecency` passes. `go build ./...` is clean, `go test ./internal/session/` is green in 111s, and the change entry `docs/changes/unreleased/520-tasks-order-by-work.md` is written and good. None of that needs touching.

THE REGRESSION — two tests that pass on a clean tree now fail. I verified this by stashing your change and re-running them; both were green without it, and neither is in `.github/known-red.txt`.

    $ go test ./internal/tui3/ -count=1 -timeout 15m
    --- FAIL: TestTheTasksSectionsAreSeparatedByABlankLineAndNothingElse
        tasksplace_test.go:196: the fixture drew 3 section words, want 4
    --- FAIL: TestTheTasksCursorOnlyOpensTaskRows
        tasksplace_test.go:222: row 3 lost its entry or its conversation:
            {entry:{ID:1 ... Label:verify the pro model's pricing Status:unverified ... SessionID:}
             row:{ID: Dir: Transcript: Project: ... Tasks:{Rows:[] ...}}}

THE CAUSE, and it is the one line you changed
The old loop had the `SessionRow` IN HAND — it was the loop variable:

    for _, project := range world.Projects {
        for _, row := range project.Sessions {
            for _, entry := range row.Tasks.Rows {
                put(tasksKeyOf(entry), tasksItem{entry: entry, row: row, runs: row.Runs(entry)})

Your version throws that away and looks the row up again by identity:

    for _, entry := range world.Work() {
        row := tasksRowFor(world, mine, entry)

`tasksRowFor` (tasksplace.go:244) searches `world.Projects` for a matching row and, when it finds none, `return mine.row`. So any entry whose `SessionID` is empty — or that names a session the search cannot match — comes back with a ZERO `SessionRow`. That is exactly what the dump above shows: every field of `row` empty. And because `runs: row.Runs(entry)` and `tasksSectionOf` (line 253) read `item.row.NeedsPerson()`, a lost row also mis-files the item into the wrong section, which is the second failure — a section word disappears because a section came out empty.

WHAT TO DO
Carry the row with the entry instead of re-deriving it. The row is a fact `Work()` already had in hand when it flattened the projects, and re-looking it up by an identity that may be empty is what lost it. Fix it at the seam — `internal/session/world.go` — so the page cannot lose a row again, rather than by patching `tasksRowFor` to guess better. Whatever shape you choose, `readTasks` must end up with the same `SessionRow` for a file-authority entry that the old triple loop gave it, and `TestWorkOrdersByTaskFactsNotConversationRecency` and `TestWorkTiebreakIsSessionIDThenID` must still pass.

DONE MEANS ALL FIVE
1. `go test ./internal/tui3/ -count=1 -timeout 15m` is GREEN — the whole package, including both tests named above, which you must NOT modify.
2. `go test ./internal/session/ -count=1 -timeout 15m` stays green.
3. `TestTasksOrderedByWorkFactsNotConversationRecency` still passes and `readTasks` still seeds from `world.Work()`.
4. `go build ./...` passes.
5. Nothing outside `internal/session/`, `internal/tui3/` and `docs/changes/unreleased/` is touched. Your last run left `internal/enginehost/host.log` in the tree — it is gitignored so it did no harm, but do not write logs into the source tree.

`go test ./internal/tui3/` takes 150-460s depending on load — ALWAYS `-timeout 15m`, never the default, or it is cut off at the finish line and reports whichever test was running as a hang. While iterating use `-run` on the two failing tests; run the whole package once before you say you are done. Run it YOURSELF: the last run reported all five items green having never run the full package.

Your tools are `sh`, `job`, `write`, `edit`, `web`, `recall`, `capabilities` — there is no `read` tool; use `sh` with `sed -n`. Everything above is verified. Start editing in your first turn.
```
