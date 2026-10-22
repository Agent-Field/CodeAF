# What I remember

## Do you remember me between conversations? Can you remember my preferences for next time?

Yes. codeaf keeps short lines — a preference, a rule, a correction, a decision,
a lasting fact about you, this machine or this project. **Explicit pins always
ride in their scope.** Other lines are selected by activation: past use,
recency and the current project or topic help decide what rides. A protected
share of the prompt keeps durable rules from being crowded out by progress
updates; specific matches to your message get space before generic topics. A line that
does not ride stays stored and searchable; the Memories tab explains why.

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
a watch's news keeps and forgets nothing — until you type into it — so words
someone else wrote cannot plant a line in every future conversation. Answering
one of its questions after its turn has ended counts the same way, because the
clock can answer one for you: say "remember that", or `/remember` it.

The `remember` tool can label a standing rule, decision, lasting fact or
project progress directly, rather than infer every kind from wording.
Capture instructions keep what you stated or explicitly accepted, preserving
conditions without adding the assistant's suggestions. Wording may be normalized;
this is model guidance, not a verbatim or lossless-capture guarantee.
The Memories tab shows the stored kind.

A line is one sentence, at most 512 characters, and a credential pasted into it
is redacted before it is kept.

Anything with a **time** in it is not a memory: "remind me at six", "every
Monday", "tell me when CI goes red" are automations (see `automations`).


## Always do it this way — make it always do something: a rule like always use tabs, coding style, conventions for this repo

A rule is a memory. "Always use tabs here", "never touch the public API", "our
conventions for this repo: tests beside the file" — keep it with `/remember` or
`/always` (they are the same command), or say it and the model keeps it.

**Lines you keep with `/remember`, `/always`, or the Memories tab are pins:**
they always ride conversations in their scope, even when the ordinary memory
budget is full. Other automatically kept lines compete by activation. Bare
`/always` prints every line kept here (on home, the ones that hold everywhere).

There is no rule for one conversation only.


## Will you keep every condition of a saved rule? What if current facts contradict an old memory?

The memory prompt tells the model to preserve every condition: requirements
stay mandatory, prohibitions stay prohibited, and preferences stay preferred.
A rule requiring both approval and a complete checklist must not make the
checklist optional.

Current user instructions outrank saved lines. Current authoritative evidence,
such as the manual, can correct stale factual claims. Narrow exceptions coexist
with broader rules unless revoked. These are prompt instructions, not a
perfect-recall guarantee; inspect actual admission on the Memories tab.

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

Open it with `alt+8`, the **Memories** word on the tab bar (the fifth word; its number is 8
all the same), or `/memory`. It is
drawn like the Activity tab: groups, each a row of its own in the heading ink
with its fold mark after it, and every line hanging under its group on the
tree's connectors:

- `you, everywhere`, `this machine` and `this project · <name>` — what a
  conversation here can draw on, open from the start;
- each other project, by its name, and `couldn't tell which project` — lines an
  earlier build kept without naming the project — folded;
- `let go` — forgotten and replaced lines, folded, ready to put back.

Two columns, headed on the filter row: **kept by** (`you`, `the model`,
`noticed`, or on a let-go line `forgotten` / `replaced`) and **age**. The head
row counts what is kept, and says `N more in memory` for lines not admitted
to this conversation. Each line starts with `riding` or `archived`; open its pane or card for
the full reason. Admission reflects the conversation's most recent prompt.
With an older engine that cannot report admission, the heading explicitly
says **project working set preview**; that preview can differ from a live turn. `+ remember something`
keeps a new line; what you type is drawn in that row. When more rows exist,
`load more memories` appears above the groups. Press `enter` on it to load
the next 500 active and archived rows; repeat until every row is available.
Filtering searches the rows loaded so far. `memory mix` shows the share of
the actual prompt occupied by project progress, corrections, preferences,
decisions and facts. Open a line to see its kind; an older line reclassified
as project progress also says its original kind, without changing its words.

When codeaf cannot ask which project this conversation is in, the foot says
`couldn't ask which project this is · every project's lines are shown by name`,
no group is `this project`, and the head row counts nothing that doesn't fit.

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
model both read the corrected line. The receipt says `fixed · <line>`; new
words another line already says are refused with `already kept · <words> ·
nothing was changed`.

To change where a line holds instead, `→` offers the moves (see "Where a line
holds").


## How do I make you forget something? Forget a memory, undo forgetting one. Can a forgotten memory come back or be relearned?

On the Memories tab: `→` on the line, then `f forget it` — or `delete`. The
line moves under `let go`, marked `forgotten`; open `let go` with `→`, and
`→` then `u put it back` on the line restores it, whenever you like — unless the
same words are kept again since, which says `already kept · <line> · nothing was
changed` and leaves one line.

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

If you have moved to another conversation or home by then, the line waits and
is drawn in its own conversation when you come back to it.

The tab marks it `noticed`, and `→` then `f` (or `delete`) there forgets it. Most
turns keep nothing. Turn noticing off with the `notice things to remember` row
(see "How do I turn memory off").


## Why did it say superseded, or replaces? Why did my memories get merged?

When something you said corrects a line already kept — "we deploy on Tuesdays
now" against `deploys on Fridays` — noticing replaces the old line with the new
one and says so:

`noticed and remembered · deploys on Tuesdays · replaces deploys on Fridays · for this project · see or forget it on the Memories tab`

The replaced line waits under `let go` on the Memories tab, marked `replaced`:
put it back there, and forget the new one, to undo the change. When the
correction is already kept, the old line is let go and nothing new is written:
`noticed · deploys on Fridays is let go · already kept: deploys on Tuesdays · see
or forget it on the Memories tab`.

Nothing merges or rewrites lines while you are away any more. An earlier build
ran a background tidy that merged and retired lines and said `memory tidied ·
…`; it stopped on 2026-10-14, because a change to what you are remembered by
that you never see is exactly what this memory does not do.


## Does it clean up or delete old memories on its own? Does it know if a memory actually helped?

Memory selection uses activation: applying, confirming or merging a line
strengthens it; merely showing it or finding it in a search does not. A
correction contradicting a remembered line counts against it. Unrehearsed
lines lose activation with age. A low-activation line stays stored;
`archived` in the Memories tab means it is not riding this conversation, not
that it was deleted. Forgotten and replaced lines remain under `let go`.

Near-duplicate writes can merge with an existing line instead of filling the
store with copies. Ephemeral observations are not kept as durable memories.
Local daily maintenance refreshes activation and associations while the clock
is running, with catch-up after reopening. It makes no model call. Semantic
status consolidation and write-time embeddings are not implemented.


## How many lines ride along in every conversation? It said memory is full

The ordinary working set shares a **4,800-rune prompt budget** across your,
this machine's and this project's lines. Memory storage is not capped by that
budget: keeping, editing, moving and restoring a line are no longer refused
because a prompt is full. A line remains at most 512 runes.

Explicit user pins always ride, even when they alone exceed the prompt budget.
After pins, at least 40% of the remaining budget is reserved for durable
corrections, preferences and decisions when they need it. Remaining space
is available to relevant facts, project progress and other durable lines.
Within each group, specific phrases, identifiers and nearby matching
words take priority over generic topic repetition, with activation breaking
ties. A line must still meet the activation threshold. The block says `· N more in memory` for stored lines that do
not ride. Open the Memories tab to see `riding` or `archived` and a reason for
each line; archive does not mean forgotten. Admitted lines stay with the
conversation while its context is unchanged, so adjacent turns do not
randomly swap remembered facts. Short related follow-ups such as “why?” or
“give an example” keep that context. A meaningful topic change or a change to
saved memory can refresh the working set; a greeting alone does not freeze
the selection for the real request that follows.


## Why did an old preference become project progress? Does the upgrade rewrite my memories?

An older build sometimes saved a release update or progress paragraph as a
preference. A local migration derives a corrected kind alongside the original
kind; the saved words, source and original label are preserved. Admission uses
the derived kind so a long progress update does not consume the protected rule
share just because it was once called a preference. The line's card shows both
kinds when they differ. New noticing also distinguishes progress from a
lasting preference before keeping it.


## Can you search memories that are archived or not riding? search_memories

Yes. The `search_memories` tool finds stored active memories in this
conversation's scope, including lines not admitted to its working set. Ask
"what do you remember about the test runner?" to search explicitly. Selection
itself runs locally without a model or embedding call; searching a memory does
not require re-adding it.


## How do I turn memory off? The notice things to remember setting

`/settings` → Memory → **notice things to remember** governs one thing: whether
codeaf keeps lines on its own after your turns. Off, nothing is noticed. It
applies from the next turn, with no restart.

It does not stop memory selection or explicit pins from riding, and it does
not stop `/remember` or the Memories tab. To stop a line, forget it there.

If memory was off before this build, off used to keep your saved lines out of
every prompt; now they ride again. The first conversation after the upgrade says
so once on this machine — `N kept memory lines now ride every conversation they
hold in — memory off now only stops noticing · see or forget them on the
Memories tab` — and nothing about the lines changes.
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
tools) the selected lines still ride and noticing still runs. What a small
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
memories go with it. Deleting it loses lines that no chat kept in its sealed
ledger; a continued chat rebuilds the lines its ledger holds. A line kept for **this machine** travels in the file too, so check that
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

Keeping, forgetting and listing by hand cost nothing: no model is asked. The selected
lines ride in the cached front of requests. Selection runs locally with no
model call; prompt tokens still follow your provider's cache pricing.

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

## Does /remember work on my other computer — memory across computers, memory missing on the new laptop, and what travels with a chat

A memory is kept in the file `~/.codeaf/graph.db` of the computer that made it, and that file is never copied to the relay on its own. With cells enabled, what travels is the memories a **chat** wrote, with that chat. When you `/remember` a line in a conversation (or the model remembers one), the line is also written into that conversation's own ledger, which is sealed and moved with the chat. When you continue the chat on another computer, that computer reads the ledger back and has those lines in its own memory.

So: a line remembered in a chat reaches your other computer **when that chat is continued there**, not before, and not for chats you never move. A memory that belongs to no chat, such as a line you edited by hand in the memory place, stays on the computer where you wrote it. Pairing a second computer does not merge two memories into one. To carry everything to a new computer at once, copy `graph.db` as described under "Where is everything you remember kept", or continue the chats you care about there.

## Memory when cells are off

With `CODEAF_CELLS=0`, memories use the existing local store. A legacy conversation writes no portable `memories.jsonl` sidecar. An enabled cell session keeps the memories it writes in its sealed `.cell/memories.jsonl`, so those memories can follow the chat to another paired computer.
