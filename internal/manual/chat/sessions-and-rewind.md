# Conversations, and taking a message back

## Taking a message back — rewind

Rewind cuts the conversation back to an earlier point and drops everything after it. Use it
when you phrased something badly and want to say it again better.

Two doors, and they do the same thing:

| Way in | What you do |
| --- | --- |
| The gesture | `esc`, then `esc` again within half a second |
| The command | `/rewind` (aliases `/undo`, `/back`) |

The first `esc` keeps its ordinary meaning — mid-turn it interrupts, at rest it does nothing
— and also arms rewind. The arming window is **500ms**. While it is warm, the hint slot says
exactly `esc again to rewind`. A stray `esc` after the window has lapsed changes nothing.
The command row for `/rewind` reads `take back a message · esc esc`.

Once you are in, the whole transcript on screen becomes the picker. A line is drawn across
it, everything below the line is washed out, and that washed-out part is what the cut will
drop.

Rewind edits what the model has been told. It does not undo work that was done. Read the
section on what rewind does not undo before you rely on it.

## What rewind does NOT undo — your files stay changed

**A rewind is an edit of the conversation, not of your workspace. Nothing on disk comes
back.**

In the code's own words: *"Files the dropped turn wrote stay written and commands it ran
stay run: this makes the model stop having been told something, which is what a person means
when they take back a badly-phrased instruction and types a better one. Nothing pretends the
work did not happen."*

So, plainly:

- No file is reverted. A file the dropped turn wrote or edited stays exactly as it was left.
- No command is un-run. A `rm`, a migration, a deploy, a push — all still happened.
- No git state is touched.

**The session file on disk is not rewritten either.** The journal is append-only. The cut is
recorded as one extra line — `{"type":"rewind","dropped":N}` — and the dropped lines stay in
the file exactly where they were. When the file is read back, that marker drops the last N
messages accumulated up to that point and the read continues; everything after the marker is
ordinary conversation. This is deliberate: *"a rewound turn really did run, and its tool
calls really did touch the workspace."* The journal stays *"a record of what happened rather
than of what is currently believed"*.

If you need work on disk undone, undo it yourself. Rewind will not do it for you.

## What a rewind point is

A rewind point is one legal place to cut. Cutting at a point drops that message and
everything after it.

There are two kinds:

- A **turn point** — something you said. Cutting here takes back your words and everything
  the saying caused.
- A **step point** — any other message boundary, such as after a reply or after a tool
  result. Cutting here keeps your instruction and drops only the last thing the model did.

Three things are never offered as points:

- **The very first message**, which is the system message.
- **A compaction note** — a message starting `[context compacted]`. Cutting to it would drop
  a whole resumed conversation while leaving the summary that replaced its beginning.
- **A boundary that would separate a tool call from its answer.** A kept reply whose tool
  calls were answered in the dropped part is a transcript no model provider will accept, so
  that boundary is left out of the list entirely. A call that was never answered anywhere is
  dangling before and after the cut and disqualifies nothing.

One quirk worth knowing: a background job's completion line rides on your side of the
conversation, so a rewind taken right after one drops the note, and a second rewind drops
the turn. The screen shows what was removed, so you can see this happen.

## Moving the cut line, and what the screen says

In rewind mode the keys are:

| Key | What it does |
| --- | --- |
| `↑` / `↓` | Walk whole turns |
| `←` / `→` | Step through the points inside the current turn |
| `enter` | Commit the cut |
| `esc` | Leave with nothing changed |

The mode bar prints exactly `↑↓ turns · ←→ steps · enter rewind · esc back`.

The cut opens on the last thing you said — the newest turn point — or on the newest point of
any kind when there is no turn point at all. `←`/`→` are bounded by the current turn, so a
step walk cannot leave it. `↑`/`↓` never fall off either end.

While the mode is up it takes **every** key. Nothing falls through to the draft box, because
the mode bar is standing where that box was. `ctrl+c` is read above it and stays the way
out.

The mouse can do everything the keys can: click any transcript row to move the cut, click
the cut line itself to commit. A click chooses the nearest point at or above the row you
pointed at, so a click never drops less than the row you aimed at. A click above every point
takes the oldest one. Nothing here is pointer-only.

**What you see:** the bar that replaced the draft box reads `⟲ drops 2 turns` — the glyph,
the word `drops`, and the count. A cut landing on a turn boundary is counted in turns; a cut
inside the newest turn takes no whole turn and is counted in steps instead. On a
screen-reader ("linear") palette the glyph is `<<`. On a frame too narrow for both, the key
legend goes and the count stays.

The cut line itself is a horizontal rule drawn above the chosen block, labelled
`⟲ rewind here` (`<< rewind here` in linear). Everything from the line down is repainted
dim — accents, diff colours and all — because "all of this goes" is the true statement.

## What happens when you press enter on a rewind

In order: the drop count is taken, the cut is made, the drawn conversation is thrown away
and rebuilt from the session's own transcript (the same path a resume uses), and one dim
note is added reading `⟲ rewound · 2 turns`.

Then the draft box is filled for you:

- For a **turn** cut, the message that was taken back is put back into the draft box, so you
  can say the same thing better without retyping it.
- For a **step** cut, the draft you had stashed on the way in comes back instead.

Everything keyed to a position in the old conversation — the selection, the pointer, the
folds, the live block — is dropped in the same breath.

If the cut is refused, the mode stays up and the refusal is printed in the bar, so you can
press `enter` again a moment later.

Files, commands and git state are untouched by all of this. Only the conversation changes.

## When rewind refuses or does nothing

**"nothing to rewind".** The hint slot shows exactly `nothing to rewind` for **2.5 seconds**
when the mode cannot open at all — the session has no rewind ability, or there is no legal
place to cut.

**`esc` `esc` does nothing.** `esc` will not arm rewind when something else on the surface
holds the keyboard. That is: the settings sheet, the deck, the expand view, the model
picker, the resume roster, the connections panel, the command menu, the completion list, the
welcome box, copy mode, a recall walk, an open room, the rail hold, fullscreen rail, an
approval question, an awaited task proposal, an active guard, any pending connect ask, and a
pending harness offer. Close or answer that thing first.

**The sentences a rewind can come back with:**

```
session: a turn is in flight; interrupt it first
session: nothing to rewind
session: agent is closed
session: 12 is not a rewind point; the nearest is 10
```

- `session: a turn is in flight; interrupt it first` — a turn is running, or the
  conversation is being compacted. This is a refusal rather than a wait on purpose:
  interrupt first, rewind after, which is one keystroke.
- `session: nothing to rewind` — a session nobody has spoken to yet, or one whose whole
  transcript is a compaction summary.
- `session: agent is closed`.
- The "not a rewind point" sentence names the nearest legal point. The index is never
  rounded to it for you.

## Where your conversations are saved, kept, and stored

Every conversation is one folder on disk, and the transcript inside it is one JSONL file.
The path shape is:

```
~/.aforge/v3/projects/<workspace with separators turned to dashes>/<session id>/transcript.jsonl
```

The workspace part replaces `/` and `:` with `-` and always starts with a `-`, so
`/home/me/code/app` becomes `-home-me-code-app`. The workspace itself is the repository
root, so the same project opened from any of its subdirectories is one project. The
session id is 16 hex characters and names both the folder and the transcript's header.

Beside the transcript, in the same folder: `meta.json` (what the conversation is called,
which workspace it is about, when you last spoke in it), `state.json` and `tasks.json`,
the task transcripts, and — for a conversation with no project of its own — `work/`, the
directory it works in. Removing one conversation is removing one folder.

The directories are created with mode `0700`; the transcript itself is `0644`. The
surface's own files — model cache, input history, drafts — sit in `~/.aforge/v3`.

Coming back with no arguments opens the conversation **you spoke in most recently**, not
the file that was written to most recently: work finishing in the background does not
change which conversation you were having. A conversation you opened and never said
anything in is reused rather than piled up, and the leftovers are cleaned away.

**What is in the file:** JSONL, append-only, one header line and then one line per
**completed** message and per compaction pass. The line types are `session` (the header:
version, id, working directory, model, timestamp), `message`, `compaction`, `rewind` and
`title`. Message content is written as text.

The session id is 16 random hex characters, minted when the file is created and replayed
unchanged on every resume — that is what keeps one conversation one identity across days.
The first header wins when the file is read back. The title is its own appended line rather
than a header field, because the name is not known until the first turn has been answered;
the last title line wins, so renaming never rewrites the file.

Attached pictures are stored as **references** — path, sha256 digest, media type — never as
bytes. On a resume the file is re-read only when its digest still matches. Anything moved,
deleted, edited, unreadable or over the size limit becomes a text placeholder reading
exactly `[image /path/to/file — file changed or gone]`.

## Picking up where you left off

Launching with no arguments resumes **this directory's most recently written conversation**,
chosen by file modification time. A directory that has never held one gets a fresh file.

`aforge chat --session <path>` takes the path you named as given — *"a path a person named
is a path they mean, existing or not"*. `~` is expanded and the directory is created. The
launch reports it as resumed only if the file already existed.

Reopening a conversation is a forward read of the journal with no rewriting. It starts after
the latest compaction marker, using that marker's summary as the context. A line that does
not parse is skipped rather than being fatal. A `rewind` line drops that many messages and
the read continues.

Two repairs then make the transcript legal to send again:

- An unanswered trailing tool batch is dropped, together with any partial results it wrote.
- A tool result with no call above it is dropped.

Without these, a session killed mid-batch would be rejected by every model provider forever
— *"a file that can never be resumed."*

One thing is refused loudly: a file written by a newer aforge.

```
session file: <path> was written by a newer aforge (format version 3; this build reads 2)
```

**The welcome box.** On the first frame of an empty session, aforge offers the four most
recent conversations under the heading `recent sessions` (`no recent sessions` on a fresh
machine). With the draft empty, `↑`/`↓` walk the list and `enter` opens the highlighted one;
a click on a row opens it too. The hint slot says `↑↓ recent · enter open`. Each row is a
name and a coarse age (`now`, `12m`, `3h`, `5d`, then a date like `16 Aug`). The box shows
**once** — the first submit, key or click retires it for the life of the surface, and
nothing brings it back. It never shows over a resumed conversation, and it draws nothing at
all on a frame under 12 rows or under 40 columns.

## /resume — opening an earlier conversation

`/resume` (alias `/sessions`) opens the picker of earlier conversations in this directory.
From the shell, `aforge resume` opens the ordinary surface with the picker already up.

A filter box takes the place of the input line, with up to **10** rows under it. `↑`/`↓` to
move, `enter` to open, `esc` to cancel. The empty filter box shows
`filter · ↑↓ · enter open · esc cancel`.

Rows are ranked over the name and the description together, so typing `migration` finds a
conversation that was never named that. Each row is a name, then what was last happening in
it, then a dim age. The age is reserved first and never truncated. The cursor opens on the
conversation this window is already in, which is also the marked row. File names and ids
appear nowhere.

The name of a conversation is the title it gave itself; failing that, the first seven words
you said; failing that, the file name with `.jsonl` stripped.

There is **no argument form** of `/resume`. A conversation is named by a title the model
wrote and lives in a timestamped file, so the only honest way to ask for one is to be shown
them.

The list is read without locking anything: open, scan, close. It never writes and never
creates a file, so it can show a conversation another window is holding open. One known
staleness: the list reads what the file says rather than the transcript a resume would
rebuild, so a rewind with nothing typed after it leaves the taken-back message as the row's
description.

## When /resume refuses, and what aforge resume does

`/resume` (alias `/sessions`) opens the picker of earlier conversations in this directory.
Here is what it says when it cannot do what you asked.

- A directory with no conversations gets a note and no overlay:
  `No sessions yet — start one with aforge chat`.
- A surface that cannot resume says `resuming is unavailable here`.
- A filter that matches nothing draws `  no session matches`.
- `enter` on the row you are already in does no work and notes `already here · <Name>`.
- `aforge resume` takes no positional arguments and refuses `--once`:
  `aforge resume opens the session picker; for one headless message use: aforge chat --once "text"`.
  Its usage line is
  `usage: aforge resume [--model slug] [--reasoning level] [--host host[:path]] [--no-compact] [--yolo]`.
- `aforge resume` does not resume anything by itself. The surface opens exactly as bare
  `aforge` does, on this directory's most recent conversation, with the picker over it — so
  `esc` lands you where you would have been anyway.

Opening a conversation interrupts any running turn, closes the current one (a failed close
notes `close failed: <err>`), clears everything that belonged to it — transcript, selection,
pending questions, follow-ups, folds, meters — and replays the new journal. It ends with a
note reading `resumed <path>`.

A conversation another window is holding open is reported rather than worked around. See the
section on two terminals in the same folder.

## Two terminals in the same folder

**Yes, you can run more than one aforge at once in the same workspace.** Each gets its own
conversation file. What they cannot do is share one.

Opening a journal takes a non-blocking exclusive lock on the file before anything is
replayed, so the second window fails at the door rather than paying for a replay it cannot
use. But it does not fail *you*. The second launch quietly names a new session file, opens
that instead, and shows one notice reading exactly:

```
session open elsewhere — started a new one
```

In a `--once` run the same notice goes to stderr as `<notice>: <new session file>`.

**There is one place this fallback deliberately does not apply: the resume picker.** If you
pick a conversation another window is holding open, the error is reported rather than worked
around — *"a person who picked a conversation by name means that one"*. The surface prints
`resume failed: ` followed by the locked-file error, which names the file:

```
session file: /path/to/20260817-101112_a3f2.jsonl is open in another aforge
```

The underlying sentence reads `session file is open in another aforge`.

Close the other window, or start a new conversation instead.

A filesystem that cannot take this kind of lock at all — some network mounts — opens the
session unlocked rather than refusing it.

## Will I lose this if it crashes?

**The journal is the safety.** Lines are appended as each message completes, unbuffered, one
line per message. A crash mid-write costs the last line and nothing before it. A line that
will not parse is skipped when the file is read back rather than being fatal. A session
killed mid-tool-batch is repaired on the next open rather than being permanently
unresumable.

A failed write is dropped rather than raised at you — there is nothing useful you could do
about "the transcript did not save" in the middle of a turn. Closing the session is what
reports the state of the file.

**A crash leaves nothing to clean up.** The lock on a session file is an OS-level lock, and
the kernel releases it when the holding process dies, however it dies. There is no pid file
and no staleness check: after a crash the next launch opens the file again.

**Closing cleanly** — `/quit`, `ctrl+c`, or any other road out — disarms the idle timer,
drops queued follow-ups, closes the wake lanes, cancels the turn with a grace wait, shuts
down background jobs — which is where a task still running ends, a harness still being
designed among them — and finally syncs and closes the journal, releasing the lock. Calling it twice is safe, and a session that returns by any
other road still flushes the file.

**A dropped connection**, when the session is running on another machine, loses nothing that
reached the journal. The far machine is the only writer of its file and the surface holds
nothing that is not in it. Every outstanding call fails and every open stream is closed with
an error, so no turn is left spinning, and the message names the one thing to do:

```
the connection to devbox is gone — run the same command to pick the conversation back up
```

That is not advice dressed up: the conversation is on that machine's disk and the same
command opens it again. Running the session elsewhere with `--host` has its own page.

## What is never written down

Some things live only in memory, and a resumed conversation does not have them.

- **The model's thinking.** Only the text of a message is written to the journal. The
  reasoning behind a reply is not kept, so a resumed conversation shows you the answers, not
  the thinking that produced them.
- **Anything mid-turn.** A line is written when a message **completes**. A reply that was
  still streaming when the process died is not in the file.
- **Image bytes.** Pictures are journaled as a path and a digest. If the file has moved or
  changed, the resumed conversation carries the placeholder
  `[image /path/to/file — file changed or gone]` instead.

Two things are kept on your behalf rather than the conversation's, and they survive
independently of it: your input history at `~/.aforge/v3/history.jsonl`, and your unsent
draft, which is kept per workspace. Neither is ever waited for — a history file that cannot
be opened costs you the up arrow and nothing else.

## Starting a fresh conversation with /new

`/new` (aliases `/clear`, `/clean`, `/reset`) closes this conversation and starts a fresh
one in the same directory, with a brand-new session file.

What carries over: the settings the session was launched with, and **the approval gate as it
stands right now** rather than as it stood when aforge started. If you have changed what is
allowed during this conversation, the new one begins with that. A conversation opened from
the resume picker is built the same way, for the same reason.

What does not carry over: the conversation itself. The new session starts empty — none of
the messages, none of the context, no memory of what was said. The old conversation is not
deleted; it is still on disk and still in the `/resume` list.

Nothing on disk changes. Files written and commands run during the old conversation stay
exactly as they were.
