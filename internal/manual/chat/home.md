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

## Everything I have ever worked on — what home shows

Two columns, no borders.

**On the left**, each project is a dim heading — the last part of its folder, or `~` for
your home directory — with one line per session under it:

```
  aforge-v2
  ● port the resume picker      2 running · 12m
  ○ import cleanup              3 tasks · 3h
```

A line is a glyph, the session's name, what it has going on, and how long since you last
spoke in it. The glyphs: `●` something is running, `◌` work was left unfinished, `○` at
rest. On a terminal that cannot draw them they are `*`, `o` and `-`.

Sessions with work running or work left unfinished come first inside a project, then the
rest by when you last spoke. Quiet ones past the first four collapse into one dim line,
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

`enter` on the one you are already in does no work and says `already here · <Name>`.

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

## Why a task says incomplete on home

The project's record of its work is append-only: a task writes a row when it starts and
another when it lands. So a machine that lost power, or an aforge that was killed, leaves
rows on disk that say `running` forever.

Home never repeats that claim. It asks the operating system who is actually holding each
journal open, and a `running` row in a session **nobody is holding** is drawn as
`incomplete` — work that was under way when the window went — rather than as something
happening now. The same rule decides the glyph: `◌`, not `●`.

The right column says `open here` for the one this window is in and `open in another
window` for one a second aforge has, and says nothing at all when nobody has it.

## Home on a fresh machine, and over --host

A machine that has held nothing yet draws one line:
`nothing here yet — say something and this fills up`.

Over `--host` home refuses to open and says
`home shows this machine's projects, and this session is on another`. The projects it
would read are under *this* computer's `~/.aforge/v3`, and the work is on the far end — a
screen full of the wrong machine's projects would be a confident lie.

## Does home update while I look at it?

Yes, every few seconds, by reading the folders again. A task landing in another window, or
work somebody starts in a second terminal, shows up without you doing anything. There is
no file watcher and nothing runs when the screen is closed.

The cursor stays on the row it was on rather than on the line number — the order genuinely
changes when work starts or finishes, and a cursor that stayed put would move you onto
something else between two glances.
