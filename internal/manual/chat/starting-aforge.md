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

## Does a run with no screen carry its own work on

Yes, when launched with `--once`, `--yolo` and a budget as above. A headless
unattended run checkpoints long replies at the same points. Its decisions are
kept in the transcript, where a run without a screen can still be inspected.

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

## What changes when you give it a budget — done when, carrying on by itself, tidying up after itself

For a fixed headless goal (`--once --yolo` with a budget), four things change:

- **It writes down what finished means.** At the start it turns your ask into one
  `done when` sentence and shows it to you on a dim line. That sentence is fixed
  for the whole session — nothing it does later can rewrite it. It is context for
  the work and never evidence about it: what says the work is done is the work.
- **A stopped turn is looked at rather than taken at its word.** When it stops
  talking, it checks whether any piece of work came home unfinished, and whether
  the checks your work names passed when they ran. If any of that is unmet it
  carries on by itself instead of going quiet — and if none of it is, the ask is
  finished, whatever else was said about it.
- **A piece of work that came home unfinished starts its own next go.** You used
  to be offered a follow-up in your own words — with nobody there, that offer went
  nowhere. Now what was missing becomes the next brief. If the same thing stops it
  three times in a row it stops for good and says so.
- **It tidies up after itself before it says it is done.** It re-runs the checks
  in a fresh shell, then looks at every file it made: anything inside the folder
  it is working in is part of the answer and is left alone, and anything it wrote
  outside that folder is scratch and is deleted. It never touches a file it did
  not create, and it never touches one it only changed.

## Why did it stop at a task that was finished

If it ended without starting more work, or did not hand the work over, the
budgeted run checked whether anything remained before starting another task.

## It ended without starting more work · why did it not hand the work over

With a budget, **every** way a turn ends is read, not just the ones where the model stops
talking. A long turn can also end by having its work moved onto a task — when a second
reader says the work has parts, when the turn has changed enough files, when it has run
past its own price, or — only with a budget — when it has had its share of the hours you
gave the run. Each of those seals the turn, and until this they sealed it without
asking anything: the run then had no way back at all, because the only thing that could end
it was a landing waking a turn that moved its work onto another task, and so on until the
hours ran out. Three measured runs finished their work, went green, and still ran to the
wall that way.

So before the work moves, the same reading a stopped turn gets is taken:

- **Something of yours is still running** — the work moves onto a task exactly as it always
  did, and you read the same line about it.
- **Nothing is left and nothing is running** — the run ends there, on the line
  `finishing here · what was asked is done`, and **no task is started**. It used to hand the
  work over anyway, on the grounds that the reply had not yet read its last results; measured,
  that started two tasks nineteen seconds after the run had already read the tree, the checks
  and the second reader and said the ask was met, and both ran to the wall. Being finished is
  read from those, not from the reply's own words, so nothing a further reply said could change
  it. If a piece of work is still running when that happens, it is not ended there: the work
  moves onto a task as usual and the same answer is read again at the next ending.
- **The same thing is left as last time** — it stops for good with the reason, on the same
  `stopping here · ` line every other stop uses. If a piece of work is still running when
  that happens, it does **not** stop there: the work moves onto a task as usual and the same
  stop is said again at the next ending, once nothing is in flight.
- **The hours or the money ran out** — this is the one stop that does not wait for anything.
  Waiting is more of exactly what ran out, so it ends the turn even with work still going,
  and says so on the end of its own line: `· work was still going and was left where it was`.
  Nothing is killed and nothing is thrown away; what was running is where you left it.

**And a handover it asked for is never dropped.** A long turn can normally talk its own
handover out of happening: if the model says nothing is left and the second reader's sketch
does not name independent parts still to do, the work stays where it is and the turn
finishes. It gets that **once per request**, whether or not the sketch agreed — a turn that
says it is done and then keeps working is met by the next look with the claim already spent.
On a run with a budget that
only holds while the run's own owner agrees — and when it has just read the ending and said
the ask is **not** finished, the work moves anyway, on its account of what is left rather
than on your bare sentence. A run measured before this said "not yet confirmed" at its
write seam and again at its ceiling, had both handovers thrown away by the two readers, and
ended eight hundred seconds later inside a `git stash` with the fix uncommitted.

**And for a fixed headless goal, one reply may spend at most a third of the wall.** *Why did it move my
work to a task after five minutes; it kept running tests for ten minutes and then handed it
over; why did it not hand over sooner.* The other three ways above all COUNT something —
parts in a sketch, files changed, rounds spent — and a reply that spends its time reading
and running tests crosses none of them. A measured run did exactly that: a fifteen-minute
wall, the fix working in the checkout at five minutes, twelve and a half minutes of reading
and tests inline, the work finally moved with 147 seconds left, sixty of which went on
opening the task's working copy — and the wall came down on a task that had committed
nothing, checked nothing and landed nothing. So a reply that has been running for a third of
the hours you gave the run hands over, and you read:

```
this has taken a third of the time · moving it to a task that can be checked before the wall
```

**A third, because the other two thirds are what the work needs after it moves** — a working
copy opened, the job run, and somebody who is not the model that did it reading the result,
which is the whole difference between work that happened and work that landed. **And it hands over only what can still be checked before the wall**: once less of the
wall remains than a task needs to open its working copy and be checked — the two waits a
task is already held to, added — a reply is not moved at all, and nothing is spent asking,
because a task started then could not be set up, let alone checked; a measured run started
one with six seconds to go. It happens
**once** in a reply; rounds spent only watching work you already handed out do not count
towards it; and it needs a wall, so `--max-cost` on its own never triggers it.

None of this applies to a session you are sitting in front of: your turn's work moves onto a
task exactly as it always has, nothing is decided for you, and a reply that says nothing is
left still leaves your answer where it is. There is no wall on your session, so there is no share
of one either, however long your reply runs.

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

## It says nothing has been finished yet after editing or creating a file · inline work · emptied file · blank file · zero bytes

**And work aforge did itself counts as finished work.** The question is not only "did a
task come home": a session that made the change and wrote the tests **inline**, with no
task at all, has finished something — as long as the second reader agrees nothing is left.
That includes a fix that is one edit to a file the project already had, which is the
commonest fix there is: it used to count only files the session created, so a one-line change
to an existing file read as nothing finished until the run stopped itself over green work.
Before this, a run that did the whole job in the conversation read `nothing has been
finished yet` at the end of every reply over a tree it had just written, said the same
thing twice, and stopped itself for going round in circles one second after tidying up. If
that reader names a gap instead, the run carries on into it, and being finished is settled
by running your checks over the tree either way.

**And a file is your work only while its content still differs from what it was before the
edit.** A change put back the way it was, a revert, or a fix pushed onto `git stash` and
never popped leaves the path written and nothing in the tree, so none of them counts as
finished work. A stash the run took itself and never popped is said out loud as well —
`1 stash entry holds work that is not in the tree` — and the run carries on rather than
finishing over it; a stash you already had before the run started is yours and is never
counted, and neither is the one a landing takes to set your uncommitted work aside.

**A file the run created counts as its work only while there is something in it.** A
rewrite that produced nothing, a generator that wrote no bytes, or a `> file` in a shell
step can leave it emptied, blank, or at zero bytes; none of those empty files counts as
finished work. It is still your file: nothing inside the folder aforge is working in is
ever deleted, whatever is in it.

## It keeps saying the tests fail but they were already failing · red before the work · a check that was broken when I started

**A check is yours only if your run turned it red.** On a run with a budget, aforge runs
the checks its acceptance names once at the start — before it has touched anything — and
writes down which were already failing. Only the acceptance it wrote for the work and each
task's own brief supply checks; a command pasted into your ask (the steps you took to see a
bug, say) is never run as one — `$ chmod 000 tox.ini` in a pasted issue once was, and no
longer is. At the end it runs them again, and only a check that was
**green before and red after** counts as work still to do.

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
so in as many words. At the very end it waits for the reading before deciding, so a run is
never called finished over red nobody could account for.

That matters because of the sentence people naturally write: *"the existing test suite
passes"*. Over a project whose suite already has one failing test, that can never become
true, and a run that reads its own failure in the project's will spend its whole ceiling on
somebody else's bug. One measured run did exactly that.

What was already broken is not hidden from the work either — the brief it carries on with
says so plainly, so nothing goes off to fix it by accident:

```
1 check was already failing before this work and is not counted: tox -e py
```

A session you are sitting in front of runs none of this: your tree is in front of you, and
nothing is checked or decided on your behalf.

## It stopped and said the same thing was still left · why did it keep saying carry on · it kept repeating the same thing

An unattended run with a budget looks at the work at the end of every reply: which
pieces of work came home finished, and what the checks your work names said when
they ran. If something is left, it carries on by itself with that as the brief.

**The same thing left twice running stops the run.** The evidence that a run is
getting anywhere is that what is left CHANGES. When it reaches the end of a reply
holding exactly the list it held last time, it stops and tells you, on one line:

```
stopping here · nothing moved since the last look and what is left is the same — wire the handlers did not finish · saying it again would not change it
```

That is the whole message: what is still left, and that it stopped rather than say
it again. Nothing crashed and nothing is wrong with your machine — the list in the
middle is where to pick the work up.

**It holds for the rest of the session.** The floor belongs to the thing that
decides, not to the reply it stopped, so a piece of work landing and waking a fresh
reply cannot start the loop over. There is no number to raise and no setting for
it. Before this, a run in that state repeated one identical line until its hours
ran out.

## A task waiting on one that did not finish · work that will never start · it says something is still running

**Work that is still going is never a standstill, and never finished either.** A
piece of work that is actually moving — started, or queued behind something that
is — is named on the list in its own right, `write the tests is still running`, so
the ask cannot be called finished over the top of it. And a list that has not
changed because it is waiting on that work is a run waiting rather than a run
repeating itself, so the count above starts again once everything has landed.

**Work that cannot start is not waiting, it is left.** A piece of work queued
behind one that did not finish, or behind one that came home and needs your look,
has nothing coming to start it — nobody is going to look at it while the run is
unattended. So it is said whole, with what it waits on and what became of that:

```
wire the handlers is waiting on port the parser, which needs your look
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

**What came home outranks what was said about it.** A piece of work that finished
and covers what you asked for, with every check that ran passing, is finished — a
reader's opinion about the transcript cannot carry the run on over the top of it.

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
