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

## Which model does the planning, the working and the small calls aforge makes for itself

aforge makes calls you did not type: naming a session, the summary a compaction keeps, the
safety gate, the check on finished task work, the planner of an adaptive run and the nodes
under it, the designer of a saved harness page, looking at an image. Each of those is a
**role**, and every role sits on one of two **tiers** you set once, in `/settings` → Session:

- **small work** — the cheap model, for short things a wrong answer costs a glance.
- **careful work** — the capable model, for the ones a wrong answer destroys something.

Leave either blank and the roles on it follow the model you are talking to. There is no
tier for a role to fall through to before that — a fresh install with nothing set makes
every one of these calls on your own model, which is what it did before tiers existed.

Directly under the **pinned roles** row the panel lists **every registered role**, one per
line: the role's name, the tier answering it, and the model that comes out. As shipped:

| role | tier | what it is |
| --- | --- | --- |
| `title` | small work | the name a session gives itself |
| `compaction` | careful work | the summary that is all that survives a compaction |
| `consolidate` | small work | what gets kept out of a session's memory |
| `guardian` | small work | "is this one tool call plainly safe" |
| `auditor` | careful work | whether finished-looking task work is actually finished |
| `planner` | careful work | an adaptive run's plan, amended after every node |
| `designer` | careful work | writes and reviews a harness page before it is saved |
| `worker` | small work | one node of an adaptive run |
| `vision` | careful work | reads images for a model that cannot see them |

The list is built from what is registered in the running binary, so it is the truth about
this build rather than a table someone kept up to date.

One caveat on `vision`: the **looking** row on the Providers tab is the front door for
which model sees, and it wins over this role's tier. The `vision` pin is the second rung
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
the **careful work** tier, then the model you are talking to. With a tier set it is usually
*not* the model in the rest of this conversation, which is why the run's own page says it
rather than leaving you to work it out.

It never changes while a run is going: the ladder is walked once, at the start.

On a narrow screen (**under 60 columns**) the segment comes off that line and is drawn dim
on the first row of the page instead, above the chips. It is moved, not dropped — what the
header sheds first is the goal, which you can still read in the conversation.

The nodes under the planner run on the `worker` role, which is a different model and is not
on this line. `/settings` → Session lists both.

## Pinning one role to its own model, and unpinning it

In `/settings` → Session, move onto any row of the roles list and press **enter**. That
opens the model picker — the same one `/model` opens, same filter box, same ranking — and
what you choose is **pinned** to that role alone. The row then reads
`careful work · <model>  pinned`, and the legend at the foot offers **del unpin**. Press
**del** on a pinned row to clear it; the role goes back to following its tier.

The picker a role opens asks that role's own question. `vision` offers only models that
can see; every other role offers the models you can hold a conversation with.

Every pin lives in the one **pinned roles** row, written as
`planner:openai/gpt-5, worker:openai/gpt-5-mini`. Pinning from the list and typing into
that row are **the same setting** — a pin you typed by hand shows in the list as pinned,
and pinning from the list rewrites the row without disturbing the other pins in it.

**A third door: just ask.** "Use `deepseek/deepseek-v4-pro` for planning and for designing
harnesses" is a sentence aforge acts on — it looks the row up with `settings` and writes it
with `change_setting`, into the same `models.roles` row, after asking you. The two tier
rows (`models.tiers.high`, `models.tiers.low`) and the pins are all writable that way; only
the role **slots** on the Providers tab are not, because those are bindings the running
session holds rather than values in your profile.

So the ladder for any role, most specific first: **its pin**, then **its tier's model**,
then **the model you are talking to**.

Two limits worth knowing:

- A change here lands on the **next session**. The models are resolved once when aforge
  starts, so that two calls in one conversation cannot answer to different settings.
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

**Every line is dropped when its figure is absent.** A provider that publishes no cache
accounting says nothing about caches, rather than teaching you that your cache never hits.

It will not go silent. A session with no figures at all answers exactly:

```
nothing spent yet — this session has not sent a turn.
```

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

The settings panel's **Session** tab carries a row for it, labelled `session ceiling`. The
panel's search matches a row's registry key as well as its label, so typing either `spendRail`
or `ceiling` finds it.
