# How work on its own actually runs

## Does a task touch my working copy?

No. Each task gets its own checkout of the repository, on its own branch, so you can keep
working in yours while it runs.

aforge runs `git worktree add -b <branch> <dir> HEAD` off your **current HEAD**.

- **Directory:** `<repo root>/.aforge-v3/tasks/<session-slug>/<task id>`. The session
  segment is the conversation's own id. A conversation with no session file on disk gets
  `unfiled-<6 hex>`, minted once per process, so two windows never collide.
- **Branch:** `task/<title slugified, at most 32 characters>-<6 hex>` — for example
  `task/fix-the-nil-map-crash-9c1a2f`. The random tail lets the same title be proposed
  twice.

The worktree lives **inside** the repository, so you will see it in `git worktree list` and
in your file browser. `.aforge-v3` is reset out of every task commit, so it never merges.

If a directory is already at that name it can only be this session's own dead run, so it is
removed with `git worktree remove --force`, pruned and deleted before the add.

Two limits:

- A workspace that is **not a repository**, or a repository with **no commit to branch
  from**, runs **in place** in your own directory, and says so:
  `it worked directly in the workspace: there was no repository to branch`
- A failed `git worktree add` fails the task with
  `could not prepare a working copy: git worktree add: <first line of git output>`

## What a task can do while it runs

A task is the same agent you talk to, with the same tools, in a quieter place.

It inherits the conversation's provider client, context window, image support, roles
source, search provider and fetcher, **connected accounts**, image-generation model and
document engine. It inherits **neither the transcript nor the memory file** — the brief is
its whole world. If it runs on a **different model from the conversation**, its context
window is set to 0 rather than reusing a window measured for another model.

**Three tools are missing from its belt:** `propose_task` (a task does not propose more
work), `watch`, and `tasks`.

Approval inside a task is allow-everything, with the critical floor still under it (things
like `rm -rf /`, `mkfs`, redirecting onto a raw disk, shutdown). When a call hits that
floor there is nobody to ask, so the task reads the refusal
`refused in a task: <rule> — nobody to ask` and keeps working. `use_service` cannot
connect a new account inside a task.

A task is also a job. It shows in `jobs list` labelled `task 7` with the title as detail,
`jobs kill` ends it exactly as a time limit does, and closing the session kills every
running task. Its step-by-step log is the job log, at
`<workspace>/.aforge-v3/jobs/<job id>.log`.

## How a task reports back to you

When a task lands, its **report** is its final assistant message, cut to the first **3
non-empty lines**, each clipped to **300 characters**.

The landing note arrives at a step boundary, exactly like a background job's exit. Its
first line carries the task's transcript URI:

```
task 7 finished: <title> · transcript file:///…
```

Then the report. Then, when there were changes, `changed: a.go, b.go`, and one line saying
where the branch went:

- `its branch task/… merged into yours`
- `its branch task/… did not merge cleanly and was kept — merge it yourself when you are ready`
- `it was stopped; its branch task/… was kept`
- `it worked directly in the workspace: there was no repository to branch`

The task's own tool rows never enter the chat. They go to its journal and its room only.

When a task's work does come home, everything it wrote is staged with `.aforge-v3` reset
out, committed on its own branch as `task: <first line of title, at most 72 chars>` with
the identity `aforge <aforge@localhost>`, then merged into your branch with
`git merge --no-edit`. The merge is attempted whatever your tree looks like — a dirty
checkout is normal. On success the worktree is removed and the branch is deleted. Two
tasks finishing at once are serialized, so a merge is never lost.

## How long a task gets before it is stopped

Two clocks.

**One hour for the whole task.** The run, every correction round and every check inside it
share one deadline of 60 minutes. On expiry the task lands failed with the report
`ran out of time`, the task's own last words underneath, and its branch kept.

**Five minutes for a check.** Each second look at finished work is bounded at 5 minutes.
It hangs off the task's own deadline, so `jobs kill` ends it too. A check that burned its
whole five minutes is not retried.

There are two step limits as well, both of which stop a task that is going nowhere:

| Limit | Default | Report when it fires |
| --- | --- | --- |
| `max_steps` — finished tool calls | 200 | `stopped: 200 steps and no finish` |
| `no_progress` — calls in a row that change and teach nothing | 6 | `stopped: 6 steps without progress` |

The `no_progress` counter resets on a successful `edit` or `write`, on a read-only call at
a target the task has not aimed at before (`read`, `read_document`, `ls`, `grep`, `find`,
`web_search`, `web_fetch`, `jobs`, `recall` — a failed one still counts as learning), and
on a `bash` that either left new changes in the worktree or ran a command not run before.
`note`, `forget`, `track`, `commit` and `generate_image` are deliberately not progress.

## How aforge knows a task really finished

A task is never done on its own say-so. When the work finishes, a **separate, fresh,
read-only checker** is put in the task's worktree, runs the repository's own checks, reads
the diff, and answers. Only a pass merges.

The checker has no shared context and no memory of the work. Its whole world is the
acceptance you set, the task's own claim (labelled as a claim, not as evidence), the list
of files written, and where to look. **The brief is deliberately withheld** so it grades
the contract, not the effort.

What it may touch: `read`, `grep`, `find`, `ls`, and a `bash` restricted to an allowlist —
`go test`, `go build`, `go vet`, `git diff`, `git log`, `git status`, `git show`, `pwd`,
`wc`, `head`, `cat`. It cannot edit, write, install, fetch or paint. Shell composition is
refused outright: any of `; | & < > $ ( ) { }`, a backtick or a newline in the command is
turned away before the allowlist is even consulted. Every result it reads is capped at
8000 bytes.

Before the check, new files are staged so the diff shows everything including brand-new
files. Staging happens once, so every look judges the same tree. In a workspace that is
not a repository the checker is told
`This workspace is not a repository, so there is no diff to read: check the files themselves.`

This is controlled by `task.audit`, **on by default**, and settable in your profile only.
With it off, the gate stands open, the task's own account merges, the task lands done, and
the report is marked `nothing checked this work: the task.audit setting is off` above the
task's own words. There are no correction rounds at all.

## What happens when the work is not right yet

When the second look says what is missing, the task gets a **fresh worker in the same
worktree**, the original brief, and the gaps in front of it, word for word. The worker is
asked to close the gaps and nothing else:

```
The work so far stands and is already in this working copy. Do not start it again and do not undo any of it: close the gaps above, and nothing else.
```

The worker is fresh; the worktree is not. The task stays *running* while a round is under
way, and you see one plain line of what is being closed.

**How many rounds:** `task.repair_rounds`, default **1**, profile-only. 0 turns correction
off. With the default, a task is worth at most **2 checks and 1 correction worker**.

The person checking is never told it is looking at corrected work — the same packet, the
same contract, no round number. A finding is never re-rolled; asking again until the
answer changes is not checking.

**When it still is not right:** the task lands **failed**, its branch is **kept**, and
anything waiting on it fails with it. The report leads `incomplete — ` followed by the
first gap and then the rest. A later round leads `still incomplete after another go — `,
so three sets of evidence read as three attempts. With nothing said at all, the report is
`incomplete — nothing was said about what is missing`.

The model is told plainly not to quietly spend another task on it:
`what is missing is above and the branch is kept: offer them a follow-up in their own words before anything else is spent on it`

Everything a correction worker and every check spends is folded into the same task's cost,
and the task's elapsed keeps running, because the task never landed.

## The three ways a task can land

Every task ends in exactly one of three states, and the words are the same everywhere you
read them.

**Finished.** `task 7 finished: <title>`. The second look held. The branch merges into
yours, and the report leads with the evidence, alone — no lead word at all.

**Failed.** `task 7 failed: <title>`. Somebody looked and made a finding, or a limit
fired. The branch is kept. Anything waiting on it fails with it.

**Needs your look.** `task 7 needs your look: <title>`. Nobody could look, or nobody would
say. The task is neither done nor failed: nothing merges, the branch is kept, and nothing
waiting on it fails. The report leads
`finished, but needs your look — ` and then what was said, or
`finished, but needs your look — nobody could say whether it holds` when nothing was said.
The landing then says in as many words that it is neither done nor failed, that its branch
is kept, and that anything waiting on it waits until somebody decides.

The sentences you may see when nobody could say are written plainly:
`the checker could not start: <err>`, `the checker could not be asked: <err>`,
`no answer in 5m0s, so nothing was accepted`, `the checker answered neither way`. When two
tries in a row got nothing, the first line is prefixed
`asked twice and got no answer either time — `.

## Nothing is thrown away

On every ending except a clean merge, the branch is **kept and named**. This is true
without exception:

- a task that ran out of time keeps its branch;
- a task you killed with `jobs kill` keeps its branch, and the partial work with it;
- a task whose work was found incomplete keeps its branch, exactly as a killed one does.
  "Not proven" is not "throw it away";
- a task that needs your look keeps its branch;
- a task whose merge conflicted keeps its branch, and you are told
  `its branch task/… did not merge cleanly and was kept — merge it yourself when you are ready`;
- a session that ended mid-run keeps the branch, and says where it is.

A task that ran **in place** — no repository to branch from — is never described as
aborted, because its edits are already in your tree.

So work is recoverable even when it did not merge. The branch name is in the landing note,
in the checkpoint on disk, and in the project's index of landed work.

## Work that needs your look holds up what depends on it

A task can name `depends_on` — the ids of tasks that must finish first. When it starts,
its brief is given their reports, under the line
`What the work before you learned:` and then, per prerequisite,
`<title> (task N):` and the report.

What happens depends on how the earlier task landed:

| The task it waits on | What happens |
| --- | --- |
| Finished | It becomes ready and starts when a slot is free |
| Failed | It fails too, with the report `it waits on task 3, which did not finish` |
| Not in this session's work at all | It fails, with `it waits on task 3, which is not in this session's work` |
| **Needs your look** | It **stays queued** — it does not fail |

That last row is the point. Work that needs your look does not knock over everything
behind it. Dependents wait rather than failing, and they wait indefinitely: nothing will
move them on its own until you decide what to do with the task in front of them.

Dependencies can only point backwards — ids ascend — and that is enforced when a session's
work is reloaded from disk.

## Choosing which model a task runs on

You say it in the conversation, in words: "use opus for this one". The model then puts the
`model` argument on `propose_task`. There is no key, command or field you type directly.

The word may be a whole catalog id (`anthropic/claude-opus-5`), the tail after the vendor
(`claude-opus-5`), or any set of tokens found in one id (`opus 5`, `opus-5`). Case,
surrounding space and a leading OpenRouter `~` are ignored.

Matching tries three rungs in order, and the first that answers wins: the **whole id**
exactly (so `openai/gpt-5` is never read as part of `openai/gpt-5-mini`); the **tail** after
the vendor; then **every token** appearing anywhere in an id, sorted shortest id first,
ties alphabetically — the plain name before its variants.

**One match** is just used. Nobody is asked, and the receipt reads
`task 7 started on anthropic/claude-opus-5: <title>`.

**Two to four matches** get settled by you, on the proposal you are already being shown.
The closest match leads, and that is what silence takes. Only a member of that shortlist
can win: naming anything else, an empty answer, and the clock all fall back to the leading
member. The task is admitted with one model, never a set. The shortlist is capped at 4 —
the fifth would turn a proposal into a picker.

**If no model was named**, the task runs on `task.model` from settings when that is set,
otherwise on **the model the conversation is on right now** — read live, so `/model` moves
it and a task groomed after the switch goes to the new one. `task.model` is profile-only:
a repository must not be able to send your work and your credit to a model you never
picked. Blank means the conversation's own model.

The receipt only names a model when the `model` argument was given. A task that named no
model is not told which default it got.

## When aforge refuses a model name

Two refusals, both ordinary tool results the model can retry from in one round trip.

**No model by that name.** With near matches — ids sharing at least one token, shortest
first, at most four:

```
no model here is called "opos-5" — did you mean anthropic/claude-opus-5, anthropic/claude-opus-5-thinking? Name one of those, or leave model out to run on <default id>.
```

With no overlap at all — a word like "fast" or "cheap":

```
no model here is called "fast". Name a model id the person has, or leave model out to run on <default id>.
```

`<default id>` is what the task **would** run on if the argument were left out.

**Too many matches.** More than four candidates is not a shortlist, it is a list:

```
"claude" matches several models — say which: a, b, c, d.
```

The four named are the first four candidates, shortest id first.

**When nothing can say which models exist**, there is no refusal at all. An empty list is
"nobody can say", not "there are none": the word is taken exactly as written and the
provider answers for it. This is what happens in the first seconds of a session, while the
catalog is still warming — a `model` argument used then is passed through unchecked.

Once resolved, the model is remembered for the task's whole life: on the proposal, on
every update, and in the checkpoint on disk, so it survives a restart. A checkpoint from
an older build carries none, and the task reads as "the conversation's own".

## How many tasks run at once

By default, **no limit**. `task.parallel` is 0 (blank) out of the box, and 0 means no cap.

A cap, if you set one, is a **queue and never a refusal**: a ready task past the cap sits
and starts when a slot frees.

The real ceiling is the machine. Before each scheduling pass, aforge asks whether one more
task may start:

| Setting | What it reads | Default | Effect |
| --- | --- | --- | --- |
| `task.max_load` | one-minute load average divided by core count, from `/proc/loadavg` | **1.5** per core | at or above it, no new task starts |
| `task.min_free_mb` | `MemAvailable` (not free memory) from `/proc/meminfo`, in MiB | **1536** (1.5 GiB) | below it, no new task starts |

Either one set to 0 turns that check off. Readings are cached for **1 second**. When a
task is held back this way it is re-asked every **5 seconds** — a machine getting quieter
is not an event, so it has to be looked at on a clock.

This gates **starts only**. Nothing already running is ever touched; pressure drains as
running tasks finish.

**The honest caveat:** these two governors read `/proc`, so they only work on Linux. On
macOS and Windows there is no `/proc`, the machine cannot say, and silence is never
treated as a hold — those platforms get no pressure gating at all, and `task.max_load` and
`task.min_free_mb` do nothing there.

A running task whose provider call is being paced reports that it is rate limited. That is
a count, not a flag: a task can have a correction worker and a checker out at once, and it
stops being paced when the last of them gets through.

`task.parallel`, `task.max_load` and `task.min_free_mb` are all profile-only settings.

## Does a task survive a restart?

The graph survives. The running work does not.

When a session comes back:

- tasks that were **done**, **failed**, **needing your look** or **queued** come back
  exactly as they were, with their leavings intact;
- a task that was **running** comes back **failed** and marked interrupted, with a report
  saying where its work is:
  - `session ended mid-run; branch task/… kept` — plus `, its worktree is at <dir>` when the
    directory is still there. The branch is checked in the repository first;
  - `session ended mid-run; its branch task/… is no longer in the repository`;
  - `session ended mid-run; it had not got as far as a working copy`;
  - `session ended mid-run; it worked directly in the workspace, so whatever it wrote is in your tree`;
- then the queue is turned again: a queued task whose prerequisites are still done starts
  now.

You see one line about it, as context for your first turn rather than as a reason to start
one:

```
recovered task graph: 2 done · 1 interrupted (branch task/fix-it-9c1a2f kept) · 1 waiting
```

The counts are done, failed, needing a look, interrupted and waiting. The branch clause
reads `no branch kept`, `branch X kept` or `branches X, Y kept`. Any completion notes that
were never delivered appear underneath.

A completion is announced **exactly once across lives** — a resumed session does not
re-tell the model about work it already read about.

## Where task state is written on disk

Three places.

**The checkpoint**, one per conversation, in the conversation's own folder:

```
~/.aforge/v3/projects/<workspace-with-dashes>/<session id>/tasks.json
```

It holds the id counter and, per task in admission order: id, title, summary, brief,
acceptance, depends_on, state, report, the task's own claim, changed files, branch,
worktree, merge outcome, model, `max_steps`, `no_progress`, elapsed, and whether it has
been noted or was interrupted. Cost is deliberately **not** in it — a rehydrated figure
would be a number nobody could point at a request for. The assembled brief is not in it
either; it is rebuilt from the prerequisites' reports when a task starts.

It is written after **every** transition, atomically, never only at exit. On load it is
schema-checked, and **any** violation drops the file whole and starts the session with no
graph rather than refusing to start.

A conversation with **no session file on disk** gets no checkpoint at all, and runs tasks
anyway.

**The project index**, one per workspace, shared by every window open on that project:

```
~/.aforge/v3/projects/<workspace-with-dashes>/tasks.jsonl
```

Append-only, one row per landed task: id, name, label, title, status, the first sentence
of the outcome, file count, cost, duration, when it ended, the session id, and two URIs —
where the work is and where the transcript is. Never the content: it is an index, not an
archive. A read keeps the newest 2000 rows.

**Task transcripts**, which are real, resumable session files you can open with `read`:

```
~/.aforge/v3/tasks/<session id>/<YYYYMMDD-HHMMSS>_<task id>.jsonl
```

Beside each one sit `…_<id>-audit-<6 hex>.jsonl` per check and `…_<id>-repair1.jsonl` per
correction round. A task's own pointer stays on the **first** file — the run that is the
task itself.

## What happens when a task fails

Endings are checked in a fixed order, and the first match wins:

| # | What happened | The report |
| --- | --- | --- |
| 1 | No working copy could be made | `could not prepare a working copy: <err>` |
| 2 | The worker would not start | `could not start the task: <err>` |
| 3 | A step limit fired | `stopped: 200 steps and no finish` or `stopped: 6 steps without progress` |
| 4 | The hour ran out | `ran out of time` |
| 5 | You stopped it (`jobs kill`, closing the session) | `stopped before it finished` |
| 6 | The run errored | `it ended with an error: <err>` |
| 7 | Stopped while its work was being looked at | `stopped while its work was being checked` |
| 8 | Nobody could say | `finished, but needs your look — …` |
| 9 | Gaps left after the correction rounds | `incomplete — …` |
| 10 | Otherwise | done: the evidence first, then the task's words |

In rows 3 to 7 the task's **own last words are kept underneath** the one-line reason, and
the branch is kept.

What you read on a failure is `task 7 failed: <title>`, the report, the changed files, and
the line saying the branch was kept. Anything waiting on that task fails with it — the
cascade walks one layer per scheduling pass.

## What propose_task needs from you

`propose_task` is how the model moves a self-contained piece of work out of the
conversation. You cannot call it yourself — you ask for the work, and the model grooms it.

Four arguments are **required**:

| Argument | What it is |
| --- | --- |
| `title` | One line naming the work, as you would say it |
| `summary` | Two or three lines you read to decide whether to redirect it |
| `brief` | The task's **whole** context. The task never sees the conversation |
| `acceptance` | The observable done-condition: the command that must pass, the behaviour that must hold, the output that must appear |

What the task is actually asked is `brief` + `"\n\nAcceptance: "` + `acceptance`. The same
`acceptance` string is also what the second look judges against — one text, two readers.
So a vague acceptance costs twice.

A missing argument comes back as an ordinary result, never an error:
`Invalid arguments: title is required`, and the same sentence for `summary`, `brief` and
`acceptance`, in that order. Unparseable JSON answers `Invalid arguments: ` and the parse
error.

Once a task is admitted, its brief and its acceptance are **frozen**. Nothing changes them
after that — not steering, not a correction round. Steering is talk to the worker, not a
new target. If the objective itself was wrong, the answer is a new proposal.

## The optional arguments on propose_task

Four more arguments, all optional. Every bad value is an ordinary result, not an error.

**`depends_on`** — an array of task ids that must finish first. The task waits for them,
and their reports are put in front of it when it starts. Ids can only point backwards.

**`model`** — which model this task runs on. Set only when you asked for a particular model
or class of model for this work. Left out, the task runs on `task.model` if set, otherwise
on whatever model the conversation is on at that moment.

**`max_steps`** — how many finished tool calls the work is worth before it is stopped as
stuck. Default **200**. On the limit the worker is cancelled and the task lands failed with
`stopped: 200 steps and no finish`, the number being the limit that was in force. A
negative value answers `Invalid arguments: max_steps cannot be negative`. Zero or absent
means the default.

**`no_progress`** — how many tool calls in a row may teach nothing and change nothing
before the task is stopped as spinning. Default **6**. On the limit the report is
`stopped: 6 steps without progress`. A negative value answers
`Invalid arguments: no_progress cannot be negative`.

Both step limits are recorded in the checkpoint, so they survive a restart along with the
rest of the task.
