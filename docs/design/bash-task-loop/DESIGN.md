# The bash-only task loop — DESIGN

*2026-09-16, written against `origin/dev @ 96eccc72d`. Status: design for an
experiment. Everything here lives on one branch, `spark/bash-task-loop`, and
`dev` does not move unless the experiment wins. The experiment measures
codeaf's task belt against a one-action bash loop; the loop's shape is
described below, step by step.*

## The one sentence

A task node's belt of twenty-odd tools is replaced by one native `bash` tool —
plus the few hands that cannot be a shell command — under a strict
one-action-per-response envelope, and the experiment is won or lost on numbers
anybody can read: suites green on the Spark, and the same briefs run on both
belts with steps, cost, wall time and failures counted.

## Why, and why now

The loop the experiment measures against runs every agent — root and every
recursive worker — on one loop with ONE native bash tool. All coordination is PlanDB
commands run *through* that bash. The bet the owner wants measured: most of
what codeaf's task belt carries is a shell command wearing a schema, and a
worker with one honest shell, a strict envelope and good truncation may do the
same work in fewer, cheaper, more legible steps — because every model alive
has seen ten thousand times more `sed` than `edit`-tool JSON.

codeaf is the right place to measure that because the belt is already composed
in one function (`Agent.belt`, `internal/session/tools.go:134`), a task node's
belt is already a *variant* of it ("A TASK NODE'S BELT IS THIS BELT MINUS
THREE"), and the safety rails around a task node's shell already exist. The
experiment changes what the worker is *handed*, never what it is *allowed*.

## The loop the experiment measures against

Its shape:

- **One action per response.** Exactly one tool call, name must be `bash`,
  args exactly `{command: nonempty string}` (`action_from_message`). Malformed
  responses are rejected *without poisoning replay history* — the diagnostic
  goes back as a tool result or a user message, the bad envelope never re-enters
  the context (`reject_action`) — and 4 invalid actions fail the task.
- **Checkpoint before execute.** `trajectory.json` is written with
  `state["pending"] = call_id` BEFORE the command runs. On resume the action is
  never replayed: the model is told *"Previous execution interrupted. Effects
  may exist. Inspect files and PlanDB before next action; never blindly
  repeat."*
- **Per step, before the model call**, a fresh PlanDB context update (diffs,
  not full state) plus a frame — constraints, environment, remaining steps,
  shared spend, capacity — is appended to the messages. Responses log to
  `llm.jsonl`; `finish_reason` is validated (`length` means no action
  executed).
- **bash runs with a timeout that kills the process group**; output over
  20,000 chars is truncated to head 10k + tail 10k with the full output's file
  path.
- **A task is a context boundary**: a child starts from its assignment,
  upstream results and notes — never the parent's transcript. Decomposition
  first; a focused leaf executes directly; its parent integrates and verifies.
- **Shared cost limit across workers and a per-task step limit**; a provider
  response that omits cost blocks further requests when a limit is set.

## What this repo already has (verified 2026-09-16)

| piece | where |
| --- | --- |
| The belt, composed in one function | `(*Agent).belt`, `internal/session/tools.go:134` |
| The seven file tools and their hands | `internal/exec/bare` (`AllToolsCapped`, `tools.go:56`); schemas pinned to the shipped belt |
| Window-following result caps | `bare.CapsFor` (`internal/exec/bare/truncate.go:71`) over `ctxbudget.ToolResultBytes`; `(*Agent).resultCaps` (`tools.go:74`) |
| The session's wrapped bash (background jobs) | `(*Agent).backgroundBash` (`tools_jobs.go:185`) |
| read's PDF text-layer sense | `(*Agent).pdfRead`/`senseRead` (`tools_pdf.go:68`) |
| write's append/continuation wrap | `(*Agent).appendableWrite` (`tools_write.go:55`; salvage in `salvage.go`) |
| Oversized-output stubbing to disk | `internal/session/stub.go` (`stubOldOutputs`) — the same head+path idea, today applied at compaction time |
| Task-node belt gates ("minus three, minus five at the floor") | `Config.mayWatch` (`beltfacts.go:97`), `(*Agent).mayProposeTask` (`task.go:534`), `fansOutAt` (`task.go:549`), `taskDepthLimit = 3` (`task_run.go:201`) |
| The worker's construction | `(*Agent).newTaskAgentOn` (`task_run.go:7135`) |
| The runner and its step cap | `runTaskChild` (`task_child_run.go:129`); `taskLimits.maxSteps` (`task_run.go:2707`), checkpoint/extension at `task_child_run.go:592` |
| The turn loop and its one execution seam | `internal/session/loop.go`; the batch runs every call through one function (`loop.go:3181`) |
| **The ground guard that already reads bash** | `internal/session/taskoutside.go` — refuses a command aimed outside the task's copy, names both sides (`outsideGroundRefusal`) |
| The approval posture of an unwatched node | `task_run.go:7321`: `ActionAllow` with the critical floor — "never a question and never a hang" |
| The write-scope guard, and its stated limit | `writeGuard` (`orchestrate.go:1327`) binds edit/write/edit_video paths and **names bash out of reach** — "What bounds a node's shell is the same thing that bounds every other agent's: the approval floor." |
| The no-todo law this experiment extends | `docs/CHAT-V3.md` Decision 11 (the todo-drop paragraph at :581) |
| The bench protocol and its honesty rules | `bench/README.md`, `BENCHMARKS.md` (self-reported cost only; graded by code, no LLM judge; same-day medians) |
| The Spark remote and runner | git remote `spark` = `santosh@spark:src/aforge-v2`; `spark --help`: `run`/`now`/`push`/`wait`/`logs`, `--lane gpu|cpu`, `--name`, `--key`, `--wait` |

Two facts in that table do most of the safety work, and they are worth reading
twice because they are why this experiment is small: **every bash command a
task node runs is already scanned for outside-ground effects** (taskoutside),
and **bash was never inside the write-scope guard**, so removing the guarded
tools removes nothing the guard was catching. The belt swap widens no
capability; it removes six ways of spelling what bash already spells.

## Decision 1 — the experiment is a second belt, not an engine change

A task worker's belt is composed by the same `belt()` as the conversation's,
differentiated by predicates on its `Config`. The experiment adds one more
predicate and one more composition:

- `Config.bashBelt bool`, set exactly where the worker's `Config` is built —
  `newTaskAgentOn` (`task_run.go:7135`) — from one environment variable,
  `CODEAF_TASK_BELT=bash`. Unset, every byte of today's behavior is kept,
  which is what lets both arms of the experiment run from one binary.
- `(*Agent).bashBelt()` in a new file, `internal/session/bashbelt.go`,
  composes the experiment belt: the one bash tool, the kept hands from the
  table below, and nothing else. `belt()` delegates to it when the predicate
  holds.
- **`internal/exec/bare` is untouched.** The hands stay its; the experiment is
  a *wire* change — which tools the model can name — not a change to how a
  file is read or a command is run. The conversation belt, subharness leaves
  and the resident keep the file tools exactly as they are. This is the wrong
  answer the brief warns about, stated as law: **we are not gutting bare; we
  are handing one family of workers a smaller belt.**

Every `InTask` worker reads the switch: ordinary tasks, quick tasks, division
parts, merge resolvers. The conversation itself never does.

## Decision 2 — one action per response, and parallelism moves into the shell

the one-action envelope is adopted whole for bash-belt workers: **exactly one tool
call per response.** A response carrying zero calls and no final answer, or
two calls, or a call whose arguments do not parse, is rejected without
entering the replay history — the diagnostic goes back as the tool result, the
malformed assistant message is dropped — and **four consecutive invalid
actions end the node failed** through the existing `circling` road
(`task_run.go`'s halted endings), which is where "the model could not drive
the belt" already lands.

The hook is the one seam every execution already passes through: the batch
runner in `loop.go` (:3181). On the branch belt it validates the envelope
before running anything.

codeaf today *encourages* parallel tool batches, so this is a real trade and
the doc says so plainly: the worker loses multi-tool batches and gains
one-action discipline. The replacement idiom is the shell's own —
`cmd1 & cmd2 & wait`, `xargs -P`, one `git grep` instead of three `grep`
calls — and the doctrine page (below) teaches it. Whether that is a win is
exactly what the bench measures; if workers compensate with giant compound
commands that fail atomically, the step count will say so.

## Decision 3 — truncation takes head+tail+path, sized by the window law

The branch's bash tool truncates **head + tail + path**: over the cap, the
full output is written beside the node's own log as `action-NNNNNN.txt`, and
the result carries the head, a `[output truncated; full output: <path>]`
marker, and the tail. This replaces the tail-only cut for bash, and it is
the head+tail+path shape verbatim.

Two things are deliberately not the loop's:

- **The cap is not a flat 20,000 chars.** It is `bare.CapsFor(a.window())`,
  the same window-following pair the rest of the belt uses (ONE READ MAY NOT
  BE MOST OF WHAT THE MODEL CAN HOLD). The head/tail split is half the byte
  cap each. A 128k-window worker therefore gets head ≈25KB + tail ≈25KB —
  close to the loop's 10k+10k — and a small-window worker gets proportionally
  less, which a flat 20k would have blown past.
- **The output file lands in the node's own log directory**, not the working
  copy, so a worker's `git status` is never dirtied by its own telemetry.

This is also the answer to today's stub machinery: `stub.go` already teaches
that a big output belongs on disk with a path in its place. The branch moves
that moment from compaction time to truncation time, which is strictly earlier
and strictly more honest.

## Decision 4 — the checkpoint ports as a sentence, not as trajectory replay

The loop checkpoints a *trajectory* and resumes it; codeaf's recovery unit is
the *node*, and its law is already written: **THE TREE IS THE RECORD**
(`docs/design/task-continue/DESIGN.md` §D). A dead process means
`taskStore.interrupt` requeues the node and a FRESH worker opens on the tree
the predecessor left. Replaying a half-executed tool call across a process
boundary is not a mechanism this engine has or needs, and building one for the
branch is rejected: it is the one loop mechanic whose problem codeaf
already solves differently.

What DOES port is the sentence that makes the loop's resume safe. A node
resumed after an interrupt has its opening composed with one added clause —
*"your predecessor was interrupted mid-work; effects may exist in the tree —
inspect before repeating anything"* — in the same opening brief the
interrupted node's fresh worker already gets (`task_child_run.go`'s queued
opening). Wave 2 owns it, and owns verifying what the resumed opening already
says so the clause is added, not duplicated.

## Decision 5 — coordination stays native; the graph is not PlanDB

The loop runs coordination THROUGH bash because its plan store is a file
hierarchy a CLI can own. codeaf's task graph lives inside the session process — the
frontier, the admission governor, the room, the rail, the settle door — and
`propose_task`/`tasks` are its doors. The branch keeps them native.

The fuller experiment — a `codeaf task` CLI the worker shells out to, making
coordination bash-shaped too — is named here and rejected for this branch: it
is a second experiment stacked on the first, and it would confound the
measurement. If the bash belt wins, it is the obvious next branch.

## Decision 6 — `background` and `jobs` stay

The loop has no background execution. codeaf's task nodes run unwatched under
a bash ceiling (`BashCeilingSeconds`, `internal/exec/bare/tools.go:121`), and
`background: true` plus `jobs` is how a twenty-minute build outlives it
without burning the node's steps. They stay on the branch belt. The deviation
from the loop is one boolean on the one tool, and it is bought with the reason
the ceiling exists: a worker nobody is watching must not hold its node on a
clock.

## The tool mapping — every tool on today's task-node belt

A depth-1 task node's belt today, grouped as `belt()` composes it, with the
gate that puts each family on. "Bash" means the tool comes off and the idiom
column is what the doctrine page teaches instead; "kept" means it stays on the
branch belt, with the reason it cannot be a shell command.

### The seven file tools — all seven collapse into the one bash

| tool | idiom on the branch | what the wrapper loses | the answer to the loss |
| --- | --- | --- | --- |
| `read` | `sed -n '400,520p' f`, `cat f`, `head -50 f` | window-following caps; the PDF text layer; oversized results landing as a disk stub | caps: **reimplemented** in the branch bash (Decision 3). PDF: **tool stays** — `read_document` remains, and the doctrine page sends PDFs to it; catting a PDF yields bytes, which is honest. Stubs: **reimplemented** as head+tail+path, earlier than today (Decision 3) |
| `bash` | — it IS the substrate | nothing; it gains the head+tail+path cut | the one tool; keeps `timeout`, keeps `background`, keeps the process-group kill and `BashCeilingSeconds` |
| `edit` | inline `python3` patch, `sed -i`, or a `applypatch`-style heredoc diff | exact-match uniqueness safety (refuses when oldText matches 0 or 2+ regions) | **accepted**, and measured: the doctrine page teaches `grep -c` before `sed`; a patch applied to the wrong region is a failure the cell's own tests grade |
| `write` | `cat > f <<'EOF'`, append `cat >> f` / `tee -a` | the append-continuation salvage (`salvage.go`); auto-created parent dirs | **accepted**: heredoc append is `>>`; the doctrine page teaches `mkdir -p` first and quoting `<<'EOF'` against expansion |
| `grep` | `git grep -n pat` in a repo, `grep -rn --exclude-dir=.git` outside | gitignore respect; result caps | **accepted**: `git grep` IS gitignore-respecting search; outside a repo the exclude idiom is taught; caps ride Decision 3 |
| `find` | `find . -name '*.go' -not -path './.git/*'` | gitignore respect; result caps | **accepted**, same as grep |
| `ls` | `ls -la` | nothing real | caps ride Decision 3 |

### The session's own tools — each judged on whether a shell can be it

| tool | decision | reason |
| --- | --- | --- |
| `read_document` | **kept** | the billed parser; the split from `read` is where the bill is (`tools_doc.go`). No shell command is a paid document parse, and the absence law forbids a fake one |
| `jobs` | **kept** | the other end of bash's `background` argument (Decision 6) |
| `manual` | **kept** | the packed BM25 corpus; a worktree holds the Markdown but not the index, and the chat's account of itself is not a `grep` away |
| `ask` | **removed** | the loop reaches the person through the plan CLI, not a consent gate. A question the loop answers by re-planning is not a message to route, and a belt carrying both would teach two ways to say one thing |
| `watch` | already absent on nodes (`mayWatch`) | — |
| `settings`, `change_setting` | already absent on nodes | — |
| `propose_task`, `tasks` | **kept** | the task graph's doors (Decision 5) |
| `quick_task`, `divide_work`, `revise_assignment`, `items` | **kept** | task-kind verbs on the same graph and gates as today |
| `remember`, `search_conversations` | **kept** | session-owned stores (durable memory, indexed transcripts); files, yes, but the indexing and the provenance law are the tool |
| `track`, `commit`, `recall` | **kept** | compaction-surviving working state; pure session machinery |
| `web_search` | **kept** | the search backend has no CLI on the belt's machines |
| `web_fetch` | **kept** | `curl` fetches bytes; the tool strips markup and bounds the result. Listed as an open question below because it is the most bash-able of the kept set |
| `stand` | absent on nodes already (nothing unwatched may arm a standing spend) | — |
| `build_harness`, `list_harnesses`, saved-program verbs | absent on nodes already (no registry, no runner) | — |
| `anchor_workspace` | absent (a node is never unanchored) | — |
| furrow's workspace verbs (restore points, forks, merge) | **kept** | furrow IS a CLI — but the belt's verbs carry the bound law (`internal/furrow/boundlaw_test.go`) and the ID-plus-`--yes` discipline; a worker with raw `furrow` in bash has the destructive commands one typo closer. Kept, and the doctrine page does not mention furrow |
| `services`, `use_service` | **kept** | credentials and account consent; never a shell |
| `generate_image`, `speak`, `generate_music`, `generate_video` | **kept** | billed model calls; nothing in bash paints |
| `view_image` | **kept** | a vision model call |
| `edit_video` | **kept** | ffmpeg is bash-reachable, but the tool is the curated door with measured joins and letterboxing; bash ffmpeg remains possible because bash exists — the tool stays as the safe default |
| `load_capability` (the shelf) | kept as machinery | the kept-but-rare families (media, services, furrow) ride the shelf exactly as today, so the branch belt's *fixed* surface is: `bash`, `jobs`, `manual`, the graph verbs, the state trio, `read_document` |

### The safety rails, restated against the new belt

Nothing about the swap weakens a rail, and the section exists to say so in
writing:

- **Every bash command still passes `taskoutside.go`** — the ground guard
  refuses writes aimed outside the task's copy, and it already parses shell
  commands, because today bash is how a node would make such a write.
- **The approval floor is unchanged** (`ActionAllow` + critical table): the
  commands the floor refuses are refused whichever tool spelled them.
- **The write-scope guard loses nothing** — it never claimed bash
  (`orchestrate.go:1308`: "a guard that pattern-matched commands would be
  claiming a guarantee it cannot keep"). Quick tasks keep their scope
  semantics exactly as today: the guard binds the kept tools' paths, and their
  shell was always bounded by the floor instead.

## The doctrine page

One new prompt page, `internal/session/prompts/bashworker.md`, replaces the
file-tool guidance for bash-belt workers only (`prompt.go`'s composition reads
the same `Config.bashBelt`). It carries, short and flat: the one-action
envelope and what a rejection reads like; the read/write/edit/grep/find/ls
idioms from the table; the `grep -c`-before-`sed` discipline; `mkdir -p`
before heredocs; `&`/`wait` and `xargs -P` for parallelism; head+tail+path and
where the full output lives; "PDFs and office documents go to `read_document`,
never `cat`"; and the loop's own resume sentence's sibling: never simulate
execution — run it and read the observation. `prompts/worker.md` and the other
task pages are untouched; a second convention beside an existing one is
prohibited, so the bash page REPLACES the tool-idiom paragraphs for its
workers rather than adding beside them.

## The per-step frame

The one loop mechanic beyond the belt that ports fully. The loop appends, per
step, a frame with current facts. codeaf's runner already OWNS those facts and
shows them to everyone except the model: steps against `maxSteps`
(`task_child_run.go:592`), the node's own spend (the usage ledger every
request feeds), and family news (the queue the room drains). The branch
composes a three-line frame at each step boundary for bash-belt workers, at
the same drain point the queued opening brief already uses
(`landVolatileLocked`, `agent.go`):

```
step 41/200 · $0.183 so far
family: part 3 of 5 landed (12 files); part 4 running
```

Everything in it is already counted; the frame is a rendering, not a new
instrument. It is deliberately NOT a PlanDB diff — codeaf's coordination state
is the graph, and the graph's news already has a lane.

## What stays untouched

Named so the branch's diff has a boundary it can be reviewed against:

- **`internal/exec/bare` and the shipped wire schemas** — the conversation belt and
  every subharness leaf keep the seven tools, byte-identical.
- **The conversation belt** — `belt()`'s existing composition, the standing
  wrap, the shelf. The experiment is the task node's belt.
- **The ground ladder, worktrees, and the seal** — where a task stands is not
  a belt question.
- **Audit, settle, the checker, the merge roads** — how a task is judged is
  not a belt question either, and keeping it identical is what makes the
  comparison honest: same judge, two belts.
- **Crew model seats and routing** — both arms run the same pinned model.
- **`internal/tui3` wholesale** — see the next section.
- **`internal/exec`'s own engine** (`executor.go`, `runner.go`, `schedule.go`,
  `settle.go`), `internal/plan`, `internal/orchestrate`, `internal/head`,
  `internal/resident` — the v1 machinery neither runs v3 task nodes nor
  changes.
- **The manual corpus.** The flag is off by default, the shipped belt is
  unchanged, and the manual gates (`make test-quick`) keep passing because the
  belt they read is built without it. The day the bash belt SHIPS is the day
  the manual law applies; the branch's own documentation is this folder.

## How a bash-only task still reads on screen

No surface change, and the reason is already true today: the room's tool
cluster and the step gutter key off tool NAME through the tokens vocabulary,
and a node that runs only `bash` renders as rows of commands with the command
glyph — which is exactly how a bash-heavy conversation already reads. The
truncation marker's path points into the node's log directory, which the room
already knows how to show. The rail's phase words, the pulse file, the
`flight` journal lines and the call trail are all belt-agnostic. If the bench
shows reviewers squinting at raw commands where a `read` row used to say
"foo.go, lines 400-520", THAT is a finding about legibility to report with the
numbers — not a problem to pre-solve with a surface change on an experiment
branch.

## The seams, file by file

| file | change |
| --- | --- |
| `internal/session/bashbelt.go` *(new)* | `(*Agent).bashBelt()`: the one bash tool (`bashTool`: bare's bash hand + `background` + head+tail+path truncation) and the kept families, composed |
| `internal/session/bashbelt_envelope.go` *(new)* | the one-call validator and its reject-without-poison result; the consecutive-invalid counter ending the node on the `circling` road |
| `internal/session/bashbelt_truncate.go` *(new)* | head+tail+path, sized by `bare.CapsFor(a.window())`, writing `action-NNNNNN.txt` beside the node log |
| `internal/session/tools.go` | `belt()` delegates to `bashBelt()` on the predicate; header comment gains the paragraph naming the experiment and its branch |
| `internal/session/beltfacts.go` | `Config.mayBashBelt()` — `InTask && bashBelt`, so no road hands it to a conversation |
| `internal/session/task_run.go` | `newTaskAgentOn` sets `Config.bashBelt` from `CODEAF_TASK_BELT`; nothing else in the file moves |
| `internal/session/loop.go` | the batch seam (:3181) runs the envelope validator when the agent's belt is the branch belt |
| `internal/session/agent.go` | the per-step frame composed at `landVolatileLocked` for bash-belt workers |
| `internal/session/prompts/bashworker.md` *(new)* | the doctrine page; `prompt.go` swaps it in on the predicate |
| `internal/session/task_child_run.go` | the resumed node's opening gains the effects-may-exist clause (wave 2) |
| `internal/session/bashbelt_test.go` *(new)* | the wave-1/2 acceptance tests below |
| `bench/bashloop/` *(new)* | the cell driver, cell definitions, and the results CSV — the comparison's whole apparatus |
| `docs/design/bash-task-loop/DESIGN.md` | this file; a `REPORT.md` beside it lands with the verdict |

Deleted: nothing. Renamed: nothing. `internal/exec/bare`: untouched.

## Branch, base, and running the suite on the Spark

- **Base:** `origin/dev @ 96eccc72d`. The local `dev` sits at `77293538c` and
  is NOT the base; the branch is cut from the fetched origin tip so the
  experiment stands on what would merge, not on a local pointer.
- **Name:** `spark/bash-task-loop`. The repo's families are `task/`, `feat/`,
  `fix/`, `tune/`, `chore/`, and this is none of them: it is never opened as a
  pull request against `dev`, and it lives primarily on the `spark` remote
  (`santosh@spark:src/aforge-v2`). The prefix advertises both facts. Cut and
  publish:

  ```sh
  git fetch origin dev
  git switch -c spark/bash-task-loop origin/dev
  git push -u spark spark/bash-task-loop    # the experiment's home
  git push -u origin spark/bash-task-loop   # backup and review surface; NO pull request
  ```

- **Suites run on the DGX Spark, never the laptop** (the repo's standing
  rule). The exact spelling, against the local `spark` binary's own
  `--help`:

  ```sh
  spark push                                              # sync this checkout to the Spark
  spark run --lane cpu --name suite "make check"          # the full-tree ritual: vet, fmt, test, packed manual, size
  spark wait <id>                                         # blocks, exits with the job's code
  spark tail <id> 100                                     # bounded log on a red
  ```

  The cpu lane is the right lane: the suite is agent work, not GPU work, and
  `--lane cpu` runs concurrently instead of queueing behind whoever holds the
  GPU lease. During the waves the lighter loop is
  `spark run --lane cpu --name focus "make test-focus PKGS=./internal/session RUN='^TestX$'"`,
  and the per-wave acceptance below names which. `make check` is the wave-gate
  spelling because it is the same command CI runs on the way into staging, so
  green on the branch means mergeable, not merely passing.

## Waves, each landing green

| wave | lands | acceptance (all on the Spark) |
| --- | --- | --- |
| **W0 — branch and switch** | the branch, `Config.bashBelt`, the env read, no behavior change | `make check` green; `git diff origin/dev` touches no behavior; the baseline arm (below) can already run |
| **W1 — the bash belt** | `bashbelt.go`, truncation, the envelope, the doctrine page | `make test-focus PKGS=./internal/session RUN='^TestBashBelt'` green: belt composition (kept set present, the six file tools absent), envelope rejects a two-call batch without poisoning history, truncation shape and cap-follows-window, a scripted task reads+writes+edits a file through bash alone. Then `make check` |
| **W2 — loop mechanics** | the per-step frame, the resume clause, the invalid-action ending | `RUN='^TestBashBelt'` plus a scripted interrupt/resume: the fresh worker's opening carries the clause; the frame's numbers equal the runner's own counters; four invalids land `failed · went in circles`. Then `make check` |
| **W3 — the comparison** | `bench/bashloop/`, the grid, the CSV | every cell's row exists; both arms same model, same day, interleaved order; the driver's own dry run prints every invocation it will make (bench protocol's honest-wiring rule) |
| **W4 — the verdict** | `REPORT.md` beside this file | the numbers below, the verdict word, and the branch kept (win) or deleted (loss) |

## The comparison

**Arms.** A is today's belt, B is the bash belt — one binary, `CODEAF_TASK_BELT`
unset vs `bash`, the same model pinned for both (`deepseek/deepseek-v4-flash`,
the bench's recorded model; a kimi-k3 pair rides along if the owner wants the
new model read at the same time, never mixed into the same arm).

**The driver.** `bench/bashloop/` starts one task per cell through the v3
engine's real door (`StartTask`) against a throwaway `CODEAF_HOME`, waits on
the landing, and reads **steps, cost, wall and the ending** out of the node's
own journal and ledger — the readings the engine already keeps. No LLM judge
anywhere: code cells are graded by the target repo's own test suite, document
cells by presence-and-content checks, per the bench protocol's standing
doctrine.

**Cells** (six briefs, n=3 replicates per arm, interleaved same-day):

| cell | shape it measures | graded by |
| --- | --- | --- |
| c1 small fix | one failing suite in a fixture repo | the suite green |
| c2 feature | new module + tests | the suite green, diff sane |
| c3 multi-file refactor | reads-heavy work — the read-idiom tax | suite green, API unchanged |
| c4 report | research and synthesis into a `REPORT.md` | presence + section coverage |
| c5 wide job | forces `propose_task` fan-out (kept-tool coordination through the bash loop) | children landed, integrated result |
| c6 kept-tool | a brief needing `generate_image` (a diagram) through the bash belt | the image exists and is referenced |

**Quoted per arm:** success rate, and over successful cells the medians of
steps, cost, wall — plus the branch-only diagnostics: invalid-action count,
truncation events, and (spot-checked) edit-idiom misapplications.

## The verdict: pass/fail as numbers, and the way back

**Gate 0 — the branch is sound:** `make check` green on the Spark at every
wave boundary. Red stops the experiment; it does not get argued past.

**Gate 1 — the comparison, read as a table:**

- **PASS** — all three: (a) no cell family shows the capability-regression
  shape (arm A ≥2/3 successes and arm B ≤1/3); (b) median steps or median cost
  on successful cells improves ≥20% on arm B, and no family is worse on BOTH
  cost and wall by >25%; (c) zero safety incidents — no outside-ground write
  past the guard, no approval-floor trip, no dirty working copy from
  telemetry.
- **FAIL** — any family with the regression shape; or cost AND wall both worse
  >25% on the median; or one safety incident. The branch is fixed or rolled
  back, and the report says which.
- **Between the two** — the person's call, made on the printed table. The
  experiment does not grade its own middle.

**Rollback.** The branch is the whole blast radius: `git push spark --delete
spark/bash-task-loop && git push origin --delete spark/bash-task-loop` and the
local branch deleted. `dev` never contained a line of it, the flag never
shipped, and there is nothing to revert. That is the point of Decision 1 and
it is stated here so the deletion is the designed ending, not the sad one.

## Open questions

1. **Scope.** The experiment covers task nodes only — the conversation belt
   keeps the file tools whatever the verdict. If the bash belt wins big, is a
   conversation-side bash mode a follow-up anyone wants, or is "the worker's
   belt" the whole claim?
2. **`web_fetch`.** Kept native on the branch belt (markup stripped, result
   bounded) — or should the experiment answer "what is genuinely not bash" by
   making fetch `curl` and accepting raw bytes, so the kept set is proven
   rather than asserted?
3. **The media family in wave 1.** Kept on the bash belt from day one
   (recommended — the absence law is about machines, not belts), or left off
   until a brief actually needs one, so wave 1 measures the smallest possible
   belt?
4. **What a win merges as.** A second belt behind a shipped setting, or a
   replacement of the task belt outright? The bench answers whether the bash
   loop is better; it does not answer whether the product should carry two.
