# How work on its own actually runs

## What happens when I type /task — the forming line is stuck, the task spinner is not moving, nothing happens after /task

`/task <brief>` first raises one forming block at the transcript tail. Its dim `▏ `
hairline joins the word `task`, your verbatim quoted brief, and the live phase. The phase
begins as `sizing it up…` for the plain sized form and advances in place to
`shaping the brief…`; `/task solo` and the preset `single` road skip straight to
shaping. The spinner and count-up keep moving on the same frame clock as the
other live rows — the block starts that clock itself, so it turns even when the
conversation is otherwise idle, which is every time you type `/task`. In the plain-text
tier the mark is a still `*` on purpose and only the clock climbs. When the start
succeeds, the block is replaced in that same frame by
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

By default, no. Each code task gets its own checkout of the repository the conversation is
about, on its own branch, so you can keep working in yours while it runs. If your request
explicitly names another folder, the task works in that exact folder instead; its card and
its `/history` record show the resolved `where`.

aforge runs `git worktree add -b <branch> <dir> HEAD` off your **current HEAD**.

- **Directory:** `<session folder>/trees/<task id>`. The task folder is the task's home,
  while the worktree is registered in the repository it was cut from.
- **Branch:** `task/<title slugified, at most 32 characters>-<6 hex>` — for example
  `task/fix-the-nil-map-crash-9c1a2f`. The random tail lets the same title be proposed
  twice.

The worktree lives inside the conversation's session folder. You will see its registration
in the repository's `git worktree list`, but aforge puts no task directory in your repo.

If a directory is already at that name it can only be this session's own dead run, so it is
removed with `git worktree remove --force`, pruned and deleted before the add.

Two limits:

- An explicitly named folder, or a task shaped as non-code work with `where: in place`,
  runs **in place** in that directory and says so:
  `it worked directly in the workspace: there was no repository to branch`.
  While that task runs, **the chat cannot write in that directory** — see *A task working
  in place holds the directory* below.
- A failed `git worktree add` fails the task with
  `could not prepare a working copy: git worktree add: <first line of git output>`

## A task in a conversation with no project — task failed saying it needs a project, task in a conversation with no folder, do tasks work without a repository

They work. A conversation opened where there is no project — your home directory, a temp
folder, a launcher; the place line reads `aforge` — has a **workspace of its own**, and
aforge quietly makes that workspace a git repository the moment the conversation opens.

So a task with no named place takes the ordinary road described above, against that
repository instead of a project's: a worktree at `<session folder>/trees/<task id>`, a
branch `task/<title>-<6 hex>` off its HEAD, and a merge home when the task lands. Work that
needs no repository at all — filing an issue with `gh`, reading something, writing a
document — simply runs, and its card and `/history` record name the task folder it stood
in.

Nothing has to be named first. `/workspace <path>` still anchors the conversation to a real
repository when that is what you meant, and naming a folder in the request still sends that
one task there.

Two things follow from it:

- The worker is told where it is standing, in one line of its instructions:
  `There is no project here: this is the conversation's own space, and it holds only what
  this conversation has put there.` A task folder holding nothing is the ordinary state of
  a conversation that never had a project, and the line is what stops a worker reading it
  as a checkout that failed.
- If that workspace is **not** a repository — a conversation from an older aforge, or a
  machine with no `git` — the task runs **in place** in it and says so, exactly as any
  other non-repository does. It is never refused for want of a project.

An older aforge stopped such a task with `this task needs a project; use /workspace <path>
or name where it should work`. Nothing says that any more.

That scratch workspace is `work/` **inside the conversation's own session folder**, so anything
made there is inside the conversation and deleting the conversation deletes it. The path and the
three ways to keep the work are on the starting-aforge page, under *Where do task files go when I
did not open a project*.

## Does a task see my unsaved changes — it worked on an old version of the file

No. The task's checkout is cut from your **last commit**, so an edit sitting uncommitted in
your working copy does not travel with it. That is the same isolation that lets you keep
typing while it runs, and it is also how a task ends up reporting a file as it was this
morning: it was reading the committed version, and it was right about that version.

Aforge says so before it spends anything. When you start a task and your working copy has
uncommitted changes, one line goes into the chat with the brief:

```
your unsaved edits stay here · the task works from the last commit
```

It is a **note and not a gate** — the task starts on the very next breath and nothing waits
for you. It is said once per task you start, and it is not said at all when your working
copy is clean or when the conversation is not in a repository, because there would be
nothing to tell you.

**New files you have never committed are not counted.** Build output and scratch files
would otherwise make the line appear on every single start, and a line that always appears
is a line nobody reads. They are just as invisible to the task, so a brand-new file the task
needs is one to commit — or to name in the brief, so the worker makes it itself.

**If you want the task to have your changes, commit them first**, then start it. There is no
flag that sends a dirty working copy; the checkout is `git worktree add … HEAD` and HEAD is
what it gets.

The one task that does see your unsaved edits is a task **running in place** — a named
folder, `where: in place`, or a workspace that is no repository at all. There is only one
directory in that case, which is why such a task holds it while it runs (below).

## A task working in place holds the directory — nothing was written, a task is using this working copy, I cannot edit a file while a task runs

When a task got a checkout of its own, you and it are in different directories and nothing
either of you writes can reach the other. **When a task is running in place there is only
one directory**, and two writers in one directory do not produce either person's work.

So a task running in place **holds that directory for as long as it runs**, including
while its work is being checked and repaired. Anything else that tries to write a file
there — this conversation, one of the hands inside a reply, another task — is refused
before the write happens, and told who has it:

```
src/analysis.rs is in the working copy task 4 (repair the parser) is using right now, so
nothing was written. That work is writing there until it finishes — wait for its report and
make this change on top of what it did, or change something outside /workspace/rust-java-lsp.
```

Three things this does **not** stop:

- **Reading.** Everything can still read every file in there. The hold is on writing only.
- **The task's own family.** The task itself, its sub-tasks, and the hands its worker forks
  are all that task writing, and they are never refused.
- **`bash`.** A shell command's effects are whatever the command did, so a command that
  writes is not caught. Only `write` and `edit` — the two hands whose file is known before
  they run — are held back.

The hold ends the moment the task does: it lands, fails, is stopped, or the process closes,
and the next write goes straight through. Nothing has to be released and there is nothing to
clear by hand.

**Two tasks cannot both run in place in one directory.** Whichever started first has it;
the second is refused its writes and told which task to wait for. When the first lands, the
second gets the directory.

## What a finished task brings home, and what it leaves behind

A task lands **the files it wrote** — every path it handed to its `write` or `edit` hand,
plus anything its own report names on a `files:` line. Nothing else is committed and
nothing else merges.

That is why a task's branch is a change you can read. Its checkout is its own to make a
mess in: it installs what your tests need, it builds, it caches. A `.venv`, a
`node_modules`, a `target/`, a `.pytest_cache`, a downloaded model — none of those is
something the task wrote, so none of them reaches your branch. Tasks used to land with the
virtualenv attached and the actual change buried inside it.

What it leaves behind is named in the landing, and the sentence says where it went:

`it left files it did not write, and they went with its working copy rather than onto your branch: .venv/bin/activate, .venv/pyvenv.cfg and 812 more`

A task's checkout is removed once its work is merged, and the leavings go with it — that is
what a throwaway checkout is for. When the branch is kept instead, its Git worktree is
unregistered but the ordinary task folder and leavings stay, and the sentence reads `they
are still in its task folder rather than on its branch`. The first few files are named and
the rest are counted. Your `.gitignore` is respected exactly as it always was: a path your
repository ignores is not committed and is not mentioned.

**When a command made the deliverable.** A task that runs a scaffold, a code generator or a
formatter produces real files it never typed. It brings them home by naming them on the last
line of its report — `files: site/index.html, site/app.css` — and only names that really
exist in its checkout are believed. A task that says nothing about them has left them
behind, and that is the difference between a deliverable and a dropping.

## My task's branch would not merge — what happens then

Nothing is forced onto your branch. A merge that hits a conflict is **abandoned** and your
checkout is put back exactly as it was: no `<<<<<<<` markers in your files, no half-finished
merge to get yourself out of.

The task then lands as **needs your look** rather than finished, and the report names the
files that changed on both sides:

`finished, but needs your look — its branch task/edit-the-parser-9c1a2f did not merge cleanly and was kept: internal/auth/session.go changed on both sides`

The work is committed on that branch, so `git merge task/…` is a real offer whenever you are
ready to reconcile the two versions. Nothing waiting on the task fails — it waits until you
decide, exactly as with any other task that needs a look.

Pressing **accept** on the card does not change this. Accepting says the work is good, and
it is; it cannot make two versions of one file into one, so an accept whose merge conflicts
leaves the task needing your look with the same sentence.

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

**And it can use hands.** `fork` is on a task's belt for the same reason it is on the
conversation's: a task worker is a mind in the middle of its own work, so it can copy
itself into two to four hands inside one of its own turns, in its own copy of the
repository, and stitch what they bring back. The tasks page has the whole of it, under
*Hands*. The one agent that cannot fork is a hand — the fork is one level deep, and a hand
simply does not have the tool.

Approval inside a task is allow-everything, with the critical floor still under it (things
like `rm -rf /`, `mkfs`, redirecting onto a raw disk, shutdown). When a call hits that
floor there is nobody to ask, so the task reads the refusal
`refused in a task: <rule> — nobody to ask` and keeps working. `use_service` cannot
connect a new account inside a task.

A task is also a job. It shows in `jobs list` labelled `task 7` with the title as detail,
`jobs kill` ends it exactly as a time limit does, and closing the session kills every
running task. Its step-by-step log is the job log, at
`<session folder>/logs/jobs/<job id>.log`.

**A task's own droppings are kept with the conversation, never in the checkout it works
in.** The job log above, and the bytes of any long tool result lifted out of a worker's
live context to save room, both land under the conversation's `logs/` — the same place the
conversation's own do. A worker reads a lot of files, and none of what the harness keeps
about that reading is your work: nothing of aforge's is written into your repository or
into the task's copy of it. Only a conversation with no folder at all falls back to
`<workspace>/.aforge-v3/`.

## How a task is told to spend its time — the measure, a zero, and not remaking what exists

Beyond the tools, a task is given three working habits in its instructions. They are
written as principles rather than examples, because aforge hands tasks prose, research,
data, operations and code through the same door and a habit written in one trade's words
is a habit that is wrong for the next job. **The chat is given the same three, in the
same words** — see *What aforge can do for you* — because the approach to a piece of work
is usually chosen in the conversation, before the task exists.

- **When the work comes with its own measure, that measure is the loop, not the report.**
  A check to run, a count to reach, a reading somebody will take — the task is told to
  work *between* readings rather than saving the reading for the end, and that the
  interval between two of them **shrinks when the reading is zero** rather than growing.
  A result it cannot explain is the moment to take the reading more often and change less
  in between. Anything changed but never measured is called what it is: a guess.
- **Nothing on every count is one shared fault, not many separate ones.** Parts that do
  not depend on each other do not all fail at once by coincidence, so a task reading zero
  everywhere is told to find what they have in common — how they are reached, where they
  are looked for, the step before any of them runs — and to prove that shared path carries
  one case end to end **before** it touches any single part. Uneven readings say the
  opposite, and there it starts with the worst one.
- **Before making a thing itself, it spends one step asking whether it already exists** in
  a form it can use: a tool, a source, a service, something the work already carries,
  something done here before. Asking costs one step; not asking costs the whole thing.
  And it asks *before* the first piece exists, because the answer stops being welcome once
  there is something to be attached to.

These came out of two unattended runs of the same brief measured against each other: the
one that hunted for what every zero had in common was off zero eleven minutes later, and
the one that answered the same zero by reading its own work spent most of its calls
changing things it had never measured — and built from scratch something that already
existed, without ever spending the one step it would have cost to ask.

## Where a task may write — its own copy, and nowhere else on the machine

A task **works in one directory** and may **write only there**. That directory is its own
copy of the repository — a worktree cut from yours, or your folder itself when there is no
repository. Everywhere else on the machine it may **read as much as it likes** and change
nothing.

**Reading anywhere is the point.** A task briefed about a repository it is not standing in
still has to look at it: `read`, `grep`, `ls`, `cat`, and `git log`, `git show`, `git diff`,
`git status`, `git branch -a`, `git remote -v` against **any** repository on the machine all
run normally. Looking has never been what goes wrong.

**Writing anywhere else is refused before it runs**, and the task reads the refusal and
carries on. It covers every hand that names its target:

| Aimed outside its copy | What it looks like |
| --- | --- |
| `write` and `edit` | any path outside the task's own copy |
| a shell command that moved first | `cd ~/code/yours && git checkout -b fix`, `cd ~/code/yours && mkdir -p src` |
| git pointed somewhere else | `git -C ~/code/yours add .`, `GIT_DIR=~/code/yours/.git git update-ref …`, `--work-tree=` |
| the file hands | `cp`, `mv`, `rm`, `mkdir`, `touch`, `tee`, `chmod`, `sed -i`, `patch` |
| a redirection | `echo x > ~/code/yours/NOTES.md` |

The wording it reads names **both the path and the directory it may write in**:

> /Users/you/code/yours/NOTES.md is outside your copy — this task works in
> /Users/you/.aforge/v3/projects/…/trees/1; read anywhere, write only there. Say what needs
> changing out there in your report; what you write in your copy comes home on its own.

**The machine's scratch is not yours.** `/tmp`, the temp directory and `/dev/null` are
written freely — that is where a command line puts what it is about to read back.

**What it cannot see.** It reads the paths a command *names*. `cd elsewhere && python
fix.py`, where the script writes what it likes, is not caught, and neither is a path built
out of a shell variable. What it does close is every shape that says its target out loud.

**Why it exists.** A conversation opened in a home directory handed out two tasks whose
brief named a repository somewhere else by its full path. Both worked in that live checkout,
were refused with a sentence about "your own copy" that was false about the path they had
named, and went around it three ways — plumbing commands, then `GIT_DIR=`, then the forge's
own API. Two commits landed on a branch of the person's repository from work nobody had
approved landing there.

## Can a task push, or open a pull request? No — its work comes home through its landing

**`git push` is refused**, wherever the task is standing, and so is anything that changes a
project on GitHub, GitLab or another host:

| Refused | |
| --- | --- |
| `git push` (any remote, any branch) | |
| `gh api` carrying a change — `-X POST/PUT/PATCH/DELETE`, or any `-f`, `-F`, `--input` | |
| `gh pr create`, `gh pr merge`, `gh issue create`, `gh release create`, and the rest that write | |
| `curl`/`wget` posting to a host that hosts repositories | |

> git push is not yours to run: This task's work comes home through its landing, and a pull
> request is the person's or the conversation's to open — say what you want in it in your
> report.

**Reading the host is untouched**: `gh pr list`, `gh pr view`, `gh pr diff`, `gh issue view`,
`gh run list` and `gh api` GETs are how a task finds out what it is fixing.

**So how does the work get to you?** Every path the task passed to `write` or `edit` is
staged by name and merged home onto your branch when the task lands — that is the road, and
it is the only one. If the work should become a pull request, the task says so in its
report and you or the conversation opens it.

## What git a task may run — merge, pull, checkout, stash, reset are refused

A task works in **its own copy of the repository**, and its copy shares the repository's
object store with yours: every branch you have is visible from inside it. So the line
aforge draws around a task's `git` is about **whose work it may take**, not about which
directory it is standing in.

**It may read anything.** `git status`, `git diff`, `git log`, `git show`, `git branch
--list`, `git rev-parse`, `git merge-base` — against any branch, including `main` and any
other task's branch. Knowing what is around it is how it does the work.

**It does not have to save anything.** What lands on your branch is every path the task
passed to `write` or `edit`, staged by name on the way home — the task is told not to stage
its own work, and `git add` and `git commit` are neither needed nor refused.

**It may not move its copy onto work it did not do, and may not reach a remote.** These are
refused before they run, and the task reads the refusal and keeps working:

| Refused | Because |
| --- | --- |
| `merge`, `rebase`, `cherry-pick`, `revert`, `checkout`, `switch`, `am`, `apply`, `worktree`, `update-ref` | they put somebody else's commits into the task's copy, and only what the task writes there comes home |
| `pull`, `fetch`, `clone`, `remote`, `submodule` | they bring in work the task did not do, and a task reports what it writes as its own |
| `push` | a task's work comes home through its landing, not over a remote (the section above) |
| `stash`, `stash pop`, `stash apply` | a stash that will not go back cleanly leaves raw conflict markers in files nobody looks at again (`git stash list` and `git stash show` are fine) |
| `reset --hard`, `--merge`, `--keep`, and `restore --source` | they throw the working copy away or fetch a file off another branch (plain `git reset` to unstage, and `git restore <path>`, are fine) |

The wording it reads names the verb and what it may do instead, for example:

> git merge is not yours to run: it would put work this task did not do into your copy, and
> only what you write here comes home. Look with git status, diff, log and show — any
> branch, as much as you want. What you write with write and edit in this copy comes home
> on its own.

**These sentences are for the task's OWN copy, and are never said about anywhere else.** A
command aimed at another directory is answered by the path law above instead — "outside your
copy" — because "this is your own copy" is false about a repository the task is not standing
in, and a refusal a model can see through is a refusal it goes around.

**None of this applies to you.** In your own conversation, in your own checkout, aforge
runs whatever git you ask for. The rule exists because a task reports work as *its own*,
and one that fast-forwarded onto `main` really did report somebody else's fixes as the
thing it had just built.

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

When a task's work does come home, the paths it wrote are staged by name — never
`git add -A`, and never `.aforge-v3` — then committed on its own branch as
`task: <first line of title, at most 72 chars>` with the identity
`aforge <aforge@localhost>`, then merged into your branch with `git merge --no-edit`. The
merge is attempted whatever your tree looks like — a dirty checkout is normal. On success
the worktree is removed and the branch is deleted. A merge that conflicts is abandoned, the
branch is kept, the task worktree is unregistered, and the task needs your look. Two tasks finishing at
once are serialized, so a merge is never lost.

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

**That reply is priced exactly like one you typed.** It climbs the same three points, has the
same ceiling, and is handed to a task the same way — see *An answer that runs long is read and
moved* in *Tasks*. It used to be exempt, and a measured run had one such reply grind for 46
minutes with nobody watching and then leave the session idle for seven and a half hours.

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

**What the landing turn may still do.** It keeps only the tools that SAVE something
before the call returns: `write`, `edit`, and — when the task had them — `generate_image`
and `speak`. Everything else comes off, and the instruction names exactly the hands it
kept, so a task whose deliverable is a picture or a voiceover can still produce it.
`generate_video` and `generate_music` are **not** kept, even by a task that had them: they
answer with a background job and land minutes later, and the task is closed the moment
its landing turn ends — a render started there would be stopped before the file existed.
Reading, searching and running commands are gone for that turn too: it is a turn for
finishing, not for one more look.

**Five minutes for a check.** Each second look at finished work is bounded at 5 minutes.
It hangs off the task's own clock, so `jobs kill` ends it too. A check that burned its
whole five minutes is not retried.

There are two step limits as well, and they work the same way — checkpoints, not killers:

| Limit | Per checkpoint | Backstop | Report when it finally stops |
| --- | --- | --- | --- |
| `max_steps` — finished tool calls | 200 | 1000 (200 × 5) | `stopped: 200 steps and no finish` |
| `no_progress` — calls in a row that teach nothing, ask nothing new, save nothing and leave nothing new in the worktree | 6 | 6 (this one fires) | `stopped: 6 steps without progress` |

At a `max_steps` checkpoint the same second look runs: progress buys another 200 steps, up
to the 1000-step backstop. Whatever stops the work, the landing turn runs first — the task
writes up what it has — so nothing is ever lost mid-flight. And a task stopped this way is
still checked against its acceptance afterwards: if the work holds it lands finished and
merges, and the `stopped:` line never reaches you.

## What "bringing the work home" tells the task, and why a tool says it was withdrawn

When the landing turn takes a tool away, a task that reaches for it anyway is not told the
tool is unknown. It is told it was **withdrawn**, and the answer carries three things: why
it is gone (`bash was withdrawn from your tools: the work is being brought home`), the
**exact list of what it still has** by name, and what to do with them — finish what it is
saving and stop, because calling it again cannot bring it back and there is nothing left to
run or poll.

This is the difference between an eighteen-byte `Unknown tool: bash` and a sentence. One
really happened: a task lost `bash`, `read` and `grep` when it was landed, was answered
`Unknown tool` eight times, retried each call because nothing told it the hand was gone for
good, and worked out what had happened only in its very last words. A name that was **never**
on the belt still answers `Unknown tool: <name>` — that one is a genuine mistake by the
model, and the two are deliberately worded differently.

**Nothing the harness refuses is counted against the task.** A withdrawn tool and a call a
permission rule turned down are the harness's own answers, not the task working badly: they
never advance the no-progress counter, never reset it, and never earn a `[stuck]` note. They
are still steps, they still cost, and they are still in the task's transcript.

**Files saved after a withdrawal are reported as unverified.** If a task had been running
its work with `bash`, lost it to the landing turn, and then saved something anyway, its
report says `incomplete — nothing checked the files it saved after its tools were withdrawn
— they were never built or run`. It stands at the head of the report, directly under the
limit that fired, so nobody reading it — you, or the conversation that started the task —
takes those last edits for finished work. It says nothing about whether they are right,
only that nothing looked at them.

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
something. Anything under `.aforge-v3` is excluded: what the harness leaves there is not
the task's work. Job logs and stubbed tool results are not even there any more — they are
kept with the conversation, under its own `logs/` — and the exclusion stays as the floor
under everything else aforge may leave in a checkout.

**It learned something.** A read-only call — `read`, `read_document`, `ls`, `grep`, `find`,
`web_search`, `web_fetch`, `jobs`, `recall`, `view_image`, `manual`, `tasks`, `settings`,
`list_harnesses`, `services`, `gmail_read`, `gmail_search`, `calendar_list`, `slack_search`,
`slack_read_thread`, `slack_list_channels` — or a `bash`, **whose answer was more new than
old**. A failed one still counts as learning: finding out that something does not work is
finding something out.

"Whose answer was more new than old" is measured **line by line, not result by result**.
aforge remembers the lines a task has already been shown, and counts how many of a
result's lines are ones it has never been given. Nothing is stripped out or excused first:
a line is the same line, or it is not.

The reason is a shape that looks like work and is not. A task re-runs its own check
against something it has stopped changing; the check prints the time it started, so every
run comes back with one new line in sixteen and fifteen the task already had. Counting
whole results, that is six discoveries in a row and the task can spin for hours. Counting
lines, it is what it is — six per cent new — and the counter fires.

**But that count only decides a re-measurement, and two other things count on their own.**
Half a rule was landing tasks in the middle of real work, so the whole rule is:

- **A reading taken over work that has just changed is information whatever it says.** Edit
  a file, rebuild, run the check: the build reprints the same warnings and the check
  reprints the same table with three numbers moved. Almost nothing in either is new, and
  both told the task something — it measured a state that had never existed before, and
  finding out that an edit moved little is finding something out. One reading gets this;
  the second re-run of an unchanged check is a re-measurement again.
- **A question the task has never asked, whose answer brought something back, is
  information.** Pulling six different records out of a corpus of pretty-printed JSON gives
  six answers that are mostly `    {` and `  }` — twenty per cent new lines at best — and
  the task is learning six things it did not know. What is *not* information is a new
  question whose answer holds nothing new at all: nine different `sleep N && tail` commands
  answered `(no output)` nine times are nine steps of nothing, and that is the counter's
  oldest catch.

So what actually fires the counter is **a step that changed nothing, asked nothing new, and
brought back almost nothing new** — the same search six times, the same failing edit
retried, a measurement re-run over work that has not moved. `note`, `forget`, `track`,
`commit` and `change_setting` are deliberately not progress: a task writing its own memory
again has not learned anything.

Failure matters for saving and not for learning. A `generate_image` that came back with an
API error saved no file, so a task calling it repeatedly and getting the same error is
stuck and is stopped — which is what the counter is for.

**A failure aforge itself produced is never counted, in either direction.** A tool that was
withdrawn from the task's belt, and a call a permission rule refused before it ran, are
answers written on this side of the wall: the tool never ran and the world never saw the
call. Those steps do not advance the counter, do not reset it, and do not earn the task a
`[stuck]` note telling it to stop repeating itself. The reason is the run that produced this
rule: a task was disarmed mid-flight, answered `Unknown tool` eight times, and was then
nudged three times for the retries the harness had just manufactured.

**A task that handed parts of its work out waits for them, and that wait is never counted
as being stuck.** While any part is still running the counter does not advance, nothing is
asked of the task, and its clock does not run: its row shows `waiting · its parts`, and the
next thing it is asked is the one turn that carries every part's report at once. The counter
starts again from zero when the last report lands, so a task that spins over the *fold* is
caught exactly as any other is. A failed part is a report too: its failure reason reaches
that same turn beside the successful reports, so the parent integrates what landed and says
what is missing or retries it. The failed part does not stop the parent, and delayed steps
from before the report landed cannot spend the fresh allowance before the parent reads it.

**A task that repeats itself is told what the work has been doing.** Before it is stopped it
gets a `[stuck]` note, and that note now carries one more fact than the repetition itself:
*the work has not changed since step 12; nine results since brought nothing new*. aforge
knows which steps changed the deliverable — the files the task's own `write`, `edit` or
generating hands saved — so it can say when that last happened and what the steps since
brought back. The same line appears in the short account a checkpoint hands to whoever
looks at the task: `work last changed: step 12 · results since: 9 · new lines since: 4%`.
The clock sentence is a description and not a rule by itself. The session loop separately
uses the same ledger for one structural rule the sentence makes visible: after five
consecutive tool rounds in which every result has no fresh line, it adds a `[stuck]` note
saying the answer is already in the transcript. Fresh information or a successful write
resets that streak. The task-level no-progress counter above remains the rule that stops an
entire task run.

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
read-only checker** is put in a clean restore of what the task wrote, runs the repository's
own checks, reads the diff, and answers. Only a pass merges.

The checker has no shared context and no memory of the work. Its whole world is the
acceptance you set, the task's own claim (labelled as a claim, not as evidence), the list
of files written, and where to look. **The brief is deliberately withheld** so it grades
the contract, not the effort.

**On work that started itself, the acceptance is your own message.** Nobody groomed a
done-condition for a task aforge started out of a reply, so what the checker is held against
is your request in full, framed as "everything asked for below is actually done — all of it,
not the part that was easiest to reach". Before that it was a generic line pointing at the
task's name, and on a long piece of work that meant a request being accepted as met the moment
the small piece the reply happened to be holding was finished.

What it may touch: `read`, `grep`, `find`, `ls`, and a `bash` restricted to an allowlist
built for **that one task** — see the next section. It cannot edit, write, install, fetch or
paint. Shell composition is refused outright: any of `; | & < > $ ( ) { }`, a backtick or a
newline in the command is turned away before the allowlist is even consulted. Every result
it reads is capped at 8000 bytes.

Before the check, new files are staged so the diff shows everything including brand-new
files. Staging happens once, so every look judges the same tree. In a workspace that is
not a repository the checker is told
`This workspace is not a repository, so there is no diff to read: check the files themselves.`

This is controlled by `task.audit`, **on by default**, and settable in your profile only.
With it off, the gate stands open, the task's own account merges, the task lands done, and
the report is marked `nothing checked this work: the task.audit setting is off` above the
task's own words. There are no correction rounds at all.

## Which commands the checker is allowed — the task's own check, not a fixed list

**The checker is allowed the checks the work itself names.** There is no list of build tools
in aforge, and no setting that holds one. The commands its `bash` will accept come from three
places, and nothing else gets through:

- **the check your task declares** — every command named in the brief or the acceptance,
  read out of the text: a span in backticks (`` `bash verify.sh` ``, `` `make check` ``) or a
  line that opens with a shell prompt (`$ ./verify --quiet`). A wildcard you wrote is
  honoured, so a brief naming `` `verify.*` `` admits `verify.sh`;
- **the check the task itself used** — any command its worker issued, taken from the same
  tool results the checker is shown, read for **the one command that line runs**. A leading
  `cd <a directory in your tree> &&` is dropped — that only states the directory the task was
  working in — and so is everything after the first pipe, along with the redirections of output
  at the end of the line (`2>&1`, `> log`). So
  `cd /workspace/thing && cargo build --release 2>&1 | tail -3` contributes
  `cargo build --release`, which the checker then runs as one command, composing nothing. A
  `cd` to somewhere outside your tree contributes nothing at all, an arrow in the middle of the
  line leaves it composed and it contributes nothing, and a command that even a
  permit-everything policy would still stop and ask about — `rm -rf /`, `shutdown`, `mkfs` —
  never becomes one either;
- **the always-safe reading commands** — `git diff`, `git log`, `git status`, `git show`,
  `pwd`, `wc`, `head`, `cat`. These print and cannot change what is being judged. They are
  not verification, so a checker holding only these can read your work but cannot exercise
  it.

A check that names no file is matched as a prefix, field by field, so `make check` admits
`make check ./...` and does not admit `make checkout`. A check that **names a file in your
tree** is matched by which file it is instead — see the next section. **Every refusal names
what this particular check is allowed**, listing the task's own check first and the reading
commands after it, so the model reads the door in the same breath as the no.

**When the work names no check and issued nothing that looks like one**, the checker is told
so in as many words, told to judge from reading and answer, and given a much shorter window —
one minute rather than five. There is no slow command for it to wait on, and the failure this
replaced was a checker spending the full five minutes reaching for a door that was never
going to open. That was measured on a Rust deliverable: the allowlist used to be a fixed set
of Go verbs plus git, so on a project that was not Go the checker could confirm nothing at
all, exhausted its five minutes on all six attempts, and every one of them landed the task
needing your look.

## How the check is spelled — one file, and the ways that really start it

**A check that names a file in your tree is allowed under every spelling that really starts
that file.** If your brief says the check is `bash verify.sh` and `verify.sh` is really there,
the checker may run it as `verify.sh`, as `./verify.sh`, by its full path, or behind **the
interpreter the file itself names** — the program on its `#!` first line, or the program that
line hands to `/usr/bin/env`. So a file beginning `#!/usr/bin/env bash` is allowed
`bash verify.sh`, one beginning `#!/usr/bin/python3` is allowed `python3 check.py`, and any
path to that same program counts. **aforge holds no list of launchers**: the file answers the
question, which is why `rm verify.sh` is not a spelling of your check. Paths are resolved
against the directory the checker stands in and compared as files, so anything that starts the
same file is the same check, and a wildcard you wrote is resolved the same way — `verify.*`
names the file it actually matches on disk.

**A file that says nothing about being run** — no `#!` line and no executable bit — gets no
program word at all. It is run **the way your work ran it**: the exact spelling your brief
declared, or the exact command its worker issued. The refusal says so, as "the check
/path/data.txt declares no interpreter; run it the way the work ran it". A file with the
executable bit but no `#!` line is allowed its own bare spellings and nothing in front of them.

**What is still refused:** a different file (`bash other.sh`), the wrong interpreter
(`python3 verify.sh` for a bash script), arguments your brief never declared
(`bash verify.sh --flag` — one word, then the file, and nothing after it), an option where the
program word should be (`bash -x verify.sh`), and anything composed (`cd x && bash verify.sh`).
Where a check can be spelled, the refusal spells it out — "the check /path/verify.sh — run it
as `/path/verify.sh` or `bash /path/verify.sh`" — and the checker is told the same thing before
it types anything.

This was measured. On a Rust deliverable the checker was handed a door naming the project's own
script and then had five spellings of that one file refused in a row — the directory stated
first, the absolute path, the bare name, the name behind a program word, the name behind `./` —
so it gave up and read source code until its window ran out.

## Where the check runs — a clean restore, not the task's messy checkout

**The check does not run where the work happened.** It runs in a **clean restore**: the
repository as it stood before the task began, with exactly the files the task wrote laid
over it, and nothing else the run left lying about. The checker installs and builds there
itself — that is what its five minutes are for.

The reason is one measured failure. A task was asked to make a scorer pass; the scorer
looked for files at a path the repository did not keep them at, and instead of changing the
source the task made the path exist with `ln -sf`. It re-ran the scorer against its own
symlink, watched it pass, and reported the job finished — and the checker, standing in the
same directory with the same symlink under it, saw the same pass. What would have landed on
your branch was a change that stops working the moment it leaves that machine.

So **a passing check may not depend on state your branch does not carry**. A fixture the
task dropped somewhere by hand, a link it made so a path would resolve, a directory it
created outside its own writes, a value it set in the environment: none of it is in the
restore, so a check leaning on it fails there and the task comes back incomplete naming what
is missing. Installs, builds and caches are exempt — the checker makes those again.

How the restore is built depends on your workspace:

- **In a repository**, it is a fresh detached checkout of the task's own branch with the
  written files laid over it and staged, so `git diff --cached` still shows the whole change.
  It sits beside the task's own checkout with `-check` on the end of the name and shows up in
  `git worktree list` while the check runs, then is removed and pruned.
- **In a workspace that is not a repository**, it is copied by the clock: everything that
  predates the task's start is the original tree, and everything younger that the task did
  not write is left out. A directory in which nothing predates the task — a `target/`, a
  `node_modules/` — is skipped whole.
- **When the restore cannot be made** — no repository and no record of when the work began,
  or a working copy of more than 20000 files — the check runs where it always did, in the
  task's own checkout, and the job log says why.

The task's own checkout is untouched by any of this, and the restore is removed as soon as
the answer is in.

## What the checker is shown of what the task already ran

The checker is also handed the **last few tool results of the task's own worker** — up to
six, each cut at 1200 bytes: what was called, with what, and what came back. It is the real
result the worker read, not a display copy.

That exists because a check is often the most expensive thing in the whole task. A checker
made to rediscover the command and run it twice from scratch spends its whole five minutes
and the task lands needing your look, which is exactly what was measured. Seeing what
already happened tells it which command the check even is.

It is **not** a shortcut to a pass. The checker is told where those results came from: in a
restore they came from the task's own copy — the one a verdict may not rest on — so they can
settle a refusal outright (a check that failed, or a check nobody ever ran, needs no second
run to be believed) while anything that could pass there and fail in the restore has to be
settled in the restore. A task that never ran a tool leaves this out of the packet entirely.

## What happens when the work is not right yet

When the second look says what is missing, the task gets a **fresh worker in the same
worktree**, the original brief, and the gaps in front of it, word for word. The worker is
asked to close the gaps and nothing else:

```
The work so far stands and is already in this working copy. Do not start it again and do not undo any of it: close the gaps above, and nothing else.
```

The worker is fresh; the worktree is not. The task stays *running* while a round is under
way, and you see one plain line of what is being closed.

**A correction round is attempted by a more capable model.** The first attempt runs on the
task's own model; when a check finds gaps, the worker sent back to close them runs on your
crew's **careful work** model — the `repair` role, in `/crew`. It is the one place aforge
spends more than you asked it to, and it is spent only after something has actually gone
wrong, on a job the check has already narrowed to named gaps in a working copy that is
already most of the way there. Two things turn it off by themselves: a crew whose careful
model is the same as the model the work is on repairs on that model and costs nothing
extra, and a task whose model **you named** — on the card or from inside its room — keeps
your model for the correction round too.

The correction worker is also handed the change as it stands: the files the first attempt
wrote, `git diff --cached --stat` over them, and the sentence that `git diff --cached`
shows the whole thing. It reads the diff rather than the repository.

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

## What the card says while a task is checked — task says checking what it left, closing gaps what does that mean, why does my task say not done under it, round 1 of 1, the task finished but the card is still busy, my task went quiet after the last line

A task has three lives and one state. Its worker writes the work; a second look reads what
the worker left; a round closes the gaps that look named. **All three are `running`** —
nothing has landed and nothing was undone between them — so the card, the rail row, the
room header and the home row say which of the three it is in:

- `checking what it left` — the worker is finished and its work is being read.
- `closing gaps · round 1 of 1` — a fresh worker is closing what the look found. The
  second number is `task.repair_rounds` (default 1), so with the default you will only
  ever see `round 1 of 1`.
- Nothing at all while the task is simply working. The row draws what it always drew: the
  call it is inside, its clock, its tokens and its spend.

**Under a round, one dim line says what was found**, in the checker's own sentence with
what happened in front of it:

```
closing gaps · round 1 of 1
not done — go test ./... reports no test files
```

That line is the reason the work is being done again. It takes the row the clock and the
spend would have had, because the clock is true every second and this is not.

**How long it can take.** Both are full model runs on your work, so minutes each is
normal — a check on a large change has been four minutes, and a round is a second worker
doing the last ten percent of the job. The whole time is on the one task's clock and the
whole cost is on the one task's bill, because you asked for one piece of work.

**If the row says nothing and the clock is still going**, the task is at its own work and
the heartbeat is the thing to read — see the heartbeat section on this page for telling a
working task from a hung one.

## The three ways a task can land

Every task ends in exactly one of three states, and the words are the same everywhere you
read them.

**Finished.** `task 7 finished: <title>`. The second look held. The branch merges into
yours, and the report leads with the task's own account of the work, with what it was
checked on under it — no lead word at all.

**Halted.** `task 7 lost the connection: <title>`, `task 7 went in circles: <title>`,
`task 7 was blocked by another task: <title>`, `task 7 ran out of steps: <title>`. Nothing was
found wrong with the work; the branch is kept and the task can be run again from it. The
rail draws these with `!` (see the section on the words under a stopped task).

**Failed.** `task 7 failed: <title>`. Somebody looked and made a finding — or a limit fired
and the work did not hold when it was checked afterwards. The branch is kept. Anything
waiting on it fails with it. A limit firing on its own is no longer enough: work that was
stopped and then held lands under *finished* above.

**Needs your look.** `task 7 needs your look: <title>`. Nobody could look, or nobody would
say — or the work held and one of the files it wrote moved under it while it ran, which is
its own section below — or the work held and its branch would not merge cleanly. The task
is neither done nor failed: nothing merges, the branch is kept, and nothing
waiting on it fails. The report leads
`finished, but needs your look — ` and then what was said, or
`finished, but needs your look — nobody could say whether it holds` when nothing was said.
The landing then says in as many words that it is neither done nor failed, that its branch
is kept, and that anything waiting on it waits until somebody decides.

The sentences you may see when nobody could say are written plainly:
`the checker could not start: <err>`, `the checker could not be asked: <err>`,
`no answer in 5m0s, so nothing was accepted`, `the checker answered neither way`. The time
in that third one is the window the check actually had — `5m0s` when it had a command to
run, `1m0s` when the work named no check and there was nothing for it to run. When two
tries in a row got nothing, the first line is prefixed
`asked twice and got no answer either time — `.

The last line of that landing is the only thing the `task.settle` setting changes. With it
on `ask` — the default — the note says the task waits until somebody decides and offers
`tasks id 7 resolve accept|reaudit|refute`, and tells aforge to say what it thinks and leave
the choice with you; the four choices on the landed card are the door. With it on `auto` the
same note tells aforge to read the report and the work and settle the task itself, and to
come back to you only when it genuinely cannot tell. Everything else in the landing is
identical either way.

**A session with nobody watching reads as `auto` whatever the row says.** `aforge --once`
and every other headless door run with no surface to raise a card on, no settings panel and
nobody to read a landing that says it is waiting on somebody — so a task that needs a look
there would stop the run for good, and that was measured stopping a ten-hour run. Such a
session takes the same road your own `[d] decide these for me` takes: aforge reads the
report and the work and settles the task itself, with the same standing escape to say it
cannot tell. It never goes the other way — a session you are sitting in front of keeps the
row you set, and a blank row still means aforge asks you.

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
- a task whose merge conflicted keeps its branch, lands as **needs your look** rather than
  finished, and names the files that changed on both sides. The note adds
  `its branch task/… did not merge cleanly and was kept — merge it yourself when you are ready`;
- a task that left files it did not write keeps them too — in its task folder, named in the
  report, never on your branch, after unregistering the Git worktree;
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

## Which workers are installed — only use the normal workers, turn off the specialist worker

A **worker** is the program that actually takes a piece of work. The **general-purpose
worker** — one agent, alone and in order, with tools — is what takes it when nothing else
is named, and it is never on any list because it is what you get when you pick nothing.
Beside it this build ships **specialists**: `bare`, the cheapest whole-taker for work that
fits in one sitting, and `swe`, an end-to-end software-engineering pipeline.

The **workers** row on the **Workspace** tab of `/settings` says which of them are
installed here. It is a list separated by commas, and **blank is all of them**, which is the default.
The same thing written by hand in your profile's `config.json`:

```json
"work.workers": "bare"
```

That line is the whole of "only use the normal workers": `bare` and the general-purpose
worker stay, `swe` is not installed at all. `AFORGE_WORKERS=bare` pins it for one launch
without touching the file, and while it is set the settings row is read-only and says so.

**A worker you leave out is absent, not refused.** It is never registered, so nothing can
reach it: it is off the list the planner chooses a worker from, work that runs out of room
cannot be escalated onto it, and `--subharness swe` answers

```
no subharness named "swe" — this build has: bare; running on the default worker
```

and does the work on the general-purpose worker anyway. Nothing dies over it and nothing
is half-done.

**A name this build does not know is said once and ignored**, on stderr as aforge starts:
`note: no worker named "reviewer" — this build has: swe, bare`. A line that names nothing
this build has — including a line that only says `linear` — leaves you the
general-purpose worker and no specialists, which is a working install and the way aforge
ran before there was a second worker.

**The chat cannot change this row for you.** Ask it to and it answers `"workers"
(work.workers) is one of the rails on what may be spent without you being asked, so it is
not mine to change. Open /settings and change it yourself.` The specialists are the
expensive way of taking a job, so putting one back is yours.

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

Four places.

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

## The heartbeat — telling a working task from a hung one, is my task still alive, why does the file keep changing

The checkpoint is written when a task is **admitted** and when it **lands**, and a task can
spend eleven minutes between those two moments. From outside the process that leaves
`"state": "running"` and nothing else, so a task calling a model every twenty seconds and a
task wedged on a build that will never return look identical.

**So every running task writes its own heartbeat**, one small file per task, beside its
transcript:

```
<session folder>/tasks/<task id>.beat.json
```

The running task's row in `tasks.json` names the file in a `beat` field, so anything that
already has the checkpoint open can find it without guessing.

It holds:

| field | what it says |
| --- | --- |
| `phase` | `working`, `checking` or `repairing` — which of the task's three lives this is |
| `request_started` | when its last model request went out |
| `request_finished` | when that request came back; earlier than `request_started` means one is in flight |
| `requests` | how many requests the task's workers, checkers and repair rounds have made between them |
| `started` | when the task itself began |
| `updated_at` | when the file was last written |

**It is written at every model request boundary** — the cadence of the work itself, not a
clock. There is no ticker, so a task that is genuinely wedged writes nothing new, and that
is the news: a `request_started` four minutes old with no finish beside it is a task inside
one long call, and a `request_finished` four minutes old with nothing since is a task inside
one long command.

**The file goes away when the task lands**, because the checkpoint's own row is the answer
from then on. A process that is killed removes nothing, so a heartbeat left behind is
believed only by its age — the same bargain the session's presence file makes.

## What the words and the ! exclamation mark under a stopped task mean — lost the connection, went in circles, out of steps, not accepted, blocked by another task

A task that did not finish keeps its branch, and the row under its name on the rail says
**why** it stopped. The same words lead the task's card. They are three kinds of news:

- `stopped — branch kept`, with a `⊘` — **you stopped it** (`x` on its room, `jobs kill`).
  Nothing is wrong with the work; it is on that branch.
- `!` and one of these — **it was halted, and nothing is known to be wrong**. The work can go
  on from its branch: say `continue task 7` or start a task that builds on that branch.
  - `lost the connection — branch kept`: the connection to the model dropped (a reset, a
    closed socket). The call was retried, and then one more worker was run on the same
    model in the same working copy; this row means both were spent.
  - `went in circles — branch kept`: the worker kept making the same calls and its own loop
    guard ended the turn (its last words are `this turn is going in circles · stopping here
    with anything remaining left undone`).
  - `blocked by another task — branch kept`: the calls it kept making were writes into a
    working copy another task holds, and every one was refused. Wait for that task's report,
    then run this one again on top of it.
  - `out of steps — branch kept`: a step, no-progress or time limit fired and the work did
    not hold when it was checked.
- `✗` and one of these — **something was found**, and the report says what:
  - `not accepted — branch kept`: the check named gaps, or you refuted it on its card.
  - `ended with an error — branch kept`: a working copy could not be made, the worker would
    not start, or an error nobody classified.

The first cause wins: a check that refuses a run which had already given up is written as
`went in circles`, because that is what happened first. A task that lands as `!` is not
`failed` in what it asks of you — it is not asking whether the work is right, it is asking
you to pick it up. A task from before these words existed reads `stopped — branch kept`
whatever ended it.

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
| 6 | The connection to the model dropped — a reset, a closed socket — after the call's own retries and one more worker on the same model | `lost the connection to the model: <err>` |
| 6b | The run errored | `it ended with an error: <err>` |
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

**`no_progress`** — how many tool calls in a row may teach nothing, ask nothing new, save nothing and leave
nothing new in the worktree before the task is stopped as spinning. Default **6**. On the
limit the report is `stopped: 6 steps without progress`, the landing turn runs, and what
the task made is committed onto its kept branch. A negative value answers
`Invalid arguments: no_progress cannot be negative`.

Both step limits are recorded in the checkpoint, so they survive a restart along with the
rest of the task.
