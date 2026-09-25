# Commands

## Typing a slash to see the command list

Type `/` in the home or conversation message box to see every available command in
**alphabetical order above the seam**. The list and `/help` use the same command table.
The list scrolls; a short window does not remove commands. The new-conversation greeting
yields its space to the list while browsing. At phone width, descriptions
appear below command names. On home, only commands appear while this list is active:
thread, task, project and place search results return when you leave the command token.

Keep typing to filter names and aliases by substring. The rows stay alphabetical; the
initial selection favors a name match over an alias, and a prefix over a later match.
For example, `/res` selects `/resume`, although `/new` also matches its `reset` alias.
A filter with no matches shows `no commands match` and keeps unrelated results hidden.

- ↑ / ctrl+p and ↓ / ctrl+n choose a row. PgUp / PgDown and the mouse wheel scroll.
- Enter takes the selected row. A command that takes words, such as `/model <slug>`,
  leaves `/model ` in the box ready for its argument. A bare command runs.
- In a conversation, Esc dismisses the list and leaves the typed word; the list stays
  dismissed while you continue that token. On home, Esc clears the draft as usual.

The list follows the caret as well as edits. The box remains editable while it is open.
A command chosen inside a sentence completes its token rather than running on its own;
The `/task` send tag retains its submission behavior.

On home, command rows describe what they will do there, including commands that open a
conversation first. See *What each command does on home*. In a conversation, pointer
hover highlights a row but clicking does not execute it. Enter confirms the keyboard
selection. Commands entered in a conversation are kept in its ↑-history.

## Why a file path does not pop up the command list

The same token rules apply on home and in conversations:

- A slash starts a command token only at the beginning of the draft or after whitespace.
  `cmd/codeaf` and `https://example.com` do not open the list.
- The caret must be inside the command word. A space begins its argument and closes the
  list; moving the caret back into the command word opens it again.
- The entire token must contain command-name characters. A further slash, a dot or a
  backslash makes it a path rather than a command token, even with the caret midway
  through it. `/tmp/project` and `/shot.png` therefore leave the list closed.

A partial path such as `/tmp` is still indistinguishable from an unknown command word:
it shows `no commands match` until another slash or path punctuation makes the intent
clear. Pasting a complete path never needs those intermediate states.

Panels such as settings, the model picker, resume and copy mode keep their own keyboard
handling; typing `/` there does not open this composer list.

## Slash commands are drawn as chips

A command codeaf recognizes is not drawn as ordinary text. `/task`, `/compact`, `/clear`
and the rest get a **chip**: a tinted background behind exactly the letters of the
command — the same tint the *chosen* row of a list wears, the one that marks the model in
use or the conversation you are in — with the accent ink on top.
No brackets, no border, and nothing added to the line.

It happens in two places: **live in the message box as you type**, and in your message
after it is sent, where it stays for as long as the conversation is scrolled back
through.

Only a command this surface will act on gets one. At the start of the box that is every
recognized command. In the middle or at the end it is one of the two send-door tags,
`/standing` or `/task`, including `/orders`. Other commands there are plain words. A typo
is plain too: `/tsak` stays ordinary text. A path is never chipped, for the reasons in
"Why a file path does not pop up the command list".

The chip never adds a cell. A leading command runs in its usual form. A live send-door
tag is removed from the words handed through its door, while your transcript keeps the
tag and its chip so it is clear why that door acted.

Below the 256-colour rung there is no background to draw, and the command reads as
**bold** instead. On a NO_COLOR terminal there is no mark at all.

## Use /standing or /task in the middle of a sentence

A slash command at the start still runs normally. Two commands are also **send-door
tags** anywhere else in a draft:

- `keep the tests green /standing` removes `/standing` from the sentence and sends
  `keep the tests green` through the standing-order door. `/orders` is the same tag.
- `please investigate the flaky test /task` removes `/task` and sends the remaining
  brief through the same door `/task <brief>` opens.

Both roads end at something you can see: standing raises its ratification card, and task
starts one worker in the open — its started row, and its row on the roster, where it can be
stopped. A pasted tag does not silently do work, because the chip says what enter will do. With no other words, each tag behaves like that command's existing
bare form. With two live tags codeaf sends nothing, leaves the draft in the box, and says
`one tag per send — backspace one to make it plain words`.

Other commands remain ordinary prose away from the start. `later I will run /compact on
this` is sent literally, and `/compact` is plain rather than chipped.

## Backspace after a slash tag makes it plain words

With the caret immediately after a live `/standing`, `/orders`, or `/task` tag, the first
backspace removes its chip but deletes no letter. The word is now plain prose and Enter
sends it to the conversation normally. A second backspace edits the word as usual.

Editing the demoted word makes codeaf recognize its current spelling afresh. Edits before
it merely move the annotation with the text. Emptying or sending the draft forgets all
demotions.

## Slash command did nothing

A command in the middle of a sentence acts only when it is `/standing`, `/orders`, or
`/task`, and a chip is the promise that it will act. `/clear`, `/model`, `/compact` and
the other commands are plain prose there. Put one of those commands at the start if you
want to run it. If a send-door word is plain, it was demoted with backspace; edit it or
type it again to make it live.

The command list follows that rule when you choose a row from it:

- If the word is at the very start of the box and there is nothing else in it, enter runs
  the command — or, for a row that takes an argument, writes `/model ` into the box.
- Anywhere else, choosing a non-door row replaces just that word with the command's name,
  parks the caret after it, and runs nothing. Choosing `/standing` or `/task` completes a
  live tag; Enter on the finished tag routes the send as described above.

So the list can never send a message you did not send yourself, and choosing a row mid
sentence is a way of spelling a word rather than a second way of running a command.

## Aliases, and what happens to an unknown command

Many commands answer to more than one word. Words borrowed from other tools are accepted,
and each one resolves through the single command table. Aliases are not separate rows:
typing an alias narrows the list to the canonical row, and running it runs the canonical
command. Beside each row the list draws the alias tail, like `also /clear /clean /reset`.

A typed word is lower-cased and looked up against the alias lists. An alias may never
equal a canonical name or another alias; that is checked when codeaf starts.

A word that is in no list is passed through as typed and gets this answer:

```
there is no command called /<word> · / lists them
```

It says the word back as you wrote it, not what it resolved to. It points at `/` — one
keystroke, the list itself — rather than at a second command to type. `?` over an empty
box opens the `/help` sheet in one key.

If nothing in the list matches what you typed and you press enter, the list closes and
the line is submitted as an ordinary message. So `/nonsense` still gets an answer.

No command is ever hidden from the list or from `/help`. The table is the one place a
command is written down, and both renderings draw all of it. What varies between states
is what a command *answers*, not whether you can see it.

## Every command codeaf has

Canonical word, the other words it answers to, its argument form, and what it does.

| Command | Aliases | Argument | Effect |
|---|---|---|---|
| `/model` | — | — | opens the model picker |
| `/model` | — | `<slug>` | switches the model to that slug |
| `/settings` | `/set`, `/config` | — | opens the fullscreen settings panel (also ctrl+,) |
| `/connect` | `/connections` | — | opens the connection panel; its `models` group holds model services, followed by connected accounts |
| `/new` | `/clear`, `/clean`, `/reset` | — | closes this session and starts a fresh one |
| `/resume` | `/sessions` | — | opens the earlier-conversations picker |
| `/compact` | — | — | summarizes the conversation now |
| `/home` | — | — | every project and conversation on this machine, fullscreen |
| `/folder` | `/place`, `/dir` | — | locally opens the add context sheet for THIS conversation; on home it opens a conversation first; over `--host` says the folder chooser is unavailable |
| `/folder` | `/place`, `/dir` | `<path>` | locally opens it with that in the box; over `--host` gives the same refusal |
| `/project` | — | — | on home: the browser, opened where the next conversation would open; in a conversation it says it is home's |
| `/project` | — | `<path>` | on home: sets the folder the next conversation opens in, with no browser |
| `/attach` | `/upload` | — | opens the add context sheet for files, including over `--host`; enter on this row of the `/` list opens it at once |
| `/attach` | `/upload` | `<path>` | a file goes on the tray; locally a folder is referred, while over `--host` it is refused |
| `/land` | — | — | says what has been changed for a folder you chose and is waiting to go into it |
| `/land` | — | `now` | …puts it in: a branch merged for a repository, files copied back for a plain folder |
| `/land` | — | `<folder>` | …when more than one folder is waiting; `/land <folder> now` puts that one in |
| `/rewind` | `/undo`, `/back` | — | opens the rewind timeline — the whole conversation as a list (esc esc is the quick inline version) |
| `/permissions` | `/perms` | — | lists what runs without asking; `d` drops a line |
| `/standing` | `/orders` | `<words>` | makes those words a standing order — a card to answer, never work done once |
| `/standing` | `/orders` | — | what stands over this conversation; `p` pauses, `s` stops, `n` excepts this place |
| `/harness` | `/harnesses` | — | lists the saved shapes of work and what they did |
| `/subharness` | `/sub` | — | lists the programs you can run; type to filter, enter opens that one's card |
| `/subharness` | `/sub` | `<name>` | opens that subharness's intake card straight away |
| `/<program>` | — | `<brief>` | one row per program this build carries: starts a task that program does on its own |
| `/skill` | `/skills` | — | opens the skill shelf under the message box; enter toggles a skill, and its chip stays attached across messages |
| `/memory` | — | — | opens the memory panel |
| `/memory` | `/memories` | `<query>` | prints matching memories into the conversation |
| `/memories` | — | — | prints every memory into the conversation |
| `/remember` | — | `<text>` | keeps one thing across conversations |
| `/forget` | — | `<query>` | forgets the best matching memory |
| `/crew` | — | — | opens the six-seat reading: the model you talk to, then the three crew presets |
| `/crew` | — | `<preset>` | sets the crew to `frugal`, `balanced` or `max` |
| `/task` | — | — | opens the full-screen task page — the same page as `/history` and ctrl+. |
| `/task` | — | `<brief>` | starts one worker at once; its brief is written and its width read beside it, and wide work splits |
| `/task` | — | `solo <brief>` | starts one worker at once, with no reading of its width |
| `/history` | — | — | opens the full-screen sessions place — every task this machine has run, filterable (also ctrl+.) |
| `/status` | `/info`, `/context` | — | prints every fact the status line knows, one per line |
| `/status` | `/info`, `/context` | `--json` | prints the same facts as one JSON object, keys in the same order |
| `/search` | — | — | opens the search place — everything said on this machine (also `alt+7`) |
| `/spend` | — | — | opens the spend place — what this machine has cost, by the day (also `alt+3`) |
| `/cost` | `/usage`, `/tokens` | — | prints what this conversation has spent, and on what |
| `/budget` | `/limits` | — | what codeaf may spend · every limit on one tab |
| `/budget` | `/limits` | `<amount>` | sets the day's limit · `none` removes it |
| `/budget` | `/limits` | `<row> <amount>` | sets one by name: `day`, `conversation`, `plan`, `practice` |
| `/cache` | — | — | how big the shared build cache is, and where |
| `/cache` | — | `clean` | asks first, then deletes the cache to free disk — confirm with `/cache clean now` |
| `/debug` | — | — | keeps the full record of **this conversation** from here on, and says which folder it goes to |
| `/update` | `/upgrade` | — | installs the newest stable release and restarts this conversation on it |
| `/update` | `/upgrade` | `<stable\|rc\|dev\|staging\|tag>` | installs that channel's newest release or one exact tag, then restarts this conversation on it |
| `/copy` | — | — | enters copy mode (also ctrl+b) |
| `/select` | — | — | hands the pointer back to the terminal (also ctrl+s) |
| `/export` | `/save` | — | writes the whole conversation to a file |
| `/export` | `/save` | `<path>` | …and writes it there; tab completes the path |
| `/files` | — | — | lists what has been made for you; opens, reveals or copies one — over `--host` it opens the browse page for that machine |
| `/files` | — | `<path>` | over `--host`, brings that one file back and opens it here |
| `/help` | `/?` | — | prints this list |
| `/manual` | — | — | asks the model what codeaf can do, answered from codeaf's own manual |
| `/manual` | — | `<question>` | puts that question to the model, answered from codeaf's own manual, naming the page |
| `/quit` | `/exit`, `/q` | — | leaves |

## /help, /?, /quit, /exit, /q — how do I close just this chat, does closing one conversation quit codeaf

`/help` (or `/?`) prints the whole command table into the conversation, name column
aligned, each row with its alias tail. The first line is the product's own name,
`codeaf` — the one place inside a conversation it names itself.

Under the table `/help` prints the keys that have no slash command, including
`ctrl+c`, `ctrl+o`, `ctrl+q`, `ctrl+e`, `ctrl+t` (a new chat), `ctrl+w` (close this tab),
`alt+t` (the task roster), `ctrl+l`, `alt+backspace` (the word kill), `ctrl+,`, `@path`,
`alt+enter`, and `d` inside `/permissions`. The keys page covers those in full. The
`ctrl+c` line reads `ctrl+c         quits everything · mid-turn it interrupts instead, like esc`.

**It also names the way into the seven places**, which it did not for a long while — three
rows, directly under the `tab` row:

```
alt+1…7        go to a place · in the tab bar's own order: home tasks standing memory spend search settings
alt+.          on a place: what else is here · every key that place has, drawn
               on a place, tab is the next place · esc back
```

On a Mac those read `opt+1…7` and `opt+.`; the substitution happens once, at the moment of
drawing, and the words are the same.

**One gesture, one spelling.** Wherever the sheet names the escape key it writes `esc
back` — the places row, the task roster on `alt+t`, the conversation switcher on `alt+k`,
`space space` — and that is the same two words the cards, pickers, the rewind sheet and the
switcher's own strip already use. The sheet used to say `esc comes back`, `esc goes back`
and `esc leaves` on four different rows, which read as four gestures on the one screen you
open to find out how many there are. The longer `esc leaves it as it was` is a different
sentence and stays where it is: it is said over a value you were editing, and it means the
edit is discarded, not that you moved.

Two rows say what their key DOES rather than naming the thing it reaches: `ctrl+,` is
`open settings`, and `n` on the `/standing` row is `keep it out of here`.

The last line of `/help` is `session · <path>`, and it appears **only when the session
has a file**. The path is written against your home — `~/.codeaf/v3/…` — so that it fits
on one row and still pastes into a shell; `/status` prints it in full. Over `--host` the
path is written `machine:/path`.

## The ? key — the one-key way to the key sheet

`?` **over an empty box** opens `/help` — the whole key sheet, in the transcript.

**On a place it draws the map instead** (the same thing `alt+.` draws): that place's own
keys, in the cells the foot was already using. The key means one thing — show me the keys
for where I am standing — and the two screens have two answers to it.

**With anything typed in the box, `?` is just a question mark** and goes into your
sentence, which is where a question mark nearly always belongs. The same is true inside
any filter box, picker or panel: those have the keyboard first, so the key never reaches
this binding.

The key is named on the first row of `/help` itself, and on the line every session opens
with — `esc interrupts · ctrl+c quits · ? for help`. `/?` is also an alias of
`/help`, and has been all along.

`/quit` (or `/exit`, `/q`) **closes the conversation in front**, and it does it at once —
it is typed out on purpose, so it is not asked twice. Your draft is written to disk
synchronously first, with any message still waiting for an answer folded in underneath it,
so nothing typed in the last moment is lost. Then that conversation's running turn is
interrupted and its agent is closed for real.

**If this terminal is holding another conversation, codeaf stays up** and the most recently
open one comes forward, saying `closed · <the name of the one that went>`. `/quit` leaves
the program only when the conversation it closed was the last one. Home's page has the
whole arrangement under *Switch between projects without leaving*.

`ctrl+c` is the other road out, it takes **one press**, and it closes **everything** —
every conversation this terminal holds, not just the one in front. Nothing is asked and
nothing is named first: your draft and anything waiting for an answer are written to disk,
and codeaf exits. Work the codeaf service is running keeps going and is there when you open
the workspace again; work running inside this terminal stops with it.
While a turn is running `ctrl+c` interrupts the turn instead, and that press does not leave.
The keys page has the whole rule under "Quitting codeaf".

## /new — start another conversation in this project

`/new` (or `/clear`, `/clean`, `/reset`) opens a fresh conversation on the same config,
with a fresh agent and a fresh session file.

**It adds one rather than closing this one.** The conversation you were in is left open
behind it — still streaming its turn, still running its tasks — and `tab` over an empty
message box goes back. The **tab strip** above the transcript then shows both.

**The one exception is a conversation nobody has used yet**: no transcript, no turn ever
run, nothing out and nothing waiting. That one is closed and replaced, because closing it
costs nothing and keeping it would spend a slot on a conversation you never typed in.

The screen is cleared either way. So are selections, thinking, pending approvals, connect
offers and harness offers, the task rail, the frozen copy viewport, and any open browser
sign-in — they all belong to the conversation you were in. The title, model and meters are
re-read from the new agent.

**Your draft is deliberately not cleared.** `/new` is the one door where the sentence in
the box goes with **you** rather than with the conversation: it is carried into the new one
and cleared in the old, along with any message that was waiting for an answer.

It ends with a note that says which of the two happened: `new conversation · <project>`
when it added one, and `new session · <path>` — or just `new session` with no file — when
it replaced a fresh empty one.

**How many conversations are already open is never a reason to refuse.** The ninth and
the fiftieth `/new` open exactly like the first, and the one you were in is left running.
Past twelve open, a quiet conversation you have not looked at for fifteen minutes may be
let go of — home's page under *How many conversations can one terminal hold* is the whole of
that. `/quit` is still how you close the one in front.

Reasoning level does not survive: `/new` forgets the level you set on a model.

Refusals, exactly as written:

```
/new is unavailable here
```

That is the answer where no fresh-session seam is wired — a headless frame, or a launcher
that did not supply one.

```
close failed: <error>
new session failed: <error>
```

A close that fails says so and the surface continues. A launcher that cannot build the
replacement says so, and nothing is replaced.

## /compact — summarize the conversation now

`/compact` notes `compacting…` immediately and runs the compaction off the loop.

**There is no success message.** A compaction that worked is silent — the note that it
started is all you get.

A failure comes back as:

```
compact failed: <error>
```

`/compact` has no argument form and no alias.

**A compaction costs nothing and asks no model.** It is two mechanical passes over
the messages this session already has: tool results the model has already used
become pointers to their own bytes, and if that is not enough the oldest assistant
work is replaced by one marker line naming how much went and where it can be read.
Your own words are never folded. There is **no summariser** and there is **no
`compaction` role in settings** — there was one, and it was a priced row wired to
nothing. What the model is handed instead of a summary is the state card, which is
maintained a little at a time by the reader that runs after each turn.

## /rewind — go back to an earlier point in the conversation

`/rewind` (or `/undo`, `/back`) opens the **rewind timeline**: a fullscreen list of the
whole conversation, oldest first, that you pick a point out of. It is the deliberate way
in. The quick way is esc esc, which draws a cut line through the transcript on screen
instead of opening anything — see the sessions and rewind page for both.

The command row reads `go back to an earlier point · esc esc takes back the last`.

On the timeline: ↑↓ move, typing searches, the first `enter` places the pick and the
second `enter` on that same point does the rewind, `esc` clears the search and then
closes the page. The head reads `⟲ rewind — pick where the conversation goes back to`
and the foot reads `⟲ drops 2 turns — everything below the pick is let go`.

**Be warned: `/rewind` silently does nothing in six states.** No message, no page,
nothing at all happens when:

- the rewind timeline is already open,
- the inline rewind mode is already on,
- copy mode is on,
- a task room is open,
- the settings panel is open,
- the task rail is full.

Each of those already owns the frame or the row the rewind needs, so the command is
dropped rather than half-drawn. If `/rewind` seems to do nothing, one of those six is why.

With no rewind points, or no agent that can rewind, it does answer, exactly:

```
nothing to rewind
```

## /copy — read the conversation back and copy from it

`/copy` freezes the visible conversation and enters copy mode. It is the same thing
ctrl+b does. In copy mode ↑↓ move, `v` marks, `a` takes the block, `y` yanks.

**`/copy` silently does nothing in two states**, with no message either way:

- copy mode is already on,
- there are no visible rows to freeze.

If you type `/copy` and the screen does not change, one of those two is why.

## /project — set the project on home, which folder will my next conversation open in, change the project

`/project` is **home's** command, and it sets the folder the conversation you start next
will open in — the `project: ~/src/parser` at the right end of the keys row under home's
box. Bare, it opens the folder browser where that next conversation would open. With a
path after it, it takes the path and opens nothing:

```
/project                 the browser
/project ~/src/parser    pinned at once · the keys row says project: ~/src/parser
```

A path that is not a directory on this machine is refused by name — `no folder there ·
~/src/parsr` — and nothing changes. Over `--host` it refuses: the folders this program can
read are the laptop's and the conversation would be on the other machine.

**In a conversation it does nothing but say where it lives**, exactly:

```
/project is home's · it sets the folder the next conversation opens in · /folder gives this conversation one
```

The two commands are one word apart and do different jobs, so the answer names both.

**It was the home half of `/folder` until 2026-09-22.** `/folder` meant "give this
conversation a folder" in a conversation and "pin the next conversation's folder" on home,
which is two acts behind one word. The pin is `/project` now, and `/folder` means the one
thing on every screen — on home it opens a conversation first and browses there. `alt+p`
is the same pin without a browser, walking the projects this machine knows.

## /select — drag to select with your mouse

You usually do not need this any more: **dragging over the conversation already
selects and copies, to the character** — sweep with the left button down from one
character to another, the cells between them highlight, and on release their text is
on your clipboard, with `copied · 14 chars` or `copied · 3 lines` on the status line.
Double-click takes a word, triple-click a line (see the keys page's *Selecting text
with your mouse*).

**The message box answers the same drag**, and so does the box at the foot of every
place: sweep across what you have typed and it highlights, release and it is copied.
A selection in a box is live — typing replaces it and `backspace` removes it whole
(the keys page's *how do I select text in the message box*).

`/select` is for when you want your **terminal's own** selection instead: it hands the
pointer back so a drag selects text natively. It is the same thing ctrl+s does.

It toggles. Pressing it again takes the pointer back, and so does the next ordinary
keystroke.

When codeaf never took the mouse in the first place — the `mouse` setting is off — it
answers out loud, exactly:

```
your terminal already has the pointer — drag to select.
```

## /image — attach a picture, and why there is no /image command any more

There is no `/image` command. Until 2026-09-22 it was a second word for `/attach` that
took only a picture and refused everything else; `/attach <path>` does the whole job now.
A picture handed to it lands on the tray as `▣ #1 name.png` and **its `[image #1]` token
is appended to your sentence when you press `enter`**, so you can refer to it by number the
same way you would one you dragged in. Anything else lands as a file. Typing `/image` is
answered the way every unknown word is: `there is no command called /image · / lists them`.

Path rules: `~` is your home directory, a bare name is under the directory this
conversation is about, an absolute path is left alone, and **a path in quotes, or with
its spaces backslashed** — the shape Finder and a terminal drop hand you — is read as the
one path it is. Over `--host` the path is anchored to **this** machine — the picture is on
the laptop you are sitting at, and its bytes travel with the message.

A full attachment tray does not stop the command: a second picture is a second chip rather
than a send. Dragging or pasting a file over a line that already starts with `/` leaves
the path as text, so `/attach ` still takes the path you dropped on it.

Refusals, exactly as written on the attaching-files page:

```
no such file: <what you typed>
<name> is already attached
```

The second is what you get for the wrong kind of file. The third covers both a path that
is not there and a path that turns out to be a directory. The fourth means that picture
is on the tray already.

Tab completes the path as you type it.

## Does export save to my laptop — /export writes this conversation here

`/export` (or `/save`) writes the **whole** conversation to a markdown file somebody else
can read. It is built from the full transcript, not from the tail on screen.

With no argument the file lands in the local root under a name derived from the session,
like `20260817-150405_a3f2-port-the-resume-picker.md` — the transcript file stem plus the
session name in kebab case, clipped to 48 characters. With neither file nor name it is
called `conversation.md`.

With a path, `~` is home, a bare name is under the conversation's directory, and an
absolute path is left alone. If the path is an existing directory the file goes inside it
under the derived name. A missing parent directory is created **only** for a path you
typed yourself; codeaf will not create directories under the workspace on its own.

Success reads `exported · <short path>`, with ` · on this machine` appended over `--host`.

Refusals, exactly as written:

```
nothing to export yet.
<short path> is already there · /export <path> writes it somewhere else
export failed: <error>
```

The first is what you get with no agent or an empty transcript. The second means the file
exists — `/export` never overwrites.

## What an exported file looks like

`/export` writes markdown meant to be read by a person who was not there.

The document opens with `# <session name>`. Under it, `## you` and `## codeaf` headings
appear only when the speaker changes, so a run of turns from one side is not chopped up.

Your text and the model's text are kept verbatim as markdown.

A tool call becomes one list item, `` - `tool` gloss ``. Its output is quoted underneath
it **only** while it fits inside 600 runes and 12 lines; anything longer is left out
rather than allowed to swamp the document. The fence around an output is always longer
than the longest run of backticks inside it, so a fenced block in the output does not
break the file.

Session "note" entries are skipped. "aside" entries are kept, in italics.

The file is written with permissions 0600, and it is created exclusively — the filesystem
decides whether the name is free, not a check that could go stale between looking and
writing. A directory codeaf creates for a path you typed is made 0700.

## /files — what has been made for you

`/files` lists the things conversations have produced — a generated picture, an exported
document, a report a session wrote — newest first, whatever directory each one was made
in. It reads one index of everything made rather than looking in a folder, so "the report
from Tuesday" is found by its title from anywhere.

Each row is a mark, the title it was given, and a dim tail: where the file went, in short
form, and how long ago it landed. Pictures carry one mark and everything else carries the
other.

Typing narrows the list by title, the same way the model picker and `/resume` narrow
theirs. ↑↓ move, esc clears the filter and then closes the list.

Three verbs:

- **enter** opens the file the way your desktop would (`open` on a Mac, `xdg-open` on
  Linux).
- **ctrl+r** opens the folder the file is in, the same way.
- **ctrl+y** asks where to copy it. Type a path — `~` is home, a bare name lands under
  the conversation's own directory — and enter copies it there. A path that is an
  existing directory receives the file under the name it already has.

The verbs are chords rather than bare letters because plain letters go into the filter
box.

A file that has been deleted, moved or renamed since it was made keeps its row and its
tail reads `gone`. None of the three verbs act on such a row. The original is never
moved: copying leaves it exactly where it was.

`/files` never overwrites. If something is already at the destination the copy is refused
and says so; give it another name.

If nothing has been made yet, `/files` opens no list and answers `nothing made yet.`

On a session running on **this** machine that is the whole of `/files`. Over `--host` it
means something else — see the next section.

## /files over --host — browse, open and download files on the other machine

On a `--host` session the files the conversation is about are on the **other** machine, so
`/files` points at that machine instead of at this one's list.

`/files` with nothing after it opens a **browse page** for the far workspace in this
machine's own browser, and writes the address into the conversation as well, so you can
paste it into a different browser or reach it when nothing opened. The page is served by a
listener on `127.0.0.1` that this window owns: it starts the first time you need it, its
addresses work only while the window is open, and it can only show what the session itself
chose to show. What may be shown is the far session's own law and not this window's — the
workspace it is working in and the session's own folder, and nothing outside those two.

`/files <path>` brings **one file** back and opens it the way your desktop would. The path
is a path on the other machine, relative to that workspace. The bytes are kept here by
content, under `~/.codeaf/v3/remote/`, and a copy under the file's own name is what your
viewer is handed — so the window title says `report.pdf` and not a row of hex. A file the
model wrote during the turn is usually already here before you ask, so it opens at once:
codeaf quietly fetches a file of 2MB or less as it sees it being written, and says nothing
about having done it.

The two open in different places, and that is the difference worth knowing: **a clicked
path and the browse page open in your browser** — a terminal hands a web address to a
browser and that is what the link is — while **`/files <path>` hands the file to your own
program**, `open` on a Mac and `xdg-open` on Linux, so a `.csv` lands in your spreadsheet.

A fetch that takes less than a third of a second says nothing at all. A longer one draws
one line naming the file. If the other machine refuses — the path is outside those two
places, or the file is over the 16MB one file may cross this connection — you get that
machine's own sentence, unchanged.

**Nothing here edits that machine's files.** The copy on this one is a copy: editing it
changes nothing over there. The one thing you can put ON the other machine is a file
dropped onto the browse page, and it lands in that session's `attachments/` folder — never
anywhere else you could name, and never over anything already there. **A drop says
nothing:** no message is sent, no turn starts, and nothing about it appears in the
conversation, so the chat only knows about the file when you mention it. `/attach` is the
same landing place *with* your own message saying what it is for. A file over the 16MB one
file may cross is refused, and so is a name that is a path.

On a local session `/files <path>` does nothing but say so: `that form of /files is for a
session on another machine — this one is local, so the paths in it are already yours to
open`.

The browse page and the links are served by the same listener, which starts the first time
one is needed and closes with the window; every address it minted stops working then, and
a wrong one gets a plain `404` with nothing in it to tell one wrong guess from another.
If it cannot start at all you get one line and no links: `the file door did not open on
this machine`.

**Paths in replies are links again over `--host`.** A path the reply names is checked with
the other machine first, and only a real file there becomes clickable; cmd+click opens it
through the same door the browse page uses. A word that machine did not confirm stays
plain text, which is the same rule a local session has always followed. A confirmed
**folder** is not a link over a connection.

*Opening files from that machine* is the whole of this in a person's terms: what turns
into a link and why one did not, where the copies live on this machine, what a drop on the
browse page does, the 16MB ceiling, and who else can reach those addresses.

## /status — everything the status line knows

`/status` (or `/info`, `/context`) prints every fact the status line can carry, one per
line, **into the conversation**. It is a note, not a panel — you can scroll it and copy
from it later.

Usage totals are refreshed first, so a command typed between turns answers from what the
session holds right now.

The labels come in this order, and each is dropped when its value is empty: `session`,
`task` (only inside a task room), `model` (the full routing address, with `:level` when a
reasoning level is set), `crew`, `task model` (only in a room), `served`, `search`,
`approvals`, then the telemetry words — `spend`, `cache`, `context`, `compacts at`,
`compaction`, `background`, `changes`, `rate`, `open`, `watching`, `speed`, `connection`,
`state` — then `tasks`, `keeping watch`, `place`, `keys`, and last `build` and `file`.
Labels are padded into two aligned columns.

Four other words are facts this command and the phone's sheet carry and the status row
does not: `changes` (`Σ +128 −14`), `rate` (`1.2k tok/s avg`, this turn's output over its
whole wall time — the right edge of the row shows the live `38 tok/s` instead), `open`
(`2 open · 1 waiting`) and `watching` (the standing count, which is drawn at the foot of the
task column). `crew` is a fifth and has its own line above.

The `crew` line sits directly under `model` and reads the preset word — or `custom` — and
the three classes:

```
crew     max · brain claude-opus-5 · hands glm-5.3 · checks claude-fable-5.1
```

The crew is **not on the status line**. It was one short segment there — `crew max`, or
`crew custom` — at the head of the telemetry until 2026-09-09, and it came off: the row is
a ledger of things you act on from it, and a preset is changed on a page. The `crew` line
here and on the phone's status sheet is where it is read now, in full. Every ordinary
launch has a crew — one is never unset, only `custom` — so the line is always there; the
one session that shows none is a **remote** one opened with `--host`, where the crew
belongs to the other machine.

`/status` differs from the on-screen status sheet in two deliberate ways:

- The session **file** is added. A path is a thing you copy into another program.
- The `spend` line is **dropped** when nothing has been spent. The live status line keeps
  showing `$0.00`; a note in the transcript must not.

Over `--host` the `place` and `file` values are written in full as `machine:/path`.

## Is the asking on — what `/status` says under `approvals`, and where the YOLO badge went

`/status` carries the tool gate's posture on a line of its own, labelled `approvals`, in
the engine's posture words: `ask` (it asks you), `guardian` (a small model answers the
plainly safe ones first), `allow` (it runs things without asking) or `deny` (it refuses).
It is **this conversation's** posture — the one the `◇` cell on the legend shows — whichever
setting decided it. `/status --json` carries the same fact under the `approvals` key, and
the phone's status sheet has the same row.

The legend spells that fact as a cell: `◇ asks`, `◇ guardian`, `◇ YOLO`, `◇ refuses`. The
status line draws `YOLO` only while the gate is open **and** the legend has no cell — a
conversation whose engine has no approvals door — because there a permanent badge is a badge
nobody reads. The welcome box draws neither the cell nor the badge: the legend and its cell come up the
moment the greeting goes — the first keystroke, or on the very first conversation the first
message. Inside a task's page the legend carries the task's own
cell, `◇ on its own`, and the badge stays off. A page has room for the whole answer, so it
names the posture whichever it is.

The cell, the badge and the line always agree, because all three read one posture. Walk
it with `alt+a` and all three move on the keystroke; change "ask before running" in
`/settings` and they move together for a conversation that follows the rows, or the panel
says `saved · from the next session`.

The one session with no `approvals` line is a remote one whose engine carried no posture
over the wire. The gate there is the far machine's, and a line drawn from this laptop's
settings would be a claim about a machine nobody consulted.

## /status --json — status as JSON, machine-readable status for a script

`/status --json` prints the same facts as **one JSON object** on one line, instead of
aligned columns. The labels are the keys, the values are strings, and the keys come in
the same order `/status` prints them — `session`, `model`, `crew`, `spend`, `context`,
`place`, `build`, `file` and the rest of the list above. Both forms are built from one
list inside codeaf, so they cannot disagree about a fact.

```
{"session":"lab","model":"openrouter/deepseek-v4-flash","spend":"$0.31","file":"/tmp/lab/.codeaf/sessions/2026-08-17T09-15-02.json"}
```

`--json` is spelled exactly that way, and it is the only argument `/status` takes. Any
other argument is ignored and the text form prints — `/status --JSON`, `/status json`
and `/status --json --pretty` all print the aligned columns. The command's other words
take the flag too: `/info --json` and `/context --json` print the object.

What the object does **not** carry:

- **A fact this session does not have is not a key.** There is no `null`, no `""` and no
  `0`: a session that has spent nothing has no `spend` key, one whose window nobody named
  has no `context` key, one not saved to disk has no `file` key. That is the same silence
  the aligned form keeps — and it means the object cannot tell you "unknown" apart from
  "absent".
- **Every value is a string**, the words the status line itself draws: `"spend":"$0.31"`,
  not `0.31`, and `"context":"12.4k/128k · 10%"`, not a number and a percentage. There is
  no nesting, no schema, no version and no timestamp.
- **It is not the whole session.** It is what the status line knows — no message history,
  no task list beyond the `tasks` count, no settings.

It prints **into the conversation**, as a note like every other one — there is nothing to
pipe it into and it is not written to a file. That also means it is wrapped to the width
of your terminal, so text copied off the screen carries the line breaks the frame put in
and the note's leading `· `; strip those before feeding it to a parser.

## /search and /spend — the typed doors onto those two places

`/search` opens the **search place** — everything that has been said on this machine,
found by the words you remember of it. It is the same place `alt+7` opens and the same
place `tab` walks to. It takes no argument: the place *is* a box, and typing in it
searches. With the **memory** row off nothing said is indexed, and the place says so and
searches nothing — find the conversation from home's box instead (see the *places* page).

`/spend` opens the **spend place** — what this machine has cost, by the day, by the model
and by what it was for. It is the same place `alt+3` opens.

**`/spend` used to be an alias of `/cost` and is not any more.** The two answer different
questions: `/cost` is *this conversation's* bill, printed into the conversation, and the
spend place is *the whole machine* — every window, every task and every standing run,
including a session opened from another machine over `--host` whose calls are still made
here. The word `spend` belongs to the bigger reading, so the one guess most people make
now lands on the place. `/cost` keeps `/usage` and `/tokens`.

## /cost — what this conversation has spent

`/cost` (or `/usage`, `/tokens`) prints what this conversation has spent, and
on what, into the conversation. For the whole machine's ledger, ask `/spend`.

It draws up to seven aligned lines:

- `spend` — only when it is above zero. It is **this conversation and every task it
  started**, which is the same figure the status line carries.
- `conversation` and `tasks` — the two halves of that figure, in that order, and they add
  up to it. Both lines are dropped unless the work has actually spent something: a
  conversation that has started no tasks has no split to state.
- `tokens` — like `48.1k in · 3.2k out`, or one half alone, or the combined figure.
- `cache` — like `31.2k read · saved $0.0180`. The money half appears only when a price
  pair was published.
- `model calls` — requests to the provider. Deliberately not called "turns".
- `time`.

So a conversation whose tasks are still running reads:

```
spend         $53.58
conversation  $2.53
tasks         $51.05
```

Every line is dropped when its figure is absent. A provider that publishes no cache
accounting says nothing about caches, rather than teaching you that your cache never
hits.

`/cost` will not go silent. A session with no figures at all answers exactly:

```
nothing spent yet — this session has not sent a turn.
```

## /budget — setting a limit from the message box, without opening settings

`/budget` (alias `/limits`) is the keyboard door onto the limits, and it writes through
the same row the Spending tab writes through.

| What you type | What it does |
| --- | --- |
| `/budget` | opens the Spending tab on `per day` |
| `/budget 50` | sets the day's limit to $50 |
| `/budget none` | removes the day's limit — the row then reads `no limit` |
| `/budget plan 20` | sets one row by name |
| `/budget plan` | a row named with no figure opens the tab on that row |

The row names it takes are **`day`** (`daily`, `today`), **`conversation`** (`chat`,
`session`), **`plan`** (`plans`, `ask`) and **`practice`** — the four rows that can be
edited. There is deliberately **no `/budget task`**: an ordinary `/task` has no dollar
limit of its own, so a command that accepted one would write a number nothing reads.
senior-dev has a separate ceiling for each run; see its page for the shell flags and
conversation limits that can lower it.

A write says back what it landed, in the tab's own words for that row — `per day · $50`,
or `per day · no limit`. A figure it cannot read is refused in the row's own words with
the rows listed after it: `that's not a dollar amount — a number, or none for no limit ·
rows: day, conversation, plan, practice`. A row this machine does not have answers `that
limit is not on this machine`.

The `$` is optional, and the amount can be a word — see the next section.

## /cache — the build cache, disk space, and why codeaf is using so much disk

`/cache` prints one line: how big the shared build cache is and where it lives —
`~/.codeaf/cache`. That directory holds the toolchain caches task workers fill as they
build — go modules and build outputs, npm, pip, cargo — shared across sessions so the same
module is downloaded once instead of per task. It can quietly grow to hundreds of
megabytes; that growth is this cache, not your conversations.

With nothing in it, `/cache` answers `the cache is empty · ~/.codeaf/cache`.

**The cache is not your conversations.** Conversation history, tasks, settings and
credentials live elsewhere under `~/.codeaf` and no cache command can reach them.

## /cache clean — clean the cache, clear the cache, free disk space

`/cache clean` deletes the shared build cache. It is the one deliberately destructive
command on this surface, so it never acts on the first ask:

1. `/cache clean` **only asks**. It answers with the size, the path, what deleting costs
   (`builds start cold afterwards`), what is out of reach (`conversations and settings are
   not touched`), and the sentence that would proceed.
2. `/cache clean now`, typed out in full, does the deletion and answers
   `cache cleaned · <size> freed`.

Deleting it is safe but not free: the next task that builds something re-downloads its
modules cold. The cache refills itself as work runs; there is nothing to set up again.

Over an empty cache both forms answer, exactly:

```
the cache is already empty — nothing to delete.
```

A word after `/cache` that is not `clean` changes nothing and answers
`/cache takes clean, or nothing · /cache shows what it holds`.

From the terminal the same pair is `codeaf cache` and `codeaf cache clean` — the latter
prints the same size-and-path warning and asks you to type the word `clean` before it
deletes anything; `codeaf cache clean --yes` skips the question for scripts.

**What `/cache clean` will not do:** it does not delete conversations, reset the
dashboard, or forget memories. To start a fresh conversation the command is `/new`;
memories are dropped with `/forget`.

## /model — pick a model

`/model` with nothing after it opens the model picker: a filter box in the input line's
place with a short list of models under it. It is bottom-anchored, so the conversation
shrinks above it and nothing pops up over what you were reading. Pressing the model's
name on the legend line above the box opens the same picker.

Models from connected services sit under their service's name as a dim heading, default
service first; a custom connection's heading is the name you gave it.

`/model <slug>` switches straight to that slug: no list, no confirmation, and no check
that the slug exists in any list. If the slug is in no known list, the context window is
left alone.

A switch carries the conversation's words and nothing of the previous model's private
thinking. A model's reasoning is its own — the router encrypts it and refuses to replay it
to any other model — so the new model reads what was said and continues from there. If a
switch mid-conversation is ever refused with "encrypted reasoning … produced under a
different model", the request is repaired and sent again on its own; nothing is lost.

Moving in the picker: type to filter; ↑ / ctrl+p and ↓ / ctrl+n move; pgup/pgdown move
12; left, right, home, end, ctrl+u and ctrl+w edit the filter. **ctrl+t** walks the
reasoning effort of the model under the cursor through
`auto → low → medium → high → xhigh → max → auto`, which is the same walk a task's own
thinking control takes. enter switches.

**→ or tab on a model opens its providers and walks the cursor into them**, onto the pinned
provider or `auto`; enter pins, ← or tab walks back out. *Providers → Pinning one provider yourself*
has the rest.

**Enter chooses and the list stays up; esc is the way out.** Pressing enter on a row
switches to it there and then and leaves the list on screen, so you can compare two models
by their prices, switch, and switch back without reopening anything — and the mark moves to
whatever you just chose. The same is true of a provider inside a fold: enter pins it, the
list stays.

esc itself changes **nothing** — it closes the list and gives your half-typed draft and the
frame back as they were. What enter already did is already done; esc does not undo it. The
filter is forgotten when the picker closes.

The cursor opens on the model in use, which is also the marked row, so enter with nothing
typed confirms rather than changes. Emptying the filter with ctrl+u puts it back there.

The placeholder in the empty filter box reads:

```
filter by name · ctrl+r refresh
```

It says **by name** because that is the whole of what the box does: there is no way to type
a question about speed, price or capability into it (the *models and cost* page, "You cannot
filter the picker by speed, price or capability"). On a frame too narrow for the words it
falls back to `filter`.

**The keys are named on the foot and not in the box**, because a placeholder disappears the
moment you type — which is exactly when you have found your model and want its providers.
The foot follows the cursor and always reads in one order — the keys that move the **cursor**,
then the ones that change the **list**, then `enter`, then the one key that is about neither:
`→ providers · alt+s sort · enter switch · ctrl+t effort · esc` on a model,
`← back · alt+s sort · enter choose · esc` inside its providers — and `← back · alt+s sort ·
enter unpin · esc` on the provider you are already pinned to, where the same key takes the pin
off again. While you are
mid-typing and the arrow would step over a character, those read `tab providers` and `tab
back` — the foot names whichever key actually works at that moment. What is left in
the box is the name of the box and the one key that is about the LIST rather than about the
row under the cursor.

Choosing a model sets it on the agent, teaches the surface its context window and tells
the session — compaction fires at a fraction of that window, so this is not decoration —
notes `model · <model>`, and writes the choice into your profile, so the next `codeaf`
opens on it. Over `--host` the switch takes for the session and is not written down: the
model a remote session opens on is that machine's to resolve.

## What the model picker lists, and what it will not do

The picker **never fetches on its own** — it fetches only when you ask, with `ctrl+r` (see
"Refreshing the model list" below). The list is what is already known, tried in this order,
each rung used only when the one above it came back empty after filtering:

1. the catalog handed in at launch,
2. `~/.codeaf/v3/models.json`,
3. five names this build remembers: `deepseek/deepseek-v4-flash`, `openai/gpt-4.1-mini`,
   `anthropic/claude-sonnet-4.5`, `google/gemini-2.5-flash`, `moonshotai/kimi-k3`.

Filtering is over the model's **name** and nothing else — no word in the box means
anything but itself. It splits your text on whitespace and every word must match, each
scored by the fuzzy alignment every picker on this surface shares: a word that starts an
id, or lands right after a `/` or a hyphen, outranks the same letters sitting loose inside
it. So `ds v4` finds `deepseek/deepseek-v4-flash` and `claude 4.5` finds
`anthropic/claude-sonnet-4.5`, and a tight prefix sits above a scattered match.

**`alt+s` walks the sort and `alt+shift+s` walks it back.** The list is always sorted — it
opens on the name, A to Z — and every column is two presses, its own direction then reversed.
Columns this list published nothing in are skipped, the sorted one wears the arrow, and inside
an open provider fold the same key sorts the providers instead (the *models and cost* page,
"Sorting the model list by a column").

Twelve rows show at a time, under a dim heading line. The list is a **table**: `<id>:<level>`
on the left under `model`, and dim columns to the right of it for what the catalog
published — `via`, `first`, `in/M`, `out/M`, `window`, `t/s`, `elo`, `inputs` and
`outputs`. The last two carry everything the model takes in and gives back, in the
catalog's own words: `text`, `image`, `audio`, `video`, `file`, `speech`, `music`. The heading names the unit, so the figure
under it does not: `$0.18` under `out/M`, `1290` under `elo`. An empty cell means nobody
published that fact; a column no row published is not drawn at all. A price shows only when
both halves are known — a zero means "nobody said", never "free". `text` is never drawn on either
side — every model on this list reads and writes it, so the cell is for what a model can do
**beyond** holding a conversation, and an empty `inputs` cell means text in and nothing
else. `outputs` is not drawn in `/model` at all: a model that answers with anything but
text cannot hold a conversation and is not on that list, so the column every row agrees on
is dropped. A narrow window gives up columns from the right, and under sixty
columns the table gives way to the older `·` tail on a line of its own (the *models and
cost* page, "What each row in the model picker tells you").

Only models you can hold a conversation with are listed: text in and text out. A model
that publishes `["image","text"]` out — a drawing model that also captions — is left out,
and so is a transcription model. A model that publishes nothing is judged by its id.

Limits:

- `/model <slug>` refuses a slug the catalog carries that **cannot hold a conversation**,
  in one line — `openai/gpt-4o-mini-tts cannot hold a conversation — it answers with speech. Still on
  <model>.` — and does not switch. A slug the catalog has never carried is taken as typed.

- **ctrl+t does nothing at all, silently**, on a model whose catalog row does not accept a
  reasoning knob. The level would be an error at the next turn. A row that published
  nothing counts as "does not accept".
- The reasoning level lives on the agent, per model id, so it survives switching away and
  back. `/new` forgets it.
- There is no mouse commit on the picker's rows.

## Refreshing the model list — a new model is not in /model, the list is out of date

The list `/model` shows is fetched from the router at most once a day, so a model a
provider shipped this morning may not be in it yet. With the picker open, press
**`ctrl+r`** to fetch the newest list now. The placeholder names it — `ctrl+r refresh` —
and when your filter matches nothing the list says `no model matches · ctrl+r fetches the
newest list`. Nothing on screen shows how old the list is; when in doubt, press it.

While it runs, the list's first line reads `fetching the newest list…` and the picker keeps
working: type, move, switch. A second `ctrl+r` while one is out does nothing. It waits at
most fifteen seconds.

When it lands, the list is filtered again by what you typed, the cursor goes back to the
model in use, and the conversation gets one note: `models · 612 · 9 new ·` and up to three
of the new ids, or `models · 612 · nothing new`. A model that left the list is not
mentioned. The new list is saved (`~/.codeaf/v3/models.json`), so the next `codeaf` opens on
it. From a terminal, `codeaf models --refresh` does the same.

If it fails, the list stays exactly as it was and the note says why in one line —
`could not fetch the model list · dial tcp: lookup openrouter.ai: no such host` — and
`ctrl+r` is offered again.

Where it is absent: only `/model` (and the model word in a task's status line, which opens
the same list) has the key. Every other model list — the settings panel's rows, home's `/model` list, and the
task composer's `alt+o` list — does not: there `ctrl+r` does nothing and nothing names it. Over `--host` it works and fetches on this
machine, whose list of names the picker shows.

## /resume — open an earlier conversation

`/resume` (or `/sessions`) opens the picker of earlier conversations. It is the same
surface `codeaf resume` opens on. The list is resolved at that keystroke, so a
conversation you had in another terminal since is in it.

A filter box takes the input line's place. Type to filter, ↑↓ to move, pgup/pgdown by 10.
The filter ranks over the name and the description together, every token matching, prefix
beating substring beating subsequence — which is why typing `migration` finds the session
you never named. An empty box is newest first.

esc leaves the conversation exactly as it was.

Ten rows show, each a name, a description and an age. The name climbs a ladder: the title
the session gave itself, else the first seven words you said in it, else the transcript's
file name — then title-cased, with small words left lowercase and nothing ever
lowercased, so `OpenAI` keeps its shape. The description is the last thing that happened,
capped at 80 columns. The age (`2h ago`, or a date past a month) is reserved first and
never cut. **Ids and file names appear nowhere.**

The cursor opens on the conversation you are already in. enter on another row closes this
agent, interrupting a running turn first, opens the chosen transcript, clears everything
belonging to the old conversation, replays the new one, and notes `resumed <path>`.

**`/resume` follows the project you are in.** Its list is this project's conversations, and
enter on one of them **replaces** the conversation on screen rather than adding to it —
which is the one place this differs from home, where `enter` opens any project's row and
leaves the one you were in running. If you want the conversation you are in kept, go
through home.

**Except for one this terminal already holds.** A conversation open behind the screen is
not reopened and not refused: enter goes straight to it, exactly as `tab` would, and
nothing is closed. The lock the door would meet is our own.

There is no argument form, on purpose: a session is named by a title a model wrote and
lives in a file named after a timestamp, so the only honest way to ask for one is to be
shown them. There is no mouse commit on this list either.

Refusals, exactly as written:

```
resuming is unavailable here
No sessions yet — start one with codeaf chat
already here · <name>
open in another window — go there, or start a new conversation here
resume failed: <error>
close failed: <error>
```

The fourth is a conversation another window is holding open — **no file path is printed**,
and nothing is closed: the conversation you are in is still there. `resume failed:` is now
only for the rest, which are rare.

`already here` is enter on the row you are on; nothing is closed. A directory with no
conversations never opens the picker at all — a modal list with no rows would be a trap.

**`/resume` lists this directory's conversations only.** For every project on the machine
at once, and every conversation in all of them, the command is `/home`.

## /home — every project on this machine

`/home` opens a fullscreen screen of **every project on this machine and every
conversation in them**, which is the one thing `/resume` cannot show you: `/resume` is
"which conversation, here", and this is "what is there at all".

**It is also what a bare `codeaf` opens on.** The conversation the launch picked is loaded
underneath, and `esc` — or `enter` on the row the cursor starts on, which is that same
conversation — drops into it. Home stays out of the way when you named a conversation
(`--session`, `codeaf resume`), on a `--once` or `--host` run, and on a machine whose only
conversation is the one already open. There is no welcome box when home greets you. Not
greeting you is not the same as being out of reach: `/home`, or `space` twice on an empty
box, opens it on a one-conversation machine and on an empty one alike, and over `--host`
it opens the far machine's.

There is no argument form. There are three other ways in: **`alt+1`**, home being the first
of the four places on the tab bar; **`space` twice** on an empty box; and **`tab`** from any
other place.

**It is seven panels**, in one column under 110 cells, two from 110 and three from 170,
always in one order: an unheaded list of open tabs followed by up to three dimmed
closed conversations, `needs you` (every question waiting on you, a digit answers the
top one from anywhere), `projects` (click a folder to select it for the next message),
`tasks` (the last day's tasks, running or landed, newest first), `since you left` (what landed while you were
away), `spend` (today and the fortnight) and `standing` (standing orders, soonest first).
Which column a panel stands in follows what it holds: the panels with rows fill the **field**
at the left, and the **rail** at the right holds `projects` and `spend` at its top with the
quiet panels under them. An empty panel keeps its heading and one dim line naming what
arrives there.

`↑`/`↓` walk a column, `←`/`→` cross columns, `enter` opens, `esc` closes back into the
conversation you came from. **Typing does two things at once**: what you type is a new
conversation waiting to be sent AND a live search over every project on the machine — the
panels give way to the matches, with `start a new conversation: "…"` directly above the box
holding the cursor, so type-and-enter still starts a chat. **A line that starts with `/` is
the third thing typing can be**: a command, run rather than sent (see *Typing a slash to see
the command list*). The box says `› type to search or start something new` and the foot
names the available draft controls:
`alt+p project · alt+e effort · alt+a approvals · alt+k chats · / commands`. The arrow, `enter`, `ctrl+o` and `tab`
keys still work, without hints on this row.

Search matches conversation names, project names, task titles and **what tasks came to** —
the one-sentence outcome — so `postgres` finds the chat whose work mentioned it, including
the ones no panel is drawing.

**`enter` opens any conversation on the screen, in any project**, and `ctrl+t` on a
conversation's row starts a fresh one in that row's folder; clicking a `projects` row
instead picks the folder your next message from home goes to. The conversation you were in is left
**open** behind it — still streaming, still running its tasks — and the new one is built on
its own workspace with that project's own permissions, crew and spend ceiling. Nothing is
carried across, because a second project is a second conversation rather than this one
moving. `tab` over an empty message box goes back. Home's page has the whole of it.

Refusals, exactly as written:

```
no conversation matches
/new is unavailable here
that folder is gone · <path>
```

`no conversation matches` is a search that found nothing — the `start a new conversation`
row is still there. `/new is unavailable here` is what the typing-to-start box says where no
fresh-session seam exists. The last is `enter` on a row whose folder has been deleted or
moved since its last conversation: home stays up and nothing is opened. **How many
conversations this terminal already holds is never a refusal.** Past twelve, a quiet
one left alone may be let go of; that is not a refusal of the one you asked for.
A task another window is running cannot be stopped from home: its `tasks` row says
`another window` and offers no stop.

## Changing approvals from the message box

Use `alt+a` (Option+A on macOS) or press the approvals cell above the message box to
cycle `asks → guardian → YOLO`. This works on home and in conversations. The keys and
permissions pages describe the scope and meaning of each posture.

`/approvals` and its `/yolo` alias are no longer commands. The `--yolo` launch flag is
unchanged.

## /permissions — what runs without asking

`/permissions` (or `/perms`) lists the answers you have banked, and gives you the way to
take one back. The rows are read from disk at that moment, not held from boot, so a card
you answered in another window five minutes ago is on the list. The permissions page
covers the rules.

Moving in it: ↑ / ctrl+p, ↓ / ctrl+n, pgup / pgdown by 9. A click on a row moves the
cursor there and acts on it; a click elsewhere closes the panel. esc undoes one thing at a
time — an armed row first, then the panel.

The heading is `what runs without asking`, drawn whether or not anything is under it. A
`$` marks a row about one shell command, a `◇` (or `o` in linear mode) a row about a
whole tool. The dim tail carries only what the heading did not already say: `every call`
for a tool row, `refused` for a deny, `asks every time` for a prompt, and nothing at all
for an ordinary shell allow. With nothing banked you get the heading and nothing under it.

Shell command rules keep the order they were written in, because first match wins. Tool
exceptions are sorted alphabetically.

To drop a line, press enter or **d** once: the row's tail becomes `enter again to drop
it`. Press again, or click again, and the line leaves your profile. Moving the cursor
disarms it. The receipt is `dropped · <name>`, with ` · from the next session` appended
when the running session's gate could not be told.

Refusals, exactly as written:

```
the shell command rules do not read back · <error>
the tool exceptions do not read back · <error>
"<name>" is no longer on the list
"<key>" cannot be changed here
```

The two read-back messages are said in the transcript when the panel opens, never as an
empty list.

**These rows are yours, not the policy in force.** Inside a repository that carries its
own approval rules, that project's row replaces yours wholesale at launch — so dropping a
line here changes what you carry everywhere and nothing inside that repository.

**Over `--host`, this page still reads this machine's saved rows, not the other machine's.**
The `◇` cell on the legend is the far session's actual approval posture, but `/permissions` has
no way to list or remove the far profile's individual rules yet. The page does not print a
host-specific warning in this build, so do not treat its rows as the rules governing the
remote conversation.

## /harness — the shapes of work you have saved

`/harness` (or `/harnesses`) lists your saved shapes of work, with what each one is for
and what its runs did. The registry is walked at that moment, so a harness registered in
another window is listed.

Moving in it: ↑ / ctrl+p, ↓ / ctrl+n, pgup / pgdown by 10. A click on a row acts on it; a
click anywhere else closes the panel. esc leaves.

A row reads `◆ name v3` on the left — the version is part of what the thing is — and
dimly on the right the description, then `6 runs`, then the last run in the same two words
`/subharness` uses for it: `2h · finished`, or `2h · incomplete`. A harness nobody has run
draws nothing there — not `never run`, not `0 runs`. A run count with an unreadable newest
trace shows just the count, because "never ran" and "ran, and I cannot read the trace" are
different facts.

enter **prints the harness's card into the conversation** and closes the panel: numbered
steps with the bounds under them, plus `last run` and that run's card when there is one.
It is prose, and prose belongs in the transcript where you can scroll and copy it. The
card keeps the columns it was written in — a line too wide for the frame is cut, never
wrapped, because the indent under a lane is what says which step belongs to it.

Building a harness is a conversation, not a command. This panel only says which ones exist;
`/harness ` with a space picks one to run on a typed request, and `/subharness` lists the
same programs beside everything else runnable here and starts one from its card. While one
is running, a chip with a spinner and its name leads the task strip.

Refusals, exactly as written:

```
harnesses are unavailable here
no harnesses are registered yet — build one in the conversation
```

The first is what you get with no registry wired — a headless frame, and every
`--host` or `--at` session, because the registry lives on the other machine. A plain
launch on this machine and `--no-host` both wire this machine's registry and open the
panel. The second is drawn as the panel's only row, and it is also what a registry that
cannot be read at all shows, rather than an error.

## /<program> — a program codeaf carries, handed a whole task

Every program your build carries is a command of its own: `/<name> <brief>` hands the brief
to that program and starts a task at once, exactly as `/task <brief>` does with codeaf's own
worker. The rows come from the build itself, so there is nothing to install and a build that
carries no program has no such row. With no brief it says its usage:
`usage: /<name> <brief> · hands the whole task to that program`.

Over `--host` the rows are the far machine's build's, and a row you run starts the work there.
The *Programs codeaf carries* page says what one is, what it cannot do, and where its work goes.

## /subharness — the command's two forms, bare and with a name after it

`/subharness` (or `/sub`) opens a filtering list of the programs this conversation can
run — the ones built in, the bundles on disk, and the harnesses you have had designed, in
one list. Typing narrows it over the name, the one line and the cues. `enter` opens that
one's intake card. `esc` closes.

`/subharness <name>` skips the list and opens that one's card. A name nothing answers to
is **not** an error: it becomes the list's filter, so a typo turns into a search.

It takes a name where `/harness` deliberately does not, and the reason is that a
subharness's name is one lowercase word written down in its own manifest — the same string
in the binary, in the store and on the command line — so somebody who knows which one they
want does not have to find it in a list first.

What it says when there are none, exactly as written:

```
no subharnesses here yet — a subharness is a saved program for work that comes round again.
```

That one sentence covers every way of having none — no registry wired, a registry with
nothing in it, and **every `--host` session**, because the registry lives on the far
machine. No list opens behind it. See the *Subharnesses* page for the card and its keys.

## /standing — the command's two forms, bare and with words after it

`/standing` (or `/orders`) has two forms, and they do different things. The words form
leads — making an order is what the command exists for; the page is the follow-up.

**With words after it, those words become a new standing order.**

```
/standing always run the tests before you say you are done
```

The tag form works in the middle or at the end too: `always run the tests /standing` and
`always /orders run the tests` hand the remaining sentence through the same door. Press
backspace immediately after the tag to make it plain words instead.

They go through the same deliberate door `ctrl+enter` opens: codeaf is told to shape the
sentence into a standing order's card — when it wakes, what it does, how far it reaches —
and it never carries the sentence out as one-off work as well. Nothing stands until you
answer the card. A sentence that cannot stand at all gets one short line saying so and
nothing else. Typed while an answer is still arriving it waits above the box and goes
through the marked door when its turn comes. On a build with no ambient side it says
`nothing here can hold a standing order` and sends nothing.

**Bare, it opens a page.** A short list under the message box of what stands over this
conversation, on up to three shelves, with `p` to pause one, `s` to stop one, `n` to except
this place and `enter` to open the conversation that asked for it. With nothing standing it
opens all the same, on its heading `standing orders` and one dim line:
`reminders, watches and routines · "remind me at 6" or "every morning at 9"`
Nothing is written into the conversation either way.

Nothing on the page is ever named at the command line — the words are always a new order,
never a query, because the only way to name one is to read it off the page first.

**The command list carries both rows, the words form first**: `/standing <words>` whose
tail reads `keep this true · a card, never work done once`, and under it `/standing` on
its own, whose tail reads `…or what stands over this conversation · stop, pause or not
here`. Pressing the words row puts the command in your box rather than running it. The
`+ /standing` row at the foot of the column on the right does the same thing.

## /task — start work you can walk away from

`/task <brief>` starts work directly from the words after the command; the brief does not
pass through the conversation model. **Nothing is waited for in front of it**: the task
exists the moment you press enter, the ordinary started-task row appears
(`single task 12 started · …`), and one worker starts on your own sentence — nothing is asked
of you. Two readings then run **beside that worker**, never before it:

- **A model writes the fuller brief** — your sentence kept word for word, with the
  constraints and the done-condition written around it. It reaches the worker a few steps
  in, as one message opening `YOUR BRIEF IS WRITTEN OUT NOW`, and from the moment the
  worker reads it that is the brief and done-condition the work is judged by. If it cannot
  be written, or arrives after the worker has finished, the work simply stands on your words.
- **A small judge reads your words for width.** If it finds separate jobs in them, those
  parts are weighed the way any division is and, where they hold up, handed out to other
  workers while the first one keeps going; the parts appear on the roster as their own
  rows. If it finds one job, nothing happens.

The row is called by the first words you typed for a second or two, and then by a short
name a small model gives it.

When a `/task` runs on the run engine (*The worker harness* page), the row it writes to the
tasks place carries the step its worker is on **right now** — the running glyph `◐`, the shell
lead `$` and the command — with the task's `N steps · $0.11` under it. The line is there only
while a step is in flight, and goes the moment the command ends.

`/task` is also a live tag in the middle or at the end: `investigate the flaky test /task`
strips the tag and takes the remaining words through this same road. Backspace
immediately after the tag makes it plain prose.

**A bare `/task` opens the full-screen task page** — the same page `/history` and `ctrl+.`
open, holding every task this project has ever run. It does *not* print a usage line, and
it starts nothing. On a project that has never run one it opens the page anyway, headed
`tasks` over one line: `work you send off with /task lands here, and its record stays`. The `+ /task`
row at the foot of the task column types `/task ` into your box, which is why the word on
its own has an answer worth giving.

The *work that runs on its own* page has the brief in full, under *Why my task's brief is
longer than what I typed* and *Why my task is called something I did not type*.

`/task solo <brief>` starts one worker in the same instant and asks for no reading of its
width at all. Its brief is still written beside it.

**`/task adaptive <brief>` is retired**, and it is the only `/task` word that ever opened an
adaptive run. Typing it now starts an ordinary task: your brief is kept exactly as typed —
the word is left in it rather than cut out, because a brief that genuinely opens "adaptive
rate limiting for the api" must not lose its first word — and one dim line says what
happened:

```
/task adaptive retired · the word stays in your brief, and the work starts as one worker that can split as it goes
```

`/task adaptive` on its own, with no brief after it, starts nothing and prints the usage
line instead: `usage: /task <brief> · /task solo <brief>`.

The `starting a task` row in `/settings` → Session decides what the plain form does:
`sized` is the default and is the behaviour above, and `single` always starts one worker
and does not read its width at all. `solo` typed on the command line overrides the row
either way. Two of the row's old answers are gone — `ask` with the two-choice list it
opened, and `adaptive` with the planner road itself — and a profile still set to either
reads as `sized`.

## What happens when I type /task

The task starts. There is no wait to watch: the started-task row appears as soon as you
press enter and the task's row is on the roster, where you can open its room and see the
worker at work. What you used to wait for — `sizing it up…` and then `shaping the brief…`,
up to half a minute on a thinking model before the task existed — now runs beside the
worker instead (see */task — start work you can walk away from* above). If starting fails,
the error sentence is the only thing written.

## Why is there a line next to my task

The thin `▏ ` hairline at the transcript tail is the forming block, and it is drawn only
while a task you **approved from a proposal card** is coming into existence: the word
`task`, the proposal's name, and a spinning mark with a clock on `shaping the brief…`. It
is a single left hairline, not a box or a task-status border, and it collapses into the
ordinary task row the moment the task appears. A typed `/task` draws no forming block,
because it has no wait in front of it.

## Can I see the brief while it is being written — the preview line under shaping the brief

Not any more. `/task` used to wait on the brief behind `shaping the brief…`, with one dim
row under it showing the newest words — the model's thinking in italics, then the brief
upright — and a window that opened on `→`. That wait is gone, and the line, the italics and
the window went with it: the brief is now written beside the worker, so there is nothing on
screen to preview. An approved proposal's forming block never had a line under it, because
its brief was written before you were asked.

What you can see is the brief itself once it lands: the task's own room carries it in full,
and the worker's transcript shows the message it was handed, opening
`YOUR BRIEF IS WRITTEN OUT NOW`.

## Several tasks forming at once — one block with a row each

Two approved proposals can be forming at the same time. They share **one** block rather
than stacking two blocks at the tail of the transcript:

```text
▏ tasks · 2 forming
▏ ⠙ release notes · 13s
▏ ⠙ nil-map crash fix · 9s
```

The head counts them, and each is one compact row: the same spinning mark, its name, and
its own clock. Each row collapses the moment its task appears. There is nothing to open
and nothing to walk between — the arrows keep their ordinary meaning — and with one task
forming there is no head that counts, only the block described under *Why is there a line
next to my task*. A typed `/task` never joins the block: it has no wait in front of it.

## /history — the task history command: past tasks, every task this project has run

`/history`, or `ctrl+.`, opens a full-screen page holding the project's whole task record:
this conversation's work **and every earlier conversation's**. It is the one place that
answers "what did we do about this last week" — the roster's column beside the conversation
is built from this session's own work, and carries only a short dulled note of the rest.

**It is not `/tasks`, and there is no `/tasks` command.** `/task <brief>` and its `solo` form
mean *give codeaf work*; this page starts none, so it does not share their word. Typing
`/history` is the only slash form — but the PLACE this opens is called `tasks` on the tab
bar, and **`alt+2`** and `tab` reach it without a command at all. The word is a place, not a
command.

Two sections. `running` is the tree of everything still going, drawn whole, with each task's
current call, clock, tokens and spend under its name — and a row a run writes there carries
the step its worker is on right now: the running glyph `◐`, the shell lead `$` and the command,
with the task's `N steps · $0.11` beneath it. `earlier` is a flat list, newest first, of
everything the project has finished — one line each, the same rows the `@` list offers.

**Type to filter.** Any printable key, spaces included, narrows both sections at once
against the titles, the ids and the outcomes; the bottom of the page shows what was typed
as `filter · port`, and a section with no match disappears entirely. `backspace`, `ctrl+w`
and `ctrl+u` edit it. `esc` clears the filter first and closes the page on the second press.

`esc` closes it. `↑`/`↓` move, `enter` opens the row: a task this session is holding opens
its room, and a task another conversation ran goes into your message box as
`@its-name` — that conversation is closed, so there is no room to open, and the mention is
what carries its outcome and its transcript to the model when you send.

Its `running` section also carries **a row for each task every other codeaf window open on
this directory has out right now**, marked `another window` on the right. Those rows take
no cursor and `enter` does nothing on them: there is no room here and nothing has landed for
a mention to point at. They are how you find out that the directory is busy somewhere else.

On a project that has never run a task the page opens on its heading and one line,
`work you send off with /task lands here, and its record stays`. The tasks pages describe the
page in full.

## /crew — the five models codeaf uses on its own behalf, read beside the one you talk to

codeaf runs **six model seats**. Seat one is the model you talk to, and `/model` is what
moves it. The other five — reflex, small work, worker, careful work, mastermind — are the
models codeaf uses on its own behalf, for the calls you did not type and for the work
inside every task. `/crew` reads all six and sets the five in one word. **It never moves
seat one.**

```
/crew
```

opens the six-seat reading, bottom-anchored like the model picker. From the top:

```
the five models codeaf uses on its own behalf — not the one you chat with
  you talk to · deepseek-v4-flash
  family ‹ open models · all models ›
  frugal — glm-flash works and thinks, qwen-max checks
    reflex       google/gemini-2.5-flash · small work   deepseek/deepseek-v4-flash-0731 · worker       z-ai/glm-5.3-flash · careful work qwen/qwen3.8-max-0902 · mastermind   z-ai/glm-5.3-flash
› balanced — glm-flash works, fable checks, opus thinks
    reflex       google/gemini-2.5-flash · small work   deepseek/deepseek-v4-flash-0731 · worker       z-ai/glm-5.3-flash · careful work anthropic/claude-fable-5.1 · mastermind   anthropic/claude-opus-5
  max — glm-5.3 works, fable checks, opus thinks
    reflex       google/gemini-2.5-flash · small work   deepseek/deepseek-v4-flash-0731 · worker       z-ai/glm-5.3 · careful work anthropic/claude-fable-5.1 · mastermind   anthropic/claude-opus-5
each of the five can be pinned on its own in /settings → Providers
```

The first line says what the presets change and what they do not. The second is **seat
one** — `you talk to · <model>`, spelled as the legend above the box spells it — with no marker and
no highlight, because nothing in this chooser can move it. The third is the **family**: **←→** moves `family`, which says which pool the three presets below it draw from, open weights or the
whole catalog. Then the three presets: the one
in force wears a highlighted ground, `›` is where **enter** is aimed and it opens on yours,
↑ / ctrl+p and ↓ / ctrl+n move, and **esc** closes without changing anything. The last
line points at the settings row where one seat can be pinned by itself; the chooser does not
pick seats one at a time.

If you have pinned one of the five yourself, no preset wears the ground and the chooser says
`yours is none of the three — picking one puts all five back` above the closing line.

A word that is not one of the three changes nothing and prints the three:
`/crew cheap` answers `/crew cheap · not one of the three` and then the listing.

What each of the five classes funds, and how to set one of them on its own, is on the models
page.

## /crew <preset> — the confirm line, and the model it leaves alone

`/crew frugal`, `/crew balanced` or `/crew max` sets the five and confirms in one line:

```
crew → max · brain claude-opus-5 · hands glm-5.3 · checks claude-fable-5.1 · you are still talking to deepseek-v4-flash — /model changes that
```

**The model ids are drawn brighter than the words around them.** `crew →`, the preset
word, `brain`/`hands`/`checks` and `you are still talking to` stay at the grey every note
is written in; the three crew ids and the model you are talking to step up, because they
are what the command was typed to find out. `/model` wears the command chip, because it is
a command you can type. See "Why is one word in a line brighter than the rest" on the
screen page.

The last clause names, by id, the one seat the command did not touch: the model you are
talking to, in the same spelling the legend above the box uses, so you can check it
against the line over your own prompt. `/crew` never changes that model and never offers to; only
`/model` does. When the session has no model yet the clause reads
`the model you talk to is untouched — /model changes that`.

**The change is live.** The next call codeaf makes on its own uses the new crew — no
relaunch, and no waiting for the next session. To read the crew back afterwards: the live
`/status` prints the `crew` line under `model`, the phone's status sheet has the same row,
`/settings` → Providers has the crew row, and bare `/crew` opens on yours.

**That promise is local-session only.** Over `--host` the session resolves its crew from
the other machine, and there is no crew write across the connection — so `/crew` refuses
rather than writing this laptop's profile behind your back:

```
devbox owns the crew · change it on that machine
```

For the same reason a remote window shows no `crew` line in `/status` and none on the
status sheet. Change that machine's profile there.

## /connect — your connected accounts

`/connect` (or `/connections`) opens the connection panel. Its pinned `models` group
holds the six built-in model services plus every one already connected; the account
catalog groups follow it. The Codex row says `browser`; enter opens the sign-in road and
the waiting card keeps the address available to copy. The other listed services say what
they need. Pick a row and connect it. There is no argument form. **Custom OpenAI-compatible API** connects a custom service: it asks for a
base URL, then a name of your own with the host's own spelling pre-filled (`127.0.0.1`
becomes `127-0-0-1`), then a key. Several custom connections sit beside each other,
each under its name; once one is connected an `add custom connection` row appears and
the **Custom OpenAI-compatible API** row becomes that connection's edit door. The
[services page](services.md) covers model keys, and the accounts page covers what each
account can do once it is connected.

Refusals, exactly as written:

```
connecting an account is not available over --host yet — the sign-in opens a browser here and the account belongs to the machine over there. accounts already connected on that machine keep working.
```

```
connections are unavailable here
```

```
there is nothing to connect yet
```

The first is the `--host` or `--at` answer. The second means a headless frame has no
connections seam wired; a plain launch on this machine and `--no-host` always wire one.
The third means the catalog came back empty.

The settings panel has a `Connections` tab over the same accounts. It is a different
surface from `/connect`, not a second copy of it.

## Which machine's settings are these

The settings panel shows this machine's profile. Over `--host`, the conversation
runs with the other machine's profile and project rules. Changing local settings
does not silently change the remote conversation's approvals. The panel explains
this split when it opens; configure the remote profile on that machine.

## /settings, /set, /config — opening and navigating settings

`/settings` (or `/set`, `/config`, or ctrl+,) opens a fullscreen page: a tab bar over the
codeaf settings, plus a tab of connected accounts. It was the first of the three fullscreen
pages here — the others are `/history` (the task page, ctrl+.) and `/home` — and **only one
of the three is ever up at a time**: opening any one closes the other two.

Moving in it:

- ↑ / ctrl+p and ↓ / ctrl+n move a row at a time. Headings are stepped over, never landed
  on. pgup/pgdown move 16. home/end jump to the ends.
- ← and → switch sections, clamping at the ends rather than wrapping. **`tab` no longer
  does**: it is the way to the next place — home, tasks, standing, memory, spend, search,
  settings — here as everywhere else, and `shift+tab` walks that circle back. The panel's own
  bar is the second one, under the places' bar.
- **Any printable key types into a search box** that filters across all tabs at once,
  grouping matches under faint tab headings and moving the tab bar to the first match's
  tab, so backing out leaves you where the thing lives. backspace,
  ctrl+w and ctrl+u edit the query. The search matches the label, the settings
  key, the description, the value and the tab's name — `spendRail`, `ceiling`
  and `prompt` all find rows.
- enter and space open or change the row; while a search is on, space types.
- A click on a tab word switches tabs. A click on a row **selects** it, and a second click
  on the already-selected row **acts** on it. One press never does both.

esc backs out one layer at a time: the search first, then anything the `Connections` tab
has standing open, then the panel. The head line says `esc close` on the right. The
bottom legend normally reads
`↑↓ move · ←→ tabs · enter change · type to search · esc close`.

Only the selected row shows its description, at most two wrapped lines. A `•` (or `*` in
ASCII) before a value means you changed it from the untouched default. `set by <NAME>`
after a value means an environment variable holds it.

A terminal too short to draw the whole panel keeps its head and its foot.

## Where settings are saved

Every change made in the settings panel goes through the settings registry and is
**saved to your global profile**.

So does a change you ask codeaf for. There are **two doors onto one file**: the panel,
and the `change_setting` tool codeaf reaches when you say "set my daily budget to 5" or
"use a different model for planning". Both go through the same registry, take the same
validation, refuse in the same words, and land in the same `config.json` — so a row you
change by asking is a row you find changed in the panel, and the reverse. `settings` is
the read beside it, listing every row by the key `change_setting` names. Which rows
codeaf refuses to change for you, and why, is on the permissions page.

The project layer, `<workspace>/.codeaf/config.json`, is deliberately not writable from
the panel — or from `change_setting`, which writes your profile only. The foot line says
so:

```
saved to your profile · a project's own .codeaf/config.json is a hand edit
```

So a value set here follows you between projects, and a value a project sets for itself
has to be edited by hand in that file. When a project answers the same row, a write
through `change_setting` says so rather than reporting a change that is not in force.

Over `--host`, opening the panel first notes:

```
these rows are this machine's — the ones that govern the conversation are read from the profile on the other one
```

and then opens anyway.

Refusals inside the panel, exactly as written:

- A row pinned by an environment variable refuses, and the foot says
  `held by <name> — unset it to change this here`.
- The conversation's model and the six capability slots — looking, drawing, speaking,
  composing, filming, voice — are all live here: a pick is saved to your profile and takes
  effect on the next thing that uses it, with no relaunch. The only slots that refuse are
  the **roles** nothing on this surface resolves — planning, verification, naming — and
  they say `that model is chosen where its session is opened`.
- Any refusal from the settings registry is shown on the foot line in the registry's own
  words. Nothing is swallowed.
- A search that matches nothing says `nothing matches`. The `Connections` tab has its own
  sentences.

## config.json keys are not read — why codeaf says a setting I wrote is ignored

codeaf reads the top-level keys of your profile's `config.json` that a settings row or
the model-service setup owns. A key nothing reads — a hand-written `models` object, a
spelling from another tool — does nothing, and the defaults apply in its place. (A key
codeaf itself retired is passed over quietly rather than named.) So the conversation says so once, as a note:

```
config.json keys are not read: models, tiers; anything set under them is ignored and defaults apply.
```

It names every unread key, sorted. It is said **once per profile for that set of keys**:
the next launch with the same keys says nothing, and a set that changes — a key added
or taken away — is said again. It is not a tip, so turning tips off in the Display
tab's `hints` row does not hide it, and a conversation over `--host` says it about the
profile on the machine running the work. Nothing is rewritten: to act on it, move the
value to the key a settings row names (`settings` lists every one), or delete the key.

## The nine settings tabs

The tabs, in order:

```
Session · Context · Workspace · Display · Spending · Safety · Tasks · Providers · Connections
```

## The settings tab strip on a narrow terminal

The nine tabs need about 96 columns. On anything narrower the strip **scrolls** rather
than being cut: the tab you are standing on is always drawn and always inked, its
neighbours are drawn while they fit, and each end that had to give a tab up wears a `…`
saying there is more that way. `←` and `→` still walk the nine one at a time, and the
strip follows.

It anchors left while you are near `Session` and right while you are near `Connections`,
so both ends of the walk look exactly like a strip that fits. A tab the strip could not
draw cannot be clicked, because nothing on this surface acts on something you cannot see.

**Session** — the rows this conversation carries that belong nowhere else: "memory" and
"fallback models". Two rows, and that is the honest size of it.

The four ssh rows a `--host` conversation rides on ("ssh reuse", "ssh heartbeat", "ssh
missed heartbeats", "ssh traffic") are **not** here any more — they are on **Workspace**.
Every one of them lands on the next launch rather than on the conversation in front of
you, so they are a fact about this machine and not about this session. Nothing you saved
moved: the keys they are stored under are unchanged.

The five models codeaf uses on your behalf are **not** here — they are on Providers, with
the row that says which model you are talking to. They used to be on this tab, one tab away
from it, which made "which model does the planning" and "which model am I talking to" two
errands on two screens. Neither is the conversation's own money limit here any more: it is
`per conversation` on **Spending**.

**Context** — what a model carries. Rows: "compact at", "answer room", "working set",
"context reuse", "searching", "exa key", "firecrawl key", "jina key".

The selected **searching** row is a live explanation rather than a static
description. On untouched `auto` it reads `now firecrawl, keyless — set
search.exaKey or search.firecrawlKey to raise it`; a pinned Exa row with no key
quotes the exact `Search failed (exa): no API key` answer; a configured pin
reads `exa, with your key`. Provider and key changes land on the next search in
the conversation already open.

**Workspace** — this machine and this project: what codeaf does with its own time here, and
what it may reach on your behalf. Rows: "quiet before practice", "arrival brief after",
"tenure after", "background checks", "model in commits", "google sign-in id", "google sign-in
secret", "slack sign-in id", and the four ssh rows — "ssh reuse", "ssh heartbeat", "ssh
missed heartbeats", "ssh traffic". **It holds no money row at all** — every one of those
moved to Spending.

The ssh rows are here because "what may codeaf reach on your behalf" is this tab's own
question, and a link to another machine is that question asked about a machine rather
than about a service. They are not on the tab named **Connections**: that one is the
catalog of third-party accounts you sign in to, and it is built from the account list
rather than from the settings registry.

**"background checks"** is on by default: one small timer under your own login checks
your reminders, watches and routines every 5 minutes with no window open. Off removes it
and nothing standing is lost — see *Keeping an eye on things* for the whole of it.

**Spending** — money, and nothing that is not money. Seven lines: the reading `today`, then
"per day", "per conversation", "per plan", the readings "per task" and "per standing run",
and "practice". `/budget` and `/limits` open it. *Models, context, and what it costs* has
every row and every door onto them.

**Safety** — what codeaf may do without asking you first. Rows: "ask before running",
"tool exceptions", "shell command rules", "guardian", "approval countdown", "background
after", "task countdown", "who settles work that needs a look".

**Tasks** — how work you can walk away from is run. Rows: "starting a task", "check task
work", "task repair rounds", "tasks at once", "busy machine", "memory floor", "task
model".

## What the number on a settings row is counted in

Every row that shows a number shows what the number counts, so a whole tab can be read
without moving the cursor onto each row. The unit is part of the value:

| Row | Reads | Counted in |
| --- | --- | --- |
| ssh reuse | `300s` | seconds a connection stays reusable after its channel closes |
| ssh heartbeat | `3s` | seconds of silence before ssh asks whether the far machine is there |
| ssh missed heartbeats | `3` | unanswered heartbeats — the label says what is counted |
| approval countdown | `10s` | seconds an approval question counts down before it pauses and keeps waiting |
| background after | `30s` | seconds a foreground command runs before it becomes a job |
| task countdown | `15s` | seconds a proposed task waits for you before it starts |
| compact at | `60%` | how much of the model's window is filled before compaction |
| answer room | `65536 tok` | tokens every call keeps free for its answer |
| working set | `160000 tok` | tokens of material kept quoted in front of a worker |
| context reuse | `250%` | shares of one whole context a job may re-send — 100% is once |
| memory floor | `1536 MB` | megabytes that must be free before another task starts |
| busy machine | `1.5 per core` | the load average per core at which new tasks wait |
| tenure after | `3 clean firings` | clean firings a standing charter needs to earn tenure |
| chat width | `50%` | the chat pane's share of the frame while the task rail is open |

A row whose label already names what is counted — "task repair rounds", "tasks at once",
"ssh missed heartbeats" — shows the figure alone rather than saying the word twice.

**A row takes its unit back.** The box a value is typed into opens on the bare figure, and
typing the unit back in is accepted: `120s` on "ssh reuse" and `120` both save 120
seconds. Anything else is refused in the row's own words.

**A duration row is different and always was.** "quiet before practice" and "arrival brief
after" are written the way you would say them — `20m`, `4h`, `1h30m` — and `0` turns them
off.

**Display** — how the surface draws itself and what it remembers of your typing. Rows:
"input history", "keep drafts", "task column", "chat width",
"mouse", "timestamps", "turn work". There is no "nerd font" or "linear mode" row: icons
need no patched font anywhere on this surface, and the accessible single-column rendering
is the `--linear` flag at launch rather than a persisted setting.

**Providers** — which model answers what. It leads with the **Models section**, in this
order:

1. **your model** — the model you are talking to. It is the conversation slot, and picking
   here is the same road `/model` takes — the same picker, providers and all. Its value carries
   the provider serving it: `deepseek/deepseek-v4-flash · auto (cloudflare now)`.
2. **provider** — which provider answers your model. enter opens them with
   what has been measured of each, and enter on one pins it.
3. **speed guard** — whether an answer slow to start is asked of the next-best provider as
   well.
4. **routing** — what every request prefers among the providers, and it cycles
   `simple`, `latency`, `price`, `off`. **`simple` is what it ships as**: codeaf sends no
   preference of its own, a provider you pinned goes out as the whole request, and with no pin
   the router's own default routing answers. `latency` asks for the fastest provider and
   `price` for the cheapest, on every call. With `off` nothing is measured, so the two rows
   above it have no provider to name.
5. **prompt profile** — how much codeaf tells the model before you type: `auto`, `lean`,
   `full`. Another cycle row. `auto` reads the model's context window and goes lean under
   32,000 tokens (see *Models, context, and what it costs*).
6. **crew** — the five below, chosen as one word: `frugal`, `balanced`, `max`. It is a cycle
   row: enter or space walks it. Answer any of the five yourself and it reads `custom`.
7. **reflex** — `near-free · reads every turn — memory, titles, safety`
8. **small work** — `cheap · the small calls — names, digests, the safety gate`
9. **worker** — `does the work · every task, its parts, every run node — most of the bill`
10. **careful work** — `careful · checks what must not be wrong — audits, briefs, vision`
11. **mastermind** — `thinks · plans runs and designs harnesses — add :low, :medium or :high`
12. **pinned roles**, and hanging off it the **roles** list — one row per auxiliary call
    codeaf makes for itself, grouped under its class. Those rows come from the running binary
    rather than the settings registry.

**A pin for a role this build no longer has is ignored, and the row stops showing it.** Roles
come and go with the calls that use them — `compaction` was one, and a compaction has not asked
a model since long before it was deleted. A pin left behind for a word like that is dropped
when the row is read, never written back, and the foot line says `compaction is no longer a
role — that pin is ignored` the next time you change any pin. **Every other pin on the row
keeps working**, which is the whole point: the row is one string holding all of them, and
refusing the lot over one dead word would leave you unable to change any of them without
editing `config.json` by hand.

Typing a word that is not a role is still refused outright, with the real names listed — that
refusal is for the pin you are adding now, which is the one you can do something about.

The four machine rows lead because the endpoint serving your model is part of the same
decision as the model, and they used to sit at the foot of the tab, forty rows below it.

Then the rest of the tab: "looking", "reading", "reply guard" — whether a reply
that has come apart is cut and asked again, on by default (see *Models, context, and what
it costs*) — and one row per capability slot added automatically from the settings
registry: drawing, speaking, composing, filming, voice.

The first four of the five classes are **select** rows and open the model picker. The
**mastermind** row is a **text** box instead, because its value may carry a thinking level
(`moonshotai/kimi-k3:high`) and a picker hands back a bare id.

The connected model services have their own section on the tab, each with its billing
door, the safe spelling of its key, its region and its order. The section ends with an
`add custom connection` row, and once a custom connection is connected an `active
connection` row follows it: it reads
`answering on localhost · enter moves it to homelab`, and enter moves this conversation
onto the next connection, wrapping past the last back to the first. The
[services page](services.md) has the whole of it.

**Connections** — the accounts this profile has connected and what each may do. Its rows
come from the engine rather than the settings registry.

## Changing a row in settings

A settings row is one of four kinds.

**toggle** — enter or space flips it on or off in place.

**cycle** — enter walks a short list of choices, in the registry's own order.

**text** — enter opens a one-line box at the foot. Its legend is
`enter save · empty clears · esc cancel`. A secret row shows bullets, and an unchanged
mask counts as no change — so pressing enter on a row you only looked at does not
overwrite your key.

The line above the box says which row you are changing **and what it will take**, in the
same words the row refuses in: `per conversation · an amount in dollars, like 5 or 2.50 —
or none for no limit`, `ssh reuse · a whole number`, `guardian · one of: off, on`. A row
that only takes text says nothing extra, because "text" is not a fact about a row.

**select** — enter opens the model picker itself, the same component `/model` opens, in
the list's place. It is filtered to the question that row asks: "looking" only offers
models that can see, "drawing" only ones that draw, "speaking" only ones that speak,
"composing" only ones that compose, "filming" only ones that film, "voice" only ones that
hear, and everything else follows the general chat rule. Its legend is
`↑↓ move · enter choose · esc cancel · type to filter`.

Because the picker is the same component, everything true of `/model`'s ranking, its rows
and its ctrl+t effort knob is true here too — **including the providers** on the row that has
them. On **your model**, `→` or `tab` unfolds the providers serving the model under the
cursor, walks the cursor into them, and `enter` on one pins it, exactly as under
`/model`. The legend says `↑↓ move · → or tab providers · enter choose · esc cancel · type to filter`
on a model and `↑↓ move · ← or tab back · enter choose · esc cancel · type to filter`
inside its providers. The media slots
have no provider row behind them, so nothing unfolds there and the legend does not offer the
key.

While any of those layers is up — the value box, the model picker, the key box on the
Connections tab — the foot drops `tab next place`, because the layer has taken that key.

## The roles rows in settings — pinning a role, and del to unpin

The **roles** list sits on the **Providers** tab, directly under "pinned roles". Each row is
one call codeaf makes outside a turn — `title`, `guardian`, `auditor`,
`planner`, `designer`, `worker`, `router`, `vision`, `reflex`, and `spellout`, which is the
one of them you ask for yourself with `ctrl+r` (see the keys page) — drawn as
`<role>    <model>`, with `pinned` after it when that role has a model of its own.

The rows are **grouped under their class**, in the same order the five class rows are drawn
above them: `roles · reflex`, `roles · small work`, `roles · worker`, `roles · careful work`,
`roles · mastermind`. The class is the heading, so it is not repeated on every row — which
leaves the widest part of the row for the model id it is there to show.

Stop on a row and the line under the list says **what that role is** and where its answer
came from: `the plan that steers an adaptive run · follows mastermind above. enter pins it
to a model of its own.`

- **enter** opens the model picker and pins the role to what you choose.
- **del** on a pinned row clears the pin. The legend says `del unpin` while you are on one,
  and del does nothing on any other row of the sheet.
- Typing filters these rows too: they answer to their own names and to the line that says
  what they do — neither of which is in any settings key. Searching for `image` finds
  `vision`, whose description mentions it.

Every pin is written into the "pinned roles" registry row and nowhere else, so the list and
that text box are one setting seen two ways. What each role does and how the tiers work is
in the models page.

## Which model draws my pictures, speaks, films, or looks at an image

Each of those is one row on the **Providers** tab, and the row is the front door: the model
you pick there is the model that runs. A blank row reads `automatic`, which is not "off" —
it means codeaf picks one for you.

Choosing is resolved at the moment something is actually drawn, spoken or looked at, down
one order:

1. the row you set here (an environment variable of the same name still wins over it —
   `CODEAF_IMAGE_MODEL`, `CODEAF_SPEECH_MODEL`, `CODEAF_MUSIC_MODEL`, `CODEAF_VIDEO_MODEL`,
   `CODEAF_VOICE_MODEL`, `CODEAF_VISION_MODEL`);
2. a role pinned in "pinned roles" — `imagegen`, `speech`, `video`, `vision`;
3. the best model the catalog advertises that publishes the capability;
4. a name this build remembers.

Steps 3 and 4 read one preference list per kind of media, strongest first, and these are
the names at the head of each list — what an `automatic` row actually gets:

| kind | default |
| --- | --- |
| draws | `bytedance-seed/seedream-5-0-pro` |
| speaks | `fish-audio/s2.1-pro` |
| composes | `google/lyria-3-clip-preview` |
| films | `bytedance/seedance-2.5` |
| looks at an image | `google/gemini-3.7-flash` |

Each list has older names under its leader — `krea/krea-2-medium-turbo` under drawing,
`fish-audio/s1`, `openai/gpt-4o-mini-tts` and `hexgrad/kokoro-82m` under speech,
`google/lyria-3-pro-preview` under music, `bytedance/seedance-2.0-mini` under video,
`qwen/qwen3.8-27b` under looking — used when
your catalog does not advertise the leader. When the whole list misses, the catalog's own
rows are used — settled ones first: a row whose name marks it experimental (`-exp`,
`-preview`, `:free`, alpha, beta, a stealth vendor) is passed over while any ordinary row
can do the job, because those rows often sit behind a data-policy opt-in your provider
account may not have made. **These are defaults, not choices made for you**: a row you set, or the matching
environment variable, wins over every one of them and is never overwritten.

**Every step is checked against what the catalog says the model can do.** A row or a pin
naming a model that cannot do the job is skipped and the next step is used, so a model that
was renamed degrades to a working one instead of failing at the provider. If nothing on the
list can do it, that ability is simply absent rather than present and failing.

A change here lands on the **next** picture, sentence or film — not on the next launch.

## /manual — how do I read the manual, is there a help page, show me the page about a command, ask codeaf about itself

`/manual` puts a question about codeaf to the model **with the manual open**. The words
after it go out as a turn of the conversation, told to answer out of codeaf's own manual —
the same pages the chat reads whenever you ask what a key or a command does — and to say
which page the answer came from, so you can go on and read that page yourself.

| Typed | What happens |
|---|---|
| `/manual` | asks what codeaf can do, and which pages are worth reading first |
| `/manual how do I change the effort level` | puts that question; the answer names the page it came from |

Your line in the transcript is what you typed — `/manual how do I change the effort level`
— and the answer lands under it the way every answer does. **It is a turn**: it goes to the
model this conversation is on and costs what a turn costs. While an answer is already
coming it steers that turn, exactly as a plain `enter` does.

**On home it opens a conversation first.** Home is not a conversation, so `/manual` there
is one of the commands that *opens a conversation here first* (see the home page): a
conversation opens at the folder named at the right of the keys row and the model on the
rule above the box, home closes,
and the question is sent there. Until 2026-09-22 `/manual` on home printed its answer into
the conversation *behind* home, where nothing could be seen of it — typing it looked like
nothing happening.

**To read a page as it is written, with no model call**, use the terminal: `codeaf manual`
lists every page and `codeaf manual <page>` prints one whole (next section). Until
2026-09-22 `/manual` did that in the conversation too — a bare `/manual` listed the pages,
`/manual <page>` printed one and `/manual <question>` printed the sections that answered
it, spending nothing — and that reading now lives at the terminal alone.

## codeaf manual — reading the manual from the terminal, without a key and without spending anything

The same manual is a command line, for the questions people ask **before** they have set
anything up:

```
codeaf manual                          every page, one per line, with what each is about
codeaf manual permissions              that page, printed as it is written
codeaf manual "who can see my files"   the sections that answer it, labelled with page and heading
```

It needs no API key, makes no model call, opens nothing and spends nothing — so
`what is this`, `what does it cost` and `who can see my files` are all answerable on a
machine where you have not decided yet whether to set codeaf up. Quote a question so your
shell hands it over as one piece.

**Nothing is cut.** A page printed here is the whole page, however long it is — the limits
the chat reads under are about what a model can be handed at once, and a terminal has no
such limit. If a page is longer than your screen, pipe it: `codeaf manual keys | less`.

`codeaf --help` lists it beside the other commands.

## codeaf <command> --help — asking one command what it takes, which is not a failure

Every command in the terminal answers `--help` (and `-h`) with its own usage: the line
that names its shape and its flags, then its flags one to a row, then

```
run `codeaf --help` for every command and the environment table.
```

It goes to **standard output** and the command leaves with **0**. Asking a program what
it takes is not a failure, so a Makefile or a CI step that runs `codeaf do --help` to
check the binary is healthy reads a command that worked. This includes the commands that take
no flags at all — `codeaf show --help` and `codeaf cache --help` answer the same way
rather than reading `--help` as a filename or ignoring it.

**A flag that does not exist is still a refusal**, and it is said once, on the **error
stream**, and leaves with **1**:

```
error: flag provided but not defined: -nosuchflag
  codeaf do   "<task>" [-w dir] [--json] [-o file] ...
```

— the sentence, then that command's same usage, so the fix is on the screen beside the
complaint. Nothing goes to standard output, so a script reading the answer never sees a
refusal mixed into it.

The usage one command prints is **read out of the table** `codeaf --help` prints, not
typed out a second time beside the flags, so the two can never disagree about what a
command takes or what its codes mean.

## What codeaf manual refuses at the terminal — a page name that does not exist, and a question with no answer

A **name** you type at the terminal is an exact request, so it gets an exact answer or an
exact refusal — never a near miss quietly shown as though you had asked for it.
`codeaf manual no-such-page` says there is no page by that name, prints the list of pages
there are, changes nothing, and **exits non-zero**, so a script can tell a missing page
from a page it just read.

A **question** the manual has nothing on is a different thing, and it is an answer rather
than a failure: you are told

```
the manual has nothing on that, which usually means codeaf does not do it
```

followed by the list of pages, and the command exits **0** — the manual saying "no, codeaf
does not do that" is a fact about codeaf, not a broken command.

In a conversation `/manual` refuses nothing: the words go to the model, and a question the
manual has no page for is answered by the model saying so. The manual describes **this**
conversation surface. It has no pages about anything else, and the model is told to answer
questions about codeaf out of it rather than out of what it remembers about other programs.

## codeaf --help, and --help on any command — what does this command take, what are its flags, how do I see the usage

`codeaf --help` prints every command, what each is for, and the environment table under
them. **Any single command answers for itself the same way:**

```
codeaf do --help
codeaf logs --help
codeaf exec --help
```

Each one prints that command's own line — the shape it is called with, and what its exit
codes mean where it has any — then its flags, one to a line, with what each does and what
it defaults to. It goes to **standard output** and the command **exits 0**: asking for help
is not a failure, so `codeaf do --help` inside a Makefile or a health check reads as a
command that worked. `-h` says the same thing.

A flag that does not exist is the other answer: the refusal said once, then that same usage,
both on the **error stream**, and a non-zero exit.

Flags are written with two dashes for a word and one for a single letter — `--json`,
`--timeout`, `-w`, `-o`. Either spelling is accepted whichever way you type it.

## A command name I typed wrong, and a command I gave nothing to

A word codeaf does not have is answered with the nearest one it does:

```
there is no `codeaf lgos`. did you mean `codeaf logs`?
run `codeaf --help` for every command
```

Two lines, not the whole book — the command list is behind `codeaf --help`, and the
environment table behind `help env` on the same door, where either can be read without the
answer scrolling off the top.

`codeaf do` and `codeaf plan new` with nothing after them say `no goal given` and print
**that command's** line, not every command's. Pipe the task in instead if it is long:
`echo "the task" | codeaf do`.

## Reopening a closed chat is a key rather than a command

There is no slash command that reopens a tab you closed. **`ctrl+shift+t` does it**
— the last tab you shut comes back, and pressing it again walks further back
through the ones before it. The keys page has the whole of it under *Reopen a tab
you closed*, including what a terminal that cannot send the key does instead.

`alt+k` is the other way back: it lists every conversation on this machine, closed
tabs included, and opening a row brings the tab and its draft back too.
