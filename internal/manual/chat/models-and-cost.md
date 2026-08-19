# Models, context, and what it costs

## Which model am I talking to, which model is it using right now, and how do I switch or change it

The model in use is written in the status line. There are two doors to the picker:

- type `/model` with nothing after it, or
- press the model's name in the status line.

If you have turned the mouse off (`ui.mouse`), only the command works. Inside a task room
the name in the status line is inert — the picker moves the conversation's model, not the
node's.

The picker is a filter box in the input line's place with a short list of models under it.
It is bottom-anchored: the conversation shrinks above it, so nothing pops up over what you
were reading.

Choosing a model does four things: the model is set on the session, the surface learns
that model's context window and tells the session (compaction fires at a fraction of the
window, so this is not decoration), a note appears reading `model · <model>`, and the
choice is written into your profile.

## Does aforge remember the model I picked, or does it go back to the default?

**It remembers.** The model you last switched to is the model the next `aforge` opens on,
whether you chose it in the picker, typed `/model <slug>`, or set the conversation row in
`/settings` — all three are the same road.

The order a launch resolves is: `--model <slug>` on the command line beats everything for
that session alone; then the model you last chose; then `AFORGE_MODEL`; then the built-in
default. `AFORGE_MODEL` seeds a model for somebody who has never chosen one and does not
override somebody who has — which is why the settings row stays editable while it is set.

Two things do not persist. Over `--host` the switch takes for the session and is not
written anywhere: the model a remote session opens on is resolved on that machine, from
that machine's profile. And reasoning effort is kept per model for the session, not
written to the profile.

esc leaves the picker and changes **nothing** — your half-typed draft, the model in use and
the frame all come back exactly as they were. The picker holds its own filter text, and the
filter is forgotten when it closes.

## Moving and filtering in the model picker

Type to filter. The keys:

| Key | What it does |
|---|---|
| ↑ / ctrl+p, ↓ / ctrl+n | move a row |
| pgup / pgdown | move 12 rows |
| left, right, home, end, ctrl+u, ctrl+w | edit the filter text |
| ctrl+t | walk the reasoning effort of the model under the cursor |
| enter | switch to the row under the cursor |
| esc | cancel, changing nothing |

The cursor opens **on the model in use**, which is also the marked row, so enter with
nothing typed confirms rather than changes.

Filtering splits what you type on whitespace; every token must match, each in one of three
tiers — prefix, then substring, then subsequence. So `ds v4` finds
`deepseek/deepseek-v4-flash` and `claude 4.5` finds `anthropic/claude-sonnet-4.5`, and fuzzy
hits sit at the bottom rather than mixed through. Twelve rows show at a time.

The picker **never fetches**. The list comes from what is already known, in this order: the
catalog the door passed in, then `~/.aforge/v3/models.json`, then five names this build
remembers (`deepseek/deepseek-v4-flash`, `openai/gpt-4.1-mini`,
`anthropic/claude-sonnet-4.5`, `google/gemini-2.5-flash`, `moonshotai/kimi-k3`). Each rung is
tried only when the one above it came back empty after filtering.

The placeholder in the empty filter box is the only place the overlay explains itself:
`filter · ↑↓ · ctrl+t effort · enter switch · esc cancel`

There is no mouse commit on the picker's rows.

## What each row in the model picker tells you

A row reads `<id>:<level>` on the left and, dimly on the right, the facts the catalog
published: the context window, the price per million prompt and completion tokens, the
arena elo, and what the model can do besides write. **Each part is hidden when nobody
published it.** A price shows only when both halves are known — a zero means "nobody said",
never "free".

Only models you can hold a conversation with are listed: text in, text out. A model that
publishes `["image","text"]` out (a drawing model that captions) is excluded, and so is a
transcription model (audio in, text out). A row that publishes nothing about itself is judged
by its id against a narrow list of generation and sidecar words.

## What "sees", "draws", "speaks", "films", "hears" mean on a model row

The dim tail of a picker row ends with what the model can do besides hold a conversation,
in one word each:

| Word | What the catalog published |
|---|---|
| `sees` | it reads images |
| `hears` | it reads sound |
| `watches` | it reads video |
| `draws` | it answers with images |
| `speaks` | it answers with speech, audio or music |
| `films` | it answers with video |

Input words come first, so a model that reads and paints pictures reads `sees · draws`.

**A plain text chat model shows nothing here at all**, and neither does a model that
published no modalities — silence means text in, text out, and nothing more. The same tail
appears on `aforge models`.

## Switching model by name in one command

`/model <slug>` switches straight to that slug — no list, no confirmation.

There is one check, and only one. If the slug **is** in the catalog and cannot hold a
conversation — a drawing model, a speech model, a transcriber — aforge refuses in one line
and the conversation does not move:

```
openai/gpt-4o-mini-tts cannot hold a conversation — it speaks. Still on moonshotai/kimi-k3.
```

A slug the catalog has never carried is still **taken at its word**, exactly as before:
aforge may be offline, or you may be naming a model this build has never listed. In that
case the context window is left alone.

## The crew — which models aforge uses on my behalf, and /crew

aforge makes calls you did not type: naming a session, the summary a compaction keeps, the
safety gate, the check on finished task work, the planner of an adaptive run and the nodes
under it, the designer of a saved harness page, looking at an image. Each of those is a
**role**, and every role sits on one of four **classes** — the **crew** — which you set in
`/settings` → Providers, or in one word with `/crew`:

- **reflex** — near-free · reads every turn — memory, titles, safety.
- **small work** — cheap · does the bulk work — run nodes, digests.
- **careful work** — careful · checks what must not be wrong — audits, compaction, vision.
- **mastermind** — thinks · plans runs and designs harnesses.

**All four arrive with a model already in them**, and the four together are the `balanced`
preset:

| class | as shipped |
| --- | --- |
| reflex | `nex-agi/nex-n2-mini` |
| small work | `deepseek/deepseek-v4-flash` |
| careful work | `qwen/qwen3.8-27b` |
| mastermind | `moonshotai/kimi-k3:low` |

They are all open-source models, and none of them is the model you are talking to. A crew
that followed your conversation would put the most expensive model in the build on the
cheapest questions in it — a call made twice every turn on a frontier model is a bill nobody
agreed to.

The careful class's `qwen/qwen3.8-27b` is vision-capable, so the shipped crew can fund the
vision role as well as text checks. Its OpenRouter rates are $0.45/M input tokens and
$3.20/M output tokens, with a 262k-token context window.

**Clearing a row is still an answer.** A class you empty on purpose reads
`follows the conversation`, and every role on it runs on the model you are talking to. That
is the only way to say "use my model for this", and it is deliberately something you have to
say rather than the default.

### The three presets

| | frugal | balanced | max |
| --- | --- | --- | --- |
| reflex | `nex-n2-mini` | `nex-n2-mini` | `nex-n2-mini` |
| small work | `deepseek-v4-flash` | `deepseek-v4-flash` | `deepseek-v4-pro` |
| careful work | `qwen3.8-27b` | `qwen3.8-27b` | `kimi-k3` |
| mastermind | `qwen3.8-27b` | `kimi-k3:low` | `kimi-k3:high` |

- **frugal** — qwen handles careful work · pennies a day
- **balanced** — kimi-k3 thinks, qwen checks
- **max** — kimi-k3 everywhere, thinks longer

`/crew` opens all three as a chooser with yours marked. ↑ / ctrl+p and ↓ / ctrl+n move;
enter applies and esc cancels. If the four classes make a custom crew, no row is marked and
the footer says picking one puts all four back. `/crew max` still sets it directly and
confirms in one line.
The **crew** row in `/settings` → Providers is the same thing: enter or space walks it
frugal → balanced → max.

**The crew row is not stored — it is worked out from the four.** Answer any one of the four
rows yourself and the crew row reads `custom`, because that is what is true. `/crew balanced`
puts all four back in one write.

### The roles under each class

Directly under the **pinned roles** row the panel lists **every registered role**, grouped
under the class answering it, saying which model comes out. As shipped:

| role | class | what it is |
| --- | --- | --- |
| `reflex` | reflex | reads every turn for memory — routing and keeping |
| `title` | small work | the name a session gives itself |
| `worker` | small work | one node of an adaptive run |
| `guardian` | small work | is this one tool call plainly safe |
| `router` | small work | which surface a request belongs to |
| `compaction` | careful work | the summary that survives a compaction |
| `auditor` | careful work | whether finished-looking work is actually finished |
| `vision` | careful work | reads images for a model that cannot see them |
| `planner` | mastermind | the plan that steers an adaptive run |
| `designer` | mastermind | writes and reviews a harness page |

The list is built from what is registered in the running binary, so it is the truth about
this build rather than a table someone kept up to date. Stop on a row and the line under the
list is that role's own description followed by which class it follows.

**`planner` and `designer` are the mastermind's two roles**, and they used to sit on careful
work beside the compaction summary — which made one model id answer two unrelated bills.
The careful calls are many and short; these two are few, and each one decides what all the
other calls do. A planner that cuts badly spends a whole run on work nobody wanted; a
designer that writes badly puts a wrong answer on the menu with a name on it.

## Asking a class to think harder — a level on a class value

A class value may carry a thinking level as well as a model:

```
moonshotai/kimi-k3:high
```

`:low`, `:medium` and `:high` are the three, and the shipped **mastermind** carries `:low`.
The level is not part of the model id — it travels as its own request option, exactly as
the picker's **ctrl+t** effort does — so the id sent to the provider is
`moonshotai/kimi-k3` and the thinking is asked for separately.

- **Any of the four class rows takes one**, though the mastermind is the one it is for.
- **Any other suffix is refused**, in words: *"off" is not a thinking level. Add `low`, `medium`,
  `high` to a model id, or leave the level off*. It is a different request shape — it asks the
  provider to suppress thinking outright — and some endpoints refuse it. `:max`, `:none`,
  `:xhigh` and the other near-misses are refused the same way.
- Where a level is set, the role rows print it after the id, `kimi-k3:low`, which is the
  same notation the model picker and `/status` use.

The **mastermind** row is a text box rather than a picker for exactly this reason: a picker
hands back a bare id, and this row's value may be an id with an instruction on it.

One thing about `reflex`: it is the only role called **twice on every message** — once
before, to pick which remembered lines belong in this one, and once after, to decide
whether the exchange held anything worth keeping (what-i-remember). That is why it has a
class of its own rather than sharing "small work", and why it is the one row where a large
model is an expensive mistake rather than a preference. Both calls are folded into the
session's total, not into the message that triggered them, so `/cost` includes them
without any one message reading as three times the price of its neighbours.

One caveat on `vision`: the **looking** row further down the Providers tab is the front door
for which model sees, and it wins over this role's class. The `vision` pin is the second rung
of that ladder — set the looking row for the ordinary case, and pin the role only when
you want a pin that also binds the older surfaces.

## Which model is planning my adaptive run — what `planner:` means on that page

An adaptive run's page pins one line at the top, and it names the model doing the thinking
next to the money it is spending:

```
◐ main ▸ ship the parser fix · planner: kimi-k3 · $0.87 / $2.00 · working
```

`planner: <model>` is the model amending the plan after every node — resolved once when the
run started, from the model you named in the sentence, then the `planner` role's pin, then
the **mastermind** class, then the model you are talking to. Since the mastermind ships with
a model in it, this is usually *not* the model in the rest of this conversation, which is why
the run's own page says it rather than leaving you to work it out.

It never changes while a run is going: the ladder is walked once, at the start.

On a narrow screen (**under 60 columns**) the segment comes off that line and is drawn dim
on the first row of the page instead, above the chips. It is moved, not dropped — what the
header sheds first is the goal, which you can still read in the conversation.

The nodes under the planner run on the `worker` role, which sits on **small work** — a
different model, and not on this line. `/settings` → Providers lists both.

## Pinning one role to its own model, and unpinning it

In `/settings` → Providers, move onto any row of the roles list and press **enter**. That
opens the model picker — the same one `/model` opens, same filter box, same ranking — and
what you choose is **pinned** to that role alone. The row then reads
`<model>  pinned`, and the legend at the foot offers **del unpin**. Press
**del** on a pinned row to clear it; the role goes back to following its class.

The picker a role opens asks that role's own question. `vision` offers only models that
can see; every other role offers the models you can hold a conversation with.

Every pin lives in the one **pinned roles** row, written as
`planner:openai/gpt-5, worker:openai/gpt-5-mini`. Pinning from the list and typing into
that row are **the same setting** — a pin you typed by hand shows in the list as pinned,
and pinning from the list rewrites the row without disturbing the other pins in it.

**A third door: just ask.** "Use `deepseek/deepseek-v4-pro` for planning and for designing
harnesses" is a sentence aforge acts on — it looks the row up with `settings` and writes it
with `change_setting`, into the same `models.roles` row, after asking you. The four class
rows (`models.tiers.reflex`, `models.tiers.low`, `models.tiers.high`,
`models.tiers.mastermind`), the crew word (`models.crew`) and the pins are all writable that
way; only the role **slots** further down the Providers tab are not, because those are
bindings the running session holds rather than values in your profile.

So the ladder for any role, most specific first: **its pin**, then **its class's model**,
then **the model you are talking to**.

Two things worth knowing:

- **A change is live.** The next call aforge makes on its own uses it — whether you changed
  it in the panel, with `/crew`, or by asking. It used to land on the next session, and it no
  longer does. A turn already in flight finishes on what it started with.
- Naming a model in the sentence outranks all of it for that piece of work. `orchestrate
  the migration with opus` runs the planner *and* every node on opus; `make a harness for
  triaging flakes with opus` designs on opus. The roles decide only when you named nothing.

## Reasoning effort — making the model think harder or faster

Reasoning effort is set in the model picker with **ctrl+t**, on the model under the cursor.
Each press walks it round: off → low → medium → high → off.

- On a model whose catalog row does not accept a reasoning knob, ctrl+t does **nothing at
  all, silently** — the level would be a 400 at the next turn. A row that published nothing
  counts as "does not accept", so the knob is missing rather than offered wrongly.
- The level lives on the session, **per model id**. It survives switching away to another
  model and back. `/new` forgets it.
- Where the level is set, it is shown after the id as `<id>:<level>` — in the picker row, and
  on the `model` line of `/status`.

## What this conversation has cost — /cost

`/cost` (also `/usage`, `/tokens`, `/spend`) prints what this conversation has spent, and on
what, into the conversation. Up to five aligned lines:

| Line | What it is |
|---|---|
| `spend` | the money, printed only when it is above zero |
| `tokens` | `48.1k in · 3.2k out`, or one half alone, or the combined figure |
| `cache` | `31.2k read · saved $0.0180` — the money half only when a price pair was published |
| `model calls` | **requests to the provider**, deliberately not "turns" |
| `time` | how long |

`model calls` is the line people misread. One thing you type can become several requests to
the provider — every step of a turn is its own request — so this figure is normally larger
than the number of times you have spoken.

It counts **every** request, not only the ones in your turns: naming the session, a judge
deciding where something should be routed, looking at a picture, every request a task's
own agent made on its own lane, and every request a harness run made while it walked its
program. That is deliberate, because the `spend` line above it is the
sum over exactly those requests — a smaller count beside it would be a bill divided by the
wrong number.

**Every line is dropped when its figure is absent.** A provider that publishes no cache
accounting says nothing about caches, rather than teaching you that your cache never hits.

It will not go silent. A session with no figures at all answers exactly:

```
nothing spent yet — this session has not sent a turn.
```

## Does /cost remember after I close and resume — is the spend kept across restarts

Yes. The figures are the **whole conversation's**, not this sitting's.

Every turn that spent something, and every call made beside a turn, appends a line to the
conversation's own transcript recording what it cost. When you resume, those lines are read
back and added up before anything else happens, and that sum *is* the session's totals. So
`/cost`, `/status` and the status line the day after show yesterday's money and tokens with
today's added on top, in one figure.

Two things follow from that:

- A conversation whose transcript has no such lines yet — one written by an older build, or
  one that has genuinely never spent anything — reports only what has happened **since you
  reopened it**. There is nothing to rebuild from, and aforge does not invent a figure.
- `/new` starts a fresh conversation with a fresh file, so it starts at nothing. Resuming an
  old conversation is the opposite: it picks the old bill back up.

The `model calls` count is rebuilt the same way, so a resumed conversation's call count also
covers the requests made before the restart.

## Everything at once — /status

`/status` (also `/info`, `/context`) prints **every fact the status line can carry**, one per
line, into the conversation. It is not a panel. Usage totals are refreshed first, so a
command typed between turns answers from what the session holds now.

The labels come in this order, each dropped when its value is empty: `session`, `task` (only
inside a task room), `model` (the full routing address, with `:level` when a reasoning level
is set), `task model` (only in a room), `served`, then the telemetry segments under their own
words — `background`, `changes`, `spend`, `context`, `cache`, `rate`, `compaction`,
`approvals`, `state` — then `tasks`, `place`, `keys`, and finally `file`. Labels are padded
into two aligned columns.

Two things differ deliberately from the status line on screen:

- the session **file** is added, because a path is a thing you copy into another program;
- the `spend` line is **dropped** when nothing has been spent. The live status line keeps
  `$0.00`; a note printed into the conversation must not.

Over `--host`, the `place` and `file` values are written in full as `machine:/path`.

## How much room the conversation has, and giving it a longer context window

The context window is, most specific first: the one the surface set for the model actually in
use, then the window the session was configured with, then a conservative default of
**128,000 tokens** (the smallest window among the models this surface routes to).

Compaction is triggered at **`window − max(15% of window, 16384)`**, with that reserve
clamped to at most half the window. When a pass runs, the **verbatim tail** it keeps is
**20,000 tokens**, capped at a quarter of the window.

The current size is the larger of two figures: the context size the provider last reported,
and an estimate of the transcript at 4 bytes per token. That way a 300KB tool result appended
since the last response is already visible to the threshold.

An unknown window has no threshold at all.

**Accuracy note.** aforge also carries a shared context-budget package with a 60%-fill rule,
a 160k working set and a 250% reuse law. **That package is not used by this chat.** Its
consumer is the sub-harness leaf sizing elsewhere in aforge. The chat's own law is the one
above — do not describe this conversation as filling to 60%.

## What happens before the conversation is summarized

aforge does not jump straight to summarizing. There are rungs before it.

**Rung 1 — stubbing.** At the end of every completed turn, tool results older than the last
**4 turns** and larger than **1500 bytes** are replaced *in the live context* by a pointer
line:

```
[output stubbed — 214332 bytes · full output: .aforge-v3/stubs/<hash>.txt]
```

The bytes are written to disk first, named by their own digest, and the model can `read` them
back at any time. **The journal is never stubbed** — the record on disk keeps the whole
result. An interrupted or failed turn is left alone, and a session with no workspace does
nothing here.

**Rung 2 — page images.** Instead of summarizing the part being dropped, it can be
photographed: rendered verbatim to monospaced page images that the model reads back. No model
call, nothing paraphrased. This rung is chosen only when you gave `/compact` no focus, there
is a workspace, there is page budget, and the model in use can read images. Pages are 120
columns by 64 lines, greyscale, deterministic, footed `<title> | context page 1 of 4`, and
saved under `<workspace>/.aforge-v3/frames/`. The ceiling is **8 pages**; anything past it is
summarized and appended after the pages.

**Rung 3 — the summary.** One call on the session's own model, with no tools, that must
produce six sections — `## Goal`, `## Constraints & Preferences`, `## Progress`,
`## Key Decisions`, `## Next Steps`, `## Critical Context` — and must reproduce any
unanswered question verbatim and preserve exact paths, symbols, commands and error text.

## What a compaction pass keeps

The transcript is cut at a message boundary, walking back from the tail until the keep-recent
budget is spent. The system message is never cut.

What is rebuilt, in order: the system message, then the page images if there are any, then
the summary note if there is one, then **the state block verbatim**, then the kept tail. The
state block — what `track` and `commit` recorded — is injected directly and is **never routed
through the summarizer**, so working state cannot be paraphrased away.

The note the model reads above a summary begins:

```
[context compacted] Everything before this point was summarized to fit the context window. This note is the record of that conversation — it is not something either of us said, and any question inside it is still open.
```

A pass can decline: `session: nothing to compact` (everything already fits in the tail),
`session: a compaction pass is already running`, or `session: summarizer returned nothing`.

## When compaction happens by itself

Three ways a pass starts:

- **Automatically**, after any step where the estimate is over the threshold. A failed pass is
  not a failed turn.
- **On a context-overflow error from the provider**, once per turn. This one runs **even when
  automatic compaction is switched off** — the switch governs the automatic pass, not the
  recovery from a request the provider has already refused. Overflow errors are never retried.
- **On demand**, when you ask for it.

While a pass runs you see `compacting ~84k tokens` (`~842` under a thousand). On success:
`compacted from ~84k tokens, kept last ~20k`, or with page images
`compacted from ~84k tokens to 4 page images, kept last ~20k`. On failure:
`compaction failed · context unchanged`. A failed pass always settles its row.

## /compact — compacting now

`/compact` summarizes the conversation on demand. It notes `compacting…` immediately and runs
the pass off the loop, so the surface stays alive.

**Success is silent.** There is no "done" message — a compaction that worked simply leaves the
conversation shorter. A failure comes back as `compact failed: ` followed by the error.

A `/compact` given a focus always goes to the summary rung rather than page images, because a
renderer cannot be careful about anything. The focus is *appended* to the summarizer's
instructions and never replaces them, so one careless phrase cannot cost the next session its
file paths. It travels on that one request and no further.

## Turning automatic compaction off

Launch with `aforge chat --no-compact` or `aforge resume --no-compact`. The flag's own help
text reads `never compact automatically`.

What it turns off is exactly the automatic threshold check. Still working:

- `/compact`, when you ask for it, and
- the recovery pass when the provider itself refuses a request as too long.

With a `--host` remote launch the flag is **refused rather than ignored**, because it cannot
travel to the other machine.

## Spending limits

A session can carry a spend ceiling. When it is set, a turn is stopped **before it starts**,
reading the session's own recorded usage — so a turn already in flight is never cut in half.
The refusal arrives as an error carrying the sentence:

```
session: the spend rail was reached
```

A ceiling of zero means there is none.

**The ceiling counts what this conversation spent before you resumed it.** The usage the
rail reads is the rebuilt total, which includes every turn from every earlier sitting — so a
ceiling reached yesterday is still reached when you open the conversation today, and the
first thing you type is refused before it runs. That is the one place the rebuilt bill
changes what happens rather than only what is printed. If you want a clean allowance, raise
the ceiling on the `session ceiling` row, or start a fresh conversation with `/new`; there
is no way to zero a conversation's recorded spend while keeping the conversation.

The settings panel's **Session** tab carries a row for it, labelled `session ceiling`. The
panel's search matches a row's registry key as well as its label, so typing either `spendRail`
or `ceiling` finds it.
