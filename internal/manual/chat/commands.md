# Commands

## Typing a slash to see the command list

Type `/` and the command list opens under the message box. There is one list of commands
in aforge: the pop-up you get by typing `/` and the list `/help` prints are drawn from
the same table.

**It opens at a word boundary, and not only at the start of the line.** A `/` typed as
the first character of the box opens it, and so does a `/` typed after a space or a
newline — so you can find a command half a sentence in without throwing the sentence
away. A `/` with anything other than a space in front of it opens nothing at all, which
is what keeps `cmd/aforge` and `https://example.com` quiet.

The list is not modal. You keep typing into the same box and the list narrows under it.
Only ↑ ↓ enter esc are taken from the editor; every other key types into your draft and
re-filters. A space ends the word the list is filtering on and closes it, because a line
with an argument is a line being written rather than a command being chosen.

Moving in it:

- ↑ / ctrl+p and ↓ / ctrl+n move.
- enter takes the row under the cursor. Whether that *runs* or completes a live tag
  depends on where the word sits — see "Use /standing or /task in the middle of a
  sentence".
- esc closes the list and seals that word: it does not come back on the next letter you
  type. Start another word and it opens again. The text you typed stays.

Eight rows show at once and the list scrolls under the cursor. At phone width fewer rows
show, each with its description on its own line. Rows highlight under the mouse pointer,
but a click does not run a row — this list has no mouse commit.

Filtering is a substring search over the command's name, ranked by where the match was
found, prefix first. An alias match ranks a whole rung below any name match, so typing
`res` puts `/resume` above the `/new` that answers to `reset`.

**Rows that take an argument are not run.** At the head of an otherwise empty box,
choosing `/model <slug>` or `/export <path>` writes `/model ` or `/export ` into the box
with the caret after it, and runs nothing.

Anything you press enter on goes into the ↑-history, commands included. Choosing a row
from the list records it as `/<name>`, exactly as if you had typed it.

While a panel is up — settings, the model picker, resume, connect, harness, permissions,
copy mode, rewind — typing `/` does nothing. Those states take the key first.

**Home's box has the same list.** Typing `/` on the home screen opens the same ranked
list over its box, and `↑` walks to a row and `enter` runs it, exactly as in a chat. A
slash line typed in full and entered from home's typing row is run too — `/settings`
opens the settings place rather than starting a conversation with the word in it.
Commands that act on a conversation act on the one home holds behind the screen.

## Why a file path does not pop up the command list

Typing `/Users/santosh/notes.md` or `/tmp/log` into the message box does not leave the
command list flickering over your sentence. Three rules keep it away, and they are the
same three wherever the slash is:

- **A slash needs a space in front of it.** Only the first character of the box, or a
  slash after a space or a newline, is a candidate. So the second slash of
  `/Users/santosh` is not one, and neither is the one in `cmd/aforge/main.go` or in
  `https://`.
- **A word that matches no command closes the list.** The candidate runs to the next
  space, so the word being matched is `Users/santosh`, and nothing in the table looks
  like it. In practice a path drops the list within a couple of keystrokes and it stays
  gone. Backspace back to a word that does match and it returns.
- **esc seals the word.** If it did open over something you meant literally, esc puts it
  away and it stays away for that word.

There is no setting for this and nothing to turn off. The list follows what you type; it
never holds itself open.

## Slash commands are drawn as chips

A command aforge recognizes is not drawn as ordinary text. `/task`, `/compact`, `/clear`
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
  brief through the task sizing road.

Both roads stop at something you can see and answer: standing raises its ratification
card, and task raises its sizing choice when there is a choice to make. A pasted tag does
not silently do work. With no other words, each tag behaves like that command's existing
bare form. With two live tags aforge sends nothing, leaves the draft in the box, and says
`one tag per send — backspace one to make it plain words`.

Other commands remain ordinary prose away from the start. `later I will run /compact on
this` is sent literally, and `/compact` is plain rather than chipped.

## Backspace after a slash tag makes it plain words

With the caret immediately after a live `/standing`, `/orders`, or `/task` tag, the first
backspace removes its chip but deletes no letter. The word is now plain prose and Enter
sends it to the conversation normally. A second backspace edits the word as usual.

Editing the demoted word makes aforge recognize its current spelling afresh. Edits before
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
equal a canonical name or another alias; that is checked when aforge starts.

A word that is in no list is passed through as typed and gets this answer:

```
unknown command: /<word> · try /help
```

It says the word back as you wrote it, not what it resolved to.

If nothing in the list matches what you typed and you press enter, the list closes and
the line is submitted as an ordinary message. So `/nonsense` still gets an answer.

No command is ever hidden from the list or from `/help`. The table is the one place a
command is written down, and both renderings draw all of it. What varies between states
is what a command *answers*, not whether you can see it.

## Every command aforge has

Canonical word, the other words it answers to, its argument form, and what it does.

| Command | Aliases | Argument | Effect |
|---|---|---|---|
| `/model` | — | — | opens the model picker |
| `/model` | — | `<slug>` | switches the model to that slug |
| `/image` | — | `<path>` | attaches a picture; tab completes the path |
| `/settings` | `/set`, `/config` | — | opens the fullscreen settings panel (also ctrl+,) |
| `/connect` | `/connections` | — | opens the connected-accounts panel |
| `/new` | `/clear`, `/clean`, `/reset` | — | closes this session and starts a fresh one |
| `/resume` | `/sessions` | — | opens the earlier-conversations picker |
| `/compact` | — | — | summarizes the conversation now |
| `/home` | — | — | every project and conversation on this machine, fullscreen |
| `/folder` | `/place`, `/dir` | — | opens the folder picker — say which folder this conversation is also about |
| `/folder` | `/place`, `/dir` | `<path>` | …opens it with that already in the box: a word filters, a path browses |
| `/attach` | `/upload` | `<path>` | a file goes on the tray; **a folder** is referred instead, and says `folder · <path>` |
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
| `/memory` | — | — | opens the memory panel |
| `/memory` | `/memories` | `<query>` | prints matching memories into the conversation |
| `/memories` | — | — | prints every memory into the conversation |
| `/remember` | — | `<text>` | keeps one thing across conversations |
| `/forget` | — | `<query>` | forgets the best matching memory |
| `/crew` | — | — | opens the five-seat reading: the model you talk to, then the three crew presets |
| `/crew` | — | `<preset>` | sets the crew to `frugal`, `balanced` or `max` |
| `/task` | — | — | opens the full-screen task page — the same page as `/history` and ctrl+. |
| `/task` | — | `<brief>` | sizes the work, then starts one worker that can split itself if it is wide; shapes the brief |
| `/task` | — | `solo <brief>` | starts one worker immediately, without sizing |
| `/history` | — | — | opens the full-screen tasks place — every task this machine has run, filterable (also ctrl+.) |
| `/status` | `/info`, `/context` | — | prints every fact the status line knows, one per line |
| `/cost` | `/usage`, `/tokens`, `/spend` | — | prints what this conversation has spent, and on what |
| `/budget` | `/limits` | — | what aforge may spend · every limit on one tab |
| `/budget` | `/limits` | `<amount>` | sets the day's limit · `none` removes it |
| `/budget` | `/limits` | `<row> <amount>` | sets one by name: `day`, `conversation`, `plan`, `practice` |
| `/cache` | — | — | how big the shared build cache is, and where |
| `/cache` | — | `clean` | asks first, then deletes the cache to free disk — confirm with `/cache clean now` |
| `/copy` | — | — | enters copy mode (also ctrl+b) |
| `/select` | — | — | hands the pointer back to the terminal (also ctrl+s) |
| `/export` | `/save` | — | writes the whole conversation to a file |
| `/export` | `/save` | `<path>` | …and writes it there; tab completes the path |
| `/files` | — | — | lists what has been made for you; opens, reveals or copies one — over `--host` it opens the browse page for that machine |
| `/files` | — | `<path>` | over `--host`, brings that one file back and opens it here |
| `/help` | `/?` | — | prints this list |
| `/quit` | `/exit`, `/q` | — | leaves |

## /help, /?, /quit, /exit, /q — how do I close just this chat

`/help` (or `/?`) prints the whole command table into the conversation, name column
aligned, each row with its alias tail. The first line is the product's own name,
`openaf` — the one place on this surface it names itself.

Under the table `/help` prints the keys that have no slash command, including
`ctrl+c`, `ctrl+o`, `ctrl+q`, `ctrl+e`, `ctrl+t`, `ctrl+l`, `ctrl+w`, `ctrl+,`, `@path`,
`alt+enter`, and `d` inside `/permissions`. The keys page covers those in full. The
`ctrl+c` line reads `ctrl+c         twice quits · mid-turn one press interrupts, like esc`.

The last line of `/help` is `session · <path>`, and it appears **only when the session
has a file**. Over `--host` the path is written `machine:/path`.

`/quit` (or `/exit`, `/q`) **closes the conversation in front**, and it does it at once —
it is typed out on purpose, so it is not asked twice. Your draft is written to disk
synchronously first, with any message still waiting for an answer folded in underneath it,
so nothing typed in the last moment is lost. Then that conversation's running turn is
interrupted and its agent is closed for real.

**If this terminal is holding another conversation, aforge stays up** and the most recently
open one comes forward, saying `closed · <the name of the one that went>`. `/quit` leaves
the program only when the conversation it closed was the last one. Home's page has the
whole arrangement under *Switch between projects without leaving*.

`ctrl+c` is the other road out and it takes **two presses**, and it closes **everything**:
the first arms the door and the hint slot reads `ctrl+c again to quit`, with how many
conversations and what work a second press would stop — `ctrl+c again to quit ·
3 conversations · 2 tasks and a job will stop`. A second press within 1.5 seconds leaves.
While a turn is running `ctrl+c` interrupts the turn instead and does not arm anything. The
keys page has the whole rule under "Quitting aforge".

## /new — start another conversation in this project

`/new` (or `/clear`, `/clean`, `/reset`) opens a fresh conversation on the same config,
with a fresh agent and a fresh session file.

**It adds one rather than closing this one.** The conversation you were in is left open
behind it — still streaming its turn, still running its tasks — and `tab` over an empty
message box goes back. The status line then reads `2 open`.

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

**How many conversations are already open is never a reason to refuse.** There is no cap:
the ninth and the fiftieth `/new` open exactly like the first, and the one you were in is
left running. Nothing closes one for you — that is what `/quit` is for.

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

## /select — drag to select with your mouse

You usually do not need this any more: **dragging over the conversation already
selects and copies, to the character** — sweep with the left button down from one
character to another, the cells between them highlight, and on release their text is
on your clipboard, with `copied · 14 chars` or `copied · 3 lines` on the status line.
Double-click takes a word, triple-click a line (see the keys page's *Selecting text
with your mouse*).

`/select` is for when you want your **terminal's own** selection instead: it hands the
pointer back so a drag selects text natively. It is the same thing ctrl+s does.

It toggles. Pressing it again takes the pointer back, and so does the next ordinary
keystroke.

When aforge never took the mouse in the first place — the `mouse` setting is off — it
answers out loud, exactly:

```
your terminal already has the pointer — drag to select.
```

## /image — attach a picture

`/image <path>` attaches a picture to your next message. Use it for a picture that is not
under this directory, or one the `@` completion walk does not reach.

Path rules: `~` is your home directory, a bare name is under the directory this
conversation is about, and an absolute path is left alone. Over `--host` the path is
anchored to **this** machine — the picture is on the laptop you are sitting at, and its
bytes travel with the message.

A full attachment tray does not stop the command: `/image` adds a second picture rather
than sending the first.

The picture lands on the tray as `▣ #1 name.png` and **its `[image #1]` token is appended
to your sentence when you press `enter`**, so you can refer to it by number the same way
you would one you dragged in. Dragging or pasting a file over a line that already starts
with `/` leaves the path as text, so `/image ` still takes the path you dropped on it.

Refusals, exactly as written:

```
/image takes a path · try /image shot.png
<name> is not a picture · png, jpeg, webp and gif are
no such picture: <what you typed>
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
typed yourself; aforge will not create directories under the workspace on its own.

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

The document opens with `# <session name>`. Under it, `## you` and `## openaf` headings
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
writing. A directory aforge creates for a path you typed is made 0700.

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
content, under `~/.aforge/v3/remote/`, and a copy under the file's own name is what your
viewer is handed — so the window title says `report.pdf` and not a row of hex. A file the
model wrote during the turn is usually already here before you ask, so it opens at once:
aforge quietly fetches a file of 2MB or less as it sees it being written, and says nothing
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
reasoning level is set), `crew`, `task model` (only in a room), `served`, then the telemetry
words — `search`, `background`, `changes`, `spend`, `context`, `cache`, `rate`, `compaction`,
`approvals`, `state` — then `tasks`, `place`, `keys`, and last `file`. Labels are padded
into two aligned columns.

The `crew` line sits directly under `model` and reads the preset word — or `custom` — and
the three classes:

```
crew     max · brain kimi-k3:high · hands deepseek-v4-pro · checks kimi-k3
```

On the live status line the same fact is one short segment — `crew max`, or
`crew custom` — at the head of the telemetry, beside the model on the left, and it is among
the first segments a narrow row gives up. The `crew` line here and on the phone's status
sheet is the full reading; there is no `crew` line at all when the session was opened
without a profile directory.

`/status` differs from the on-screen status sheet in two deliberate ways:

- The session **file** is added. A path is a thing you copy into another program.
- The `spend` line is **dropped** when nothing has been spent. The live status line keeps
  showing `$0.00`; a note in the transcript must not.

Over `--host` the `place` and `file` values are written in full as `machine:/path`.

## /cost — what this conversation has spent

`/cost` (or `/usage`, `/tokens`, `/spend`) prints what this conversation has spent, and
on what, into the conversation.

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

## /budget — setting a limit from the message box

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
edited. There is deliberately **no `/budget task`**: a task has no dollar limit of its
own, so a command that accepted one would be writing a number nothing reads.

A write says back what it landed, in the tab's own words for that row — `per day · $50`,
or `per day · no limit`. A figure it cannot read is refused in the row's own words with
the rows listed after it: `that's not a dollar amount — a number, or none for no limit ·
rows: day, conversation, plan, practice`. A row this machine does not have answers `that
limit is not on this machine`.

The `$` is optional, and the amount can be a word — see the next section.

## /cache — the build cache, disk space, and why aforge is using so much disk

`/cache` prints one line: how big the shared build cache is and where it lives —
`~/.aforge/cache`. That directory holds the toolchain caches task workers fill as they
build — go modules and build outputs, npm, pip, cargo — shared across sessions so the same
module is downloaded once instead of per task. It can quietly grow to hundreds of
megabytes; that growth is this cache, not your conversations.

With nothing in it, `/cache` answers `the cache is empty · ~/.aforge/cache`.

**The cache is not your conversations.** Conversation history, tasks, settings and
credentials live elsewhere under `~/.aforge` and no cache command can reach them.

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

From the terminal the same pair is `aforge cache` and `aforge cache clean` — the latter
prints the same size-and-path warning and asks you to type the word `clean` before it
deletes anything; `aforge cache clean --yes` skips the question for scripts.

**What `/cache clean` will not do:** it does not delete conversations, reset the
dashboard, or forget memories. To start a fresh conversation the command is `/new`;
memories are dropped with `/forget`.

## /model — pick a model

`/model` with nothing after it opens the model picker: a filter box in the input line's
place with a short list of models under it. It is bottom-anchored, so the conversation
shrinks above it and nothing pops up over what you were reading. Pressing the model's
name in the status line opens the same picker.

`/model <slug>` switches straight to that slug: no list, no confirmation, and no check
that the slug exists in any list. If the slug is in no known list, the context window is
left alone.

A switch carries the conversation's words and nothing of the previous model's private
thinking. A model's reasoning is its own — the router encrypts it and refuses to replay it
to any other model — so the new model reads what was said and continues from there. If a
switch mid-conversation is ever refused with "encrypted reasoning … produced under a
different model", the request is repaired and sent again on its own; nothing is lost.

Moving in the picker: type to filter; ↑ / ctrl+p and ↓ / ctrl+n move; pgup/pgdown move
12; left, right, home, end, ctrl+u and ctrl+w edit the filter. **ctrl+t** walks the model
under the cursor through off → low → medium → high → off reasoning effort. enter
switches.

esc leaves and changes **nothing** — your half-typed draft, the model in use and the
frame all come back as they were. The filter is forgotten when the picker closes.

The cursor opens on the model in use, which is also the marked row, so enter with nothing
typed confirms rather than changes.

The placeholder in the empty filter box is the only place the picker explains itself:

```
filter · ↑↓ · → lanes · ctrl+t effort · enter · esc
```

Choosing a model sets it on the agent, teaches the surface its context window and tells
the session — compaction fires at a fraction of that window, so this is not decoration —
notes `model · <model>`, and writes the choice into your profile, so the next `aforge`
opens on it. Over `--host` the switch takes for the session and is not written down: the
model a remote session opens on is that machine's to resolve.

## What the model picker lists, and what it will not do

The picker **never fetches**. The list is what is already known, tried in this order,
each rung used only when the one above it came back empty after filtering:

1. the catalog handed in at launch,
2. `~/.aforge/v3/models.json`,
3. five names this build remembers: `deepseek/deepseek-v4-flash`, `openai/gpt-4.1-mini`,
   `anthropic/claude-sonnet-4.5`, `google/gemini-2.5-flash`, `moonshotai/kimi-k3`.

Filtering splits your text on whitespace and every token must match, each in one of three
tiers: prefix, then substring, then subsequence. So `ds v4` finds
`deepseek/deepseek-v4-flash` and `claude 4.5` finds `anthropic/claude-sonnet-4.5`, with
fuzzy hits sitting at the bottom rather than mixed through.

Twelve rows show at a time. A row reads `<id>:<level>` on the left and, dimly on the
right, what the catalog published: window, price per million prompt and completion, arena
elo, and what the model can do besides write — `sees`, `hears`, `watches`, `draws`,
`speaks`, `films`. Each part is hidden when nobody published it. A price shows only when
both halves are known — a zero means "nobody said", never "free". A plain text chat model
shows no capability words at all.

Only models you can hold a conversation with are listed: text in and text out. A model
that publishes `["image","text"]` out — a drawing model that also captions — is left out,
and so is a transcription model. A model that publishes nothing is judged by its id.

Limits:

- `/model <slug>` refuses a slug the catalog carries that **cannot hold a conversation**,
  in one line — `openai/gpt-4o-mini-tts cannot hold a conversation — it speaks. Still on
  <model>.` — and does not switch. A slug the catalog has never carried is taken as typed.

- **ctrl+t does nothing at all, silently**, on a model whose catalog row does not accept a
  reasoning knob. The level would be an error at the next turn. A row that published
  nothing counts as "does not accept".
- The reasoning level lives on the agent, per model id, so it survives switching away and
  back. `/new` forgets it.
- There is no mouse commit on the picker's rows.

## /resume — open an earlier conversation

`/resume` (or `/sessions`) opens the picker of earlier conversations. It is the same
surface `aforge resume` opens on. The list is resolved at that keystroke, so a
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
No sessions yet — start one with aforge chat
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

**It is also what a bare `aforge` opens on.** The conversation the launch picked is loaded
underneath, and `esc` — or `enter` on the row the cursor starts on, which is that same
conversation — drops into it. Home stays out of the way when you named a conversation
(`--session`, `aforge resume`), on a `--once` or `--host` run, and on a machine whose only
conversation is the one already open. There is no welcome box when home greets you. Not
greeting you is not the same as being out of reach: `/home`, or `space` twice on an empty
box, opens it on a one-conversation machine and on an empty one alike — over `--host` it
refuses.

There is no argument form. There are three other ways in: **`alt+1`**, home being the first
of seven places; **`space` twice** on an empty box; and **`tab`** from any other place. Projects are dim
headings, one line per conversation under each: a glyph (`?` waiting on you, `◐` running,
`◌` left unfinished, `○` at rest), the name, what it has going on, and how long since you
spoke in it. A conversation stopped on a question sorts to the top of its project and the
right half shows the line it is stopped on. Quiet
conversations past the first four per project collapse to `…3 more, quiet since 2d`. The
right half shows whatever the cursor is on — its tasks, what it spent, the last thing said.

`↑`/`↓` walk, `enter` opens, `esc` closes back into the conversation you came from.
**Typing does two things at once**: what you type is a new conversation waiting to be sent
AND a live search over every project on the machine. The top row — `start a new
conversation: "…"` — holds the cursor, so type-and-enter still starts a chat; one `↓` steps
onto the matches and `enter` opens one instead. **A line that starts with `/` is the third
thing typing can be**: it is a command, not a conversation — the command list opens over
the box while it is typed, and enter runs it (see *Typing a slash to see the command list*).
The foot reads exactly
`type to search or start something new · ↑↓ pick · enter open`.

Search matches conversation names, project names, task titles and **what tasks came to** —
the one-sentence outcome — so `postgres` finds the chat whose work mentioned it. A project
folds its quiet conversations into `…13 more, quiet since 1d`; that line is a door (`enter`
or `→` opens it, `←` folds it), and searching sees through the fold.

**`enter` opens any row on the screen, in any project.** The conversation you were in is
left **open** behind it — still streaming, still running its tasks — and the new one is
built on its own workspace with that project's own permissions, crew and spend ceiling.
Nothing is carried across, because a second project is a second conversation rather than
this one moving. `tab` over an empty message box goes back. Home's page has the whole of
it under *Open another project from home*.

Refusals, exactly as written:

```
nothing here yet — say something and this fills up
no conversation matches
/new is unavailable here
that folder is gone · <path>
```

The first is not a refusal: it is what an empty home says where its rows will be, with the
box and the keys at the foot still live — typing there offers
`start a new conversation: "…"` as it does anywhere. It is said over `--host` too, where the
rows are the **far** machine's and that machine may simply not have been used yet. `/new is
unavailable here` is what the typing-to-start box says where no fresh-session seam exists.
The last is `enter` on a project whose folder has been deleted or moved since its last
conversation: home stays up and nothing is opened. **How many conversations this terminal
already holds is never a refusal** — there is no cap on that.

`that folder is gone` is never said over `--host`: the folders are the far machine's and this
one cannot stat them, so nothing is claimed either way (the Places page has the whole of it).

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
The badge on the chat is the far session's actual approval posture, but `/permissions` has
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

The first is what you get with no registry wired — a headless frame, and **every `--host`
session**, because the registry lives on the far machine. The second is drawn as the
panel's only row, and it is also what a registry that cannot be read at all shows, rather
than an error.

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

They go through the same deliberate door `ctrl+enter` opens: aforge is told to shape the
sentence into a standing order's card — when it wakes, what it does, how far it reaches —
and it never carries the sentence out as one-off work as well. Nothing stands until you
answer the card. A sentence that cannot stand at all gets one short line saying so and
nothing else. Typed while an answer is still arriving it waits above the box and goes
through the marked door when its turn comes. On a build with no ambient side it says
`nothing here can hold a standing order` and sends nothing.

**Bare, it opens a page.** A short list under the message box of what stands over this
conversation, on up to three shelves, with `p` to pause one, `s` to stop one, `n` to except
this place and `enter` to open the conversation that asked for it. With nothing standing it
opens all the same, on three dim lines beginning
`nothing stands here yet — say what should always be true, and I'll hold it.`
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
pass through the conversation model. aforge briefly shows a forming block while a small
judge reads the words for width, and then **one worker starts, whatever the answer was —
nothing is asked of you**. If the work is meaningfully parallel, one dim line says so
(`the work looks wide · one worker starts, and it can split as it goes`) and that worker
may hand the parts out once it has opened the material. Otherwise the task starts silently.

`/task` is also a live tag in the middle or at the end: `investigate the flaky test /task`
strips the tag and takes the remaining words through this same sizing road. Backspace
immediately after the tag makes it plain prose.

**A bare `/task` opens the full-screen task page** — the same page `/history` and `ctrl+.`
open, holding every task this project has ever run. It does *not* print a usage line, and
it starts nothing. On a project that has never run one it opens the page anyway, and the
page says what tasks are and ends `no tasks yet — /task <brief> starts one`. The `+ /task`
row at the foot of the task column types `/task ` into your box, which is why the word on
its own has an answer worth giving.

Then, whichever shape it takes, `shaping the brief…` appears while a model turns your words
into the fuller brief the worker is given — your sentence kept word for word, with the
constraints and the done-condition written around it, **and the name the roster will call
the work**. The forming block carries a spinning mark and a climbing clock while it runs,
so you can see the wait is alive rather than stuck. It collapses into the ordinary
started-task row when the work starts, or into the honest error line if it cannot start.
The *work that runs on its own* page has this in full, under
*Why my task's brief is longer than what I typed* and *Why my task is called something I did
not type*.

`/task solo <brief>` starts one worker immediately. It skips sizing altogether and still
shapes the brief.

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
and does not size the work at all. `solo` typed on the command line overrides the row
either way. Two of the row's old answers are gone — `ask` with the two-choice list it
opened, and `adaptive` with the planner road itself — and a profile still set to either
reads as `sized`.

## What happens when I type /task

While `/task <brief>` is being sized and shaped, one dim block appears at the transcript
tail. Every row has the same thin left line and one space of padding:

```text
▏ task
▏ "fix the flaky auth test and add coverage for the retry path"
▏ ⠙ sizing it up… · 3s
```

The quoted line is your brief verbatim; a long brief is fitted to at most about two rows.
The last row advances in place from `sizing it up…` to `shaping the brief…`. Explicit
`/task solo <brief>` and a `single` starting setting begin at shaping because they skip
sizing. Once shaping starts writing, a fourth dim row appears under the phase row with the
newest words of the brief on it — see *Can I see the brief while it is being written*. When
work starts, the thin line and scaffold disappear in the same frame and the normal
started-task row takes their place. If starting fails, only the error sentence remains.

## Why is there a line next to my task

The thin `▏ ` at the transcript tail joins `task`, your quoted words, and the live phase
into one thing being formed. It is a single left hairline, not a box or a task-status
border. It exists only while a `/task` command is in flight and disappears when that
command becomes the ordinary started-task row or an error line.

## Can I see the brief while it is being written — the preview line under shaping the brief

Yes. While `shaping the brief…` is up, one extra dim row hangs under it carrying the newest
part of the brief as the model writes it:

```text
▏ task
▏ "write the release notes"
▏ ⠙ shaping the brief… · 13s
▏ ▸ the failure this kind of work has is a release note that lists comm
```

It is **one row, always**. It never grows into a second row and never pushes the
conversation up the screen — the words on it change, the height does not. It is the last
line the brief lays out to at your terminal's width, so it fills up left to right and then
starts again, and the end of it is where the model's pen is.

**While the model is still thinking, the row shows its thinking, in italics.** The shaper
runs on the careful-work model and is allowed to reason before it writes, and on some models
that is most of the wait — so the row shows whatever is actually being produced. Italic is
the model working; upright is your brief. **The brief takes the row the moment there is a
brief and never gives it back**, so nothing you have started reading is un-said.

**It appears only when there is something to show.** Before the model has produced anything
there is no fourth row at all, and a task you approved from a proposal card never grows one
— that brief was written before you were asked, so there is no stream behind the wait.

**`▸` on the row means there is more behind it.** Press the row, or press `→` with an empty
box, and the one line becomes the last six lines of the brief so far; `←` or another press
shuts it again and `▾` goes back to `▸`. No new key is involved — it is the same fold every
block on this surface has.

Nothing about the preview is kept. The reasoning in particular is never written anywhere,
never sent anywhere, and is not part of the brief the worker is given.

The preview is **telemetry about a wait, not a transcript**. When the brief lands, the
preview and the whole forming block disappear in the same frame, and what stays is the
ordinary started-task row. Nothing of the preview is kept, and the shaped brief itself is
readable in full in the task's own room.

## Several tasks forming at once — one block with a row each

Two `/task` commands can be shaping at the same time. They share **one** block rather than
stacking two four-row blocks at the tail of the transcript:

```text
▏ tasks · 3 forming
▏ ⠙ write the release notes · 13s
▏ ⠙ fix the nil-map crash in the loader · 9s
▏ ▸ Reproduce the crash from the stack trace in issue #94, then write a
▏ ⠙ write the docs for /task · 2s
```

The head counts them. Each task is one compact row: the same spinning mark, its name or the
opening of what you typed, and its own clock. **Only the row you are pointed at shows a
preview under it**, so the block stays the same height however long the briefs get.

`↑` and `↓` with an empty box move between the rows — the keys that walk rows in the
transcript already — and the preview follows. `→` opens that row's window, `←` shuts it.
Clicking a row you are not on points at it and opens it; clicking the row you are on shuts
it again. Walking off either end of the block hands the arrows back to whatever they do
next, so nothing else you press changes meaning.

**With one task forming, none of this appears.** No head that counts, no rows to walk: the
block is `task`, your words, the phase row and the preview, exactly as above.

## /history — the task history command: past tasks, every task this project has run

`/history`, or `ctrl+.`, opens a full-screen page holding the project's whole task record:
this conversation's work **and every earlier conversation's**. It is the one place that
answers "what did we do about this last week" — the roster's column beside the conversation
is built from this session's own work, and carries only a short dulled note of the rest.

**It is not `/tasks`, and there is no `/tasks` command.** `/task <brief>` and its `solo` form
mean *give aforge work*; this page starts none, so it does not share their word. Typing
`/history` is the only slash form — but the PLACE this opens is called `tasks` on the tab
bar, and **`alt+2`** and `tab` reach it without a command at all. The word is a place, not a
command.

Two sections. `running` is the tree of everything still going, drawn whole, with each task's
current call, clock, tokens and spend under its name. `earlier` is a flat list, newest
first, of everything the project has finished — one line each, the same rows the `@` list
offers.

**Type to filter.** Any printable key, spaces included, narrows both sections at once
against the titles, the ids and the outcomes; the bottom of the page shows what was typed
as `filter · port`, and a section with no match disappears entirely. `backspace`, `ctrl+w`
and `ctrl+u` edit it. `esc` clears the filter first and closes the page on the second press.

`esc` closes it. `↑`/`↓` move, `enter` opens the row: a task this session is holding opens
its room, and a task another conversation ran goes into your message box as
`@its-name` — that conversation is closed, so there is no room to open, and the mention is
what carries its outcome and its transcript to the model when you send.

Its `running` section also carries **a row for each task every other aforge window open on
this directory has out right now**, marked `another window` on the right. Those rows take
no cursor and `enter` does nothing on them: there is no room here and nothing has landed for
a mention to point at. They are how you find out that the directory is busy somewhere else.

On a project that has never run a task the page opens on its own teaching prose, ending
`no tasks yet — /task <brief> starts one`. The tasks pages describe the page in full.

## /crew — the four models aforge uses on its own behalf, read beside the one you talk to

aforge runs **five model seats**. Seat one is the model you talk to, and `/model` is what
moves it. The other four — reflex, small work, careful work, mastermind — are the models
aforge uses on its own behalf, for the calls you did not type. `/crew` reads all five and
sets the four in one word. **It never moves seat one.**

```
/crew
```

opens the five-seat reading, bottom-anchored like the model picker. From the top:

```
the four models aforge uses on its own behalf — not the one you chat with
  you talk to · deepseek-v4-flash
  frugal — qwen handles careful work · pennies a day
    reflex       mistralai/mistral-nemo · small work   deepseek/deepseek-v4-flash · careful work qwen/qwen3.8-27b · mastermind   qwen/qwen3.8-27b
› balanced — kimi-k3 thinks, qwen checks
    reflex       mistralai/mistral-nemo · small work   deepseek/deepseek-v4-flash · careful work qwen/qwen3.8-27b · mastermind   moonshotai/kimi-k3:low
  max — kimi-k3 everywhere, thinks longer
    reflex       mistralai/mistral-nemo · small work   deepseek/deepseek-v4-pro · careful work moonshotai/kimi-k3 · mastermind   moonshotai/kimi-k3:high
each of the four can be pinned on its own in /settings → Providers
```

The first line says what the presets change and what they do not. The second is **seat
one** — `you talk to · <model>`, spelled as the status line spells it — with no marker and
no highlight, because nothing in this chooser can move it. Then the three presets: the one
in force wears a highlighted ground, `›` is where **enter** is aimed and it opens on yours,
↑ / ctrl+p and ↓ / ctrl+n move, and **esc** closes without changing anything. The last
line points at the settings row where one seat can be pinned by itself; the chooser does not
pick seats one at a time.

If you have pinned one of the four yourself, no preset wears the ground and the chooser says
`yours is none of the three — picking one puts all four back` above the closing line.

A word that is not one of the three changes nothing and prints the three:
`/crew cheap` answers `/crew cheap · not one of the three` and then the listing.

What each of the four classes funds, and how to set one of them on its own, is on the models
page.

## /crew <preset> — the confirm line, and the model it leaves alone

`/crew frugal`, `/crew balanced` or `/crew max` sets the four and confirms in one line:

```
crew → max · brain kimi-k3:high · hands deepseek-v4-pro · checks kimi-k3 · you are still talking to deepseek-v4-flash — /model changes that
```

**The model ids are drawn brighter than the words around them.** `crew →`, the preset
word, `brain`/`hands`/`checks` and `you are still talking to` stay at the grey every note
is written in; the three crew ids and the model you are talking to step up, because they
are what the command was typed to find out. `/model` wears the command chip, because it is
a command you can type. See "Why is one word in a line brighter than the rest" on the
screen page.

The last clause names, by id, the one seat the command did not touch: the model you are
talking to, in the same spelling the status line's model segment uses, so you can check it
against the foot of the frame. `/crew` never changes that model and never offers to; only
`/model` does. When the session has no model yet the clause reads
`the model you talk to is untouched — /model changes that`.

**The change is live.** The next call aforge makes on its own uses the new crew — no
relaunch, and no waiting for the next session. To read the crew back afterwards: the live
status line says `crew max` beside the model, `/status` prints the `crew` line under
`model`, `/settings` → Providers has the crew row, and bare `/crew` opens on yours.

**That promise is local-session only.** Over `--host`, `/crew` reads and writes this
machine's profile; the session resolves its crew from the other machine. There is no crew
write across the connection and this build prints no host-specific warning, so `/crew max`
does not change the four models the far session uses. Change that machine's profile there.

## /connect — your connected accounts

`/connect` (or `/connections`) opens the connected-accounts panel, where you pick a
service from a list and connect it. There is no argument form. The accounts page covers
what each account can do once it is connected.

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

The first is the `--host` answer. The second means no connections seam is wired. The
third means the catalog came back empty.

The settings panel has a `Connections` tab over the same accounts. It is a different
surface from `/connect`, not a second copy of it.

## Which machine's settings are these — /settings, /set, /config over --host

`/settings` (or `/set`, `/config`, or ctrl+,) opens a fullscreen page: a tab bar over the
aforge settings, plus a tab of connected accounts. It was the first of the three fullscreen
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
  tab, so backing out leaves you where the thing lives. backspace, ctrl+w and ctrl+u edit
  the query. The search matches the label, the settings key, the one-line description and
  the registry's own label — so `spendRail` finds a row as well as "ceiling" does.
- enter and space open or change the row under the cursor.
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

So does a change you ask aforge for. There are **two doors onto one file**: the panel,
and the `change_setting` tool aforge reaches when you say "set my daily budget to 5" or
"use a different model for planning". Both go through the same registry, take the same
validation, refuse in the same words, and land in the same `config.json` — so a row you
change by asking is a row you find changed in the panel, and the reverse. `settings` is
the read beside it, listing every row by the key `change_setting` names. Which rows
aforge refuses to change for you, and why, is on the permissions page.

The project layer, `<workspace>/.aforge-v3/config.json`, is deliberately not writable from
the panel — or from `change_setting`, which writes your profile only. The foot line says
so:

```
saved to your profile · a project's own .aforge-v3/config.json is a hand edit
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

## The nine settings tabs

The tabs, in order:

```
Session · Context · Workspace · Display · Spending · Safety · Tasks · Providers · Connections
```

**Session** — the rows this conversation carries that belong nowhere else: "memory",
"fallback models", and the four ssh rows a `--host` conversation rides on ("ssh reuse",
"ssh heartbeat", "ssh missed heartbeats", "ssh traffic").

The four models aforge uses on your behalf are **not** here — they are on Providers, with
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

**Workspace** — this machine and this project: what aforge does with its own time here, and
what it may reach on your behalf. Rows: "quiet before practice", "arrival brief after",
"tenure after", "background checks", "attribution", "google sign-in id", "google sign-in
secret", "slack sign-in id". **It holds no money row at all** — every one of those moved to
Spending.

**"background checks"** is on by default: one small timer under your own login checks
your reminders, watches and routines every 5 minutes with no window open. Off removes it
and nothing standing is lost — see *Keeping an eye on things* for the whole of it.

**Spending** — money, and nothing that is not money. Seven lines: the reading `today`, then
"per day", "per conversation", "per plan", the readings "per task" and "per standing run",
and "practice". `/budget` and `/limits` open it. *Models, context, and what it costs* has
every row and every door onto them.

**Safety** — what aforge may do without asking you first. Rows: "ask before running",
"tool exceptions", "shell command rules", "guardian", "approval countdown", "background
after", "task countdown", "who settles work that needs a look".

**Tasks** — how work you can walk away from is run. Rows: "starting a task", "check task
work", "task repair rounds", "tasks at once", "busy machine", "memory floor", "task
model".

**Display** — how the surface draws itself and what it remembers of your typing. Rows:
"input history", "keep drafts", "task column", "hints" — the one-line tips above the
message box, and the what's-new lines with them (see *Hints and tips*) — "chat width",
"mouse", "timestamps", "turn work". There is no "nerd font" or "linear mode" row: icons
need no patched font anywhere on this surface, and the accessible single-column rendering
is the `--linear` flag at launch rather than a persisted setting.

**Providers** — which model answers what. It leads with the **Models section**, in this
order:

1. **your model** — the model you are talking to. It is the conversation slot, and picking
   here is the same road `/model` takes — the same picker, lanes and all. Its value carries
   the machine serving it: `deepseek/deepseek-v4-flash · auto (cloudflare now)`.
2. **lane** — which endpoint behind that model answers you. enter opens the machines with
   what has been measured of each, and enter on one pins it.
3. **speed guard** — whether an answer slow to start is asked of the next-best machine as
   well.
4. **routing** — what every request prefers among the endpoints: `latency`, `price`, `off`.
   With `off` nothing is measured, so the two rows above it have no machine to name.
5. **crew** — the four below, chosen as one word: `frugal`, `balanced`, `max`. It is a cycle
   row: enter or space walks it. Answer any of the four yourself and it reads `custom`.
6. **reflex** — `near-free · reads every turn — memory, titles, safety`
7. **small work** — `cheap · does the bulk work — run nodes, digests`
8. **careful work** — `careful · checks what must not be wrong — audits, compaction, vision`
9. **mastermind** — `thinks · plans runs and designs harnesses — add :low, :medium or :high`
10. **pinned roles**, and hanging off it the **roles** list — one row per auxiliary call
    aforge makes for itself, grouped under its class. Those rows come from the running binary
    rather than the settings registry.

The four machine rows lead because the endpoint serving your model is part of the same
decision as the model, and they used to sit at the foot of the tab, forty rows below it.

Then the rest of the tab: "looking", "reading", "reply guard" — whether a reply
that has come apart is cut and asked again, on by default (see *Models, context, and what
it costs*) — and one row per capability slot added automatically from the settings
registry: drawing, speaking, composing, filming, voice.

The first three of the four classes are **select** rows and open the model picker. The
**mastermind** row is a **text** box instead, because its value may carry a thinking level
(`moonshotai/kimi-k3:high`) and a picker hands back a bare id.

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

**select** — enter opens the model picker itself, the same component `/model` opens, in
the list's place. It is filtered to the question that row asks: "looking" only offers
models that can see, "drawing" only ones that draw, "speaking" only ones that speak,
"composing" only ones that compose, "filming" only ones that film, "voice" only ones that
hear, and everything else follows the general chat rule. Its legend is
`↑↓ move · enter choose · esc cancel · type to filter`.

Because the picker is the same component, everything true of `/model`'s ranking, its rows
and its ctrl+t effort knob is true here too — **including the lanes** on the row that has
them. On **your model**, `→` or `tab` unfolds the endpoints serving the model under the
cursor and `enter` on one pins it, exactly as under `/model`, and the legend says
`↑↓ move · → or tab lanes · enter choose · esc cancel · type to filter`. The media slots
have no lane row behind them, so nothing unfolds there and the legend does not offer the
key.

While any of those layers is up — the value box, the model picker, the key box on the
Connections tab — the foot drops `tab next place`, because the layer has taken that key.

## The roles rows in settings — pinning a role, and del to unpin

The **roles** list sits on the **Providers** tab, directly under "pinned roles". Each row is
one call aforge makes outside a turn — `title`, `compaction`, `guardian`, `auditor`,
`planner`, `designer`, `worker`, `router`, `vision`, `reflex`, and `spellout`, which is the
one of them you ask for yourself with `ctrl+r` (see the keys page) — drawn as
`<role>    <model>`, with `pinned` after it when that role has a model of its own.

The rows are **grouped under their class**, in the same order the four class rows are drawn
above them: `roles · reflex`, `roles · small work`, `roles · careful work`,
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
it means aforge picks one for you.

Choosing is resolved at the moment something is actually drawn, spoken or looked at, down
one order:

1. the row you set here (an environment variable of the same name still wins over it —
   `AFORGE_IMAGE_MODEL`, `AFORGE_SPEECH_MODEL`, `AFORGE_MUSIC_MODEL`, `AFORGE_VIDEO_MODEL`,
   `AFORGE_VOICE_MODEL`, `AFORGE_VISION_MODEL`);
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
