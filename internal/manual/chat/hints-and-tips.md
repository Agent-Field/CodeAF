# Hints and tips

## What was that tip above the message box — the one-line hint in the border

The dim row under your message box — the last row of the frame — is the hint slot (until
2026-09-17 it was the right end of the rule above the box; the numbers have that end now).
Most of the time it names the keys that work right now — `ctrl+c interrupt` while an answer is coming,
`y allow · n deny · a always` while codeaf is asking you something, `/ commands` when nothing
is happening. Once you have used codeaf a little, that idle line sometimes carries a tip
instead: one sentence naming a key or a command you have not used yet, and what it does.
For example `ctrl+. sees every task this project has run`, or `/rewind takes back an earlier
message`.

A tip only appears over an empty box while nothing else is happening. The moment you type,
open a list, or an answer starts, the slot goes back to the keys for that state; the tip
returns when things are quiet again. A tip never takes a row of its own and never blocks a
keystroke — it is the keys row, which is on the screen anyway.

Home has a row of its own for the same tips, directly above the rule over its box — see
*The dim sentence above the rule on home*.

## The dim sentence above the rule on home — the tip on home, what is that line over the box

On home the tip is the dim row **directly above the rule** over the message box — the blank
that separates the list from the rule, with one sentence written into it. It reads the way
every tip does: the key or the command first, then what it does — `/ask answers right here
without opening a conversation`, `alt+1 to alt+7 jump straight to a place`. The keys row at
the very foot of home is not a tip and never changes: it names the row's options and the
draft's chords (`→ options · alt+p project · alt+e effort · alt+a approvals · / commands`).

It is there only while home is at rest: the box empty, no command list or model list up, no
task or question open in the right pane. Type a letter and the row is blank again; clear
the box and the tip is back. The row is the same row whether or not a tip is on it, so the
list above never moves.

**It changes on every visit and every two minutes.** Each time you come to home — `esc`
from a conversation, `/home`, `alt+1`, `tab` — the row moves on to the next tip that is
true for you, in a fixed order, round and round. Left at rest, it moves on by itself after
two minutes; a home nobody is looking at (the box being typed into, a list up) does not
age, because what has not been read has not been shown. On a Mac the row says `opt` where
the table below says `alt`, exactly as the keys row does.

## Why did the hint disappear — each tip retires once you use what it teaches

Every tip is earned and then spent. It appears the first time it becomes relevant — the
first task you start, the first long answer, the first time a conversation passes half its
context window, or simply the first time home is open — and it goes away for good the first
time you do the thing it names. Open the task page once and `ctrl+. sees every task this
project has run` never comes back; run `/compact` once and the compact tip is retired. A tip
retired from either box is retired from both: opening the model list on home retires
`/model lists every model` in every conversation as well.

A tip you never act on is not shown forever either. In a conversation, once it has been
shown in three separate sessions it is taken as read and retires by itself; on home, where
the row turns over faster, a tip retires after six turns of the rotation. Between tips in a
conversation there is always a gap of a couple of turns, so a busy first session does not
turn the border into a slideshow.

This is remembered per profile, in a small file called `notices.json` beside `config.json`
in your codeaf profile directory. Retiring is permanent: turning hints off and on does not
bring a retired tip back. Deleting that file brings every tip back once; nothing else is in
it.

## Every hint codeaf can show, and what makes each one go away

There are thirty. Each one says where it can appear — in a conversation's keys row, on
home's row above the rule, or both — the moment it first appears, and the gesture that
retires it. The list is the program's own table (the surface refuses to build if the two
disagree), so a tip you saw is on it word for word.

**Starting work**

- `/ask answers right here without opening a conversation` — home only, whenever home's
  ask door is there. Retired the first time `/ask` or `alt+enter` sends something from home.
- `alt+enter sends what you typed off as a task` — home only, on the same terms and retired
  by the same gesture.
- `/task starts work you can walk away from` — conversation only, after the first exchange.
  Retired when `/task` is typed, bare or with a brief.
- `ctrl+enter sends your message as something to keep true` — both. Retired when a standing
  order is made or the standing page opened.
- `/standing keeps something always true` — both, once this directory has three or more
  earlier conversations. Retired by the same gesture; it is the quietest and yields to every
  other in a conversation.
- `ctrl+r spells out what your sentence is taken to mean` — both, where the chord works.
  Retired the first time you press it.

**Files and context**

- `@ completes a file, a folder or a task into your message` — both. Retired when the `@`
  list opens.
- `/attach sends a file along with your message` — both. Retired when a file goes on the
  tray by path or the file browser opens.
- `/image attaches a picture, or paste a screenshot in` — both. Retired by the same gesture.
- `/folder picks the folder codeaf works in` — both. Retired when the folder chooser opens,
  from a conversation or aimed at home's target.
- `/export writes this whole conversation to a file` — conversation only, after two
  exchanges. Retired when an export lands.
- `/files finds everything made for you` — both, after the first export writes a file.
  Retired when you run `/files`.

**Models, thinking and cost**

- `/model lists every model, /model <slug> switches at once` — both. Retired when the model
  list opens, over a conversation or over home's draft.
- `/crew sets the models codeaf uses on its own behalf` — both. Retired when `/crew`
  answers, bare or with a preset.
- `/budget caps what today may cost` — both. Retired when `/budget` answers.
- `alt+3 shows what this machine has spent, by the day` — both. Retired when the spend
  place opens by any door.
- `/cost says what this conversation has spent` — both, once the conversation has spent
  about ten cents. Retired when you run `/cost`.
- `/compact summarizes the conversation now` — both, when the conversation passes half its
  context window. Retired when a `/compact` finishes.

**Steering a running answer**

- `enter while an answer is coming stops it and steers` — conversation only, after the
  first exchange. Retired the first time you steer.
- `ctrl+q queues this message for after the current turn` — conversation only, after the
  first exchange. Retired the first time you queue one.
- `/rewind takes back an earlier message` — both, after an answer of about 1,500 characters
  or more. Retired the first time a rewind lands.

**Moving around**

- `ctrl+t starts a fresh chat in this folder` — both. Retired when the new-chat page opens.
- `alt+1 to alt+7 jump straight to a place` — both. Retired the first time a place chord
  reaches one.
- `ctrl+. sees every task this project has run` — both, after the first task starts.
  Retired when you open the task page, by `ctrl+.` or `/history`.
- `/resume opens an earlier conversation` — both, when you start in a directory that
  already has a conversation. Retired when you run `/resume`.

**Memory, accounts and the rest**

- `/remember keeps one thing across conversations` — both. Retired when `/remember` is
  typed.
- `/search finds anything ever said on this machine` — both. Retired when the search place
  opens by any door.
- `/subharness lists the programs you can run` — both. Retired when `/subharness` is typed,
  bare or with a name.
- `/connect links Google, Slack or another model service` — both. Retired when the connect
  panel is reached for.
- `ask for a picture, a voiceover, music or a video` — both. Retired the first time the
  session begins making one.

When two are relevant at once in a conversation the more useful one wins — the compact tip
over the cost tip, the cost tip over the task page tip — and the other waits its turn. On
home nothing wins: every tip that is true for you has its turn, in the order above.

`/ shows every command` used to be one of these. It is gone because both keys rows now say
`/ commands` outright, so there was nothing left to teach.

## Turn off hints — stop showing tips, disable the hints

Open the settings panel with `/settings` (or `ctrl+,`), go to the **Display** tab, and flip
the **hints** row off. Enter or space toggles it. The change lands at the end of the next
turn. Off silences the tips — in the conversation's keys row and on home's row alike — and
the what's-new lines together; it does not touch the keys the slot names for a live state —
`ctrl+c interrupt` and the rest are not hints and cannot be turned off.

Turning the row back on shows whatever is due. Tips you had already retired stay retired.

## What "news" lines are — what's new after an update

A news line is one dim sentence in the conversation, said once, the first time codeaf runs
after its build has changed — the place a newly shipped feature introduces itself. It lands
under the replayed conversation and above the message box, and it never repeats: the build it
was said under is written into the same `notices.json` file the tips use, so the next launch
of the same build says nothing.

There is nothing to announce yet, so no news line has ever been printed by this build. A
first launch on a fresh profile says nothing either — nothing is new to somebody who never saw
the older build. The **hints** row on the Display tab silences news lines along with the tips.
