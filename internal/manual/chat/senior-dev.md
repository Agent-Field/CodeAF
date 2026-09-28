# senior-dev

## What /senior-dev does — hand one large change to senior-dev, an autonomous coding agent

`/senior-dev <brief>` hands the whole brief to **senior-dev**, an autonomous coding agent
codeaf carries. At a shell the same program is `codeaf senior-dev <brief>`. It is built
into codeaf and runs only through it: there is nothing to install and no senior-dev of
its own to start.

In a git repository it works alone in a private copy of its own, on a branch of its own,
so your checkout is never touched and several runs can work on one repository at once; in
a folder with no git history it works in the folder itself. It writes your brief down word for word, reads
the repository, keeps a checklist of what the brief asks for, pins a command that shows
the work passes, and edits until it believes the change is done. Then it **submits**:
the tree is frozen at that moment, so nothing it does afterwards can change what it hands
back. It then runs the project's own build and tests on the frozen tree, and if anything
moved after it submitted, the tree is put back to what it submitted.

It is for complex, multi-part coding work: fixing an issue in a mature codebase whose
cause spans files, a feature with its tests, a rewrite across a package, a migration.
codeaf hands work like that to it by itself, and uses it whenever you name it (the next
section). Its brief has to settle everything, because nobody will be asked anything.

## Will codeaf use senior-dev by itself — when does codeaf hand work to senior-dev, how do I make codeaf use senior-dev, stop it using senior-dev

**Yes, for the work it is for.** The chat's model is told to hand complex, multi-part
coding work to senior-dev, whole, rather than doing it in the conversation or giving it
to codeaf's own worker: fixing an issue in a mature codebase whose cause spans files, a
feature with its tests, a rewrite across a package, a migration. It proposes the task
with `via` naming senior-dev, and the card goes up like any proposal's, with its
countdown. A change you would make in a few steps it still makes itself.

**Naming it is enough.** Say senior-dev in your message, in any spelling: "fix issue 412
with senior-dev", "/senior-dev should take this", "senior dev". The model is told to use
it, and if it proposes the work without senior-dev anyway, codeaf turns that proposal
back once, with any others in the same reply, and tells it you named senior-dev. That
holds even for a one-file fix you ask senior-dev for, which otherwise stays in the
conversation, and through a correction you type while it works that does not name
senior-dev again. A commit, an undo or a revert stays in the conversation even when it
names senior-dev ("revert senior-dev's commit"): senior-dev works on a branch of its own
and never moves yours.

**Saying not to is kept too.** "don't use senior-dev for this" names it, so the first
proposal is turned back the same way; the model reads that and proposes it again as it
was, and that proposal passes.

**Typing `/senior-dev <brief>`** starts it at once, with your brief word for word and no
card. Work codeaf moves to a task on its own, because a reply ran long or looked like
work, goes to codeaf's own worker, never to senior-dev.

## Watching senior-dev work — open its task, what it is doing step by step, how long it has run, stop it

A senior-dev run is a task of the conversation that started it. Its compact side-list
row wears `[senior-dev]` after its title (`[sd]` when narrow) and shows its time while
the page is closed. Hover over the row for its current step and spend; both are also in
the task page header. A card in the conversation lands when it ends. That card
stands outside the chat's `worked` fold as soon as the run lands, including while its
task page is open; the chat's own work in the wake reply still folds. Click the row or the
card, or follow a task link to it, and its task opens **inside the conversation's own
tab**: the tab strip stays on top, with the conversation's tab selected and the `home` tab beside
it. senior-dev gets no tab of its own.

The task shows **what senior-dev is doing**, action by action, each under the step of its
process it served — the workspace it set up, what it read and ran and changed, its
hand-in, the build and tests it ran itself, and how it finished — with the call to its
model in flight as the last line, `◐ thinking` and its seconds. The next section says what
each step means. **Click an action to see the whole step** — the command or file it was
called with and what came back — and click it again to fold it. **Every change to your
files wears git's `+N,-M`** at the right of its line — the lines it added in green and the
lines it removed in red — so you can see how much each step moved the work; senior-dev's
own spec, pinned check and checklist wear none.

The one line over it is the task's title with its `[senior-dev]` badge, a `▸ brief`
dropdown, and the step, the spend of the run's ceiling, the number of model calls and how
long the run has been going — the same time the side list and the landed card show,
counted from the moment codeaf handed the work over. **The brief is behind the dropdown**:
click `▸ brief`, or press `ctrl+o`, and the whole brief senior-dev was handed is drawn in
grey under the title; the same again hides it. While the
task is open, its row on the side list leaves its clock out rather than show a time that
stopped when you clicked; the true time is back on the row the moment you leave.

**The raw calls are one key away.** `ctrl+y` turns the page to senior-dev's calls to its
model — what it sent, what the model answered, and which model it was — and `ctrl+y`
turns it back; the key row says `ctrl+y calls` or `ctrl+y actions`.

`esc`, a press on the conversation's tab, or a press on the `home` tab leaves it, and the run goes
on. `x` over an empty box, `/stop`, or `Stop` on that line asks `Stop this task?` first.
Nothing typed there reaches senior-dev: the box says `senior-dev reads no messages — say
it to main`, and `enter` says the same line and keeps your words in the box.

## What is senior-dev doing — the steps on senior-dev's page, what spec, explore, pin, checklist, implement, submit, verify mean

The word down the left of senior-dev's page, and on its row while it runs, is the step of
its own process an action served. senior-dev has no planner, reviewer or helper agent:
one model works through the middle steps in the order it chooses, so a step's word comes
back whenever it returns to that step.

- `setup` — it set up the folder it works in: `git`, or `no git history` for a plain
  folder, whose checkpoints it keeps outside it.
- `spec` — it wrote your brief down word for word as its spec, and read it back.
- `explore` — it read, searched and ran commands before changing any file.
- `pin` — it wrote down the one command that shows the work passes.
- `checklist` — it listed what the brief asks for, and ticked it off.
- `implement` — it changed files, and everything it read or ran after its first change.
- `submit` — it handed in its work: `handed in its work · 4 files · 5 of 5 ticked`, or
  `its hand-in was refused` and why. The work is frozen at that moment.
- `verify` — with no model, it ran the project's own build and tests itself, one line per
  command with `passes` or `fails · exit N`, then what they came to. It also checks the
  tree this way when its model stops without handing in.
- `finish` — what it did to the tree it leaves, the size of its change, and its ending.

Lines with no word of their own are senior-dev steering its model in the step already
under way, drawn quieter: `told its model what it found, and to finish and hand in (nudge
1)`, `time is short: gave its model one last turn to finish`, a dropped call retried, a
tool call written as text corrected — and `compacted its memory` and `switched to <model>`
with its reason.

## How do I tell a senior-dev task from a normal task — the [senior-dev] badge, [sd], what the brackets on a task mean

A task handed to senior-dev wears its name as a badge wherever a task is named:
`[senior-dev]`, bold in the accent colour, after the task's title. A normal task — one
`/task` starts, or one the chat hands to codeaf's own worker — wears no badge.

- **The side list** wears it after the title. When the list is too narrow for
  everything, the task's number (`#7`) goes first; then the badge shortens to its
  initials, `[sd]` — the narrower list a frame under 120 columns draws reads
  `⠋ rewrite the… [sd] #7` — and the title is cut last. Widened with `w`, the list has
  room for the whole badge and the number.
- **The card you answer** asks `wants to start a [senior-dev] task: <title>`, and the
  card's top line wears the badge beside the task's name.
- **The task's own page** wears it beside the title — a page onto another
  conversation's task too, with that task's own badge and never the one a task of the
  same number in this conversation wears.
- **The task strip**, the row of chips that stands in for the side list under 100
  columns, wears `[sd]`.
- **The `@` list, the tasks place and home** — its list of work, a landing under
  `needs you` and a line under `since you left` — wear `[senior-dev]`, or `[sd]` where
  the row is short of room. The title is cut before the badge, and a `since you left`
  line cuts what the work came to first: `rewrite the auth middleware [senior-dev] · it…`.
- **The chat's `tasks` tool** says `via senior-dev` on the row, so the chat can tell too.

The brackets are always drawn, so a terminal with no colour, the row of the task you
have open, and a screen reader all still show the badge. It is not a button: a press
anywhere on the row opens the task.

## How do I ask senior-dev for a change — writing the brief, what to put in it

The brief is everything senior-dev knows about what you want. It is saved as
`.senior-dev/spec.md` in the folder it works in exactly as you wrote it, and it is read
back from there whenever senior-dev summarises its own history, so the words you chose
are never paraphrased away.

Write it the way you would hand work to someone who cannot reach you:

- the files, packages or commands involved, by name;
- what done means, and how to check it (the test to run, the output to see);
- the constraints: what must not change, and the wrong answer to avoid.

In the chat, `/senior-dev` followed by the brief starts it as a task. At a shell, flags go
before the brief, and `--` ends them: `codeaf senior-dev run --variant high -- rename the
config loader`. Everything from the first word that is not a flag onwards is the brief,
so a flag written after the brief becomes part of it.

## Running senior-dev on a repository you have not cloned — a benchmark task, another project

senior-dev works in the folder it is handed and nowhere else, so it has to be handed the
repository the work belongs in.

In the chat, ask for the work and name the repository, and the commit if the work names
one. The model clones it first, into a new folder, at that commit, and hands senior-dev
that folder. A benchmark task works this way: senior-dev works in the project's own
repository and not in the benchmark's, so the benchmark's files, its reference solution
among them, are not in its folder.

At a shell, clone the repository yourself, then run `codeaf senior-dev` inside it or pass
the folder with `--dir`.

A brief that names the folder it works in is fine: senior-dev reads it as written. A
brief that tells senior-dev to make a checkout of its own somewhere else does not work:
its file tools refuse to write outside its folder, and what a shell command changes out
there is not part of the task.

## What senior-dev cannot do — it cannot ask you anything, wait on another task, be retried or carried on, no step cap, no Windows

**It cannot ask you anything.** Nobody is at its keyboard, so the `question` tool is
absent from the model's tools. Put everything it would stop and ask into the brief.

**It cannot wait on another task.** A task handed to senior-dev starts the moment it is
approved, so a proposal whose `depends_on` names work that has not finished is refused
before its card: `depends_on names task 3, which has not finished, and senior-dev starts
the moment it is approved — it cannot wait. Propose it again once task 3 has landed, or
with depends_on left out if nothing must finish first.` The chat is told when that task
lands and can propose it again then. The other way round, a task may name a senior-dev
run that ended done in its `depends_on` (the run's work is on its branch, in its folder),
but not one still going: `depends_on names task 5, a program's run that has not ended,
and a task cannot wait on one.`

**A run is never resumed, but codeaf may send the work back.** A run that ended is not
started again: `senior-dev's run is never carried on: its work is left where it ended, and
a new hand-off starts a new run`. Its card offers no retry, and the `@` list offers no
steer on a running one, because it reads no messages. What codeaf does instead is the next
section.

## What codeaf does when senior-dev ends — its ending, checked, sent back, retry, at most twice, ask before spending more

**senior-dev's ending goes to the chat, not to you.** The moment a run ends, the
conversation wakes on its own with how it came out — passed its own check of the project,
nothing finished checking it, handed in work that does not pass, stopped on a limit, or
broke — and acts on it:

- **passed**: the chat looks at what changed against what was asked, then tells you where
  the work is and offers to merge its branch;
- **nothing checked it**: the chat runs the project's checks on its branch itself, then
  acts on what they show;
- **does not pass, or did not finish**: the chat fixes a small gap on its branch itself, or
  hands the work back to senior-dev with a brief sharpened by what failed;
- **stopped on a dollar or time limit**: the chat never sends it back on its own, because
  another run spends more of your money: it says what is done and what is left, and asks;
- **broke**: the chat hands it back once if the cause looks passing (a network or model
  service failure), and otherwise tells you what broke.

**codeaf sends senior-dev back at most twice on its own** for one piece of work. A third
hand-off it tries, or one after a limit, is refused
(`senior-dev has been sent back to this work 2 times already, the most codeaf does on its
own: tell the person where the work stands and let them decide`), and you decide. A
hand-off you ask for yourself is yours, and starts the count again. This count holds
through wake turns and a reopened conversation until you send a message. Each hand-off still
shows its card, with the same countdown as any other, so you can stop one.

**A run codeaf sends back carries on on the last run's branch.** When the chat hands the
work back after an ending, before you have said anything, the new run is cut in a copy of
its own on the branch the last run left, and its receipt says `carrying on on its branch
<branch>, where the last run left it`. What earlier runs committed is counted as the line's
work, so a run that adds nothing never reads it as nothing. **A branch whose work passed is
never written again**: the next hand-off before you speak — the rest of the work, or the
next piece — is cut on a new branch from its tip, and says `on a new branch <new> cut from
<branch>, whose work passed and which it leaves as it is`; its ending says its branch is
`on top of <branch>, whose work it holds too`. A hand-off made after you have spoken starts
fresh, on a new branch cut from your checkout's last commit: check an earlier run's branch
out first and the new run is cut from its work.

**The card stays quiet.** senior-dev's landed card says `done` when the run finished,
`stopped` when you stopped it, and `ended` when it ended without finishing (never a red
cross), and `senior-dev's ending went to the chat`; the chat's own reply is where you read
what came of the work. `ctrl+o` on the card still shows senior-dev's own words.

**It has no step cap.** Every run has finite dollar and wall-clock ceilings: by default,
**up to $10.00 and 3h**. `/budget conversation` can lower the dollar ceiling to
what remains. At a shell, `--max-cost` and `--max-hours` set either ceiling explicitly.
The proposal card, typed command's start note and shell run's first line say which ceiling
applies. These ceilings are enforced outside senior-dev whatever it does.
`/budget conversation 1.5` in an open chat binds $1.50 to that conversation before
its receipt appears, so the next approval card and run use $1.50 or what remains.

**It reaches a model only through codeaf.** Its engine receives a short-lived token for
codeaf's loopback model API, but its model-written shell commands inherit neither that
token nor provider-key environment variables codeaf recognizes. Those commands can
still read files their process can read, including a profile stored on disk. A
`senior-dev.json` in your folder that sets
`apiKey`, `baseURL` or `providerRouting` is
refused by name, because codeaf decides which model service serves each call.

**It writes only inside its folder** — its copy, in a repository. Its file tools (`write`,
`edit`, `apply_patch`) refuse any path outside it, including one reached through a link,
and say so to its model; it can still read files elsewhere. Its shell is not fenced the
same way, and nothing a shell command changes outside its folder is part of the task.

**It keeps its record in git if git is there**, on its own branch. Where there is no git
history it keeps its checkpoints outside the folder instead and commits nothing — it
reads that itself, and never ends for want of git (see the section on folders that are
not a git repository).

**On Windows it is absent**: there is no `/senior-dev` and no `codeaf senior-dev`. Its
engine needs a Unix shell, process groups and file locks, so Windows builds leave it out
rather than carry something that fails every time.

## Can senior-dev use the internet — webfetch, websearch, models.dev, network off

Yes. Its `webfetch` tool can fetch URLs by default. Web search through Exa and Parallel
is opt-in: set `SENIOR_DEV_ENABLE_EXA=1` or `SENIOR_DEV_ENABLE_PARALLEL=1` before
starting codeaf. `SENIOR_DEV_NET=off` withholds `webfetch` and `websearch` for that run.
Senior-dev also requests model sizes and capabilities from models.dev when its cached
catalog is absent or stale. If that site cannot be reached, the run uses conservative
model limits and still calls models through codeaf's loopback API.

## What happens to background commands after senior-dev ends — stop and detached processes

On Linux, senior-dev's engine ends every process its shell started when the run ends,
you stop it, or it reaches a ceiling, including detached processes that clear their
environment; the engine follows its own process tree. If the engine itself
is killed with SIGKILL, codeaf makes a best-effort sweep for children that kept its
private launch marker. On macOS, a process that detaches itself may outlive the run.
On Windows, senior-dev is unavailable.
If Linux cannot enable the engine's subreaper, the run refuses before starting
work: `senior-dev cannot contain its shell processes on this machine: <error>`.

## How does senior-dev run Python tests — pytest, unittest, missing pytest

senior-dev chooses a project's test command in this order: CI, `AGENTS.md`, a
declared script such as a Makefile `test` target, `README.md` or
`CONTRIBUTING.md`, then an ecosystem default. For a Python project with test
files, the default is `python3 -m pytest` when pytest is installed or declared.
Otherwise it runs unittest discovery in each top-level `test/` or `tests/`
folder holding `test*.py`; without either folder it runs plain
`python3 -m unittest discover`. A folder with `__init__.py` uses `-t .` to
resolve project imports. Older Python cannot use `-t .` on a folder without
`__init__.py`, so that folder uses `-s <folder>` alone. A test command that
reports it ran no tests leaves the submission unchecked, even if it exits zero;
the ending says `no tests were found by <command>`.

## Can I run senior-dev in a folder that is not a git repo — a plain folder, no git, --in-place, operation not permitted, .Trash

Yes. **senior-dev uses git only if it is there.** A folder with no git history — a plain
folder, a repository with no first commit yet, a broken `.git`, a machine with no git — is
worked in as it is: senior-dev reads that itself when it starts, keeps its checkpoints
outside the folder and makes no commits. A folder inside a git repository whose root is
your home folder (a dotfiles repository) is worked in the same way, because codeaf starts
senior-dev with `--in-place` there: no branch is ever cut in your dotfiles.

When it ends its changes are already in the folder. The task's page says `its work is in
<folder>, which has no git history, so nothing was committed` (or, under a repository at
your home folder, `its work is in <folder>; the git repository around it is at <repo>,
which holds your home folder, so codeaf cut no branch there and committed nothing`).

It works in your folder itself. Edits saved while it runs can join its work;
after submission, edits the restore would replace are set aside as described
under "Can I keep editing while senior-dev works?" below.

**A folder or file in it that senior-dev may not read is skipped**, not a reason to stop:
it is in none of its checkpoints, and nothing of it is changed or removed. senior-dev
needs no Full Disk Access; a folder macOS keeps to itself (`operation not permitted`) is
skipped like any other. It is never started on your home folder or a folder above it
(see the programs page): to check what it changed, it reads every file in the folder,
and your home folder is not one project.

senior-dev used to stop at once there with `workspace is not a git repository:
<folder>; run with --in-place to work in a plain folder`, which the chat could not act on.
It no longer does, whatever flags it is started with.

## Can senior-dev work in a gitignored folder inside a repo?

Yes. A folder git ignores inside a larger repository is worked in place like a
plain folder. codeaf does not widen it to the enclosing repository, cut or
delete a branch there, or commit any of its files. What senior-dev writes
stays in the folder. The ending says `its work is in <folder>; git ignores
this folder inside <repo>, so codeaf cut no branch and nothing was committed`.
The start receipt says `git ignores this folder inside <repo>, so codeaf cuts no
branch there and commits nothing`.

## Why can't codeaf edit files while senior-dev is working — the folder is senior-dev's while it runs, a write or a task refused, bash, your own editor

**In a git repository nothing is held.** senior-dev works in a copy of its own, so the
chat's tools, your editor, and tasks and senior-dev runs from other conversations or a
shell all keep working in your folder while it runs.

**A folder with no git history is senior-dev's until the run ends**, because it works in
the folder itself: once it has submitted, anything changed there is put back to what it
submitted and a file added there is removed after a copy is kept outside the folder. So
nothing else of codeaf's writes there meanwhile, from any conversation, window or shell:

- the chat's `write` and `edit`, `edit_video`, `workspace_restore`, `workspace_merge`,
  and a picture, music, video or speech saved there, including unnamed default
  outputs for `edit_video` and generation, are refused: `<file> is in <folder>, where
  senior-dev, task 4 (Fix the parser), is working, so nothing was written; wait for that
  run to end, or stop it, then write there`. Reading stays open.
- a task on that folder, inside it or around it — proposed, typed with `/task`, a quick
  task, or one whose turn to start comes — is refused before it starts: `<folder> is
  busy: senior-dev, task 4 (Fix the parser), is working in it, and nothing else of
  codeaf's works there until that run has ended; wait for it, or stop it, then ask again`.
  A repository inside that folder counts.
- a task already running when it started lands beside it: `its branch <branch> was kept:
  senior-dev, task 4 (…), is working in it — bring it in when that run has ended`. A
  `/land` of the chat's changes there is refused the same way, and waits.

**`bash` is not fenced**: codeaf cannot know what a command writes. **Neither is your
own editor**: what you save in a plain folder while it runs joins its work, or is put
back.

## Can I keep editing while senior-dev works?

Yes. **In a git repository your edits stay yours**: senior-dev works in a copy of its own,
so what you save in your folder is neither part of its work nor touched by it, and what
you commit on your own branch meanwhile stays there. In a folder with no git history it
works in the folder itself, and edits saved there can join its work. If a submitted change or an
earlier checkpoint has to be restored, codeaf first copies every changed tracked
file and new file not ignored when the run started that restore would replace into a rescue folder under
codeaf's state root, outside your project. The submitted candidate is then put
back. The ending says exactly: `Files that changed in the folder before senior-dev
restored its checkpoint were set aside in <path>`. The shell ending, the task's
end record and the chat's landing all carry that path. The path holds the bytes as they
were before the restore. A tracked file deleted after submission is named in
`deleted-files.txt` there, and the ending names that manifest. A later restore in
the same run has its own subfolder. With nothing to rescue, no folder is created
or named. If the copy cannot be made, nothing is restored and your folder is left
as it was; the run then does not say its build and tests passed, and the ending
adds `The folder changed after senior-dev's last check and could not be put back
(<why>), so it also holds later changes that nothing checked`.
If codeaf cannot compare the folder with the submitted candidate at all, it
also leaves the folder untouched and ends unchecked: `The folder could not be
checked against what was verified (<why>), so it may hold later changes that
nothing checked`.
Files git ignored when the run started are not committed even if senior-dev
changes `.gitignore`. Python `__pycache__/`, `.pytest_cache/` and `*.pyc` files
made by its checks are not committed either. Those files stay in your folder.

## What does a rescue copy, where is it kept, and how large can it be?

Before restoring a submitted candidate or checkpoint, senior-dev copies files
changed after submission that the restore would replace, including files newly
ignored by a rule added during the run. It records later deletions in
`deleted-files.txt`; links are kept as links, and files ignored when the run
started are left in place. The copy is under the state root at
`v3/carried/senior-dev/rescued/run-…` (normally
`~/.codeaf/v3/carried/senior-dev/rescued/run-…`), outside the project. If the
state root itself is inside the project, the rescue instead uses the machine's
temporary `codeaf-rescued/` folder so the restore cannot erase its own copy.
The rescue folder is private to your account (mode `0700`); copied regular files
and the deletion manifest use `0600`. Each copied file is limited to **25 MiB**
and one rescue to **250 MiB** total. If a file or the total exceeds the limit,
senior-dev refuses the restore before changing the folder; the ending says it
could not be put back and that the folder holds later changes nothing checked.

## Its notes — .senior-dev, its checklist, its session database, moved out when it ends

senior-dev keeps its own records in `.senior-dev/` in the folder it works in (its copy, in
a repository): the brief,
its checklist, the command it pinned, its session database and its whole conversation
with its model. **They are moved out of your folder when the run ends or you stop it**,
into the task's record folder beside `delegate-conversation.jsonl` (a shell run's record
folder at a shell), and the page adds `its notes (.senior-dev/) are kept in <path>`. So
they never end up on a branch, and the next run in that folder never reads the last
one's checklist as its own. A `.senior-dev/` already in the folder when the run began is
left where it is, and never ends up on a branch either.

## Where does senior-dev put its work — its own branch, in a copy of its own, not merged, not squashed

In a git repository, codeaf cuts a branch of its own for the run (`task/<title>-<id>`) from
the commit your checkout stands on, checked out in a private copy of the repository
(see where senior-dev's copy is), and senior-dev works there. senior-dev commits each file
it writes, except initially ignored files and test caches (`wip(write): <path>`,
`wip(edit): <path>`), on that branch, which is how it keeps a record to restore from; they
stay there, and nothing squashes them.

When the run ends — finished or not, stopped, crashed, or codeaf gone — codeaf commits
what it left uncommitted on that branch, excluding paths ignored at the start and known
test caches, in one commit whose subject is the task's title and whose body is senior-dev's
own ending, and **removes the copy**, so the branch is checked out nowhere and free to
merge, check out or hand back. **The branch is always kept**, even when the run changed
nothing. Nothing is merged into your own branch. The task's page and the conversation both
say ``its work is on the branch <branch> in <folder>, N files; your checkout was not
touched, and `git -C '<folder>' merge <branch>` brings it in``. Merge it when you are ready,
or ask the chat to.

The finishing commit's model credit names only models recorded as answering a call in
that run, including a model that answered in place of the one asked for. If no model
answered, there is no `Assisted-by` trailer. The attribution setting still decides
whether answered model names are shown.
If senior-dev runs `git commit` itself, the commit uses codeaf's run identity rather
than your Git identity. The `Assisted-by` credit is added only to a finishing
commit codeaf makes when there is something left to stage, and that holds for a run you
stopped and for one whose codeaf closed under it. It never amends, rebases or rewrites a
commit.

The ending keeps two witnesses apart: what senior-dev's model said it did
(`senior-dev's model said: …`) and what senior-dev saw when it ran the project's build
and tests (`senior-dev observed: …`). Read the second for "did it work".

**A run you stop keeps its work the same way**: the stop says `its work so far goes onto its
branch <branch> in <folder> as it stops` at once. **A run that changed nothing** says `it changed
nothing; its branch <branch> in <folder> is kept where it began, and your checkout was not
touched`.

## Where is senior-dev's copy — a git worktree in codeaf's cache folder, node_modules and .env linked in, removed when it ends

In a git repository senior-dev works in a git worktree of your repository under your
account's cache folder (`~/Library/Caches/codeaf/worktrees/<repository>-<random>` on a
Mac, `~/.cache/codeaf/worktrees/…` on Linux; `worktrees` inside `CODEAF_HOME` when that
moves codeaf's folder), private to your account and not emptied by a restart. It is not for you to open: it exists so several runs can work on one repository
at once and your checkout is never touched, and it is removed the moment senior-dev exits.
`git worktree list` shows it while the run works. A copies folder that is a link, or that
another account made, is refused rather than used.

**It is cut from your last commit**, so what you have not committed is not in it. **A few
folders git ignores are linked in from your folder**, because a fresh checkout lacks them
and a project's build and tests need them: `node_modules`, `.venv`, `venv`, `.env`,
`.envrc` and every `.env.*`, when they sit at the top of your repository and git ignores
them. Nothing else is: build output such as `bin/`, `dist/` or `target/` is never linked,
because two runs building into one folder corrupt each other. The links are removed before
anything is committed, and never on a branch. **What senior-dev's shell writes through a
link lands in your folder**: an `npm install` there updates your `node_modules`.

A link that your `.gitignore` ignores only as a folder (`node_modules/`) is named in the
repository's `.git/info/exclude`, between two `# codeaf:` lines, while a copy is on disk,
and codeaf takes those lines out when the last copy of the repository is removed.

The brief senior-dev reads opens with one line saying where its copy is, and that a path
the brief names under your folder is the same file in the copy.

## Which folders are linked into senior-dev's copy — program.links, .codeaf/config.json, Git LFS

**Name your own list** in the repository's `.codeaf/config.json`, as text or as a list:
`"program.links": "node_modules, .env, vendor"` or `["node_modules", ".env", "vendor"]`.
An empty one links nothing. **A list that does not apply refuses the run** with the file
named, rather than quietly linking the defaults: `<file>: program.links wants
comma-separated text, or a list of names, got …; fix it, then ask again`; a name that is
not at the top of the repository (`tools/bin`) is refused the same way, and so is a
`.codeaf/config.json` that cannot be read at all.

**Git LFS.** A copy is a fresh checkout, so a repository that keeps files in Git LFS needs
`git-lfs` on the PATH codeaf runs with, and a run without it is refused: `could not cut
senior-dev's copy of <folder>: … command not found; this repository keeps files in Git
LFS, and git-lfs is not on the PATH codeaf runs with: install it, or start codeaf where it
is on the PATH, then ask again`. With it, the copy is filled from the files your checkout
already has.

## When senior-dev's copy is kept — its copy is not removed, what it left could not be saved, delete the copy

**codeaf never deletes what senior-dev left unsaved.** When what it left in its copy can be
neither committed on its branch nor kept as a patch — git can no longer read the copy, or
the patch cannot be written — the copy stays where it is and the ending says so: `its copy
is kept at <copy>, because <why>: take what you need from it, then delete that folder, and
`git -C '<folder>' worktree prune` releases <branch>`. A lock file a killed git left in
the copy (`index.lock`) does not cause this: nothing can still be using the copy by then,
so codeaf removes it and commits. A copy that would not be removed says `its copy at
<copy> could not be removed, so <branch> is still checked out there`.

A copy left behind — codeaf crashed, or the machine restarted mid-run — is finished by the
next run on that repository, which also prunes git's record of it.

## Does senior-dev change my branch — your branch never moves, your checkout is never touched

No. In a git repository senior-dev works in a copy of its own, so codeaf never switches,
resets, commits on or merges into your checkout or your branch: after the run your folder
is exactly as you left it, and your uncommitted changes are still there. You can keep
committing on your own branch while it works. The work is on its own branch; `git -C
'<folder>' merge <branch>` brings it in, and the page names the exact command.

## What if HEAD moves to main or detaches, or my branch moves, while senior-dev is working?

senior-dev's shell can still run `git checkout` in its copy, and a brief that says "work on
a new branch" makes that likely. **So a brief need not ask for a branch: the work already
has one.** It cannot check out a branch your own checkout has (git refuses it). If its
copy is not on its branch when the run ends, codeaf commits nothing there: its branch keeps
what was committed on it, what was left uncommitted is kept as a patch in the run's record
folder, and the page says so: `senior-dev left its copy on the branch <other> instead of
its own branch <branch>, so codeaf committed nothing there; <branch> in <folder> holds N
files …; what it left uncommitted is kept as a patch at <path>`. Commits it made on no
branch are kept on a branch named `<branch>-detached` (`<branch>-detached-2` and on when a
run before it took that name), because the copy that held them is removed. A merge its shell left half done is never committed: it is kept as a patch the
same way.

**Your own branch may move freely**: you committing on it while senior-dev works is what
the copy is for, and nothing about it is reported or undone.

## Uncommitted changes, a dirty checkout, a merge in progress — senior-dev starts anyway and leaves them alone

senior-dev is no longer refused over changes you have not committed. It works in a copy cut
from the commit your checkout stands on, so **modified, staged or untracked files — and a
merge, rebase or cherry-pick in progress — stay in your folder, untouched, and are not in
its copy**. The receipt names them: `Your uncommitted changes (a.go, b.go, c.go and 2 more)
are not in its copy.` Commit them first if senior-dev's work needs them.

Until 2026-09-28 senior-dev worked in your checkout itself, and refused to start with
`<folder> has changes that are not committed (…); commit or stash them, then ask again`.

## Two senior-dev runs on one repository, the folder is busy — runs side by side, one run per plain folder, another window, a shell run

**Runs on one git repository work side by side**, from different conversations, windows or
shells: each has a copy and a branch of its own, and none holds your folder. **One
conversation runs one program run at a time**, though: a second hand-off in it while one
works is refused, `work is already underway in a copy of <folder>; senior-dev runs alone in
a conversation, so propose it again when that work has ended`. Start the other from a
second conversation or a shell.

**A folder with no git history takes one run at a time**, because senior-dev works in it
itself. A second is refused, naming the one working there: `<folder> is busy: senior-dev,
task 4 (Fix the parser), is working in it, and one folder takes one program run at a time;
ask again when that run has ended` (or `senior-dev, a run started at a shell`). **So are the
folders inside it, and a folder around it**, a repository inside it included: a run on a
folder of projects puts back whatever changed anywhere under it once it has submitted, so
`<folder> is busy: senior-dev, task 4 (…), is working in <held folder>, which holds it, …`
(`which is inside it` the other way round). Two runs in two folders side by side both go.

The hold goes with the codeaf holding it, however it ends, so a crash never leaves a
folder refused.

## What a senior-dev run costs — model calls, the dollar ceiling, which models

Every model call senior-dev makes goes through codeaf, which serves each run its own
model API. So every call is priced like one of codeaf's own, shows in the conversation's
total, its tokens and its call count, under `tasks` in `/cost`, and under the task on the
spend place. What the whole run came to is on its row, its landed card once opened
and the chat's `tasks` tool (`#3 · … · done · ran 22m 51s · $2.30 · via senior-dev`).
Before forwarding a call, codeaf reserves the larger estimate from the requested model
and its possible fallback seat when both have known prices, using the request's input size
and output cap (4,096 output tokens when none is named); if either price is unknown, it
uses the unpriced bound. It refuses a call whose estimate would cross the run's dollar
ceiling. In-flight calls can
finish above their estimates, so the final spend can exceed the ceiling by a call's cost.
The refusal says `the run's dollar ceiling of $5.00 is reached ($5.04 spent), so codeaf made no call`;
the shown spend is the money already charged, not the reserved estimate. A refused call ends senior-dev's turn; it runs the project's build and tests on
the tree it has, and ends there, and the task says
`senior-dev reached the run's dollar ceiling of $5.00: …` with senior-dev's own words
after it. A run handed off after the conversation's dollar limit is already spent starts
nothing and makes no call: its row ends at once with `a dollar limit you set stopped it`.
An estimated call may be refused while the metered spend is still below the ceiling;
that ending is still a dollar limit, its line gives the metered spend, and codeaf
refuses an automatic re-hand-off until you ask for one.
When a dollar or time limit ends the run, the conversation also gets a line naming the
limit, what the run spent and the branch or folder holding its work, even if that limit
prevents the chat from making a wake call.

The time ceiling is kept by senior-dev as well as by codeaf. It holds back the last part
of its time to land: two fifteenths of the run, at least 45 seconds, at most 12 minutes,
and never more than a quarter of it. When that window opens it gets one last turn to
submit.

**When none of your model services can serve the model it asks for**, codeaf answers the
call on the run's own work model — the one a task's own worker would use — and the raw
calls on the task page (`ctrl+y`) name the model that answered. When nothing here can serve
that model either, the conversation's own model may answer instead, and the page names
whichever model did. A dated build or a variant of the model it asked for, such as
`deepseek/deepseek-v4-pro-0731` or `qwen/qwen3.6-plus:free`, is that model and is not named
again; a sibling such as `openai/gpt-5.5-mini` answering for `openai/gpt-5.5` is a different
model and is named. Which models it asks for is the next section.

## senior-dev on a service that reports no prices — a local proxy, a Codex sign-in, unknown dollar cost

Some model services answer without saying what a call cost: most of the services you
connect in `/connect` besides the default router, such as a local proxy or runner, a
vendor's own API, or a plan you signed in to such as Codex. codeaf never guesses a price,
so each call
senior-dev makes through one is counted with its tokens and no dollars. The task page,
the rail and the spend place show no money for those calls, never `$0.00`, and a missing
price does not mean the service charged nothing.

**A missing price cannot become a dollar charge in the ledger.** To bound concurrency,
codeaf reserves half the run's dollar ceiling for a call with no known model price and
admits at most one such call in flight once the recorded spend reaches half the ceiling.
This limits simultaneous calls but cannot say what an unpriced service actually charged.
The run's wall-clock ceiling still ends it; use `--max-hours` at the shell
if you need a shorter or longer run.

## Why a stopped senior-dev run takes a moment to end — the price of the call it was in the middle of

When you stop a run, or codeaf ends it at its dollar ceiling, senior-dev is usually in
the middle of a model call. That call is still paid for, and the router prices a call cut
off like that by a receipt codeaf fetches afterwards, usually about twenty seconds later.
**The run is not over until that receipt is in**, for at most 70 seconds, so the task's
spend, the run's total and the conversation's `/cost` all include that call. A shell run
waits the same way before it prints its last line.

A receipt that never comes is kept as a call nobody could price, never as a free one
(the section `Was I charged for a reply that got cut off` says where those are counted).
A senior-dev call answered whole whose answer carried no usage block at all is asked
about the same way, including one codeaf then set aside because it was not usable text:
priced by its receipt, or kept as a call nobody could price. codeaf never guesses a figure
for either.

## Which models does senior-dev use — your crew, a model you ask for, its own list, --high

**Ask for a model and it works with that one.** Say which in the chat — "use senior-dev
with kimi-k2.6", or several: "with kimi-k2.6 and deepseek-v4-pro" — and senior-dev works
with exactly those, routing among them call by call when there are several; the card and
the task's first line name them. A name that fits more than one model is put to you to
settle. A model none of your connected services can serve is refused before the card, by
name, rather than swapped for another. When a catalog was loaded, a model it cannot size cannot be used: the
run ends before its first call with `senior-dev cannot work with <model>: …`, and nothing
is spent. If models.dev is unavailable and there is no cache, conservative limits let
the run start. The models are fixed when the run starts; changing the crew later does not move
a run already working. `/senior-dev` typed with a brief uses your crew.

**Otherwise, from the chat it uses your crew.** codeaf hands senior-dev the worker
(hands) model for its work and the low model for its history summaries. A pinned
worker is kept; with no worker pin, codeaf reads one profile worker recommendation
at the start of the run, independent of this task's brief — and "do this one properly" or
"do it cheaply" said in words, which the chat's hand-off carries, moves that
recommendation the way it moves codeaf's own worker. `/task --best` and `--cheap` start
codeaf's own worker, never senior-dev. The per-task worker,
planner and checker routing and `/crew`'s per-task limit apply to codeaf's own
tasks, not senior-dev's run. Change the crew and the next run follows. The
mastermind (brain) model is not used: every call senior-dev makes is either its
work or a history summary.
A crew model senior-dev's model catalog cannot size is left out, and its log says so;
if that leaves no working model, it uses its own list instead.

## Which models does a senior-dev shell run use — --high, fresh profile, no crew

**The same as the chat's.** A shell run without `--high` works on your profile's worker
seat, handed over the way a conversation hands its crew (`--crew --high <seat>`, and the
seat's own rung as `--variant` when it names one); if no connected model can fill that seat,
it says to widen or pin `/crew` models. Until 2026-09-28 a shell run routed on senior-dev's
own list instead, with the seat only as a fallback.

**Its own list** is six open models it routes among call by call, avoiding one for a
while after it fails: deepseek-v4-flash, deepseek-v4-pro, qwen3.6-plus, kimi-k2.6,
glm-5.1 and minimax-m2.7. A run whose crew models its catalog cannot size uses it.

An explicit `--high <model>` runs on a fresh profile with a provider key and no crew rows;
that model is used without resolving a profile seat. It accepts a bare OpenRouter id
(`z-ai/glm-5.3-flash`), a service-prefixed id (`openrouter/z-ai/glm-5.3-flash`), or
a short model word from the same list `/crew` uses (`glm-5.3-flash`). Each word is
resolved before senior-dev starts. A word that is ambiguous, unknown, or cannot be
served by a connected service is refused with `cannot use model "<model>" here;
choose one this service serves with /crew or add its service with codeaf connect`.

**At a shell you choose**: `--high` replaces the seat, `--low` sets the summaries' models,
and `--variant` sets how hard the coder thinks (see how hard senior-dev thinks).

## How hard does senior-dev think — reasoning effort, thinking, --variant, high, xhigh

**Its coder asks for `high` by default** on every call: senior-dev is handed long,
many-sided work, and each model's own default is tuned for a chat. The summaries it writes
of its own history send no effort at all.

**The chat can choose otherwise for one hand-off.** A proposal to senior-dev may carry
`thinking` — `low`, `medium`, `high`, `xhigh` or `max` — which the model sets only when the
work is plainly mechanical (lower) or plainly hard (`xhigh`, `max`); left out, the default
applies. Ask for it in words ("have senior-dev think as hard as it can") and the chat sets
it. The card does not show it; the run's raw calls (`ctrl+y`) do.

**What decides, nearest first**: the `thinking` a hand-off carried; then a rung written on
your worker seat (`tiers.worker` set to `vendor/model:high` — `low`, `medium` or `high`);
then `high`. At a shell, `--variant` decides, then the seat's rung, then `high`.
`--variant none` sends no effort, leaving the model's own default. `/effort`, the
conversation's own thinking level, does not reach senior-dev.

A model that refuses an effort has it dropped for that call rather than failing; `xhigh`
and `max` are asked as a thinking budget, which some models do not take.

## What a shell run prints at the end — how long senior-dev ran, what it cost, waiting for the last price, a closed terminal

At a shell, `codeaf senior-dev` first says where it works (`senior-dev · working in
<folder>, in a copy of its own on its own branch <branch>` in a repository), then prints each stage, step and
model call as it happens, then how the run ended, then where its work is (the sentence a
task's page says), then `the run's record is in` and the run's record folder, and last one
line with what it came to:

```
  277 model calls · $2.30 · 22m 51s
```

That is the calls, the dollars, and how long senior-dev's own process ran. A figure nobody
measured is left off, never written as a zero.

When ctrl-c or `--max-cost` stops the run in the middle of a model call, that call is still
paid for, and its price arrives by a receipt about twenty seconds later. The run waits for
it before those last lines, and says so on stderr:
`waiting up to 1m 10s for the price of 1 call that was cut short`. **A second ctrl-c leaves
at once** instead of waiting; its folder is already finished by then, and only a price
still owed is missing from the run's line and from this machine's spending ledger.

**A closed terminal or a dropped ssh connection stops the run the way ctrl-c does**:
senior-dev is stopped and its folder finished. If the codeaf running it is killed
outright, senior-dev sees within a second that it is gone and stops; its copy is then
finished by the next run started on that repository, which commits what it left on its
branch (see `If codeaf quits while senior-dev works`).

Every call is written to this machine's spending ledger, filed as one piece of work named
after the run's record folder (such as `20260924-150405.000000`). That folder also keeps
`delegate-program.json`, with the instant senior-dev's process started and the instant it
ended.

## senior-dev's flags — run, --variant, --in-place, --high, --max-cost

`codeaf senior-dev <brief>` is `codeaf senior-dev run -- <brief>`. codeaf gives every
program it carries four flags:

- `--dir DIR` — the folder to work in (the current one by default; inside a git
  repository, a copy of the repository's root);
- `--max-cost USD` and `--max-hours H` — replace the default ceilings for a shell run;
- `--json` — the program's records on stdout instead of readable lines.

senior-dev's own flags on `run`:

- `--variant NAME` — how hard the coder thinks: `low`, `medium`, `high`, `xhigh`, `max`,
  or `none` for the model's own default; `high` when unset. Its history summaries send
  none;
- `--in-place` — work without git even inside a repository: no commits, and its
  checkpoints kept outside the folder. A folder with no git history is worked that way
  without it; codeaf passes it itself under a repository at your home folder;
- `--high`, `--low` — comma-separated models it routes among; `--low` (its history
  summaries) falls back to `--high`. On a shell run, each `--high` entry accepts a
  bare OpenRouter id, service-prefixed id, or short `/crew` model word;
- `--asked` — the `--high` models were chosen by name, so one senior-dev cannot
  size ends the run before its first call rather than being skipped;
- `--frontier` — accepted, and changes nothing: no call senior-dev makes uses that tier;
- `--crew` — the models came from a conversation's crew: one its catalog cannot size is
  left out instead of failing the run. codeaf passes it with the crew's models.

`codeaf senior-dev help` describes it and its one command, `run`;
`codeaf senior-dev run --help` prints all of them, codeaf's four included.

## How long did senior-dev take — a run's time, the clock on its page, wall time

A senior-dev run is timed from the moment you handed it off — when its row first reads
`running`, after its copy is cut — to the moment senior-dev's own
process ended. Readying the folder before it, and committing what it left after it, are
not counted. A run whose senior-dev never started is timed to the moment the run ended.

Everything that shows the run's time shows that one span: the line under its page's title
(counting up from the hand-off while it runs, and stopped at senior-dev's exit once it has
ended, even before its last changes are committed), its row and card once it has ended, the note the
conversation is handed when it lands (`done · ran 22m 51s · …`), and the chat's `tasks`
tool (`#3 · <title> · done · ran 22m 51s · via senior-dev`, or `running for 3m` while it
goes) — so you can ask the chat how long it took. Each spells it the way the page does — `42s`, `22m 51s`,
`1h 7m` — except the landed card, which spells it `22m51s`.

The instants senior-dev's process started and ended are also kept in `delegate-program.json`
in the task's record folder, beside `delegate-stderr.log`.

**After a reopen.** A conversation closed and opened again still shows each run's time, how
it ended in senior-dev's own words (a `senior-dev did not finish: …` stays that sentence and
is not turned into a fault), which limit stopped it when one did, `stopped` when you stopped
it, and the branch its work is on.

**A run nothing is running any more.** If codeaf closed or crashed while senior-dev was
working, nothing is driving that run: its page reads `incomplete` rather than `running`,
its time stops at the last thing it did, and it offers no stop.

## Does another conversation or window see my senior-dev run — the @ list, other windows, the project's task list, watching it from another window

Yes. A senior-dev run takes a row in the project's task list the moment it starts, saying
running, and a second row closes it when it ends, with its time, how it ended, the branch
its work was kept on and what it cost. So the `@` list, another conversation's `tasks`
tool, the conversation list's task counts and every other codeaf window on the project
see it, and a window that has the run's conversation open says it is being worked on. The
conversation that started the run lists it once, by the number its rail shows.

**Another window can watch it, read-only.** On that window's tasks place the run's row
stands under `running` with `another window` beside it, and `enter read it as it runs`
opens the page the conversation that started it shows: senior-dev's actions under their
steps, the line over them with the step, the spend, the calls and the time, and `ctrl+y`
for its raw calls. The trail reads `reading in <that conversation>` and the box says
`Reading this task… (esc: main)`; `enter` over words answers `this window is reading this
task — go to the conversation that owns it to steer or stop it`. It offers no stop: only
the conversation that started the run can stop it. This works where the engine is local,
as every page read from another window does.

If codeaf went away while the run was working, its row is closed the next time that
conversation is opened, with the time the run had when it was last seen: it reads `codeaf
closed while senior-dev was running`, or the run's own ending when it had one. A run
senior-dev had finished but that codeaf closed under before the run was over reads
`incomplete — codeaf closed while this was still running`.

## Why did senior-dev stop — how a run ends, its log, crashed or stopped

A run ends in one of these ways, and the task's ending says which:

- `finished: …` — it submitted, and the words after say what the project's build and
  tests did on the frozen tree;
- `senior-dev did not finish: …` — it ended without submitting, or what it submitted fails
  the project's own build or tests. It is not drawn as a fault, what it made is still on its
  branch, and the chat acts on it (see what codeaf does when senior-dev ends);
- `senior-dev reached the run's dollar ceiling of $5.00: …` — codeaf refused a model call
  at the dollar ceiling; the words after are senior-dev's own ending;
- `senior-dev stopped on its own ceiling: …` — it stopped itself at the time ceiling;
- `senior-dev crashed: …` — the program itself broke, or could not start (no brief, a
  refused `senior-dev.json`);
- `stopped by the run: …` — you, or the run it belonged to, stopped it; what follows is
  what senior-dev said on its way out, usually `stopped before it finished`;
- `codeaf closed while senior-dev was running` — the codeaf holding its conversation
  stopped or crashed while it worked (see the next section);
- `senior-dev had ended; codeaf closed before it could say where its work is` —
  senior-dev had already exited, and codeaf stopped before it had finished its folder
  (see the next section).

When it ends without submitting, it still checks the tree it leaves. If the project's
tests cannot even start there, the tree is put back to the last state whose build and
tests could run, or to where it began.

## If codeaf quits while senior-dev works — closed, crashed, engine stopped, restarted mid-run, where is its work

senior-dev ends with the engine holding its conversation. Leaving a hosted conversation's
window only detaches: senior-dev keeps working. When that engine is stopped or crashes,
the conversation is closed, or a `--no-host` codeaf quits, the run is over: its page and
side-list row read `incomplete` with `codeaf closed while senior-dev was running`, no
stage, nothing waiting on you, and no fault; or `senior-dev had ended; codeaf closed
before it could say where its work is` if it had exited. A run senior-dev had finished
reads done, with its result.

Its copy is finished by the next codeaf that finds the run (the next section). **In a
folder with no git history nothing is committed**: its work stays in the folder as it left
it, and its notes are moved out.

**The run ends where it was last seen working**: senior-dev's exit, or else the end of its
last model call, its last charge, or its store's last change, whichever is latest. So its
time and spend do not count the hours codeaf was closed. An orderly close writes the ending
before senior-dev is stopped; after a crash the next codeaf that opens that conversation,
or hands work off in it, writes it.

**Nothing carries it on.** The next `/senior-dev` starts a run of its own, with its own
task and page; the old page stays as the record of what it did.

## Where senior-dev's work goes when codeaf quits — its copy finished later, committed when codeaf found its run had gone, the submitted candidate

**senior-dev is never cut off mid-edit.** codeaf hands its hold on the copy to senior-dev's
own process, so a codeaf that dies leaves senior-dev its 15 seconds to stop — put back
what it submitted, commit its last edits — and nothing touches the copy until it has gone.

**Then the next codeaf that finds the run finishes its copy**: the one that opens that
conversation, hands work off in it, or starts a run on that repository, a shell run
included. Nothing but senior-dev's own work can be in its copy, so what it left there is
committed on its branch and the copy is removed, releasing the branch: `its work so far is
on the branch <branch> in <folder>, N files, committed when codeaf found its run had gone;
`git -C '<folder>' merge <branch>` brings it in` — or `as its run had committed it before
it went away` when nothing was left to commit. The commit credits only models recorded as
answering a call in that run. Your checkout is not touched.

**A candidate it had submitted and not yet finished is kept.** If senior-dev stopped
between submitting and finishing, what it submitted is put on a branch of its own before
the copy goes: `what it had saved at refs/worktree/senior-dev/submitted, which its branch
does not hold, is kept on the branch <branch>-submitted`.

## senior-dev's log — delegate-stderr.log, agent-summary, a shell run's record folder

Everything senior-dev said while it worked (each stage and what it knew at the time)
is kept in `delegate-stderr.log` in the task's record folder, and every stage, step and
ending it reported — what its page draws — in `delegate-actions.jsonl` beside it. Its `agent-summary` there
adds up each of its agents' calls, time and cost; the cost is the price codeaf's model
API told it for each call, not a catalog estimate, and a call nobody priced adds nothing. A run started at a shell has
no task, so its record — that log, its conversation with codeaf, its actions, its stages
and when it started and ended — is kept in a folder of its own under
`~/.codeaf/v3/carried/senior-dev/`, one per run.
