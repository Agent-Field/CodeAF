# What I remember

## Do you remember me between conversations? Can you remember my preferences for next time?

Yes. codeaf keeps short lines — a preference, a rule, a correction, a decision,
a lasting fact about you, this machine or this project — and **every line kept
for where you are rides in front of every conversation there**, every turn,
until it is forgotten. There is no lookup deciding whether a line is relevant:
what is kept is what the model is told.

Lines get kept three ways, and all three tell you:

- you keep one: `/remember <text>` (or `/always <text>`, the same thing);
- you ask in words — "remember that I deploy on Fridays" — and the model keeps it;
- codeaf notices something durable you said and keeps it, saying
  `noticed and remembered · <line> · <where> · see or forget it on the Memories tab`.

The commands:

- `/memory` or `/memories` — open the Memories tab (`alt+8`).
- `/memory <words>` or `/memories <words>` — print the lines those words name.
- `/remember <text>` — keep one line.

There is no `/forget`: a line is forgotten on the Memories tab, where you can see
it, or by asking — "forget that I use npm".

The words themselves are not memory: everything said in every conversation on
this machine is also kept and can be **searched** (see "What did we decide last
week" below).


## Can you remember this for me? /remember, remember that, and the remember tool

Yes. Typed or said, it is kept straight away — no model decides whether to keep
it. `/remember <text>` answers with exactly what was kept and where it holds:

`remembered · uses pnpm, not npm · for this project`

Typed on home, it holds for you everywhere instead. The same words twice say
`already remembered · …` and keep one line. Said in words ("remember that…"),
the model uses its `remember` tool and you see
`remembered · <line> · <where> · see or forget it on the Memories tab`. The
model can also drop a line you ask it to with its `forget` tool, and says
`forgot · <line> · put it back on the Memories tab`. It does either only on a
turn **you typed**: a turn opened by a task's report, a team member's message or
a watch's news keeps and forgets nothing, so words someone else wrote cannot plant
a line in every future conversation.

A line is one sentence, at most 512 characters, and a credential pasted into it
is redacted before it is kept.

Anything with a **time** in it is not a memory: "remind me at six", "every
Monday", "tell me when CI goes red" are automations (see `automations`).


## Always do it this way — make it always do something: a rule like always use tabs, coding style, conventions for this repo

A rule is a memory. "Always use tabs here", "never touch the public API", "our
conventions for this repo: tests beside the file" — keep it with `/remember` or
`/always` (they are the same command), or say it and the model keeps it.

**Every memory already rides every conversation**, so there is no separate
"rule" strength and no switch between "always" and "only when it matters": a
line is in front of every conversation in its scope, or it is forgotten. Bare
`/always` prints every line kept here (on home, the ones that hold everywhere).

There is no rule for one conversation only.


## Is that an instruction or a rule? How it knows you mean always, how to make it permanent, and when it didn't notice a rule and did it once

A sentence with no time in it that you want to hold from now on is a memory;
something you want done once is just done; something tied to a time or an event
is an automation.

When it did something once and you meant it to hold — it didn't notice this was
a rule — make it permanent: say "remember that", or type `/remember <the rule>`.
When it kept something you only meant for now, forget it on the Memories tab, or
say "forget that". Noticing reads every turn you type, so a rule you state plainly ("always…",
"never…", "from now on…") is usually kept and says so.


## Where a line holds — you everywhere, this machine, or this project? Does this rule apply to all my projects? Not in this project

Every line holds in one of three places:

- **for you everywhere** — every conversation, every project;
- **for this machine** — every conversation on this machine;
- **for this project** — every conversation in this project, and nowhere else.

`/remember` typed in a conversation keeps the line **for this project**; typed on
home, **for you everywhere**. The model keeps a line for this project unless you
said it holds everywhere or only on this machine. A conversation with no project
of its own (home, or a folder codeaf could not identify) keeps a project line
for you everywhere and says so: `… · for you everywhere, since this conversation
has no project of its own`.

Can I change everywhere to just this project? Yes: on the Memories tab, `→` on the
line offers `hold it everywhere`, `hold it in this project` and `hold it on this
machine` — whichever it is not. There is no "everywhere except here": move the
line to the projects it should hold in instead.

A project is known by its git `origin` (so every clone and worktree of one
repository is one project), or by the folder when there is no remote.


## How do I see what codeaf remembers about me? The Memories tab (it was the Memory tab)

Open it with `alt+8`, the **Memories** word on the tab bar, or `/memory`. It is
drawn like the Activity tab: groups, each a row of its own in the heading ink
with its fold mark after it, and every line hanging under its group on the
tree's connectors:

- `you, everywhere`, `this machine` and `this project · <name>` — what a
  conversation here is told, open from the start;
- each other project, by its name, and `couldn't tell which project` — lines an
  earlier build kept without naming the project — folded;
- `let go` — forgotten and replaced lines, folded, ready to put back.

Two columns, headed on the filter row: **kept by** (`you`, `the model`,
`noticed`, or on a let-go line `forgotten` / `replaced`) and **age**. The head
row counts what is kept, and says `N don't fit in front of the model · forget
some` when a conversation here has more lines than fit. `+ remember something`
keeps a new line; what you type is drawn in that row.

On a frame 110 columns or wider a pane beside the list reads out the row under
the cursor: the whole line, where it holds, who kept it, and where it was
learned.


## Open and fold a group on the Memories tab — the arrow keys, enter, and the keys

- `→` on a group opens it; `←` folds it back up. `enter` does not open groups.
- `enter` on a line opens its card; on `+ remember something`, starts a line.
- `→` on a line opens its verbs (fix, forget, move, ask, put back).
- Typing filters every group at once; `esc` clears the filter, then leaves.
- `delete` forgets the line under the cursor. A click on a group opens or folds it.

Every change the tab makes happens beside the screen, so a slow engine never
freezes the keyboard, and its receipt appears on the line at the foot.


## What the Memories tab shows when there is nothing in it

On a machine that has kept nothing yet, the Memories tab draws its heading and
one line — `the lines codeaf keeps in front of every conversation · /remember
adds one` — and its foot offers only the way out. There is no `0 kept`: nothing
kept is drawn as nothing. Once there is a first line, the groups and the
`+ remember something` row appear.


## How do I edit a memory? Fix the wording

On the Memories tab, put the cursor on the line and press `→`, then `e fix the
wording` — or open its card with `enter` and press `→` then `e`. The line's
words load into its own row, where you edit them; `enter` saves and `esc`
cancels. The line keeps where
it holds and who kept it; its title follows the new words, so the list and the
model both read the corrected line. The receipt says `fixed · <line>`.

To change where a line holds instead, `→` offers the moves (see "Where a line
holds").


## How do I make you forget something? Forget a memory, undo forgetting one. Can a forgotten memory come back or be relearned?

On the Memories tab: `→` on the line, then `f forget it` — or `delete`. The
line moves under `let go`, marked `forgotten`; open `let go` with `→`, and
`→` then `u put it back` on the line restores it, whenever you like.

Or ask in words — "forget that I use npm". The model's `forget` drops the
**one** line those words name and says `forgot · <line> · put it back on the
Memories tab`. When the words name more than one line it forgets nothing and
lists them; when they name none it says `nothing remembered matches "<words>"`.

There is no `/forget` command (there was one until 2026-10-14) and no undo key:
every change — kept, noticed, replaced, forgotten — is a line on the tab you can
forget or put back.

Nothing is ever deleted outright, so a forgotten line comes back only if you put
it back or keep the same words again. Searching an old conversation does not
bring a forgotten line back or re-activate it: search is reading, not
remembering. Noticing only reads turns you type from now on.


## ask me about it — what enter does on a memory line, and talking about something you remember

`enter` on a line opens its card: all of its words, where it holds, who kept it,
and when. The verbs on the line's `→` strip include `a ask me about it`, which
starts a new conversation that opens with `about something you remember: <the
line>` — the place to ask why it was kept, whether it still holds, or what it
means for something you are doing. `esc` on a card returns to the list.


## Where did a memory come from, and who says so?

The card says who kept the line — `kept by you`, `kept by the model` or `kept by
noticed` (the pass after a turn) — how long ago, and the conversation it was
learned in: `2h ago · in 'Editor setup'`. When that conversation is on this
machine, `→` then `o open where it was learned` opens it. A line kept by an
earlier build may not know its conversation, and then the card simply says less.


## How does something get remembered without me asking? Noticing

After each turn **you typed**, one small call reads what you said and the answer
and may keep **one** line: a preference, a standing rule, a correction, a
decision, or a lasting fact. It is shown what is already kept, so it does not
keep the same thing twice. It never keeps something only the assistant said, the
work in progress, code, or anything with a time in it — and never anything from a
task's report, a watch's news or a team's message, because you did not type
those. An `ask here` exchange from home is not noticed either.

Every time it keeps a line it says so in the conversation:

`noticed and remembered · wants the change made directly · for you everywhere · see or forget it on the Memories tab`

The tab marks it `noticed`, and `→` then `f` (or `delete`) there forgets it. Most
turns keep nothing. Turn noticing off with the `notice things to remember` row
(see "How do I turn memory off").


## Why did it say superseded, or replaces? Why did my memories get merged?

When something you said corrects a line already kept — "we deploy on Tuesdays
now" against `deploys on Fridays` — noticing replaces the old line with the new
one and says so:

`noticed and remembered · deploys on Tuesdays · replaces deploys on Fridays · for this project · see or forget it on the Memories tab`

The replaced line waits under `let go` on the Memories tab, marked `replaced`:
put it back there, and forget the new one, to undo the change.

Nothing merges or rewrites lines while you are away any more. An earlier build
ran a background tidy that merged and retired lines and said `memory tidied ·
…`; it stopped on 2026-10-14, because a change to what you are remembered by
that you never see is exactly what this memory does not do.


## Does it clean up or delete old memories on its own? Does it know if a memory actually helped?

No and no. A line holds until somebody forgets it — there is no expiry, no
decay by age, and no background tidy. Every line weighs the same, so there is no
ranking and no count of how often a line helped: the Memories tab shows who kept
a line and when, not a score. Nothing is ever deleted outright either; a
forgotten or replaced line waits under `let go`.


## How many lines ride along in every conversation? It said memory is full

The lines a conversation here is told — yours everywhere, this machine's and
this project's — share about 4,800 characters, roughly fifty ordinary lines.
They ride in the cached front of every request, so they cost little once sent,
and they only change when somebody keeps or forgets a line.

Anything that would go past that in **any** conversation is refused: keeping a
line, rewording one, moving one, or putting one back. A line for you everywhere
or for this machine rides in every project's conversations, so it is refused when
any project's would overflow, and the receipt names that project:

`memory is full here · forget a line on the Memories tab to make room`
`memory is full for infra · forget a line on the Memories tab to make room`

Noticing says `noticed something worth keeping, but memory is full here · <line>`
instead of keeping it.

A store an earlier build filled with no bound can hold more than fits. Then what
rides is chosen: lines that build kept as rules first, then this project's, then
this machine's, then yours everywhere, newest first. The conversation says once,
`N remembered lines don't fit in front of the model here · forget some on the
Memories tab`, and the tab's head row counts them.


## How do I turn memory off? The notice things to remember setting

`/settings` → Memory → **notice things to remember** governs one thing: whether
codeaf keeps lines on its own after your turns. Off, nothing is noticed. It
applies from the next turn, with no restart.

It does not stop the lines you kept from riding every conversation, and it does
not stop `/remember` or the Memories tab. To stop a line, forget it there.
Saved lines are never deleted by the setting.

Conversation search and the bash fix hints are separate and keep working either
way (see their sections below).


## It said memory is off, or memory is unavailable in this conversation

Earlier builds said `memory is off for this session · turn it on under /settings`
whenever a conversation had no memory — including on a small model with the
setting on, which was wrong. That message is gone: memory is not a setting that
turns off, and a small model carries your lines like any other.

What you can see now, rarely, is
`memory is unavailable in this conversation · the store could not be opened`.
It means `~/.codeaf/graph.db` could not be opened when codeaf started, and the
terminal said why: `memory is unavailable in this session: <reason>` — for
example `could not open …/graph.db: permission denied`, or SQLite's `out of
memory (14)`, which is its code for "cannot open the file". Fix the reason (a
full disk, a permission, a file moved away) and start codeaf again. Your lines
are in the file; nothing was lost.


## It said no such column: owner

That sentence came from an older codeaf reading a store a newer one had already
upgraded, or the reverse: a mismatch between the file and the program, not
damage to the file. The newer codeaf repairs the file as it opens it, keeping
every line's words, where it came from and whether it was active. A line an old
store kept as "project" without naming the project is kept under the Memories
tab's `couldn't tell which project` group until you move it.

A window held by an engine keeps that engine until a newer build replaces it.
Update codeaf on the machine holding the workspace and reconnect; on that same
machine a newer build replaces the older engine as the window connects. For an
engine on another machine, update codeaf there too, then run
`codeaf engine --stop --workspace <that folder>` there when its work is safe and
reconnect. `codeaf engine --status` names the engine.


## Does memory work on the ordinary launch, over --host, and on a smaller model?

Yes. Plain `codeaf` and `--host devbox` both use the connected conversation's
memory: the commands, the Memories tab and the receipts all reach that machine's
store. `--no-host` uses this machine's store directly.

On a small model (a context window under about 32,000 tokens, or a model with no
tools) your lines still ride every turn and noticing still runs. What a small
model gives up is its own two hands: `remember` and `forget` are left off its
belt to save room, so it tells you it cannot keep a line itself and that you can
`/remember` it.


## Can I keep typing while a memory command waits? Remember has not answered yet

Yes. `/remember` and `/memories <words>` wait beside the screen, in
the order you sent them, one conversation at a time. Their receipts stay with
the conversation that issued them; a command typed on home answers on home's own
message line. Closing a window cancels commands still waiting behind another,
but does not undo a save the engine already received.

On the engine road a save can take longer than the connection waits for its
receipt, and then `/remember` says:

`saving that has not answered yet · check /memory before trying again`

That is not a failure: the engine may still be saving, and codeaf does not retry
on its own. Check the Memories tab before sending it again.


## Where is everything you remember kept? Where is my memory file on disk? Can I copy my memories to another machine?

In `~/.codeaf/graph.db`, one file, made the first time codeaf runs. It holds
every remembered line, every conversation this machine has had, and the work it
has run. One file holding every project's lines is not every project seeing
them: a line for one project is only ever put in front of that project's
conversations.

`CODEAF_HOME` moves the whole folder. Copy the file to another machine and your
memories go with it; delete it and they are gone, because nothing keeps a second
copy. A line kept for **this machine** travels in the file too, so check that
group after copying.


## Does a task, an automation or a team member see my memory? Is memory shared with my team?

They read the same lines a conversation in that project would carry:

- a **task** — every worker's brief closes on the conversation's memory section,
  read-only; a task never keeps or forgets a line;
- an **automation's run** reads the lines kept for its place before it acts, and
  never writes — nobody is there to see a receipt;
- a **team's manager and members** run in the same workspace, so the project's
  lines reach them as they reach any conversation there.

There is no team group. A line belongs to you, one project, or this machine;
nothing shares a line with another person's account.


## What does remembering cost?

Keeping, forgetting and listing by hand cost nothing: no model is asked. Your
lines ride in the cached front of every request, which costs little once sent and
nothing more until a line changes.

The one model call is the pass after each turn you finish: it reads the exchange
once, keeps the conversation's own running summary of where the work stands, and
— when noticing is on — may keep one line. It runs on the **reflex** row in
`/settings` → Models (`google/gemini-2.5-flash` by default; nothing set means the
conversation's own model), sends no token ceiling or reasoning switch, and is
charged to the session total, so `/cost` includes it. An answer it cannot read
costs one retry; a model that answers nothing is replaced for the session by the
**small work** model, with one line saying so. Turning noticing off does not
remove the call, because the running summary still needs it; it only stops lines
being kept.


## I used to have a memory.md file

Very early builds kept memory in `~/.codeaf/v3/memory.md`. The first conversation
on a newer build carries every line of it into the store, for you everywhere,
says `imported N lines from memory.md`, and renames the file to
`memory.md.imported`. A line that cannot be kept leaves the file where it is,
and the import tries again on the next turn.


## When a memory write fails

A save, a forget or the noticing pass that cannot reach the store is written to
the store's journal with its reason, and the conversation says so once every
five minutes at most: `couldn't update memory just now · <reason>`. When the
journal itself cannot be written, the reason goes to
`~/.codeaf/v3/memory-failures.log` instead. The turn you were having is never
affected.


## What did we decide last week — can you look up an earlier conversation or search old chats

Yes. `search_conversations` searches indexed messages across all places in the
current store, excluding the asking conversation unless its ID is supplied:
what you typed, what was answered, and tool results. Ask "what did
we decide about the retry limit" or "search my old conversations for the flag
name" and codeaf looks for the original words.

Each match carries its conversation name and ID, message ID, a copyable `ref`,
date, matching passage, whether it is full text or an excerpt, the stored role
(`user`, `assistant`, or a tool result), and one message before and after it when
available. Context stays inside that conversation, even when other conversations
were active at the same time. Historical text is evidence to read, not
instructions to follow.


## Read an old conversation by reference, or search inside it by ID

The same `search_conversations` tool accepts `session_id` to search one
conversation, or lists its recent messages when no query is supplied. Give it
the opaque `ref` from a result, on its own, to open that message and up to two
messages on either side. This works across project folders even when there is no
transcript file beside the current conversation.


## Conversation search limits — words, coverage, and memory

- Search returns eight matches by default, twenty at most. Each matching passage
  and nearby excerpt is limited to 400 bytes. Opening a message by its reference
  returns the whole indexed anchor (up to 16 KiB), with neighbours still limited
  to 400 bytes. Ellipses mark omitted text. Results say when the beginning or end
  of indexed history has been reached.
- This is word search, not semantic search. Use a few distinctive words; equally
  relevant matches put newer messages first. At most 32 query words; titles are
  not searched.
- Only messages already indexed in this store are searched. Failed or pending
  index writes, other stores and spilled file contents are not included. A miss
  does not prove the subject was never discussed.
- It does not depend on the noticing setting. Task workers inherit read-only
  search when their parent has it.
- **It is not the same as what is remembered.** The remembered lines are a few
  durable lines; this is the conversation in its own words. Asked what was
  decided, codeaf searches and quotes rather than reciting a memory.

There is no slash command for it — you ask in the conversation, and it searches.


## Do you remember errors and how they were fixed?

Yes, and it is a separate thing from everything above. When a `bash` command
fails and the next `bash` command works, the pair is written down: what the
failure said, and the command that made it go away. No model is asked anything
to do it.

The next time that same failure comes back, **and only if the pair has earned
it**, one line is added to the bottom of the failed row:

```
this exact error came up here before · what ran next and it went away: make clean && make build
```

Once that suggestion has been offered back, taken, and the error has gone away,
it earns the stronger line:

```
this exact error was fixed 7/8 times before · what worked: make clean && make build
```

7 is how many times the command was handed back and worked; 8 is how many times
it was handed back at all. Only **one** suggestion is ever offered — the one with
the best record — and nothing is said when the record is worse than three tries
in five. It only happens **after** something failed, and only for `bash`.


## When does it say nothing, and why did a suggestion stop appearing?

Two things have to be true before anything is added to a failed row:

- **the suggestion has to be a command this machine can run** — a regex, a path,
  a sentence or a program that is not installed is never offered;
- **the pairing has to have been seen more than once**, or offered, taken and
  watched to clear the error.

An earlier build offered a pair the first time it saw one, and answered
`npm error Missing script: build` with `git log --oneline -5`. A wrong suggestion
costs a step chasing it, which is worse than silence.


## What it refuses to learn from — a refusal, a missing tool, a missing program

Three kinds of failure are deliberately **not** written down, because nothing the
next command did could have fixed them: codeaf itself saying no (a permission you
denied, a tool a task does not have), `Unknown tool: …`, and a program that is
not on this machine (`git: command not found`). A refused call is also never
answered with a suggestion.


## Remembering a failed method or tool — will codeaf avoid trying that tool again, and what if the inputs change?

No. codeaf does not remember which methods or tools failed, and nothing it keeps
bans a tool or a command. An earlier build kept failed attempts and showed them
to the model as advice before a turn; that was removed on 2026-10-14, with the
rest of the memory nobody could see. When the inputs have changed, the agent
simply runs, reads or adapts as the work needs.

The one thing kept about failures is the bash fix hint below: a single line
added under a failed `bash` row when the same error comes back, suggesting what
worked before. It is advice after the fact and never stops a command running.


## What happens to old command results when input files change?

No earlier result is carried into today's work. A command that failed before is
not remembered as a failure, so a changed data file needs no forgetting: the
agent reads the file and runs what it needs now. The only trace is the bash fix
hint, which appears only if the same error text comes back, and only as a
suggestion.


## Why did it say this error was fixed before, and how did it know?

Because it watched it happen here, on this machine. Two files hold it, both
called `fixes.json`: one beside this project's conversations, asked first, and
one at the top of codeaf's own folder. Tasks write into the same project file.
`CODEAF_HOME` moves both.

The counts are **halved every week**, so something confirmed once months ago
drops out and something confirmed this morning rises. A suggestion that fails on
the same error is counted against itself. There is no command or panel for it,
it is not on the Memories tab, and the noticing setting does not turn it off: the
one line on a failed row is the whole of what it says.
