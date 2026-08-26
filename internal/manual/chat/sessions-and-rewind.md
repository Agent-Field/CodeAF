# Conversations, and taking a message back

## Taking a message back — rewind

Rewind cuts the conversation back to an earlier point and drops everything after it. Use it
when you phrased something badly and want to say it again better.

**There are two tiers, and they answer two different questions.**

| Way in | What you get |
| --- | --- |
| `esc`, then `esc` again within half a second | The **quick** inline mode: a cut line drawn through the transcript already on screen |
| `/rewind` (aliases `/undo`, `/back`) | The **rewind timeline**: the whole conversation as a fullscreen list, with a search and a preview |
| `tab`, from inside the inline mode | Lifts the inline mode into the timeline, carrying the cut you had already chosen |

The first `esc` keeps its ordinary meaning — mid-turn it interrupts, at rest it does nothing
— and also arms rewind. The arming window is **500ms**. While it is warm, the hint slot says
exactly `esc again to rewind`. A stray `esc` after the window has lapsed changes nothing.
The command row for `/rewind` reads `go back to an earlier point · esc esc takes back the last`.

Both tiers use the same `⟲` glyph, the same "drops N turns" arithmetic, the same cut, and
do the same things afterwards. Which one to reach for: `esc esc` for "not that, let me say
it again", `/rewind` for "take us back to before we started down this road".

Rewind edits what the model has been told. It does not undo work that was done. Read the
section on what rewind does not undo before you rely on it.

## Seeing the whole conversation — the rewind timeline

`/rewind` opens a fullscreen page listing the **whole** conversation, oldest first, with no
scrolling needed to get at the old end of it. This is the one that can reach turns the
inline mode cannot: the inline mode picks out of the transcript **drawn on screen**, and a
conversation you came back to opens showing its last **40** blocks. Scrolling up pulls the
older ones in 40 at a time until you reach the first message (see the screen page), so the
inline mode reaches as far back as you have scrolled — and `/rewind` reaches the whole thing
without your having to.

What is on it:

- The head: `⟲ rewind — pick where the conversation goes back to`, with `esc close` at the
  right.
- A **preview** of the point you are on — your message in full, wrapped, up to **3** rows,
  with `… N more lines` if it runs longer. A step point shows the row the cut begins at
  instead.
- The list. One row per thing: your messages marked `›`, the model's replies as their first
  line under them, and each tool call as a dim `[read: internal/parse/lex.go]`.
- The foot: `⟲ drops 2 turns — everything below the pick is let go`, then the keys.

Everything from the pick down is drawn **dim**, and it moves as you move, so the page is
always showing what the next rewind would let go.

**It works on a `--host` session too.** The list, the points and the cut all travel over
the wire, so a conversation you are driving on another machine rewinds exactly like a local
one. A dead connection lands in the foot as an ordinary refusal, with the page still up.

## The keys on the rewind timeline, and searching your messages

| Key | What it does |
| --- | --- |
| `↑` / `↓` | Move between points |
| `pgup` / `pgdown` | Twelve rows at a time |
| `home` / `end` | The oldest point, the newest |
| any letter | Searches — see below |
| `enter` | Places the pick; pressing it again on that same point does the rewind |
| `esc` | Clears the search first, closes the page second |
| mouse | Click a row to place the pick, click the placed point again to rewind; the wheel moves the cursor |

**Enter is two-stage on purpose.** The first `enter` places the pick and the foot changes to
`esc close · ↑↓ move · enter again rewinds here`; the second `enter` on that same point makes
the cut. Moving the cursor takes the placement back, so a destructive key never sits waiting
under a row you have walked away from.

**Typing searches.** Every printable key goes into a search over what you said, the model's
replies and the tool calls' arguments — so `lex.go` finds the turn that edited it. The foot
shows `search · lex.go`, and adds ` · nothing matches` when the search has emptied the page.
`backspace`, `ctrl+u` and `ctrl+w` edit it. `esc` clears it before it closes anything. Any
row a search kept can be picked, whether it is a message, a reply or a call.

**What a turn cost.** A turn that used tools carries a dim tally after its words, like
`4 tools · 2 files`. `files` counts the distinct paths that turn's `write` and `edit` calls
named. A turn that called nothing says nothing at all — not `0 tools`. There are no
timestamps and no costs on these rows, because the transcript this page is built from does
not keep them, and aforge will not invent them.

## Jumping to an old message from far back in the conversation

Open `/rewind`, type a word you remember from the message, and press `enter` twice. That is
the whole route. The search reaches the entire conversation, not just the part drawn on
screen, which is why `esc esc` is the wrong tool for anything older than the last screenful.

`tab` inside the inline mode does the same thing without retyping the command: it lifts you
onto the timeline with the cut you had already chosen, and your half-written draft comes
with you. The inline mode's own legend names the key.

If the cut is refused, the page stays up with the engine's sentence in the foot, and the
same `enter` retries it a moment later.

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

Rewind will not undo work on disk for you. **On a machine with furrow installed and watching
this folder, `workspace_restore` is the separate verb that does** — ask for the files to be
put back and aforge uses the workspace's own restore points, which cover things git never
sees: `.env`, a dev database, an untracked file, a dependency that changed. It is a
different thing from a rewind and they are never the same gesture: a rewind edits the
conversation and touches no file; `workspace_restore` moves bytes and leaves the
conversation alone. See *Forking and syncing a workspace*.

Without furrow there is no such verb, and then the sentence above is the whole truth: undo
it yourself.

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

## Moving the cut line in the quick inline mode, and what the screen says

This is the `esc` `esc` mode — the transcript on screen with a line drawn through it. The
keys are:

| Key | What it does |
| --- | --- |
| `↑` / `↓` | Walk whole turns |
| `←` / `→` | Step through the points inside the current turn |
| `enter` | Commit the cut |
| `tab` | Lift into the rewind timeline, carrying this cut |
| `esc` | Leave with nothing changed |

The mode bar prints exactly
`↑↓ turns · ←→ steps · enter rewind · tab the whole conversation · esc back`.

The cut opens on the last thing you said — the newest turn point — or on the newest point of
any kind when there is no turn point at all. `←`/`→` are bounded by the current turn, so a
step walk cannot leave it. `↑`/`↓` never fall off either end.

While the mode is up it takes **every** key. Nothing falls through to the draft box, because
the mode bar is standing where that box was. `ctrl+c` is read above it and stays the way
out — it does not leave rewind, it arms the door underneath, and a second press within
1.5 seconds quits aforge with the mode still up.

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

This is the same for both tiers — the inline mode's `enter` and the timeline's second
`enter` land in one place.

In order: the drop count is taken, the cut is made, the drawn conversation is thrown away
and rebuilt from the session's own transcript (the same path a resume uses), and one dim
note is added reading `⟲ rewound · 2 turns`.

Then the draft box is filled for you:

- For a **turn** cut, the message that was taken back is put back into the draft box, so you
  can say the same thing better without retyping it.
- For a **step** cut, the draft you had stashed on the way in comes back instead.

Everything keyed to a position in the old conversation — the selection, the pointer, the
folds, the live block — is dropped in the same breath.

If the cut is refused, the mode — or the timeline — stays up and the refusal is printed
where its own feedback goes, so you can press `enter` again a moment later.

Files, commands and git state are untouched by all of this. Only the conversation changes.

## When rewind refuses or does nothing

**"nothing to rewind".** The hint slot shows exactly `nothing to rewind` for **2.5 seconds**
when neither tier can open at all — the session has no rewind ability, or there is no legal
place to cut. `/rewind` will not raise an empty timeline.

**`/rewind` opens nothing at all** in six states, silently: the timeline is already open, the
inline mode is already on, copy mode is on, a task room is open, the settings panel is open,
or the task rail is full. The commands page lists them.

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
**completed** message, per compaction pass, and per piece of spending. The line types are
`session` (the header: version, id, working directory, model, timestamp), `message`,
`compaction`, `rewind`, `title` and `usage`. Message content is written as text. A `usage`
line records what one piece of work cost, and on a resume those lines are added back up —
which is why the bill survives a restart.

The session id is 16 random hex characters, minted when the file is created and replayed
unchanged on every resume — that is what keeps one conversation one identity across days.
The first header wins when the file is read back. The title is its own appended line rather
than a header field, because the name is not known until the first turn has been answered;
the last title line wins, so renaming never rewrites the file.

Attached pictures are stored as **references** — path, sha256 digest, media type — never as
bytes. On a resume the file is re-read only when its digest still matches. Anything moved,
deleted, edited, unreadable or over the size limit becomes a text placeholder reading
exactly `[image /path/to/file — file changed or gone]`.

## Is my spend written down — the usage lines in the file

Yes. Every piece of work that cost money appends a `usage` line to the transcript, carrying
the model it ran on, tokens in and out, cache read and cache write, the money, how many
requests it took and how long it lasted.

There is one line per **completed turn**, and one more for each call made beside a turn —
naming the conversation, the memory reflex, the check on a reply, a task's fold-up, a
planner, a harness run. Those are marked `aux` so a reader can tell the turn itself from
the work done around it. A turn that spent nothing writes no line at all, because a zero is
absence, not a fact worth a line.

**On a resume these lines are added up, and that sum is the session's totals.** So `/cost`,
`/status` and the status line show what the whole conversation has spent across every
restart, not only what has happened since you reopened it. The spend ceiling reads the same
sum — see the spending pages for what that means the day after you reach one.

The file version is still `1`. A file written before `usage` lines existed opens exactly as
it always did, and a file that has them opens in an older build too, which skips the lines
it does not recognise.

## Picking up where you left off

Launching with no arguments resumes **this directory's most recently written conversation**,
chosen by file modification time. A directory that has never held one gets a fresh file.

It opens showing the last **40** blocks of that conversation rather than all of it, which is
what keeps the first frame quick. Everything older is still there and still reachable:
aforge keeps a local copy of the transcript and prepares the previous 40 blocks before you
reach the top — when the first visible line is within one screen of the oldest part already
drawn. The page arrives between frames and is put above what you are reading without moving
that line. Keep scrolling and this repeats until you are at the first message. While there is
more above you the top row says `· earlier · keep scrolling`. The screen page has the keys.

**This is local even with `aforge chat --host <machine>`.** The conversation and its files
stay on the other machine, but the transcript shown by this window is mirrored on the machine
holding your terminal. A scroll key or wheel movement never waits for ssh; several trackpad
reports arriving inside one frame are applied together. New conversation work still crosses
the connection in the ordinary way.

**A compaction in the middle of it does not shorten what you can read.** A pass rewrites the
conversation for the model and writes the rewritten version back into the file below its
marker — so the file holds the session twice, once as it happened and once shortened.
Scrolling up is given the **original**, and one dim line is drawn where the two meet:

```
· above here the model keeps a shortened record — you can still read it all
```

Above that line you are reading the file, not the model's context: the words are the ones
that were said, and the model's own copy of them is shorter (old tool results are pointers,
long runs of its work are one line). Ask about something above the line and it may answer
from the shortened version. See the screen page for the whole of that line's meaning.

This needs the pass to have written down **how much it wrote back**, which aforge started
recording with this version. A session compacted by an older aforge is drawn from the
shortened copy, exactly as it always was, and gains the fuller history the next time it
compacts — showing both copies would print the whole conversation twice.

`aforge chat --session <path>` takes the path you named as given — *"a path a person named
is a path they mean, existing or not"*. `~` is expanded and the directory is created. The
launch reports it as resumed only if the file already existed.

Reopening a conversation is a forward read of the journal with no rewriting. It starts after
the latest compaction marker, using that marker's summary as the context. A line that does
not parse is skipped rather than being fatal. A `rewind` line drops that many messages and
the read continues.

The same read also keeps what is **above** that marker — the conversation as it stood one
instant before the pass edited it — for scrolling back into. It is history and never
context: nothing is ever sent to a model from it. It is built with the same read and the
same rules, so a turn a `rewind` took back before the compaction stays taken back rather
than coming back from the dead. A file compacted more than once keeps the region before the
**latest** marker; it still opens on the conversation's first words, because a pass never
shortens anything you typed.

The `compaction` line carries a `window` count: how many message lines the pass wrote back
below it. That is what says where the shortened copy ends and new conversation begins, so
the screen can draw the conversation once. A marker without it — every one written before
this version — leaves the region unusable, and the transcript below the marker is all the
screen shows, which is what it always showed. The count is never guessed at from the
`stubbed` and `folded` figures: those are a record of what a pass did, and a length derived
from them would silently draw somebody's conversation twice the day a pass changed.

Two repairs then make the transcript legal to send again:

- An unanswered trailing tool batch is dropped, together with any partial results it wrote.
- A tool result with no call above it is dropped.

Without these, a session killed mid-batch would be rejected by every model provider forever
— *"a file that can never be resumed."*

One thing is refused loudly: a file written by a newer aforge.

```
session file: <path> was written by a newer aforge (format version 3; this build reads 2)
```

**The empty screen's greeting.** On the first frame of an empty session, aforge draws one
centred group — the wordmark, the model line, the message box itself and a line of things
to try — and, when this folder has earlier conversations, up to four of them under the
heading `recent sessions` beneath it. On a fresh machine that list is simply absent: no
heading, no `no recent sessions` line. With the draft empty, `↑`/`↓` walk the list and
`enter` opens the highlighted one; a click on a row opens it too. Each row is a name and a
coarse age (`now`, `12m`, `3h`, `5d`, then a date like `16 Aug`). The greeting shows
**once** — the first submit, key or click retires it for the life of the surface, and
nothing brings it back. It never shows over a resumed conversation, and it draws nothing at
all on a frame under 12 rows or under 40 columns. *The empty screen* page has the whole
of it.

**When home greets you instead, there is no welcome box at all.** On a machine that holds
a conversation other than the one your launch opened, the first frame is the home screen
(see the home page), and the box is retired before it ever draws — home's list is every
conversation in every project, which is the box's four recent rows and more. Two
greeters would be one too many. It does not appear behind home either: `esc` out of home
lands you on the ordinary prompt.

So the greeting is what a **first run** sees — the launch where home has nothing to say,
because the only conversation on the machine is the one already on screen. That is the one
case where the wordmark and the centred message box greet you; home itself is still a
`space space` away. On a profile with nothing configured yet, the once-only setup screen
comes first (the getting-started page), and the greeting arrives the moment it closes.

## Why does the line above my box say something I did not type — who names this conversation, and can I rename it

The name is written by a model, once, and appears in the status line below the message
box: `porting the parser · gpt-4.1-mini:high`.

**It arrives one turn in.** As soon as your first exchange finishes, the model on the
`title` role — small work, so a cheap one — is shown the opening question and answer and
then asked, at the end of that same message,
`Name this session in ≤8 words, lowercase, no quotes. Answer with the name only.` That is
**one call per conversation** — it is never retried inside a conversation, so a provider
having a bad minute costs you a name and nothing else. Until it lands, the status line
falls back to the folder's name; nothing says
"untitled".

The name is capped at **80 characters**, and the status line fits it to the room left by
the model rather than letting identity push telemetry off the frame.

**There is no command to rename a conversation.** The name lives in the transcript as its
own appended line, and the last one wins when the file is read back — but nothing on this
surface writes a new one. If the name is wrong, nothing about the conversation is wrong
with it.

A name that arrived from somewhere else as one welded token — `port_b_parser_fix`,
`fix-the-nil-map` — is read back as words where it is drawn (`Port B Parser Fix`) and left
exactly as it is in the file. A name that is plainly an id — a timestamp, a hex tail — is
never prettied.

## Why is my session called "name this session in 8 words" — a name thrown away, and the opening words that stand in

Because the model that was asked to name it answered with the question instead of the
answer. The small models the `title` role lands on sometimes hand the instruction straight
back, and a conversation on this machine really was called
`Name This Session in ≤8 Words, Lowercase, No Quotes` for a whole evening.

**That answer is now thrown away.** A name is refused when it repeats the instruction — the
session namer's or the one that names a piece of work — or when it is only throat-clearing
with nothing behind it: `Sure, here is the title:`, `Title:`, `sure`. A leading label is cut
off first, so `Title: tokenizer speed` is kept as `tokenizer speed`; a colon you meant
survives, so `fix: nil map crash` is kept whole.

**A refused name is not a blank row.** The conversation simply has no name of its own, and
the lists that draw a name — home, `/resume`, `recent sessions` — fall back to **your own
opening words**, the first line you typed, exactly as they do for a conversation whose
first turn has not finished yet. The legend above the message box shows only the branch
until a real name lands.

**The ones already named badly heal themselves.** A transcript or a folder that was written
down under the instruction is read back as having no name at all, and the folder's row gets
your opening words back — they are read out of the transcript, where they have been all
along. Nothing is rewritten: the old line stays in the file, which is append-only. The next
time you open that conversation the namer gets its one call again, on your next completed
turn, appending the good name the way every name is appended.

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
  `usage: aforge resume [--model slug] [--reasoning level] [--host host[:path]] [--no-compact] [--yolo] [--one-model]`.
- `aforge resume` does not resume anything by itself. The surface opens exactly as bare
  `aforge` does, on this directory's most recent conversation, with the picker over it — so
  `esc` lands you where you would have been anyway.

Opening a conversation interrupts any running turn, closes the current one (a failed close
notes `close failed: <err>`), clears everything that belonged to it — transcript, selection,
pending questions, follow-ups, folds, meters — and replays the new journal. It ends with a
note reading `resumed <path>`.

**`/resume` follows the project you are in, and it replaces.** Its list is this directory's
conversations, and opening one closes the one you were in. That is the one place it differs
from home: `enter` there opens any project's row and leaves the conversation you were in
**open**, still running. `aforge resume` in a shell follows that shell's folder in exactly
the same way — the picker it opens is that directory's list.

**Except for a conversation this terminal already holds.** One open behind the screen is not
reopened and not refused: `enter` goes straight to it, nothing is closed, and the one you
were in stays open. The lock the door would otherwise meet is our own.

A conversation another *terminal* is holding open is reported rather than worked around. See
the section on two terminals in the same folder.

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

**There is one place this fallback deliberately does not apply: picking one by name.** If
you pick a conversation another window is holding open — from the resume picker, the welcome
box, or home — it is reported rather than worked around, because *"a person who picked a
conversation by name means that one"*. Nothing is opened, and the conversation you are in is
left exactly as it was. The sentence is:

```
open in another window — go there, or start a new conversation here
```

**No file path is printed.** The path is aforge's bookkeeping and not something you can act
on; what you can act on is in the sentence.

On **home**, that line appears in the screen's own foot and home stays open, so you can pick
a different row straight away — and home marks such a row `another window` in the list
*before* you press anything. See the home page.

A conversation you pick that is NOT held opens normally. And the one time this can still
surprise you is a lock taken in the instant between the screen being drawn and your
keystroke; you get the same sentence, in the same place.

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

**Closing cleanly** — `/quit`, `ctrl+c` twice, a `kill -INT`, or any other road out — disarms the idle timer,
drops queued follow-ups, closes the wake lanes, cancels the turn with a grace wait, shuts
down background jobs — which is where a task still running ends, a harness still being
designed among them — waits up to two seconds for the second copy of the conversation to
finish landing where memory keeps it, and finally syncs and closes the journal, releasing
the lock. Nothing on that road waits without a clock on it: if that second copy is still
being written when the two seconds are up, quitting goes ahead without it and says so in
the log file, because the journal on disk is the whole transcript either way. Calling it twice is safe, and a session that returns by any
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

`/new` (aliases `/clear`, `/clean`, `/reset`) starts a fresh conversation in the same
directory, with a brand-new session file — and **leaves the one you were in open behind
it**, still streaming its turn and still running its tasks. `tab` over an empty box goes
back. The exception is a conversation nobody has used yet, which is closed and replaced
because there is nothing in it to keep.

What carries over: the settings the session was launched with, and **the approval gate as it
stands right now** rather than as it stood when aforge started. If you have changed what is
allowed during this conversation, the new one begins with that. A conversation opened from
the resume picker is built the same way, for the same reason.

What does not carry over: the conversation itself. The new session starts empty — none of
the messages, none of the context, no memory of what was said. The old conversation is not
deleted; it is on disk, still in the `/resume` list, and still running.

Nothing on disk changes. Files written and commands run during the old conversation stay
exactly as they were.

## What switching keeps, and what it forgets

Leaving a conversation is **not** closing it. Nothing is interrupted, no agent is closed, no
lock is dropped: the surface stops drawing it and everything in it carries on.

Coming back redraws it from its own transcript, which is the one moment you can tell this is
not several terminals. These come back with it:

- the transcript, the task column, the meters, the model, the title, and any approval
  question, task proposal or sign-in offer the session is still holding;
- a turn that is still running, from its **first token** rather than from wherever it had
  got to — and exactly once: the replayed history stops where that turn's work begins, so
  the steps it had already finished are not drawn a second time above the live copy;
- the unsent sentence in the box and the pictures on it — including any message you typed
  while it was busy, folded back into the box rather than dropped;
- where you were reading, the room you had open, and how much of an approval countdown was
  left.

These are forgotten, and each is something you were in the *middle* of or a door onto
something the whole terminal shares: copy mode, an inline rewind or an open rewind timeline,
the expand sheet, the deliverables shelf, every picker and panel, the settings panel, the
status deck, the task page, and the task column's focus.

Contrast that with what `/quit` and `ctrl+c` do, which is close for real: the turn is
cancelled with a grace wait, background jobs are shut down, running tasks end, and the
journal is synced and unlocked — see *Will I lose this if it crashes?*.
