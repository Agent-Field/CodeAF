# Hints and tips

## What was that tip above the message box — the one-line hint in the border

The dim line at the right end of the rule above your message box is the hint slot. Most of
the time it names the keys that work right now — `esc interrupt` while an answer is coming,
`y allow · n deny · a always` while aforge is asking you something, `/ commands` when nothing
is happening. Once you have used aforge a little, that resting line sometimes carries a tip
instead: one sentence naming a key or a command you have not used yet, and what it does.
For example `ctrl+. sees every task this project has run`, or `esc esc takes back the last
message`.

A tip only appears over an empty box while nothing else is happening. The moment you type,
open a list, or an answer starts, the slot goes back to the keys for that state; the tip
returns when things are quiet again. A tip never takes a row of its own and never blocks a
keystroke — it is one line in a border that is on the screen anyway.

## Why did the hint disappear — each tip retires once you use what it teaches

Every tip is earned and then spent. It appears the first time it becomes relevant — the
first task you start, the first long answer, the first time a conversation passes half its
context window — and it goes away for good the first time you do the thing it names. Open
the task page once and `ctrl+. sees every task this project has run` never comes back; run
`/compact` once and the compact tip is retired.

A tip you never act on is not shown forever either. Once it has been shown in three separate
sessions it is taken as read and retires by itself. Between tips there is always a gap of a
couple of turns, so a busy first session does not turn the border into a slideshow.

This is remembered per profile, in a small file called `notices.json` beside `config.json`
in your aforge profile directory. Deleting that file brings every tip back once; nothing else
is in it.

## Every hint aforge can show, and what makes each one go away

There are seven at the moment. Each one names the moment it first appears and the gesture
that retires it.

There is no tip about `/`. There used to be one — `/ shows every command`, from the end of
your first turn — and it was removed because the resting line under your box already ends in
`/ commands`, on every frame, from the first one. A tip has to teach something the frame is
not already saying; this one stood in the slot and took the other three doors down with it.

- `ctrl+. sees every task this project has run` — after the first task starts. Retired when
  you open the task page, by `ctrl+.` or `/history`.
- `esc esc takes back the last message` — after an answer of about 1,500 characters or more.
  Retired the first time a rewind lands, from `esc esc` or from `/rewind`.
- `/compact summarizes the conversation now` — when the conversation passes half its
  context window. Retired when a `/compact` finishes.
- `/files finds everything made for you` — after the first `/export` writes a file. Retired
  when you run `/files`.
- `/resume opens an earlier conversation` — when you start in a directory that already has
  a conversation. Retired when you run `/resume`.
- `/cost says what this conversation has spent` — once the conversation has spent about
  ten cents. Retired when you run `/cost`.
- `/standing keeps something always true` — once this directory has three or more earlier
  conversations. Retired when you open `/standing` or make a standing order. It is the
  quietest of the seven and yields to every other.

When two are relevant at once the more useful one wins — the compact tip over the cost tip,
the cost tip over the task page tip — and the other waits its turn.

## Why the line above my box went blank — the key advertising decays with the tips

**Because you have earned the quiet.** Under every tip there is one more rung: the generic
key advertising, `space space home · tab last · ctrl+k switch · / commands`, which the
resting slot draws over an empty box. It is the same kind of thing as a tip — a nudge
towards a gesture you have not used yet — so it lives and dies with them. While any of the
seven tips is still un-retired the advertising is drawn; once every one of them has been
retired, that line is quiet too and the rule above the box is the plain rule it always was.

**It does not go quiet before then, and it survives your first answer.** Each clause is
under its own condition, so you see only the ones that would act: on a machine holding one
conversation there is nowhere for `tab last` to go and nothing for `ctrl+k switch` to list,
so the line reads `space space home · / commands` until there is.

**Only the advertising decays; the doors do not.** `space space` still opens home, `tab`
still goes back to the last conversation, `ctrl+k` still opens the switcher, `/` still
lists the commands — and the pointer's doors are unaffected: the top bar's project step
opens home, and the status row's `N open · M want you` opens the conversations list,
whether or not any of it is named.

The keys a live state names — `esc interrupt`, `y allow · n deny · a always`, `x stop` —
never decay. They are not hints; they say what the next keystroke does.

## Turn off hints — stop showing tips, disable the hints

Open the settings panel with `/settings` (or `ctrl+,`), go to the **Display** tab, and flip
the **hints** row off. Enter or space toggles it. The change lands at the end of the next
turn. Off silences the tips and the what's-new lines together; it does not touch the keys
the slot names for a live state — `esc interrupt` and the rest are not hints and cannot be
turned off.

Turning the row back on shows whatever is due. Tips you had already retired stay retired.

## What "news" lines are — what's new after an update

A news line is one dim sentence in the conversation, said once, the first time aforge runs
after its build has changed — the place a newly shipped feature introduces itself. It lands
under the replayed conversation and above the message box, and it never repeats: the build it
was said under is written into the same `notices.json` file the tips use, so the next launch
of the same build says nothing.

There is nothing to announce yet, so no news line has ever been printed by this build. A
first launch on a fresh profile says nothing either — nothing is new to somebody who never saw
the older build. The **hints** row on the Display tab silences news lines along with the tips.
