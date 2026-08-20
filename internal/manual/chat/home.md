# Home — every project and session on this machine

## See all my projects — /home

Type `/home`. It takes the whole screen and shows **every project on this machine and
every session in them**, not just the folder this window was started in.

The top line reads `home`, with `esc close` on the right. `esc` puts you back in exactly
the chat you came from, untouched — nothing was closed and nothing was sent while you were
looking.

There is no argument form. The screen is how you name what you want; a command that took a
project name would be asking you to type out the very thing home exists to show you.

Home is a glance you take, not a place you live. It does nothing on its own: no
notifications, no charts, no history graphs. You open it, you see where things stand, and
you leave.

## Why did a dashboard open when I started aforge — home greets you

**Home is the first thing you see when you open aforge.** The conversation your launch
would have opened is loaded and waiting underneath it: `esc` drops straight into it, and so
does `enter` on the row you are already standing on, which is where the cursor starts. In
effect the launch is the launch you always had, with home already open on top of it.

Nothing about *which* conversation opens is changed by this. The door picks it exactly as it
always did — this directory's most recently spoken-in chat, or a fresh one — before home is
drawn at all.

It greets you only when it has something to say. All of these have to be true:

- You opened aforge **without naming a conversation**. `aforge` or `aforge chat`.
- The machine holds **a conversation other than the one this launch opened**. Somewhere
  else to go, in other words.
- It is a real terminal session — not `--once`, not `--host`.

When home greets you there is no welcome box: home's left column already lists every
conversation the box's `recent sessions` would have, and more.

## Skip the home screen — launching straight into a conversation

Four ways, and each of them is you saying which conversation you mean:

| What you run | What you get |
|---|---|
| `aforge chat --session <path>` | that conversation, no home |
| `aforge resume` | the session picker, no home |
| `aforge chat --once "text"` | one reply, printed; no surface at all |
| `aforge --host <machine>` | the far machine's session, no home |

And on a machine with only one conversation — a first run — home stays out of the way by
itself. There is no setting for this and no flag to turn it off: whether home greets you
follows from how you launched and what the machine holds, both of which answer themselves.

Once you are in a conversation, `/home` opens the screen whenever you want it.

## Everything I have ever worked on — what home shows

Two columns, no borders.

**On the left**, each project is a dim heading — the last part of its folder, or `~` for
your home directory — with one line per session under it:

```
  aforge-v2
  ▲ pricing research          waiting on you · 4m
  ● port the resume picker      2 running · 12m
  ○ import cleanup              3 tasks · 3h
```

A line is a glyph, the session's name, what it has going on, and how long since you last
spoke in it. The glyphs: `▲` it is stopped waiting on you, `●` something is running, `◌`
work was left unfinished, `○` at rest. On a terminal that cannot draw them they are `!`,
`*`, `o` and `-`.

Inside a project the order is **what wants you first**: sessions stopped on a question,
then ones with work running, then ones with work left unfinished, then the rest by when
you last spoke. Quiet ones past the first four collapse into one dim line,
`…3 more, quiet since 2d`.

**On the right** is whatever the cursor is on: its name, its project and workspace path,
whether a window has it open, a short list of the work it ran, what it spent, and the last
thing said in it. Under 76 columns the right column is dropped and the list takes the
frame.

Nothing that is zero is drawn. A chat that ran no tasks says nothing about tasks; one that
spent nothing says nothing about spending.

## Switch between sessions — enter on home

`↑`/`↓` (or `ctrl+p`/`ctrl+n`) walk the rows, stepping over the project headings.
`pgup`/`pgdown` jump four. The right column follows the cursor. Clicking a row puts the
cursor on it; clicking the row the cursor is already on opens it.

`enter` opens the session under the cursor. That is the same door `/resume` walks through:
the running turn is interrupted, the chat you were in is closed, the chosen journal is
opened and replayed, and the surface says `resumed <path>`.

`enter` on the one you are already in simply steps into it and says nothing — it is
already loaded underneath, so there is nothing to reopen and nothing to announce. That is
what makes `enter` the calm keystroke on a launch: the cursor starts on that very row.

The foot line reads exactly:

```
type start something new · @ find · enter open
```

and the line under it says what the keyboard does: `↑↓ move · enter open · esc close`.

## What home will not do yet — opening another project's work

**`enter` only opens sessions of the project this window is in.** Every other project is
shown, and its heading carries a dim `elsewhere` to say so. Pressing `enter` on one of its
rows opens nothing and says `elsewhere · <the project's path>` — the path to start aforge
in.

This is a limit and not a bug. A window's approval rules, its crew, its spend ceiling and
its saved shapes of work were all resolved from the workspace it launched in; carrying a
chat across without carrying those would be a window quietly running under another
project's permissions. Until that is built, home shows you the whole machine and moves you
around inside one project of it.

So: home is the honest answer to "what have I been doing everywhere". It is not yet the
answer to "put me in that other project without changing terminals".

## Find an old chat from anywhere — the @ box on home

Type `@` on home and the left column becomes a search over **everything on the machine**,
headings dropped, best match first. Each row then carries its project name on the right
instead of its age.

It ranks over the name and the project together, on the same ladder the model picker uses:
a prefix beats a substring beats letters found in order. So three characters of something
you did last week finds it.

`enter` opens the highlighted row, under the same rule as everywhere else on home — this
project's rows open, others say `elsewhere`. The hint line reads
`type to search every conversation · enter open · esc clear`, and `enter open · esc clear`
once you have typed something.

A filter that matches nothing draws `no conversation matches`.

`esc` clears the box and leaves you on home. A second `esc` closes home.

## Start something new from home — just type

Anything you type on home that does not begin with `@` goes into the box at the foot, and
`enter` **closes home, opens a fresh session in this project, and sends what you typed as
its first message.** No picker, no folder to choose, nothing declared before there is
anything to declare it about.

While something is in the box, the hint reads
`enter starts a new conversation here and sends this · esc clear`.

It is `/new` followed by your sentence, so everything `/new` does applies: the running turn
is interrupted, a fresh session file is opened, and the transcript, the task column and any
pending offers all belong to the chat that just closed. On a surface with no fresh-session
seam it refuses in `/new`'s own words, `/new is unavailable here`.

`esc` clears the box without sending it.

## Is there a key for home?

**No. `/home` is the only way in.** Every `ctrl+<letter>` this surface could use is
already taken, and the chords that were left — `ctrl+.` and the `alt+` letters — arrive in
some terminals and silently do nothing in others. A key that works on one machine and not
the next is worse than a command that works everywhere, so none was bound.

## Why a session says it needs you — waiting on you

A session that has asked you something and can go no further writes that down, and home is
where you see it without opening the window it is in. The row wears `▲`, its rollup reads
exactly `waiting on you`, and **it sorts to the top of its project** — above work that is
running, above everything you spoke in more recently.

The right column then shows the one line it is stopped on, and it is the only thing on
that pane that is not dim: everything else there is a fact about what happened, and this
is a thing somebody has to do. A session that gave no words for what it is waiting on
shows no line at all rather than a placeholder.

This is read out of a small file each live session keeps in its own folder, refreshed
every five seconds and believed for fifteen. So a window that was killed, or a laptop that
closed, stops claiming to need you within a glance — nothing on home asks you for
something that nobody is waiting for any more.

`enter` on the row opens it under the ordinary rule, so a question in this project is one
key away and one in another project tells you where to go.

## Why a task says incomplete on home

The project's record of its work is append-only: a task writes a row when it starts and
another when it lands. So a machine that lost power, or an aforge that was killed, leaves
rows on disk that say `running` forever.

Home never repeats that claim. **It asks the session itself.** A live session says out
loud, every few seconds, which task nodes it currently has out; a `running` row is drawn
as `running` only when the session that ran it is still alive and still names that node.
Every other live-looking row is `incomplete` — work that was under way when the window
went — and the same rule decides the glyph: `◌`, not `●`.

A session too old to keep that file, but whose journal a window is holding, falls back to
the older answer: the lock is asked, and its rows are believed. That is the same rule with
less to go on, not a different one.

The right column says `open here` for the one this window is in and `open in another
window` for one a second aforge has, with what it is doing after it — `open in another
window · working`. `idle` is not spelled out, because it is what an open session usually
is. Nothing at all is said when nobody has it.

## Home on a fresh machine, and over --host

A machine that has held nothing yet draws one line:
`nothing here yet — say something and this fills up`.

Over `--host` home refuses to open and says
`home shows this machine's projects, and this session is on another`. The projects it
would read are under *this* computer's `~/.aforge/v3`, and the work is on the far end — a
screen full of the wrong machine's projects would be a confident lie.

## Does home update while I look at it?

Yes, every few seconds, by reading the folders again. A task landing in another window,
work somebody starts in a second terminal, or a session stopping to ask a question all
show up without you doing anything. There is no file watcher and nothing runs when the
screen is closed.

The cursor stays on the row it was on rather than on the line number — the order genuinely
changes when work starts or finishes, and a cursor that stayed put would move you onto
something else between two glances.
