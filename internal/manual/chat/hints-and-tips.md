# Hints and tips

## What was that tip at the bottom of the screen — the one-line hint, the sentence under the message box

The dim row under your message box — the last row of the frame — mostly names the keys that
work right now: `esc interrupt` while an answer is coming, `y allow · n deny · a always`
while codeaf is asking you something, `space space home` when there is a home to go to,
`/ commands` when nothing else is true. Once you have used codeaf a little, that idle line
sometimes carries a **tip** as well — one sentence naming a key or a command you have not
used yet, and what it does, for example `esc esc or /rewind takes back an earlier message`
or `/files finds files codeaf wrote for you`. It reads the way every hint on this surface
does: the key or the command first, then what it does.

**In a conversation the tip stands at the right end of the keys row, covering the
project** (`project: <path>`) while it is up — led by a bulb (💡) and closed by a small
cross (`✕`), as home's is. The controls keep their place at the row's left:

```
─ glm-5.3-flash:auto · ◇ asks ──────────────────────────────────────  $0.00   idle ─
 ›
 opt+e effort · / commands · space space home   💡 /files finds files codeaf wrote for you ✕
```

When the tip goes, `project:` is back in its place. When the row is too narrow for the
whole sentence it is cut short with `…`, and where there is no room for a word of it the
project stays.

**It waits for 15 seconds of quiet**, counted from when codeaf opened at the earliest.
Any key, click, scroll or paste hides it and starts the 15 seconds again, and so does an
answer finishing — the tip never appears under an
answer you have just started reading or between two things you are typing. A running turn,
a list, a panel, a room, or a box with so much as one letter in it also keep it down. It
shows whether the task column is open or put away with `ctrl+g`.

**The cross puts it away**, and gives the project back, until you leave the conversation —
for home, a place, or another conversation — and come back; it does not retire the tip,
which comes round again later.

**Home says its tips differently, and the two are not the same row.** Home's is the dim
line **directly above the rule** over its box, right-aligned, led by a bulb and closed by a
small cross. It rotates on every visit and every two minutes at rest, and the cross blanks
it until you leave home and come back. A conversation is a screen you sit in and home is a
screen you pass through, so the tip worth saying differs: a conversation gets the most
urgent thing that is true right now, and home gets everything in turn. Home's row does not
wait for quiet; a conversation's waits 15 seconds.

Both rows draw from **one list** of twenty-three tips (below), and using a gesture on
either retires it on both.

**A conversation's tip has moved several times.** It was the keys row's lowest rung until
2026-09-22, moved up to a row of its own over the rule that day — with a quiet minute
before it appeared, a two-minute rotation and a cross — and moved back to the keys row the
same day, replacing the controls there. On 2026-09-24 it stopped replacing the controls,
spent part of that day above the rule again, and settled at the keys row's right end over
the project, with a cross and a 15-second wait, which is where it is now.

On a Mac the row says `opt` where the table below says `alt`, exactly as the keys row does.

## The dim sentence above the rule on home — the tip on home, what is that line over the box

On home the tip is the dim row **directly above the rule** over the message box — the blank
that separates the list from the rule, with one sentence written into its right end, led
by a bulb: `💡 /project sets the project folder for a new conversation ✕`. It is drawn only
while the box is empty and nothing else is up — a letter in the box, the `/` list, the `@`
list or a reply being read all take the row back — and it moves on to the next tip that is
true for you on every road home (two spaces in an empty box, `/home`, `alt+1`, `tab`), in a
fixed order, round and round. Left at rest, it moves on by itself after two minutes; a home
nobody is looking at (the box being typed into, a list up) does not age, because what has
not been read has not been shown. The cross at its end blanks the row for the rest of that
visit — leaving home and coming back is what brings the next tip — and costs the one you
put away nothing.

## Why did the hint disappear — each tip retires once you use what it teaches

Every tip is earned and then spent. It appears the first time it becomes relevant — the
first task you start, the first long answer, the first time a conversation passes half its
context window, or simply the first time home is open — and it goes away for good the first
time you do the thing it names. Run `/files` once and `/files finds files codeaf wrote for
you` never comes back; run `/compact` once and the compact tip is retired. A tip
retired from either box is retired from both: opening the model list on home retires
`/model lets you see and choose models and providers` in every conversation as well.

A tip you never act on is not shown forever either. Once a tip has been **shown six times**
it is taken as read and retires by itself — and the two rows count a showing differently,
because they behave differently.

- **In a conversation, a showing is one session.** However many times the tip comes and
  goes on the keys row while you work, that is one showing, counted the first time the row
  actually draws it — after its 15-second wait — so a tip you never paused long enough to
  see is not counted. Six sessions of never acting on it and it is done.
- **On home, a showing is a tip that stood twenty seconds or more on a row you could see.**
  Passing through home for a second or two is not a showing, however many times you do it,
  and a row deciding while home is not in front is not one either. Until 2026-09-22 every
  visible change of hands on home counted, so an afternoon of stepping through could spend
  the whole table in flashes nobody read; the first launch of a build with the
  twenty-second rule gives back, once, every tip that rule spent, and leaves retired every
  tip you retired by using it.

This is remembered per profile, in a small file called `notices.json` beside `config.json`
in your codeaf profile directory. Retiring is permanent: turning hints off and on does not
bring a retired tip back. Deleting that file brings every tip back once; nothing else is in
it.

## No hints at all any more, nothing on home's row — every tip has been retired

When neither row says anything and hints are not turned off, every tip in the table has
retired: you have used what each one teaches, or it stood its six showings. That is the
design working, not a fault — the row over the box is for what you have not found yet.
To see the whole set again, delete `notices.json` from your profile directory; the next
launch starts every tip from nothing.

## The tip on home changed by itself — the order the tips come round in, and the tip that jumps the queue

Home's row takes turns through the one list, in the order below, round and round: every tip
that is true for you gets its turn before any repeats, and a tip that stops being true
stands down at once for the next. (A conversation's keys row does not take turns: it ranks,
and the first tip in the list that is true for you there is the one it says.) Nothing outranks anything — with one exception. **A tip
that has just become true jumps the queue**: when a conversation crosses half its context
window, `/compact summarizes the conversation now` is said next rather than forty minutes
later when the ring comes round. It jumps once and then takes its turn like the rest.

## Every hint codeaf can show, and what makes each one go away

There are twenty-three, one list for both boxes. Each one says the moment it first appears
and the gesture that retires it. The list is the program's own table (the surface refuses to
build if the two disagree), so a tip you saw is on it word for word.

**Starting work**

- `/compact summarizes the conversation now` — when the conversation passes half its
  context window. Retired when a `/compact` finishes.
- `/cost says what this conversation has spent` — once the conversation has spent about
  ten cents. Retired when you run `/cost`.
- `esc esc or /rewind takes back an earlier message` — after an answer of about 1,500
  characters or more. Retired the first time a rewind lands, by either door. It is the one
  row that names a chord and a command for the same thing, on purpose: `esc esc` is the
  half nobody discovers, and `/rewind` is the half you can type into `/` or ask the manual
  about a week later.
- `/files finds files codeaf wrote for you` — after the first export writes a file. Retired
  when you run `/files`.
- `/resume opens an earlier conversation` — when you start in a directory that already has
  a conversation. Retired when you run `/resume`.
- `/standing turns a message into a rule work must follow` — once this directory has three or more earlier
  conversations. Retired when a standing order is made or the standing page opened.
- `space space takes you back to home` — after the first exchange, in a conversation only
  (never on home itself). Retired the first time two spaces in an empty box open home,
  from a conversation or from a place; reaching home by `/home` or the tab does not retire
  it. It is the first tip a conversation says, ahead of `/task`.
- `/task starts a single-shot task on the side` — after the first exchange. Retired when
  `/task` is typed, bare or with a brief.
- `ctrl+enter makes your message a rule instead of a request` — retired when a standing
  order is made or the standing page opened. It teaches the same door as the `/standing`
  row above and retires with it, so the two say a rule in the same words.
- `/manual answers any question about codeaf` — retired when
  `/manual` is typed, bare or with a question.
- `ctrl+shift+t reopens the last conversation tab` — retired the first time the chord is
  pressed, on a terminal that can send it.

**Files and context**

- `type @ to find paths in the current project` — retired when the `@` list
  opens.
- `/attach sends a file or folder with your message` — retired when a file or a folder goes
  on by path, or the browser opens.
- `/project sets the project folder for a new conversation` — on home only, since that is
  the only screen `/project` works on. Retired when `/project` takes a folder, by a path
  after it or on the browser it opens.
- `/export writes the current conversation to a file` — after two exchanges. Retired when
  an export lands.

**Models, thinking and cost**

- `/model lets you see and choose models and providers` — retired when the model list
  opens, over a conversation or over home's draft.
- `/crew sets the models codeaf uses on its own behalf` — retired when `/crew` answers,
  bare or with a preset.
- `/budget sets the spending cap for the day` — retired when `/budget` answers.

**Steering a running answer**

- `using enter steers conversations · use ctrl+q to queue messages` — after the first
  exchange. Retired the first time you queue a message. It was two rows until 2026-09-22
  — one for the steer and one for the queue — and the owner folded them into one.

**Moving around**

- `ctrl+t starts a fresh chat in this project` — retired when the new-chat page opens.

**Memory, accounts and the rest**

- `/remember carries a fact forward, /forget drops it` — retired when `/remember` is typed.
  It is the only row that names two commands as a pair, because the two rows about keeping
  something used to be told apart by nothing: a standing order is a condition the work has
  to honour and a memory is a fact carried forward.
- `/connect links Notion, Slack and other services` — retired when the connect panel
  is reached for.
- `/autonomy sets how questions are handled while you are away` — after the first
  exchange. Retired when `/autonomy` is typed, bare or with a rule. (It took the seat
  `ctrl+b freezes the screen so you can read and copy from it` held for one build on
  2026-09-22, and `ask for a picture, a voiceover, music or a video` before that.
  **Copy mode itself still works** — `ctrl+b`, `/copy`, the whole frozen viewport — it
  just has no tip on this list any more.)

**Eight rows came off on 2026-09-22**, over three reads of the whole list, and **every one
of the commands they named still works** — only the tips about them are gone.

- `alt+3 shows what this machine has spent, by the day`, `alt+1 to alt+7 jump straight to a
  place`, `/search finds anything ever said on this machine` and `/subharness lists the
  programs you can run` name doors the tab bar or the `/` list already puts in front of
  you, which is the argument that kept `alt+p`, `alt+e` and `/` off the list in the first
  place.
- `/ask answers right here without opening a conversation` came off ahead of the door it
  taught, and the door followed: `/ask` is not a command any more, and asking from home is
  the `ask here` row over home's box. It was the only tip true on home alone until
  `/project` took that place.
- `/folder picks the folder codeaf works in` came off because **it was not true**. `/folder`
  never moves the directory codeaf is standing in — that is fixed for the life of a
  conversation — it registers a directory the conversation is *about*, which is exactly
  what a folder after `/attach` does, through the same door. So `/attach`'s row says "a
  file or folder" now and this one is gone.
- `/attach lets you browse anywhere for files` was a second row about one command, which
  is one row too many.
- `ctrl+. sees every task this project has run` came off on the owner's word, the last of
  the three reads. The chord still opens the task page, `/history` still opens it too, and
  the *tasks* page still says so.

Unless a line above says otherwise, a tip is true from the first minute on home and after
the first exchange in a conversation.

`/ shows every command` used to be one of these. It is gone because both keys rows now say
`/ commands` outright, so there was nothing left to teach. Three more were cut on
2026-09-22: an `alt+enter` tip that promised a task where the chord asks, a `ctrl+r` tip
for a chord that works only in a conversation and only over a making-shaped sentence, and
`ask for a picture, a voiceover, music or a video`, whose seat the copy-mode tip took.

## Turn off hints — stop showing tips, disable the hints, the disable hints row

Open the settings panel with `/settings` (or `ctrl+,`), go to the **Workspace** tab, and flip
the **disable hints** row on. The line under the row reads
`disable💡 tips everywhere (requires restart)`. Enter or space toggles it; it is off by
default, which means the tips show. (Until 2026-09-22 it was a **hints** row on the Display
tab, on by default.) Inside a running codeaf the change lands at the end of the next turn,
in every conversation and on home alike; restarting codeaf is the only way to have it at once. On silences the tips — over a conversation's
box and over home's alike — and the what's-new lines together; it does not touch the keys
row's own words for a live state — `esc interrupt` and the rest are not hints and cannot
be turned off.

Turning the row back off shows whatever is due. Tips you had already retired stay retired.

## What "news" lines are — what's new after an update

A news line is one dim sentence in the conversation, said once, the first time codeaf runs
after its build has changed — the place a newly shipped feature introduces itself. It lands
under the replayed conversation and above the message box, and it never repeats: the build it
was said under is written into the same `notices.json` file the tips use, so the next launch
of the same build says nothing.

There is nothing to announce yet, so no news line has ever been printed by this build. A
first launch on a fresh profile says nothing either — nothing is new to somebody who never saw
the older build. The **disable hints** row on the Workspace tab silences news lines along with the tips.
