# The empty screen — nothing on it, the screen is blank

## What you see when you open codeaf — the empty conversation, the greeting, why the screen is blank

A new conversation opens on **one centred group** and nothing else. From the top:

```
          ╷     ┌─┐ ┌──
┌─  ┌─┐ ┌─┤ ┌─┐ │ │ ├─          the codeaf wordmark, drawn CodeAF
│   │ │ │ │ ├─╴ ├─┤ │
└─  └─┘ └─┘ └─┘ ╵ ╵ ╵
anthropic/claude-sonnet-4.5 · balanced crew
                                 a blank row
› _                              the message box, cursor in it
try "what is in this folder" · /task <brief> starts work · / shows commands

recent sessions                  only when this folder has earlier conversations
  porting the parser     20m
  the welcome box        3h
```

The line under the wordmark is the model that will answer, as its full routing
address, and the crew preset behind it (`frugal`, `balanced`, `max`, or `custom`) —
the two facts that decide what a turn costs. The group is centred in the window, a
shade above the middle. It shows once, arrives with a slow sweep across the letters
over about a second and a quarter, and is then still.

**Nothing else is drawn.** No column of tasks on the right, no rule with the legend in
it — so no conversation name and no model above the box yet — no `+ /task` or
`+ /standing` doors, no `❯ ctrl+g hide`, no `$0.00` and no token count on the status
line — only `idle` at the right of the status row. Every one of those arrives with the
conversation rather than before it (see the other headings on this page).

**The first key, the first submit, or a click anywhere on the group except a recent
session or the message box puts it away for good.** The message box goes back to the
foot of the frame with your keystroke already in it, the legend and the column appear,
and nothing brings the greeting back — not `/new`, not a resize. It never shows over a
resumed conversation, and it is not drawn at all on a frame under 12 rows or under 40
columns, where the prompt simply opens at the foot as it always did. When the home
screen greets you instead, there is no greeting of this kind at all.

**Your very first conversation on this machine is drawn differently, and it leads with the
question rather than the logo.** The greeting that follows the first-run setup drops the
three-row wordmark and the model-and-crew line under it — you chose those on the screen
behind this one, and the status row still says both — and puts a one-word `codeaf`
signature there instead. Under it: the heading **What would you like to work on?**, the
line `Choose a starting point or type your request.`, the folder this conversation is
standing in (`in ~/src/parser`), and three starting points in place of the usual dim try
line:

```
  Understand this folder
  Make or fix something
  Compare two options
↑↓ choose · enter fills the box · or just type
```

`↑`/`↓` walk them over an empty box and only the selected one gets a helper line under
it saying what actually happens next. **`enter` on a starting point fills the message box
and sends nothing** — the sentence lands in the composer with the cursor after it, and you
edit it, add to it or delete it. Two of the three are deliberately unfinished (`Fix this
for me: `) because a starting point that filled the box with a complete request about
your project would be guessing at your work.

**On that first conversation the composer does not move when you start typing.** Every
later greeting is dismissed by the first keystroke and the box drops to the foot of the
frame; this one stands until you actually send something, so the box you aimed at does
not move out from under you mid-word and the three starting points stay readable while
you decide. A starting point can only be selected while the box is empty, so it can never
overwrite something you had already typed. A folder with earlier conversations in it is
not having its first one, whatever the profile says, and gets the ordinary greeting.

## The message box is in the middle of the screen — why did it move, where do I type

On the empty screen the message box inside the centred group **is the real message
box**: the terminal's cursor is in it and your first keystroke lands there. That
keystroke also retires the greeting, so the box returns to its usual place at the foot
of the frame with the letter you typed in it, and everything after that is the chat as
it always was. Typing is never blocked and no keystroke is eaten.

A click on the box leaves the greeting standing — the keyboard is already there. A
click on a recent session opens it; a click on the wordmark or the line of things to
try dismisses the greeting.

One exception: when something else is holding the message box — `codeaf resume` opens
the sessions picker over the greeting, for instance — the group is drawn without the
box and the picker's filter stays at the foot of the frame where its list is.

## Where is the task column, the sidebar, the right rail on a new conversation — what happened to the sidebar, the column on the right is missing

**The column on the right is absent while it has nothing to say.** On an untouched
empty conversation there is no column, no border, no `+ /task`, no `+ /standing`, no
`❯ ctrl+g hide`, and no two-column edge from a column you closed in an earlier session.
The conversation is laid out at the full width of the terminal.

It appears the moment there is something true to put on it:

- **your first keystroke** — the conversation has begun, and the column stands from then
  on exactly as the tasks page describes, doors and all, at 100 columns or more;
- **a task** — a standing task landing on this conversation raises it;
- **a standing order** reaching this conversation — a session with orders over it meets
  the column on its very first frame, with the orders on it.

The `+ /task` and `+ /standing` doors are not lost: `/` lists every command, and the
line under the message box says `/task <brief> starts work`. `ctrl+g` on the empty
screen counts as a first keystroke like any other — the greeting goes, and the key then
does what it always does, which is to close the column it would just have raised; press
it again to bring the column back. `alt+t` — the roster's own chord — falls through on a
fresh screen, as it does on any conversation that has run nothing. `ctrl+t` is not that
key: it starts a new chat, and it works on a fresh screen like anywhere else.

## Why the status line shows no cost or token count before I type — where is the $0.00

While the greeting is up the status row carries **only the state**: `idle` on the right,
and nothing on the left. There is no `$0.00`, no `9.4k/1.3M · 1%` context meter, no cache
figure. The conversation's name and model arrive on the legend line above the box with
the conversation itself. A conversation nobody has typed into
has nothing to bill and nothing but its own prompt to meter, and a row of zeros under a
greeting was the first thing a person paying with their own card read.

From your first keystroke on — the same frame the greeting dissolves on — the row is
exactly what the *What is on the screen* page describes, `$0.00` included: that figure
stays on a session in use so the segments beside it do not jump sideways as it changes
width. The phone-width status deck keeps the same rule: no spend and no percentage while
the greeting is up. The full-screen status sheet, `/status` and `/cost` are unchanged —
they answer with the prompt's size when asked, and never print a zero bill.

A resumed conversation has no greeting, so its row shows the numbers on its first
frame — and they are **the whole conversation's, not this sitting's**. The spend
segment opens on the total its transcript records, before you type anything and
before any turn is sent, so a session reopened tomorrow does not read as free. A
resumed conversation that genuinely never spent anything still shows `$0.00`,
because that is what a row in use shows.

## The try line under the message box — what does "try what is in this folder" mean

The dim line under the message box reads, in full:

```
try "what is in this folder" · /task <brief> starts work · / shows commands
```

It is a line of things to type, not a menu and not a command. The first clause is a
sentence you can send as-is in any directory; the second says that `/task` followed by a
brief starts work in the background (see the tasks page); the third says that `/` on an
empty box lists every command. On a narrower window the clauses drop from the left —
first the example, then the task door — and the last thing standing is `/ shows
commands`. The line dissolves with the rest of the greeting on your first keystroke and
is not drawn again.

On your **first** conversation this line is not drawn at all: the three starting points
above take its place, because a row you can press and a line telling you to type the same
sentence are one idea drawn twice.

## Recent sessions on the empty screen — where did the recent sessions list go

When this folder has earlier conversations, up to four of them are listed under a dim
`recent sessions` heading beneath the line of things to try: a name and a coarse age
(`now`, `12m`, `3h`, `5d`, then a date like `16 Aug`), the ages in one column. With the
message box empty, `↑`/`↓` walk the list and `enter` opens the highlighted one; a click on
a row opens it too. On a short window the list is cut to the rows that fit.

**When there are none, nothing is drawn** — no heading, no `no recent sessions` line, no
rows held open. A fresh machine sees the wordmark, the model line, the box and the try
line, and that is all. Every earlier conversation, in every project, is on the home
screen (`/home`, or space twice on an empty box) and in `/resume`.

## The line about esc and ctrl+c — when does "esc interrupts · ctrl+c quits" appear

The empty screen carries no line about leaving. `esc interrupts · ctrl+c quits · ?
for help` lands as the first dim line of the conversation the moment the greeting goes — your
first keystroke — where it sits directly above the box you have just started typing
into. A conversation that opens on a transcript, such as a resumed one, has it on its
first frame as before. The keys themselves work at every moment either way: `esc`
stops a running answer, and `ctrl+c` at rest quits on the press that lands.
