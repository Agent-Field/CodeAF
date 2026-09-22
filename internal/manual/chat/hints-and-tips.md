# Hints and tips

## What was that tip above the message box — the one-line hint over the rule, the sentence with a bulb

The dim sentence directly above the rule over your message box, led by a bulb and closed
by a small cross — `💡 ctrl+. sees every task this project has run ✕` — is a **tip**: one
line naming a key or a command you have not used yet, and what it does. It reads the way
every hint on this surface does: the key or the command first, then what it does. Home has
the same row over its own box, and the two rows draw from **one list** of thirty-one tips
(below).

**In a conversation the row appears only once you have been quiet for a minute** — no key
pressed and no answer landing for sixty seconds — so it never talks over you while you type
or read what just arrived. The moment you press a key it goes away, and the minute starts
again. Left alone, the row moves on to the next tip every two minutes. On home the row is
there from the first minute, moves on every time you come to home and every two minutes at
rest, and goes blank while the box is being typed into or a list is up.

**The small cross after the tip means ENOUGH OF THESE FOR NOW**: click it and the row goes
blank and stays blank — no second sentence takes its place on the screen you are still
looking at. **The cross is lifted when the row leaves the frame, and by nothing else.** On
home that means leaving home and coming back; in a conversation it means the next key
taking the row away and the next quiet minute bringing it back. Neither the two-minute
beat nor anything else happening on the screen brings it back sooner. The tip you put away
is **not** spent: it keeps its whole allowance, nothing is written down, and the row that
comes back is a different one, with the one you dismissed taking its turn again later.
Until 2026-09-22 the cross counted the tip as shown, so the same sentence was back with
one of its six showings gone. A tip never takes a row of its own: it stands on the blank row that separates the conversation (or home's list) from the
rule, and never blocks a keystroke. The keys row at the very foot — `alt+e effort · alt+a
approvals · / commands` — is not a tip and never changes; until 2026-09-22 the tip stood
there in a conversation, and it moved up to the row over the rule so both boxes say their
tips the same way.

On a Mac the row says `opt` where the table below says `alt`, exactly as the keys row does.

## The dim sentence above the rule on home — the tip on home, what is that line over the box

On home the tip is the dim row **directly above the rule** over the message box — the blank
that separates the list from the rule, with one sentence written into its right end, led
by a bulb: `💡 /ask answers right here without opening a conversation ✕`. It is drawn only
while the box is empty and nothing else is up — a letter in the box, the `/` list, the `@`
list or a reply being read all take the row back — and it moves on to the next tip that is
true for you on every road home (`esc` from a conversation, `/home`, `alt+1`, `tab`), in a
fixed order, round and round. Left at rest, it moves on by itself after two minutes; a home
nobody is looking at (the box being typed into, a list up) does not age, because what has
not been read has not been shown. The cross at its end blanks the row for the rest of that
visit — leaving home and coming back is what brings the next tip — and costs the one you
put away nothing.

## Why did the hint disappear — each tip retires once you use what it teaches

Every tip is earned and then spent. It appears the first time it becomes relevant — the
first task you start, the first long answer, the first time a conversation passes half its
context window, or simply the first time home is open — and it goes away for good the first
time you do the thing it names. Open the task page once and `ctrl+. sees every task this
project has run` never comes back; run `/compact` once and the compact tip is retired. A tip
retired from either box is retired from both: opening the model list on home retires
`/model lists every model` in every conversation as well.

A tip you never act on is not shown forever either. **A showing is a tip that stood for
twenty seconds or more on a row you could see** — home's row while home was in front, a
conversation's row after its quiet minute — and once a tip has been shown six times it is
taken as read and retires by itself. Passing through home for a second or two is not a
showing, however many times you do it, and a row deciding while nobody could see it —
home's while you are in a conversation, a conversation's before its quiet minute — is not
one either. Until 2026-09-22 every visible change of hands counted, so an afternoon of
stepping through home could spend the whole table in flashes nobody read; the first
launch of a build with the twenty-second rule gives back, once, every tip that rule
spent, and leaves retired every tip you retired by using it.

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

Both rows take turns through the one list, in the order below, round and round: every tip
that is true for you gets its turn before any repeats, and a tip that stops being true
stands down at once for the next. Nothing outranks anything — with one exception. **A tip
that has just become true jumps the queue**: when a conversation crosses half its context
window, `/compact summarizes the conversation now` is said next rather than forty minutes
later when the ring comes round. It jumps once and then takes its turn like the rest.

## Every hint codeaf can show, and what makes each one go away

There are thirty-one, one list for both boxes. Each one says the moment it first appears
and the gesture that retires it. The list is the program's own table (the surface refuses to
build if the two disagree), so a tip you saw is on it word for word.

**Starting work**

- `/compact summarizes the conversation now` — when the conversation passes half its
  context window. Retired when a `/compact` finishes.
- `/cost says what this conversation has spent` — once the conversation has spent about
  ten cents. Retired when you run `/cost`.
- `ctrl+. sees every task this project has run` — after the first task starts. Retired
  when you open the task page, by `ctrl+.` or `/history`.
- `/rewind takes back an earlier message` — after an answer of about 1,500 characters or
  more. Retired the first time a rewind lands.
- `/files finds everything made for you` — after the first export writes a file. Retired
  when you run `/files`.
- `/resume opens an earlier conversation` — when you start in a directory that already has
  a conversation. Retired when you run `/resume`.
- `/standing keeps something always true` — once this directory has three or more earlier
  conversations. Retired when a standing order is made or the standing page opened.
- `/ask answers right here without opening a conversation` — on home, whenever home's ask
  door is there (it is the one tip that is only true on home). Retired the first time
  `/ask` or `alt+enter` sends something from home.
- `/task starts work you can walk away from` — after the first exchange. Retired when
  `/task` is typed, bare or with a brief.
- `ctrl+enter sends your message as something to keep true` — retired when a standing
  order is made or the standing page opened.
- `/manual answers any question about codeaf from its own manual` — retired when
  `/manual` is typed, bare or with a question.
- `ctrl+shift+t reopens the last closed conversation tab` — retired the first time the chord is
  pressed, on a terminal that can send it.

**Files and context**

- `@ completes a file, a folder or a task into your message` — retired when the `@` list
  opens.
- `/attach sends a file along with your message` — retired when a file goes on the tray by
  path or the file browser opens.
- `/project sets the folder the next conversation opens in` — on home only, since that is
  the only screen `/project` works on. Retired when `/project` takes a folder, by a path
  after it or on the browser it opens.
- `/folder picks the folder codeaf works in` — retired when the folder chooser opens, from
  a conversation or aimed at home's target.
- `/attach takes a picture too, or paste a screenshot in` — retired by the same gesture as
  the other `/attach` tip.
- `/export writes this whole conversation to a file` — after two exchanges. Retired when
  an export lands.

**Models, thinking and cost**

- `/model lists every model, /model <slug> switches at once` — retired when the model list
  opens, over a conversation or over home's draft.
- `/crew sets the models codeaf uses on its own behalf` — retired when `/crew` answers,
  bare or with a preset.
- `/budget caps what today may cost` — retired when `/budget` answers.
- `alt+3 shows what this machine has spent, by the day` — retired when the spend place
  opens by any door.

**Steering a running answer**

- `enter while an answer is coming stops it and steers` — after the first exchange.
  Retired the first time you steer.
- `ctrl+q queues this message for after the current turn` — after the first exchange.
  Retired the first time you queue one.

**Moving around**

- `ctrl+t starts a fresh chat in this folder` — retired when the new-chat page opens.
- `alt+1 to alt+7 jump straight to a place` — retired the first time a place chord reaches
  one.

**Memory, accounts and the rest**

- `/remember keeps one thing across conversations` — retired when `/remember` is typed.
- `/search finds anything ever said on this machine` — retired when the search place opens
  by any door.
- `/subharness lists the programs you can run` — retired when `/subharness` is typed, bare
  or with a name.
- `/connect links Google, Slack or another model service` — retired when the connect panel
  is reached for.
- `/autonomy sets how questions are handled while you are away` — after the first
  exchange. Retired when `/autonomy` is typed, bare or with a rule. (It took the seat
  `ctrl+b freezes the screen so you can read and copy from it` held for one build on
  2026-09-22, and `ask for a picture, a voiceover, music or a video` before that.)

Unless a line above says otherwise, a tip is true from the first minute on home and after
the first exchange in a conversation.

`/ shows every command` used to be one of these. It is gone because both keys rows now say
`/ commands` outright, so there was nothing left to teach. Three more were cut on
2026-09-22: an `alt+enter` tip that promised a task where the chord asks, a `ctrl+r` tip
for a chord that works only in a conversation and only over a making-shaped sentence, and
`ask for a picture, a voiceover, music or a video`, whose seat the copy-mode tip took.

## Turn off hints — stop showing tips, disable the hints, the disable hints row

Open the settings panel with `/settings` (or `ctrl+,`), go to the **Workspace** tab, and flip
the **disable hints** row on. Enter or space toggles it; it is off by default, which means
the tips show. (Until 2026-09-22 it was a **hints** row on the Display tab, on by default.)
The change lands at the end of the next turn. On silences the tips — over a conversation's
box and over home's alike — and the what's-new lines together; it does not touch the keys
row's own words for a live state — `ctrl+c interrupt` and the rest are not hints and cannot
be turned off. From the terminal, `codeaf config` shows the same row under the same name.

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
