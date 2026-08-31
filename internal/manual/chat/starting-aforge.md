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

The command you type is `aforge`. On screen the surface calls itself `openaf` —
that name appears at the top of `/help`, on the card that asks to connect an
account, and as the speaker heading in an exported conversation. They are the
same program.

## Starting it

| What you type | What you get |
| --- | --- |
| `aforge` | the home screen, over this directory's most recent conversation |
| `aforge chat` | the same thing |
| `aforge chat --session <path>` | that conversation, straight in, no home screen |
| `aforge resume` | the chat, opened on the picker of earlier conversations |
| `aforge chat --host devbox` | the chat here, the work on another machine |

**The very first launch on a machine with nothing configured** opens on a short setup
instead — your openrouter key, the crew, a daily ceiling; `enter` accepts, `esc` skips —
and then on the empty conversation. It is shown once, ever; the getting-started page has
the whole of it. Without a key in your shell or your profile, `aforge` still opens; a
`--once` or piped run with no key still stops at the door with
`aforge chat needs a model to talk with.`

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
| `--max-hours <n>` | with `--yolo`: how many hours it may carry its own work on |
| `--max-cost <n>` | with `--yolo`: how many dollars it may carry its own work on |
| `--one-model` | every text call this session makes runs on the session model |

`--yolo` does not make aforge unstoppable: a small set of destructive commands
and anything that acts in your name still ask, whatever the setting says. See the
permissions page.

## Leaving it running on its own · unattended · overnight · nobody watching

`--yolo` on its own only changes what it asks you about. It still stops when the
model stops talking — which is right when you are sitting there, because you are
the one who says what happens next.

Give it a budget as well and it carries its own work on:

    aforge chat --yolo --max-hours 6
    aforge chat --yolo --max-cost 20
    aforge chat --yolo --max-hours 6 --max-cost 20

Either number alone is enough; both together means whichever runs out first. You
can set them once for a whole run of launches with `AFORGE_MAX_HOURS` and
`AFORGE_MAX_COST`, and the flag always beats the variable.

With a budget, four things change, and only with a budget:

- **It writes down what finished means.** At the start it turns your ask into one
  `done when` sentence and shows it to you on a dim line. That sentence is fixed
  for the whole session — nothing it does later can rewrite it.
- **A stopped turn is looked at rather than taken at its word.** When it stops
  talking, it checks whether any piece of work came home unfinished, and whether
  the checks your work names still pass. If any of that is unmet it carries on by
  itself instead of going quiet.
- **A piece of work that came home unfinished starts its own next go.** You used
  to be offered a follow-up in your own words — with nobody there, that offer went
  nowhere. Now what was missing becomes the next brief. If the same thing stops it
  three times in a row it stops for good and says so.
- **It tidies up after itself before it says it is done.** It re-runs the checks
  in a fresh shell, then looks at every file it made: anything inside the folder
  it is working in is part of the answer and is left alone, and anything it wrote
  outside that folder is scratch and is deleted. It never touches a file it did
  not create, and it never touches one it only changed.

Without a budget none of that happens, and it tells you so in one line when it
starts.

If you are sitting there watching it, nothing above applies to you: your session
is exactly what it has always been, and nothing is ever deleted on your behalf.

`--max-hours` and `--max-cost` cannot travel over `--host` — the conversation is
built on the far machine, so set them there.

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
and future tasks cut their worktrees from that repository rather than from the
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

## Asking aforge about itself

Aforge ships with this manual compiled into it, and it reads it with a tool
called `manual` rather than answering about itself from memory. So "what can you
do?", "what does ctrl+b do?", "can you read a PDF?" and "why did you just ask me
that?" are all fair questions to type straight into the conversation. If the
manual has nothing on something, that usually means aforge does not do it, and it
will tell you so instead of inventing an answer.
