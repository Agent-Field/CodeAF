# What aforge is, and how you start it

## What aforge is

Aforge is a working colleague in a terminal. You talk to it in ordinary language,
and it works in the directory you started it in — reading, writing, running
commands, searching, and handing longer jobs off to run on their own while you
keep talking. The window is where you sat down, and it stays there; the
conversation can go on to be **about** other folders as well, which is how work in
another project happens without opening another window (see *Choosing a folder*).

It is a conversation you sit in front of. There is one conversation **on screen**
at a time, it is written down as you go, and you can leave it and come back to it.
One terminal can hold several at once — up to eight, each on its own project, one
in front and the rest running behind it; `tab` and home move between them. It is
not a background service and it does not keep working after you close the window:
the work happens while you are here, except for jobs and tasks it has already
started, which have their own rules.

## What it is called

The command you type is `aforge`, and `aforge` is what the surface calls itself
everywhere it speaks: the wordmark on the first screen and in the welcome box, the
name on home's top line, the first line of `/help`, the card that asks to connect
an account, the desktop notification's title, and the speaker heading in an
exported conversation.

It used to say `openaf` in some of those places and `aforge` in the others, which
meant a fresh install met one name in the wordmark and a different one in the
prose three rows under it. There is one name now, and `openaf` is on no screen.

## Starting it

| What you type | What you get |
| --- | --- |
| `aforge` | the home screen, over this directory's most recent conversation |
| `aforge chat` | the same thing |
| `aforge chat --session <path>` | that conversation, straight in, no home screen |
| `aforge resume` | the chat, opened on the picker of earlier conversations |
| `aforge chat --host devbox` | the chat here, the work on another machine |

**The very first launch on a machine with nothing configured** opens on a short setup
instead — connect OpenRouter in your browser, choose the crew, set the spending rails —
and then on the empty conversation. The preference questions are shown once. The
OpenRouter step returns on any later local interactive launch while no key exists,
including a named or resumed conversation, and `enter` on an unsent message brings it back
without clearing the draft. The getting-started page has the whole flow.

A `--once` or piped run cannot open a browser and still stops at the door with
`aforge chat needs a model to talk with.` Its next line says to run bare `aforge` in a
terminal to connect OpenRouter, or to export `OPENROUTER_API_KEY` (or `OPENAI_API_KEY`). A
custom `AFORGE_BASE_URL` is never offered the OpenRouter connection.

**The first frame is home** — every project on this machine and every conversation
in them — with the conversation this directory would have opened loaded and ready
underneath it. `esc`, or `enter` on the row the cursor starts on, drops into that
conversation; everything after that is the chat exactly as it always was. Home
stays out of the way when you name a conversation, on a `--once` or `--host` run,
and on a machine whose only conversation is the one already open — though it is
still there to go to: `space` twice on an empty box, or `/home`, opens it on that
machine too. The home page covers the whole of it.

Run it in the directory you want it to work in. That directory is where it stands
— what a bare filename means, where its `AGENTS.md` and project settings come from
— and it is shown in the status line so you can always tell. It never moves for
the life of the conversation. Starting in the "wrong" place is not a dead end,
though: name the folder you actually meant with `/folder`, and the work goes
there.

## The flags you can start it with

| Flag | What it does |
| --- | --- |
| `--model <slug>` | start on a particular model instead of the configured default |
| `--reasoning <level>` | how hard the model is asked to think: `off`, `low`, `medium` or `high` |
| `--session <path>` | open a particular conversation file instead of the most recent |
| `--host <host[:path]>` | run the conversation on another machine over ssh |
| `--once "<text>"` | send one message, print the reply, and exit — no screen, nobody watching |
| `--no-compact` | never shorten the conversation automatically |
| `--yolo` | run every tool without asking, subject to the limits that nothing lifts |
| `--max-hours <n>` | with `--yolo`: elapsed-time limit; interactive chat checks before new turns |
| `--max-cost <n>` | with `--yolo`: dollar limit; interactive chat checks before new turns |
| `--one-model` | every text call this session makes runs on the session model |

`--yolo` does not make aforge unstoppable: a small set of destructive commands
and anything that acts in your name still ask, whatever the setting says. See the
permissions page.

## Leaving it running on its own · leaving a headless run going with a budget · --once yolo · no screen · unattended · overnight · nobody watching

`--yolo` changes tool approvals. Normal interactive chat remains a conversation you
can steer, even with a budget: an unrelated question does not become a new assignment
for running tasks, and the first request does not remain a fixed goal for every later
reply. Tasks retain their own assignments until you revise them.

For a fixed unattended goal, use the one-message door with a budget:

    aforge chat --once "finish the import fix" --yolo --max-hours 6
    aforge chat --once "finish the import fix" --yolo --max-cost 20

## Does a run with no screen carry its own work on · headless --once checkpoints

Yes, when launched with `--once`, `--yolo` and a time or cost budget. The main
model completes the ordinary reply; concrete task, delivery, and declared-check results can
start a follow-up when they require one. Its decisions are kept in the transcript, where a
run without a screen can still be inspected.

Either limit alone is enough; both means whichever runs out first. The defaults can
come from `AFORGE_MAX_HOURS` and `AFORGE_MAX_COST`; explicit flags take precedence.
A headless `--yolo` launch without a budget stops when the model stops.

## Interactive chat limits · --max-hours · --max-cost

In normal interactive chat, `--max-hours` and `--max-cost` are checked before a new
turn starts, including a reply started by background work. They do not freeze the
conversation's goal. The time limit uses the current engine session's clock; the
money limit uses its recorded cumulative cost. An exhausted limit refuses the next
turn before recording your message or calling a model.

A turn already running and work already delegated may finish. These are not hard
reservations across every concurrent task, so the final bill can exceed a limit.
`/budget` remains a separate conversation spending limit; the stricter dollar limit
applies. Launch limits are changed by relaunching with different flags. A refusal
states which launch limit was reached.

The local persistent host carries these launch settings. Explicit `--host` still
refuses the budget flags at its door; configure that machine's launch instead.

## What changes when you give it a budget — done when, carrying on by itself, does it tidy up after itself

For a fixed headless goal (`--once --yolo` with a budget), the main model still owns the
answer and decides when it has completed the original request. Aforge does not ask a routine
second model to approve that ending, and it does not move the turn to a task because of file
writes, tool-round counts, or a share of the wall.

The unattended owner still enforces concrete execution facts. Work that is running remains
open. A task that lands incomplete, a delivery that could not be made, or an explicitly
declared check that fails is carried into the next turn. Repeating the same concrete gap can
still stop the run instead of looping forever. Before a successful end, declared checks run
through their existing gate.

Completion does not sweep files merely because the session created them outside its working
folder. A requested report at an absolute output path is part of the result and stays there.
The model can explicitly remove temporary material it created when the request calls for
cleanup, and aforge's own runtime directories retain their own lifecycle; neither makes every
outside path disposable.

The money and hour ceilings are unchanged. Nothing new starts after a ceiling is exhausted;
a speaking turn reaches its boundary, and task work is stopped or retained through the same
budget and cancellation rules described on the task pages.

## Why did it stop at a task that was finished

A finished task landing is evidence for the main model's next answer. If no running work,
failed landing, delivery problem, or declared check remains, the main model can finish the
original request without another model reading its conclusion.

## It ended without starting more work · why did it not hand the work over

Current builds do not automatically hand an ordinary turn to a task. Aforge does not create a
handoff from a second reader's sketch, a write count, a tool-round count, or one third of the
run's wall. The model can still use `propose_task` when separate watched work is useful, and a
task can still divide its own work. If neither explicit door is used, the answer remains with
the main model until it completes or a real budget, cancellation, provider failure, or other
execution boundary ends it.

If an older run moved work to a task after five minutes, or ran tests for ten minutes and then
handed the work over, that was the retired wall-share policy. Elapsed time alone no longer
causes either move.

## What counts as still left · a task that died on the wire · it kept working after everything was finished

**What is left is read from the tree, not from a task's death.** A run with a budget looks
at every piece of work at the end of each reply and asks what stands between it and
finished. Two kinds of dead task do **not** count:

- **One that died on the connection or was refused by the model provider.** A dropped
  stream, an API error, a model that is not there: nothing was found out about the job, so
  it is not evidence that anything is unfinished. Every other ending still counts — a check
  that found gaps, a loop that went round, a step limit, a working copy that could not be
  made, one you stopped yourself.
- **One whose files somebody else wrote and brought home.** If every file a dead task was
  going to change has since been changed by a task that finished **and** merged, the thing
  it was for is on your branch already, and asking for it again would rewrite a file that is
  already written. It has to be every file and the other task has to have merged; half of
  somebody's work is still work.

Measured before this: a task died on an API 404 an hour before its parent wrote the very
file it was for, went green and merged. The run read the dead sibling as a gap in the ask
and carried on over a finished tree until its wall ran out.

## Does inline work need a completion witness · the reader timed out · it did the work twice · it says nothing has been finished yet after editing or creating a file

Inline work no longer needs a second-model witness. When the main model completes the original
request, aforge does not require a `Made` file-activity mark or a completion reader before it
can finish. Explicitly declared checks and concrete task or delivery failures can still
contradict a successful ending; absence from a write ledger cannot.

The old lines `nothing has been finished yet` and `the reader could not be reached, so the
checks stood in for it` belong to the retired witness gate. They should not appear on a new
ordinary run.

## A changed file counts only while its content differs · a reverted or stashed edit is not finished work

File-change tracking no longer decides whether the main model may finish. A reverted edit is
simply the current tree, and a changed file is evidence the model should inspect and report.
A stash created during an unattended run still matters when it holds requested work outside
the delivered tree; that concrete delivery gap can keep the run from claiming completion.

## An emptied, blank or zero-byte created file does not count as finished work

A blank file has no special role in the completion decision. The main model must judge whether
it satisfies the request, while declared checks and actual task or delivery failures retain
their existing authority.

## The git an unattended run left on its own will not run · why it refused to stash, checkout, pull or reset --hard

**A session left running on its own with a budget answers to the same git list as a
task.** It decides on its own word that the work is done, and a stash can make that word
false by taking the work out of the tree it judges. The refusal happens before the shell
runs.

It will not run `stash` in any form except `stash list` and `stash show`; `merge`,
`rebase`, `cherry-pick`, `revert`, `checkout`, `switch`, `am`, `apply`, `worktree`,
`update-ref`, or `symbolic-ref`; `pull`, `fetch`, `clone`, `remote`, or `submodule`;
`push`; `reset --hard`, `reset --merge`, or `reset --keep`; or `restore --source`.
These are one list: they take the working copy away, put it onto work the session did not
do, bring in remote work, or send the session's work to a shared remote on its own word.

**Reading is still allowed.** `git status`, `diff`, `log`, `show`, `branch --list`,
`stash list`, and `stash show` can look at any branch. Plain `git reset` can unstage,
plain `git restore <path>` can restore the session's own path, and `git add` and
`git commit` are allowed. For a stash, the session reads exactly:

> git stash is not yours to run here: it takes your working copy away, and what is in it is the work this session will be judged on. Leave the change in the tree, or commit it.

The `1 stash entry holds work that is not in the tree` reading in the section above says
the stash out loud after the fact; this refusal is why there is now usually nothing for
that reading to say. None of these refusals applies to a session you are sitting in front
of, or to an unattended run that named no ceiling: nothing is deciding on its own that
the work is done then, and your git is your own.

## It keeps saying the tests fail but they were already failing · red before the work · a check that was broken when I started

**A check is yours only if your run introduced new red.** On a run with a budget, aforge runs
the checks its acceptance names once at the start — before it has touched anything — and
writes down which were already failing. Only the acceptance it wrote for the work and each
task's own brief supply checks; a command pasted into your ask (the steps you took to see a
bug, say) is never run as one — `$ chmod 000 tox.ini` in a pasted issue once was, and no
longer is. At the end it runs them again. A check that was **green before and red after**
counts as work still to do. When a runner names individual failures, a new failure inside
a command that was already red also counts; unparsed red stays uncertain.

That first reading runs **in the background**, so nothing waits for it: your first turn
starts straight away. It gets one window for the whole set rather than one per check, and
two things are deliberately not read — a check that **changed the tree** (a build, a
formatter, a migration: that would be aforge making the first edit, not looking) and a
check the shell **could not run at all**. Writing a cache or a coverage file is not
changing the tree: in a git repository the question is asked against what the project
itself keeps, so anything your `.gitignore` covers is invisible here and a test runner's
`.pytest_cache/` costs you nothing.

**A check the reading could not take is never counted against you — or for you.** Nobody
knows whose red it is, so it is left out of the arithmetic in both directions rather than
guessed at. Until the whole reading lands the same is true of every check, and the run says
so in as many words. At the very end it waits for the reading before deciding. Missing
before-evidence remains unknown; waiting does not turn it into a passing check.

That matters because of the sentence people naturally write: *"the existing test suite
passes"*. Over a project whose suite already has one failing test, that can never become
true, and a run that reads its own failure in the project's will spend its whole ceiling on
somebody else's bug. One measured run did exactly that.

What was already broken is not hidden from the work either — the brief it carries on with
says so plainly, so nothing goes off to fix it by accident:

```
1 check was already failing before this work; that does not show the requested result works: tox -e py
```

This section describes the session's own end-of-reply reading, which runs only when you
leave a session working with a budget. A task's checker takes its separate before-reading
whether you are watching or away; **The check says my tests fail but they were already
failing** in *How tasks run* explains that task reading and its limits.

## A task added a check after work started · no before-reading for a late check

A task can name a check after the session has already taken its initial reading. The
session still runs that check at completion, but cannot tell whether its failure existed
before the work. It records which commands the initial reading covered; a later command
stays unknown rather than becoming a new regression just because it has no earlier entry.
The ending or continuation names that uncertainty, for example:

```
one check has no usable before-reading, so its current result cannot establish a regression from this work: check-report
```

An unknown result does not prove the requested result works. A task still running, a
failed task or work that has not reached its requested destination still keeps the session
from finishing. A check actually read before work that turns from green to red still
counts as work left to do.

## It stopped and said the same thing was still left · why did it keep saying carry on · it kept repeating the same thing

An unattended run can carry on from a concrete unmet result: a task came home incomplete, a
delivery failed, or a declared check did not pass. If the same concrete gap survives the next
turn unchanged, the run can stop with that reason instead of repeating it until the wall.

This standstill guard does not ask a second model what remains and does not invent a gap from
the main answer. It compares execution facts already held by the run. Task and job wakes reset
the picture when new results arrive.

## A task waiting on one that did not finish · work that will never start · it says something is still running

**Work that is still going is never a standstill, and never finished either.** A
piece of work that is actually moving — started, or queued behind something that
is — is named on the list in its own right, `write the tests is still running`, so
the ask cannot be called finished over the top of it. And a list that has not
changed because it is waiting on that work is a run waiting rather than a run
repeating itself, so the count above starts again once everything has landed.

**Work that cannot start is not waiting, it is left.** A piece of work queued
behind one that did not finish, or behind one that came home needing a decision,
has nothing coming to start it — nobody is going to look at it while the run is
unattended. So it is said whole, with what it waits on and what became of that:

```
wire the handlers is waiting on port the parser, which is your call
```

That counts as part of what is left rather than as a reason to keep going, which
is what lets a run in that state reach the standstill above and stop — instead of
carrying on for the rest of its hours over work nothing was ever going to start.

## It keeps saying a file does not pass · a check nobody asked for

**A check is a command explicitly declared in the task's `checks` field.**
A command quoted in your request, a brief or a `done when` sentence does not
become permission to run it. Declared checks run in a fresh shell to see whether
the work stands up. A file is opened the way the file itself says it opens: if it is
executable, or its first line names the program that runs it, that is what gets
run. **A file that says neither is not a check at all** and is left out — a
source file quoted in a sentence is something to look at, not something to run.

Before this, a bare path was handed to a shell, which refused to start it, and the
run recorded "does not pass" about it for the rest of the evening — a wall no work
could ever get past. Anything that says `does not pass` now is something that ran.

**A file in your own folder beats a program of the same name.** If the tree you are
working in holds a file called `check`, `build` or `test`, that file is what a
check by that name means — not whatever program of the same name your shell would
have found. And if the tree holds it but nothing can start it, nothing is run at
all: running a different program of the same name would be worse than running
nothing.

**What came home is concrete evidence.** A piece of work that finished and covers
what you asked for, with every declared check that ran passing, can support the main
model's completion account without another model judging the transcript.

## Which folder does aforge work in, and where do my files go

It depends on where you started it, and there are two cases. (Where the conversation
**stands** is one thing; the folders it turns out to be **about** are another, and *Choosing
a folder* has that half. This section is about where you are standing.)

**You started it inside a project.** Aforge borrows that directory. Its tools read
and write your repository, exactly where you are standing — and that stays true of it
however many other folders the conversation turns out to be about, because a folder you
chose with `/folder` is written through a copy and landed with `/land` instead, and the status line
shows that directory's name — `app`, `my-site` — until the conversation names
itself, so you can always tell which project this conversation is about. The full
path is on `/status` and on the status sheet, under `place`, with the git branch
beside it.

**You started it anywhere else** — your home directory, a temp folder, a
launcher with no directory in mind. Then the conversation gets a workspace of
its own, inside its session folder, and anything it writes lands there instead
of scattered across wherever you happened to be. In that case the place reads
simply:

```
aforge
```

That word means "no project — this conversation has its own space". The real
path exists and is not a secret; it is just aforge's own bookkeeping, and
showing it where you look to answer "which project am I in" told you nothing.
Type `/status` and the `place` line gives you the full path to copy.

An owned workspace of that kind is quietly made into a git repository, so work
done there has undo history like work done anywhere else — and so a task started
in such a conversation branches from it and merges home exactly as a task in a
project does. **A conversation with no project still runs tasks**; nothing has to
be anchored first. See *A task in a conversation with no project* on the page
about how work on its own runs.

## Where do task files go when I did not open a project — work lives inside the conversation

Inside the conversation's own folder, and nowhere else. A conversation opened outside any
project owns a scratch workspace, and that workspace is a `work/` directory in the session
folder:

```
~/.aforge/v3/projects/<project folder>/<conversation folder>/work
```

`/status` prints that full path on its `place` line. Read it there rather than assembling
it — the two folder names are encoded, not typed.

That containment is the point: nothing is scattered across wherever you happened to be
standing, and closing the conversation leaves nothing behind anywhere else. It is also the
sharp edge — **the work is inside the conversation, so deleting the conversation deletes
the work.** See *I deleted my chat and lost the files the task made* below.

**A task is never refused for want of a project.** That scratch workspace is quietly made a
git repository when the conversation opens, so a code task branches from it and merges home
exactly as a task in a real project does. An older aforge stopped such a task with `this
task needs a project; use /workspace <path> or name where it should work`; nothing says that
any more.

And a conversation that has been somewhere else goes there instead: a folder you named, or
one this conversation turned out to be about, is where the work stands, and the scratch
workspace is not used at all. *A task in a conversation with no project* has the whole of
it.

## I deleted my chat and lost the files the task made

If the conversation owned its workspace, they are gone and there is no second copy. The
files were in `work/` inside the session folder (above), so removing that folder under
`~/.aforge/v3/projects/` removed the transcript and everything made in that workspace in
one move. Nothing is copied out first and nothing is mirrored anywhere else.

Aforge removes a conversation of its own accord in exactly one case: one you **started in a
temp directory**, seven days after you last said anything to it. Every other conversation
under `~/.aforge/v3/projects/` stays whatever its age. See *What gets cleaned up, and when*
on the keeping-an-eye page.

**Three ways to keep the work instead**, all of them before the fact rather than after:

- **`/workspace <path>`** anchors the conversation to a repository or folder you keep, and
  everything from then on happens there. It does not move what is already in `work/`, and
  a conversation may do it once.
- **Copy it out yourself.** They are ordinary files; `/status`'s `place` line is the path.
- **`/files`** lists what was made *for* you — pictures, sound, video, exports — with the
  path each landed at, and `ctrl+y` on a row copies one somewhere else. It is a list of
  those, not of every file a task wrote.

`/export <path>` writes **the conversation** to a file: what was said, not the files that
were made. It is not a way to rescue the work.

## Anchor a conversation to a repository or folder — /workspace and the workspace tool

When a conversation says `aforge` because it opened with no project, type `/workspace
<path>` to make the repository or folder at that path its project. The path may begin with
`~`; a path inside a Git repository resolves to the repository root. The place line changes,
the project's `AGENTS.md` and `CLAUDE.md` are loaded into the conversation instructions,
and future tasks cut their working copies from that repository rather than from the
conversation's own workspace.

The model has the conditional `workspace` tool for the same move when you name a repository
path in ordinary chat. Both doors exist only while the conversation owns a scratch
workspace. Once it is anchored, it stays on that project and both doors refuse another
switch. The resolved path is saved in the conversation's `meta.json`, so reopening it keeps
the anchor.

**And a conversation opened later can be in a different folder from the one you
started in.** `enter` on home opens a row of any project, and typing a path on
home starts a conversation there — each on **its own** workspace, with that
project's approval rules, crew, spend ceiling and saved shapes of work, resolved
the same way this one's were. An unanchored conversation may acquire its project once with
`/workspace`. The status line's place word is
always the folder of the conversation on screen. See home's page under *Open
another project from home*.

**You do not need a second conversation to work on a second project, though.** Where a
conversation stands is fixed and one; what it is **about** is not. Name another folder with
`/folder` or `/attach <dir>`, or name its path when you ask for the work, and the work goes
there — same conversation, same window. What a second conversation gets you that this does
not is a second set of project settings, approval rules, crew and ceiling. *Choosing a
folder* has the distinction in full.

## Where your conversations are kept

Every conversation is written to disk as it happens, under your home directory in
`.aforge/v3/projects/`, in a folder named after the project you were working in
and a folder of its own inside that. The conversation's own folder holds the
transcript and everything else the conversation kept. That transcript is what
`aforge` reopens when you come back, and what the picker lists. Closing the
window, or losing the connection, does not lose what was said.

Opened inside a project, aforge works in the project and leaves nothing of its
own in it. Opened where there is no project at all — your home directory, a
temporary directory, a launcher — it works in a folder of its own instead, so
scratch files and downloads land somewhere they can be thrown away with the
conversation.

## Where aforge writes its log — chat.log, the crash log, and why nothing appears on screen

Aforge keeps one log file for the running program: `chat.log`, beside the rest of what it
keeps. With nothing moved that is `~/.aforge/chat.log`; `AFORGE_HOME` moves it with the
rest of aforge's state, and `AFORGE_PROFILE_DIR` puts it inside that profile folder
instead. Read it with `cat ~/.aforge/chat.log`, or `tail -f ~/.aforge/chat.log` in a
second terminal while aforge is running.

**Everything the program logs while the conversation is on screen goes there and never on
the screen.** That is on purpose: aforge takes over the whole terminal, so a warning
printed to it would land spliced into the middle of what you are typing and be gone with
the next redraw. It holds internal warnings, recovered internal faults with their stacks,
and notes about things aforge fell back from — the kind of thing to send along if you are
reporting something odd. The redirect lasts exactly as long as the conversation is up, and
ordinary output goes back to the terminal when you quit.

**It is the same file however you started.** Opened here, opened on another machine with
`--host`, reached over a relay with `--at`, or handed to a session already running in the
background — all four write to `chat.log` in this machine's profile, the machine you are
sitting at.

If aforge stops with `aforge hit an internal fault and had to stop`, the details it points
you at are appended to that same file, so there is one place to look either way. In the
rare case aforge cannot write there at all — a profile folder it has no permission for —
it leaves the log on the terminal rather than dropping it, on the grounds that a torn
frame is better than a lost warning.

This is not the record of what aforge sent the model: that is a separate file, read with
`aforge logs`, and `aforge logs --path` prints where it is.

## Asking aforge about itself

Aforge ships with this manual compiled into it, and it reads it with a tool
called `manual` rather than answering about itself from memory. So "what can you
do?", "what does ctrl+b do?", "can you read a PDF?" and "why did you just ask me
that?" are all fair questions to type straight into the conversation. If the
manual has nothing on something, that usually means aforge does not do it, and it
will tell you so instead of inventing an answer.
