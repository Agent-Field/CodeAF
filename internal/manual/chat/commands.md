# Commands

## Typing a slash to see the command list

Type `/` as the first character of the message box and the command list opens under
it. There is one list of commands in aforge: the pop-up you get by typing `/` and the
list `/help` prints are drawn from the same table.

The list is not modal. You keep typing into the same box and the list narrows under it.
Only ↑ ↓ enter esc are taken from the editor; every other key types into your draft and
re-filters. A space or a newline anywhere in the line closes the list again, because a
line with an argument is a line being written rather than a command being chosen.

Moving in it:

- ↑ / ctrl+p and ↓ / ctrl+n move.
- enter runs the row under the cursor.
- esc closes the list. The text you typed stays.

Eight rows show at once and the list scrolls under the cursor. At phone width fewer rows
show, each with its description on its own line. Rows highlight under the mouse pointer,
but a click does not run a row — this list has no mouse commit.

Filtering is a substring search over the command's name, ranked by where the match was
found, prefix first. An alias match ranks a whole rung below any name match, so typing
`res` puts `/resume` above the `/new` that answers to `reset`.

**Rows that take an argument are not run.** Choosing `/model <slug>` or `/export <path>`
writes `/model ` or `/export ` into the box with the caret after it, and runs nothing.

Anything you press enter on goes into the ↑-history, commands included. Choosing a row
from the list records it as `/<name>`, exactly as if you had typed it.

While a panel is up — settings, the model picker, resume, connect, harness, permissions,
copy mode, rewind — typing `/` does nothing. Those states take the key first.

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
| `/rewind` | `/undo`, `/back` | — | enters rewind mode (also esc esc) |
| `/permissions` | `/perms` | — | lists what runs without asking; `d` drops a line |
| `/harness` | `/harnesses` | — | lists the saved shapes of work and what they did |
| `/status` | `/info`, `/context` | — | prints every fact the status line knows, one per line |
| `/cost` | `/usage`, `/tokens`, `/spend` | — | prints what this conversation has spent, and on what |
| `/copy` | — | — | enters copy mode (also ctrl+b) |
| `/select` | — | — | hands the pointer back to the terminal (also ctrl+s) |
| `/export` | `/save` | — | writes the whole conversation to a file |
| `/export` | `/save` | `<path>` | …and writes it there; tab completes the path |
| `/files` | — | — | lists what has been made for you; opens, reveals or copies one |
| `/help` | `/?` | — | prints this list |
| `/quit` | `/exit`, `/q` | — | leaves |

## /help, /?, /quit, /exit, /q

`/help` (or `/?`) prints the whole command table into the conversation, name column
aligned, each row with its alias tail. The first line is the product's own name,
`openaf` — the one place on this surface it names itself.

Under the table `/help` prints the keys that have no slash command, including
`ctrl+o`, `ctrl+q`, `ctrl+e`, `ctrl+t`, `ctrl+l`, `ctrl+w`, `ctrl+,`, `@path`,
`alt+enter`, and `d` inside `/permissions`. The keys page covers those in full.

The last line of `/help` is `session · <path>`, and it appears **only when the session
has a file**. Over `--host` the path is written `machine:/path`.

`/quit` (or `/exit`, `/q`) leaves. Your draft is written to disk synchronously first, so
a sentence typed in the last moment before quitting is not lost. Then the running turn is
interrupted, the agent is closed, and the program exits. ctrl+c does the same thing when
the session is idle; while a turn is running ctrl+c interrupts the turn instead.

## /new — close this session and start a fresh one

`/new` (or `/clear`, `/clean`, `/reset`) closes this conversation and opens the next one
on the same config. A running turn is interrupted, the agent is closed, and a fresh agent
and session file are opened.

The transcript is cleared. So are selections, thinking, pending approvals, connect offers
and harness offers, the task rail, the frozen copy viewport, and any open browser sign-in
— they all belong to the old conversation. The title, model and meters are re-read from
the new agent.

**Your draft is deliberately not cleared.** The sentence in the box is your next one.

It ends with a note, `new session · <path>`, or just `new session` when there is no file.

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

## /rewind — take back a message

`/rewind` (or `/undo`, `/back`) enters rewind mode, where ↑↓ pick a point in the
conversation to cut back to. The keyboard gesture for the same thing is esc esc; the
command exists because a gesture nobody can see is a gesture nobody finds.

The cut opens at the last thing **you** said; everything older is one ↑ away. Your draft
is stashed and restored when you leave. The mode bar's legend reads
`↑↓ turns · ←→ steps · enter rewind · esc back`, and the cut line is labelled
`rewind here`. The sessions and rewind page covers what the cut actually does.

**Be warned: `/rewind` silently does nothing in five states.** No message, no mode bar,
nothing at all happens when:

- rewind mode is already on,
- copy mode is on,
- a task room is open,
- the settings panel is open,
- the task rail is full.

Each of those draws over the row the mode bar needs, so the command is dropped rather
than half-drawn. If `/rewind` seems to do nothing, one of those five is why.

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

`/select` hands the pointer back to the terminal so you can drag-select text with the
mouse. It is the same thing ctrl+s does.

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

## /export — write this conversation to a file

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

Over `--host` the list is the files made on **this** machine; what the session on the
other machine made is written down over there, and the command says so as it opens.

## /status — everything the status line knows

`/status` (or `/info`, `/context`) prints every fact the status line can carry, one per
line, **into the conversation**. It is a note, not a panel — you can scroll it and copy
from it later.

Usage totals are refreshed first, so a command typed between turns answers from what the
session holds right now.

The labels come in this order, and each is dropped when its value is empty: `session`,
`task` (only inside a task room), `model` (the full routing address, with `:level` when a
reasoning level is set), `task model` (only in a room), `served`, then the telemetry
words — `background`, `changes`, `spend`, `context`, `cache`, `rate`, `compaction`,
`approvals`, `state` — then `tasks`, `place`, `keys`, and last `file`. Labels are padded
into two aligned columns.

`/status` differs from the on-screen status sheet in two deliberate ways:

- The session **file** is added. A path is a thing you copy into another program.
- The `spend` line is **dropped** when nothing has been spent. The live status line keeps
  showing `$0.00`; a note in the transcript must not.

Over `--host` the `place` and `file` values are written in full as `machine:/path`.

## /cost — what this conversation has spent

`/cost` (or `/usage`, `/tokens`, `/spend`) prints what this conversation has spent, and
on what, into the conversation.

It draws up to five aligned lines:

- `spend` — only when it is above zero.
- `tokens` — like `48.1k in · 3.2k out`, or one half alone, or the combined figure.
- `cache` — like `31.2k read · saved $0.0180`. The money half appears only when a price
  pair was published.
- `model calls` — requests to the provider. Deliberately not called "turns".
- `time`.

Every line is dropped when its figure is absent. A provider that publishes no cache
accounting says nothing about caches, rather than teaching you that your cache never
hits.

`/cost` will not go silent. A session with no figures at all answers exactly:

```
nothing spent yet — this session has not sent a turn.
```

## /model — pick a model

`/model` with nothing after it opens the model picker: a filter box in the input line's
place with a short list of models under it. It is bottom-anchored, so the conversation
shrinks above it and nothing pops up over what you were reading. Pressing the model's
name in the status line opens the same picker.

`/model <slug>` switches straight to that slug: no list, no confirmation, and no check
that the slug exists in any list. If the slug is in no known list, the context window is
left alone.

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
filter · ↑↓ · ctrl+t effort · enter switch · esc cancel
```

Choosing a model sets it on the agent, teaches the surface its context window and tells
the session — compaction fires at a fraction of that window, so this is not decoration —
and notes `model · <model>`.

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
elo. Each part is hidden when nobody published it. A price shows only when both halves
are known — a zero means "nobody said", never "free".

Only models you can hold a conversation with are listed: text in and text out. A model
that publishes `["image","text"]` out — a drawing model that also captions — is left out,
and so is a transcription model. A model that publishes nothing is judged by its id.

Limits:

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

There is no argument form, on purpose: a session is named by a title a model wrote and
lives in a file named after a timestamp, so the only honest way to ask for one is to be
shown them. There is no mouse commit on this list either.

Refusals, exactly as written:

```
resuming is unavailable here
No sessions yet — start one with aforge chat
already here · <name>
resume failed: <error>
close failed: <error>
```

`already here` is enter on the row you are on; nothing is closed. A directory with no
conversations never opens the picker at all — a modal list with no rows would be a trap.

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

## /harness — the shapes of work you have saved

`/harness` (or `/harnesses`) lists your saved shapes of work, with what each one is for
and what its runs did. The registry is walked at that moment, so a harness registered in
another window is listed.

Moving in it: ↑ / ctrl+p, ↓ / ctrl+n, pgup / pgdown by 10. A click on a row acts on it; a
click anywhere else closes the panel. esc leaves.

A row reads `◆ name v3` on the left — the version is part of what the thing is — and
dimly on the right the description, then the history: `never run`, or
`6 runs · last ok, 2h ago`. A run count with an unreadable newest trace shows just the
count, because "never run" and "ran, and I cannot read the trace" are different facts.

enter **prints the harness's card into the conversation** and closes the panel: numbered
steps with the bounds under them, plus `last run` and that run's card when there is one.
It is prose, and prose belongs in the transcript where you can scroll and copy it.

Building a harness is a conversation, not a command. This list only says which ones
exist. While one is running, a chip with a spinner and its name leads the task strip.

Refusals, exactly as written:

```
harnesses are unavailable here
no harnesses are registered yet — build one in the conversation
```

The first is what you get with no registry wired — a headless frame, and **every `--host`
session**, because the registry lives on the far machine. The second is drawn as the
panel's only row, and it is also what a registry that cannot be read at all shows, rather
than an error.

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

## The settings panel — /settings, /set, /config

`/settings` (or `/set`, `/config`, or ctrl+,) opens the one fullscreen thing this surface
draws: a tab bar over the aforge settings, plus a tab of connected accounts.

Moving in it:

- ↑ / ctrl+p and ↓ / ctrl+n move a row at a time. Headings are stepped over, never landed
  on. pgup/pgdown move 16. home/end jump to the ends.
- ← / shift+tab and → / tab switch tabs, clamping at the ends rather than wrapping.
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

Every change you make in the settings panel goes through the settings registry and is
**saved to your global profile**.

The project layer, `<workspace>/.openaf/config.json`, is deliberately not writable from
the panel. The foot line says so:

```
saved to your profile · a project's own .openaf/config.json is a hand edit
```

So a value you set here follows you between projects, and a value a project sets for
itself has to be edited by hand in that file.

Over `--host`, opening the panel first notes:

```
these rows are this machine's — the ones that govern the conversation are read from the profile on the other one
```

and then opens anyway.

Refusals inside the panel, exactly as written:

- A row pinned by an environment variable refuses, and the foot says
  `held by <name> — unset it to change this here`.
- Only the conversation's own model slot is live here. Every other slot refuses with
  either `<label> is set with <ENV_VAR>` for a media slot backed by an environment
  variable, or `that model is chosen where its session is opened` for a role slot.
- Any refusal from the settings registry is shown on the foot line in the registry's own
  words. Nothing is swallowed.
- A search that matches nothing says `nothing matches`. The `Connections` tab has its own
  sentences.

## The six settings tabs

The tabs, in order:

**Session** — what this conversation may run, spend, and which models answer the small
calls it makes for itself. Rows include "ask before running", "tool exceptions", "shell
command rules", "guardian", "approval countdown", "check task work", "memory
consolidation", "task countdown", "task repair rounds", "tasks at once", "busy machine",
"memory floor", "task model", "small work", "careful work", "pinned roles", "fallback
models", "session ceiling".

**Context** — what a model carries. Rows: "compact at", "answer room", "working set",
"context reuse", "searching", "exa key", "jina key".

**Workspace** — what aforge may do and spend while it works for you. Rows: "daily
budget", "ask before spending", "practice budget", "quiet before practice", "arrival
brief after", "tenure after", "attribution", "google sign-in id", "google sign-in
secret".

**Display** — how the surface draws itself and what it remembers of your typing. Rows:
"input history", "keep drafts", "nerd font", "linear mode", "sidebar", "chat width",
"mouse", "timestamps".

**Providers** — which model answers what. Rows: "looking", "reading", "routing", plus one
row per model slot added automatically from the settings registry: drawing, speaking,
composing, filming, voice, and the conversation slot.

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
models that can see, "drawing" only ones that draw, "voice" only ones that hear, and
everything else follows the general chat rule. Its legend is
`↑↓ move · enter choose · esc cancel · type to filter`.

Because the picker is the same component, everything true of `/model`'s ranking, its rows
and its ctrl+t effort knob is true here too.
