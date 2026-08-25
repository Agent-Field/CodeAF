# How work on its own actually runs

## What happens when I type /task

`/task <brief>` first raises one forming block at the transcript tail. Its dim `▏ `
hairline joins the word `task`, your verbatim quoted brief, and the live phase. The phase
begins as `sizing it up…` for the plain sized form and advances in place to
`shaping the brief…`; `/task solo`, `/task adaptive`, and the preset `single` road skip
straight to shaping. The spinner and count-up keep moving on the same frame clock as the
other live rows. When the start succeeds, the block is replaced in that same frame by
the normal started-task row and the task appears on its rail. When it fails, the block is
replaced by the error sentence. The hairline never remains on settled work.

The same block appears when you say yes to a task the model proposed on its card:
approving is followed by the same short pause while the brief is prepared, so the block
rises with the task's name on it — unquoted, because the name is aforge's word rather
than something you typed — and it collapses the moment the task appears on the rail.
Saying no, or redirecting the proposal, raises nothing. One forming look for both ways a
task begins; only the card, which asks before the model spends your money, is particular
to the proposed one.

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
document engine. It inherits **not the transcript** — one assembled brief is its whole
world, and your own message is the first part of it (below, under *What the task actually
reads*). The
one thing it is given of what aforge remembers about you is the handful of lines its own
brief needs: the conversation asks the router once, against that brief, and puts the
answer at the top of the task's instructions (what-i-remember). The task itself never
writes a memory — a family of eight tasks would be eight writers on one brain, each blind
to the others. If it runs on a **different model from the conversation**, its context
window is set to 0 rather than reusing a window measured for another model.

**Three tools are missing from its belt:** `watch`, and the settings pair `settings` and
`change_setting` — a task works in a worktree with nobody watching it, so a watch's news
would arrive in a conversation it does not have, and a permanent change to your machine
that no transcript ever showed you is exactly what a task must not be able to make.

**It keeps `propose_task` and `tasks`, as a pair.** A task may hand pieces of its own work
out when its brief holds parts that do not need each other — at most **5**, and a piece it
hands out cannot hand out more — and `tasks` is how it then watches them. Inside a task
both are scoped to its own family: `tasks` lists the pieces it handed out and refuses an id
outside them with `No task "…" among the pieces you handed out.` Its brief is still its
whole world; the project's history is not its to read. The tasks page has the whole of it,
under *When a task splits its own work*.

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

A task is told to make those lines the **substance** of the work — what it found or made,
the key findings, the decisions it took, with every file named by its full path — and not
the evidence trail. "`git diff` shows a staged new file", test output, staging and branch
status and step counts are proof it did the work, and they stay in the task's journal.
On a finished task the report leads with the task's own account, and what the second look
checked it on stands under that.

The landing note arrives at a step boundary, exactly like a background job's exit. Its
first line carries the task's transcript URI:

```
task 7 finished: <title> · transcript file:///…
```

Then the report. Then, when there were changes, `changed: a.go, b.go`, and one line saying
where the branch went:

- `its branch task/… merged into yours`
- `its branch task/… did not merge cleanly and was kept — merge it yourself when you are ready`
- `it was stopped; what it made is committed on its branch task/…, which was kept — merge that branch to take the work`
- `it was stopped; its branch task/… was kept` (when it made nothing)
- `it worked directly in the workspace: there was no repository to branch`

The task's own tool rows never enter the chat. They go to its journal and its room only.

**To read a finished task's report again later**, open the task from `/history` — `ctrl+.`,
or the `ctrl+. earlier` line at the bottom of the task column, which is the column's one
door onto that page. `enter` on an `earlier` row goes inside it, and the card
carries the whole of that final message — read back off the task's own journal — under
`what it said at the end`.

When a task's work does come home, everything it wrote is staged with `.aforge-v3` reset
out, committed on its own branch as `task: <first line of title, at most 72 chars>` with
the identity `aforge <aforge@localhost>`, then merged into your branch with
`git merge --no-edit`. The merge is attempted whatever your tree looks like — a dirty
checkout is normal. On success the worktree is removed and the branch is deleted. Two
tasks finishing at once are serialized, so a merge is never lost.

## What aforge says in the chat when a task lands, and the full path to the file

Nobody typed the landing note, so aforge answers it as if you had asked for the work
directly: what it writes next is **the answer itself** — the findings, the summary of what
was made, what it changes.

The card the landing writes into the conversation already says the task finished, how long
it took, how many files it touched and where the branch went, so aforge does not say that
again, and it does not grade the deliverable. "In good shape", "solid", "genuinely non-trivial" are sentences *about* the
work in place of the work, and so is narrating what it did to get there.

When the report is too thin to answer from, aforge reads the deliverable and answers out of
what is in it. The message is the answer; the file is the deep dive.

## Why did the chat reply on its own

A finished task can arrive while you are not typing. Its landing note starts the turn
that answers the work, so the answer may appear on its own. Immediately above that reply,
aforge draws a dim line with the task's identity mark, its name, and the exact words you
originally asked it to handle. That line is part of the transcript and returns when you
resume the conversation. The finished-task card still stays above the input as before.

An ordinary reply to something you typed has no such line. If an old task has no recorded
request, the line shows its identity mark and name alone rather than an empty quotation.
If several tasks arrive before one answer, their lines are stacked in arrival order above
that answer.

## Which task is this answer about

Read the dim line immediately above the answer. Its task mark and short name are the same
identity used for that task in the task column and its finished card; the quoted text is
your original request verbatim, not the more detailed brief prepared for the worker. More
than one line means the answer is responding to all of those finished tasks, top to bottom.

**Every file aforge names you is named by its full absolute path** — after a task and
everywhere else in the conversation. A relative path like `research/notes.md` is one you
would have to work out a root for, and a task that ran in its own copy of the repository
makes even that a guess.

## Does a task proposal expire while I am in another conversation

**Yes, and it starts the work.** A task proposal counts its own deadline down
inside the session rather than on the screen, so it is unaffected by which
conversation you are looking at: when the countdown runs out the task is
approved and starts, exactly as it would have on a screen you were watching.
A sign-in offer counts down the same way and lapses after five minutes, deciding
nothing.

**The approval question for a tool call is the one that holds.** A conversation
you have switched away from holds it for as long as you are away, and coming back
gives you the reading time you had left — see the permissions page.

All three say `waiting on you` while they wait: on home, on the status line's
`2 open · 1 waiting`, and in a desktop notification the moment the question goes
up — which now fires for a conversation this terminal is holding behind the
screen even while the terminal is focused, because a focused terminal is no
longer evidence that anybody is looking at *that* conversation.

## How long a task gets before it is stopped

Two clocks, and neither is a hard stop.

**One hour per checkpoint.** The run, every correction round and every check inside it
share a 60-minute interval — but when it fires, a second look at the evidence decides what
happens next. Working toward the brief: the task gets another hour, up to five in all
(5 hours is the hard backstop, and a healthy task never meets it). Circling: it is told to
land now — one final turn to write the deliverable from what it already has — and only
then is it stopped, with the threshold and the evidence in the report.

**What the landing turn may still do.** It keeps only the tools that SAVE something:
`write`, `edit`, and whichever media verbs the task had — `generate_image`, `speak`,
`generate_music`, `generate_video`. Everything else comes off, and the instruction names
exactly the hands it kept, so a task whose deliverable is a picture or a piece of audio can
still produce it. Reading, searching and running commands are gone for that turn: it is a
turn for finishing, not for one more look.

**Five minutes for a check.** Each second look at finished work is bounded at 5 minutes.
It hangs off the task's own clock, so `jobs kill` ends it too. A check that burned its
whole five minutes is not retried.

There are two step limits as well, and they work the same way — checkpoints, not killers:

| Limit | Per checkpoint | Backstop | Report when it finally stops |
| --- | --- | --- | --- |
| `max_steps` — finished tool calls | 200 | 1000 (200 × 5) | `stopped: 200 steps and no finish` |
| `no_progress` — calls in a row that teach nothing, save nothing and leave nothing new in the worktree | 6 | 6 (this one fires) | `stopped: 6 steps without progress` |

At a `max_steps` checkpoint the same second look runs: progress buys another 200 steps, up
to the 1000-step backstop. Whatever stops the work, the landing turn runs first — the task
writes up what it has — so nothing is ever lost mid-flight. And a task stopped this way is
still checked against its acceptance afterwards: if the work holds it lands finished and
merges, and the `stopped:` line never reaches you.

## What counts as progress, and what gets a task stopped as stuck

The `no_progress` counter resets on any one of three things, and only fires when a step is
none of them:

**It saved a file.** A successful `edit`, `write`, `generate_image`, `generate_video` or
`speak` — every hand that puts a file on disk at a path the call names. Making a picture is
working; a task asked for two marketing images that generates them, looks at them and
generates them again has never called `edit` in its life, and is not stuck.

**It changed the worktree.** Any step at all — whatever tool it was — that left the task's
working copy different from how the step before it found it. This is the backstop under
everything else, so a tool nobody classified still counts when it actually produced
something. Job logs under `.aforge-v3` are excluded: the harness's own droppings are not
the task's work.

**It learned something.** A read-only call aimed at a target the task has not aimed at
before — `read`, `read_document`, `ls`, `grep`, `find`, `web_search`, `web_fetch`, `jobs`,
`recall`, `view_image`, `manual`, `tasks`, `settings`, `list_harnesses`, `services`,
`gmail_read`, `gmail_search`, `calendar_list` — or a `bash` running a command not run
before. A failed one still counts as learning: finding out that something does not work is
finding something out.

So what actually fires the counter is **the same call again, changing nothing and teaching
nothing** — the same search six times, the same failing edit retried, a command already
run. `note`, `forget`, `track`, `commit` and `change_setting` are deliberately not
progress: a task writing its own memory again has not learned anything.

Failure matters for saving and not for learning. A `generate_image` that came back with an
API error saved no file, so a task calling it repeatedly and getting the same error is
stuck and is stopped — which is what the counter is for.

Being stopped as stuck is **not** a verdict on the deliverable: a stopped task is still
checked against its acceptance, and when the check passes it lands finished and merges with
the `stopped:` line gone. The section below is that whole rule.

## A task stopped as stuck that had already finished its work

Being stopped is a statement about the **trajectory**, never about the deliverable. One of
these really happened: a task wrote all six of the stories it was asked for, spent six steps
re-reading them to be sure, and was stopped with `stopped: 6 steps without progress` — the
same target twice is exactly the spin the counter is for. Its own landing turn then said the
six files were written and the work was done. You saw ✗ failed and a kept branch next to a
report saying it had finished.

So a stopped task is still judged on its work. After the landing turn writes up what it has,
the same check a task that finished on its own gets is run — the same acceptance, the same
worktree, the same read-only checker.

**If the work holds:** the task lands **finished**, its branch **merges** into yours, and
`stopped: 6 steps without progress` is nowhere in what you read. The report is the task's own
account of the work with what it was checked on under it, exactly as any finished task's is.
A limit that fired is not news about a deliverable that is sitting there.

**If it does not hold, or there was nobody to ask:** nothing changes. The report leads with
the limit that fired, the task's own last words stand under it, the branch is kept with the
work committed onto it, and nothing merges.

**One look, and no correction round.** A stopped task gets a single check — never the
`task.repair_rounds` worker a task that finished on its own can earn, because a second worker
in the worktree is paying twice for the run the limit has just ended. With `task.audit` off,
or with no acceptance to judge against, there is nobody to ask and the task simply stays
stopped.

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
yours, and the report leads with the task's own account of the work, with what it was
checked on under it — no lead word at all.

**Failed.** `task 7 failed: <title>`. Somebody looked and made a finding — or a limit fired
and the work did not hold when it was checked afterwards. The branch is kept. Anything
waiting on it fails with it. A limit firing on its own is no longer enough: work that was
stopped and then held lands under *finished* above.

**Needs your look.** `task 7 needs your look: <title>`. Nobody could look, or nobody would
say — or the work held and one of the files it wrote moved under it while it ran, which is
its own section below. The task is neither done nor failed: nothing merges, the branch is kept, and nothing
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

The last line of that landing is the only thing the `task.settle` setting changes. With it
on `ask` — the default — the note says the task waits until somebody decides and offers
`tasks id 7 resolve accept|reaudit|refute`, and tells aforge to say what it thinks and leave
the choice with you; the four choices on the landed card are the door. With it on `auto` the
same note tells aforge to read the report and the work and settle the task itself, and to
come back to you only when it genuinely cannot tell. Everything else in the landing is
identical either way.

## Why my task needs my look when it finished fine — another window changed the same file

There is a second reason `needs your look` fires, and it has nothing to do with whether the
work is any good. **A task that finished, was checked, and passed will still stop short of
merging if somebody else changed one of the same files while it was running.**

This is the case nothing else can catch. A task opens a file, thinks for twenty minutes,
and writes. If a change landed in that file during those twenty minutes, the work is
correct against a world that stopped being true — and the check cannot see it, because the
check runs inside the task's own working copy, which is a copy of the world as it was when
the task started.

So at the moment the branch would merge, the files the task **wrote** are held up against
two things:

- **what finished in them since this task started** — the project's record of landed work,
  which now names the files behind each row's count;
- **what other windows on this project are writing right now** — the live claims each open
  window publishes about the paths its running work has already touched.

When either overlaps, the task lands `needs your look` instead of `done`. Nothing merges,
the branch is kept, dependents wait, and the four choices on the card are the same ones
described in the tasks page — `accept` merges it the ordinary way once you have looked.

**What the report says.** The first line names the files and, where it can, the work that
changed them. Landed work and a window that is still going get separate sentences, because
they are different facts:

```
finished, but needs your look — "rail permanence" changed internal/tui3/home.go while this ran
finished, but needs your look — "drop-up nearest" is also working in internal/tui3/home.go
```

Work nothing ever named is called `another window is also working in internal/tui3/home.go`.
Long lists stop counting out loud after two — `internal/tui3/home.go, internal/tui3/task.go
and 4 more` — and so do long lists of tasks. The task's own account of what it did stands
underneath, along with what it was checked on.

**What will not trigger it**, on purpose:

- **Work that named no files.** A row written by an older build, and work that genuinely
  wrote nothing, look exactly alike. Neither is treated as overlap — a warning raised on a
  silence would fire constantly and you would learn to ignore the real one.
- **The task's own sub-tasks.** A sub-task branches off its parent's working copy and merges
  back into it, so a child landing in a file its parent also wrote is the design working.
- **Files the other work merely read.** Nothing anywhere records what a task read, so the
  claims are about writes only.
- **A neighbouring file in the same package.** Paths must match exactly; there is no
  directory-level matching and no patterns.
- **Work that finished before your task started.** That is a file your task read, not a file
  that moved under it.

If nothing overlaps, nothing changes: the task merges and lands `done` exactly as it always
did. There is no setting for this and no way to see it before the run — the earlier warning
before a task starts is a separate thing, and it cannot see this case at all.

## Nothing is thrown away

On every ending except a clean merge, the branch is **kept and named**. This is true
without exception:

- a task **stopped at a step limit or for lack of progress** whose work did not hold keeps
  its branch — and what it made is **committed onto that branch** before it lands, so
  `git merge task/…` really brings the files over. The landing note names them under
  `changed:` and offers the merge. (If the check passes, that task merges instead and there
  is no branch left to offer.);
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
`<title> (task N):` and the report. That lands inside the `THE WORK` part of what the task
reads, below the model's brief.

What happens depends on how the earlier task landed:

| The task it waits on | What happens |
| --- | --- |
| Finished | It becomes ready and starts when a slot is free |
| Failed | It fails too, with the report `it waits on task 3, which did not finish` |
| Not in this session's work at all | It fails, with `it waits on task 3, which is not in this session's work` |
| **Needs your look** | It **stays queued** — it does not fail |

**A bad id never gets that far on a new proposal.** `depends_on` takes only ids
`propose_task` itself returned in this session. A number that names no task — a
background job's id, an adaptive run's, a step count — and a number whose task has
already failed are both refused on the spot, before you are even asked about the task:
`depends_on names task 1 — no task in this session has that id`. Nothing is created and
nothing dies; aforge corrects the proposal and asks again. The failure rows above remain
for work that goes wrong **after** a task was admitted — a prerequisite that fails while
its dependent is already queued.

That last row is the point. Work that needs your look does not knock over everything
behind it. Dependents wait rather than failing, and they wait indefinitely: nothing will
move them on its own until you decide what to do with the task in front of them.

Dependencies can only point backwards — ids ascend — and that is enforced when a session's
work is reloaded from disk.

## A task's own sub-tasks are its problem — only the top of a family asks you

A task that hands part of its work out is the one that reads those pieces back. A sub-task's
landing report goes to **its parent task's own worker**, not to this conversation — that
worker has the `tasks` tool, the diff and the brief, and it is the only reader that can fold
the piece into the whole. So while a parent is still working, a sub-task of it that needs a
look is **not** put in front of you: the roster does not file that family under `needs you`,
and a folded family row does not wear the `?` its own head is already holding.

That flips the moment the parent lands. A sub-task still waiting on a decision when its
parent settles has nobody left reading its news, so it is handed up one level — to the
grandparent's worker if there is one, and to this conversation if there is not — with the
line `task 4 has finished, and a piece of work it handed out is still waiting on somebody to
decide:` and then the sub-task's own landing under it. Its row then rises to `needs you` on
the roster like any other work that will not move without you.

What this means in practice: **the demand you see is the top of the family, once.** You are
never asked about six pieces of one job while the job is still running, and nothing quietly
rots underneath a task that went home.

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
otherwise on **the model the conversation was on at the moment the task was admitted**.
The id is settled then and **frozen against drift** — a `/model` after that moves the
conversation and never the work already handed over, so a task that sat in the queue
starts on the model you launched it with rather than on whatever you have switched to
since. A task groomed *before* the switch keeps the old model; one started after it gets
the new one.

**Frozen against drift is not frozen against you.** The one deliberate way to move a
running task off its id is to walk into that task's room and press the model's name at
the bottom of the screen: the picker opens aimed at that task, and choosing moves that
task from its next turn onward — the conversation and every other task are untouched. A
task that has already landed is refused, in the words `task 7 is done, not running`. The
tasks page has the whole of it under "Changing the model for one task while it is
running".
`task.model` is profile-only: a repository must not be able to send your work and your
credit to a model you never picked. Blank means the conversation's own model.

**This is a promise about the task, not about everything under it.** A task can start work
of its own, and each of those settles its own model when *it* is admitted — the only id
anybody can be told up front is the one the task you asked for is running on. The one
thing that can move a task off its frozen id is a model with **no tool use**: a task
cannot run without tools, so aforge swaps once to the small-work class and says so on the
row — `model <id> has no tools; using <other>` — and from then on the row names the model
it is really on.

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
stops being paced when the last of them gets through — or when its patience runs out, which
the next section spells.

`task.parallel`, `task.max_load` and `task.min_free_mb` are all profile-only settings.

## Rate limited — why work waits, how long it waits, and why things stay slow after

When the provider answers **too many requests**, that is not a failure. It is the provider
saying *not yet*, and aforge waits rather than throwing the work away.

**What the wait looks like.** The first retry comes after about **0.7 seconds**, and each
one after that doubles — but no single wait is ever longer than **one minute**, whatever
asked for it. If the provider sent its own comeback time, that time is used instead when
it is longer, still under the one-minute ceiling. A task whose call is waiting shows
`waiting · rate limited` on its row for as long as it is held.

**How long patience lasts.** Two clocks, and whichever runs out first ends the call:

| Whose call | Attempts | Time spent waiting |
| --- | --- | --- |
| your conversation's turn | 6 | 2 minutes |
| a task's own calls | 60 | 10 minutes |

A turn you are watching gives up sooner on purpose: an error you can act on beats a cursor
that never comes back. A task waits far longer because nobody is sitting in front of it and
a working copy of real work is behind it — but it does give up in the end. Ten unbroken
minutes of pacing is not a burst; it is an account that cannot serve the work right now,
and a task that says so is more use than one that sits. When patience runs out the call
fails with the provider's own words, the turn is retried three more times as any provider
failure is, and then it surfaces as a failure like any other. A request that was **cut**
rather than refused — a model that went quiet, a reply that came apart — has a budget of
its own and does not spend any of those three (see *Models, context, and what it costs*).

**Routing around a full pool.** Some *too many requests* answers name which upstream
provider's pool is full — one machine room out of the several that can serve the same
model. When that happens, aforge remembers the name and asks the router to route new
calls around that provider for the next five minutes (or for the comeback time it named,
if shorter), so fresh work lands on machines with room instead of queueing behind the
full one. The call that drew the answer still waits its own wait — only calls sent after
it steer around. A model served by a single provider has nowhere else to go, and simply
waits as described above.

**Why things can stay slow afterwards.** aforge watches how many calls the provider will
take at once and pulls that number in half when it is told *too many requests* — once per
burst, not once per answer. It gives it back on the clock: after **20 seconds** with no
further pacing, one call's worth returns every **5 seconds** until it is back where it
started. So a burst costs a few minutes of reduced throughput, not the rest of the session.
This matters most when several aforge windows share one API key: the pacing one of them
causes is charged to all of them, and without the healing every window would ratchet down
and stay there.

## Does a task survive a restart?

The graph survives. The running work does not.

When a session comes back:

- tasks that were **done**, **failed**, **needing your look** or **queued** come back
  exactly as they were, with their leavings intact;
- a task that was **running** comes back **queued** and marked interrupted, and it is
  resumed once — a process exit pauses work, it does not make a finding about it. Its
  report says where its work is:
  - `paused — it resumes; branch task/… kept` — plus `, its worktree is at <dir>` when the
    directory is still there. The branch is checked in the repository first;
  - `paused — its previous branch task/… is gone, so it resumes in a fresh working copy`;
  - `paused — it resumes`, when it had not got as far as a working copy;
  - `paused — it resumes; whatever it wrote is in your tree`, when it worked directly in
    the workspace;
- **a sub-harness design that was still being written is the exception: it does not
  resume.** It comes back **failed**, saying `the design did not finish before aforge
  closed; nothing was saved`, and it is never handed to an ordinary worker. Nothing reaches
  the harness registry until you approve the card, so an unfinished design left nothing
  behind to pick up — ask for it again and it is designed from the start;
- then the queue is turned again: a queued task whose prerequisites are still done starts
  now.

You see one line about it, as context for your first turn rather than as a reason to start
one:

```
recovered task graph: 2 done · 1 interrupted (branch task/fix-it-9c1a2f kept) · 1 waiting
```

The counts are done, failed, needing a look, interrupted, designs that did not finish, and
waiting. A design's own clause is `1 design did not finish (nothing saved)`. The branch
clause reads `no branch kept`, `branch X kept` or `branches X, Y kept`. Any completion notes
that were never delivered appear underneath.

**An adaptive run is not a task and does not come back at all** — it has no checkpoint.
What comes back is its row in the project's list, closed with `incomplete — aforge closed
while this was still running`. See *Adaptive runs*.

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
been noted or was interrupted.

**What each node cost is in it too** — the money, tokens in and out, cache read and cache
write — so a conversation reopened tomorrow still shows what every task spent. Nothing is
invented on the way back in: each figure was written down when the node settled, and the
requests behind it are the `usage` lines in that node's own task transcript, which you can
open and read. The assembled brief is not in it; it is rebuilt from the prerequisites'
reports when a task starts.

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
of the outcome, file count, cost, the model it ran on, its tokens in and out as one sum,
duration, when it ended, the session id, and two URIs — where the work is and where the
transcript is. Never the content: it is an index, not an archive. A read keeps the newest
2000 rows.

An adaptive run writes **two** rows for the run itself: one when it starts, saying only
that it is running, and one when it ends, carrying the whole tank it spent, the planner's
model and how it finished. Rows are never edited — the newest row for an id is the one
that counts — so the closing row's cost minus its nodes' costs is what the planning and
the closing write-up cost on their own.

**Task transcripts**, which are real, resumable session files you can open with `read`:

```
~/.aforge/v3/tasks/<session id>/<YYYYMMDD-HHMMSS>_<task id>.jsonl
```

Beside each one sit `…_<id>-audit-<6 hex>.jsonl` per check and `…_<id>-repair1.jsonl` per
correction round. A task's own pointer stays on the **first** file — the run that is the
task itself. A conversation that has a session folder keeps them inside it instead, under
`<session folder>/tasks/`, and that pointer is written on the conversation's task
checkpoint, which is what lets a finished task's room replay its transcript after a
restart.

## What happens when a task fails

Endings are checked in a fixed order, and the first match wins:

| # | What happened | The report |
| --- | --- | --- |
| 1 | No working copy could be made | `could not prepare a working copy: <err>` |
| 2 | The worker would not start | `could not start the task: <err>` |
| 3 | A step limit fired **and the work did not hold when it was checked** | `stopped: 200 steps and no finish` or `stopped: 6 steps without progress` |
| 4 | The checkpoints ran out | `ran out of time` |
| 5 | You stopped it (`jobs kill`) | `stopped before it finished` |
| 5b | The session closed or detached | paused — it resumes, it is not failed. A sub-harness **design** is the exception: `the design did not finish before aforge closed; nothing was saved` |
| 6 | The run errored | `it ended with an error: <err>` |
| 7 | Stopped while its work was being looked at | `stopped while its work was being checked` |
| 8 | Nobody could say | `finished, but needs your look — …` |
| 8b | The work held, and a file it wrote changed elsewhere while it ran | `finished, but needs your look — "…" changed <path> while this ran` |
| 9 | Gaps left after the correction rounds | `incomplete — …` |
| 10 | Otherwise | done: the evidence first, then the task's words |

Row 3 is checked before it is written down: a task that hit a limit is judged against its
acceptance one more time, and if the work holds it lands **done** at row 10 instead, merged,
with no `stopped:` line anywhere in the report.

**Row 6 gets one more go, on a different model.** When the worker ended because the
*provider* could not answer — nothing serving the model would take the request, an account
limit, a set of retries the provider never cleared — the task has learned nothing about the
work, and the working copy it prepared is the expensive part. So it **runs again once**, in
the same working copy, on the same brief, on the next model in your `fallback models` row.
Its row says `model <first> could not answer; using <second>` while it runs, and the report
says it either way it ends:

```
openai/gpt-5 stopped answering, so this ran again on openai/gpt-5-mini
```

Three things it deliberately does not do. It never moves for a **tool** that failed — a
failed call is a result the worker reads and goes on from, and it never ends a task. It
never moves for work that is merely **incomplete** — that is the check's verdict, and
re-rolling a model on it would be guessing at the answer. And it never moves for a reply
that kept **going quiet**, because that turn already moved to another model on its own (see
*Models, context, and what it costs*) and doing it again would spend a whole second worker
learning the same thing.

**Once per task.** The second failure is real, and the report names both models. With no
chain to move to — or under `--one-model` — the task fails on the error it always failed on.
The id the task was **admitted** with is not overwritten by any of this: a rescue is not a
choice somebody made, and picking a model yourself inside the task's own room still outranks
it and clears the line.

In rows 3 to 7 the task's **own last words are kept underneath** the one-line reason, and
the branch is kept — with the work committed onto it. A task that was stopped mid-flight
still hands over the files it produced: they are listed under `changed:` and the branch is
offered for you to merge. What is never done for you is the merge itself, because only work
that was checked reaches your branch.

What you read on a failure is `task 7 failed: <title>`, the report, the changed files, and
the line saying the branch was kept. Anything waiting on that task fails with it — the
cascade walks one layer per scheduling pass.

**This table is about tasks that do work in a working copy.** A `harness` task — a
sub-harness being designed — has none of that machinery and its own short list of endings
instead: it can run out of time only while the page is being *written*, and a card left
unanswered settles it **done** rather than failed. Ask the manual about designing a harness
for that list.

## What propose_task needs from you

`propose_task` is how the model moves a self-contained piece of work out of the
conversation. You cannot call it yourself — you ask for the work, and the model grooms it.

Five arguments are **required**:

| Argument | What it is |
| --- | --- |
| `title` | One line naming the work, as you would say it |
| `summary` | Two or three lines you read to decide whether to redirect it |
| `brief` | The work itself: files, symbols, conventions, what has been tried |
| `deliverable` | What must **exist** when it is over, and where: the file and its path, the branch, the answer and its shape |
| `acceptance` | The observable done-condition: the command that must pass, the behaviour that must hold, the output that must appear |

The same `acceptance` string is what the second look judges against — one text, two
readers. So a vague acceptance costs twice.

A missing argument comes back as an ordinary result, never an error:
`Invalid arguments: title is required`, and the same sentence for `summary`, `brief`,
`deliverable` and `acceptance`, in that order. Unparseable JSON answers
`Invalid arguments: ` and the parse error.

There is a sixth part the model is **not** asked for and cannot leave out: your own
message. See the next section.

## What the task actually reads — does it see what I said?

Yes. Your own message travels with the work, word for word.

A task's first and only message is assembled by aforge from four parts, under headings, in
this order:

```
WHAT THE PERSON ASKED FOR, IN THEIR OWN WORDS
This is the message this work came out of. Where anything below reads
differently from it, their words are what was asked for.

<what you typed, verbatim>

THE WORK
<the model's brief, plus what any task it waits on learned>

WHAT TO PRODUCE
<the deliverable>

DONE WHEN
<the acceptance>
```

The first part is taken by aforge from the conversation — the message that was in front of
the model when it proposed the work, or the newest thing you typed into that turn if you
steered it. The model never writes that part and cannot edit it. Nothing else from the
conversation travels: the task does not see the discussion around your message, and it
cannot ask you anything once it starts.

**A part with nothing in it gets no heading.** A task you wrote yourself with `/task` has no
separate deliverable, so it reads as your words, the work and a done-condition. A task
restored from a checkpoint written before this existed has no verbatim part at all.

**For a `/task` the THE WORK part is your brief after shaping**, not a model's paraphrase of
a conversation: your sentence with the constraints and decisions written around it, from the
pass described on the *work that runs on its own* page under *Why my task's brief is longer
than what I typed*. Where shaping could not run, the two parts are the same sentence and it
is printed once — under your own heading, with no THE WORK at all.

Long messages are cut at 6000 bytes and the cut is marked with `…`, so a task that was
handed a shortened version of what you said can see that it was.

A task the model hands out from **inside** another task inherits the same words: there is
nobody in a worktree to type a new message, so the sentence that started the family is what
every task under it reads.

Once a task is admitted, its brief and its acceptance are **frozen**. Nothing changes them
after that — not steering, not a correction round. Steering is talk to the worker, not a
new target. If the objective itself was wrong, the answer is a new proposal.

## The optional arguments on propose_task

Four more arguments, all optional. Every bad value is an ordinary result, not an error.

**`depends_on`** — an array of task ids that must finish first. The task waits for them,
and their reports are put in front of it when it starts. Ids can only point backwards,
and only ids `propose_task` itself returned count: a job or adaptive-run number is a
different kind of work, and naming one — or a task that already failed — refuses the
proposal on the spot instead of queueing work that could never start.

**`model`** — which model this task runs on. Set only when you asked for a particular model
or class of model for this work. Left out, the task runs on `task.model` if set, otherwise
on whatever model the conversation is on at the moment of admission — settled once, then
frozen for the task's whole life.

**`max_steps`** — how many finished tool calls make one checkpoint. Default **200**. At a
checkpoint a second look at the evidence decides: progress buys another 200 (up to 1000 in
all), circling gets a landing turn — the task writes the deliverable from what it has —
and only then a stop with `stopped: 200 steps and no finish`, the number being the
checkpoint that was in force. A negative value answers `Invalid arguments: max_steps cannot
be negative`. Zero or absent means the default.

**`no_progress`** — how many tool calls in a row may teach nothing, save nothing and leave
nothing new in the worktree before the task is stopped as spinning. Default **6**. On the
limit the report is `stopped: 6 steps without progress`, the landing turn runs, and what
the task made is committed onto its kept branch. A negative value answers
`Invalid arguments: no_progress cannot be negative`.

Both step limits are recorded in the checkpoint, so they survive a restart along with the
rest of the task.
